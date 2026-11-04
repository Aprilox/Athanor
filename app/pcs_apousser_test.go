package main

import (
	"strings"
	"testing"
)

// A milestone another computer built whose push did not go yet stays its own: never built a second time
// (the CI's race: its push refused by a push of the other at the same moment).
func TestAPousserPC(t *testing.T) {
	moi := MachinePC{ID: "aaaaaaaaaaaa", Nom: "Moi"}
	plan := analyserPlan("# Plan\n\n- [x] M1 : One\n- [ ] M2 : Two\n- [ ] M3 : Three\n")
	b := PresencePC{ID: "bbbbbbbbbbbb", Nom: "B", EnLigne: true, Pilote: true, Jalon: "M3", APousser: []string{"M2"}}
	if j, att, raison := choisirJalonPC(plan, moi, []PresencePC{b}, ""); !att || j.ID != "" || !strings.Contains(raison, "B") {
		t.Fatalf("M2 built by B, not pushed yet: %q %v %q", j.ID, att, raison)
	}
	b.EnLigne = false // gone: what it kept is free again
	if j, att, _ := choisirJalonPC(plan, moi, []PresencePC{b}, ""); att || j.ID != "M2" {
		t.Fatalf("B gone: %q %v", j.ID, att)
	}
	d := t.TempDir()
	noterAPousserPC(d, "M2", true)
	noterAPousserPC(d, "M2", true)
	noterAPousserPC(d, "M4", true)
	if v := lireAPousserPC(d); strings.Join(v, ",") != "M2,M4" {
		t.Fatalf("%v", v)
	}
	noterAPousserPC(d, "M5", false) // a push went: everything is on GitHub
	if v := lireAPousserPC(d); len(v) != 0 {
		t.Fatalf("%v", v)
	}
}
