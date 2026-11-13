package main

// Remote Athanors (ARCHITECTURE.md §Ouverture, stage 11): this hub is a client of another Athanor's MCP,
// through the address of its remote access tunnel (or, for an Athanor of this PC, its loopback port read
// in its announcement). Adding one = its address + the 6 digits it shows (the same pairing as the MCP
// clients, sorte athanor; its owner accepts on its screen). The token goes to the vault's infra side
// (athanor-distant-<id>); vault locked = the token waits in memory and the remote says so. Each remote
// is asked "etat" every 15 s (online / offline, never a blocking error) and appears as a module
// (moduleDistant): its tile, and the bus (I5: checked and audited here) relayed to its MCP.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Distant: a paired remote Athanor. Coffre is the vault entry holding its token (never the token).
type Distant struct {
	ID       string   `json:"id"`
	Nom      string   `json:"nom"`
	URL      string   `json:"url,omitempty"`      // its tunnel's address
	Instance string   `json:"instance,omitempty"` // or: the id of an Athanor of this PC (port read in its announcement)
	Modules  []string `json:"modules"`
	Outils   []string `json:"outils"` // the tools its owner granted this hub
	Vu       string   `json:"vu,omitempty"`
	Coffre   string   `json:"coffre,omitempty"`
}

// etatDistant: en_ligne | hors_ligne | coffre (vault locked, token unreadable) | revoque (401) | "" (not asked yet)
type magasinDistants struct {
	mu       sync.Mutex
	liste    []Distant
	etats    map[string]string
	jetons   map[string]string          // tokens waiting for the vault to open (memory only)
	attentes map[string]*Distant        // pairings waiting for the other owner's yes, by request secret
	projets  map[string][]ProjetDistant // a remote that carries dev: its projects (the last list read, kept offline)
	message  string                     // the last pairing that failed, said once in the window
}

func cheminDistants() string { return filepath.Join(dossierAthanor(), "hub", "distants.json") }

// pauseStatutDistant: how often a pending pairing asks whether the other owner answered (a variable for the tests).
var pauseStatutDistant = 2 * time.Second

var clientDistants = &http.Client{Timeout: 10 * time.Second}

func (s *Serveur) dst() *magasinDistants {
	s.distantsOnce.Do(func() {
		m := &magasinDistants{etats: map[string]string{}, jetons: map[string]string{}, attentes: map[string]*Distant{}, projets: map[string][]ProjetDistant{}}
		if b, err := os.ReadFile(cheminDistants()); err == nil && json.Unmarshal(b, &m.liste) != nil {
			m.liste = nil
		}
		s.distantsEtat = m
	})
	return s.distantsEtat
}

// enregistrer: m.mu held.
func (m *magasinDistants) enregistrer() error {
	if err := os.MkdirAll(filepath.Dir(cheminDistants()), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m.liste, "", "  ")
	return ecrireAtomique(cheminDistants(), b, 0o600)
}

// urlDistant: the remote's tunnel address (https), or a loopback http address (an Athanor of this PC, the tests).
func urlDistant(brut string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(brut))
	mauvaise := errors.New("Give the address of the other Athanor's remote access (https://…).")
	if err != nil || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", mauvaise
	}
	h := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1")) {
		return "", mauvaise
	}
	return u.Scheme + "://" + u.Host, nil
}

// baseDistant: where its MCP answers now.
func baseDistant(d Distant) (string, error) {
	if d.Instance == "" {
		return d.URL, nil
	}
	var a Annonce
	if b, err := os.ReadFile(filepath.Join(dossierAnnonces(), d.Instance+".json")); err != nil || json.Unmarshal(b, &a) != nil || a.Port <= 0 || a.Port > 65535 {
		return "", errors.New("not running on this computer")
	}
	return "http://127.0.0.1:" + strconv.Itoa(a.Port), nil
}

func nomCeHub() string {
	nom, _ := os.Hostname()
	return "Athanor " + nom + " " + idInstance()
}

