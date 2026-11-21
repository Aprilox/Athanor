package main

// External MCP clients (ARCHITECTURE.md §Ouverture, stage 11): Claude Desktop, Claude Code or another
// Athanor get a client token only by pairing. The window (Settings › MCP clients › Connect) shows a 6-digit
// code (10 minutes, 5 tries then closed); the client sends it to POST /api/mcp/appairer, then asks
// POST /api/mcp/appairer/statut until the owner accepts on screen with a scope (projects, tools). The token
// is handed over once and stored hashed in hub/clients.json (0600). The emergency stop suspends every
// client token until Resume; revocation is the window's.

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ClientMCP: a paired external client. Jeton is the sha256 of its token (hacherJeton), never the token.
type ClientMCP struct {
	ID      string   `json:"id"`
	Nom     string   `json:"nom"`
	Sorte   string   `json:"sorte"` // desktop | code | athanor
	Jeton   string   `json:"jeton"`
	Outils  []string `json:"outils"`
	Projets []string `json:"projets"` // "*" = every project
	Cree    string   `json:"cree"`
	Vu      string   `json:"vu,omitempty"`
}

// outilsClients: the tools a client may be granted (hub_outils_externes.go, M183). None decides an
// approval, launches a mission, reads a vault value or touches a server.
var outilsClients = []string{"etat", "projets", "plan", "demandes", "ajouter_demande", "pilote", "missions", "proposer_mission", "approbations"}

// outilsAthanor: granted to another Athanor only (a hub paired with a module, module_sans_fenetre.go).
var outilsAthanor = []string{"serveurs", "taches_serveur", "envoyer_bus"}

var sortesClients = []string{"desktop", "code", "athanor"}

// pauseRefusClient: a wrong code waits this long (a variable for the tests).
var pauseRefusClient = time.Second

// demandeClient: a client that gave the right code and waits for the owner's answer on screen.
type demandeClient struct {
	Ref   string `json:"ref"`
	Nom   string `json:"nom"`
	Sorte string `json:"sorte"`
	IP    string `json:"ip"`
	Quand int64  `json:"quand"`
	etat  string // attente | acceptee | refusee
	jeton string // the token, kept until said once
}

type magasinClients struct {
	mu       sync.Mutex
	chemin   string
	liste    []ClientMCP
	code     string
	codeFin  time.Time
	essais   int
	demandes map[string]*demandeClient // by the secret the client polls with
	essaisIP map[string]int
	essaisLe time.Time // the attempts per address count over fenetreEssaisIP, then start again
}

func cheminClients() string { return filepath.Join(dossierAthanor(), "hub", "clients.json") }

// cli: the client store, read once (an unreadable file starts empty: every client must pair again).
func (s *Serveur) cli() *magasinClients {
	s.clientsOnce.Do(func() {
		m := &magasinClients{chemin: cheminClients(), demandes: map[string]*demandeClient{}, essaisIP: map[string]int{}}
		if b, err := os.ReadFile(m.chemin); err == nil && json.Unmarshal(b, &m.liste) != nil {
			m.liste = nil
		}
		s.clientsEtat = m
	})
	return s.clientsEtat
}

// enregistrer: m.mu held.
func (m *magasinClients) enregistrer() error {
	if err := os.MkdirAll(filepath.Dir(m.chemin), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m.liste, "", "  ")
	return ecrireAtomique(m.chemin, b, 0o600)
}

// nettoyer: the requests over 10 minutes old are forgotten (m.mu held).
func (m *magasinClients) nettoyer() {
	for k, d := range m.demandes {
		if time.Since(time.Unix(d.Quand, 0)) > dureeCodeDirect {
			delete(m.demandes, k)
		}
	}
}

// urgenceClients: the emergency stop is on (client tokens suspended, no new pairing).
func (s *Serveur) urgenceClients() bool { return s.coffre != nil && s.coffre.Urgence() != "" }

