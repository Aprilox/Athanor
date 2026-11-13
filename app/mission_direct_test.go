package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAvancementVoie (M171): the Studio lane of the live page says the milestones done of the plan and, while the pilot
// works, the title of the milestone in progress; a stopped pilot gives no title.
func TestAvancementVoie(t *testing.T) {
	p := Projet{Nom: "p", Dossier: t.TempDir()}
	if err := os.MkdirAll(filepath.Dir(p.Plan()), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := "# Plan\n- [x] M1 : Squelette — Acceptance: ok\n- [x] M2 : Accueil — Acceptance: ok\n- [ ] M3 : Formulaire de contact — Acceptance: ok\n- [ ] M4 : Mise en ligne — Acceptance: ok\n"
	if err := os.WriteFile(p.Plan(), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Serveur{}
	v := VoieMission{Module: "dev", Actuel: "travaille"}
	s.avancementVoie(&v, p, "M3")
	if v.Fait != 2 || v.Total != 4 || v.Titre != "Formulaire de contact" {
		t.Fatalf("working: %+v", v)
	}
	v = VoieMission{Module: "dev", Actuel: "arrete"}
	s.avancementVoie(&v, p, "M3")
	if v.Fait != 2 || v.Total != 4 || v.Titre != "" || v.Activite != "" {
		t.Fatalf("stopped: %+v", v)
	}
}
