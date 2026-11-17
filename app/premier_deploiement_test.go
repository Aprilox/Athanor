package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// chefOpsTest: the fake ops lead of the Servers tasks (its critic and verifier sessions say nothing): jouer, if
// any, plays one task from its folder and brief. The returned function waits for every task to end, then gives
// the briefs played so far.
func chefOpsTest(t *testing.T, jouer func(dossier, consigne string)) func() []string {
	t.Helper()
	ancienne := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancienne })
	var mu sync.Mutex
	var consignes []string
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		if c := string(b); !strings.Contains(c, "security critic") && !strings.Contains(c, "external verifier") {
			mu.Lock()
			consignes = append(consignes, c)
			mu.Unlock()
			if jouer != nil {
				jouer(dossier, c)
			}
		}
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"done"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	return func() []string {
		t.Helper()
		for range 2400 { // the tasks end (their goroutines must not outlive the test)
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
		return slices.Clone(consignes)
	}
}

// chefOpsDeployeur: the scripted ops lead of a first deployment, through the real MCP route: its first session
// proposes the plan the brief asks for (one deployer step: commit to env, static, by artefact), its resumed one
// applies it; sorties: what its two tools answered, read once the task has ended.
func chefOpsDeployeur(t *testing.T, mux http.Handler, serveur, env, commit string) (func() []string, *[]string) {
	t.Helper()
	sorties := &[]string{}
	return chefOpsTest(t, func(d, _ string) {
		jeton, _ := os.ReadFile(filepath.Join(d, "hub-jeton"))
		dep := `{"env":"` + env + `","methode":"statique","source":"artefact","dossier":"site","commit":"` + commit + `"}`
		a := [2]string{"proposer_plan", `{"serveur":"` + serveur + `","resume":"Publier le site","etapes":[{"outil":"deployer","arguments":` + dep + `,"pourquoi":"premier déploiement"}]}`}
		if reprise, _ := os.ReadFile(filepath.Join(d, "reprise")); len(reprise) > 0 { // its session resumed: the plan is accepted
			a = [2]string{"deployer", dep}
		}
		rep := appelMCP(t, mux, "Bearer "+strings.TrimSpace(string(jeton)), appelOutil(a[0], a[1]))
		if rep.Error != nil {
			*sorties = append(*sorties, "ERROR "+rep.Error.Message)
		} else {
			*sorties = append(*sorties, map[bool]string{true: "ERROR "}[rep.Result.IsError]+rep.Result.Content[0].Text)
		}
	}), sorties
}