// verifierClient: the client this token belongs to; false when unknown, revoked or during an emergency stop.
func (s *Serveur) verifierClient(jeton string) (ClientMCP, bool) {
	if jeton == "" || s.urgenceClients() {
		return ClientMCP{}, false
	}
	h := hacherJeton(jeton)
	m := s.cli()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.liste {
		if subtle.ConstantTimeCompare([]byte(c.Jeton), []byte(h)) == 1 {
			now := time.Now().UTC().Format(time.RFC3339)
			if !strings.HasPrefix(m.liste[i].Vu, now[:16]) { // written at most once a minute
				m.liste[i].Vu = now
				_ = m.enregistrer()
			}
			return m.liste[i], true
		}
	}
	return ClientMCP{}, false
}

func (s *Serveur) auditClient(nom, action, resultat string) {
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "client:" + nom, Vers: "hub", Action: action, Resultat: resultat})
}

// apiAppairerClient: POST /api/mcp/appairer {code, nom, sorte} → 202 {attente, demande} (outside garde, local only).
func (s *Serveur) apiAppairerClient(w http.ResponseWriter, r *http.Request) {
	var c struct{ Code, Nom, Sorte string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&c) != nil {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	c.Nom = coupeOctets(strings.TrimSpace(c.Nom), 60)
	if depuisTunnel(r) && c.Sorte != "athanor" { // through the tunnel: another Athanor only, never Desktop or Code
		repondreDirect(w, 403, map[string]string{"erreur": "only another Athanor pairs through remote access"})
		return
	}
	if c.Nom == "" || !slices.Contains(sortesClients, c.Sorte) {
		repondreDirect(w, 400, map[string]string{"erreur": "a name and a kind (desktop, code or athanor) are needed"})
		return
	}
	if s.urgenceClients() {
		repondreDirect(w, 403, map[string]string{"erreur": "Athanor is in emergency stop: pairing is closed until Resume."})
		return
	}
	ip := ipAppel(r)
	m := s.cli()
	m.mu.Lock()
	if time.Since(m.essaisLe) > fenetreEssaisIP {
		m.essaisIP, m.essaisLe = map[string]int{}, time.Now()
	}
	m.essaisIP[ip]++
	trop := m.essaisIP[ip] > 20
	bon := m.code != "" && time.Now().Before(m.codeFin) &&
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(c.Code)), []byte(m.code)) == 1
	ferme := false
	if !bon && m.code != "" {
		m.essais++
		if m.essais >= essaisCodeMax { // 5 wrong codes: this code is closed, the window opens a new one
			m.code, ferme = "", true
		}
	}
	var d *demandeClient
	secret := ""
	if bon && !trop {
		delete(m.essaisIP, ip) // a pairing that went through counts against nobody
		m.code = ""            // a code is used once
		m.nettoyer()
		secret = aleatoire(24)
		d = &demandeClient{Ref: aleatoire(6), Nom: c.Nom, Sorte: c.Sorte, IP: ip, Quand: time.Now().Unix(), etat: "attente"}
		m.demandes[secret] = d
	}
	m.mu.Unlock()
	if d == nil {
		res := "code refusé"
		if ferme {
			res = "code refusé (5 essais : code fermé)"
		}
		s.auditClient(c.Nom, "mcp appairage", res)
		time.Sleep(pauseRefusClient)
		repondreDirect(w, 403, map[string]string{"erreur": "This code is wrong or expired: open a new one in Athanor (Settings › MCP clients › Connect)."})
		return
	}
	s.auditClient(c.Nom, "mcp appairage", "code bon, en attente de l'accord")
	go notifier(texteDemandeClient(s.langue(), c.Nom), "Athanor Hub")
	repondreDirect(w, 202, map[string]any{"attente": true, "demande": secret})
}

func texteDemandeClient(langue, nom string) string {
	m := map[string]string{"fr": "%s veut se brancher au MCP d'Athanor", "es": "%s quiere conectarse al MCP de Athanor",
		"de": "%s möchte sich mit Athanors MCP verbinden", "it": "%s vuole collegarsi all'MCP di Athanor", "pt": "%s quer ligar-se ao MCP do Athanor"}
	f, ok := m[langue]
	if !ok {
		f = "%s wants to connect to Athanor's MCP"
	}
	return strings.Replace(f, "%s", nom, 1)
}

