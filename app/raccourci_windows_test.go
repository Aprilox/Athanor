package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The default folder comes from Windows itself (no PowerShell: the window never waits for it).
func TestRaccourciDefautWindows(t *testing.T) {
	debut := time.Now()
	d := dossierRaccourciDefaut()
	if d == "" || !filepath.IsAbs(d) {
		t.Fatalf("default folder: %q", d)
	}
	if time.Since(debut) > 5*time.Second {
		t.Fatalf("the default folder took %s", time.Since(debut))
	}
}

// A command that does not answer is stopped (PowerShell never keeps the window waiting).
func TestExecuterAvecDelai(t *testing.T) {
	if os.Getenv("ATHANOR_TEST_DORMIR") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestExecuterAvecDelai$")
	c.Env = append(os.Environ(), "ATHANOR_TEST_DORMIR=1")
	debut := time.Now()
	if err := executerAvecDelai(c, 2*time.Second); err == nil {
		t.Fatal("no error for a command that never ends")
	}
	if time.Since(debut) > 20*time.Second {
		t.Fatalf("stopped after %s", time.Since(debut))
	}
	ok := exec.Command(os.Args[0], "-test.run=^$")
	if err := executerAvecDelai(ok, 30*time.Second); err != nil {
		t.Fatalf("a quick command: %v", err)
	}
}