// TestPremierDeploiementProjetConteneur (M284, the short trial on the test container): a project whose deployment
// is set by its route (the disposable server, at delivery, mode ignorer), no mission: a delivery launches the first
// deployment's task, its ops lead deploys the delivered HEAD, the site answers, and the relay shows that
// deployment: at no moment a failure, an error text nor a bell.
func TestPremierDeploiementProjetConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, appelAPI, id, _, direct := serveurConteneurTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	depot, _, c2 := depotSiteExemple(t)
	p := Projet{Nom: "m284c", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	if _, err := definirReglageProjet(p.Nom, "permission", "ignorer"); err != nil {
		t.Fatal(err)
	}
	oublierRelais(p.Nom)
	const hote = "m284c.test"
	resoudreTest(t, hote+":80", web)
	nettoyer := "sudo -n rm -rf /srv/athanor/m284c-* /etc/caddy/sites/" + hote + ".caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	direct("ssh_commande", map[string]any{"commande": nettoyer})
	t.Cleanup(func() {
		direct("ssh_commande", map[string]any{"commande": nettoyer})
		oublierRelais(p.Nom)
		deplacerReglagesProjet(p.Nom, "")
	})
	if code, v := appelAPI("/api/projets/m284c/deploiement", map[string]any{"serveurs": []string{id}, "url": "http://" + hote,
		"plan": "Un site statique servi par Caddy", "moment": "livraison"}); code != 200 {
		t.Fatalf("the deployment by its route: %d %v", code, v)
	}
	env := s.envJalon(p.Nom)
	briefs, sorties := chefOpsDeployeur(t, mux, id, env, c2)
	calme := func(etat string) {
		t.Helper()
		if v := s.etatRelais(p.Nom); v == nil || v.Etat != etat || v.Erreur != "" || lireRelais()[p.Nom].Erreur != "" || len(s.alertesRelais()) != 0 {
			t.Fatalf("wanted %q without an error nor a bell: shown %+v, record %+v, bell %+v", etat, v, lireRelais()[p.Nom], s.alertesRelais())
		}
	}
	s.noterLivraison(p, time.Now()) // the first look learns
	livrerTest(t, p, "1")
	s.noterLivraison(p, time.Now())
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		t.Fatal("deployed without a method")
	}
	calme("preparation")
	if b := briefs(); len(b) != 2 || !strings.Contains(b[0], c2) || len(*sorties) != 2 || !strings.Contains((*sorties)[1], `"etat": "ok"`) {
		t.Fatalf("the first deployment's task: %d sessions, tools: %q", len(b), *sorties)
	}
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		t.Fatal("deployed again what the task deployed")
	}
	calme("ok")
	if h := historiqueDeploiements(p.Nom, env); len(h) != 1 || h[0].Commit != c2 || lireRelais()[p.Nom].ID != h[0].ID {
		t.Fatalf("history: %s, record %+v", enJSON(h), lireRelais()[p.Nom])
	}
	req, _ := http.NewRequest("GET", "http://"+web+"/", nil)
	req.Host = hote
	rep, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	corps, _ := io.ReadAll(rep.Body)
	rep.Body.Close()
	if !strings.Contains(string(corps), "site-exemple v2") {
		t.Errorf("served: %.200q", corps)
	}
	s.noterLivraison(p, time.Now())
	if ch := s.avancerRelais(p, time.Now()); ch != nil || len(historiqueDeploiements(p.Nom, env)) != 1 {
		t.Error("deployed twice")
	}
}