// apiStatutClient: POST /api/mcp/appairer/statut {demande} → {attente} | {jeton} (once) | 403 refused | 404 gone.
func (s *Serveur) apiStatutClient(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Demande string `json:"demande"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&c) != nil || c.Demande == "" {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	m := s.cli()
	m.mu.Lock()
	m.nettoyer()
	d := m.demandes[c.Demande]
	var etat, jeton string
	if d != nil {
		etat, jeton = d.etat, d.jeton
		if etat != "attente" {
			delete(m.demandes, c.Demande) // said once
		}
	}
	m.mu.Unlock()
	switch etat {
	case "":
		repondreDirect(w, 404, map[string]string{"erreur": "No answer in 10 minutes: open a new code in Athanor."})
	case "attente":
		repondreDirect(w, 200, map[string]any{"attente": true})
	case "refusee":
		repondreDirect(w, 403, map[string]string{"erreur": "The owner refused this client."})
	default:
		repondreDirect(w, 200, map[string]string{"jeton": jeton})
	}
}

// vueClients: what the window shows (never a token nor its hash).
func (s *Serveur) vueClients() map[string]any {
	m := s.cli()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nettoyer()
	liste := []ClientMCP{}
	for _, c := range m.liste {
		c.Jeton = ""
		liste = append(liste, c)
	}
	demandes := []demandeClient{}
	for _, d := range m.demandes {
		if d.etat == "attente" {
			demandes = append(demandes, *d)
		}
	}
	slices.SortFunc(demandes, func(a, b demandeClient) int { return int(a.Quand - b.Quand) })
	v := map[string]any{"clients": liste, "demandes": demandes, "outils": outilsClients, "outils_athanor": outilsAthanor, "urgence": s.urgenceClients()}
	if exe, err := os.Executable(); err == nil { // the command the snippets give to Claude Desktop and Claude Code
		v["commande"] = exe
	}
	if m.code != "" && time.Now().Before(m.codeFin) {
		v["code"], v["code_fin"], v["essais"] = m.code, m.codeFin.Unix(), m.essais
	}
	return v
}

// apiClients: GET the clients, requests and open code; POST {action: code|fermer|accepter|refuser|revoquer}.
func (s *Serveur) apiClients(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.ecrireJSON(w, s.vueClients())
		return
	}
	var c struct {
		Action  string   `json:"action"`
		Ref     string   `json:"ref"`
		ID      string   `json:"id"`
		Projets []string `json:"projets"`
		Outils  []string `json:"outils"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, 400, "Invalid request.")
		return
	}
	var err error
	switch c.Action {
	case "code":
		if s.urgenceClients() {
			err = errors.New("Athanor is in emergency stop: pairing is closed until Resume.")
			break
		}
		m := s.cli()
		m.mu.Lock()
		m.code, m.codeFin, m.essais = sixChiffres(), time.Now().Add(dureeCodeDirect), 0
		code := m.code
		m.mu.Unlock()
		ecrireCodeTest(code)
	case "fermer":
		m := s.cli()
		m.mu.Lock()
		m.code = ""
		m.mu.Unlock()
	case "accepter", "refuser":
		err = s.deciderClient(c.Ref, c.Action == "accepter", c.Projets, c.Outils)
	case "revoquer":
		err = s.revoquerClient(c.ID)
	default:
		err = errors.New("Unknown action.")
	}
	if err != nil {
		s.erreur(w, 400, err.Error())
		return
	}
	s.ecrireJSON(w, s.vueClients())
}

// ecrireCodeTest: the CI's two instances (deux_instances_test.go) read a new pairing code from the file
// ATHANOR_TEST_CODE_FICHIER names; only under ATHANOR_TEST=1, never in a real launch.
func ecrireCodeTest(code string) {
	if f := os.Getenv("ATHANOR_TEST_CODE_FICHIER"); f != "" && os.Getenv("ATHANOR_TEST") == "1" {
		_ = os.WriteFile(f, []byte(code), 0o600)
	}
}

