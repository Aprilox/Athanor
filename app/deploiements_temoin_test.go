package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestSiteExempleTemoinConteneur (M140): the sample site end to end on the test container. The server is added
// by its password; dev is put behind the fake Cloudflare (exact calls) and gets v1 by an accepted plan of the
// Servers agent, then v2 by the Dev agent (policy auto); the owner goes back to v1, checked again. Prod: no
// deployment without the owner's accord (the plan, the tool and the Dev request all wait), then accorded. A
// simulated outage of dev → the bell with its three actions, Retry serves v1 again. No witness (Cloudflare
// token, server key, deployment key, proxy password, .env value) in anything an agent or the owner can read.
func TestSiteExempleTemoinConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	debut := time.Now()
	s, appelAPI, id, _, sur := serveurConteneurTest(t) // the server added by its password
	var err error
	dossierAppr := t.TempDir()
	if s.approbations, err = ouvrirApprobations(dossierAppr); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(fichierSanteEnvs())
	t.Cleanup(func() { _ = os.Remove(fichierSanteEnvs()) })
	const projet, hoteDev, hoteProd = "exemple", "dev.exemple.fr", "www.exemple.fr"
	depot, c1, c2 := depotSiteExemple(t)
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: projet, Dossier: depot})
	s.mu.Unlock()
	resoudreTest(t, hoteDev+":80", web)
	resoudreTest(t, hoteProd+":80", web)
	nettoyer := "sudo -n rm -rf /srv/athanor/exemple-dev /srv/athanor/exemple-prod /etc/caddy/sites/" + hoteDev + ".caddy /etc/caddy/sites/" + hoteProd +
		".caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	sur("ssh_commande", map[string]any{"commande": nettoyer})
	t.Cleanup(func() { sur("ssh_commande", map[string]any{"commande": nettoyer}) })

	// the provider, the environments (through the owner's API), the deployment key, a .env value, the proxy password
	const jetonCF = "cf-jeton-TEMOIN-m140-5d2e"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	tous := []string{"dns:lire", "dns:écrire", "ssl", "certificats", "access"}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "f1", Type: "cloudflare", Nom: "CF", Cle: cle.ID, Etat: "ok", Compte: "acc1", Droits: tous,
			Zones: []ZoneFournisseur{{ID: "z1", Nom: "exemple.fr", Droits: tous}}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		for i := range l {
			l[i].Roles = []string{"dev", "staging"}
		}
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, nom := range []string{"dev", "staging"} { // M281: an older project, with several environments
		if err := s.acces.definirAncien(projet, nom, envDefaut()); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []map[string]any{
		{"env": "dev", "url": "http://" + hoteDev, "serveurs": []string{id}, "protection": "aucune", "politique": "auto", "fournisseur": "f1"},
		{"env": "staging", "url": "https://staging.exemple.fr", "serveurs": []string{id}, "protection": "auth_proxy", "politique": "accord", "fournisseur": "f1"},
	} {
		if code, v := appelAPI("/api/projets/"+projet+"/environnements", e); code != 200 {
			t.Fatalf("environment %v: %d %v", e["env"], code, v)
		}
	}
	// shortcut: one container for every environment, while the API keeps a prod server for prod only (tested in
	// hub_environnements_test.go); prod is written beside it, as the owner's panel would with a second server
	if err := s.acces.definirAncien(projet, "prod", Environnement{URL: "http://" + hoteProd, Serveurs: []string{id}, Protection: "aucune", Politique: "accord", Fournisseur: "f1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.blocCaddyEnv(projet, "staging", id, "127.0.0.1:8080"); err != nil { // makes the proxy password
		t.Fatal(err)
	}
	ancienGH := executerCleDeploiement
	t.Cleanup(func() { executerCleDeploiement = ancienGH })
	executerCleDeploiement = func(*Serveur, Projet, ...string) (string, error) { return `{"id":"78","depot":"moi/exemple"}`, nil }
	if code, v := appelAPI("/api/projets/"+projet+"/cle-deploiement", nil); code != 200 {
		t.Fatalf("deployment key: %d %v", code, v)
	}
	ev, err := s.coffre.Ajouter(EntreeCoffre{Nom: "DB exemple", Famille: "secret", Type: "jeton", Portee: "code"}, "temoin-env-m140-83a1")
	if err == nil {
		_, err = s.acces.Ajouter(projet, ev, "prod", "DB_PASSWORD", "")
	}
	if err != nil {
		t.Fatal(err)
	}

	_, relever := fauxCloudflareComplet(t, jetonCF)
	mux := http.NewServeMux()
	s.routes(mux)
	vu := &strings.Builder{} // everything the agents received
	appel := func(j, nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, "Bearer "+j, appelOutil(nom, args))
		if rep.Error != nil {
			vu.WriteString(rep.Error.Message)
			return rep.Error.Message, true
		}
		vu.WriteString(rep.Result.Content[0].Text)
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	jeton := func(x SessionHub) string {
		t.Helper()
		j, err := s.jetons.CreerSession(x)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	planAccepte := func(j, args string) {
		t.Helper()
		txt, e := appel(j, "proposer_plan", args)
		var p struct{ Plan string }
		if e || json.Unmarshal([]byte(txt), &p) != nil || p.Plan == "" {
			t.Fatalf("plan: %v %q", e, txt)
		}
		if _, _, err := s.deciderApprobation("fenetre", p.Plan, "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	page := func(hote string) string {
		t.Helper()
		r, _ := http.NewRequest("GET", "http://"+web+"/", nil)
		r.Host = hote
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Body.Close()
		b, _ := io.ReadAll(rep.Body)
		return string(b)
	}
	dev := jeton(SessionHub{Projet: projet, Mission: "M1", Agent: "pilote", Outils: outilsPilote})
	suivre := func(txt string) string { // demander_deploiement's answer → the final state of the deployment
		t.Helper()
		idDep, _, ok := strings.Cut(strings.TrimPrefix(txt, "deployment "), " ")
		if !ok {
			t.Fatalf("demander_deploiement: %q", txt)
		}
		for range 240 {
			if txt, e := appel(dev, "deploiement_etat", `{"id":"`+idDep+`"}`); e || !strings.Contains(txt, `"etat": "en_cours"`) {
				return txt
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("deployment %s never ended", idDep)
		return ""
	}

	// 1. dev: the DNS record and v1, one accepted plan of the Servers agent
	ops := jeton(SessionHub{Projet: projet, Mission: "t1", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{id}, Env: projet + "/dev", Hotes: []string{hoteDev}})
	const dns = `{"nom":"dev.exemple.fr","type":"A","contenu":"198.51.100.7","proxied":true}`
	dep := `{"env":"dev","methode":"statique","source":"artefact","dossier":"site","commit":"` + c1 + `"}`
	if txt, e := appel(ops, "dns_ecrire", dns); !e || !strings.Contains(txt, "plan") {
		t.Errorf("DNS without a plan: %v %q", e, txt)
	}
	planAccepte(ops, `{"serveur":"`+id+`","resume":"Mettre dev en ligne","etapes":[{"outil":"dns_ecrire","arguments":`+dns+`,"pourquoi":"p"},{"outil":"deployer","arguments":`+dep+`,"pourquoi":"p"}]}`)
	if txt, e := appel(ops, "dns_ecrire", dns); e {
		t.Fatalf("dns_ecrire: %s", txt)
	}
	if got, want := relever(), []string{
		"GET /zones/z1/dns_records?name=dev.exemple.fr&per_page=100&type=A",
		`POST /zones/z1/dns_records {"content":"198.51.100.7","name":"dev.exemple.fr","proxied":true,"ttl":1,"type":"A"}`,
	}; !slices.Equal(got, want) {
		t.Errorf("Cloudflare calls for dev:\n got  %q\n want %q", got, want)
	}
	if txt, e := appel(ops, "deployer", dep); e || !strings.Contains(txt, `"etat": "ok"`) || !strings.Contains(page(hoteDev), "site-exemple v1") {
		t.Fatalf("v1 in dev: %v %s", e, txt)
	}

	// 2. v2 by the Dev agent: dev is in auto, the method recorded by the first deployment
	txt, _ := appel(dev, "demander_deploiement", `{"environnement":"dev","commit":"`+c2+`"}`)
	if etat := suivre(txt); !strings.Contains(etat, `"etat": "ok"`) || !strings.Contains(etat, c2) || !strings.Contains(page(hoteDev), "site-exemple v2") {
		t.Fatalf("v2 in dev: %s", etat)
	}

	// 3. the owner goes back to the version before (v1), checked, then checked again by the health check
	code, v := appelAPI("/api/projets/"+projet+"/revenir", map[string]any{"env": "dev"})
	if d, _ := v["deploiement"].(map[string]any); code != 200 || d["etat"] != "ok" || d["commit"] != c1 || !strings.Contains(page(hoteDev), "site-exemple v1") {
		t.Fatalf("back to v1: %d %v", code, v)
	}
	ctx := context.Background()
	s.recontroler(ctx, projet, "dev")
	if e := lireSante().Envs[projet+"/dev"]; e.Etat != "ok" || e.Verification == nil || !e.Verification.OK {
		t.Errorf("v1 checked again: %s", enJSON(e))
	}

	// 4. prod: neither the plan, nor the tool, nor the Dev agent deploys without the owner
	prod := jeton(SessionHub{Projet: projet, Mission: "t2", Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{id}, Env: projet + "/prod", Hotes: []string{hoteProd}})
	depProd := `{"env":"prod","methode":"statique","source":"artefact","dossier":"site","commit":"` + c2 + `"}`
	if txt, e := appel(dev, "demander_deploiement", `{"environnement":"prod","commit":"`+c2+`"}`); !e || !strings.Contains(txt, "has no deployment method") {
		t.Errorf("Dev request before prod's first deployment: %v %s", e, txt)
	}
	txt, e := appel(prod, "proposer_plan", `{"serveur":"`+id+`","resume":"Mise en prod","etapes":[{"outil":"deployer","arguments":`+depProd+`,"pourquoi":"p"}]}`)
	var plan struct{ Plan string }
	if e || !strings.Contains(txt, `"etat": "attente"`) || json.Unmarshal([]byte(txt), &plan) != nil {
		t.Fatalf("prod plan: %v %s", e, txt)
	}
	if txt, e := appel(prod, "deployer", depProd); !e || !strings.Contains(txt, "waits for the owner") {
		t.Errorf("prod before the accord: %v %s", e, txt)
	}
	if txt, e := appel(dev, "deployer", depProd); !e {
		t.Errorf("a Dev token called deployer: %s", txt)
	}
	horsProd := func(quand string) {
		t.Helper()
		if h := historiqueDeploiements(projet, "prod"); len(h) != 0 || strings.Contains(page(hoteProd), "site-exemple") ||
			sur("ssh_commande", map[string]any{"commande": "test -e /srv/athanor/exemple-prod && echo present; true"}).Sortie != "" {
			t.Fatalf("prod deployed %s: %s", quand, enJSON(h))
		}
	}
	horsProd("before the accord")
	if _, _, err := s.deciderApprobation("fenetre", plan.Plan, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if txt, e := appel(prod, "deployer", depProd); e || !strings.Contains(txt, `"etat": "ok"`) || !strings.Contains(page(hoteProd), "site-exemple v2") {
		t.Fatalf("v2 in prod once accorded: %v %s", e, txt)
	}
	// the Dev agent's request for prod waits for the owner's accord, then runs once
	txt, _ = appel(dev, "demander_deploiement", `{"environnement":"prod","commit":"`+c1+`"}`)
	idAccord, ok := strings.CutPrefix(txt, "waiting for the owner, id ")
	if idAccord, _, _ = strings.Cut(idAccord, ":"); !ok || len(historiqueDeploiements(projet, "prod")) != 1 || !strings.Contains(page(hoteProd), "site-exemple v2") {
		t.Fatalf("Dev request for prod: %q", txt)
	}
	if _, _, err := s.deciderApprobation("fenetre", idAccord, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	txt, _ = appel(dev, "demander_deploiement", `{"environnement":"prod","commit":"`+c1+`"}`)
	if etat := suivre(txt); !strings.Contains(txt, "the owner's accord") || !strings.Contains(etat, `"etat": "ok"`) || !strings.Contains(page(hoteProd), "site-exemple v1") {
		t.Fatalf("accorded Dev request for prod: %q %s", txt, etat)
	}
	if got := relever(); len(got) > 0 {
		t.Errorf("Cloudflare calls after dev's plan: %q", got)
	}

	// 5. a simulated outage of dev: red twice → the bell with its three actions; Retry serves v1 again
	resolutionsTest[hoteDev+":80"] = "127.0.0.1:1"
	s.controlerSante(ctx)
	s.controlerSante(ctx)
	var alerte *AlerteHub
	for _, a := range s.alertesIncidents() {
		if a.Element == projet+"/dev" {
			alerte = &a
		}
	}
	if alerte == nil || len(alerte.Actions) != 3 || alerte.Actions[0].ID != "reessayer" || alerte.Actions[1].ID != "revenir" || alerte.Actions[2].ID != "main" {
		t.Fatalf("the bell: %s", enJSON(s.alertesIncidents()))
	}
	if i := s.incidentEnv(lireSante(), projet, "dev"); i == nil || i.Source != "sante" || i.Commit != c1 {
		t.Fatalf("incident: %s", enJSON(i))
	}
	resolutionsTest[hoteDev+":80"] = web
	if code, v := appelAPI("/api/"+alerte.Actions[0].Route, alerte.Actions[0].Corps); code != 200 || s.incidentEnv(lireSante(), projet, "dev") != nil || !strings.Contains(page(hoteDev), "site-exemple v1") {
		t.Fatalf("Retry: %d %v", code, v)
	}

	// the witnesses: nowhere an agent, the owner or a device can read
	temoins := append(valeursCoffreTemoins(t, s), jetonCF, temoinOutils)
	if len(temoins) < 6 {
		t.Fatalf("witnesses missing (token, server key, deployment key, proxy password, .env value): %d", len(temoins))
	}
	cherche := func(ou, texte string) {
		t.Helper()
		for _, v := range temoins {
			if strings.Contains(texte, v) {
				t.Errorf("a witness in %s: %.40q", ou, v)
			}
		}
	}
	cherche("an MCP answer", vu.String())
	cherche("a session's environment", strings.Join(envSansSecrets(s, nil), "\n"))
	cherche("the bell", enJSON(alerte)+enJSON((moduleServeurs{}).Elements(s)))
	l, _ := s.approbations.Liste()
	for _, ap := range l {
		cherche("a notification", enJSON(s.messageApprobation(ap)))
	}
	reponses := &strings.Builder{}
	for _, c := range []string{"/api/projets/" + projet + "/environnements", "/api/serveurs/" + id, "/api/hub/approbations", "/api/fournisseurs"} {
		if code := lireAPI(t, s, c, reponses); code != 200 {
			t.Errorf("GET %s: %d", c, code)
		}
	}
	cherche("an API answer", reponses.String())
	cherche("the audit log", journalAuditTexte(t))
	for _, d := range []string{dossierModule("servers"), dossierModule("hub"), dossierAppr, dossierJournalAudit(), dossierAthanor()} {
		if f := fichiersAvec(t, d, temoins, filepath.Base(os.Args[0])); len(f) > 0 {
			t.Errorf("a witness in files of %s: %v", d, f)
		}
	}
	if d := time.Since(debut); d > 120*time.Second {
		t.Errorf("scenario in %s (bound 120 s)", d)
	}
}
