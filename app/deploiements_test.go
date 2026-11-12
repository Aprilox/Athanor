package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// archiveDeploiementTest: a tar.gz with the given entries (name → link target for a link, "" for a file, "/" for a folder, "!" for a hard link).
func archiveDeploiementTest(t *testing.T, entrees [][2]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entrees {
		h := &tar.Header{Name: e[0], Mode: 0o644}
		switch e[1] {
		case "":
			h.Typeflag, h.Size = tar.TypeReg, 1
		case "/":
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		case "!":
			h.Typeflag, h.Linkname = tar.TypeLink, "site/index.html"
		default:
			h.Typeflag, h.Linkname = tar.TypeSymlink, e[1]
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size == 1 {
			tw.Write([]byte("x"))
		}
	}
	tw.Close()
	gz.Close()
	f.Close()
	return p
}

// TestVerifierArchive (M131): only files, folders and links that stay inside the release; the published folder present.
func TestVerifierArchive(t *testing.T) {
	sain := [][2]string{{"site/", "/"}, {"site/index.html", ""}, {"site/a/lien", "../index.html"}, {"site/ici", "."}}
	if err := verifierArchive(archiveDeploiementTest(t, sain), "site"); err != nil {
		t.Fatalf("sound archive refused: %v", err)
	}
	if err := verifierArchive(archiveDeploiementTest(t, sain), ""); err != nil {
		t.Fatalf("whole repository refused: %v", err)
	}
	for _, c := range []struct {
		nom      string
		entrees  [][2]string
		dossier  string
		attendus string
	}{
		{"absolute", [][2]string{{"/etc/passwd", ""}}, "", "leaves the release"},
		{"dot-dot", [][2]string{{"site/../../x", ""}}, "", "leaves the release"},
		{"dot-dot at the start", [][2]string{{"../x", ""}}, "", "leaves the release"},
		{"absolute link", [][2]string{{"site/l", "/etc"}}, "", "points out"},
		{"link above", [][2]string{{"site/a/l", "../../../x"}}, "", "points out"},
		{"link to the parent", [][2]string{{"l", ".."}}, "", "points out"},
		{"hard link", [][2]string{{"site/index.html", ""}, {"h", "!"}}, "", "not a file"},
		{"no published folder", [][2]string{{"autre/index.html", ""}}, "site", "no folder"},
		{"prefix only", [][2]string{{"site2/index.html", ""}}, "site", "no folder"},
	} {
		if err := verifierArchive(archiveDeploiementTest(t, c.entrees), c.dossier); err == nil || !strings.Contains(err.Error(), c.attendus) {
			t.Errorf("%s: %v", c.nom, err)
		}
	}
}

// TestBlocCaddyStatique (M131): an absolute folder is served by file_server; a relative or ".." one is refused.
func TestBlocCaddyStatique(t *testing.T) {
	b, err := blocCaddy(Environnement{URL: "http://m131.test"}, "s1", "/srv/athanor/p-dev/current/site", "", "")
	if err != nil || !strings.Contains(b, "\troot * /srv/athanor/p-dev/current/site\n\tfile_server\n}") || strings.Contains(b, "reverse_proxy") {
		t.Fatalf("static block: %v\n%s", err, b)
	}
	for _, r := range []string{"/srv/../etc", "/srv/a b", "srv/x", "/srv/.cache", "/"} {
		if _, err := blocCaddy(Environnement{URL: "http://m131.test"}, "s1", r, "", ""); err == nil {
			t.Errorf("folder %q accepted", r)
		}
	}
}

func TestBaseDeploiement(t *testing.T) {
	if b := baseDeploiement("site-1", "dev"); b != "/srv/athanor/site-1-dev" {
		t.Errorf("simple name: %s", b)
	}
	a, b := baseDeploiement("Mon jeu", "prod"), baseDeploiement("mon-jeu", "prod")
	if a == b || !strings.HasPrefix(a, "/srv/athanor/mon-jeu-") || !reRacineCaddy.MatchString(a+"/current") {
		t.Errorf("names with spaces or capitals: %s %s", a, b)
	}
	for _, c := range []ConfigDeploiement{{Methode: "compose", Source: "artefact"}, {Methode: "statique", Source: "git"},
		{Methode: "statique", Source: "artefact", Dossier: "../x"}, {Methode: "statique", Source: "artefact", Dossier: "/x"},
		{Methode: "statique", Source: "artefact", Dossier: ".git"}, {Methode: "statique", Source: "artefact", Construire: "npm run build"}} {
		if validerConfigDeploiement(c) == nil {
			t.Errorf("configuration accepted: %+v", c)
		}
	}
}

// depotSiteExemple: a git repository whose two commits publish site-exemple v1 then v2 in site/.
func depotSiteExemple(t *testing.T) (depot, c1, c2 string) {
	t.Helper()
	depot = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)...)
		c.Dir = depot
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	var commits []string
	for _, v := range []string{"v1", "v2"} {
		os.RemoveAll(filepath.Join(depot, "site"))
		if err := os.CopyFS(filepath.Join(depot, "site"), os.DirFS(filepath.Join("testdata", "site-exemple", v))); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", v)
		commits = append(commits, git("rev-parse", "HEAD"))
	}
	return depot, commits[0], commits[1]
}

