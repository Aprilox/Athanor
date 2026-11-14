package main

// Emergency stop (P-1): every hub session token revoked, the pending approvals refused, the vault closed
// (code and infra keys, the keystore no longer reopens it), the window and devices locked when a password
// exists, then every pilot stopped. Resuming is manual: unlock (or Resume without a password), then Start.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

func init() {
	enregistrerCommande("arret-urgence", "arret-urgence: the emergency stop of the running Athanor (tokens revoked, pilots stopped, approvals refused, vault locked)", lancerArretUrgence)
}

// ResultatUrgence: what the emergency stop did.
type ResultatUrgence struct {
	Jetons       int      `json:"jetons"`
	Approbations int      `json:"approbations"`
	Regles       int      `json:"regles"` // "never ask again" rules removed
	Pilotes      int      `json:"pilotes"`
	Terminaux    int      `json:"terminaux"`        // server terminals closed (with their files panel)
	Echecs       []string `json:"echecs,omitempty"` // projects whose pilot did not stop
	Verrouille   bool     `json:"verrouille"`       // window and devices locked (a password exists)
}

// arreterPiloteUrgence stops one pilot (replaced in the tests: no kit script there).
var arreterPiloteUrgence = func(s *Serveur, p Projet) error { _, err := s.arreterPiloteProjet(p); return err }

func (s *Serveur) arretUrgence(qui string) ResultatUrgence {
	var r ResultatUrgence
	if s.jetons != nil {
		r.Jetons = len(s.jetons.Liste())
	}
	s.revoquerJetons("arrêt d'urgence", func(SessionHub) bool { return true })
	s.pauserMissionsUrgence(qui)
	chatMu.Lock() // the home conversation's turn stops too (its cards can no longer be launched: carteActionChat)
	if en := chatsActifs[projetAccueil(s.kitCourant()).Dossier]; en != nil {
		en.arrete = true
		if en.cmd != nil && en.cmd.Process != nil {
			arreterArbre(en.cmd)
		}
	}
	chatMu.Unlock()
	if s.approbations != nil {
		verrouDecision.Lock() // an accept in progress ends first, then nothing pending is left
		l, err := s.approbations.RefuserTout(qui)
		verrouDecision.Unlock()
		r.Approbations = len(l)
		regles, errR := s.approbations.RetirerRegles(func(RegleAccord) bool { return true })
		r.Regles = len(regles)
		for _, x := range regles {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: x.Projet, Env: x.Env, Action: "règle retirée: " + x.Action, Resultat: "arrêt d'urgence"})
		}
		if errR != nil {
			fmt.Fprintln(os.Stderr, "emergency stop: rules:", errR)
		}
		for _, x := range l {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: x.Env, Agent: x.Agent,
				Action: "approbation refuser", Resultat: x.Action + " " + x.ID + " → refusee (arrêt d'urgence)"})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "emergency stop: approvals:", err)
		}
	}
	r.Terminaux = fermerTerminaux(func(*sessionTerminal) bool { return true })
	if s.coffre != nil {
		s.coffre.Fermer()
	}
	s.mu.Lock()
	if s.config.MotDePasseHash != "" {
		d := s.dist()
		d.mu.Lock()
		d.deverrouilles = map[string]bool{}
		d.mu.Unlock()
		s.session, r.Verrouille = "", true
	}
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	vides := s.viderAccesUrgence(projets)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range projets { // in parallel: each stop may take up to 2 min
		if !p.PiloteActif() {
			continue
		}
		marqueAuto(p, false)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := arreterPiloteUrgence(s, p)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				r.Echecs = append(r.Echecs, p.Nom)
			} else {
				r.Pilotes++
			}
		}()
	}
	wg.Wait()
	res := fmt.Sprintf("%d jeton(s) révoqué(s), %d approbation(s) refusée(s), %d règle(s) retirée(s), %d pilote(s) arrêté(s), %d terminal(aux) fermé(s), coffre verrouillé, %d secrets.env vidé(s)", r.Jetons, r.Approbations, r.Regles, r.Pilotes, r.Terminaux, vides)
	if len(r.Echecs) > 0 {
		res += fmt.Sprintf(", %d pilote(s) non arrêté(s)", len(r.Echecs))
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Action: "arrêt d'urgence", Resultat: res})
	return r
}

