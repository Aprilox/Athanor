package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrerAtelier(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".autopilote", "queue"), 0o755)
	os.WriteFile(filepath.Join(d, ".autopilote", "PLAN.md"), []byte("- [ ] M1 : x\n"), 0o644)
	os.MkdirAll(filepath.Join(d, ".git", "info"), 0o755)
	os.WriteFile(filepath.Join(d, ".git", "info", "exclude"), []byte("# x\n.autopilote/\n.claude/\n"), 0o644)
	// near-empty .athanor with a STOP request made before the migration
	os.MkdirAll(filepath.Join(d, ".athanor"), 0o755)
	os.WriteFile(filepath.Join(d, ".athanor", "STOP"), nil, 0o644)
	p := Projet{Dossier: d}
	p.MigrerAtelier()
	if existe(filepath.Join(d, ".autopilote")) || !existe(p.Plan()) || !existe(filepath.Join(d, ".athanor", "STOP")) {
		t.Fatal("workshop not migrated")
	}
	b, _ := os.ReadFile(filepath.Join(d, ".git", "info", "exclude"))
	if string(b) != "# x\n.athanor/\n.claude/\n" {
		t.Fatalf("exclude: %q", b)
	}
}

func TestMigrerAtelierPiloteVivant(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".autopilote"), 0o755)
	os.WriteFile(filepath.Join(d, ".autopilote", "pilot.pid"), []byte("1"), 0o644)
	os.Chtimes(filepath.Join(d, ".autopilote", "pilot.pid"), time.Now(), time.Now())
	Projet{Dossier: d}.MigrerAtelier()
	if !existe(filepath.Join(d, ".autopilote")) {
		t.Fatal("must not move the workshop of a living pilot")
	}
}

func TestMigrerAtelierDejaFait(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".autopilote"), 0o755)
	os.MkdirAll(filepath.Join(d, ".athanor"), 0o755)
	os.WriteFile(filepath.Join(d, ".athanor", "REQUEST.md"), []byte("x"), 0o644)
	Projet{Dossier: d}.MigrerAtelier()
	if !existe(filepath.Join(d, ".autopilote")) || !existe(filepath.Join(d, ".athanor", "REQUEST.md")) {
		t.Fatal("a real .athanor must never be replaced")
	}
}

func TestCheminLisible(t *testing.T) {
	cas := map[string]string{
		"/mnt/wsl/docker-desktop-bind-mounts/Ubuntu/23c3f/docs/REQUIREMENTS.md": "…/docs/REQUIREMENTS.md",
		"/mnt/c/Users/x/Desktop/montre/.athanor/PLAN.md":                        ".athanor/PLAN.md",
		"src/enemies/enemy.gd":                 "src/enemies/enemy.gd",
		"/tmp/a.txt":                           "/tmp/a.txt",
		`C:\Users\x\Desktop\montre\index.html`: "…/montre/index.html",
	}
	for in, want := range cas {
		if got := cheminLisible(in); got != want {
			t.Errorf("cheminLisible(%q) = %q, want %q", in, got, want)
		}
	}
}