func (s *Serveur) auditDistant(nom, action, resultat string) {
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "distant:" + nom, Action: action, Resultat: resultat})
}

// ajouterDistant: sends the code to the remote's pairing; its owner then accepts on its screen.
func (s *Serveur) ajouterDistant(brut, code string) error {
	base, err := urlDistant(brut)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return errors.New("Type the 6-digit code shown by the other Athanor (Settings › MCP clients › Connect).")
	}
	u, _ := url.Parse(base)
	corps, _ := json.Marshal(map[string]string{"code": code, "nom": nomCeHub(), "sorte": "athanor"})
	r, err := clientDistants.Post(base+"/api/mcp/appairer", "application/json", bytes.NewReader(corps))
	if err != nil {
		return errors.New("This Athanor does not answer any more.")
	}
	defer r.Body.Close()
	var v struct{ Demande, Erreur string }
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&v)
	s.auditDistant(u.Host, "appairage distant", strconv.Itoa(r.StatusCode))
	if r.StatusCode != http.StatusAccepted || v.Demande == "" {
		return errors.New(firstNonEmpty(v.Erreur, "The other Athanor refused the code."))
	}
	s.suivreAppairageDistant(Distant{Nom: u.Host, URL: base}, v.Demande)
	return nil
}

// suivreAppairageDistant: asks the remote for its owner's answer until the token comes (or a refusal, or 11 min).
func (s *Serveur) suivreAppairageDistant(d Distant, demande string) {
	m := s.dst()
	m.mu.Lock()
	m.attentes[demande] = &d
	m.mu.Unlock()
	go func() {
		fin := func(message string) {
			m.mu.Lock()
			delete(m.attentes, demande)
			if message != "" {
				m.message = message
			}
			m.mu.Unlock()
			if d.Instance != "" {
				demandesInstances.Lock()
				delete(demandesInstances.m, d.Instance)
				demandesInstances.Unlock()
			}
		}
		corps, _ := json.Marshal(map[string]string{"demande": demande})
		for limite := time.Now().Add(dureeCodeDirect + time.Minute); time.Now().Before(limite); time.Sleep(pauseStatutDistant) {
			base, err := baseDistant(d)
			if err != nil {
				continue
			}
			r, err := clientDistants.Post(base+"/api/mcp/appairer/statut", "application/json", bytes.NewReader(corps))
			if err != nil {
				continue // the tunnel may come back: ask again
			}
			var v struct{ Jeton, Erreur string }
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&v)
			r.Body.Close()
			switch {
			case r.StatusCode == http.StatusOK && v.Jeton != "":
				err := s.enregistrerDistant(d, v.Jeton)
				msg := ""
				if err != nil {
					msg = err.Error()
				}
				fin(msg)
				return
			case r.StatusCode != http.StatusOK:
				s.auditDistant(d.Nom, "appairage distant", "refusé ou expiré")
				fin(firstNonEmpty(v.Erreur, "The other Athanor refused this pairing."))
				return
			}
		}
		fin("No answer in 10 minutes: open a new code in the other Athanor.")
	}()
}

// enregistrerDistant: the remote is kept; its token goes to the vault (infra), or waits in memory while it is locked.
func (s *Serveur) enregistrerDistant(d Distant, jeton string) error {
	d.ID, d.Modules, d.Outils = aleatoire(8), []string{}, []string{}
	m := s.dst()
	m.mu.Lock()
	if id, err := s.coffreJetonDistant(d, jeton); err == nil {
		d.Coffre = id
	} else {
		m.jetons[d.ID] = jeton
	}
	m.liste = append(m.liste, d)
	err := m.enregistrer()
	if err != nil {
		m.liste = m.liste[:len(m.liste)-1]
		delete(m.jetons, d.ID)
	}
	m.mu.Unlock()
	if err != nil {
		if d.Coffre != "" {
			_ = s.coffre.Supprimer(d.Coffre)
		}
		return err
	}
	s.auditDistant(d.Nom, "appairage distant", "accepté par l'autre ; jeton "+map[bool]string{true: "au coffre", false: "en attente du coffre"}[d.Coffre != ""])
	go s.rafraichirDistant(d.ID)
	return nil
}

