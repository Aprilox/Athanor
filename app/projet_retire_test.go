package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjetRetireApprobations (owner's note of 2026-11-17): a removed project's pending approvals left the
// bell undecidable (their project gone); now they are refused with it, and its "never ask again" rules go.
// Another project's approval, and a mission's launch, stay pending.
func TestProjetRetireApprobations(t *testing.T) {
	s := serveurCoffreTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	demo, autre := Projet{Nom: "demo", Dossier: filepath.Join(d, "demo")}, Projet{Nom: "autre", Dossier: filepath.Join(d, "autre")}
	for _, p := range []Projet{demo, autre} {
		if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo, autre}
	s.mu.Unlock()
	demander := func(projet, action string) Approbation {
		t.Helper()
		x, err := s.approbations.Demander(Approbation{Projet: projet, Env: "dev", Agent: "pilote", Action: action, Raison: "essai"})
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	for _, p := range []string{"demo", "autre"} { // a "never ask again" rule for each project
		if _, err := s.approbations.Decider(demander(p, "deployer").ID, "toujours", "fenetre"); err != nil {
			t.Fatal(err)
		}
	}
	acces, mission, ailleurs := demander("demo", "demander_acces"), demander("demo", actionMission), demander("autre", "demander_acces")

	if err := s.retirerProjet(demo); err != nil {
		t.Fatal(err)
	}
	etat := func(id string) string { x, _ := s.approbations.Lire(id); return x.Etat }
	if e := etat(acces.ID); e != "refusee" {
		t.Errorf("the removed project's approval is %q, want refusee", e)
	}
	if e, f := etat(mission.ID), etat(ailleurs.ID); e != "refusee" || f != "attente" { // M301: an old launch request goes with its project
		t.Errorf("the project's old mission launch is %q (want refusee), another project's approval %q (want attente)", e, f)
	}
	l, regles := s.approbations.Liste()
	for _, a := range alertesApprobations(l) {
		if a.Element == "demo" {
			t.Errorf("still in the bell: %+v", a.Approbation)
		}
	}
	if len(regles) != 1 || regles[0].Projet != "autre" {
		t.Errorf("rules after the removal: %+v", regles)
	}
}
