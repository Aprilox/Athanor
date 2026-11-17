package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// with the fake chef ops); an existing project in native mode (the brief queued, the Servers task forced and the
// project forced, both logged, Dev launched); a failed step (echec, history, bell, nothing after it); a mission in another state answers 409.
func TestMissionLancer(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	// the config next to the test binary moves dossierAthanor() there (installationAncienne): written now, as the
	// launch's new project does, so the servers' and missions' folders does not move in the middle (run alone, 404 otherwise)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
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
	ecrireFichier(t, boutique.Plan(), "# Plan\n\n- [x] M1 : Boutique en ligne\n", 0o644) // M249: framed already, its Servers lane starts at the launch
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
	appel := func(chemin, corps string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		muRep.Lock()
		reponses.WriteString(w.Body.String())
		muRep.Unlock()
		return w
	}
	lancerBrut := func(id, corps string) (int, Mission) {
		w := appel("/api/missions/"+id+"/lancer", corps)
		var v struct{ Mission Mission }
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v.Mission
	}
	lancer := func(id, corps string) (int, Mission) { // C78: the owner retypes the password in the window
		if corps = strings.TrimSuffix(strings.TrimPrefix(corps, "{"), "}"); corps != "" {
			corps = "," + corps
		}
		if m, err := lireMission(id); err == nil { // R3: the card the window shows
			corps += `,"empreinte":"` + empreinteCarte(m) + `"`
		}
		return lancerBrut(id, `{"mot_de_passe":"mdp-test"`+corps+"}")
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
		Dev: BriefCarte{Brief: "Un site vitrine avec les horaires", Modes: &ModesCarte{Eco: true, Rapide: true}}, Serveurs: BriefCarte{Brief: "DNS, certificats et Caddy"}, JalonsEstimes: 5,
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
	if m.Etat != "lancee" || len(m.Voies) != 3 || m.Voies[0] != (VoieMission{Module: "dev", Ref: "boulangerie-martin", Etat: "lancee"}) || m.Voies[1].Module != "servers" || m.Voies[2].Module != "servers" {
		t.Fatalf("launched mission: %+v", m)
	}
	p, ok := s.projetNomme("boulangerie-martin")
	if b, _ := os.ReadFile(p.Demande()); !ok || p.Dossier != dossier || !strings.Contains(string(b), "Un site vitrine avec les horaires") || !slices.Equal(pilotes, []string{"boulangerie-martin"}) {
		t.Fatalf("project %+v, request %q, pilots %v", p, b, pilotes)
	}
	// M174b: the card's working modes written by the same function as Manage, before the pilot starts
	if e, _ := os.ReadFile(p.envProjet()); !strings.Contains(string(e), "FAST=1") || !strings.Contains(string(e), "ECO=1") || !strings.Contains(string(e), "PARALLEL=0") {
		t.Fatalf("the new project's modes: %s", e)
	}
	if !slices.ContainsFunc(m.Historique, func(h EvenementMission) bool { return h.Detail == "working modes set: eco, fast" }) {
		t.Fatalf("no history line for the modes: %+v", m.Historique)
	}
	donne := map[string]bool{}
	for _, a := range s.acces.Projet(p.Nom).Acces {
		donne[a.Entree+"/"+a.Env] = true
	}
	if len(donne) != 2 || !donne[stripe.ID+"/dev"] || !donne[cf.ID+"/prod"] {
		t.Fatalf("grants: %v", donne)
	}
	// C86: one history line per environment set and entry given (M265: environments first); the grants in secrets.env after step 3 (the
	// fake pilot injects nothing, as a pilot already running)
	faits := []string{}
	for _, h := range m.Historique {
		if strings.HasPrefix(h.Detail, "access given: ") || strings.HasPrefix(h.Detail, "environment set: ") {
			faits = append(faits, strings.SplitN(h.Detail, " → ", 2)[0])
		}
	}
	if !slices.Equal(faits, []string{"environment set: dev https://dev.martin.fr, auth_proxy, auto", "environment set: prod https://martin.fr, aucune, accord", "access given: Clé Stripe", "access given: Jeton Cloudflare"}) {
		t.Fatalf("history of the launch: %q", faits)
	}
	if b, err := os.ReadFile(filepath.Join(dossier, ".athanor", "secrets.env")); err != nil || !strings.Contains(string(b), temoin+"-code") {
		t.Fatalf("grants not injected at the launch: %v", err)
	}
	if strings.Contains(fmt.Sprint(m.Historique), temoin) {
		t.Fatal("a value in the mission's history")
	}
	envs := s.acces.Environnements(p.Nom)
	if d := envs["dev"]; d.URL != "https://dev.martin.fr" || d.Protection != "auth_proxy" || d.Politique != "auto" || !d.DeployerAuJalon || !slices.Equal(d.Serveurs, []string{"a2"}) {
		t.Fatalf("dev: %+v", d)
	}
	if pr := envs["prod"]; pr.URL != "https://martin.fr" || pr.Politique != "accord" || pr.DeployerAuJalon || !slices.Equal(pr.Serveurs, []string{"a1"}) {
		t.Fatalf("prod: %+v", pr)
	}
	// M249: a new project has no plan yet: both Servers lanes wait for it, then start once (the follower's reading)
	if len(consignes) != 0 || m.Voies[1].Etat != "attente_plan" || m.Voies[1].Ref != "" || m.Voies[2].Env != "boulangerie-martin/prod" {
		t.Fatalf("Servers lanes before the plan: %+v, tasks %d", m.Voies, len(consignes))
	}
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site vitrine avec les horaires\n", 0o644)
	s.observerMission(neuf)
	s.observerMission(neuf)
	finies()
	if m, _ = lireMission(neuf); m.Voies[1].Ref == "" || m.Voies[2].Ref == "" || m.Voies[1].Etat == "attente_plan" {
		t.Fatalf("Servers lanes after the plan: %+v", m.Voies)
	}
	if len(consignes) != 2 || !slices.ContainsFunc(consignes, func(c string) bool {
		return strings.Contains(c, "DNS, certificats et Caddy") && strings.Contains(c, "environment prod: https://martin.fr")
	}) {
		t.Fatalf("chef ops tasks: %q", consignes)
	}
	if code, _ := lancer(neuf, ""); code != http.StatusConflict { // launched: never again
		t.Fatalf("second launch: %d", code)
	}
	// M159: the lanes read now — the project's pilot stopped (the fake pilot leaves no process), each task with its server
	r := httptest.NewRequest("GET", "/api/missions/"+neuf, nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var lu struct{ Mission Mission }
	_ = json.Unmarshal(w.Body.Bytes(), &lu)
	if v := lu.Mission.Voies; len(v) != 3 || v[0].Actuel != "arrete" || v[1].Actuel != "finie" || v[2].Actuel != "finie" ||
		!slices.Contains([]string{"a1", "a2"}, v[1].Serveur) || v[1].Serveur == v[2].Serveur {
		t.Fatalf("lanes now: %s", w.Body.String())
	}
	if m, _ := lireMission(neuf); m.Voies[1].Actuel != "" { // never stored
		t.Fatalf("stored lane: %+v", m.Voies[1])
	}

	// 2. an existing project in native Windows mode: the brief queued, Dev launched, the Servers lane launched without a
	// barrier (launching is the owner's accord): the project forced, both logged
	modeNatif.Store(true)
	t.Cleanup(func() { modeNatif.Store(false) })
	existant := proposer(CarteMission{Titre: "Page de paiement", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "Ajouter le paiement"},
		Serveurs: BriefCarte{Brief: "Ouvrir le port"}, JalonsEstimes: 2, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.boutique.fr", Serveurs: []string{"a2"}}}}, "tout", "proposee")
	// C82: the owner's "never ask again" for dev's deployments and for dev-1's plans, set before the tout mission
	for _, a := range []Approbation{{Projet: "boutique", Env: "dev", Agent: "pilote", Action: actionDeploiement},
		{Projet: prefixeTache + "t0", Env: "dev", Agent: agentServeurs, Action: actionPlanServeur, Details: map[string]string{"serveur": "a2"}}} {
		d, _ := s.demanderApprobation(a)
		if _, err := s.approbations.Decider(d.ID, "toujours", "fenetre"); err != nil {
			t.Fatal(err)
		}
	}
	regleDeploi := RegleAccord{Projet: "boutique", Env: "dev", Action: actionDeploiement}
	if !s.approbations.Regle(regleDeploi) || !s.approbations.Regle(reglePlan(prefixeTache, "a2")) || missionTout("boutique/dev") {
		t.Fatal("rules not set, or a tout mission before the launch")
	}
	code, m := lancer(existant, `{"dossier":"C:\\ailleurs"}`)
	// C82: the launch ends the deployment rule; the plan rule stays for other tasks but the mission's ignore it
	if s.approbations.Regle(regleDeploi) || !missionTout("boutique/dev") || missionTout("boutique/prod") || missionTout("autre/dev") {
		t.Fatal("tout mission: the deployment rule survives, or missionTout is wrong")
	}
	e := Etape{Outil: "service", Arguments: json.RawMessage(`{"nom":"caddy"}`), Classe: ClasseChangement, Raison: "restart"}
	if err := s.planOuRegle(SessionHub{Projet: prefixeTache + "t1", Mission: "t1", Agent: agentServeurs, Env: "boutique/dev"}, "a2", &e); err == nil || !strings.Contains(err.Error(), "plan required") {
		t.Fatalf("a change of a tout mission's task under the server's rule: %v", err)
	}
	if err := s.planOuRegle(SessionHub{Projet: prefixeTache + "t2", Mission: "t2", Agent: agentServeurs, Env: "autre/dev"}, "a2", &e); err != nil {
		t.Fatalf("a change of another task under the server's rule: %v", err)
	}
	if code != 200 || m.Etat != "lancee" || len(m.Voies) != 2 || m.Voies[0] != (VoieMission{Module: "dev", Ref: "boutique", Etat: "lancee"}) || m.Voies[1].Module != "servers" ||
		m.Voies[1].Etat == "refusee" || m.Voies[1].Ref == "" || m.Voies[1].Ref == "boutique/dev" || !slices.ContainsFunc(m.Historique, func(h EvenementMission) bool {
		return strings.Contains(h.Detail, "the project is forced by this launch")
	}) {
		t.Fatalf("native mode: %d %+v", code, m)
	}
	var tache TacheServeurs
	if b, err := os.ReadFile(filepath.Join(dossierTaches("a2"), m.Voies[1].Ref+".json")); err != nil || json.Unmarshal(b, &tache) != nil || !tache.Forcee || !s.acces.Projet("boutique").ForcerInfraNatif {
		t.Fatalf("native mode: task %+v %v, project not forced", tache, err)
	}
	natifs := map[string]bool{}
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if l.Projet == "boutique" && (l.Action == "forcer_infra_natif" || l.Action == "voie Serveurs sans barrière" && strings.HasPrefix(l.Resultat, m.Voies[1].Ref+" (boutique/dev")) {
			natifs[l.Action] = true
		}
	}
	if len(natifs) != 2 {
		t.Fatalf("native launch not logged: %v", natifs)
	}
	attendreTachesMission(t, &mu, &consignes, 3)
	if f := boutique.LireFileAttente(); len(f) != 1 || len(consignes) != 3 || pilotes[len(pilotes)-1] != "boutique" {
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
	// B87: the reason as a key of i18n.js and its argument, for the bell and the detail page in their language
	if msg := m.Historique[len(m.Historique)-1].Message; msg == nil || msg.Cle != "starting the pilot: {0}" || fmt.Sprint(msg.Args) != "[the kit cannot be found]" {
		t.Fatalf("failed step's message: %+v", msg)
	}
	if len(consignes) != 3 {
		t.Fatal("a Servers task was launched after the failed step")
	}
	if el := (moduleMissions{}).Elements(s); !slices.ContainsFunc(el, func(e ElementHub) bool {
		return e.ID == rate && e.Attente != nil && e.Attente.Raison == "The launch of the mission stopped: {0}" && strings.Contains(fmt.Sprint(e.Attente.Args[0]), "the kit cannot be found")
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

	// 5. C78: the window's cookie without the password, or with a wrong one, launches nothing (401), the mission intact
	preuve := proposer(CarteMission{Titre: "Avec preuve", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "x"}, JalonsEstimes: 1}, "prod", "proposee")
	for _, corps := range []string{"", `{"mot_de_passe":"faux"}`} {
		if code, _ := lancerBrut(preuve, corps); code != http.StatusUnauthorized {
			t.Fatalf("launched without the password (%s): %d", corps, code)
		}
	}
	if m, _ := lireMission(preuve); m.Etat != "proposee" {
		t.Fatalf("refused launch, yet %s", m.Etat)
	}

	// 6. C80: native mode, an infra entry ticked, the project not forced: said on the card (information), the launch
	// forces the project; a forced existing project is not warned, a new one always
	modeNatif.Store(true)
	if err := s.acces.ForcerInfra("boutique", false); err != nil {
		t.Fatal(err)
	}
	cInfra := CarteMission{Titre: "Infra en natif", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "x"}, JalonsEstimes: 1,
		Acces: []AccesCarte{{Entree: "Jeton Cloudflare", Pour: "boutique"}}}
	infra := proposer(cInfra, "prod", "proposee")
	if m, _ := lireMission(infra); !strings.Contains(string(m.Carte), "avertissement_infra") {
		t.Fatalf("infra in native mode, not said: %s", m.Carte)
	}
	echecPilote = false
	if code, m := lancer(infra, ""); code != 200 || m.Etat != "lancee" || !s.acces.Projet("boutique").ForcerInfraNatif {
		t.Fatalf("infra in native mode: %d %+v", code, m)
	}
	if m, _ := lireMission(infra); resumeCarteAccord(m)["natif"] != "oui" { // M245: a lancer_mission request says it
		t.Fatalf("native forcing not said in the accord request: %v", resumeCarteAccord(m))
	}
	if l := lignesAudit(t, dossierJournalAudit()); !slices.ContainsFunc(l, func(x LigneAudit) bool { // M245: as the Access page
		return x.Projet == "boutique" && x.Action == "acces_accorde_force"
	}) {
		t.Fatal("infra entry given in native mode not logged acces_accorde_force")
	}
	if err := s.acces.ForcerInfra("boutique", false); err != nil {
		t.Fatal(err)
	}
	x := s.contexteCarte()
	if err := validerCarte(&cInfra, x); err != nil {
		t.Fatal(err)
	}
	x.forcerInfra["boutique"] = true
	if c := calculerCarte(cInfra, "prod", x); c.AvertissementInfra != "" {
		t.Fatalf("forced project still warned: %+v", c)
	}
	cInfra.Projet = ProjetCarte{Nom: "boutique", Nouveau: true}
	if c := calculerCarte(cInfra, "prod", x); c.AvertissementInfra == "" {
		t.Fatal("a new project in native mode, not warned")
	}
	modeNatif.Store(false)

	echecPilote = false
	// 7. C81: an existing staging (auth_proxy, a2, accord) never weakened by the mission (aucune, no server, policy prod):
	// before → after on the card; the owner's Modify changes the protection, the policy stays the strictest
	if err := s.acces.definirAncien("boutique", "staging", Environnement{URL: "https://staging.boutique.fr", Protection: "auth_proxy",
		Serveurs: []string{"a2"}, Politique: "accord"}); err != nil {
		t.Fatal(err)
	}
	cStaging := CarteMission{Titre: "Préprod", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "x"}, JalonsEstimes: 1,
		Envs: []EnvCarte{{Nom: "staging", URL: "https://pre.boutique.fr", Protection: "aucune"}}}
	staging := proposer(cStaging, "prod", "proposee")
	var c CarteMission
	m, _ = lireMission(staging)
	_ = json.Unmarshal(m.Carte, &c)
	if e := c.Envs[0]; e.Protection != "auth_proxy" || !slices.Equal(e.Serveurs, []string{"a2"}) || e.Politique != "accord" ||
		e.Avant == nil || e.Avant.URL != "https://staging.boutique.fr" || e.Avant.Protection != "auth_proxy" {
		t.Fatalf("staging on the card: %+v %+v", e, e.Avant)
	}
	if code, m := lancer(staging, ""); code != 200 || m.Etat != "lancee" {
		t.Fatalf("staging launch: %d %+v", code, m)
	}
	if e := s.acces.Environnements("boutique")["staging"]; e.URL != "https://pre.boutique.fr" || e.Protection != "auth_proxy" ||
		!slices.Equal(e.Serveurs, []string{"a2"}) || e.Politique != "accord" {
		t.Fatalf("staging weakened: %+v", e)
	}
	staging = proposer(cStaging, "autonome", "proposee")
	if w := appel("/api/missions/"+staging+"/modifier", `{"envs":[{"nom":"staging","url":"https://pre.boutique.fr","protection":"aucune","serveurs":[]}]}`); w.Code != 200 {
		t.Fatalf("owner's modify: %d %s", w.Code, w.Body)
	}
	m, _ = lireMission(staging)
	_ = json.Unmarshal(m.Carte, &c)
	if e := c.Envs[0]; e.Protection != "aucune" || len(e.Serveurs) != 0 || e.Politique != "accord" || !e.Proprietaire ||
		e.Avant == nil || e.Avant.Protection != "auth_proxy" {
		t.Fatalf("owner's staging: %+v %+v", e, e.Avant)
	}

	// 8. B88: the new project's folder checked before lancee (relative, network, system, ../, taken, no parent) → 400, intact
	neuf2 := proposer(CarteMission{Titre: "Un autre site", Projet: ProjetCarte{Nom: "site-neuf", Nouveau: true}, Dev: BriefCarte{Brief: "x"}, JalonsEstimes: 1}, "prod", "proposee")
	for _, d := range []string{`relatif\x`, `\\serveur\partage\neuf`, `C:\Windows\System32\neuf`, `..\..\neuf`, boutique.Dossier, filepath.Join(base, "absent", "neuf")} {
		b, _ := json.Marshal(d)
		if code, _ := lancer(neuf2, `{"dossier":`+string(b)+`}`); code != http.StatusBadRequest {
			t.Fatalf("folder %s: %d", d, code)
		}
	}
	if m, _ := lireMission(neuf2); m.Etat != "proposee" || len(m.Historique) != 1 {
		t.Fatalf("refused folders, yet %s %+v", m.Etat, m.Historique)
	}
	finies()
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("a vault value in an answer")
	}
	if l := lignesAudit(t, dossierJournalAudit()); !slices.ContainsFunc(l, func(x LigneAudit) bool { return x.Action == "mission lancée" && x.Mission == neuf }) {
		t.Fatal("no audit line for the launch")
	}
}

// attendreTachesMission: the fake chef ops writes its consigne from the task's goroutine
func attendreTachesMission(t *testing.T, mu *sync.Mutex, consignes *[]string, n int) {
	t.Helper()
	for fin := time.Now().Add(5 * time.Second); time.Now().Before(fin); time.Sleep(20 * time.Millisecond) {
		mu.Lock()
		l := len(*consignes)
		mu.Unlock()
		if l >= n {
			return
		}
	}
}