// TestDeployerRefus (M131): without a method, a known commit or a free lock, nothing starts; a failed attempt
// is in the history and records no method; the panel's save keeps the recorded method.
func TestDeployerRefus(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	depot, c1, _ := depotSiteExemple(t)
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m131", Dossier: depot})
	s.mu.Unlock()
	if err := s.acces.DefinirEnvironnement("m131", "dev", Environnement{URL: "http://m131.test", Serveurs: []string{"absent"}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	ctx, cfg := context.Background(), &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}
	if _, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{}); err == nil || !strings.Contains(err.Error(), "no deployment method") {
		t.Errorf("without a method: %v", err)
	}
	if _, err := s.deployer(ctx, "m131", "dev", "0123456789abcdef0123456789abcdef01234567", OptionsDeploiement{Config: cfg}); err == nil || !strings.Contains(err.Error(), "not in the project's repository") {
		t.Errorf("unknown commit: %v", err)
	}
	v, _ := verrousDeploiement.LoadOrStore("m131/dev", &sync.Mutex{})
	v.(*sync.Mutex).Lock()
	_, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: cfg})
	v.(*sync.Mutex).Unlock()
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("locked: %v", err)
	}
	if h := historiqueDeploiements("m131", "dev"); len(h) != 0 {
		t.Errorf("refusals in the history: %+v", h)
	}
	d, err := s.deployer(ctx, "m131", "dev", c1[:8], OptionsDeploiement{Config: cfg, Auteur: "proprietaire"})
	if err == nil || d.Etat != "echec" || d.Commit != c1 || !strings.Contains(d.Erreur, "nothing was switched") {
		t.Errorf("unknown server: %v %+v", err, d)
	}
	if h := historiqueDeploiements("m131", "dev"); len(h) != 1 || h[0].Etat != "echec" || h[0].Auteur != "proprietaire" || h[0].Version != d.Version {
		t.Errorf("history: %+v", h)
	}
	if e := s.acces.Environnements("m131")["dev"]; e.Deploiement != nil {
		t.Errorf("method recorded after a failure: %+v", e.Deploiement)
	}
	if err := s.acces.NoterDeploiement("m131", "dev", *cfg); err != nil {
		t.Fatal(err)
	}
	autre := &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "x"}
	if err := s.acces.DefinirEnvironnement("m131", "dev", Environnement{URL: "http://m131.test", Serveurs: []string{"absent"}, Protection: "aucune", Politique: "auto", Deploiement: autre}); err != nil {
		t.Fatal(err)
	}
	if e := s.acces.Environnements("m131")["dev"]; e.Deploiement == nil || *e.Deploiement != *cfg {
		t.Errorf("the panel changed the method: %+v", e.Deploiement)
	}
}

