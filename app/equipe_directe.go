package main

// The direct link between the computers of a project (2.44): one of them, the master, opens a listener of
// its own and a Cloudflare tunnel of its own, separate from the remote access: it only answers the team's
// three calls below, never the application's panel (a team member cannot drive another computer). The
// others, the nodes, pair once with a code and then say every 10 seconds what they do and how much they
// use; each answer gives back every computer's state, so all of them see the whole team live.
//
//   POST /equipe/appairer   {code, id, nom}        → {attente, demande}  (6 digits, 10 min, 5 tries)
//   POST /equipe/appairer/statut {demande}         → {attente} | {jeton, equipe, cle, maitre} once the master
//                                                    accepted it on its screen | 403 refused
//   POST /equipe/battement  {presence, stats}      → {noeuds: [...]}              (Authorization: Bearer jeton)
//   GET  /equipe/noeuds                            → {noeuds: [...]}
//   GET  /equipe/atelier, POST /equipe/atelier/objets, /equipe/atelier/ecrire: the team's workshop
//                                                    (atelier_lien.go), when it goes through the link
//
// The master can start every computer's pilot, and stop them at the end of their cycle (orders in the
// answer to a beat): a node follows them unless its user said no. Nothing else of a node is ever driven.
//
// The tunnel's address changes when it starts again: the master publishes it, encrypted with the team's
// key (AES-GCM), in its presence on GitHub (pcs.go, Lien); a node that no longer reaches it reads it there.
// GitHub stays what the computers share their code through, and what they fall back on.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	battementDirect = 10 * time.Second // a node says it is there
	enLigneDirect   = 35 * time.Second // three beats missed: no longer live
	dureeCodeDirect = 10 * time.Minute
	essaisCodeMax   = 5
)

// StatsPC: what a computer uses on the project, for the team's table.
type StatsPC struct {
	Cycles   int      `json:"cycles"`
	Cout     float64  `json:"cout"`               // list price, every cycle so far
	Sortie   int64    `json:"sortie"`             // tokens Claude wrote
	CachePct int      `json:"cache_pct"`          // gains.go
	RTK      int64    `json:"rtk"`                // tokens RTK spared
	Ponytail bool     `json:"ponytail"`           //
	Eco      bool     `json:"eco"`                //
	Session  *float64 `json:"session,omitempty"`  // the account's 5-hour window, 0..1
	Semaine  *float64 `json:"semaine,omitempty"`  // its week, 0..1
	Reprise  int64    `json:"reprise,omitempty"`  // the quota exhausted: when it opens again (Unix s)
	Contexte int64    `json:"contexte,omitempty"` // the session's context now
}

// NoeudDirect: a computer of the team as the master knows it.
type NoeudDirect struct {
	Presence PresencePC `json:"presence"`
	Stats    StatsPC    `json:"stats"`
	Maitre   bool       `json:"maitre,omitempty"`
	Vu       int64      `json:"vu"`        // seconds since its last beat
	EnDirect bool       `json:"en_direct"` // beat within enLigneDirect
}

// noeudConnu: a node paired with the master (its token kept hashed).
type noeudConnu struct {
	Nom    string `json:"nom"`
	Jeton  string `json:"jeton"` // sha256, hex
	Ajoute string `json:"ajoute"`
}

// ordreDirect: what the master asks every node (its pilot started, or stopped after its cycle).
type ordreDirect struct {
	Seq    int64  `json:"seq"`
	Action string `json:"action"` // demarrer, arreter
	Quand  int64  `json:"quand"`
}

// dureeOrdreDirect: an order older than this is not followed (a computer that comes back an hour later).
const dureeOrdreDirect = 15 * time.Minute

// equipeDirecte: what this computer keeps of a project's direct link (in its pilot's private folder, which
// the pilot reads too: equipe-directe.json, 0600).
type equipeDirecte struct {
	Role    string                `json:"role"` // "maitre", "noeud", or "attente" (the right code given, the master's yes awaited)
	Equipe  string                `json:"equipe"`
	Cle     string                `json:"cle"`               // the team's key (base64, 32 bytes)
	Adresse string                `json:"adresse,omitempty"` // a node: the master's address
	Jeton   string                `json:"jeton,omitempty"`   // a node: its token
	Maitre  string                `json:"maitre,omitempty"`  // a node: the master's name
	Noeuds  map[string]noeudConnu `json:"noeuds,omitempty"`  // the master: who paired
	Depuis  int64                 `json:"depuis,omitempty"`  // waiting for the master's yes: since when; a node: since it joined
	// the team's workshop goes through the link (the master decides it; .athanor/ then stays out of git)
	AtelierLien bool `json:"atelier_lien,omitempty"`
	// a node: the master may start and stop its pilot (on when it joins), the last order seen
	AccepteOrdres bool  `json:"accepte_ordres,omitempty"`
	DernierOrdre  int64 `json:"dernier_ordre,omitempty"`
	// the master: its orders (the last ones)
	Ordres []ordreDirect `json:"ordres,omitempty"`
}