// TestPremierDeploiementProjet (M284): a project with a server, a moment and no deployment method yet gets its
// first deployment from one Servers task at the delivery, without a mission: "preparation" while the task works,
// never a failure, a bell nor an error text. A task that did not deploy leaves "a_preparer" and is not launched
// again at each look, only at the next delivery; once a task has deployed, the relay shows that deployment. An
// older version's failure "no deployment method yet" is taken up the same way. Without a server: nothing tried
// nor noted. On request (c): "a_preparer" as an information, nothing launched alone, and the route "Prepare now"
// launches the task (once, refused in native Windows mode unless forced, and where there is nothing to prepare).
func TestPremierDeploiementProjet(t *testing.T) {
	s, appel, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.RemoveAll(dossierMissions())
	r1 := serveurRelaisTest(t)
	var deployer sync.Map // project → the commit its next task deploys
	bloquer, bloque := make(chan struct{}), make(chan struct{}, 4)
	briefs := chefOpsTest(t, func(_, c string) {
		if strings.Contains(c, "project m284c ") { // the owner's task stays at work until the test lets it go
			bloque <- struct{}{}
			<-bloquer
		}
		deployer.Range(func(projet, commit any) bool {
			if strings.Contains(c, "project "+projet.(string)+" ") {
				if err := s.acces.NoterDeploiement(projet.(string), "site", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}); err != nil {
					t.Error(err)
				}
				if err := noterHistorique(Deploiement{ID: "d-" + projet.(string), Projet: projet.(string), Env: "site", Commit: commit.(string), Etat: "ok", Auteur: "agent"}); err != nil {
					t.Error(err)
				}
			}
			return true
		})
	})
	env := Environnement{URL: "https://m284.example.fr", Serveurs: []string{r1}, Protection: "aucune", Politique: "accord", Moment: "livraison"}
	calme := func(p Projet, etat string) {
		t.Helper()
		r, v := lireRelais()[p.Nom], s.etatRelais(p.Nom)
		if r.Erreur != "" || len(s.alertesRelais()) != 0 || (v == nil) != (etat == "") || v != nil && (v.Etat != etat || v.Erreur != "") {
			t.Fatalf("%s: wanted %q without an error nor a bell: record %+v, shown %+v, bell %+v", p.Nom, etat, r, v, s.alertesRelais())
		}
	}

	// (b) a server and a moment: the delivery launches the first deployment
	p, c2 := projetRelaisTest(t, s, "m284", env)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	s.noterLivraison(p, time.Now()) // the first look learns
	livrerTest(t, p, "1")
	s.noterLivraison(p, time.Now())
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		t.Fatal("a deployment started without a method")
	}
	r := lireRelais()[p.Nom]
	if r.Etat != "preparation" || r.Premier == "" || r.Commit != c2 {
		t.Fatalf("the first deployment's task: %+v", r)
	}
	calme(p, "preparation")
	b := briefs()
	if len(b) != 1 || !strings.Contains(b[0], prefixePremierDeploiement+" of project m284 (https://m284.example.fr):") || !strings.Contains(b[0], c2) {
		t.Fatalf("brief: %q", b)
	}
	audits := slices.DeleteFunc(lignesAudit(t, dossierJournalAudit()), func(l LigneAudit) bool { return l.Action != "premier déploiement" || l.Projet != p.Nom })
	if len(audits) != 1 || !strings.HasPrefix(audits[0].Resultat, r.Premier) {
		t.Fatalf("audit: %+v", audits)
	}
	// the task ended without deploying: to prepare, said calmly, and not launched again at each look
	s.avancerRelais(p, time.Now())
	calme(p, "a_preparer")
	s.avancerRelais(p, time.Now())
	s.noterLivraison(p, time.Now())
	if n := len(briefs()); n != 1 {
		t.Fatalf("launched again without a new delivery: %d tasks", n)
	}
	// the next delivery tries again; this task deploys: the relay shows its deployment
	c3 := commitRelaisTest(t, p, "page.html")
	deployer.Store(p.Nom, c3)
	livrerTest(t, p, "2")
	s.noterLivraison(p, time.Now())
	s.avancerRelais(p, time.Now())
	if b = briefs(); len(b) != 2 || !strings.Contains(b[1], c3) {
		t.Fatalf("the next delivery: %q", b)
	}
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		t.Fatal("deployed again what the task deployed")
	}
	if r = lireRelais()[p.Nom]; r.Etat != "lance" || r.ID != "d-m284" {
		t.Fatalf("after the task's deployment: %+v", r)
	}
	calme(p, "ok")
	deployer.Delete(p.Nom)

	// native Windows mode: the task cannot start alone: to prepare, never a failure; the route proposes WSL
	env.URL = "https://m284n.example.fr" // one host name per deployment
	pn, _ := projetRelaisTest(t, s, "m284n", env)
	s.noterLivraison(pn, time.Now())
	livrerTest(t, pn, "1")
	s.noterLivraison(pn, time.Now())
	modeNatif.Store(true)
	s.avancerRelais(pn, time.Now())
	calme(pn, "a_preparer")
	if code, v := appel("/api/projets/m284n/preparer-deploiement", map[string]any{}); code != http.StatusConflict || v["proposer_wsl"] != true {
		t.Fatalf("native mode, the route: %d %v", code, v)
	}
	if code, _ := appel("/api/projets/m284n/preparer-deploiement", map[string]any{"forcer": true, "mot_de_passe": "faux"}); code == http.StatusOK {
		t.Fatal("forced with a wrong password")
	}
	modeNatif.Store(false)
	if n := len(briefs()); n != 2 {
		t.Fatalf("native mode: %d tasks", n)
	}

	// an older version's failure for want of a method: taken up by the same path, never shown in red
	envJalon := env
	envJalon.URL, envJalon.Moment, envJalon.DeployerAuJalon = "https://m284v.example.fr", "jalon", true
	pa, ca := projetRelaisTest(t, s, "m284v", envJalon)
	ecrireRelais(pa.Nom, RelaisJalon{Jalon: "M1 : Un", Commit: ca, Date: time.Now().UTC().Format(time.RFC3339), Etat: "echec",
		Erreur: "this environment has no deployment method yet: a first deployment is made by a Servers task"})
	if v := s.etatRelais(pa.Nom); v == nil || v.Etat != "a_preparer" || v.Erreur != "" || len(s.alertesRelais()) != 0 {
		t.Fatalf("an older failure, shown: %+v, bell %+v", v, s.alertesRelais())
	}
	s.avancerRelais(pa, time.Now())
	calme(pa, "preparation")
	if b = briefs(); len(b) != 3 || !strings.Contains(b[2], "project m284v ") {
		t.Fatalf("an older failure taken up: %d tasks", len(b))
	}

	// (a) no server: nothing tried, noted nor said
	sans := envJalon
	sans.Serveurs, sans.URL = nil, ""
	ps, _ := projetRelaisTest(t, s, "m284s", Environnement{Protection: "aucune", Politique: "accord"})
	if err := s.acces.definirAncien(ps.Nom, "site", sans); err != nil {
		t.Logf("a moment without a server is refused (%v): the deployment stays empty", err)
	}
	s.noterJalonRelais(ps, "M1 : Un", time.Now())
	s.noterLivraison(ps, time.Now())
	livrerTest(t, ps, "1")
	s.noterLivraison(ps, time.Now())
	s.avancerRelais(ps, time.Now())
	if r, ok := lireRelais()[ps.Nom]; ok || s.etatRelais(ps.Nom) != nil {
		t.Fatalf("without a server: %+v, shown %+v", r, s.etatRelais(ps.Nom))
	}
	if code, v := appel("/api/projets/m284s/preparer-deploiement", map[string]any{}); code != http.StatusBadRequest || v["erreur"] != errRienAPreparer.Error() {
		t.Fatalf("nothing to prepare: %d %v", code, v)
	}

	// (c) an older project deployed on request, without a method: an information, nothing launched alone
	demande := env
	demande.URL, demande.Moment = "https://m284c.example.fr", ""
	pc, cc := projetRelaisTest(t, s, "m284c", demande)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(pc, "M1 : Un", time.Now())
	s.noterLivraison(pc, time.Now())
	livrerTest(t, pc, "1")
	s.noterLivraison(pc, time.Now())
	s.avancerRelais(pc, time.Now())
	calme(pc, "a_preparer")
	if n := len(briefs()); n != 3 {
		t.Fatalf("on request, launched alone: %d tasks", n)
	}
	code, v := appel("/api/projets/m284c/preparer-deploiement", map[string]any{})
	tache, _ := v["tache"].(map[string]any)
	if code != http.StatusOK || tache["id"] == nil || tache["id"] == "" {
		t.Fatalf("Prepare now: %d %v", code, v)
	}
	select {
	case <-bloque:
	case <-time.After(30 * time.Second):
		t.Fatal("the owner's task did not start")
	}
	calme(pc, "preparation")
	if r = lireRelais()[pc.Nom]; r.Premier != tache["id"] || r.Commit != cc {
		t.Fatalf("Prepare now, the record: %+v", r)
	}
	s.avancerRelais(pc, time.Now()) // at work: followed, nothing else
	if code, v2 := appel("/api/projets/m284c/preparer-deploiement", map[string]any{}); code != http.StatusOK || v2["tache"].(map[string]any)["id"] != tache["id"] {
		t.Fatalf("Prepare now twice: %d %v", code, v2)
	}
	deployer.Store(pc.Nom, cc)
	close(bloquer)
	if n := len(briefs()); n != 4 {
		t.Fatalf("Prepare now: %d tasks", n)
	}
	s.avancerRelais(pc, time.Now())
	if r = lireRelais()[pc.Nom]; r.Etat != "lance" || r.ID != "d-m284c" {
		t.Fatalf("Prepare now, after the task's deployment: %+v", r)
	}
	if code, v := appel("/api/projets/m284c/preparer-deploiement", map[string]any{}); code != http.StatusBadRequest || v["erreur"] != errRienAPreparer.Error() {
		t.Fatalf("already deployed: %d %v", code, v)
	}
	if code, _ := appel("/api/projets/inconnu/preparer-deploiement", map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("unknown project: %d", code)
	}
}
