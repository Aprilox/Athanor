package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestChoixAgentDeploiement: what the agent chooses for a deployment (its folder, its commands, a method that sends
// nothing) is checked, classed like its commands, said in the first deployment's brief and in DEPLOYMENT.md; nothing
// in the brief is Athanor's own folder.
func TestChoixAgentDeploiement(t *testing.T) {
	for _, x := range []struct {
		c  ConfigDeploiement
		ok bool
	}{
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/var/www/boulangerie", Avant: "npm ci && npm run build", Apres: "systemctl reload nginx", Proxy: "aucun"}, true},
		{ConfigDeploiement{Methode: "compose", Source: "git", Racine: "/srv/app", Apres: "docker image prune -f"}, true},
		{ConfigDeploiement{Methode: "commande", Apres: "cd /srv/app && git pull && pm2 reload app", Retour: "cd /srv/app && git checkout HEAD~1 && pm2 reload app"}, true},
		{ConfigDeploiement{Methode: "commande", Source: "artefact"}, false},                          // the update command is the method
		{ConfigDeploiement{Methode: "commande", Apres: "x", Racine: "/srv/app"}, false},              // it sends nothing
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Retour: "x"}, false},             // releases are its way back
		{ConfigDeploiement{Methode: "k3s", Source: "artefact", Apres: "x"}, false},                   // kubectl's own
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Apres: "a\nb"}, false},           // one line
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/etc/nginx"}, false},    // a system folder
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/home/deploy"}, false},  // a home itself
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/srv"}, false},          // one level
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/srv/../etc/x"}, false}, // ..
		{ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/home/deploy/apps/boulangerie"}, true},
	} {
		if err := validerConfigDeploiement(x.c); (err == nil) != x.ok {
			t.Errorf("%+v: %v", x.c, err)
		}
	}
	if b := baseDe(&ConfigDeploiement{Racine: "/var/www/site"}, "p", "site"); b != "/var/www/site" {
		t.Errorf("the agent's folder: %s", b)
	}
	if b := baseDe(&ConfigDeploiement{}, "p", "site"); b != baseDeploiement("p", "site") {
		t.Errorf("a site deployed before keeps its folder: %s", b)
	}
	// its commands are classed like any command of the agent
	brut, _ := json.Marshal(map[string]any{"methode": "statique", "source": "artefact", "commit": strings.Repeat("a", 40), "apres": "rm -rf /var/www/old"})
	if c, raison, _, err := classerDeploiement("deployer", brut); err != nil || c != ClasseDestructrice || !strings.Contains(raison, "rm -rf /var/www/old") {
		t.Errorf("apres classed: %v %v %q", c, err, raison)
	}
	// the brief: the owner's plan and the stack's conventions decide, nothing of Athanor's own
	b := briefPremierProjet(Projet{Nom: "boulangerie"}, Environnement{URL: "https://demo.a.test", Plan: "nginx sur web-1, pm2"}, "site", strings.Repeat("a", 40), true)
	for _, x := range []string{"racine", "commande", "avant", "apres", "README", "{{coffre:NAME}}", "nginx sur web-1, pm2"} {
		if !strings.Contains(b, x) {
			t.Errorf("the brief lacks %q: %s", x, b)
		}
	}
	if strings.Contains(b, "/srv/athanor") {
		t.Errorf("the brief imposes Athanor's folder: %s", b)
	}
	// DEPLOYMENT.md, written in the project when the method is recorded
	s := serveurCoffreTest(t)
	p := Projet{Nom: "boulangerie", Dossier: t.TempDir()}
	e := Environnement{URL: "https://demo.a.test", Plan: "nginx sur web-1"}
	s.ecrireDeploiementMD(p, e, ConfigDeploiement{Methode: "statique", Source: "artefact", Racine: "/var/www/boulangerie", Dossier: "dist", Avant: "npm ci", Apres: "systemctl reload nginx"}, "site")
	md, _ := os.ReadFile(filepath.Join(p.Dossier, "DEPLOYMENT.md"))
	for _, x := range []string{"https://demo.a.test", "`/var/www/boulangerie`", "`dist`", "`npm ci`", "`systemctl reload nginx`", "Put the previous version back online", "nginx sur web-1"} {
		if !strings.Contains(string(md), x) {
			t.Errorf("DEPLOYMENT.md lacks %q:\n%s", x, md)
		}
	}
	if m := texteDeploiementMD(p, e, ConfigDeploiement{Methode: "commande", Apres: "git pull", Retour: "git checkout HEAD~1"}, "site"); !strings.Contains(m, "`git checkout HEAD~1`") || strings.Contains(m, "Folder on the servers") {
		t.Errorf("DEPLOYMENT.md of commande: %s", m)
	}
}