// VueDirecte: the direct link of a project, for this computer's pages.
type VueDirecte struct {
	Role     string           `json:"role,omitempty"`
	Etat     string           `json:"etat,omitempty"` // the master: demarrage, pret, erreur; a node: connecte, injoignable; attente; fini (refused or too late: Erreur says it)
	Adresse  string           `json:"adresse,omitempty"`
	Code     string           `json:"code,omitempty"`
	CodeFin  int64            `json:"code_fin,omitempty"`
	Maitre   string           `json:"maitre,omitempty"`
	Erreur   string           `json:"erreur,omitempty"`
	Noeuds   []NoeudDirect    `json:"noeuds,omitempty"`
	Appaires int              `json:"appaires,omitempty"`
	Demandes []demandeDirecte `json:"demandes,omitempty"` // the master: who gave the right code, its yes awaited
	// the team's workshop through the link: its last version (the master's, or what the node last heard)
	AtelierLien   bool         `json:"atelier_lien,omitempty"`
	Atelier       *VueAtelier  `json:"atelier,omitempty"`
	AccepteOrdres bool         `json:"accepte_ordres,omitempty"`
	Ordre         *ordreDirect `json:"ordre,omitempty"` // the last order given (the master) or followed (a node)
}

// VueAtelier: the team's workshop, as the page shows it.
type VueAtelier struct {
	Version  int64  `json:"version"`
	Fichiers int    `json:"fichiers"`
	Par      string `json:"par,omitempty"`
	Quand    string `json:"quand,omitempty"`
}

// ---------------------------------------------------------------------------- what is kept

var (
	directMu sync.Mutex
	hubs     = map[string]*hubEquipe{}    // project name → this computer as master
	clients  = map[string]*clientDirect{} // project name → this computer as node
)

// priveProjet: the project's pilot's private folder (the same the pilot computes, in WSL as well).
func (s *Serveur) priveProjet(p Projet) string {
	return priveDePilote(s.kitCourant().Racine, p.Dossier, estWindows)
}

func (s *Serveur) fichierDirect(p Projet) string {
	return filepath.Join(s.priveProjet(p), "equipe-directe.json")
}

// ancienFichierDirect: where 2.44 kept it (only the application read it then).
func (s *Serveur) ancienFichierDirect(p Projet) string {
	h := sha256.Sum256([]byte(p.Dossier))
	return filepath.Join(s.kitCourant().Journaux(), "equipe-directe", hex.EncodeToString(h[:8])+".json")
}

func (s *Serveur) magasinProjet(p Projet) magasinLocal {
	return magasinLocal{filepath.Join(s.priveProjet(p), "atelier-equipe")}
}

func (s *Serveur) lireDirect(p Projet) equipeDirecte {
	var e equipeDirecte
	b, err := os.ReadFile(s.fichierDirect(p))
	if os.IsNotExist(err) {
		if b, err = os.ReadFile(s.ancienFichierDirect(p)); err == nil {
			_ = os.MkdirAll(filepath.Dir(s.fichierDirect(p)), 0o700)
			if ecrireAtomique(s.fichierDirect(p), b, 0o600) == nil {
				_ = os.Remove(s.ancienFichierDirect(p))
			}
		}
	}
	if err == nil {
		_ = json.Unmarshal(b, &e)
	}
	return e
}

