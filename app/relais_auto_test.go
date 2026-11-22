package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeuilRelaisAuto(t *testing.T) {
	d := t.TempDir()
	ecrire := func(lignes ...string) string {
		f := filepath.Join(d, "stats.tsv")
		_ = os.WriteFile(f, []byte("horodatage\tcycle\tmodele\tetat\tminutes\tminutes_api\ttours\tcout_usd\tentree\tlu\tecrit\tsortie\tcontexte_max\tcommit\testimation\toptions\n"+strings.Join(lignes, "\n")+"\n"), 0o644)
		return f
	}
	ligne := func(cycle, etat string, ctxK int, options string) string {
		return fmt.Sprintf("2026-11-22T10:00:00\t%s\topus\t%s\t10\t3\t30\t1.5\t10\t1\t1\t1\t%d\tabc\t0\t%s", cycle, etat, ctxK*1000, options)
	}
	if k := seuilRelaisAuto(filepath.Join(d, "absent.tsv")); k != seuilRelaisDefautK {
		t.Errorf("no stats: %d", k)
	}
	// small milestones only (60k): the floor
	var petits []string
	for i := 1; i <= 10; i++ {
		petits = append(petits, ligne(fmt.Sprint(i), "ok", 60, "ponytail"))
	}
	if k := seuilRelaisAuto(ecrire(petits...)); k != seuilRelaisMinK {
		t.Errorf("small milestones: %d, want %d", k, seuilRelaisMinK)
	}
	// too few milestones yet (the framing and the workers do not count): the default
	if k := seuilRelaisAuto(ecrire(ligne("1", "ok", 150, "cadrage"), ligne("2", "ok", 90, ""), ligne("2·M3", "ok", 90, ""))); k != seuilRelaisDefautK {
		t.Errorf("few milestones: %d", k)
	}
	// each milestone relayed once at 110k then finished at 100k: (110-30)+100 = 180k needed, +15 % = 207 → 210
	var gros []string
	for i := 1; i <= 10; i++ {
		gros = append(gros, ligne(fmt.Sprint(2*i), "relais", 110, "ponytail"), ligne(fmt.Sprint(2*i+1), "ok", 100, "ponytail"))
	}
	if k := seuilRelaisAuto(ecrire(gros...)); k != 210 {
		t.Errorf("relayed milestones: %d, want 210", k)
	}
	// huge milestones: the ceiling
	var enormes []string
	for i := 1; i <= 10; i++ {
		enormes = append(enormes, ligne(fmt.Sprint(3*i), "relais", 200, ""), ligne(fmt.Sprint(3*i+1), "relais", 200, ""), ligne(fmt.Sprint(3*i+2), "ok", 150, ""))
	}
	if k := seuilRelaisAuto(ecrire(enormes...)); k != seuilRelaisMaxK {
		t.Errorf("huge milestones: %d, want %d", k, seuilRelaisMaxK)
	}
}
