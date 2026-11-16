package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionPremierDeploiement (M275b): the card's linked environment without a deployment method gets its first
// deployment from the mission's Servers lane: not while its preparation waits, not before a milestone is
// delivered, then one task (its brief names the environment and the project's HEAD), recorded as a lane and in the
// journal, never twice; meanwhile the milestone relay notes no failure (no bell for what the mission does).
func TestMissionPremierDeploiement(t *testing.T) {
	s := serveurCoffreTest(t)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	p := Projet{Nom: "vitrine", Dossier: filepath.Join(t.TempDir(), "vitrine")}
	ecrireFichier(t, p.Demande(), "# Vitrine\n", 0o644)
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [x] M1 : Accueil\n", 0o644)
	ecrireFichier(t, filepath.Join(p.Dossier, "site", "index.html"), "<h1>Vitrine</h1>\n", 0o644)
	for _, a := range [][]string{{"init", "-q"}, {"add", "site"}, {"commit", "-q", "-m", "M1"}} {
		if out, err := exec.Command("git", append([]string{"-C", p.Dossier, "-c", "user.name=t", "-c", "user.email=t@t.test"}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	head, _ := gitSortie(p.Dossier, "rev-parse", "HEAD")
	s.config.Projets = append(s.config.Projets, p)
	if err := s.acces.DefinirEnvironnement(p.Nom, "site", Environnement{URL: "https://vitrine.example.org", Serveurs: []string{"a1"}, Protection: "aucune", DeployerAuJalon: true}); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() {
		_ = os.RemoveAll(dossierMissions())
		_ = os.Remove(fichierJalonsFaits())
		_ = os.Remove(fichierRelais())
	})

	var mu sync.Mutex
	var consignes []string
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		consignes = append(consignes, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"sess-ops"}` + "\n" + `{"type":"result","is_error":false,"result":"ok"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}

	c := CarteMission{Titre: "Vitrine en ligne", Projet: ProjetCarte{Nom: p.Nom}, Dev: BriefCarte{Brief: "Le site"}, JalonsEstimes: 1,
		Envs: []EnvCarte{{Nom: "site", URL: "https://vitrine.example.org", Protection: "aucune", Serveurs: []string{"a1"}}}, Serveurs: BriefCarte{Brief: "Caddy"}}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(calculerCarte(c, "prod", x))
	m := Mission{ID: "premier1", Phrase: "en ligne", Etat: "lancee", Politique: "prod", Carte: b,
		Voies:      []VoieMission{{Module: "dev", Ref: p.Nom, Etat: "lancee"}, {Module: "servers", Etat: "attente_plan", Env: p.Nom + "/site"}},
		Historique: []EvenementMission{evenementMission("lancee", "")}, Cree: time.Now().UTC().Format(formatCree)}
	if err := enregistrerMission(m); err != nil {
		t.Fatal(err)
	}
	// the milestone relay does not fail for want of a method meanwhile: the bell would ring for what the mission does
	s.noterJalonRelais(p, "M1 : Accueil", time.Now())
	if r, ok := lireRelais()[p.Nom]; ok {
		t.Fatalf("milestone relay noted without a method: %+v", r)
	}
	premiers := func() []VoieMission {
		t.Helper()
		lu, _ := lireMission(m.ID)
		return slices.DeleteFunc(slices.Clone(lu.Voies), func(v VoieMission) bool { return v.Module != "servers" || v.Ref == "" })
	}
	time.Sleep(1100 * time.Millisecond) // a milestone after the launch (dated to the second)
	if err := noterJalonFait(fichierJalonsFaits(), p.Dossier, "M1 : Accueil", time.Now()); err != nil {
		t.Fatal(err)
	}
	if s.premierDeploiementMission(m); len(premiers()) != 0 {
		t.Fatal("a first deployment before the preparation")
	}
	_, _ = modifierMission(m.ID, func(x *Mission) error { x.Voies[1].Etat = "echec"; return nil }) // the preparation over
	if err := os.Remove(fichierJalonsFaits()); err != nil {
		t.Fatal(err)
	}
	if lu, _ := lireMission(m.ID); len(s.premierDeploiementMission(lu).Voies) != 2 {
		t.Fatal("a first deployment before any milestone")
	}
	_ = noterJalonFait(fichierJalonsFaits(), p.Dossier, "M1 : Accueil", time.Now())
	lu, _ := lireMission(m.ID)
	s.premierDeploiementMission(lu)
	s.premierDeploiementMission(lu) // the next reading: never twice
	v := premiers()
	if len(v) != 1 || v[0].Env != p.Nom+"/site" {
		t.Fatalf("first deployment lanes: %+v", v)
	}
	for range 200 {
		if mu.Lock(); len(consignes) > 0 {
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(25 * time.Millisecond)
	}
	for range 400 { // the task's goroutine ends before the test
		tachesEnCours.Lock()
		n := len(tachesEnCours.m)
		tachesEnCours.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(consignes) != 1 || !strings.Contains(consignes[0], prefixePremierDeploiement+" of project vitrine to environment site") || !strings.Contains(consignes[0], head) {
		t.Fatalf("the task's brief: %q", consignes)
	}
	if lu, _ = lireMission(m.ID); !slices.ContainsFunc(lu.Journal, func(e EntreeJournal) bool { return e.ID == "deploi1:vitrine/site" && e.Niveau == "info" }) {
		t.Errorf("journal: %+v", lu.Journal)
	}
}
