package main

// Session tokens of the hub (ARCHITECTURE.md, stage 4): one random 256-bit token per pilot launch,
// only its hash in hub/sessions.json (0600); the token itself is handed to the pilot in clear only in
// <private folder of the pilot>/hub-jeton (0600), outside the project.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// dureeJeton: a token unused for this long is expired (each use pushes it back).
const dureeJeton = 24 * time.Hour

// graceJeton: a pilot just started has not written its pilot.pid yet: not seen running before this
// delay is no reason to revoke its token.
const graceJeton = 3 * time.Minute

// outilsPilote: the hub's tools a project's main pilot may call (M61).
var outilsPilote = []string{"etat", "environnements", "demander_acces", "demander_deploiement", "signaler"}

// SessionHub: one token, by its hash.
type SessionHub struct {
	Hache   string   `json:"hache"`
	Projet  string   `json:"projet"`
	Mission string   `json:"mission,omitempty"`
	Agent   string   `json:"agent"`
	Outils  []string `json:"outils"`
	// Serveurs: the perimeter of a Servers agent's token (inventory ids only), empty for a pilot
	Serveurs []string `json:"serveurs,omitempty"`
	Cree     string   `json:"cree"`
	Vu       string   `json:"vu"`
	Expire   string   `json:"expire"`
}

// Sessions: the token store. Maintenant (nil: time.Now) is for the tests.
type Sessions struct {
	mu         sync.Mutex
	chemin     string
	liste      []SessionHub
	Maintenant func() time.Time
}

func cheminSessions() string { return filepath.Join(dossierAthanor(), "hub", "sessions.json") }

// ouvrirSessions reads the store (a missing file is empty; an unreadable one starts empty, every
// token it held is then invalid: the pilots get new ones at their next launch).
func ouvrirSessions(chemin string) (*Sessions, error) {
	s := &Sessions{chemin: chemin}
	b, err := os.ReadFile(chemin)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &s.liste)
	}
	if err != nil {
		s.liste = nil
	}
	return s, err
}

func (j *Sessions) maintenant() time.Time {
	if j.Maintenant != nil {
		return j.Maintenant()
	}
	return time.Now()
}

func (j *Sessions) enregistrer() error {
	if err := os.MkdirAll(filepath.Dir(j.chemin), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(j.liste, "", "  ")
	return ecrireAtomique(j.chemin, b, 0o600)
}

// Creer: a new token for a pilot launch of projet; the previous pilot session of that project is revoked.
func (j *Sessions) Creer(projet string, outils []string) (string, error) {
	return j.CreerSession(SessionHub{Projet: projet, Agent: "pilote", Outils: outils})
}

// CreerSession: a new token for x (project, agent, tools, perimeter); the previous session of the same
// project and agent is revoked.
func (j *Sessions) CreerSession(x SessionHub) (string, error) {
	jeton := aleatoire(32)
	j.mu.Lock()
	defer j.mu.Unlock()
	t := j.maintenant().UTC()
	j.retirer(func(y SessionHub) bool { return y.Projet == x.Projet && y.Agent == x.Agent })
	x.Hache, x.Outils, x.Serveurs = hacherJeton(jeton), append([]string{}, x.Outils...), slices.Clone(x.Serveurs)
	x.Cree, x.Vu, x.Expire = t.Format(time.RFC3339), t.Format(time.RFC3339), t.Add(dureeJeton).Format(time.RFC3339)
	j.liste = append(j.liste, x)
	if err := j.enregistrer(); err != nil {
		j.liste = j.liste[:len(j.liste)-1]
		return "", err
	}
	return jeton, nil
}

// retirer drops the sessions f selects and says how many (lock held).
func (j *Sessions) retirer(f func(SessionHub) bool) int {
	garde := j.liste[:0]
	n := 0
	for _, x := range j.liste {
		if f(x) {
			n++
		} else {
			garde = append(garde, x)
		}
	}
	j.liste = garde
	return n
}

// Verifier: the session of jeton, if valid; a use pushes its expiry back by dureeJeton. Every stored
// hash is compared in constant time, without stopping at the first match.
func (j *Sessions) Verifier(jeton string) (SessionHub, bool) {
	if jeton == "" {
		return SessionHub{}, false
	}
	h := hacherJeton(jeton)
	j.mu.Lock()
	defer j.mu.Unlock()
	trouve := -1
	for i, x := range j.liste {
		if egal(x.Hache, h) {
			trouve = i
		}
	}
	if trouve < 0 {
		return SessionHub{}, false
	}
	t := j.maintenant().UTC()
	x := &j.liste[trouve]
	if exp, err := time.Parse(time.RFC3339, x.Expire); err != nil || !t.Before(exp) {
		j.retirer(func(y SessionHub) bool { return y.Hache == h })
		_ = j.enregistrer()
		return SessionHub{}, false
	}
	x.Vu, x.Expire = t.Format(time.RFC3339), t.Add(dureeJeton).Format(time.RFC3339)
	_ = j.enregistrer() // shortcut: a write per call, fine for a pilot's few calls a minute
	return *x, true
}

// Revoquer drops the sessions f selects (expired ones too) and returns the projects revoked.
func (j *Sessions) Revoquer(f func(SessionHub) bool) ([]string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	t := j.maintenant().UTC()
	var projets []string
	j.retirer(func(x SessionHub) bool {
		exp, err := time.Parse(time.RFC3339, x.Expire)
		if f(x) || err != nil || !t.Before(exp) {
			projets = append(projets, x.Projet)
			return true
		}
		return false
	})
	if len(projets) == 0 {
		return nil, nil
	}
	return projets, j.enregistrer()
}

// Liste: a copy of the sessions (hashes included, never a token).
func (j *Sessions) Liste() []SessionHub {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]SessionHub{}, j.liste...)
}

