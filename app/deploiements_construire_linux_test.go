package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMasquesCage (C68): hub/, modules/ and the settings hidden in the cage, folders by an empty tmpfs.
func TestMasquesCage(t *testing.T) {
	kit := t.TempDir()
	if err := os.Mkdir(filepath.Join(kit, "hub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kit, "athanor-config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	estDossier := func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
	a := "\n" + strings.Join(masquesCage(kit, estDossier), "\n") + "\n"
	if !strings.Contains(a, "\n--tmpfs\n"+filepath.Join(kit, "hub")+"\n") || !strings.Contains(a, "\n--ro-bind\n/dev/null\n"+filepath.Join(kit, "athanor-config.json")+"\n") {
		t.Errorf("masks: %q", a)
	}
	if strings.Contains(a, filepath.Join(kit, "modules")) {
		t.Errorf("an absent folder masked: %q", a)
	}
}

// TestConstruireCageLinux (C68): in the barrier, the repository's build script cannot write hub/acces.json
// nor hub/regles.json of the kit (Linux with bubblewrap only).
func TestConstruireCageLinux(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap absent")
	}
	kit := t.TempDir()
	for _, d := range []string{"tools", "hub"} {
		if err := os.Mkdir(filepath.Join(kit, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	acces := filepath.Join(kit, "hub", "acces.json")
	if err := os.WriteFile(acces, []byte("temoin"), 0o600); err != nil {
		t.Fatal(err)
	}
	depot, c := depotEssai(t, map[string]string{"index.html": "v1"})
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	build := "echo pirate > " + acces + "; echo pirate > " + filepath.Join(kit, "hub", "regles.json") + "; echo ok > fait.txt"
	x := exec.Command(os.Args[0], "construire-deploiement", kit, depot, c[0], "", archive, "--", build)
	x.Env = append(os.Environ(), "ATHANOR_ESSAI_SOUS_COMMANDE=1")
	if out, err := x.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if b, _ := os.ReadFile(acces); string(b) != "temoin" {
		t.Errorf("hub/acces.json written by the build: %q", b)
	}
	if _, err := os.Stat(filepath.Join(kit, "hub", "regles.json")); err == nil {
		t.Error("hub/regles.json written by the build")
	}
	if a := contenuArchive(t, archive); strings.TrimSpace(a["fait.txt"]) != "ok" {
		t.Errorf("archive: %v", a)
	}
}
