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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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
	if validerConfigDeploiement(ConfigDeploiement{Methode: "statique", Source: "git", Dossier: "site"}) != nil ||
		validerConfigDeploiement(ConfigDeploiement{Methode: "compose", Source: "artefact", Dossier: "app"}) != nil ||
		validerConfigDeploiement(ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "dist", Construire: "npm ci && npm run build"}) != nil {
		t.Error("git source or compose method refused")
	}
	for _, c := range []ConfigDeploiement{{Methode: "helm", Source: "artefact"}, {Methode: "statique", Source: "ftp"},
		{Methode: "statique", Source: "artefact", Dossier: "../x"}, {Methode: "statique", Source: "artefact", Dossier: "/x"},
		{Methode: "statique", Source: "artefact", Dossier: ".git"}, {Methode: "statique", Source: "git", Construire: "npm run build"}, {Methode: "statique", Source: "artefact", Construire: "npm ci\nnpm run build"}} {
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
	if err := s.acces.definirAncien("m131", "dev", Environnement{URL: "http://m131.test", Serveurs: []string{"absent"}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	ctx, cfg := context.Background(), &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}
	if _, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{}); err == nil || !strings.Contains(err.Error(), "no deployment method") {
		t.Errorf("without a method: %v", err)
	}
	if _, err := s.deployer(ctx, "m131", "dev", "0123456789abcdef0123456789abcdef01234567", OptionsDeploiement{Config: cfg}); err == nil || !strings.Contains(err.Error(), "not in the project's repository") {
		t.Errorf("unknown commit: %v", err)
	}
	if _, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: &ConfigDeploiement{Methode: "statique", Source: "git"}}); err == nil || !strings.Contains(err.Error(), "deployment key") {
		t.Errorf("git without the deployment key: %v", err)
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
	if err := s.acces.definirAncien("m131", "dev", Environnement{URL: "http://m131.test", Serveurs: []string{"absent"}, Protection: "aucune", Politique: "auto", Deploiement: autre}); err != nil {
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
	resoudreTest(t, hote+":80", web)
	if err := s.acces.definirAncien("m131", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
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
	if v := d1.Verification; v == nil || !v.OK || v.HTTP.Statut != 200 || v.HTTP.TLS != nil {
		t.Errorf("external check of v1: %+v", v)
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
	if d3.Version == d1.Version {
		t.Errorf("c1 deployed again under v1's release folder: %s", d3.Version)
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
	recentes := []string{d4.Version, d2.Version, d1.Version}
	slices.Sort(recentes) // two deployments in the same second sort by their SHA
	slices.Reverse(recentes)
	if got, want := sur("ls "+base+"/releases | sort -r | tr '\\n' ' '"), strings.Join(recentes, " ")+" 20200101-000006-aaaaaaa 20200101-000005-aaaaaaa "; got != want {
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

	// M133: v2 served, then back to the previous version (v1), checked; going back again serves v2
	d5, err := s.deployer(ctx, "m131", "dev", c2, OptionsDeploiement{Auteur: "proprietaire"})
	if err != nil || !strings.Contains(page(), "site-exemple v2") {
		t.Fatalf("v2 again: %v %+v", err, d5)
	}
	r1, err := s.revenir(ctx, "m131", "dev", "proprietaire", "")
	if err != nil || r1.Etat != "ok" || !r1.Retour || r1.Version != d4.Version || r1.Commit != c1 || r1.Verification == nil || !r1.Verification.OK || !strings.Contains(page(), "site-exemple v1") {
		t.Fatalf("back to v1: %v %+v %.200q", err, r1, page())
	}
	if got, want := sur("cd "+base+" && readlink current previous"), "releases/"+d4.Version+"\nreleases/"+d5.Version+"\n"; got != want {
		t.Errorf("links after going back: %q, want %q", got, want)
	}
	if h := historiqueDeploiements("m131", "dev"); h[len(h)-2].ID != d5.ID || h[len(h)-2].Etat != "revenu" || h[len(h)-1].ID != r1.ID {
		t.Errorf("history after going back: %+v", h[len(h)-2:])
	}
	if r2, err := s.revenir(ctx, "m131", "dev", "proprietaire", ""); err != nil || r2.Version != d5.Version || r2.Commit != c2 || !strings.Contains(page(), "site-exemple v2") {
		t.Errorf("going back twice: %v %+v", err, r2)
	}
	liensServeur := func() string { return sur("cd " + base + " && readlink current previous") }
	// C76: a version served then found failing goes back by itself in dev under the auto policy
	vide := &ConfigDeploiement{Methode: "statique", Source: "artefact"} // the repository's root has no index.html: 404
	da, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: vide, Auteur: "proprietaire"})
	if err == nil || da.Etat != "revenu" || !da.Bascule || !strings.Contains(da.Erreur, "went back automatically to "+d5.Version) || !strings.Contains(page(), "site-exemple v2") {
		t.Fatalf("automatic way back: %v %+v %.200q", err, da, page())
	}
	if h := historiqueDeploiements("m131", "dev"); h[len(h)-1].Auteur != "auto" || !h[len(h)-1].Retour || h[len(h)-1].Etat != "ok" || h[len(h)-2].Etat != "revenu" {
		t.Errorf("history after the automatic way back: %+v", h[len(h)-2:])
	}
	if i := s.incidentEnv(fichierSante{}, "m131", "dev"); i == nil || i.Cle != "deploi-"+da.ID || !i.SansRetour {
		t.Errorf("incident after the automatic way back: %+v", i)
	}
	servis := liensServeur()
	if servis != "releases/"+d5.Version+"\nreleases/"+da.Version+"\n" {
		t.Errorf("links after the automatic way back: %q", servis)
	}
	// C71: a switch that fails once done is put back, the release removed; Go back is not offered
	site := &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}
	echecBasculeTest = func(n int) bool { return n == 0 }
	t.Cleanup(func() { echecBasculeTest = nil })
	time.Sleep(time.Second) // another version name than da (same commit, the version is per second)
	db, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: site, Auteur: "proprietaire"})
	if err == nil || db.Etat != "echec" || db.Bascule || !avantBascule(db) || !strings.Contains(err.Error(), "nothing was switched") ||
		liensServeur() != servis || sur("ls "+base+"/releases | grep -c "+db.Version) != "0\n" || !strings.Contains(page(), "site-exemple v2") {
		t.Errorf("failed switch: %v %+v %q", err, db, liensServeur())
	}
	if i := s.incidentEnv(fichierSante{}, "m131", "dev"); i == nil || !i.SansRetour || slices.ContainsFunc(s.alertesIncidents(), func(a AlerteHub) bool {
		return a.Element == "m131/dev" && slices.ContainsFunc(a.Actions, func(x ActionAlerte) bool { return x.ID == "revenir" })
	}) {
		t.Errorf("Go back offered after a failure before the switch: %+v", i)
	}
	// two servers, the second one's switch fails: both put back, newest first
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		x := l[slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })]
		x.ID += "-2"
		return append(l, x), nil
	}); err != nil {
		t.Fatal(err)
	}
	e := s.acces.Environnements("m131")["dev"]
	e.Serveurs = []string{id, id + "-2"}
	if err := s.acces.definirAncien("m131", "dev", e); err != nil {
		t.Fatal(err)
	}
	echecBasculeTest = func(n int) bool { return n == 1 }
	if rb, err := s.revenir(ctx, "m131", "dev", "proprietaire", d4.Version); err == nil || rb.Etat != "echec" || rb.Bascule || liensServeur() != servis || !strings.Contains(page(), "site-exemple v2") {
		t.Errorf("way back failing on the second server: %v %+v %q", err, rb, liensServeur())
	}
	echecBasculeTest = nil
	// Retry of a way back aims at the same version: twice is harmless
	for n := range 2 {
		if r, err := s.revenir(ctx, "m131", "dev", "proprietaire", d4.Version); err != nil || r.Version != d4.Version || !r.Bascule ||
			liensServeur() != "releases/"+d4.Version+"\nreleases/"+d5.Version+"\n" || !strings.Contains(page(), "site-exemple v1") {
			t.Errorf("way back to %s, try %d: %v %+v %q", d4.Version, n+1, err, r, liensServeur())
		}
	}
	e.Serveurs = []string{id}
	if err := s.acces.definirAncien("m131", "dev", e); err != nil {
		t.Fatal(err)
	}
	// a failed external check: the version stays served, the deployment is a failure
	resoudreTest(t, hote+":80", "127.0.0.1:1")
	// C71: its automatic way back fails its check too: the links put back on it
	if d6, err := s.deployer(ctx, "m131", "dev", c1, OptionsDeploiement{Config: site, Auteur: "proprietaire"}); err == nil || d6.Etat != "echec" ||
		d6.Verification == nil || d6.Verification.OK || !strings.Contains(d6.Erreur, "the external verification failed: the site does not answer") ||
		!strings.Contains(d6.Erreur, "the automatic way back failed") || liensServeur() != "releases/"+d6.Version+"\nreleases/"+d4.Version+"\n" {
		t.Errorf("failed check: %v %+v %q", err, d6, liensServeur())
	}
}