// TestDeploiementCommande: the method that sends nothing: each server runs the update command; going back runs the
// recorded way back, and none recorded says so.
func TestDeploiementCommande(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	var mu sync.Mutex
	var cmds []string
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) {
		mu.Lock()
		defer mu.Unlock()
		cmds = append(cmds, cmd)
		return "ok\n", 0
	})
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	lances := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(cmds, "\n") }
	cfg := ConfigDeploiement{Methode: "commande", Apres: "cd /srv/app && git pull --ff-only && pm2 reload app"}
	d := Deploiement{ID: "dc1", Projet: "boulangerie", Env: "site", Serveurs: []string{srv.ID}}
	if err := s.deployerCommande(context.Background(), cfg, &d); err != nil || !d.Bascule || !strings.Contains(lances(), "git pull --ff-only && pm2 reload app") {
		t.Fatalf("the update command: %v %+v\n%s", err, d, lances())
	}
	e := Environnement{Serveurs: []string{srv.ID}, Deploiement: &cfg}
	if _, err := s.revenirCommande(context.Background(), "boulangerie", "site", "proprietaire", e); err == nil || !strings.Contains(err.Error(), "no way back") {
		t.Errorf("no way back recorded: %v", err)
	}
	cfg.Retour = "cd /srv/app && git checkout HEAD~1 && pm2 reload app"
	_, _ = s.revenirCommande(context.Background(), "boulangerie", "site", "proprietaire", e) // its check of the address fails here: the command ran
	if !strings.Contains(lances(), "git checkout HEAD~1 && pm2 reload app") {
		t.Errorf("the way back did not run:\n%s", lances())
	}
	_ = os.Remove(cheminHistorique("boulangerie", "site"))
}

// TestClesProjetDeploiement: a deployment task may put its project's keys on the servers (an .env the app reads):
// the agent is told each one's name and the variable the app reads it as, never a value.
func TestClesProjetDeploiement(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	val := "sk_temoin_" + aleatoire(8)
	e := ajouter(t, s.coffre, "Stripe boutique", "secret", "stripe", "code", val)
	if err := s.acces.modifier(func(f *fichierAcces) error {
		f.Projets["boutique"] = &ProjetAcces{Env: "dev", Acces: []AccesProjet{{Entree: e.ID, Env: "dev", Variable: "STRIPE_SECRET_KEY"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	x := SessionHub{Env: "boutique/site"}
	if _, err := s.cleDeTache(x, "stripe boutique"); err != nil {
		t.Errorf("the project's key refused to its deployment task: %v", err)
	}
	if _, err := s.cleDeTache(SessionHub{Env: "autre/site"}, "Stripe boutique"); err == nil {
		t.Error("another project's key accepted")
	}
	if l := s.lignesClesTache(nil, s.clesProjetEnv("boutique/site")); !strings.Contains(l, "Stripe boutique (stripe, code), read by the app as STRIPE_SECRET_KEY") || strings.Contains(l, val) {
		t.Errorf("what the agent is told: %s", l)
	}
}
