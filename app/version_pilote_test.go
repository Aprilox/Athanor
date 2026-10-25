package main

import (
	"os"
	"path/filepath"
	"testing"
)

// After an update while a pilot ran: the app tells the pilot is still on its older version.
func TestVersionPiloteAncienne(t *testing.T) {
	d := t.TempDir()
	p := Projet{Dossier: d}
	at := p.Atelier()
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}
	// started before 2.39.1: no pilot.version
	if a, v := versionPiloteAncienne(p); !a || v != "" {
		t.Fatalf("no file: got %v %q, want older, no version", a, v)
	}
	if err := os.WriteFile(filepath.Join(at, "pilot.version"), []byte("2.38.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a, v := versionPiloteAncienne(p); !a || v != "2.38.3" {
		t.Fatalf("older: got %v %q", a, v)
	}
	if err := os.WriteFile(filepath.Join(at, "pilot.version"), []byte(versionAthanor+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a, _ := versionPiloteAncienne(p); a {
		t.Fatal("same version: flagged as older")
	}
}
