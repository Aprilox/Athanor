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

func TestPlanBlocs(t *testing.T) {
	plan := "# Plan\n\n- [x] M1 : a — Depends on: none. Acceptance: ok.\n- [ ] M2 : b — Files: x. Depends on: M1. Acceptance: a.\n  - note 1\n- [ ] M3 : c — Depends on: M1, M2. Acceptance: b.\n- [ ] M4 : d — Depends on: M2. Acceptance: c.\n"
	blocs := decouperPlan(plan)
	if len(blocs) != 7 || !blocs[3].jalon || blocs[3].id != "M2" || len(blocs[3].lignes) != 2 {
		t.Fatalf("blocs: %+v", blocs)
	}
	if assemblerPlan(blocs) != plan {
		t.Fatal("round trip changed the plan")
	}
	j := decouperJalon("M2 : Home page — Files: index.html, js/home.js. Depends on: M1, M4.1. Acceptance: tests green.")
	if j.ID != "M2" || j.Titre != "M2 : Home page" || j.Fichiers != "index.html, js/home.js" || len(j.Depend) != 2 || j.Depend[1] != "M4_1" || j.Acceptation != "tests green." || j.Detail != "" {
		t.Fatalf("jalon: %+v", j)
	}
}

func TestDemandesFaites(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "20261017-001830-01-footer.md"), []byte("# Un beau footer, vraiment\n\nDemandée le 2026-10-17.\n"), 0o644)
	os.WriteFile(filepath.Join(d, "20250101-0930-sans-titre.md"), []byte("du texte sans titre\n# pas un titre\n"), 0o644)
	stamp, date, titre := lireDemandeFaite(d, "20261017-001830-01-footer.md")
	if stamp != "20261017001830" || date != "2026-10-17 00h18" || titre != "Un beau footer, vraiment" {
		t.Fatalf("got %q %q %q", stamp, date, titre)
	}
	stamp, date, titre = lireDemandeFaite(d, "20250101-0930-sans-titre.md")
	if stamp != "20250101093000" || date != "2025-01-01 09h30" || titre != "sans titre" {
		t.Fatalf("old format: got %q %q %q", stamp, date, titre)
	}
	if _, date, titre = lireDemandeFaite(d, "nimporte.md"); date != "" || titre != "nimporte" {
		t.Fatalf("free name: got %q %q", date, titre)
	}
}
