package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiveKitTest: a kit archive of the files given (name → content).
func archiveKitTest(t *testing.T, fichiers map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	w := tar.NewWriter(z)
	for nom, c := range fichiers {
		if err := w.WriteHeader(&tar.Header{Name: nom, Mode: 0o644, Size: int64(len(c)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(c))
	}
	w.Close()
	z.Close()
	return buf.Bytes()
}

func avecArchive(t *testing.T, fichiers map[string]string) {
	t.Helper()
	avant := archiveKitGz
	archiveKitGz = archiveKitTest(t, fichiers)
	t.Cleanup(func() { archiveKitGz = avant })
}

func lire(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var kitDeTest = map[string]string{
	"template/CLAUDE.md":               "instructions v2",
	"template/guides/NATIVE.md":        "guide",
	"template/.claude/agents/scout.md": "scout",
	"env.example":                      "MODEL=\n",
	"LICENSE":                          "MIT",
	"app/go.mod":                       "module athanor\n",
	"app/main.go":                      "package main\n",
}

func TestDeployerKitPremiereFois(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := filepath.Join(t.TempDir(), "Athanor")
	if _, err := deployerKit(d); err != nil {
		t.Fatal(err)
	}
	if lire(t, filepath.Join(d, "template", "CLAUDE.md")) != "instructions v2" || lire(t, filepath.Join(d, ".env.example")) != "MODEL=\n" || lire(t, filepath.Join(d, "LICENSE")) != "MIT" {
		t.Fatal("kit files missing")
	}
	if lire(t, filepath.Join(d, "template", ".claude", "agents", "scout.md")) != "scout" {
		t.Fatal("hidden folder of template/ missing")
	}
	if strings.TrimSpace(lire(t, filepath.Join(d, "system", "version.txt"))) != versionAthanor {
		t.Fatal("version not recorded")
	}
	if !fichierRegulierPilote(exeDuKit(d)) {
		t.Fatal("the executable was not copied")
	}
	if _, err := os.Stat(filepath.Join(d, "app")); err == nil {
		t.Fatal("app/ written although no repair asked for it")
	}
	if !estDossierKit(d) {
		t.Fatal("not recognized as a kit")
	}
	// a second launch of the same version changes nothing
	st1, _ := os.Stat(filepath.Join(d, "template", "CLAUDE.md"))
	r, err := deployerKit(d)
	st2, _ := os.Stat(filepath.Join(d, "template", "CLAUDE.md"))
	if err != nil || r.Fichiers || r.Exe || !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatalf("same version deployed again: %+v %v", r, err)
	}
}

func TestDeployerKitMiseAJourGardeLesDonnees(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	ecrireT := func(rel, c string) {
		f := filepath.Join(d, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ecrireT("template/CLAUDE.md", "instructions v1")
	ecrireT("template/ancien.md", "removed by the update")
	ecrireT("app/go.mod", "old sources")
	ecrireT("app/vieux.go", "old")
	ecrireT("system/version.txt", "2.25.0\n")
	ecrireT(".env", "CLAUDE_CODE_OAUTH_TOKEN=secret\n")
	ecrireT(nomConfig(), "{}")
	ecrireT("logs/pilotes/p/cycle.log", "log")
	ecrireT("tools/bin/x", "tool")
	ecrireT("local/template/CLAUDE.md", "repair")
	ecrireT("extensions/biblio.json", "{}")
	ecrireT(filepath.Base(exeDuKit(d)), "old executable")
	r, err := deployerKit(d)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Fichiers || !r.Exe {
		t.Fatalf("update incomplete: %+v", r)
	}
	if lire(t, filepath.Join(d, "template", "CLAUDE.md")) != "instructions v2" {
		t.Fatal("template/ not updated")
	}
	if _, err := os.Stat(filepath.Join(d, "template", "ancien.md")); err == nil {
		t.Fatal("a file the new version no longer has stayed in template/")
	}
	if lire(t, filepath.Join(d, "app", "go.mod")) != "module athanor\n" {
		t.Fatal("app/ present but not updated")
	}
	if _, err := os.Stat(filepath.Join(d, "app", "vieux.go")); err == nil {
		t.Fatal("old source kept in app/")
	}
	for rel, c := range map[string]string{".env": "CLAUDE_CODE_OAUTH_TOKEN=secret\n", nomConfig(): "{}", "logs/pilotes/p/cycle.log": "log", "tools/bin/x": "tool", "local/template/CLAUDE.md": "repair", "extensions/biblio.json": "{}"} {
		if lire(t, filepath.Join(d, filepath.FromSlash(rel))) != c {
			t.Fatalf("%s changed by the update", rel)
		}
	}
	if lire(t, exeDuKit(d)) == "old executable" {
		t.Fatal("older executable not replaced")
	}
	if strings.TrimSpace(lire(t, filepath.Join(d, "system", "version.txt"))) != versionAthanor {
		t.Fatal("version not recorded")
	}
	for _, f := range []string{"template.nouveau", "template.ancien", "app.nouveau", "app.ancien"} {
		if _, err := os.Stat(filepath.Join(d, f)); err == nil {
			t.Fatalf("%s left behind", f)
		}
	}
}

func TestDeployerKitNeRetrogradePasLExecutable(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, "system"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "system", "version.txt"), []byte("99.0.0\n"), 0o644)
	_ = os.WriteFile(exeDuKit(d), []byte("newer executable"), 0o755)
	if _, err := deployerKit(d); err != nil {
		t.Fatal(err)
	}
	if lire(t, exeDuKit(d)) != "newer executable" {
		t.Fatal("a newer executable was replaced by an older one")
	}
}

func TestDeployerKitSourceDuCode(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(d, "template"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "template", "CLAUDE.md"), []byte("working copy"), 0o644)
	if _, err := deployerKit(d); err != nil {
		t.Fatal(err)
	}
	if lire(t, filepath.Join(d, "template", "CLAUDE.md")) != "working copy" {
		t.Fatal("the files of a copy of the source code were replaced")
	}
	if err := extraireSources(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "app")); err == nil {
		t.Fatal("sources written into a copy of the source code")
	}
}