// ----------------------------------------------------------------------------- server side

func (s *Serveur) chargerSessions() {
	var err error
	if s.jetons, err = ouvrirSessions(cheminSessions()); err != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Action: "sessions illisibles", Resultat: "tous les jetons révoqués"})
	}
}

func (s *Serveur) fichierJeton(p Projet) string { return filepath.Join(s.priveProjet(p), "hub-jeton") }

// creerJetonPilote: a token for this launch of p's pilot, handed over in its private folder.
func (s *Serveur) creerJetonPilote(p Projet) error {
	if s.jetons == nil {
		return errors.New("no session store")
	}
	jeton, err := s.jetons.Creer(p.Nom, outilsPilote)
	if err == nil {
		err = os.MkdirAll(s.priveProjet(p), 0o700)
	}
	if err == nil {
		err = ecrireAtomique(s.fichierJeton(p), []byte(jeton+"\n"), 0o600)
	}
	if err != nil {
		_, _ = s.jetons.Revoquer(func(x SessionHub) bool { return x.Projet == p.Nom })
		_ = os.Remove(s.fichierJeton(p))
		return err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "pilote", Projet: p.Nom, Agent: "pilote", Action: "jeton créé", Resultat: "ok"})
	return nil
}

// revoquerJetons: the sessions f selects (and the expired ones) are revoked, their files removed.
func (s *Serveur) revoquerJetons(raison string, f func(SessionHub) bool) {
	if s.jetons == nil {
		return
	}
	projets, err := s.jetons.Revoquer(f)
	s.annulerSessions() // C33: what a revoked session runs on a server stops now
	for _, nom := range projets {
		if p, ok := s.projetParNom(nom); ok {
			_ = os.Remove(s.fichierJeton(p))
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "pilote", Projet: nom, Agent: "pilote", Action: "jeton révoqué", Resultat: raison})
	}
	if err != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Action: "jeton révoqué", Resultat: "échec d'écriture"})
	}
}

func (s *Serveur) revoquerJetonProjet(p Projet, raison string) {
	s.revoquerJetons(raison, func(x SessionHub) bool { return x.Projet == p.Nom })
	_ = os.Remove(s.fichierJeton(p)) // a stale file of an earlier launch too
}

// verifierJeton: the session of a token, if valid and its pilot still runs (or has just started).
func (s *Serveur) verifierJeton(jeton string) (SessionHub, bool) {
	if s.jetons == nil {
		return SessionHub{}, false
	}
	x, ok := s.jetons.Verifier(jeton)
	if !ok {
		return SessionHub{}, false
	}
	if strings.HasPrefix(x.Projet, prefixeTache) { // a Servers task's session: valid while the task runs
		if !tacheActive(x.Hache) {
			s.revoquerJetons("tâche terminée", func(y SessionHub) bool { return y.Hache == x.Hache })
			return SessionHub{}, false
		}
		return x, true
	}
	if p, connu := s.projetParNom(x.Projet); !connu || !s.piloteVu(p, x) {
		s.revoquerJetons("pilote arrêté", func(y SessionHub) bool { return y.Hache == x.Hache })
		return SessionHub{}, false
	}
	return x, true
}

// piloteVu: the pilot runs, or its session is younger than the start-up grace.
func (s *Serveur) piloteVu(p Projet, x SessionHub) bool {
	if p.PiloteActif() {
		return true
	}
	c, err := time.Parse(time.RFC3339, x.Cree)
	return err == nil && s.jetons.maintenant().Sub(c) < graceJeton
}

// revoquerJetonsArretes: the sessions of pilots no longer running (or of projects gone), and the
// expired ones. Called when Athanor starts and when the project list is read.
func (s *Serveur) revoquerJetonsArretes() {
	if s.jetons == nil {
		return
	}
	s.mu.Lock() // a copy: s.mu is never taken under the store's lock
	projets := map[string]Projet{}
	for _, p := range s.projetsHub() {
		projets[p.Nom] = p
	}
	s.mu.Unlock()
	s.revoquerJetons("pilote arrêté", func(x SessionHub) bool {
		if strings.HasPrefix(x.Projet, prefixeTache) {
			return !tacheActive(x.Hache)
		}
		p, ok := projets[x.Projet]
		return !ok || !s.piloteVu(p, x)
	})
}

func (s *Serveur) projetParNom(nom string) (Projet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.projetsHub() {
		if p.Nom == nom {
			return p, true
		}
	}
	return Projet{}, false
}

// projetsHub (under s.mu): the projects, plus the temporary one of Test the hub's MCP while it runs.
func (s *Serveur) projetsHub() []Projet {
	if s.projetTestMCP.Nom == "" {
		return s.config.Projets
	}
	return append(slices.Clip(s.config.Projets), s.projetTestMCP)
}
