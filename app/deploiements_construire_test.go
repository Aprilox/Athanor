package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// this test binary as the kit's executable: a sub-command (construire-deploiement, mcp-cage) when asked
func init() {
	if os.Getenv("ATHANOR_ESSAI_SOUS_COMMANDE") == "1" {
		constructionSansCage = constructionSansCage || os.Getenv("ATHANOR_ESSAI_SANS_CAGE") == "1"
		code, ok := lancerSousCommande(os.Args[1:])
		if !ok {
			code = 2
		}
		os.Exit(code)
	}
}

// construireEssai: commandeConstruction replaced by this test binary (args gets each call's arguments).
// shortcut: on Windows, and on a Linux without bwrap (the CI runner, a golang container), the build runs with
// --sans-barriere; the barrier itself is tested on Linux with bwrap (deploiements_construire_linux_test.go).
func construireEssai(t *testing.T, args *[]string) {
	t.Helper()
	ancien := commandeConstruction
	t.Cleanup(func() { commandeConstruction = ancien })
	commandeConstruction = func(_ Kit, a ...string) *exec.Cmd {
		if args != nil {
			*args = slices.Clone(a)
		}
		_, err := exec.LookPath("bwrap")
		sansCage := runtime.GOOS == "windows" || err != nil
		if i := slices.Index(a, "--"); sansCage && i > 0 && !slices.Contains(a, "--sans-barriere") {
			a = slices.Insert(slices.Clone(a), i, "--sans-barriere")
		}
		c := exec.Command(os.Args[0], a...)
		c.Env = append(os.Environ(), "ATHANOR_ESSAI_SOUS_COMMANDE=1")
		if sansCage {
			c.Env = append(c.Env, "ATHANOR_ESSAI_SANS_CAGE=1")
		}
		return c
	}
}

// depotEssai: a repository with one commit per map of files (content "" deletes), their hashes.
func depotEssai(t *testing.T, versions ...map[string]string) (string, []string) {
	t.Helper()
	depot := t.TempDir()
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
	for i, v := range versions {
		for f, contenu := range v {
			p := filepath.Join(depot, filepath.FromSlash(f))
			if contenu == "" {
				_ = os.Remove(p)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(contenu), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		git("add", "-A")
		git("commit", "-q", "-m", "v"+string(rune('1'+i)))
		commits = append(commits, git("rev-parse", "HEAD"))
	}
	return depot, commits
}

// contenuArchive: the files of a .tar.gz and their contents.
func contenuArchive(t *testing.T, archive string) map[string]string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	m, r := map[string]string{}, tar.NewReader(z)
	for {
		h, err := r.Next()
		if err == io.EOF {
			return m
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(r)
			m[h.Name] = string(b)
		}
	}
}

func shEssai(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return "sh"
	}
	sh := bashNatif()
	if sh == "" {
		t.Skip("Git Bash absent")
	}
	return sh
}

// TestConstruireDeploiementIsole (C68): the build runs in a temporary folder outside the repository, which it
// leaves untouched; the archive holds the commit, what the build added and the ignored published folder.
func TestConstruireDeploiementIsole(t *testing.T) {
	sh := shEssai(t)
	depot, c := depotEssai(t, map[string]string{"site/index.html": "v1", ".gitignore": "dist/\n", "package.json": "{}"})
	cage := func(cmd string) func(string) (*exec.Cmd, error) {
		return func(src string) (*exec.Cmd, error) {
			x := exec.Command(sh, "-c", cmd)
			x.Dir = src
			return x, nil
		}
	}
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	var journal strings.Builder
	build := "mkdir -p dist && cp site/index.html dist/ && pwd > dist/ou.txt && echo intrus > ../intrus.txt && echo construit"
	if err := construireDeploiement(depot, c[0], "dist", archive, cage(build), &journal); err != nil {
		t.Fatalf("%v %s", err, journal.String())
	}
	a := contenuArchive(t, archive)
	if a["dist/index.html"] != "v1" || a["site/index.html"] != "v1" || a["package.json"] != "{}" || !strings.Contains(journal.String(), "construit") {
		t.Fatalf("archive: %v journal %q", a, journal.String())
	}
	if ou := a["dist/ou.txt"]; !strings.Contains(ou, "athanor-construire-") || strings.Contains(filepath.ToSlash(ou), filepath.Base(depot)) {
		t.Errorf("built in %q, not in a temporary folder of its own", ou)
	}
	if _, ok := a["intrus.txt"]; ok {
		t.Error("a file outside the build folder was archived")
	}
	if _, err := os.Stat(filepath.Join(depot, "dist")); err == nil {
		t.Error("the build wrote into the repository")
	}
	if st, _ := gitSortie(depot, "status", "--porcelain"); st != "" {
		t.Errorf("repository changed: %q", st)
	}
	// a failed build: an error, no archive
	archive2 := filepath.Join(t.TempDir(), "b.tar.gz")
	if err := construireDeploiement(depot, c[0], "dist", archive2, cage("exit 3"), io.Discard); err == nil || !strings.Contains(err.Error(), "the build failed") {
		t.Errorf("failed build: %v", err)
	}
	if _, err := os.Stat(archive2); err == nil {
		t.Error("an archive after a failed build")
	}
}

