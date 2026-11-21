package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNettoyerMajReessaie: logs/maj is removed once the new version runs, even when the first try fails
// (the downloaded executable still running on Windows), and never under another version.
func TestNettoyerMajReessaie(t *testing.T) {
	ancienne := pauseNettoyageMaj
	pauseNettoyageMaj = 20 * time.Millisecond
	t.Cleanup(func() { pauseNettoyageMaj = ancienne })
	k := Kit{t.TempDir()}
	if err := os.MkdirAll(filepath.Join(k.Racine, "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	ecrire := func(v string) {
		if err := os.WriteFile(filepath.Join(k.Racine, "system", "version.txt"), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dossierMaj(k), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dossierMaj(k), "Athanor-9.9.9.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ecrire("0.0.1-autre")
	nettoyerMaj(k)
	if _, err := os.Stat(exe); err != nil {
		t.Fatal("removed under another version:", err)
	}
	ecrire(versionAthanor)
	// held open for a while: on Windows the first tries fail like with the running executable
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(100 * time.Millisecond); f.Close() }()
	nettoyerMaj(k)
	if _, err := os.Stat(dossierMaj(k)); !os.IsNotExist(err) {
		t.Fatal("logs/maj still there:", err)
	}
}
