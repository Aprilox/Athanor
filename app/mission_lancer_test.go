package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionLancer (M157): Lancer refused while the infra keys are locked or the new project's folder holds
// files (nothing done); a new project launched once out of two simultaneous clicks (project created with the Dev
// brief as its request, ticked entries given to their environment and not the unticked one, environments set with
// prod on accord and dev deploying at each milestone, the fake pilot started, one Servers task per environment
// with the fake chef ops); an existing project in native mode (the brief queued, the Servers lane refused, Dev
// launched); a failed step (echec, history, bell, nothing after it); a mission in another state answers 409.
func TestMissionLancer(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	temoin := "TEMOIN-" + aleatoire(10)
	stripe, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé Stripe", Famille: "secret", Type: "stripe"}, temoin+"-code")
	if err != nil {
		t.Fatal(err)
	}
	cf, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare"}, temoin+"-infra")
	if err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"prod"}},
			ServeurInventaire{ID: "a2", Nom: "dev-1", Hote: "203.0.113.6", Roles: []string{"dev"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	boutique := Projet{Nom: "boutique", Dossier: filepath.Join(base, "boutique")}
	ecrireFichier(t, boutique.Demande(), "# Boutique\n\nUne boutique en ligne.\n", 0o644)
	s.config.Projets = append(s.config.Projets, boutique)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	var mu sync.Mutex
	var pilotes, consignes []string
	echecPilote := false
	ancienPilote, ancienneSession := demarrerPiloteMission, sessionTacheServeurs
	t.Cleanup(func() { demarrerPiloteMission, sessionTacheServeurs = ancienPilote, ancienneSession })
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if echecPilote {
			return "", errors.New("the kit cannot be found")
		}
		pilotes = append(pilotes, p.Nom)
		return "Pilot started", nil
	}
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error { // the fake chef ops: no plan
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		consignes = append(consignes, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"sess-ops"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}

	var reponses strings.Builder
	var muRep sync.Mutex
	lancer := func(id, corps string) (int, Mission) {
		r := httptest.NewRequest("POST", "/api/missions/"+id+"/lancer", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		muRep.Lock()
		reponses.WriteString(w.Body.String())
		muRep.Unlock()
		var v struct{ Mission Mission }
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v.Mission
	}
	proposer := func(c CarteMission, politique, etat string) string {
		t.Helper()
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(calculerCarte(c, politique, x))
		m := Mission{ID: aleatoire(8), Phrase: "une mission", Etat: etat, Politique: politique, Carte: b, Voies: []VoieMission{},
			Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission(etat, "")}, Cree: time.Now().UTC().Format(formatCree)}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	finies := func() { // the Servers tasks end (their goroutines must not outlive the test)
		t.Helper()
		for range 400 {
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("a Servers task did not end")
	}

	// 1. a new project: dev behind auth_proxy on dev-1, prod on web-1, Stripe for the project, Cloudflare for web-1
	neuf := proposer(CarteMission{Titre: "Site de la boulangerie", Projet: ProjetCarte{Nom: "boulangerie-martin", Nouveau: true},
		Dev: BriefCarte{Brief: "Un site vitrine avec les horaires"}, Serveurs: BriefCarte{Brief: "DNS, certificats et Caddy"}, JalonsEstimes: 5,
		Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.martin.fr", Protection: "auth_proxy", Serveurs: []string{"a2"}},
			{Nom: "prod", URL: "https://martin.fr", Protection: "aucune", Serveurs: []string{"a1"}}},
		Acces: []AccesCarte{{Entree: "Clé Stripe", Pour: "boulangerie-martin"}, {Entree: "Jeton Cloudflare", Pour: "a1"},
			{Entree: "Clé Stripe", Pour: "boulangerie-martin/prod", Decoche: true}, {Entree: "Clé Mailjet", Pour: "boulangerie-martin"}}}, "prod", "proposee")

	s.coffre.Verrouiller() // the infra keys locked: 409, nothing done
	if code, _ := lancer(neuf, ""); code != http.StatusConflict {
		t.Fatalf("infra locked: %d", code)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	dossier := filepath.Join(base, "boulangerie-martin") // next to the last project
	ecrireFichier(t, filepath.Join(dossier, "notes.txt"), "à moi", 0o644)
	if code, _ := lancer(neuf, ""); code != http.StatusBadRequest {
		t.Fatalf("folder with files: %d", code)
	}
	if m, _ := lireMission(neuf); m.Etat != "proposee" || len(pilotes) != 0 || len(s.config.Projets) != 1 {
		t.Fatalf("a refused launch did something: %s %v %d", m.Etat, pilotes, len(s.config.Projets))
	}
	_ = os.Remove(filepath.Join(dossier, "notes.txt"))

	var wg sync.WaitGroup // two clicks at once: one launch
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func() { defer wg.Done(); codes[i], _ = lancer(neuf, "") }()
	}
	wg.Wait()
	finies()
	if slices.Sort(codes); !slices.Equal(codes, []int{200, 409}) {
		t.Fatalf("two clicks: %v", codes)
	}
	m, _ := lireMission(neuf)
	if m.Etat != "lancee" || len(m.Voies) != 3 || m.Voies[0] != (VoieMission{"dev", "boulangerie-martin", "lancee"}) || m.Voies[1].Module != "servers" || m.Voies[2].Module != "servers" {
		t.Fatalf("launched mission: %+v", m)
	}
	p, ok := s.projetNomme("boulangerie-martin")
	if b, _ := os.ReadFile(p.Demande()); !ok || p.Dossier != dossier || !strings.Contains(string(b), "Un site vitrine avec les horaires") || !slices.Equal(pilotes, []string{"boulangerie-martin"}) {
		t.Fatalf("project %+v, request %q, pilots %v", p, b, pilotes)
	}
	donne := map[string]bool{}
	for _, a := range s.acces.Projet(p.Nom).Acces {
		donne[a.Entree+"/"+a.Env] = true
	}
	if len(donne) != 2 || !donne[stripe.ID+"/dev"] || !donne[cf.ID+"/prod"] {
		t.Fatalf("grants: %v", donne)
	}
	envs := s.acces.Environnements(p.Nom)
	if d := envs["dev"]; d.URL != "https://dev.martin.fr" || d.Protection != "auth_proxy" || d.Politique != "auto" || !d.DeployerAuJalon || !slices.Equal(d.Serveurs, []string{"a2"}) {
		t.Fatalf("dev: %+v", d)
	}
	if pr := envs["prod"]; pr.URL != "https://martin.fr" || pr.Politique != "accord" || pr.DeployerAuJalon || !slices.Equal(pr.Serveurs, []string{"a1"}) {
		t.Fatalf("prod: %+v", pr)
	}
	if len(consignes) != 2 || !slices.ContainsFunc(consignes, func(c string) bool {
		return strings.Contains(c, "DNS, certificats et Caddy") && strings.Contains(c, "environment prod: https://martin.fr")
	}) {
		t.Fatalf("chef ops tasks: %q", consignes)
	}
	if code, _ := lancer(neuf, ""); code != http.StatusConflict { // launched: never again
		t.Fatalf("second launch: %d", code)
	}

	// 2. an existing project in native Windows mode: the brief queued, the Servers lane refused, Dev launched
	modeNatif.Store(true)
	t.Cleanup(func() { modeNatif.Store(false) })
	existant := proposer(CarteMission{Titre: "Page de paiement", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "Ajouter le paiement"},
		Serveurs: BriefCarte{Brief: "Ouvrir le port"}, JalonsEstimes: 2, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.boutique.fr", Serveurs: []string{"a2"}}}}, "tout", "proposee")
	code, m := lancer(existant, `{"dossier":"C:\\ailleurs"}`)
	if code != 200 || m.Etat != "lancee" || len(m.Voies) != 2 || m.Voies[0] != (VoieMission{"dev", "boutique", "lancee"}) || m.Voies[1] != (VoieMission{"servers", "boutique/dev", "refusee"}) ||
		!strings.Contains(m.Historique[len(m.Historique)-1].Detail, errServeursNatif.Error()) {
		t.Fatalf("native mode: %d %+v", code, m)
	}
	if f := boutique.LireFileAttente(); len(f) != 1 || len(consignes) != 2 || pilotes[len(pilotes)-1] != "boutique" {
		t.Fatalf("queue %v, tasks %d, pilots %v", f, len(consignes), pilotes)
	}
	if d := s.acces.Environnements("boutique")["dev"]; d.Politique != "accord" || !d.DeployerAuJalon {
		t.Fatalf("dev with the policy tout: %+v", d)
	}
	modeNatif.Store(false)

	// 3. a failed step: echec, the reason in the history and the bell, the steps after it not run
	echecPilote = true
	rate := proposer(CarteMission{Titre: "Un blog", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "Ajouter un blog"},
		Serveurs: BriefCarte{Brief: "Rien de neuf"}, JalonsEstimes: 1, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.boutique.fr", Serveurs: []string{"a2"}}}}, "prod", "acceptee")
	code, m = lancer(rate, "")
	if h := m.Historique[len(m.Historique)-1]; code != 200 || m.Etat != "echec" || len(m.Voies) != 0 || h.Etat != "echec" || !strings.Contains(h.Detail, "starting the pilot: the kit cannot be found") {
		t.Fatalf("failed step: %d %+v", code, m)
	}
	if len(consignes) != 2 {
		t.Fatal("a Servers task was launched after the failed step")
	}
	if el := (moduleMissions{}).Elements(s); !slices.ContainsFunc(el, func(e ElementHub) bool {
		return e.ID == rate && e.Attente != nil && e.Attente.Raison == "The launch of the mission stopped: {0}" && strings.Contains(e.Attente.Args[0].(string), "the kit cannot be found")
	}) {
		t.Fatalf("no bell for the failed launch: %+v", el)
	}

	// 4. other states: 409; unknown: 404
	for _, etat := range []string{"preparation", "annulee", "echec"} {
		if code, _ := lancer(proposer(CarteMission{Titre: "X", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "x"}, JalonsEstimes: 1}, "prod", etat), ""); code != http.StatusConflict {
			t.Fatalf("%s: %d", etat, code)
		}
	}
	if code, _ := lancer("inconnue1", ""); code != http.StatusNotFound {
		t.Fatalf("unknown mission: %d", code)
	}
	finies()
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("a vault value in an answer")
	}
	if l := lignesAudit(t, dossierJournalAudit()); !slices.ContainsFunc(l, func(x LigneAudit) bool { return x.Action == "mission lancée" && x.Mission == neuf }) {
		t.Fatal("no audit line for the launch")
	}
}