// reprendreApresUrgence (Resume, no password): the keystore opens the code key again, the projects'
// secrets.env are written again. With a password, unlocking (Deverrouiller) does it.
func (s *Serveur) reprendreApresUrgence(qui string) {
	if s.coffre == nil || s.coffre.Urgence() == "" {
		return
	}
	s.coffre.Rouvrir()
	_ = s.coffre.OuvrirCode()
	s.injecterTous(nil)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Action: "reprise après arrêt d'urgence", Resultat: "coffre rouvert"})
}

const texteUrgence = "Emergency stop: {0} pilot(s) stopped, {1} session(s) revoked, {2} approval(s) refused, {3} “never ask again” rule(s) removed, vault locked. To resume: unlock Athanor, then Start each project."

func argsUrgence(r ResultatUrgence) []string {
	return []string{strconv.Itoa(r.Pilotes), strconv.Itoa(r.Jetons), strconv.Itoa(r.Approbations), strconv.Itoa(r.Regles)}
}

// apiUrgence: POST /api/hub/urgence {} the emergency stop (window or paired device, confirmed by the
// interface); {reprendre: true} reopens the vault after it when no password is set.
func (s *Serveur) apiUrgence(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Reprendre bool `json:"reprendre"`
	}
	_ = lireCorps(r, &c) // an empty or unreadable body is a stop: the safe side
	if c.Reprendre {
		s.mu.Lock()
		mdp := s.config.MotDePasseHash != ""
		s.mu.Unlock()
		if mdp {
			s.erreur(w, http.StatusConflict, "Unlock Athanor with its password to resume")
			return
		}
		if code, err := s.preuveAbsence(); err != nil { // C27: an agent still running must not undo the stop
			s.erreur(w, code, err.Error())
			return
		}
		s.reprendreApresUrgence(deQui(r))
		s.ecrireJSON(w, map[string]any{"ok": true, "message_key": "Vault reopened: start the projects again with Start."})
		return
	}
	res := s.arretUrgence(deQui(r))
	s.ecrireJSON(w, map[string]any{"resultat": res, "message_key": texteUrgence, "args": argsUrgence(res)})
}

// alerteUrgence: the bell says the emergency stop is on until the vault is reopened; Resume without a
// password (with one, the window is locked and unlocking reopens it).
func (s *Serveur) alerteUrgence() []AlerteHub {
	if s.coffre == nil || s.coffre.Urgence() == "" {
		return nil
	}
	s.mu.Lock()
	mdp := s.config.MotDePasseHash != ""
	s.mu.Unlock()
	a := AlerteHub{Empreinte: "urgence", Module: "hub", Element: "urgence", Titre: "Emergency stop",
		Raison: "Emergency stop on {0}: pilots stopped, sessions revoked, vault locked. To resume: unlock Athanor, then Start each project.",
		Args:   []any{s.coffre.Urgence()}, Actions: []ActionAlerte{}}
	if !mdp {
		a.Actions = append(a.Actions, ActionAlerte{ID: "reprendre", Libelle: "Resume", Route: "hub/urgence", Corps: map[string]any{"reprendre": true}})
	}
	return []AlerteHub{a}
}

// lancerArretUrgence: athanor arret-urgence, through the running instance (instance.json's secret).
// shortcut: with Athanor closed nothing is revoked or locked (no token is valid then, the vault is closed)
// and the pilots are not stopped here; upgrade if a closed-Athanor stop is needed.
func lancerArretUrgence([]string) int {
	b, err := os.ReadFile(fichierInstance())
	var i infoInstance
	if err != nil || json.Unmarshal(b, &i) != nil || i.Port <= 0 || i.Jeton == "" {
		fmt.Fprintln(os.Stderr, "Athanor is not open: no hub session token is valid and the vault is closed. Stop the pilots with Stop in each project.")
		return 1
	}
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(i.Port)+"/api/instance/urgence", nil)
	req.Header.Set("X-Athanor-Instance", i.Jeton)
	rep, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor does not answer:", err)
		return 1
	}
	defer rep.Body.Close()
	var r ResultatUrgence
	if rep.StatusCode != http.StatusOK || json.NewDecoder(rep.Body).Decode(&r) != nil {
		fmt.Fprintln(os.Stderr, "Athanor refused the emergency stop:", rep.Status)
		return 1
	}
	fmt.Printf("Emergency stop: %d pilot(s) stopped, %d session(s) revoked, %d approval(s) refused, %d \"never ask again\" rule(s) removed, vault locked.\n", r.Pilotes, r.Jetons, r.Approbations, r.Regles)
	if len(r.Echecs) > 0 {
		fmt.Printf("Not stopped: %v (stop them with Stop in their project).\n", r.Echecs)
	}
	fmt.Println("To resume: unlock Athanor, then Start each project.")
	return 0
}
