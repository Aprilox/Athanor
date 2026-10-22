package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The milestones the pilot archived (archives/PLAN_TERMINE.md) still count in the plan, first and done.
func TestPlanAvecArchives(t *testing.T) {
	d := t.TempDir()
	p := Projet{Nom: "x", Dossier: d}
	_ = os.MkdirAll(filepath.Join(d, ".athanor", "archives"), 0o755)
	_ = os.WriteFile(p.Plan(), []byte("# Plan\n\n- [x] M3 : trois\n- [ ] M4 : quatre\n  - Files: a.luau\n"), 0o644)
	_ = os.WriteFile(filepath.Join(d, ".athanor", "archives", "PLAN_TERMINE.md"), []byte("# Completed milestones\n\n## 2026-10-21\n\n- [x] M1 : un\n  - détail\n\n- [x] M2 : deux\n- [x] M3 : trois (copie)\n"), 0o644)
	v := p.LirePlan()
	if len(v) != 4 {
		t.Fatalf("%d milestones: %+v", len(v), v)
	}
	if v[0].ID != "M1" || !v[0].Archive || !v[0].Fait || v[1].ID != "M2" || v[2].ID != "M3" || v[2].Archive || v[3].Fait {
		t.Fatalf("order or flags: %+v", v)
	}
	_ = os.Remove(filepath.Join(d, ".athanor", "archives", "PLAN_TERMINE.md"))
	if len(p.LirePlan()) != 2 {
		t.Fatal("without archives")
	}
}

func TestTitresCycles(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "pilot.log")
	_ = os.WriteFile(f, []byte("[2026-10-22 20:36:12] Cycle 12 succeeded: M8a: Add abilities\r\n[2026-10-22 20:47:08] Cycle 13 finished without a new commit (1/3).\n[2026-10-22 20:47:09] Cycle 13 succeeded: M8b: Add the chassis\n"), 0o644)
	m := titresCycles(f)
	if m["12"] != "M8a: Add abilities" || m["13"] != "M8b: Add the chassis" || len(m) != 2 {
		t.Fatalf("titles: %v", m)
	}
	_ = os.WriteFile(f, []byte("[x] Cycle 14 succeeded: M9: Next\n"), 0o644)
	if m := titresCycles(f); m["14"] != "M9: Next" {
		t.Fatalf("cache not refreshed: %v", m)
	}
}