func (s *Serveur) coffreJetonDistant(d Distant, jeton string) (string, error) {
	if s.coffre == nil {
		return "", errors.New("no vault")
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "athanor-distant-" + d.ID, Famille: "secret", Type: "jeton", Portee: "infra",
		Note: "Token of the remote Athanor " + d.Nom}, jeton)
	return e.ID, err
}

// jetonDistant: its token, read from the vault (locked = error) or from memory.
func (s *Serveur) jetonDistant(d Distant) (string, error) {
	if d.Coffre != "" && s.coffre != nil {
		return s.coffre.Valeur(d.Coffre)
	}
	m := s.dst()
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jetons[d.ID]; j != "" {
		return j, nil
	}
	return "", errCoffreVerrouille
}

var errDistantRevoque = errors.New("the remote Athanor refused this hub's token (revoked)")

// appelDistant: one JSON-RPC call to the remote's MCP; its result.
func (s *Serveur) appelDistant(d Distant, methode string, params any) (json.RawMessage, error) {
	jeton, err := s.jetonDistant(d)
	if err != nil {
		return nil, err
	}
	base, err := baseDistant(d)
	if err != nil {
		return nil, err
	}
	corps, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": methode, "params": params})
	req, _ := http.NewRequest("POST", base+"/api/mcp", bytes.NewReader(corps))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jeton)
	r, err := clientDistants.Do(req)
	if err != nil {
		return nil, errors.New("offline")
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusUnauthorized {
		return nil, errDistantRevoque
	}
	var v struct {
		Result json.RawMessage
		Error  *erreurMCP
	}
	if r.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&v) != nil {
		return nil, fmt.Errorf("unexpected answer (%d)", r.StatusCode)
	}
	if v.Error != nil {
		return nil, errors.New(v.Error.Message)
	}
	return v.Result, nil
}

// outilDistant: tools/call on the remote; its text (an error of the tool is an error).
func (s *Serveur) outilDistant(d Distant, nom string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := s.appelDistant(d, "tools/call", map[string]any{"name": nom, "arguments": args})
	if err != nil {
		return "", err
	}
	var v struct {
		Content []struct{ Text string }
		IsError bool
	}
	if json.Unmarshal(res, &v) != nil {
		return "", errors.New("unexpected answer")
	}
	texte := ""
	for _, c := range v.Content {
		texte += c.Text
	}
	if v.IsError {
		return "", errors.New(texte)
	}
	return texte, nil
}

