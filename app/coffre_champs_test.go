package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestCoffreChamps: a secret with its fields (an SMTP account): each field reaches the pilot's file and the Compose
// .env beside the password, under its own name; a variable granted by that name wins over the field; a field
// changed in the vault file no longer opens the value; new fields seal it again.
func TestCoffreChamps(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	champs := []ChampCoffre{{"SMTP_HOST", "smtp.exemple.fr"}, {"SMTP_PORT", "587"}, {"SMTP_FROM", "L'atelier <contact@exemple.fr>"}}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Boutique SMTP", Famille: "secret", Type: "smtp", Portee: "code", Champs: champs}, "mdp-temoin")
	if err != nil {
		t.Fatal(err)
	}
	p := Projet{Nom: "m-champs", Dossier: filepath.Join(t.TempDir(), "m-champs")}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	env := s.acces.Projet(p.Nom).Env
	if _, err := s.acces.Ajouter(p.Nom, e, env, "SMTP_PASSWORD", ""); err != nil {
		t.Fatal(err)
	}
	recu := func() (map[string]string, EtatInjection) {
		lignes, etat, _ := s.planInjection(p, nil)
		m := map[string]string{}
		for _, l := range lignes {
			m[l.nom] = l.valeur
		}
		return m, etat
	}
	if m, etat := recu(); etat.Etat != "ok" || m["SMTP_PASSWORD"] != "mdp-temoin" || m["SMTP_HOST"] != "smtp.exemple.fr" || m["SMTP_PORT"] != "587" ||
		m["SMTP_FROM"] != "L'atelier <contact@exemple.fr>" || len(etat.Ignorees) != 0 {
		t.Fatalf("the pilot's file: %+v %+v", m, etat)
	}
	if l := s.variablesAcces(s.acces.Projet(p.Nom).Acces[0]); !slices.Equal(l, []string{"SMTP_PASSWORD", "SMTP_HOST", "SMTP_PORT", "SMTP_FROM"}) {
		t.Errorf("names the pilot is told: %v", l)
	}
	// a Compose .env holds no quote: said, never cut; without it the fields are there
	if _, err := s.fichierEnvCompose(p.Nom, env); err == nil {
		t.Error("a field with a quote went into a Compose .env")
	}
	champs[2].Valeur = "Atelier <contact@exemple.fr>"
	if _, err := s.coffre.ModifierMeta(e.ID, e.Nom, "smtp", "", "code", &champs, nil); err != nil {
		t.Fatal(err)
	}
	if v, err := s.fichierEnvCompose(p.Nom, env); err != nil || v != "SMTP_FROM='Atelier <contact@exemple.fr>'\nSMTP_HOST='smtp.exemple.fr'\nSMTP_PASSWORD='mdp-temoin'\nSMTP_PORT='587'\n" {
		t.Errorf("the Compose .env: %q %v", v, err)
	}
	// new fields sealed the value again: it still opens
	if v, err := s.coffre.Valeur(e.ID); err != nil || v != "mdp-temoin" {
		t.Fatalf("the value after new fields: %q %v", v, err)
	}
	// a variable granted by a field's own name wins over the field, and the panel says the field was left out
	port, err := s.coffre.Ajouter(EntreeCoffre{Nom: "PORT_PROPRE", Famille: "variable", Type: "autre"}, "2525")
	if err == nil {
		_, err = s.acces.Ajouter(p.Nom, port, env, "SMTP_PORT", "")
	}
	if err != nil {
		t.Fatal(err)
	}
	if m, etat := recu(); m["SMTP_PORT"] != "2525" || !slices.Equal(etat.Ignorees, []string{"SMTP_PORT"}) {
		t.Errorf("a granted variable against a field: %+v %+v", m, etat)
	}
	// a field changed in the file: the value no longer opens
	s.coffre.mu.Lock()
	i := s.coffre.indice(e.ID)
	s.coffre.f.Entrees[i].Champs[0].Valeur = "pirate.exemple.net"
	s.coffre.mu.Unlock()
	if _, err := s.coffre.Valeur(e.ID); err == nil {
		t.Error("a field changed in the file still opens the value")
	}
	s.coffre.mu.Lock()
	s.coffre.f.Entrees[i].Champs[0].Valeur = "smtp.exemple.fr"
	s.coffre.mu.Unlock()
	// fields: a secret's only, valid names each once, one line
	for _, mauvais := range [][]ChampCoffre{{{"smtp_host", "x"}}, {{"SMTP_HOST", "a"}, {"SMTP_HOST", "b"}}, {{"SMTP_HOST", "a\nb"}}, {{"PATH", "x"}}} {
		if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "x", Famille: "secret", Type: "smtp", Portee: "code", Champs: mauvais}, "v"); err == nil {
			t.Errorf("fields accepted: %+v", mauvais)
		}
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "y", Famille: "variable", Type: "autre", Champs: champs}, "v"); err == nil {
		t.Error("a variable with fields")
	}
}