func (s *Serveur) ecrireDirect(p Projet, e equipeDirecte) error {
	f := s.fichierDirect(p)
	if e.Role == "" {
		err := os.Remove(f)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(e, "", "  ")
	return ecrireAtomique(f, b, 0o600)
}

func hacherJeton(j string) string {
	h := sha256.Sum256([]byte(j))
	return hex.EncodeToString(h[:])
}

// chiffrerDirect / dechiffrerDirect: the master's address in its presence on GitHub (AES-256-GCM).
func chiffrerDirect(cle, texte string) (string, error) {
	k, err := base64.StdEncoding.DecodeString(cle)
	if err != nil || len(k) != 32 {
		return "", errors.New("team key")
	}
	b, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(b)
	n := make([]byte, g.NonceSize())
	_, _ = rand.Read(n)
	return base64.RawURLEncoding.EncodeToString(g.Seal(n, n, []byte(texte), nil)), nil
}

func dechiffrerDirect(cle, chiffre string) (string, error) {
	k, err := base64.StdEncoding.DecodeString(cle)
	if err != nil || len(k) != 32 {
		return "", errors.New("team key")
	}
	c, err := base64.RawURLEncoding.DecodeString(chiffre)
	if err != nil {
		return "", err
	}
	b, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(b)
	if len(c) < g.NonceSize() {
		return "", errors.New("too short")
	}
	t, err := g.Open(nil, c[:g.NonceSize()], c[g.NonceSize():], nil)
	return string(t), err
}

// statsPC: this computer's use of the project, for the others.
func (s *Serveur) statsPC(p Projet) StatsPC {
	stats := p.LireStats()
	g := calculerGains(stats, ponytailDansEnv(s.kitCourant().Env()))
	st := StatsPC{Cycles: len(stats), CachePct: g.CachePct, RTK: g.RTKEconomise, Ponytail: g.Ponytail, Eco: p.EcoActif()}
	for _, l := range stats {
		st.Cout += l.CoutUSD
		st.Sortie += l.SortieTokens
	}
	st.Cout = float64(int(st.Cout*100+0.5)) / 100
	if q := quotaActuel([]Projet{p}); q != nil {
		for _, f := range q.Fenetres {
			switch f.Nom {
			case "five_hour":
				st.Session = f.Utilisation
			case "seven_day":
				st.Semaine = f.Utilisation
			}
			if q.Statut == "rejected" && f.Reinit > st.Reprise {
				st.Reprise = f.Reinit
			}
		}
	}
	_, st.Contexte, _ = s.journalDe(p).Lire(1 << 62)
	return st
}

// presenceDirecte: this computer's presence (the last one read with GitHub's, else what it knows).
func (s *Serveur) presenceDirecte(p Projet) PresencePC {
	pcsMu.Lock()
	c := pcsCache[p.Nom]
	pcsMu.Unlock()
	if c != nil && c.vue.Moi != nil {
		m := *c.vue.Moi
		m.Lien = ""
		return m
	}
	m := lireMachinePC()
	return PresencePC{ID: m.ID, Nom: m.Nom, Capacites: m.Capacites, Mots: m.Mots, Version: versionAthanor, Pilote: p.PiloteActif(), Maj: time.Now().UTC().Format(time.RFC3339)}
}

// ---------------------------------------------------------------------------- the master

type vuNoeud struct {
	presence PresencePC
	stats    StatsPC
	quand    time.Time
}

type hubEquipe struct {
	mu       sync.Mutex
	projet   Projet
	srv      *http.Server
	port     int
	tunnel   *exec.Cmd
	adresse  string
	etat     string
	erreur   string
	code     string
	codeFin  time.Time
	essais   int
	vus      map[string]vuNoeud
	arret    chan struct{}
	essaisIP map[string]int
	demandes map[string]*demandeDirecte // secret → a computer that gave the right code, waiting for the master
}

// demandeDirecte: a computer that gave the right code: the master accepts or refuses it on its screen.
type demandeDirecte struct {
	Ref    string `json:"ref"` // what the master's page names it by (never the secret)
	ID     string `json:"id"`
	Nom    string `json:"nom"`
	IP     string `json:"ip,omitempty"`
	Quand  int64  `json:"quand"`
	etat   string // attente, acceptee, refusee
	jeton  string // accepted: given once, then forgotten
	secret string
}

// demarrerMaitre: this computer becomes the project's master (its listener and its tunnel), the team's
// key and identity created the first time.
func (s *Serveur) demarrerMaitre(p Projet) error {
	e := s.lireDirect(p)
	if e.Role == "noeud" {
		return errors.New("This computer is a node of another master's team: leave it first.")
	}
	if e.Role == "" {
		k := make([]byte, 32)
		_, _ = rand.Read(k)
		e = equipeDirecte{Role: "maitre", Equipe: aleatoire(12), Cle: base64.StdEncoding.EncodeToString(k), Noeuds: map[string]noeudConnu{}}
		if err := s.ecrireDirect(p, e); err != nil {
			return err
		}
	}
	directMu.Lock()
	if h := hubs[p.Nom]; h != nil {
		directMu.Unlock()
		return nil
	}
	h := &hubEquipe{projet: p, etat: "demarrage", vus: map[string]vuNoeud{}, arret: make(chan struct{}), essaisIP: map[string]int{}, demandes: map[string]*demandeDirecte{}}
	hubs[p.Nom] = h
	directMu.Unlock()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.arreterMaitre(p.Nom)
		return err
	}
	h.port = ln.Addr().(*net.TCPAddr).Port
	// a tunnel a previous start of Athanor left running points nowhere now
	if b, err := os.ReadFile(s.pidTunnelDirect(p)); err == nil {
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); cloudflaredVivant(pid) {
			terminerProcessus(pid)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /equipe/appairer", func(w http.ResponseWriter, r *http.Request) { s.hubAppairer(h, w, r) })
	mux.HandleFunc("POST /equipe/appairer/statut", func(w http.ResponseWriter, r *http.Request) { s.hubStatutDemande(h, w, r) })
	mux.HandleFunc("POST /equipe/battement", func(w http.ResponseWriter, r *http.Request) { s.hubBattement(h, w, r) })
	mux.HandleFunc("GET /equipe/noeuds", func(w http.ResponseWriter, r *http.Request) { s.hubNoeuds(h, w, r) })
	mux.HandleFunc("GET /equipe/atelier", func(w http.ResponseWriter, r *http.Request) { s.hubAtelier(h, w, r) })
	mux.HandleFunc("POST /equipe/atelier/objets", func(w http.ResponseWriter, r *http.Request) { s.hubAtelierObjets(h, w, r) })
	mux.HandleFunc("POST /equipe/atelier/ecrire", func(w http.ResponseWriter, r *http.Request) { s.hubAtelierEcrire(h, w, r) })
	h.srv = &http.Server{Handler: limiterCorps(mux), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 3 * time.Minute, WriteTimeout: 3 * time.Minute}
	go func() { _ = h.srv.Serve(ln) }()
	go s.tunnelMaitre(h)
	return nil
}

// limiterCorps: the team's calls are small (the workshop's writes excepted); nothing else is answered.
func limiterCorps(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		max := int64(64 << 10)
		if r.URL.Path == "/equipe/atelier/ecrire" {
			max = tailleMaxEnvoiAtelier*4/3 + 1<<20 // base64
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// sansTunnelDirect: the tests reach the master's listener itself (never set by the application).
var sansTunnelDirect bool

// tunnelMaitre: the master's own quick tunnel (never the remote access's), started again when it ends.
func (s *Serveur) tunnelMaitre(h *hubEquipe) {
	if sansTunnelDirect {
		h.poser("pret", "", "http://127.0.0.1:"+strconv.Itoa(h.port))
		return
	}
	attente := 5 * time.Second
	for {
		select {
		case <-h.arret:
			return
		default:
		}
		if _, err := os.Stat(s.cheminCloudflared()); err != nil {
			h.poser("installation", "", "")
			if err := s.installerCloudflared(); err != nil {
				h.poser("erreur", err.Error(), "")
				if !h.attendre(time.Minute) {
					return
				}
				continue
			}
		}
		c, adresse, err := s.cloudflaredEquipe(h)
		if err != nil {
			h.poser("erreur", err.Error(), "")
			if !h.attendre(attente) {
				return
			}
			attente = min(2*attente, 5*time.Minute)
			continue
		}
		attente = 5 * time.Second
		h.mu.Lock()
		h.tunnel, h.adresse, h.etat, h.erreur = c, adresse, "pret", ""
		h.mu.Unlock()
		fini := make(chan struct{})
		go func() { _ = c.Wait(); close(fini) }()
		select {
		case <-h.arret:
			_ = c.Process.Kill()
			return
		case <-fini: // the computer slept, the network dropped: a new tunnel, a new address
			h.poser("demarrage", "", "")
		}
	}
}

func (h *hubEquipe) poser(etat, erreur, adresse string) {
	h.mu.Lock()
	h.etat, h.erreur, h.adresse = etat, erreur, adresse
	h.mu.Unlock()
}

func (h *hubEquipe) attendre(d time.Duration) bool {
	select {
	case <-h.arret:
		return false
	case <-time.After(d):
		return true
	}
}

// cloudflaredEquipe: a quick tunnel to the master's listener; cloudflared ends with it.
func (s *Serveur) cloudflaredEquipe(h *hubEquipe) (*exec.Cmd, string, error) {
	d := filepath.Join(s.kitCourant().Journaux(), "equipe-directe", "tunnel-"+strconv.Itoa(h.port))
	_ = os.MkdirAll(d, 0o700)
	journal := filepath.Join(d, "cloudflared.log")
	f, err := os.Create(journal)
	if err != nil {
		return nil, "", err
	}
	c := commande(s.cheminCloudflared(), "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+strconv.Itoa(h.port))
	c.Env = append(os.Environ(), "HOME="+d, "USERPROFILE="+d) // never a named tunnel's certificate
	c.Stdout, c.Stderr, c.Dir = f, f, d
	cacherFenetre(c)
	err = c.Start()
	f.Close()
	if err != nil {
		return nil, "", err
	}
	_ = os.WriteFile(s.pidTunnelDirect(h.projet), []byte(strconv.Itoa(c.Process.Pid)), 0o600)
	for i := 0; i < 120; i++ {
		time.Sleep(500 * time.Millisecond)
		b, _ := os.ReadFile(journal)
		if a := reAdresseTunnel.FindString(string(b)); a != "" && bytes.Contains(b, []byte("Registered tunnel connection")) {
			return c, a, nil
		}
		if c.ProcessState != nil {
			break
		}
	}
	_ = c.Process.Kill()
	b, _ := os.ReadFile(journal)
	return nil, "", fmt.Errorf("The Cloudflare tunnel did not start: %s", derniereLigneUtile(string(b)))
}

func (s *Serveur) pidTunnelDirect(p Projet) string {
	return strings.TrimSuffix(s.ancienFichierDirect(p), ".json") + ".tunnel.pid"
}

// arreterMaitre: the listener and the tunnel stop (the team's file stays, unless the team ends).
func (s *Serveur) arreterMaitre(projet string) {
	directMu.Lock()
	h := hubs[projet]
	delete(hubs, projet)
	directMu.Unlock()
	if h == nil {
		return
	}
	close(h.arret)
	h.mu.Lock()
	c := h.tunnel
	h.mu.Unlock()
	if c != nil && c.Process != nil {
		_ = c.Process.Kill()
	}
	if h.srv != nil {
		ctx, fin := context.WithTimeout(context.Background(), 2*time.Second)
		_ = h.srv.Shutdown(ctx)
		fin()
	}
}

// arreterDirects: Athanor ends: every master's tunnel with it.
func arreterDirects(s *Serveur) {
	directMu.Lock()
	var noms []string
	for n := range hubs {
		noms = append(noms, n)
	}
	directMu.Unlock()
	for _, n := range noms {
		s.arreterMaitre(n)
	}
}

func ipAppel(r *http.Request) string {
	if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" {
		return ip
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

func repondreDirect(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

var reIDDirect = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)

func (s *Serveur) hubAppairer(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	var c struct {
		Code, ID, Nom string
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil || !reIDDirect.MatchString(c.ID) {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	ip := ipAppel(r)
	h.mu.Lock()
	h.essaisIP[ip]++
	trop := h.essaisIP[ip] > 20
	bon := h.code != "" && time.Now().Before(h.codeFin) && h.essais < essaisCodeMax &&
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(c.Code)), []byte(h.code)) == 1
	if !bon && h.code != "" {
		h.essais++
	}
	if bon {
		h.code = "" // a code is used once
	}
	h.mu.Unlock()
	if trop || !bon {
		time.Sleep(time.Second)
		repondreDirect(w, 403, map[string]string{"erreur": "This code is wrong or expired: ask the master for a new one."})
		return
	}
	// the right code: the master still has to accept this computer on its screen
	d := &demandeDirecte{Ref: aleatoire(6), ID: c.ID, Nom: coupeOctets(strings.TrimSpace(c.Nom), 80), IP: ip, Quand: time.Now().Unix(), etat: "attente", secret: aleatoire(24)}
	h.mu.Lock()
	h.nettoyerDemandes()
	h.demandes[d.secret] = d
	h.mu.Unlock()
	go notifier(texteDemandeDirecte(s.langue(), d.Nom), h.projet.Nom)
	repondreDirect(w, 202, map[string]any{"attente": true, "demande": d.secret})
}

// texteDemandeDirecte: the master's notification.
func texteDemandeDirecte(langue, nom string) string {
	m := map[string]string{"fr": "%s veut rejoindre l'équipe", "es": "%s quiere unirse al equipo", "de": "%s möchte dem Team beitreten",
		"it": "%s vuole unirsi al team", "pt": "%s quer juntar-se à equipa"}
	f, ok := m[langue]
	if !ok {
		f = "%s wants to join the team"
	}
	return fmt.Sprintf(f, nom)
}

// nettoyerDemandes: the requests over 10 minutes old are forgotten (h.mu held).
func (h *hubEquipe) nettoyerDemandes() {
	for k, d := range h.demandes {
		if time.Since(time.Unix(d.Quand, 0)) > dureeCodeDirect {
			delete(h.demandes, k)
		}
	}
}

// hubStatutDemande: the waiting computer asks whether the master accepted it.
func (s *Serveur) hubStatutDemande(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	var c struct {
		Demande string `json:"demande"`
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil || c.Demande == "" {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	h.mu.Lock()
	h.nettoyerDemandes()
	d := h.demandes[c.Demande]
	var etat, jeton string
	if d != nil {
		etat, jeton = d.etat, d.jeton
		if etat != "attente" {
			delete(h.demandes, c.Demande) // said once
		}
	}
	h.mu.Unlock()
	switch etat {
	case "":
		repondreDirect(w, 404, map[string]string{"erreur": "The master did not answer in 10 minutes: ask it for a new invitation."})
	case "attente":
		repondreDirect(w, 200, map[string]any{"attente": true})
	case "refusee":
		repondreDirect(w, 403, map[string]string{"erreur": "The master refused this computer."})
	default:
		e := s.lireDirect(h.projet)
		repondreDirect(w, 200, map[string]string{"jeton": jeton, "equipe": e.Equipe, "cle": e.Cle, "maitre": s.presenceDirecte(h.projet).Nom})
	}
}

// deciderDemande: the master accepts (the computer joins, its token made) or refuses a request.
func (s *Serveur) deciderDemande(p Projet, ref string, accepter bool) error {
	directMu.Lock()
	h := hubs[p.Nom]
	directMu.Unlock()
	if h == nil {
		return errors.New("This computer is not the master of this project's team.")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nettoyerDemandes()
	for _, d := range h.demandes {
		if d.Ref != ref || d.etat != "attente" {
			continue
		}
		if !accepter {
			d.etat = "refusee"
			return nil
		}
		e := s.lireDirect(p)
		if e.Noeuds == nil {
			e.Noeuds = map[string]noeudConnu{}
		}
		jeton := aleatoire(32)
		e.Noeuds[d.ID] = noeudConnu{Nom: d.Nom, Jeton: hacherJeton(jeton), Ajoute: time.Now().UTC().Format(time.RFC3339)}
		if err := s.ecrireDirect(p, e); err != nil {
			return err
		}
		d.etat, d.jeton = "acceptee", jeton
		return nil
	}
	return errors.New("This request is no longer there (over 10 minutes old).")
}

// noeudAppel: the paired node a call comes from (its token), or "".
func (s *Serveur) noeudAppel(h *hubEquipe, r *http.Request) string {
	j, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || j == "" {
		return ""
	}
	hj := hacherJeton(strings.TrimSpace(j))
	for id, n := range s.lireDirect(h.projet).Noeuds {
		if subtle.ConstantTimeCompare([]byte(n.Jeton), []byte(hj)) == 1 {
			return id
		}
	}
	return ""
}

func (s *Serveur) hubBattement(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	id := s.noeudAppel(h, r)
	if id == "" {
		repondreDirect(w, 401, map[string]string{"erreur": "not paired"})
		return
	}
	var c struct {
		Presence     PresencePC `json:"presence"`
		Stats        StatsPC    `json:"stats"`
		DernierOrdre int64      `json:"dernier_ordre"`
	}
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		repondreDirect(w, 400, map[string]string{"erreur": "bad request"})
		return
	}
	c.Presence.ID, c.Presence.Moi, c.Presence.Lien = id, false, ""
	h.mu.Lock()
	h.vus[id] = vuNoeud{presence: c.Presence, stats: c.Stats, quand: time.Now()}
	h.mu.Unlock()
	e := s.lireDirect(h.projet)
	rep := map[string]any{"noeuds": s.noeudsMaitre(h), "atelier_lien": e.AtelierLien}
	if o := e.ordresApres(c.DernierOrdre, time.Now()); len(o) > 0 {
		rep["ordres"] = o
	}
	if e.AtelierLien {
		if v, err := s.magasinProjet(h.projet).dernier(); err == nil {
			rep["atelier"] = VueAtelier{Version: v.Version, Fichiers: len(v.Fichiers), Par: v.Par, Quand: v.Quand}
		}
	}
	repondreDirect(w, 200, rep)
}

func (s *Serveur) hubNoeuds(h *hubEquipe, w http.ResponseWriter, r *http.Request) {
	if s.noeudAppel(h, r) == "" {
		repondreDirect(w, 401, map[string]string{"erreur": "not paired"})
		return
	}
	repondreDirect(w, 200, map[string]any{"noeuds": s.noeudsMaitre(h)})
}

// noeudsMaitre: the master first, then every paired node (live or not), by name.
func (s *Serveur) noeudsMaitre(h *hubEquipe) []NoeudDirect {
	moi := s.presenceDirecte(h.projet)
	l := []NoeudDirect{{Presence: moi, Stats: s.statsPC(h.projet), Maitre: true, EnDirect: true}}
	connus := s.lireDirect(h.projet).Noeuds
	h.mu.Lock()
	var autres []NoeudDirect
	for id, n := range connus {
		v, vu := h.vus[id]
		nd := NoeudDirect{Presence: PresencePC{ID: id, Nom: n.Nom}, Vu: -1}
		if vu {
			nd.Presence, nd.Stats = v.presence, v.stats
			nd.Vu = int64(time.Since(v.quand).Seconds())
			nd.EnDirect = time.Since(v.quand) < enLigneDirect
		}
		autres = append(autres, nd)
	}
	h.mu.Unlock()
	sort.Slice(autres, func(i, j int) bool {
		return strings.ToLower(autres[i].Presence.Nom) < strings.ToLower(autres[j].Presence.Nom)
	})
	return append(l, autres...)
}

// codeMaitre: a new pairing code (the previous one no longer works).
func (h *hubEquipe) nouveauCode() (string, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.code, h.codeFin, h.essais = sixChiffres(), time.Now().Add(dureeCodeDirect), 0
	return h.code, h.codeFin
}

// lienMaitre: the master's address encrypted for its presence on GitHub; "" when it has none.
func (s *Serveur) lienMaitre(p Projet) string {
	directMu.Lock()
	h := hubs[p.Nom]
	directMu.Unlock()
	if h == nil {
		return ""
	}
	h.mu.Lock()
	a := h.adresse
	h.mu.Unlock()
	if a == "" {
		return ""
	}
	l, _ := chiffrerDirect(s.lireDirect(p).Cle, a)
	return l
}

// ---------------------------------------------------------------------------- a node

type clientDirect struct {
	noeuds  []NoeudDirect
	quand   time.Time
	echecs  int
	erreur  string
	atelier *VueAtelier
	ordre   *ordreDirect // the last one followed
}

var clientHTTP = &http.Client{Timeout: 15 * time.Second}

// appelMaitre: one call to the master; its JSON answer into v.
func appelMaitre(adresse, jeton, chemin string, corps, v any) (int, error) {
	var b io.Reader
	methode := "GET"
	if corps != nil {
		j, _ := json.Marshal(corps)
		b, methode = bytes.NewReader(j), "POST"
	}
	r, err := http.NewRequest(methode, strings.TrimRight(adresse, "/")+chemin, b)
	if err != nil {
		return 0, err
	}
	r.Header.Set("Content-Type", "application/json")
	if jeton != "" {
		r.Header.Set("Authorization", "Bearer "+jeton)
	}
	rep, err := clientHTTP.Do(r)
	if err != nil {
		return 0, err
	}
	defer rep.Body.Close()
	contenu, _ := io.ReadAll(io.LimitReader(rep.Body, 1<<20))
	if rep.StatusCode != 200 && rep.StatusCode != 202 {
		var e struct {
			Erreur string `json:"erreur"`
		}
		_ = json.Unmarshal(contenu, &e)
		return rep.StatusCode, errors.New(firstNonEmpty(e.Erreur, rep.Status))
	}
	return rep.StatusCode, json.Unmarshal(contenu, v)
}

var reInvitation = regexp.MustCompile(`(https://[A-Za-z0-9.-]+|http://127\.0\.0\.1:[0-9]+)\S*?[#\s]+([0-9]{6})\s*$`)

// rejoindreMaitre: this computer pairs with a master: "ADDRESS#CODE" (or the two, apart).
func (s *Serveur) rejoindreMaitre(p Projet, invitation string) error {
	m := reInvitation.FindStringSubmatch(strings.TrimSpace(invitation))
	if m == nil {
		return errors.New("An invitation reads like https://….trycloudflare.com#123456: the master's address and its 6 digits.")
	}
	if e := s.lireDirect(p); e.Role == "maitre" {
		return errors.New("This computer is the master of this project's team.")
	}
	moi := s.presenceDirecte(p)
	var rep struct {
		Attente bool   `json:"attente"`
		Demande string `json:"demande"`
	}
	if _, err := appelMaitre(m[1], "", "/equipe/appairer", map[string]string{"code": m[2], "id": moi.ID, "nom": moi.Nom}, &rep); err != nil {
		return err
	}
	if !rep.Attente || rep.Demande == "" {
		return errors.New("The master gave no answer.")
	}
	// waiting for the master to accept it (battementsDirects asks every few seconds)
	if err := s.ecrireDirect(p, equipeDirecte{Role: "attente", Adresse: m[1], Jeton: rep.Demande, Depuis: time.Now().Unix()}); err != nil {
		return err
	}
	directMu.Lock()
	delete(clients, p.Nom)
	directMu.Unlock()
	go s.suivreAttente(p)
	return nil
}

var attenteMu sync.Mutex

// suivreAttente: right after the code, asked every 2 seconds (the master's yes seen at once).
func (s *Serveur) suivreAttente(p Projet) {
	for i := 0; i < int((dureeCodeDirect+2*time.Minute)/(2*time.Second)); i++ {
		time.Sleep(2 * time.Second)
		if s.lireDirect(p).Role != "attente" {
			return
		}
		s.attenteAccord(p)
	}
}

// attenteAccord: a computer waiting for the master's yes: asked again; accepted, it is a node.
func (s *Serveur) attenteAccord(p Projet) {
	attenteMu.Lock() // the loop and suivreAttente: one at a time (the master says its answer once)
	defer attenteMu.Unlock()
	e := s.lireDirect(p)
	if e.Role != "attente" {
		return
	}
	var rep struct {
		Attente                    bool `json:"attente"`
		Jeton, Equipe, Cle, Maitre string
	}
	code, err := appelMaitre(e.Adresse, "", "/equipe/appairer/statut", map[string]string{"demande": e.Jeton}, &rep)
	fin := func(msg string) {
		_ = s.ecrireDirect(p, equipeDirecte{})
		directMu.Lock()
		clients[p.Nom] = &clientDirect{erreur: msg, echecs: 1}
		directMu.Unlock()
	}
	switch {
	case err == nil && rep.Attente:
		if time.Since(time.Unix(e.Depuis, 0)) > dureeCodeDirect+time.Minute {
			fin("The master did not answer in 10 minutes: ask it for a new invitation.")
		}
	case err == nil && rep.Jeton != "" && rep.Cle != "":
		_ = s.ecrireDirect(p, equipeDirecte{Role: "noeud", Equipe: rep.Equipe, Cle: rep.Cle, Adresse: e.Adresse, Jeton: rep.Jeton, Maitre: rep.Maitre, AccepteOrdres: true, Depuis: time.Now().Unix()})
		s.battementNoeud(p)
	case code == 403 || code == 404:
		fin(err.Error())
	case time.Since(time.Unix(e.Depuis, 0)) > dureeCodeDirect+time.Minute:
		fin("The master did not answer in 10 minutes: ask it for a new invitation.")
	}
}

// battementNoeud: a node says what it does; the answer is the whole team. A master that no longer answers:
// its new address read in its presence on GitHub.
func (s *Serveur) battementNoeud(p Projet) {
	e := s.lireDirect(p)
	if e.Role != "noeud" {
		return
	}
	directMu.Lock()
	c := clients[p.Nom]
	if c == nil {
		c = &clientDirect{}
		clients[p.Nom] = c
	}
	directMu.Unlock()
	corps := map[string]any{"presence": s.presenceDirecte(p), "stats": s.statsPC(p), "dernier_ordre": e.DernierOrdre}
	var rep struct {
		Noeuds      []NoeudDirect `json:"noeuds"`
		AtelierLien bool          `json:"atelier_lien"`
		Atelier     *VueAtelier   `json:"atelier"`
		Ordres      []ordreDirect `json:"ordres"`
	}
	code, err := appelMaitre(e.Adresse, e.Jeton, "/equipe/battement", corps, &rep)
	if err != nil && code != 401 {
		if a := s.adresseMaitreGitHub(p, e); a != "" && a != e.Adresse {
			e.Adresse = a
			_ = s.ecrireDirect(p, e)
			code, err = appelMaitre(e.Adresse, e.Jeton, "/equipe/battement", corps, &rep)
		}
	}
	if err == nil {
		s.suivreMaitre(p, rep.AtelierLien, rep.Ordres)
	}
	directMu.Lock()
	defer directMu.Unlock()
	if err != nil {
		c.echecs++
		c.erreur = err.Error()
		if code == 401 {
			c.erreur = "The master no longer knows this computer: ask it for a new invitation."
		}
		return
	}
	c.noeuds, c.quand, c.echecs, c.erreur, c.atelier = rep.Noeuds, time.Now(), 0, "", rep.Atelier
}

// reAdresseDirecte: what a master's address published on GitHub may be (a Cloudflare quick tunnel).
var reAdresseDirecte = regexp.MustCompile(`^https://[a-z0-9-]+\.trycloudflare\.com$`)

// adresseMaitreGitHub: the master's address in its presence on GitHub (encrypted with the team's key).
func (s *Serveur) adresseMaitreGitHub(p Projet, e equipeDirecte) string {
	pcsMu.Lock()
	c := pcsCache[p.Nom]
	pcsMu.Unlock()
	if c == nil {
		return ""
	}
	for _, x := range c.vue.PCs {
		if x.Lien == "" {
			continue
		}
		if a, err := dechiffrerDirect(e.Cle, x.Lien); err == nil && reAdresseDirecte.MatchString(a) {
			return a
		}
	}
	return ""
}

// battementsDirects: every 10 seconds, the nodes beat; at the start, the masters open again.
func (s *Serveur) battementsDirects() {
	time.Sleep(5 * time.Second)
	premier := true
	for {
		s.mu.Lock()
		projets := append([]Projet{}, s.config.Projets...)
		s.mu.Unlock()
		for _, p := range projets {
			if p.Archive || !p.Existe() {
				continue
			}
			switch s.lireDirect(p).Role {
			case "maitre":
				if premier {
					_ = s.demarrerMaitre(p)
				}
			case "noeud":
				s.battementNoeud(p)
			case "attente":
				s.attenteAccord(p)
			}
		}
		premier = false
		time.Sleep(battementDirect)
	}
}

// vueDirecte: the project's direct link for this computer's pages.
func (s *Serveur) vueDirecte(p Projet) *VueDirecte {
	e := s.lireDirect(p)
	if e.Role == "" {
		directMu.Lock()
		c := clients[p.Nom]
		directMu.Unlock()
		if c != nil && c.erreur != "" { // refused, or the master never answered: said until the next try
			return &VueDirecte{Etat: "fini", Erreur: c.erreur}
		}
		return nil
	}
	v := &VueDirecte{Role: e.Role, Appaires: len(e.Noeuds), AtelierLien: e.AtelierLien}
	switch e.Role {
	case "maitre":
		directMu.Lock()
		h := hubs[p.Nom]
		directMu.Unlock()
		if h == nil {
			v.Etat = "arrete"
			return v
		}
		h.mu.Lock()
		v.Etat, v.Erreur, v.Adresse = h.etat, h.erreur, h.adresse
		if h.code != "" && time.Now().Before(h.codeFin) {
			v.Code, v.CodeFin = h.code, h.codeFin.Unix()
		}
		h.nettoyerDemandes()
		for _, d := range h.demandes {
			if d.etat == "attente" {
				v.Demandes = append(v.Demandes, *d)
			}
		}
		h.mu.Unlock()
		sort.Slice(v.Demandes, func(i, j int) bool { return v.Demandes[i].Quand < v.Demandes[j].Quand })
		v.Noeuds = s.noeudsMaitre(h)
		if e.AtelierLien {
			if m, err := s.magasinProjet(p).dernier(); err == nil {
				v.Atelier = &VueAtelier{Version: m.Version, Fichiers: len(m.Fichiers), Par: m.Par, Quand: m.Quand}
			}
		}
		if n := len(e.Ordres); n > 0 {
			v.Ordre = &e.Ordres[n-1]
		}
	case "attente":
		v.Etat, v.Adresse = "attente", e.Adresse
	case "noeud":
		v.Maitre, v.AccepteOrdres = e.Maitre, e.AccepteOrdres
		directMu.Lock()
		c := clients[p.Nom]
		directMu.Unlock()
		v.Etat = "injoignable"
		if c != nil {
			v.Erreur, v.Atelier, v.Ordre = c.erreur, c.atelier, c.ordre
			if c.echecs == 0 && time.Since(c.quand) < enLigneDirect {
				v.Etat = "connecte"
			}
			v.Noeuds = c.noeuds
			if !c.quand.IsZero() && v.Etat != "connecte" {
				for i := range v.Noeuds { // the last table known, aged
					v.Noeuds[i].EnDirect = false
				}
			}
		}
	}
	return v
}

// apiDirect: GET the project's direct link; POST {action: maitre | inviter | rejoindre (invitation) |
// accepter (id) | refuser (id) | retirer (id) | quitter}.
func (s *Serveur) apiDirect(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		s.ecrireJSON(w, map[string]any{"direct": s.vueDirecte(p)})
		return
	}
	var c struct {
		Action, Invitation, ID string
		Valeur                 bool
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	var err error
	switch c.Action {
	case "maitre":
		err = s.demarrerMaitre(p)
	case "inviter":
		if err = s.demarrerMaitre(p); err == nil {
			directMu.Lock()
			h := hubs[p.Nom]
			directMu.Unlock()
			h.nouveauCode()
		}
	case "rejoindre":
		err = s.rejoindreMaitre(p, c.Invitation)
	case "atelier": // the master: the team's workshop through the link, or through GitHub again
		err = s.changerAtelierLien(p, c.Valeur)
	case "demarrer-equipe", "arreter-equipe": // the master: every pilot of the team, its own first
		err = s.ordreEquipe(p, strings.TrimSuffix(c.Action, "-equipe"))
	case "ordres": // a node: whether the master may start and stop its pilot
		e := s.lireDirect(p)
		if e.Role != "noeud" {
			err = errors.New("Only a node of a team follows the master's orders.")
			break
		}
		e.AccepteOrdres = c.Valeur
		err = s.ecrireDirect(p, e)
	case "accepter", "refuser": // the master's answer to a computer that gave the right code
		err = s.deciderDemande(p, c.ID, c.Action == "accepter")
	case "retirer": // the master forgets a node: its token no longer works
		e := s.lireDirect(p)
		if e.Role != "maitre" {
			err = errors.New("Only the master removes a computer.")
			break
		}
		delete(e.Noeuds, c.ID)
		err = s.ecrireDirect(p, e)
		directMu.Lock()
		if h := hubs[p.Nom]; h != nil {
			h.mu.Lock()
			delete(h.vus, c.ID)
			h.mu.Unlock()
		}
		directMu.Unlock()
	case "quitter": // a node leaves (or stops waiting); the master ends the team (every token with it)
		s.arreterMaitre(p.Nom)
		directMu.Lock()
		delete(clients, p.Nom)
		directMu.Unlock()
		err = s.ecrireDirect(p, equipeDirecte{})
		pcsMu.Lock()
		if c := pcsCache[p.Nom]; c != nil {
			c.quand = time.Time{} // the presence on GitHub: without the address at once
		}
		pcsMu.Unlock()
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"direct": s.vueDirecte(p)})
}
