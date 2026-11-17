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
// restart); without a method it fails into the bell; a git source waits for the push, then tries anyway.
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
	depot, _, c2 := depotSiteExemple(t)
	p := Projet{Nom: "m136u", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	oublierRelais(p.Nom)
	t.Cleanup(func() { oublierRelais(p.Nom) })
	env := Environnement{URL: "http://m136u.test", Serveurs: []string{serveurRelaisTest(t)}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true}
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
	r := s.etatRelais(p.Nom)
	if r == nil || r.Etat != "echec" || !strings.Contains(r.Erreur, "no deployment method") {
		t.Fatalf("without a method: %+v", r)
	}
	al := s.alertesRelais()
	if len(al) != 1 || al[0].Element != p.Nom || al[0].Actions[0].Page != "acces" || al[0].Args[0] != n[0].Titre {
		t.Fatalf("bell: %+v", al)
	}
	actives, _ := s.alertesHub(nil)
	if !strings.Contains(enJSON(actives), al[0].Empreinte) {
		t.Errorf("not in the bell: %s", enJSON(actives))
	}
	// the same milestone seen again, and a restart (a new observer): nothing relayed again
	s.noterJalonRelais(p, n[0].Titre, maintenant)
	if r := lireRelais()[p.Nom]; r.Etat != "echec" {
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
	if r := s.etatRelais(p.Nom); r.Etat != "echec" || !strings.Contains(r.Erreur, "deployment key") {
		t.Errorf("after the wait: %+v", r)
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
// recorded method, without an accord (the box is the owner's), checked; nothing on prod.
func TestRelaisJalonConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	depot, _, c2 := depotSiteExemple(t)
	p := Projet{Nom: "m136", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	oublierRelais(p.Nom)
	t.Cleanup(func() { oublierRelais(p.Nom) })
	const hote, hoteProd = "m136.test", "m136p.test"
	resoudreTest(t, hote+":80", web)
	for env, e := range map[string]Environnement{"dev": {URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true},
		"prod": {URL: "http://" + hoteProd, Serveurs: []string{id}, Protection: "aucune", Politique: "accord"}} {
		if err := s.acces.definirAncien(p.Nom, env, e); err != nil {
			t.Fatal(err)
		}
		if err := s.acces.NoterDeploiement(p.Nom, env, ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}); err != nil {
			t.Fatal(err)
		}
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
	if len(h) != 1 || h[0].Auteur != "relais" || h[0].Verification == nil || !h[0].Verification.OK {
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
	if ch := s.avancerRelais(p, time.Now()); ch != nil || len(historiqueDeploiements(p.Nom, "dev")) != 1 {
		t.Error("deployed twice")
	}
}