func TestExtraireSources(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	if err := extraireSources(d); err != nil {
		t.Fatal(err)
	}
	if lire(t, filepath.Join(d, "app", "go.mod")) != "module athanor\n" || lire(t, filepath.Join(d, "app", "main.go")) != "package main\n" {
		t.Fatal("sources not written")
	}
	if _, err := os.Stat(filepath.Join(d, "template")); err == nil {
		t.Fatal("extraireSources wrote more than app/")
	}
	// a repair in progress is never overwritten
	_ = os.WriteFile(filepath.Join(d, "app", "main.go"), []byte("repaired"), 0o644)
	_ = extraireSources(d)
	if lire(t, filepath.Join(d, "app", "main.go")) != "repaired" {
		t.Fatal("the sources of a repair were overwritten")
	}
}

func TestFichiersArchiveRefuseLesSorties(t *testing.T) {
	for _, nom := range []string{"../evil", "/etc/passwd", "template/../../x", `a\b`} {
		if _, _, err := fichiersArchive(archiveKitTest(t, map[string]string{nom: "x"})); err == nil {
			t.Errorf("%q accepted", nom)
		}
	}
}

func TestSansArchiveRienNeChange(t *testing.T) {
	avant := archiveKitGz
	archiveKitGz = nil
	defer func() { archiveKitGz = avant }()
	if kitEmbarque() {
		t.Fatal("kitEmbarque without archive")
	}
	if _, ok := lancerDepuisTelechargement([]string{"autotest"}); ok {
		t.Fatal("a plain build took over the launch")
	}
	if _, err := deployerKit(t.TempDir()); err == nil {
		t.Fatal("deployerKit without archive")
	}
}

func TestArchiverKitDuDepot(t *testing.T) {
	racine, _ := filepath.Abs("..")
	if !estDossierKit(racine) {
		t.Skip("not run from the source code")
	}
	f := filepath.Join(t.TempDir(), "kit.tar.gz")
	if err := archiverKit(racine, f); err != nil {
		t.Fatal(err)
	}
	gz, _ := os.ReadFile(f)
	fichiers, _, err := fichiersArchive(gz)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"template/CLAUDE.md", "template/.claude/agents/scout.md", "env.example", "LICENSE", "app/go.mod", "app/main.go", "app/static/app.js", "app/third_party/go-webview2/go.mod", "app/winres/athanor.rc", "app/catalogue.json"} {
		if _, ok := fichiers[n]; !ok {
			t.Errorf("%s missing from the archive", n)
		}
	}
	for n := range fichiers {
		if strings.HasPrefix(n, "app/embarque/") || n == "app/athanor" || strings.HasSuffix(n, ".exe") || strings.Contains(n, "/.") && !strings.HasPrefix(n, "template/.claude/") {
			t.Errorf("%s should not be in the archive", n)
		}
	}
	// the same sources give the same archive
	g := filepath.Join(t.TempDir(), "kit2.tar.gz")
	_ = archiverKit(racine, g)
	gz2, _ := os.ReadFile(g)
	if !bytes.Equal(gz, gz2) {
		t.Error("two archives of the same sources differ")
	}
}