// TestConstruireArchiveNatif (C68): native Windows mode refuses a build, unless the project is forced (logged).
func TestConstruireArchiveNatif(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	depot, c := depotEssai(t, map[string]string{"index.html": "v1"})
	p := Projet{Nom: "m143-natif", Dossier: depot}
	cfg := ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "dist", Construire: "mkdir -p dist && echo ok > dist/a.txt"}
	var args []string
	construireEssai(t, &args)
	modeNatif.Store(true)
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := s.construireArchive(context.Background(), p, c[0], cfg, archive); !errors.Is(err, errConstruireSansBarriere) || args != nil {
		t.Fatalf("native, not forced: %v %v", err, args)
	}
	if err := s.acces.ForcerInfra(p.Nom, true); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		t.Skip("--sans-barriere is honoured on Windows only")
	}
	shEssai(t)
	if err := s.construireArchive(context.Background(), p, c[0], cfg, archive); err != nil || !slices.Contains(args, "--sans-barriere") {
		t.Fatalf("native, forced: %v %v", err, args)
	}
	if a := contenuArchive(t, archive); strings.TrimSpace(a["dist/a.txt"]) != "ok" {
		t.Errorf("archive: %v", a)
	}
	if !slices.ContainsFunc(lignesAudit(t, dossierJournalAudit()), func(l LigneAudit) bool {
		return l.Action == "construction sans barrière" && l.Projet == p.Nom
	}) {
		t.Error("the forced build is not in the audit")
	}
}

// TestFichiersBuildChanges (C68): the build files changed since the last deployment the owner accepted.
func TestFichiersBuildChanges(t *testing.T) {
	depot, c := depotEssai(t,
		map[string]string{"package.json": "{}", "site/index.html": "v1", "scripts/build.sh": "echo 1"},
		map[string]string{"site/index.html": "v2"},
		map[string]string{"scripts/build.sh": "echo 3"},
		map[string]string{"sous/yarn.lock": "x"})
	p := Projet{Nom: "m143-fichiers", Dossier: depot}
	_ = os.Remove(cheminHistorique(p.Nom, "dev"))
	t.Cleanup(func() { _ = os.Remove(cheminHistorique(p.Nom, "dev")) })
	cfg := &ConfigDeploiement{Methode: "statique", Source: "artefact", Construire: "npm ci && sh ./scripts/build.sh"}
	egal := func(nom string, f []string, attendu ...string) {
		t.Helper()
		if !slices.Equal(f, attendu) {
			t.Errorf("%s: %v, expected %v", nom, f, attendu)
		}
	}
	egal("without a build", fichiersBuildChanges(p, "dev", c[0], &ConfigDeploiement{Methode: "statique"}))
	egal("without a method", fichiersBuildChanges(p, "dev", c[0], nil))
	egal("nothing accepted yet", fichiersBuildChanges(p, "dev", c[1], cfg), "package.json", "scripts/build.sh")
	for _, d := range []Deploiement{{ID: "a", Commit: c[0], Accorde: true}, {ID: "b", Commit: c[2]}, {ID: "r", Commit: c[3], Accorde: true, Retour: true}} {
		d.Projet, d.Env = p.Nom, "dev"
		if err := noterHistorique(d); err != nil {
			t.Fatal(err)
		}
	}
	egal("site only", fichiersBuildChanges(p, "dev", c[1], cfg))
	egal("the named script", fichiersBuildChanges(p, "dev", c[2], cfg), "scripts/build.sh")
	egal("a lockfile anywhere", fichiersBuildChanges(p, "dev", c[3], cfg), "scripts/build.sh", "sous/yarn.lock")
}