// perimetreClient: the scope chosen on screen, checked (known tools only; "*" or projects that exist).
func (s *Serveur) perimetreClient(projets, outils []string) ([]string, []string, error) {
	if len(outils) == 0 {
		return nil, nil, errors.New("Choose at least one tool.")
	}
	for _, o := range outils {
		if !slices.Contains(outilsClients, o) && !slices.Contains(outilsAthanor, o) {
			return nil, nil, errors.New("Unknown tool: " + o)
		}
	}
	if len(projets) == 0 {
		return nil, nil, errors.New("Choose the projects this client may see.")
	}
	if slices.Contains(projets, "*") {
		projets = []string{"*"}
	} else {
		for _, n := range projets {
			if _, ok := s.projetNomme(n); !ok {
				return nil, nil, errors.New("Unknown project: " + n)
			}
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(projets))), slices.Compact(slices.Sorted(slices.Values(outils))), nil
}

// deciderClient: the owner accepts (the client is stored, its token made and kept for one status call) or refuses.
func (s *Serveur) deciderClient(ref string, accepter bool, projets, outils []string) error {
	var err error
	if accepter {
		if s.urgenceClients() {
			return errors.New("Athanor is in emergency stop: pairing is closed until Resume.")
		}
		if projets, outils, err = s.perimetreClient(projets, outils); err != nil {
			return err
		}
	}
	m := s.cli()
	m.mu.Lock()
	m.nettoyer()
	var d *demandeClient
	for _, x := range m.demandes {
		if x.Ref == ref && x.etat == "attente" {
			d = x
		}
	}
	if d == nil {
		m.mu.Unlock()
		return errors.New("This request is no longer there (over 10 minutes old).")
	}
	nom := d.Nom
	if accepter && d.Sorte != "athanor" && slices.ContainsFunc(outils, func(o string) bool { return slices.Contains(outilsAthanor, o) }) {
		m.mu.Unlock()
		return errors.New("The servers' tools are for another Athanor only.")
	}
	if !accepter {
		d.etat = "refusee"
		m.mu.Unlock()
		s.auditClient(nom, "mcp appairage", "refusé à l'écran")
		return nil
	}
	jeton := aleatoire(32)
	t := time.Now().UTC().Format(time.RFC3339)
	m.liste = append(m.liste, ClientMCP{ID: aleatoire(8), Nom: nom, Sorte: d.Sorte, Jeton: hacherJeton(jeton), Outils: outils, Projets: projets, Cree: t})
	if err = m.enregistrer(); err != nil {
		m.liste = m.liste[:len(m.liste)-1]
		m.mu.Unlock()
		return err
	}
	d.etat, d.jeton = "acceptee", jeton
	m.mu.Unlock()
	s.auditClient(nom, "mcp appairage", "accepté : projets "+strings.Join(projets, ",")+" ; outils "+strings.Join(outils, ","))
	return nil
}

// revoquerClient: the client's token stops working at once.
func (s *Serveur) revoquerClient(id string) error {
	m := s.cli()
	m.mu.Lock()
	i := slices.IndexFunc(m.liste, func(c ClientMCP) bool { return c.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return errors.New("Unknown client.")
	}
	c := m.liste[i]
	m.liste = slices.Delete(m.liste, i, i+1)
	err := m.enregistrer()
	if err != nil {
		m.liste = slices.Insert(m.liste, i, c)
	}
	m.mu.Unlock()
	if err == nil {
		s.auditClient(c.Nom, "mcp révocation", "jeton révoqué")
	}
	return err
}

// cleTunnel: the request came through the remote access tunnel (gestionDistance), not from this computer.
const cleTunnel = "tunnel"

func depuisTunnel(r *http.Request) bool {
	v, _ := r.Context().Value(cleContexte(cleTunnel)).(bool)
	return v
}

// clientAthanor: the request carries the token of a paired remote Athanor (sorte athanor), the only
// client token valid through the tunnel (Desktop, Code and the agents' session tokens stay local).
func (s *Serveur) clientAthanor(r *http.Request) bool {
	jeton, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	c, ok := s.verifierClient(strings.TrimSpace(jeton))
	return ok && c.Sorte == "athanor"
}

// fenetreEssaisIP: 20 pairing attempts from one address in this time, then it waits (every local client comes from
// 127.0.0.1: a count that never started again closed pairing until a restart).
const fenetreEssaisIP = 10 * time.Minute
