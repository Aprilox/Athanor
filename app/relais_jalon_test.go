package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// planRelaisTest: the project's PLAN.md with one milestone, ticked or not.
func planRelaisTest(t *testing.T, p Projet, lignes string) {
	t.Helper()
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Plan(), []byte("# Plan\n\n"+lignes), 0o644); err != nil {
		t.Fatal(err)
	}
}

func oublierRelais(projet string) { deplacerRelais(projet, "") }

// serveurRelaisTest: a server the inventory knows (an unknown one always asks the owner, prudenceRenforcee) and
// that refuses the connection at once.
func serveurRelaisTest(t *testing.T) string {
	t.Helper()
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		if slices.ContainsFunc(l, func(v ServeurInventaire) bool { return v.ID == "r1" }) {
			return l, nil
		}
		return append(l, ServeurInventaire{ID: "r1", Nom: "relais", Hote: "127.0.0.1", Port: 1, Cree: time.Now()}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return "r1"
}

// TestRelaisJalon (M136): only dev takes the relay; a milestone done is relayed once (not again, not after a
// restart); a git source waits for the push, then tries anyway. M284, without a method, its three cases, none of
// them a failure (no error text, no bell): no server, nothing noted; a server and a moment, the first deployment's
// task; on request, "a_preparer" and nothing launched.
func TestRelaisJalon(t *testing.T) {
	for _, c := range []struct {
		nom string
		e   Environnement
	}{{"prod", Environnement{}}, {"site", Environnement{Production: true}}} { // M246: never a Production environment
		c.e.URL, c.e.DeployerAuJalon = "https://x.test", true
		if _, err := validerEnvironnement(c.nom, c.e, nil); err != errEnvRelais {
			t.Errorf("%s with the relay: %v", c.nom, err)
		}
	}
	for _, env := range []string{"dev", "staging", "demo.exemple.fr"} {
		if e, err := validerEnvironnement(env, Environnement{URL: "https://x.test", DeployerAuJalon: true}, nil); err != nil || !e.DeployerAuJalon {
			t.Errorf("%s with the relay: %v %+v", env, err, e)
		}
	}

	s, _, _ := serveursAjoutTest(t)
	briefs := chefOpsTest(t, nil) // the first deployment's task: an ops lead that deploys nothing
	depot, _, c2 := depotSiteExemple(t)
	p := Projet{Nom: "m136u", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	oublierRelais(p.Nom)
	t.Cleanup(func() { oublierRelais(p.Nom) })
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	env := Environnement{URL: "http://m136u.test", Serveurs: []string{serveurRelaisTest(t)}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true}
	calme := func(cas, etat string) {
		t.Helper()
		r, v := lireRelais()[p.Nom], s.etatRelais(p.Nom)
		if r.Erreur != "" || len(s.alertesRelais()) != 0 || (v == nil) != (etat == "") || v != nil && (v.Etat != etat || v.Erreur != "") {
			t.Fatalf("%s: wanted %q without an error nor a bell: record %+v, shown %+v, bell %+v", cas, etat, r, v, s.alertesRelais())
		}
	}
	// (a) no server: a milestone done is not even noted
	if err := s.acces.definirAncien(p.Nom, "dev", Environnement{Protection: "aucune", Politique: "accord"}); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(p, "M0 : Avant", time.Now())
	s.avancerRelais(p, time.Now())
	if _, ok := lireRelais()[p.Nom]; ok {
		t.Fatalf("(a) noted without a server: %+v", lireRelais()[p.Nom])
	}
	calme("(a)", "")
	// (c) a server, on request: an information, nothing launched alone
	env.DeployerAuJalon = false
	if err := s.acces.definirAncien(p.Nom, "dev", env); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(p, "M0 : Avant", time.Now())
	s.avancerRelais(p, time.Now())
	if _, ok := lireRelais()[p.Nom]; ok || len(briefs()) != 0 {
		t.Fatalf("(c) on request: %+v, %d tasks", lireRelais()[p.Nom], len(briefs()))
	}
	calme("(c)", "a_preparer")
	// (b) a server and a moment: below
	env.DeployerAuJalon = true
	if err := s.acces.definirAncien(p.Nom, "dev", env); err != nil {
		t.Fatal(err)
	}
	maintenant, faits, f := time.Now(), map[string]map[string]bool{}, filepath.Join(t.TempDir(), "jalons.json")
	planRelaisTest(t, p, "- [ ] M1 : Un\n- [ ] M2 : Deux\n")
	if n := observerPlan(p, faits, f, maintenant); len(n) != 0 { // the first look learns
		t.Fatalf("first look: %+v", n)
	}
	planRelaisTest(t, p, "- [x] M1 : Un\n- [ ] M2 : Deux\n")
	n := observerPlan(p, faits, f, maintenant)
	if len(n) != 1 {
		t.Fatalf("milestone done: %+v", n)
	}
	s.noterJalonRelais(p, n[0].Titre, maintenant)
	if r := lireRelais()[p.Nom]; r.Etat != "attente" || r.Commit != c2 || r.Jalon != n[0].Titre {
		t.Fatalf("recorded: %+v", r)
	}
	if ch := s.avancerRelais(p, maintenant); ch != nil {
		t.Fatal("started without a method")
	}
	calme("(b)", "preparation")
	if b := briefs(); len(b) != 1 || !strings.Contains(b[0], prefixePremierDeploiement+" of project m136u (") || !strings.Contains(b[0], c2) {
		t.Fatalf("(b) the first deployment's task: %q", b)
	}
	s.avancerRelais(p, maintenant) // that task deployed nothing: to prepare, still no failure
	calme("(b) after its task", "a_preparer")
	// the same milestone seen again, and a restart (a new observer): nothing relayed again
	s.noterJalonRelais(p, n[0].Titre, maintenant)
	s.avancerRelais(p, maintenant)
	if r := lireRelais()[p.Nom]; r.Etat != "a_preparer" || len(briefs()) != 1 {
		t.Errorf("relayed twice: %+v", r)
	}
	if n := observerPlan(p, map[string]map[string]bool{}, f, maintenant); len(n) != 0 {
		t.Errorf("after a restart: %+v", n)
	}
	// git source: waits for the commit on a remote branch, then tries anyway (and fails clearly: no key)
	if err := s.acces.NoterDeploiement(p.Nom, "dev", ConfigDeploiement{Methode: "statique", Source: "git"}); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(p, "M2 : Deux", maintenant)
	if ch := s.avancerRelais(p, maintenant.Add(time.Minute)); ch != nil || lireRelais()[p.Nom].Etat != "attente" {
		t.Fatalf("not pushed yet: %+v", lireRelais()[p.Nom])
	}
	s.avancerRelais(p, maintenant.Add(attentePushRelais+time.Minute))
	r := s.etatRelais(p.Nom)
	if r == nil || r.Etat != "echec" || !strings.Contains(r.Erreur, "deployment key") {
		t.Fatalf("after the wait: %+v", r)
	}
	// a real failure still rings
	al := s.alertesRelais()
	if len(al) != 1 || al[0].Element != p.Nom || al[0].Actions[0].Page != "acces" || al[0].Args[0] != "M2 : Deux" {
		t.Fatalf("bell: %+v", al)
	}
	actives, _ := s.alertesHub(nil)
	if !strings.Contains(enJSON(actives), al[0].Empreinte) {
		t.Errorf("not in the bell: %s", enJSON(actives))
	}
	// relay off: a milestone is not recorded, the state is not shown, the bell is quiet
	env.DeployerAuJalon = false
	if err := s.acces.definirAncien(p.Nom, "dev", env); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(p, "M3 : Trois", maintenant)
	if r := lireRelais()[p.Nom]; r.Jalon != "M2 : Deux" || s.etatRelais(p.Nom) != nil || len(s.alertesRelais()) != 0 {
		t.Errorf("relay off: %+v", r)
	}
}

// TestRelaisJalonConteneur (M136): a milestone ticked → its HEAD deployed to dev on the test container with the
// recorded method, without an accord (the box is the owner's), checked; nothing on prod. M284: dev has no method
// at first: the milestone launches the first deployment's task, whose ops lead (a scripted Claude through the real
// MCP route, the project in mode ignorer) deploys the commit before; the relay then deploys the milestone's HEAD
// itself with the method that task chose, and nothing is ever said as a failure.
func TestRelaisJalonConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	depot, c1, c2 := depotSiteExemple(t)
	p := Projet{Nom: "m136", Dossier: depot}
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
	t.Cleanup(func() {
		oublierRelais(p.Nom)
		deplacerReglagesProjet(p.Nom, "")
	})
	briefs, sorties := chefOpsDeployeur(t, mux, id, "dev", c1)
	const hote, hoteProd = "m136.test", "m136p.test"
	resoudreTest(t, hote+":80", web)
	for env, e := range map[string]Environnement{"dev": {URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true},
		"prod": {URL: "http://" + hoteProd, Serveurs: []string{id}, Protection: "aucune", Politique: "accord"}} {
		if err := s.acces.definirAncien(p.Nom, env, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.acces.NoterDeploiement(p.Nom, "prod", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}); err != nil {
		t.Fatal(err)
	}
	nettoyer := "sudo -n rm -rf /srv/athanor/m136-dev /srv/athanor/m136-prod /etc/caddy/sites/" + hote + ".caddy /etc/caddy/sites/" + hoteProd + ".caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	appel("ssh_commande", map[string]any{"commande": nettoyer})
	t.Cleanup(func() { appel("ssh_commande", map[string]any{"commande": nettoyer}) })

	faits, f := map[string]map[string]bool{}, filepath.Join(t.TempDir(), "jalons.json")
	planRelaisTest(t, p, "- [ ] M1 : Le site\n")
	observerPlan(p, faits, f, time.Now())
	planRelaisTest(t, p, "- [x] M1 : Le site\n")
	for _, j := range observerPlan(p, faits, f, time.Now()) {
		s.noterJalonRelais(p, j.Titre, time.Now())
	}
	// no method yet: the first deployment's task, never a failure
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		t.Fatal("deployed without a method")
	}
	if r := s.etatRelais(p.Nom); r == nil || r.Etat != "preparation" || r.Erreur != "" || len(s.alertesRelais()) != 0 {
		t.Fatalf("the first deployment: %+v, bell %+v", r, s.alertesRelais())
	}
	if b := briefs(); len(b) != 2 || !strings.Contains(b[0], prefixePremierDeploiement+" of project m136 (") || len(*sorties) != 2 || !strings.Contains((*sorties)[1], `"etat": "ok"`) {
		t.Fatalf("the first deployment's task: %d sessions, deployer: %q", len(b), *sorties)
	}
	if h := historiqueDeploiements(p.Nom, "dev"); len(h) != 1 || h[0].Commit != c1 || h[0].Etat != "ok" || s.acces.Environnements(p.Nom)["dev"].Deploiement == nil {
		t.Fatalf("after the first deployment: %s", enJSON(h))
	}
	// the milestone's HEAD is not what that task deployed: the relay deploys it with the method now known
	s.avancerRelais(p, time.Now())
	if r := lireRelais()[p.Nom]; r.Etat != "attente" || r.Commit != c2 || r.Erreur != "" {
		t.Fatalf("after the task: %+v", r)
	}
	ch := s.avancerRelais(p, time.Now())
	if ch == nil {
		t.Fatalf("not started: %+v", lireRelais()[p.Nom])
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Minute):
		t.Fatal("the relay's deployment did not end")
	}
	r := s.etatRelais(p.Nom)
	if r == nil || r.Etat != "ok" || r.Commit != c2 {
		t.Fatalf("relay: %+v", r)
	}
	h := historiqueDeploiements(p.Nom, "dev")
	if len(h) != 2 || h[1].Auteur != "relais" || h[1].Verification == nil || !h[1].Verification.OK || len(s.alertesRelais()) != 0 {
		t.Fatalf("dev history: %s", enJSON(h))
	}
	req, _ := http.NewRequest("GET", "http://"+web+"/", nil)
	req.Host = hote
	rep, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rep.Body)
	rep.Body.Close()
	if !strings.Contains(string(b), "site-exemple v2") {
		t.Errorf("served: %.200q", b)
	}
	if h := historiqueDeploiements(p.Nom, "prod"); len(h) != 0 {
		t.Errorf("prod deployed: %s", enJSON(h))
	}
	if ch := s.avancerRelais(p, time.Now()); ch != nil || len(historiqueDeploiements(p.Nom, "dev")) != 2 {
		t.Error("deployed twice")
	}
}
