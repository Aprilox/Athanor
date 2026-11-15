package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mdpPreuveTest = "preuve-test-123"

// avecMdpPreuve: a password and an open window session, for the routes behind hub_preuve.go's proofs.
func avecMdpPreuve(s *Serveur) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MotDePasseHash, s.config.Sel, s.session = hacherMotDePasse(mdpPreuveTest), "", "session-test"
}

var cookieSessionTest = &http.Cookie{Name: "athanor_session", Value: "session-test"}

// TestPreuveAbsence (C25): the first password is refused while an agent Athanor started runs, then created
// and logged; changing it later only needs the old one.
func TestPreuveAbsence(t *testing.T) {
	s := serveurCoffreTest(t)
	p := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	pid := filepath.Join(p.Atelier(), "pilot.pid")
	_ = os.WriteFile(pid, []byte("1"), 0o644)
	s.mu.Lock()
	s.config.Projets = []Projet{p}
	s.mu.Unlock()
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"nouveau":"alpha-1-phrase"}`, nil); w.Code != http.StatusConflict || s.config.MotDePasseHash != "" {
		t.Fatalf("first password while a pilot runs: %d %s", w.Code, w.Body)
	}
	chatMu.Lock()
	chatsActifs["x"] = &chatEnCours{}
	chatMu.Unlock()
	_ = os.Remove(pid)
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"nouveau":"alpha-1-phrase"}`, nil); w.Code != http.StatusConflict {
		t.Errorf("first password while a chat runs: %d", w.Code)
	}
	chatMu.Lock()
	delete(chatsActifs, "x")
	chatMu.Unlock()
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"nouveau":"alpha-1-phrase"}`, nil); w.Code != 200 {
		t.Fatalf("first password: %d %s", w.Code, w.Body)
	}
	_ = os.WriteFile(pid, []byte("1"), 0o644)
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"ancien":"alpha-1-phrase","nouveau":"beta-2-phrase-x"}`, nil); w.Code != 200 {
		t.Errorf("change with the old one while a pilot runs: %d %s", w.Code, w.Body)
	}
	for _, attendu := range []string{"mot de passe créé", "mot de passe changé"} {
		if !strings.Contains(auditTexte(t), attendu) {
			t.Errorf("%q not in the audit log", attendu)
		}
	}
}

// TestPlanTacheFenetre (note 19h03 §3): the plan of an owner's Servers task is decided in the window unlocked with
// the password, without retyping it; locked, without the session or for a mission's plan, the proof is still asked.
func TestPlanTacheFenetre(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	plan := func(projet string) string {
		x, err := s.demanderApprobation(Approbation{Projet: projet, Env: "dev", Agent: agentServeurs, Action: actionPlanServeur, Raison: "plan", Details: map[string]string{"serveur": "ab12"}})
		if err != nil {
			t.Fatal(err)
		}
		return x.ID
	}
	decider := func(id string, cookie *http.Cookie) int {
		return appelCoffre(t, s.apiApprobationDecider, "", `{"id":"`+id+`","decision":"accepter"}`, cookie).Code
	}
	tache, mission := plan(prefixeTache+"t1"), plan("demo")
	for nom, c := range map[string]*http.Cookie{"no session": nil, "another session": {Name: "athanor_session", Value: "autre"}} {
		if code := decider(tache, c); code != http.StatusUnauthorized {
			t.Errorf("task plan, %s, no password: %d", nom, code)
		}
	}
	if code := decider(mission, cookieSessionTest); code != http.StatusUnauthorized {
		t.Errorf("mission plan with the window's session only: %d", code)
	}
	if code := decider(tache, cookieSessionTest); code != 200 || s.approbations.PourAgent(tache) != "acceptee" {
		t.Errorf("task plan in the unlocked window: %d %s", code, s.approbations.PourAgent(tache))
	}
	autre := plan(prefixeTache + "t2")
	s.mu.Lock()
	s.session = "" // the auto-lock closed the window's session
	s.mu.Unlock()
	if code := decider(autre, cookieSessionTest); code != http.StatusUnauthorized {
		t.Errorf("task plan once the window is locked: %d", code)
	}
}
