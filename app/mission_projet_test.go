package main

// M254: a new project's settings on its mission card (folder, modes, GitHub), checked when set, kept from the
// agent, applied at the launch; a failed GitHub step leaves the project created and the launch resumes without
// a second project, request or repository.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMissionProjet(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil { // dossierAthanor() stays where it is (see TestMissionLancer)
		t.Fatal(err)
	}
	base := t.TempDir()
	boutique := Projet{Nom: "boutique", Dossier: filepath.Join(base, "boutique")}
	ecrireFichier(t, boutique.Demande(), "# Boutique\n", 0o644)
	s.config.Projets = append(s.config.Projets, boutique)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	var mu sync.Mutex
	var pilotes, appelsGH []string
	creations, echecsGH := 0, 1
	lies := map[string]bool{}
	ancienPilote, ancienGH := demarrerPiloteMission, githubMission
	t.Cleanup(func() { demarrerPiloteMission, githubMission = ancienPilote, ancienGH })
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		pilotes = append(pilotes, p.Nom)
		return "Pilot started", nil
	}
	githubMission = func(_ *Serveur, p Projet, mode, depot string) error { // a fake GitHub: fails echecsGH times first
		mu.Lock()
		defer mu.Unlock()
		appelsGH = append(appelsGH, mode+" "+depot)
		if lies[p.Nom] {
			return nil
		}
		if echecsGH > 0 {
			echecsGH--
			return errors.New("GitHub: no answer from WSL after 45 s")
		}
		if mode == "creer" {
			creations++
		}
		lies[p.Nom] = true
		return nil
	}

	appel := func(chemin, corps string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	carte := func(id string) CarteMission {
		t.Helper()
		m, err := lireMission(id)
		var c CarteMission
		if err != nil || json.Unmarshal(m.Carte, &c) != nil {
			t.Fatal("card unreadable", err)
		}
		return c
	}
	lancer := func(id string) (int, Mission) {
		m, _ := lireMission(id)
		w := appel("/api/missions/"+id+"/lancer", `{"mot_de_passe":"mdp-test","empreinte":"`+empreinteCarte(m)+`"}`)
		var v struct{ Mission Mission }
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v.Mission
	}
	proposer := func(c CarteMission) string {
		t.Helper()
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(calculerCarte(c, "env", x))
		m := Mission{ID: aleatoire(8), Phrase: "une mission", Etat: "proposee", Politique: "env", Carte: b, Voies: []VoieMission{},
			Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	neuve := CarteMission{Titre: "Atelier de poterie", Projet: ProjetCarte{Nom: "atelier-poterie", Nouveau: true},
		Dev: BriefCarte{Brief: "Un site pour l'atelier TEMOIN-BRIEF"}, JalonsEstimes: 3}
	id := proposer(neuve)

	// 1. refused folders and values: nothing changes on the card
	plein := filepath.Join(base, "plein")
	ecrireFichier(t, filepath.Join(plein, "index.html"), "x", 0o644)
	avant, _ := lireMission(id)
	for nom, corps := range map[string]string{
		"relative":       `{"dossier":"relatif/atelier"}`,
		"system":         `{"dossier":` + jsonTexte(filepath.Join(cmp.Or(os.Getenv("SystemRoot"), "/usr"), "atelier-poterie")) + `}`,
		"other project":  `{"dossier":` + jsonTexte(boutique.Dossier) + `}`,
		"with files":     `{"dossier":` + jsonTexte(plein) + `}`,
		"bad repository": `{"github":"lier","depot":"pas un depot"}`,
		"bad github":     `{"github":"public"}`,
		"bad framing":    `{"modes":{"cadrage_max":5}}`,
	} {
		if w := appel("/api/missions/"+id+"/projet", corps); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", nom, w.Code, w.Body.String())
		}
	}
	if m, _ := lireMission(id); empreinteCarte(m) != empreinteCarte(avant) {
		t.Fatal("a refused setting changed the card")
	}
	// a paired device never chooses the folder (B84)
	r := httptest.NewRequest("POST", "/api/missions/"+id+"/projet", strings.NewReader(`{"dossier":`+jsonTexte(filepath.Join(base, "tel"))+`}`))
	r.SetPathValue("id", id)
	r = r.WithContext(context.WithValue(r.Context(), cleContexte(nomCleAppareil), "tel-1"))
	wt := httptest.NewRecorder()
	if s.apiProjetMission(wt, r); wt.Code != http.StatusForbidden {
		t.Fatalf("device: %d, want 403", wt.Code)
	}

	// 2. the settings set: folder cleaned, default repository name, modes; Touche shows the folder
	dossier := filepath.Join(base, "atelier")
	w := appel("/api/missions/"+id+"/projet", `{"dossier":`+jsonTexte(dossier+string(filepath.Separator))+`,"github":"creer","modes":{"parallele":true,"parallele_max":4,"eco":true,"cadrage_max":45,"duree_max":72}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}
	c := carte(id)
	if c.Projet.Dossier != dossier || c.Touche.Dossier != dossier || c.Projet.GitHub != "creer" || c.Projet.Depot != nomDepotPropose("atelier-poterie") || !c.Projet.Proprietaire ||
		c.Dev.Modes == nil || !c.Dev.Modes.Parallele || c.Dev.Modes.ParalleleMax != 4 || *c.Dev.Modes.CadrageMax != 45 || *c.Dev.Modes.DureeMax != 72 {
		t.Fatalf("card after set: %+v %+v", c.Projet, c.Dev.Modes)
	}

	// 3. the agent's new card never brings a folder or a repository, and keeps the owner's
	sc := neuve
	sc.Dev.Brief += " avec une galerie"
	sc.Projet.Dossier, sc.Projet.GitHub, sc.Projet.Depot = filepath.Join(base, "ailleurs"), "lier", "pirate/depot"
	args, _ := json.Marshal(map[string]any{"carte": sc})
	if _, err := outilProposerMission(s, SessionHub{Mission: id}, args); err != nil {
		t.Fatal(err)
	}
	if c = carte(id); c.Projet.Dossier != dossier || c.Projet.GitHub != "creer" || c.Dev.Modes == nil || !c.Dev.Modes.Eco || !strings.Contains(c.Dev.Brief, "galerie") {
		t.Fatalf("agent's card: %+v %+v", c.Projet, c.Dev.Modes)
	}

	// 4. launched: GitHub fails, the project stays created (folder, modes), a clear echec, no pilot
	code, m := lancer(id)
	if code != http.StatusOK || m.Etat != "echec" || !echecLancement(m) {
		t.Fatalf("launch: %d %s", code, m.Etat)
	}
	raison := m.Historique[len(m.Historique)-1]
	if raison.Message == nil || !strings.Contains(raison.Message.Cle, "is created in {1}, but its GitHub repository is not linked") || !strings.Contains(raison.Detail, dossier) {
		t.Fatalf("echec reason: %+v", raison)
	}
	p, ok := s.projetNomme("atelier-poterie")
	if !ok || !strings.EqualFold(p.Dossier, dossier) {
		t.Fatalf("project not created in %s: %+v", dossier, p)
	}
	env, _ := os.ReadFile(p.envProjet())
	for _, l := range []string{"PARALLEL=1", "PARALLEL_MAX=4", "ECO=1", "FRAMING_MAX_MIN=45", "MAX_HOURS=72"} {
		if !strings.Contains(string(env), l) {
			t.Errorf("modes: no %s in %s", l, env)
		}
	}
	if len(pilotes) != 0 {
		t.Fatal("the pilot started after the failed GitHub step")
	}

	// 5. resumed: no second project, request or repository; GitHub linked, the pilot started once
	if code, m = lancer(id); code != http.StatusOK || m.Etat != "lancee" {
		t.Fatalf("resume: %d %s %+v", code, m.Etat, m.Historique[len(m.Historique)-1])
	}
	n := 0
	for _, q := range s.config.Projets {
		if strings.EqualFold(q.Nom, "atelier-poterie") {
			n++
		}
	}
	demande, _ := os.ReadFile(p.Demande())
	if n != 1 || creations != 1 || strings.Count(string(demande), "TEMOIN-BRIEF") != 1 || len(pilotes) != 1 || strings.Join(appelsGH, ",") != "creer atelier-poterie,creer atelier-poterie" {
		t.Fatalf("resume: %d projects, %d repositories, request %q, pilots %v, GitHub %v", n, creations, demande, pilotes, appelsGH)
	}
	if code, _ = lancer(id); code != http.StatusConflict {
		t.Fatalf("launched twice: %d", code)
	}

	// 6. linking an existing repository (its address accepted); an existing project's card has no project settings
	id2 := proposer(CarteMission{Titre: "Blog", Projet: ProjetCarte{Nom: "blog-poterie", Nouveau: true}, Dev: BriefCarte{Brief: "Un blog"}, JalonsEstimes: 2})
	if w := appel("/api/missions/"+id2+"/projet", `{"github":"lier","depot":"https://github.com/moi/blog.git"}`); w.Code != http.StatusOK {
		t.Fatalf("link: %d %s", w.Code, w.Body.String())
	}
	if code, m = lancer(id2); code != http.StatusOK || m.Etat != "lancee" || appelsGH[len(appelsGH)-1] != "lier moi/blog" {
		t.Fatalf("link launch: %d %s %v", code, m.Etat, appelsGH)
	}
	id3 := proposer(CarteMission{Titre: "Soldes", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "Les soldes"}, JalonsEstimes: 1})
	if w := appel("/api/missions/"+id3+"/projet", `{"github":"creer"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("existing project: %d", w.Code)
	}
}
