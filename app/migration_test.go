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

func TestEquipeArbre(t *testing.T) {
	j := &Journal{max: 1000}
	j.nouveauCycle()
	chef := []string{
		`{"type":"system","subtype":"init","model":"claude-fable-5-1"}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"model":"claude-fable-5-1","usage":{"input_tokens":10,"cache_read_input_tokens":50000},"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"subagent_type":"architect","description":"Requirements and plan","prompt":"Write the plan."}}]}}`,
		`{"type":"assistant","parent_tool_use_id":"t1","message":{"model":"claude-sonnet-5-5","usage":{},"content":[{"type":"tool_use","id":"t2","name":"Write","input":{"file_path":"/p/.athanor/PLAN.md"}}]}}`,
		`{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"Plan written: 5 milestones."}]}]},"tool_use_result":{"totalDurationMs":61000,"totalTokens":9000,"totalToolUseCount":4}}`,
	}
	for _, l := range chef {
		j.pousserCycle(l, "")
	}
	ouvrier := []string{
		`{"type":"system","subtype":"init","model":"claude-fable-5-1"}`,
		`{"type":"assistant","parent_tool_use_id":null,"message":{"usage":{"input_tokens":10,"cache_read_input_tokens":30000},"content":[{"type":"tool_use","id":"x1","name":"Agent","input":{"subagent_type":"executor","description":"Run the tests","prompt":"Run them, failures only."}}]}}`,
		`{"type":"assistant","parent_tool_use_id":"x1","message":{"model":"claude-haiku-5-5","usage":{},"content":[{"type":"text","text":"0 failure."}]}}`,
	}
	for _, l := range ouvrier {
		j.pousserCycle(l, "M2")
	}
	par := map[string]Agent{}
	for _, a := range j.Agents() {
		par[a.ID] = a
	}
	if a := par["t1"]; a.Parent != "chef" || a.Nom != "architect" || a.Consigne != "Write the plan." || a.Rapport != "Plan written: 5 milestones." || a.Statut != "termine" || a.DureeMs != 61000 || a.Modele != "claude-sonnet-5-5" || a.Outils != 4 {
		t.Fatalf("architect: %+v", a)
	}
	if c := par["chef"]; c.Statut != "travaille" || c.Modele != "claude-fable-5-1" || c.Contexte != 50010 {
		t.Fatalf("lead: %+v", c)
	}
	if o := par["ouvrier-M2"]; o.Genre != "ouvrier" || o.Jalon != "M2" || o.Statut != "attend" {
		t.Fatalf("worker: %+v", o)
	}
	if x := par["x1"]; x.Parent != "ouvrier-M2" || x.Nom != "executor" || x.Mission != "Run the tests" || x.Statut != "travaille" || x.Bulle != "0 failure." || x.Modele != "claude-haiku-5-5" {
		t.Fatalf("executor of the worker: %+v", x)
	}
}

func TestEquipeAttenteParallele(t *testing.T) {
	j := &Journal{max: 1000}
	j.nouveauCycle()
	for _, l := range []string{
		`{"type":"assistant","parent_tool_use_id":null,"message":{"content":[{"type":"tool_use","id":"a","name":"Agent","input":{"subagent_type":"executor","description":"x","prompt":"x"}},{"type":"tool_use","id":"b","name":"Agent","input":{"subagent_type":"tester","description":"y","prompt":"y"}}]}}`,
		`{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"done"}]}}`,
	} {
		j.pousserCycle(l, "")
	}
	if c := j.agents["chef"]; c.Statut != "attend" || c.Activite != "Waits for sub-agent tester" {
		t.Fatalf("lead should still wait for tester: %+v", c)
	}
	j.pousserCycle(`{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"b","content":"ok"}]}}`, "")
	if c := j.agents["chef"]; c.Statut != "travaille" || c.Activite != "Reviews the report of tester" {
		t.Fatalf("lead back to work: %+v", c)
	}
}