// rafraichirDistant: asks one remote its tools and state (online, modules); never a blocking error.
func (s *Serveur) rafraichirDistant(id string) {
	m := s.dst()
	m.mu.Lock()
	i := slices.IndexFunc(m.liste, func(d Distant) bool { return d.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return
	}
	d, jeton := m.liste[i], m.jetons[id]
	m.mu.Unlock()
	if d.Coffre == "" && jeton != "" { // the vault opened since: the token leaves memory
		if cid, err := s.coffreJetonDistant(d, jeton); err == nil {
			d.Coffre = cid
			m.mu.Lock()
			if j := slices.IndexFunc(m.liste, func(x Distant) bool { return x.ID == id }); j >= 0 {
				m.liste[j].Coffre = cid
				delete(m.jetons, id)
				_ = m.enregistrer()
			}
			m.mu.Unlock()
			s.auditDistant(d.Nom, "jeton distant", "mis au coffre")
		}
	}
	etat, outils, modules, nom := "en_ligne", d.Outils, d.Modules, d.Nom
	res, err := s.appelDistant(d, "tools/list", map[string]any{})
	if err == nil {
		var l struct{ Tools []struct{ Name string } }
		_ = json.Unmarshal(res, &l)
		outils = []string{}
		for _, o := range l.Tools {
			outils = append(outils, o.Name)
		}
		if slices.Contains(outils, "etat") {
			var e struct {
				Instance string
				Modules  []struct{ ID string }
			}
			if texte, err2 := s.outilDistant(d, "etat", nil); err2 == nil && json.Unmarshal([]byte(texte), &e) == nil {
				if u, _ := url.Parse(d.URL); d.URL != "" && u != nil && d.Nom == u.Host && strings.TrimSpace(e.Instance) != "" {
					nom = couper(strings.TrimSpace(e.Instance), 60) // added by its address: named after the instance once it answers
				}
				modules = []string{}
				for _, x := range e.Modules {
					modules = append(modules, x.ID)
				}
			}
		}
	}
	projets, devLu := s.lireProjetsDistant(d, err == nil, outils, modules)
	switch {
	case err == nil:
	case errors.Is(err, errDistantRevoque):
		etat = "revoque"
	case errors.Is(err, errCoffreVerrouille), errors.Is(err, errCoffreSansMdp):
		etat = "coffre"
	default:
		etat = "hors_ligne"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.etats[id] = etat
	if devLu {
		m.projets[id] = projets
	}
	j := slices.IndexFunc(m.liste, func(x Distant) bool { return x.ID == id })
	if j < 0 {
		return
	}
	x := &m.liste[j]
	ecrire := !slices.Equal(x.Outils, outils) || !slices.Equal(x.Modules, modules) || x.Nom != nom
	x.Outils, x.Modules, x.Nom = outils, modules, nom
	if etat == "en_ligne" {
		now := time.Now().UTC().Format(time.RFC3339)
		ecrire = ecrire || !strings.HasPrefix(x.Vu, now[:16]) // at most once a minute
		x.Vu = now
	}
	if ecrire {
		_ = m.enregistrer()
	}
}

// rafraichirDistants: every remote at once (each call has its own 10 s limit).
func (s *Serveur) rafraichirDistants() {
	m := s.dst()
	m.mu.Lock()
	ids := []string{}
	for _, d := range m.liste {
		ids = append(ids, d.ID)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() { s.rafraichirDistant(id) })
	}
	wg.Wait()
}

// surveillerDistants: the remotes asked every 15 s, for the life of the app.
func (s *Serveur) surveillerDistants() {
	for {
		s.rafraichirDistants()
		time.Sleep(15 * time.Second)
	}
}

// retirerDistant: its token revoked on the other side when it answers, then here (vault entry, list).
func (s *Serveur) retirerDistant(id string) error {
	m := s.dst()
	m.mu.Lock()
	i := slices.IndexFunc(m.liste, func(d Distant) bool { return d.ID == id })
	if i < 0 {
		m.mu.Unlock()
		return errors.New("Unknown remote Athanor.")
	}
	d := m.liste[i]
	m.mu.Unlock()
	resultat := "révoqué des deux côtés"
	if _, err := s.appelDistant(d, "athanor/revoquer", map[string]any{}); err != nil {
		resultat = "retiré ici ; l'autre ne répond pas (" + err.Error() + ")"
	}
	m.mu.Lock()
	if i = slices.IndexFunc(m.liste, func(x Distant) bool { return x.ID == id }); i >= 0 {
		m.liste = slices.Delete(m.liste, i, i+1)
	}
	delete(m.jetons, id)
	delete(m.etats, id)
	delete(m.projets, id)
	err := m.enregistrer()
	m.mu.Unlock()
	if d.Coffre != "" && s.coffre != nil {
		_ = s.coffre.Supprimer(d.Coffre)
	}
	s.auditDistant(d.Nom, "retrait distant", resultat)
	return err
}

// vueDistants: what the window shows (never a token, never the vault entry).
func (s *Serveur) vueDistants() map[string]any {
	m := s.dst()
	m.mu.Lock()
	defer m.mu.Unlock()
	liste := []map[string]any{}
	for _, d := range m.liste {
		liste = append(liste, map[string]any{"id": d.ID, "nom": d.Nom, "url": d.URL, "instance": d.Instance, "modules": d.Modules,
			"outils": d.Outils, "vu": d.Vu, "etat": m.etats[d.ID], "memoire": m.jetons[d.ID] != ""})
	}
	attentes := []map[string]any{}
	for _, d := range m.attentes {
		attentes = append(attentes, map[string]any{"nom": d.Nom, "url": d.URL})
	}
	v := map[string]any{"liste": liste, "attentes": attentes}
	if m.message != "" {
		v["message"], m.message = m.message, ""
	}
	return v
}

// apiDistants: POST {action: ajouter (url, code) | retirer (id) | pilote (id, projet, ordre)} (the computer only, like the instances).
func (s *Serveur) apiDistants(w http.ResponseWriter, r *http.Request) {
	var c struct{ Action, URL, Code, ID, Projet, Ordre string }
	if lireCorps(r, &c) != nil {
		s.erreur(w, 400, "Invalid request.")
		return
	}
	var err error
	switch c.Action {
	case "ajouter":
		err = s.ajouterDistant(c.URL, c.Code)
	case "retirer":
		err = s.retirerDistant(c.ID)
	case "pilote":
		err = s.piloteDistant(c.ID, c.Projet, c.Ordre)
	default:
		err = errors.New("Unknown action.")
	}
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ecrireJSON(w, s.vueDistants())
}

// moduleDistant: a remote seen as a module: its tile here, its actions relayed to its MCP through the bus.
type moduleDistant struct {
	d    Distant
	etat string
}

// lecturesDistant: the remote tools that change nothing (no accord, even on prod).
var lecturesDistant = []string{"etat", "projets", "plan", "demandes", "missions", "approbations"}

func (m moduleDistant) ID() string          { return "distant-" + m.d.ID }
func (m moduleDistant) Nom() string         { return m.d.Nom }
func (m moduleDistant) Pages() []PageModule { return nil }
func (m moduleDistant) Resume(*Serveur) ResumeModule {
	etats := map[string][2]string{"en_ligne": {"Online", "ok"}, "hors_ligne": {"Offline", "inactif"},
		"coffre": {"Vault locked", "attention"}, "revoque": {"Pairing revoked", "attention"}}
	e, ok := etats[m.etat]
	if !ok {
		e = [2]string{"Connecting…", "inactif"}
	}
	return ResumeModule{Etat: e[1], Stats: []StatModule{{Cle: "Remote Athanor", Valeur: e[0], Page: "#reglages"},
		{Cle: "Modules", Valeur: len(m.d.Modules), Page: "#reglages"}}}
}
func (m moduleDistant) Elements(*Serveur) []ElementHub { return nil }
func (m moduleDistant) Tutoriel() []EtapeTutoriel      { return nil }
func (m moduleDistant) Actions() []string              { return m.d.Outils }
func (m moduleDistant) Risque(action string) string {
	if slices.Contains(lecturesDistant, action) {
		return "lecture"
	}
	return ""
}
func (m moduleDistant) Recevoir(s *Serveur, msg MessageHub) (MessageHub, error) {
	args := map[string]any{}
	for k, v := range msg.Donnees {
		args[k] = v
	}
	if msg.Projet != "" {
		args["projet"] = msg.Projet
	}
	texte, err := s.outilDistant(m.d, msg.Action, args)
	if err != nil {
		return MessageHub{}, err
	}
	return MessageHub{Donnees: map[string]any{"texte": texte}}, nil
}

// modulesDistants: the paired remotes, as modules of this hub only (never announced as its own).
func (s *Serveur) modulesDistants() []Module {
	m := s.dst()
	m.mu.Lock()
	defer m.mu.Unlock()
	l := []Module{}
	for _, d := range m.liste {
		l = append(l, moduleDistant{d: d, etat: m.etats[d.ID]})
	}
	return l
}

// modulesTous: this Athanor's modules then its remotes.
func (s *Serveur) modulesTous() []Module { return append(modulesHub(), s.modulesDistants()...) }
