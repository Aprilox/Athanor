package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func projetLivreEssai(t *testing.T) Projet {
	t.Helper()
	d := t.TempDir()
	p := Projet{Nom: "livre", Dossier: d}
	ecrireEssai(t, filepath.Join(p.Atelier(), "REQUEST.md"), "# Request\n\nA small app.\n")
	ecrireEssai(t, filepath.Join(p.Atelier(), "PLAN.md"), "# Plan\n\n- [x] M1 : Core\n")
	ecrireEssai(t, filepath.Join(p.Atelier(), "FINAL_REPORT.md"), "# Final Report\nSTATUS: DONE\n")
	return p
}

// A delivered project: what came after the delivery reopens it, never twice for the same thing.
func TestNouveauDepuisLivraison(t *testing.T) {
	p := projetLivreEssai(t)
	if n := nouveauDepuisLivraison(p.Dossier); !n.Vide() {
		t.Fatalf("nothing new (dates): %+v", n)
	}
	noterLivraison(p.Dossier)
	if n := nouveauDepuisLivraison(p.Dossier); !n.Vide() || n.Le == "" {
		t.Fatalf("nothing new (record): %+v", n)
	}
	// a note
	if err := p.AjouterRemarque("Add a dark mode", "## Notes", "(notes)"); err != nil {
		t.Fatal(err)
	}
	n := nouveauDepuisLivraison(p.Dossier)
	if !n.Demande || n.Plan != 0 || len(n.Ajouts) != 2 || !strings.Contains(strings.Join(n.Ajouts, "|"), "Add a dark mode") {
		t.Fatalf("the note: %+v", n)
	}
	// a milestone added by hand, unticked
	noterLivraison(p.Dossier)
	ecrireEssai(t, filepath.Join(p.Atelier(), "PLAN.md"), "# Plan\n\n- [x] M1 : Core\n- [ ] M2 : Export\n")
	if n := nouveauDepuisLivraison(p.Dossier); n.Demande || n.Plan != 1 {
		t.Fatalf("the milestone: %+v", n)
	}
	// delivered with that line still unticked: the same line never reopens it again
	noterLivraison(p.Dossier)
	if n := nouveauDepuisLivraison(p.Dossier); !n.Vide() {
		t.Fatalf("delivered against it: %+v", n)
	}
	// not delivered: nothing to reopen
	ecrireEssai(t, filepath.Join(p.Atelier(), "FINAL_REPORT.md"), "STATUS: REOPENED\n")
	ecrireEssai(t, filepath.Join(p.Atelier(), "REQUEST.md"), "changed\n")
	if n := nouveauDepuisLivraison(p.Dossier); !n.Vide() {
		t.Fatalf("not delivered: %+v", n)
	}
}

// Delivered before the record existed: REQUEST.md newer than the final report (by more than a minute).
func TestNouveauDepuisLivraisonDates(t *testing.T) {
	p := projetLivreEssai(t)
	vieux := time.Now().Add(-time.Hour)
	for _, f := range []string{"REQUEST.md", "PLAN.md", "FINAL_REPORT.md"} {
		_ = os.Chtimes(filepath.Join(p.Atelier(), f), vieux, vieux)
	}
	if n := nouveauDepuisLivraison(p.Dossier); !n.Vide() {
		t.Fatalf("%+v", n)
	}
	_ = p.AjouterRemarque("One more level", "## Notes", "(notes)")
	if n := nouveauDepuisLivraison(p.Dossier); !n.Demande || n.Le == "" {
		t.Fatalf("the note after the delivery: %+v", n)
	}
}

// The pilot reopens it: the final report says REOPENED, the note becomes a milestone of the plan.
func TestRouvrirApresLivraison(t *testing.T) {
	p := projetLivreEssai(t)
	noterLivraison(p.Dossier)
	pl := &pilote{projet: p.Dossier, etat: p.Atelier(), prive: t.TempDir()}
	if pl.rouvrirApresLivraison() {
		t.Fatal("nothing new: reopened")
	}
	_ = p.AjouterRemarque("Add a dark mode", "## Notes", "(notes)")
	if !pl.rouvrirApresLivraison() {
		t.Fatal("the note did not reopen it")
	}
	rap := lire(t, filepath.Join(p.Atelier(), "FINAL_REPORT.md"))
	plan := lire(t, filepath.Join(p.Atelier(), "PLAN.md"))
	if strings.Contains(rap, "STATUS: DONE") || !strings.Contains(rap, "STATUS: REOPENED (since the delivery: REQUEST.md changed") {
		t.Fatalf("final report: %q", rap)
	}
	if !strings.Contains(plan, "- [ ] Resume: what changed in .athanor/REQUEST.md since the delivery of ") || !strings.Contains(plan, "New in REQUEST.md: - ") || !strings.Contains(plan, "Add a dark mode") {
		t.Fatalf("plan: %q", plan)
	}
	if projetLivre(p) {
		t.Fatal("still delivered")
	}
}

// Start on a delivered project to which nothing came: said, instead of a pilot that ends at once.
func TestDemarrerLivreRienDeNeuf(t *testing.T) {
	p := projetLivreEssai(t)
	noterLivraison(p.Dossier)
	s := nouveauServeur(Config{Kit: t.TempDir()})
	if _, err := s.demarrerPilote(p); err == nil || err.Error() != texteLivreRienDeNeuf {
		t.Fatalf("delivered, nothing new: %v", err)
	}
	if !strings.Contains(lireI18nEssai(t), `"`+texteLivreRienDeNeuf+`"`) {
		t.Fatal("not translated")
	}
}

func lireI18nEssai(t *testing.T) string {
	t.Helper()
	b, err := statique.ReadFile("static/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
