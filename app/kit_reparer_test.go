package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The downloaded file opened again at the installed version: what was damaged or deleted is put back, the
// executable too; nothing when everything matches; never another version's folder, never a copy of the code.
func TestReparerKitMemeVersion(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := filepath.Join(t.TempDir(), "Athanor")
	if _, err := deployerKit(d); err != nil {
		t.Fatal(err)
	}
	if l, err := reparerKit(d); err != nil || len(l) != 0 {
		t.Fatalf("an intact folder: %v %v", l, err)
	}
	_ = os.WriteFile(filepath.Join(d, "template", "CLAUDE.md"), []byte("damaged"), 0o644)
	_ = os.Remove(filepath.Join(d, "template", "guides", "NATIVE.md"))
	_ = os.WriteFile(filepath.Join(d, "LICENSE"), []byte(""), 0o644)
	_ = os.WriteFile(exeDuKit(d), []byte("a broken executable"), 0o755)
	ecrireEssai(t, filepath.Join(d, "logs", "garde.txt"), "the user's")
	ecrireEssai(t, filepath.Join(d, ".env"), "MODEL=opus\n")
	l, err := reparerKit(d)
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.Join(l, ","); !strings.Contains(s, "template/CLAUDE.md") || !strings.Contains(s, "template/guides/NATIVE.md") || !strings.Contains(s, "LICENSE") || !strings.Contains(s, filepath.Base(exeDuKit(d))) {
		t.Fatalf("repaired: %v", l)
	}
	if lire(t, filepath.Join(d, "template", "CLAUDE.md")) != "instructions v2" || lire(t, filepath.Join(d, "template", "guides", "NATIVE.md")) != "guide" || lire(t, filepath.Join(d, "LICENSE")) != "MIT" {
		t.Fatal("not put back")
	}
	exe, _ := os.Executable()
	if !memeContenu(exe, exeDuKit(d)) {
		t.Fatal("the executable not put back")
	}
	if lire(t, filepath.Join(d, "logs", "garde.txt")) != "the user's" || lire(t, filepath.Join(d, ".env")) != "MODEL=opus\n" {
		t.Fatal("the user's files touched")
	}
	if l, _ := reparerKit(d); len(l) != 0 {
		t.Fatalf("repaired twice: %v", l)
	}
	// another version's folder: never
	_ = os.WriteFile(filepath.Join(d, "system", "version.txt"), []byte("99.0.0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(d, "template", "CLAUDE.md"), []byte("v99"), 0o644)
	if l, _ := reparerKit(d); len(l) != 0 || lire(t, filepath.Join(d, "template", "CLAUDE.md")) != "v99" {
		t.Fatalf("a newer folder repaired: %v", l)
	}
	// a copy of the source code: never
	_ = os.WriteFile(filepath.Join(d, "system", "version.txt"), []byte(versionAthanor+"\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	if l, _ := reparerKit(d); len(l) != 0 {
		t.Fatalf("a copy of the code repaired: %v", l)
	}
}

// What the downloaded file did reaches the application it opens, once.
func TestInfoLancement(t *testing.T) {
	if infoLancement() != nil {
		t.Fatal("nothing said")
	}
	noterLancement("/tmp/Athanor.exe", "2.40.0", "", []string{"LICENSE", "template/CLAUDE.md"})
	defer func() {
		for _, k := range []string{"ATHANOR_LANCEMENT", "ATHANOR_LANCEMENT_FICHIER", "ATHANOR_LANCEMENT_ANCIEN", "ATHANOR_LANCEMENT_MAJ", "ATHANOR_LANCEMENT_REPARES"} {
			os.Unsetenv(k)
		}
	}()
	i := infoLancement()
	if i == nil || !i.Telechargement || i.Fichier != "/tmp/Athanor.exe" || i.Ancien != "2.40.0" || len(i.Repares) != 2 {
		t.Fatalf("%+v", i)
	}
}