// resoudreTest: verifierHTTP connects to adresse ("ip:port") for cible ("host:port") during the test.
func resoudreTest(t *testing.T, cible, adresse string) {
	resolutionsTest[cible] = adresse
	t.Cleanup(func() { delete(resolutionsTest, cible) })
}

// TestJugerVerification (M133): answers, certificate over https, and a protected environment closed without identification.
func TestJugerVerification(t *testing.T) {
	tlsOK, tlsKO := &InfoTLS{Valide: true, JoursRestants: 80}, &InfoTLS{Erreur: "x509: unknown authority"}
	for _, c := range []struct {
		protection string
		r          ResultatHTTP
		want       string
	}{
		{"aucune", ResultatHTTP{URL: "https://a.fr", Statut: 200, TLS: tlsOK}, ""},
		{"aucune", ResultatHTTP{URL: "http://a.fr", Statut: 301}, ""},
		{"aucune", ResultatHTTP{URL: "http://a.fr", Statut: 502}, "statut"},
		{"aucune", ResultatHTTP{URL: "https://a.fr", Statut: 200, TLS: tlsKO}, "tls"},
		{"aucune", ResultatHTTP{URL: "https://a.fr", Erreur: "connection refused"}, "injoignable"},
		{"auth_proxy", ResultatHTTP{URL: "https://a.fr", Statut: 401, TLS: tlsOK}, ""},
		{"auth_proxy", ResultatHTTP{URL: "https://a.fr", Statut: 200, TLS: tlsOK}, "ouvert"},
		{"access", ResultatHTTP{URL: "https://a.fr", Statut: 302, Redirection: "https://equipe.cloudflareaccess.com/cdn-cgi/access/login/a.fr", TLS: tlsOK}, ""},
		{"access", ResultatHTTP{URL: "https://a.fr", Statut: 302, Redirection: "https://cloudflareaccess.com.piege.fr/", TLS: tlsOK}, "ouvert"},
		{"access", ResultatHTTP{URL: "https://a.fr", Statut: 200, TLS: tlsOK}, "ouvert"},
	} {
		v := jugerVerification(Environnement{Protection: c.protection}, c.r)
		if got := strings.Join(v.Problemes, ","); got != c.want || v.OK != (c.want == "") {
			t.Errorf("%s %+v: %q ok=%v, want %q", c.protection, c.r, got, v.OK, c.want)
		}
	}
}
