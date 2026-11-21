package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDepotGitServeur (M132): only a GitHub origin, rewritten to SSH, with github.com's pinned keys.
func TestDepotGitServeur(t *testing.T) {
	d := t.TempDir()
	if out, err := exec.Command("git", "-C", d, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if _, _, err := depotGitServeur(Projet{Dossier: d}); err == nil {
		t.Error("no origin accepted")
	}
	for origine, want := range map[string]string{
		"https://github.com/Moi/site.git":   "git@github.com:Moi/site.git",
		"https://github.com/moi/site":       "git@github.com:moi/site.git",
		"git@github.com:moi/mon.site.git":   "git@github.com:moi/mon.site.git",
		"ssh://git@github.com/moi/site.git": "git@github.com:moi/site.git",
		"https://gitlab.com/moi/site.git":   "",
		"https://github.com/moi/site/x":     "",
		"git@github.com:-oProxy=x/site.git": "",
	} {
		_ = exec.Command("git", "-C", d, "remote", "remove", "origin").Run()
		if out, err := exec.Command("git", "-C", d, "remote", "add", "origin", origine).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		url, hotes, err := depotGitServeur(Projet{Dossier: d})
		if url != want || (want != "") != (err == nil) || want != "" && !strings.HasPrefix(hotes, "github.com ssh-ed25519 ") {
			t.Errorf("%s → %q %v (want %q)", origine, url, err, want)
		}
	}
}

// TestDeploiementGitConteneur (M132): the container serves a git repository over SSH (user depot, the
// project's deployment key in its authorized_keys); the server fetches the commit itself with the key put in
// /etc/athanor/deploy (0600 root; /etc/athanor stays 755 for Caddy's certificate) and only the pinned host key. A commit not pushed or an unpinned host
// fails before the switch; the private key appears in no answer, history, audit or RUNBOOK.
func TestDeploiementGitConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	depot, c1, c2 := depotSiteExemple(t)
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", depot, "-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	bundle := filepath.Join(t.TempDir(), "site.bundle")
	git("bundle", "create", bundle, "--all")
	git("commit", "-q", "--allow-empty", "-m", "v3 not pushed")
	c3, _ := gitSortie(depot, "rev-parse", "HEAD")
	b, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m132", Dossier: depot})
	s.mu.Unlock()
	prive, public, err := cleGeneree("athanor-deploiement-m132")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "DEPLOY_KEY m132", Famille: "secret", Type: "ssh", Portee: "infra", Source: "deploiement:m132"}, prive)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.acces.NoterCleDeploiement("m132", e.ID); err != nil {
		t.Fatal(err)
	}
	const hote, base = "m132.test", "/srv/athanor/m132-dev"
	resoudreTest(t, hote+":80", web)
	if err := s.acces.definirAncien("m132", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	nettoyer := "sudo -n rm -rf " + base + " /etc/caddy/sites/" + hote + ".caddy /etc/athanor/deploy/m132 /etc/athanor/deploy/m132.known_hosts; sudo -n userdel -r depot >/dev/null 2>&1; rm -f /tmp/m132.bundle; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	sur(nettoyer)
	t.Cleanup(func() { sur(nettoyer) })
	// the git host (like GitHub, a reachable commit can be fetched by its SHA): user depot, the deployment key's public half, the repository with v1 and v2 only
	prep := "set -e; sudo -n useradd -m -s /bin/sh depot; sudo -n install -d -o depot -m 700 /home/depot/.ssh; echo '" + public + "' | sudo -n tee /home/depot/.ssh/authorized_keys >/dev/null; sudo -n chown depot /home/depot/.ssh/authorized_keys; sudo -n chmod 600 /home/depot/.ssh/authorized_keys" +
		"; echo " + base64.StdEncoding.EncodeToString(b) + " | base64 -d > /tmp/m132.bundle; chmod 644 /tmp/m132.bundle; sudo -n -u depot git clone -q --bare /tmp/m132.bundle /home/depot/site.git; sudo -n -u depot git -C /home/depot/site.git config uploadpack.allowReachableSHA1InWant true; cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub"
	r := appel("ssh_commande", map[string]any{"commande": prep})
	cleHote := strings.TrimSpace(r.Sortie)
	if r.Code != 0 || !strings.HasPrefix(cleHote, "ssh-ed25519 ") {
		t.Fatalf("git host: %+v", r)
	}
	avant := depotGitServeur
	t.Cleanup(func() { depotGitServeur = avant })
	epingle := "127.0.0.1 " + cleHote
	depotGitServeur = func(Projet) (string, string, error) { return "depot@127.0.0.1:site.git", epingle, nil }
	page := func() string {
		t.Helper()
		rq, _ := http.NewRequest("GET", "http://"+web+"/", nil)
		rq.Host = hote
		rep, err := http.DefaultClient.Do(rq)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Body.Close()
		b, _ := io.ReadAll(rep.Body)
		return string(b)
	}
	ctx, cfg := context.Background(), &ConfigDeploiement{Methode: "statique", Source: "git", Dossier: "site"}
	var vus []any

	d1, err := s.deployer(ctx, "m132", "dev", c1, OptionsDeploiement{Config: cfg, Auteur: "proprietaire"})
	vus = append(vus, d1, err)
	if err != nil || d1.Etat != "ok" || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("v1 by git: %v %+v %.200q", err, d1, page())
	}
	if e := s.acces.Environnements("m132")["dev"]; e.Deploiement == nil || e.Deploiement.Source != "git" {
		t.Fatalf("method not recorded: %+v", e.Deploiement)
	}
	d2, err := s.deployer(ctx, "m132", "dev", c2, OptionsDeploiement{Auteur: "relais"})
	vus = append(vus, d2, err)
	if err != nil || !strings.Contains(page(), "site-exemple v2") {
		t.Fatalf("v2 by git: %v %+v", err, d2)
	}
	// C69: the key is gone once fetched (a container of another project cannot read it)
	etat := "sudo -n stat -c '%U %a' /etc/athanor /etc/athanor/deploy; sudo -n test -e /etc/athanor/deploy/m132 && echo key left || echo no key; cd " + base + " && readlink current && ls -A | grep -c '^[.]git-'; sudo -n git -C depot.git rev-parse --is-shallow-repository"
	if got, want := sur(etat), "root 755\nroot 700\nno key\nreleases/"+d2.Version+"\n0\ntrue\n"; got != want {
		t.Errorf("key, links, archive left, shallow:\n got  %q\n want %q", got, want)
	}

	// a commit that is not on the repository: a clear failure, nothing switched
	d3, err := s.deployer(ctx, "m132", "dev", c3, OptionsDeploiement{Auteur: "proprietaire"})
	vus = append(vus, d3, err)
	if err == nil || d3.Etat != "echec" || !strings.Contains(err.Error(), "git fetch of "+c3+" failed") || !strings.Contains(err.Error(), "nothing was switched") {
		t.Fatalf("commit not pushed: %v %+v", err, d3)
	}
	// a host whose key is not the pinned one: refused by ssh
	epingle = "127.0.0.1 " + strings.SplitN(hotesGitHub, "\n", 2)[0][len("github.com "):]
	d4, err := s.deployer(ctx, "m132", "dev", c1, OptionsDeploiement{Auteur: "proprietaire"})
	vus = append(vus, d4, err)
	if err == nil || !strings.Contains(err.Error(), "Host key verification failed") {
		t.Fatalf("unpinned host: %v %+v", err, d4)
	}
	if got := sur("cd " + base + " && readlink current && ls releases | wc -l && ls -A | grep -c '^[.]git-'"); got != "releases/"+d2.Version+"\n2\n0\n" || !strings.Contains(page(), "site-exemple v2") {
		t.Errorf("after the failures: %q", got)
	}

	// the witness: the private key is in no answer, history, audit nor RUNBOOK
	temoin := strings.Split(prive, "\n")[1]
	if j, _ := json.Marshal(vus); strings.Contains(string(j), temoin) || len(temoin) < 40 {
		t.Error("private key in an answer")
	}
	for _, d := range []string{dossierModule("servers"), dossierJournalAudit()} {
		_ = filepath.WalkDir(d, func(p string, x fs.DirEntry, err error) error {
			if err == nil && !x.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), temoin) {
					t.Errorf("private key in %s", p)
				}
			}
			return nil
		})
	}
	if h := historiqueDeploiements("m132", "dev"); len(h) != 4 || h[2].Etat != "echec" || h[3].Etat != "echec" {
		t.Errorf("history: %+v", h)
	}
}
