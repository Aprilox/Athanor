package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionPreparationServeurs (M249): a mission with a linked server launched before its project's plan exists:
// its Servers lane waits for the plan (nothing launched, the prod relay not ready), stays waiting while paused,
// starts once when the plan appears (two readings at once included), with the standard list adapted to the plan,
// one journal entry and one audit line, and is never launched again by a restart or the relay.
func TestMissionPreparationServeurs(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	temoin := "TEMOIN-" + aleatoire(10)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a2", Nom: "dev-1", Hote: "203.0.113.6"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare"}, temoin+"-infra"); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienPilote, ancienneSession, ancienPas := demarrerPiloteMission, sessionTacheServeurs, pasSuiviMission
	t.Cleanup(func() {
		demarrerPiloteMission, sessionTacheServeurs, pasSuiviMission = ancienPilote, ancienneSession, ancienPas
	})
	pasSuiviMission = time.Hour // the follower reads once at the launch, then the test reads itself
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) { return "Pilot started", nil }
	var mu sync.Mutex
	var consignes []string
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		consignes = append(consignes, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	nbConsignes := func() int {
		for range 400 { // the tasks end (their goroutines must not outlive the test)
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
		return len(consignes)
	}

	p := Projet{Nom: "vitrine", Dossier: filepath.Join(t.TempDir(), "vitrine")}
	ecrireFichier(t, p.Demande(), "# vitrine\n\nUn site.\n", 0o644)
	s.config.Projets = append(s.config.Projets, p)
	c := CarteMission{Titre: "Site", Projet: ProjetCarte{Nom: p.Nom}, JalonsEstimes: 2, Dev: BriefCarte{Brief: "Un site vitrine"},
		Serveurs: BriefCarte{Brief: "Préparer le serveur"}, Envs: []EnvCarte{{Nom: "site", URL: "https://vitrine.example.fr", Serveurs: []string{"a2"}}},
		Acces: []AccesCarte{{Entree: "Jeton Cloudflare", Pour: p.Nom}}}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil {
		t.Fatal(err)
	}
	id := aleatoire(8)
	if err := enregistrerMission(Mission{ID: id, Phrase: "mets mon site en ligne", Etat: "proposee", Politique: "prod", Carte: mustJSON(calculerCarte(c, "prod", x)),
		Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
		t.Fatal(err)
	}
	voieSrv := func() VoieMission {
		t.Helper()
		m, _ := lireMission(id)
		i := slices.IndexFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" })
		if i < 0 {
			t.Fatalf("no Servers lane: %+v", m.Voies)
		}
		return m.Voies[i]
	}

	// 1. launched before the plan: the lane waits, nothing launched, the prod relay not ready
	if m, err := s.lancerMission(id, "proprietaire", "", ""); err != nil || m.Etat != "lancee" {
		t.Fatalf("launch: %v %+v", err, m)
	}
	s.observerMission(id)
	if v := voieSrv(); v.Etat != "attente_plan" || v.Ref != "" || v.Env != "vitrine/site" || nbConsignes() != 0 {
		t.Fatalf("before the plan: %+v, %d tasks", v, nbConsignes())
	}
	if prodPrete([]VoieMission{voieSrv()}, p.Nom, "site") {
		t.Fatal("prod ready while its preparation has not started")
	}
	if m, _ := lireMission(id); s.voiesEnDirect(m).Voies[1].Actuel != "attente_plan" {
		t.Fatalf("lane read now: %+v", s.voiesEnDirect(m).Voies[1])
	}

	// 2. paused when the plan appears: still waiting
	if _, err := s.mettreEnPause(id, "proprietaire", "test"); err != nil {
		t.Fatal(err)
	}
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site vitrine en Node avec une base SQLite\n- [ ] M2 : Le formulaire de contact\n", 0o644)
	s.observerMission(id)
	if v := voieSrv(); v.Etat != "attente_plan" || nbConsignes() != 0 {
		t.Fatalf("paused: %+v, %d tasks", v, nbConsignes())
	}

	// 3. resumed: two readings at once launch one preparation
	if _, err := s.reprendreMission(id, "proprietaire"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); s.observerMission(id) }()
	}
	wg.Wait()
	v := voieSrv()
	if v.Ref == "" || v.Etat == "attente_plan" || v.Env != "vitrine/site" || nbConsignes() != 1 {
		t.Fatalf("after the plan: %+v, %d tasks", v, nbConsignes())
	}
	brief := consignes[0]
	for _, attendu := range []string{"Préparer le serveur", "nginx", "dedicated system user", "firewall", "TLS certificate", "Do not deploy",
		"Le site vitrine en Node avec une base SQLite", "Le formulaire de contact", "Jeton Cloudflare", "https://vitrine.example.fr"} {
		if !strings.Contains(brief, attendu) {
			t.Errorf("brief without %q", attendu)
		}
	}
	if strings.Contains(brief, temoin) {
		t.Fatal("a vault value in the brief")
	}
	m, _ := lireMission(id)
	if n := len(slices.DeleteFunc(slices.Clone(m.Journal), func(e EntreeJournal) bool { return e.ID != "prep:vitrine/site" })); n != 1 {
		t.Fatalf("journal of the preparation: %d entries %+v", n, m.Journal)
	}
	audits := 0
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if l.Action == "préparation des serveurs" && l.Mission == id {
			audits++
			if !strings.HasPrefix(l.Resultat, v.Ref+" (vitrine/site)") || strings.Contains(l.Resultat, temoin) {
				t.Errorf("audit line: %+v", l)
			}
		}
	}
	if audits != 1 {
		t.Fatalf("%d audit lines for the preparation", audits)
	}

	// 4. a restart, then the relay: nothing launched again
	s.reprendreMissions()
	for range 3 {
		s.observerMission(id)
		s.relaisProd(id)
	}
	if w := voieSrv(); w.Ref != v.Ref || nbConsignes() != 1 {
		t.Fatalf("after a restart and the relay: %+v, %d tasks", w, nbConsignes())
	}
	if !prodPrete([]VoieMission{voieSrv()}, p.Nom, "site") {
		t.Fatal("the preparation is done, the prod relay still waits")
	}
}
