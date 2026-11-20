package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestConfigDeploiementProxy: the web server of a deployment: Caddy by default, "aucun" for another one the plan
// names (statique and compose only); anything else refused.
func TestConfigDeploiementProxy(t *testing.T) {
	for _, x := range []struct {
		c  ConfigDeploiement
		ok bool
	}{
		{ConfigDeploiement{Methode: "statique", Source: "artefact"}, true},
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Proxy: "caddy"}, true},
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Proxy: "aucun"}, true},
		{ConfigDeploiement{Methode: "compose", Source: "git", Proxy: "aucun"}, true},
		{ConfigDeploiement{Methode: "k3s", Source: "artefact", Proxy: "aucun"}, false},
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Proxy: "nginx"}, false},
	} {
		if err := validerConfigDeploiement(x.c); (err == nil) != x.ok {
			t.Errorf("%+v: %v", x.c, err)
		}
	}
	if !strings.Contains(briefPremierDeploiement("https://a.test", "", Projet{Nom: "a"}, strings.Repeat("a", 40)), "proxy aucun") {
		t.Error("the first deployment's brief does not say how to keep the plan's web server")
	}
}

// TestDeploiementSansProxyConteneur: proxy aucun (the plan names nginx…): Athanor writes no Caddy site, only the
// releases and current; the web server set up once by hand (here a site written as the agent would write nginx's)
// serves each new version, the recorded method included, and going back too.
func TestDeploiementSansProxyConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	depot, c1, c2 := depotSiteExemple(t)
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m131n", Dossier: depot})
	s.mu.Unlock()
	const hote, base = "m131n.test", "/srv/athanor/m131n-dev"
	resoudreTest(t, hote+":80", web)
	if err := s.acces.definirAncien("m131n", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	reload := "sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1"
	nettoyer := "sudo -n rm -rf " + base + " /etc/caddy/sites/" + hote + ".caddy /etc/caddy/sites/main-" + hote + ".caddy; " + reload + "; true"
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	sur(nettoyer)
	t.Cleanup(func() { sur(nettoyer) })
	// the other web server, set up once by hand to serve the site's current release
	sur("printf 'http://" + hote + " {\\n  root * " + base + "/current/site\\n  file_server\\n}\\n' | sudo -n tee /etc/caddy/sites/main-" + hote + ".caddy >/dev/null && " + reload)
	page := func() string {
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
	ctx := context.Background()
	d1, err := s.deployer(ctx, "m131n", "dev", c1, OptionsDeploiement{Config: &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site", Proxy: "aucun"}, Auteur: "proprietaire"})
	if err != nil || d1.Etat != "ok" || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("v1: %v %+v %.200q", err, d1, page())
	}
	if got := sur("test -e /etc/caddy/sites/" + hote + ".caddy && echo ecrit || echo aucun"); got != "aucun\n" {
		t.Errorf("Athanor wrote a Caddy site for a site served by another web server: %q", got)
	}
	d2, err := s.deployer(ctx, "m131n", "dev", c2, OptionsDeploiement{Auteur: "relais"}) // the recorded method, proxy kept
	if err != nil || !strings.Contains(page(), "site-exemple v2") {
		t.Fatalf("v2: %v %+v %.200q", err, d2, page())
	}
	if r, err := s.revenir(ctx, "m131n", "dev", "proprietaire", ""); err != nil || r.Etat != "ok" || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("back to v1: %v %+v %.200q", err, r, page())
	}
	if got := sur("test -e /etc/caddy/sites/" + hote + ".caddy && echo ecrit || echo aucun"); got != "aucun\n" {
		t.Errorf("going back wrote a Caddy site: %q", got)
	}
}