// TestAccordBuildDeploiement (C68): changed build files ask the owner, even in dev auto (demander_deploiement and
// the relay), and such an accord never becomes a rule.
func TestAccordBuildDeploiement(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	depot, c := depotEssai(t, map[string]string{"package.json": "{}", "index.html": "v1"}, map[string]string{"Makefile": "all:"})
	p := Projet{Nom: "m143-accord", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	inventaireTest(t, nil, "ab12") // M247: an unknown server reads as production
	if err := s.acces.DefinirEnvironnement(p.Nom, "dev", Environnement{URL: "http://m143.test", Serveurs: []string{"ab12"}, Protection: "aucune", Politique: "auto", DeployerAuJalon: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.NoterDeploiement(p.Nom, "dev", ConfigDeploiement{Methode: "statique", Source: "artefact", Construire: "npm ci"}); err != nil {
		t.Fatal(err)
	}
	decider := func(id, decision string) error {
		_, _, err := s.deciderApprobation("fenetre", id, decision, "", "", "")
		return err
	}

	// demander_deploiement: dev auto, but package.json is new → the owner's accord, never a rule
	txt, err := outilDemanderDeploiement(s, SessionHub{Projet: p.Nom, Mission: "M1", Agent: "pilote"}, []byte(`{"environnement":"dev","commit":"`+c[0]+`"}`))
	_, reste, _ := strings.Cut(txt, "waiting for the owner, id ")
	id, _, _ := strings.Cut(reste, ":")
	ap, ok := s.approbations.Lire(id)
	if err != nil || !ok || ap.Details["fichiers_build"] != "package.json" || !strings.Contains(ap.Raison, "build files changed: package.json") {
		t.Fatalf("demander_deploiement: %v %q %+v", err, txt, ap)
	}
	if a := alertesApprobations([]Approbation{ap}); len(a) != 1 || len(a[0].Actions) != 2 {
		t.Errorf("the alert offers a rule: %+v", a)
	}
	if err := decider(id, "toujours"); err == nil || err.Error() != errBuildSansRegle.Error() {
		t.Errorf("a rule for changed build files: %v", err)
	}

	// the relay: asks once, waits, then starts with the accord; a refusal ends it, said
	e := s.acces.Environnements(p.Nom)["dev"]
	r := RelaisJalon{Jalon: "M2", Commit: c[1], Etat: "attente"}
	if accorde, attendre := s.accordBuildRelais(p, e, &r); accorde || !attendre || r.Accord == "" {
		t.Fatalf("relay, first: %v %v %+v", accorde, attendre, r)
	}
	if ap, _ := s.approbations.Lire(r.Accord); ap.Details["fichiers_build"] != "Makefile, package.json" || ap.Agent != "relais" {
		t.Errorf("relay's accord: %+v", ap)
	}
	premier := r.Accord
	if accorde, attendre := s.accordBuildRelais(p, e, &r); accorde || !attendre || r.Accord != premier {
		t.Errorf("relay, waiting: %v %v %+v", accorde, attendre, r)
	}
	if err := decider(premier, "accepter"); err != nil {
		t.Fatal(err)
	}
	if accorde, attendre := s.accordBuildRelais(p, e, &r); !accorde || attendre {
		t.Errorf("relay, accepted: %v %v %+v", accorde, attendre, r)
	}
	r = RelaisJalon{Jalon: "M3", Commit: c[1], Etat: "attente"}
	s.accordBuildRelais(p, e, &r)
	if err := decider(r.Accord, "refuser"); err != nil {
		t.Fatal(err)
	}
	if accorde, attendre := s.accordBuildRelais(p, e, &r); accorde || !attendre || r.Etat != "echec" || !strings.Contains(r.Erreur, "Makefile") {
		t.Errorf("relay, refused: %v %v %+v", accorde, attendre, r)
	}
}