// TestDeploiementStatiqueConteneur (M131): on the test container, v1 then v2 served by Caddy (read on its HTTP
// port); a configuration Caddy refuses changes nothing served; 5 versions kept, never current nor previous.
func TestDeploiementStatiqueConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	depot, c1, c2 := depotSiteExemple(t)
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m131", Dossier: depot})
	s.mu.Unlock()
	const hote, base = "m131.test", "/srv/athanor/m131-dev"
	if err := s.acces.DefinirEnvironnement("m131", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	nettoyer := "sudo -n rm -rf " + base + " /etc/caddy/sites/" + hote + ".caddy /etc/caddy/sites/zz-casse.caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	appel("ssh_commande", map[string]any{"commande": nettoyer})
	t.Cleanup(func() { appel("ssh_commande", map[string]any{"commande": nettoyer}) })
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
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	ctx := context.Background()

	d1, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}, Auteur: "proprietaire"})
	if err != nil || d1.Etat != "ok" || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("v1: %v %+v %.200q", err, d1, page())
	}
	if e := s.acces.Environnements("m131")["dev"]; e.Deploiement == nil || e.Deploiement.Dossier != "site" {
		t.Fatalf("method not recorded: %+v", e.Deploiement)
	}
	// the recorded method, without an agent
	d2, err := s.deployer(ctx, "m131", "dev", c2, OptionsDeploiement{Auteur: "relais"})
	if err != nil || !strings.Contains(page(), "site-exemple v2") {
		t.Fatalf("v2: %v %+v %.200q", err, d2, page())
	}
	liens := "cd " + base + " && readlink current previous && stat -c '%U %a' releases/*/site/index.html | sort -u && ls -A ~/.athanor-deploy | wc -l && grep -c 'import /etc/caddy/sites/' /etc/caddy/Caddyfile"
	if got, want := sur(liens), "releases/"+d2.Version+"\nreleases/"+d1.Version+"\nroot 644\n0\n1\n"; got != want {
		t.Errorf("links, owner, archive left, import:\n got  %q\n want %q", got, want)
	}

	// a configuration Caddy refuses: refused before the switch, v2 still served, the site file as it was
	avant := sur("cat /etc/caddy/sites/" + hote + ".caddy")
	sur("echo 'casse.test {' | sudo -n tee /etc/caddy/sites/zz-casse.caddy >/dev/null")
	d3, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Auteur: "proprietaire"})
	if err == nil || d3.Etat != "echec" || !strings.Contains(err.Error(), "caddy validate") {
		t.Fatalf("refused configuration: %v %+v", err, d3)
	}
	sur("sudo -n rm -f /etc/caddy/sites/zz-casse.caddy")
	if got := sur("cd " + base + " && readlink current && ls releases | wc -l && ls -A | grep -c site-avant; cat /etc/caddy/sites/" + hote + ".caddy"); got != "releases/"+d2.Version+"\n2\n0\n"+avant || !strings.Contains(page(), "site-exemple v2") {
		t.Errorf("after the refusal: %q (site file before: %q)", got, avant)
	}

	// 5 versions kept: 6 old ones made, one more deployment
	sur("cd " + base + "/releases && for i in 1 2 3 4 5 6; do sudo -n mkdir 20200101-00000$i-aaaaaaa; done")
	d4, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Auteur: "proprietaire"})
	if err != nil || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("v1 again: %v %+v", err, d4)
	}
	if got, want := sur("ls "+base+"/releases | sort -r | tr '\\n' ' '"), d4.Version+" "+d2.Version+" "+d1.Version+" 20200101-000006-aaaaaaa 20200101-000005-aaaaaaa "; got != want {
		t.Errorf("versions kept:\n got  %q\n want %q", got, want)
	}
	h := historiqueDeploiements("m131", "dev")
	etats := []string{}
	for _, x := range h {
		etats = append(etats, x.Etat+":"+x.Auteur)
	}
	if strings.Join(etats, " ") != "ok:proprietaire ok:relais echec:proprietaire ok:proprietaire" || h[1].Commit != c2 {
		t.Errorf("history: %v", etats)
	}
}
