package main

import (
	"context"
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

// TestTachesProjetRetireEtInterrompues (M312c): a removed project's running Servers task is stopped (another
// project's goes on); a task cut by Athanor's stop is closed at the next start, an ended one untouched, and the
// first deployment it was making waits to be started again.
func TestTachesProjetRetireEtInterrompues(t *testing.T) {
	s := serveurCoffreTest(t)
	demo := Projet{Nom: "demo312", Dossier: filepath.Join(t.TempDir(), "demo312")}
	s.mu.Lock()
	s.config.Projets = []Projet{demo}
	s.mu.Unlock()
	t.Cleanup(func() { _ = os.RemoveAll(dossierTaches("m312")); oublierRelais("demo312"); oublierRelais("autre312") })
	en := map[string]*tacheEnCours{}
	for id, env := range map[string]string{"t312a": "demo312", "t312b": "autre312"} {
		ctx, annuler := context.WithCancelCause(context.Background())
		en[id] = &tacheEnCours{t: TacheServeurs{ID: id, Env: env, Serveurs: []string{"m312"}}, hache: "h" + id, annuler: annuler, ctx: ctx}
		tachesEnCours.Lock()
		tachesEnCours.m[id] = en[id]
		tachesEnCours.Unlock()
		t.Cleanup(func() { annuler(nil); tachesEnCours.Lock(); delete(tachesEnCours.m, id); tachesEnCours.Unlock() })
	}
	ecrireRelais("demo312", RelaisJalon{Etat: "preparation", Premier: "t312a"})
	ecrireRelais("autre312", RelaisJalon{Etat: "preparation", Premier: "t312b"})
	if err := s.retirerProjet(demo); err != nil {
		t.Fatal(err)
	}
	if !en["t312a"].arretee() || en["t312b"].arretee() {
		t.Errorf("after the removal: the project's task stopped %v (want true), another project's %v (want false)", en["t312a"].arretee(), en["t312b"].arretee())
	}
	if _, reste := lireRelais()["demo312"]; reste {
		t.Error("the removed project's relay is still there")
	}

	for _, x := range []TacheServeurs{{ID: "t312c", Etat: "preparation"}, {ID: "t312d", Etat: "finie", Fin: "2026-11-19T00:00:00Z"}, {ID: "t312e", Etat: "preparation"}} {
		x.Serveurs = []string{"m312"}
		if err := enregistrerTache(x); err != nil {
			t.Fatal(err)
		}
	}
	ecrireRelais("demo312", RelaisJalon{Etat: "preparation", Premier: "t312c", Commit: "abc"})
	ecrireRelais("autre312", RelaisJalon{Etat: "preparation", Premier: "t312d", Preparation: &PreparationRelais{Tache: "t312e"}})
	s.fermerTachesInterrompues()
	c, d := tacheParID("m312", "t312c"), tacheParID("m312", "t312d")
	if c.Etat != "arretee" || c.Fin == "" || len(c.Historique) != 1 || c.Historique[0].Detail != tacheInterrompue {
		t.Errorf("the task cut by Athanor's stop: %+v", c)
	}
	if d.Etat != "finie" || len(d.Historique) != 0 {
		t.Errorf("an ended task was touched: %+v", d)
	}
	if r := lireRelais(); r["demo312"].Etat != "attente" || r["demo312"].Commit != "abc" || r["autre312"].Etat != "preparation" || r["autre312"].Preparation.Tache != "" {
		t.Errorf("relays after the start: %+v, %+v", r["demo312"], r["autre312"])
	}
	s.fermerTachesInterrompues() // a second start changes nothing more
	if c2 := tacheParID("m312", "t312c"); len(c2.Historique) != 1 || c2.Fin != c.Fin {
		t.Errorf("closed twice: %+v", c2)
	}
}
