package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPreparationProjet (M283): a project with a server and a moment has its servers prepared by one Servers task
// as soon as its plan holds a milestone: nothing before the plan, one task and not two (a second look, which is
// all a restart is: the record is read from its file; a milestone's relay keeps it), a new one when the
// deployment's plan or its servers change, nothing without a moment nor while a mission covers the project; a
// project already deployed is only learnt; the brief names the API, never a vault value, and asks the accord of a
// production unless the project's mode is ignorer.
func TestPreparationProjet(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	temoin := "TEMOIN-" + aleatoire(10)
	api, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	var mu sync.Mutex
	var consignes []string
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error { // the fake chef ops: no plan
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		consignes = append(consignes, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	taches := func() int {
		t.Helper()
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

	r1 := serveurRelaisTest(t)
	env := Environnement{URL: "https://m283.example.fr", Serveurs: []string{r1}, Protection: "aucune", Politique: "accord", Moment: "livraison",
		Plan: "Un conteneur Node derrière nginx", APIs: []string{api.ID}}
	p, _ := projetRelaisTest(t, s, "m283", env)
	ecrireFichier(t, p.Demande(), "# m283\n\nLa boutique en ligne de l'atelier.\n", 0o644)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	poser := func() {
		t.Helper()
		if err := s.acces.definirAncien(p.Nom, "site", env); err != nil {
			t.Fatal(err)
		}
	}

	s.preparerProjet(p)
	if n := taches(); n != 0 || lireRelais()[p.Nom].Preparation != nil {
		t.Fatalf("before the plan: %d tasks, %+v", n, lireRelais()[p.Nom])
	}
	planRelaisTest(t, p, "- [ ] M1 : Le site vitrine en Node\n- [ ] M2 : Le formulaire de contact\n")
	s.preparerProjet(p)
	prep := lireRelais()[p.Nom].Preparation
	if n := taches(); n != 1 || prep == nil || prep.Tache == "" || prep.Empreinte == "" || prep.Erreur != "" {
		t.Fatalf("the plan known: %d tasks, %+v", n, prep)
	}
	for _, attendu := range []string{`Project "m283"`, "https://m283.example.fr", "nginx", "Do not deploy", "the owner's accord", "Jeton Cloudflare",
		"Un conteneur Node derrière nginx", "La boutique en ligne", "Le site vitrine en Node", "Le formulaire de contact"} {
		if !strings.Contains(consignes[0], attendu) {
			t.Errorf("brief without %q", attendu)
		}
	}
	if strings.Contains(consignes[0], temoin) {
		t.Fatal("a vault value in the brief")
	}
	audits := slices.DeleteFunc(lignesAudit(t, dossierJournalAudit()), func(l LigneAudit) bool { return l.Action != "préparation du serveur" || l.Projet != p.Nom })
	if len(audits) != 1 || !strings.HasPrefix(audits[0].Resultat, prep.Tache) || strings.Contains(audits[0].Resultat, temoin) {
		t.Fatalf("audit: %+v", audits)
	}

	s.preparerProjet(p) // a second look, a restart: nothing kept in memory
	s.attendreRelais(p, lireRelais()[p.Nom], "M1 : Le site vitrine en Node", "relais après jalon", time.Now())
	s.preparerProjet(p)
	if n, apres := taches(), lireRelais()[p.Nom].Preparation; n != 1 || apres == nil || *apres != *prep {
		t.Fatalf("a second look, a relay: %d tasks, %+v", n, apres)
	}

	env.Plan = "Deux conteneurs et une base PostgreSQL"
	poser()
	s.preparerProjet(p)
	s.preparerProjet(p)
	plan := lireRelais()[p.Nom].Preparation
	if n := taches(); n != 2 || plan.Tache == prep.Tache || plan.Empreinte == prep.Empreinte || !strings.Contains(consignes[1], "PostgreSQL") {
		t.Fatalf("the plan changed: %d tasks, %+v", n, plan)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "r2", Nom: "relais-2", Hote: "127.0.0.1", Port: 1, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	env.Serveurs = []string{"r2", r1}
	poser()
	s.preparerProjet(p)
	env.Serveurs = []string{r1, "r2"} // the same servers in another order
	poser()
	s.preparerProjet(p)
	if n := taches(); n != 3 {
		t.Fatalf("the servers changed: %d tasks", n)
	}

	env.Moment, env.Plan = "", "Autre chose"
	poser()
	s.preparerProjet(p)
	if n := taches(); n != 3 {
		t.Fatalf("without a moment: %d tasks", n)
	}
	env.Moment = "jalon"
	env.DeployerAuJalon = true
	poser()
	if err := enregistrerMission(Mission{ID: "m283mission", Etat: "lancee", Voies: []VoieMission{{Module: "dev", Ref: p.Nom, Etat: "lancee"}}}); err != nil {
		t.Fatal(err)
	}
	s.preparerProjet(p)
	if n := taches(); n != 3 {
		t.Fatalf("a mission covers the project: %d tasks", n)
	}
	_ = os.RemoveAll(dossierMissions())
	s.preparerProjet(p)
	if n := taches(); n != 4 {
		t.Fatalf("the mission gone: %d tasks", n)
	}

	// a project already deployed when first seen: its servers serve it, the fingerprint is only learnt
	oublierRelais(p.Nom)
	if err := s.acces.NoterDeploiement(p.Nom, "site", ConfigDeploiement{Methode: "statique", Source: "git"}); err != nil {
		t.Fatal(err)
	}
	s.preparerProjet(p)
	if n, v := taches(), lireRelais()[p.Nom].Preparation; n != 4 || v == nil || v.Tache != "" || v.Empreinte == "" {
		t.Fatalf("already deployed: %d tasks, %+v", n, v)
	}
	env.Plan = "Encore un autre"
	poser()
	s.preparerProjet(p)
	if n := taches(); n != 5 {
		t.Fatalf("already deployed, the plan changed: %d tasks", n)
	}

	// native Windows mode: no task without the owner; said once, tried again once the mode is gone
	modeNatif.Store(true)
	env.Plan = "Le dernier"
	poser()
	s.preparerProjet(p)
	s.preparerProjet(p)
	if n, v := taches(), lireRelais()[p.Nom].Preparation; n != 5 || v == nil || v.Erreur == "" || v.Empreinte != "" {
		t.Fatalf("native mode: %d tasks, %+v", n, v)
	}
	modeNatif.Store(false)
	s.preparerProjet(p)
	if n, v := taches(), lireRelais()[p.Nom].Preparation; n != 6 || v.Erreur != "" || v.Tache == "" {
		t.Fatalf("after the native mode: %d tasks, %+v", n, v)
	}

	if _, err := definirReglageProjet(p.Nom, "permission", "ignorer"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { deplacerReglagesProjet(p.Nom, "") })
	if b := s.briefPreparationProjet(p, env); strings.Contains(b, "the owner's accord") || !strings.Contains(b, "Do not deploy") {
		t.Errorf("mode ignorer, brief: %s", b)
	}
}

// TestFournisseurParAPI (M283): the provider of a deployment is the one whose token is among its API (the one
// holding its host's zone first), else the older Fournisseur, else none; the Cloudflare tools read it that way.
func TestFournisseurParAPI(t *testing.T) {
	l := []Fournisseur{
		{ID: "ancien", Cle: "c0", Zones: []ZoneFournisseur{{Nom: "example.fr"}}},
		{ID: "autre", Cle: "c1", Zones: []ZoneFournisseur{{Nom: "ailleurs.test"}}},
		{ID: "zone", Cle: "c2", Zones: []ZoneFournisseur{{Nom: "example.fr"}}},
	}
	for _, c := range []struct {
		nom     string
		e       Environnement
		attendu string
	}{
		{"by its API", Environnement{URL: "https://x.test", APIs: []string{"zz", "c1"}}, "autre"},
		{"the API holding the zone first", Environnement{URL: "https://site.example.fr", APIs: []string{"c1", "c2"}}, "zone"},
		{"an API before the older provider", Environnement{URL: "https://site.example.fr", APIs: []string{"c1"}, Fournisseur: "ancien"}, "autre"},
		{"the older provider", Environnement{URL: "https://site.example.fr", APIs: []string{"zz"}, Fournisseur: "ancien"}, "ancien"},
		{"none", Environnement{URL: "https://site.example.fr", APIs: []string{"zz"}}, ""},
		{"nothing chosen", Environnement{URL: "https://site.example.fr"}, ""},
	} {
		if f, ok := fournisseurDeploiement(c.e, l); f.ID != c.attendu || ok != (c.attendu != "") {
			t.Errorf("%s: %q %v", c.nom, f.ID, ok)
		}
	}

	s, _, _ := serveursAjoutTest(t)
	if err := modifierFournisseurs(func([]Fournisseur) ([]Fournisseur, error) { return l, nil }); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m283f", Dossier: t.TempDir()})
	s.mu.Unlock()
	env := Environnement{URL: "https://site.example.fr", Serveurs: []string{serveurRelaisTest(t)}, Protection: "aucune", Politique: "accord",
		APIs: []string{"c2"}, Fournisseur: "ancien"}
	for _, c := range []struct{ apis, attendu string }{{"c2", "zone"}, {"", "ancien"}} {
		env.APIs = strings.Fields(c.apis)
		if err := s.acces.definirAncien("m283f", "site", env); err != nil {
			t.Fatal(err)
		}
		if f, err := s.fournisseurDe(SessionHub{Env: "m283f/site"}); err != nil || f.ID != c.attendu {
			t.Errorf("API %q: %q %v", c.apis, f.ID, err)
		}
	}
	env.Fournisseur = ""
	if err := s.acces.definirAncien("m283f", "site", env); err != nil {
		t.Fatal(err)
	}
	if _, err := s.fournisseurDe(SessionHub{Env: "m283f/site"}); err != errSansFournis {
		t.Errorf("without a provider: %v", err)
	}
}

// TestPreparationProjetConteneur (M283, the short trial on the test container): the deployment set by its route,
// then PLAN.md written: one look launches one preparation on the project's server, in the project's mode
// (ignorer here: no plan to accept); its ops lead, a scripted Claude through the real MCP route, reads the web
// server's package and creates the site's folder, found on the container afterwards.
func TestPreparationProjetConteneur(t *testing.T) {
	s, appelAPI, id, _, direct := serveurConteneurTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	p := Projet{Nom: "m283c", Dossier: filepath.Join(t.TempDir(), "m283c")}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	oublierRelais(p.Nom)
	if _, err := definirReglageProjet(p.Nom, "permission", "ignorer"); err != nil {
		t.Fatal(err)
	}
	const dossier = "/srv/athanor/m283c-essai"
	direct("ssh_commande", map[string]any{"commande": "sudo -n rm -rf " + dossier})
	t.Cleanup(func() {
		direct("ssh_commande", map[string]any{"commande": "sudo -n rm -rf " + dossier})
		oublierRelais(p.Nom)
		deplacerReglagesProjet(p.Nom, "")
	})
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	var mu sync.Mutex
	var briefs, sorties []string
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, d string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(d, f))
			return strings.TrimSpace(string(b))
		}
		if c := lire("consigne.txt"); !strings.Contains(c, "security critic") && !strings.Contains(c, "external verifier") {
			mu.Lock()
			briefs = append(briefs, c)
			for _, commande := range []string{"command -v caddy", "sudo -n mkdir -p " + dossier} {
				rep := appelMCP(t, mux, "Bearer "+lire("hub-jeton"), appelOutil("ssh_commande", `{"serveur":"`+id+`","commande":"`+commande+`"}`))
				if rep.Error != nil {
					sorties = append(sorties, "ERROR "+rep.Error.Message)
				} else {
					sorties = append(sorties, map[bool]string{true: "ERROR "}[rep.Result.IsError]+rep.Result.Content[0].Text)
				}
			}
			mu.Unlock()
		}
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"Prepared."}` + "\n"
		return os.WriteFile(filepath.Join(d, "session.jsonl"), []byte(j), 0o600)
	}

	if code, v := appelAPI("/api/projets/m283c/deploiement", map[string]any{"serveurs": []string{id}, "url": "http://m283c.test",
		"plan": "Un site statique servi par Caddy", "moment": "livraison"}); code != 200 {
		t.Fatalf("the deployment by its route: %d %v", code, v)
	}
	s.preparerProjet(p)
	if v := lireRelais()[p.Nom].Preparation; v != nil {
		t.Fatalf("before the plan: %+v", v)
	}
	planRelaisTest(t, p, "- [ ] M1 : Le site statique\n")
	s.preparerProjet(p)
	s.preparerProjet(p)
	for range 2400 { // the task ends
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
	prep := lireRelais()[p.Nom].Preparation
	if prep == nil || prep.Tache == "" || prep.Erreur != "" || len(briefs) != 1 {
		t.Fatalf("the preparation: %+v, %d sessions", prep, len(briefs))
	}
	if !strings.Contains(briefs[0], "Un site statique servi par Caddy") || !strings.Contains(briefs[0], "Le site statique") || strings.Contains(briefs[0], "the owner's accord") {
		t.Errorf("brief: %s", briefs[0])
	}
	if len(sorties) != 2 || !strings.Contains(sorties[0], "caddy") || strings.Contains(strings.Join(sorties, "\n"), "ERROR ") {
		t.Errorf("the ops lead's commands: %q", sorties)
	}
	if r := direct("ssh_commande", map[string]any{"commande": "test -d " + dossier + " && echo present"}); !strings.Contains(r.Sortie, "present") {
		t.Errorf("the site's folder is not on the server: %+v", r)
	}
}
