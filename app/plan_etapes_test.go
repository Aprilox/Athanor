package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A large request cut into stages: the stage lines are recognised (only with their "S" identifier), give
// the stage under way to the pages, and are never handed to a parallel worker.
func TestPlanEnEtapes(t *testing.T) {
	plan := analyserPlan("# Plan\n\n- [x] M1 : Skeleton\n- [ ] M2 : Stage 2 — the forest level\n" +
		"- [ ] S2: Stage 2/4 — The vault. Goal: shared secrets. Acceptance: the vault tests pass.\n" +
		"- [ ] S3: Stage 3/4 — The bus\n- [ ] s4 : stage 4/4 — Missions\n")
	if len(plan) != 5 || plan[0].Etape || plan[1].Etape || !plan[2].Etape || !plan[3].Etape || !plan[4].Etape {
		t.Fatalf("stage lines: %+v", plan)
	}
	if plan[2].ID != "S2" {
		t.Fatalf("identifier: %q", plan[2].ID)
	}
	if e, n := etapesPlan(plan); e != 1 || n != 4 {
		t.Fatalf("before any stage line: %d/%d", e, n)
	}
	plan[2].Fait = true
	if e, n := etapesPlan(plan); e != 2 || n != 4 {
		t.Fatalf("one stage line ticked: %d/%d", e, n)
	}
	if e, n := etapesPlan(plan[:2]); e != 0 || n != 0 {
		t.Fatalf("no stage: %d/%d", e, n)
	}
	// no total written: as many stages as lines, plus stage 1
	if e, n := etapesPlan(analyserPlan("- [ ] M1 : a\n- [ ] S2: Stage 2 — b\n")); e != 1 || n != 2 {
		t.Fatalf("without a total: %d/%d", e, n)
	}

	// parallel mode: a stage line whose dependencies are met is still left to the lead
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "PLAN.md"), []byte("- [x] M1 : a\n- [x] M2 : b — Depends on: M1.\n- [ ] S2: Stage 2/3 — c\n- [ ] M9 : e — Depends on: M1.\n"), 0o644)
	prets := (&pilote{etat: d}).jalonsPretsParallele()
	if len(prets) != 1 || prets[0][0] != "M9" {
		t.Fatalf("ready for the workers: %v", prets)
	}

	// the examples of the instructions are lines the pilot recognises
	re := regexp.MustCompile("`- \\[ \\] (S\\d+:[^`]*)`")
	for _, f := range []string{"guides/FRAMING.md", ".claude/agents/architect.md", "CLAUDE.md"} {
		b, err := os.ReadFile(filepath.Join("..", "template", f))
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindAllSubmatch(b, -1)
		if len(m) == 0 {
			t.Fatalf("%s: no stage line example", f)
		}
		for _, x := range m {
			if !reEtapePlan.Match(x[1]) {
				t.Fatalf("%s: %q is not recognised", f, x[1])
			}
		}
	}
}
