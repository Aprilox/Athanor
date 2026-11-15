package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// TestInfraAttente (note 12h28 §3): locked, a step that needs the infra key waits (bell alert) while the code
// keys keep working, then resumes at the unlock with its value; a cancelled step stops waiting.
func TestInfraAttente(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	if err := s.coffre.ChangerMotDePasse("", mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller(mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH_WEB", Famille: "secret", Type: "ssh"}, "cle-temoin")
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.coffre.Ajouter(EntreeCoffre{Nom: "PORT", Famille: "variable", Type: "autre"}, "8080")
	if err != nil {
		t.Fatal(err)
	}
	s.coffre.Verrouiller()
	// the window open: its own request is answered at once
	if _, err := s.valeurInfra(context.Background(), cle.ID, "servers", "web-1"); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("window open: %v", err)
	}
	s.mu.Lock()
	s.session = "" // the window locked: background work waits
	s.mu.Unlock()
	alerte := func() bool {
		a, _ := s.alertesHub(nil)
		return slices.ContainsFunc(a, func(x AlerteHub) bool { return x.Empreinte == "infra-attente-servers-web-1" })
	}
	attendre := func(f func() bool) bool {
		for range 100 {
			if f() {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}

	fini := make(chan string, 1)
	go func() { v, _ := s.valeurInfra(context.Background(), cle.ID, "servers", "web-1"); fini <- v }()
	if !attendre(alerte) {
		t.Fatal("no bell alert for the waiting step")
	}
	if v, err := s.coffre.Valeur(code.ID); err != nil || v != "8080" {
		t.Fatalf("the code keys stopped while locked: %q %v", v, err)
	}
	select {
	case v := <-fini:
		t.Fatalf("the step did not wait: %q", v)
	case <-time.After(100 * time.Millisecond):
	}
	s.deverrouillerCoffre(mdpPreuveTest)
	select {
	case v := <-fini:
		if v != "cle-temoin" {
			t.Fatalf("resumed with %q", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the step did not resume at the unlock")
	}
	if !attendre(func() bool { return !alerte() }) {
		t.Fatal("alert left after the resume")
	}

	// a step cancelled while it waits: its error, no alert left
	s.coffre.Verrouiller()
	ctx, annuler := context.WithCancel(context.Background())
	erreur := make(chan error, 1)
	go func() { _, err := s.valeurInfra(ctx, cle.ID, "servers", "web-1"); erreur <- err }()
	if !attendre(alerte) {
		t.Fatal("no alert for the second wait")
	}
	annuler()
	if err := <-erreur; !errors.Is(err, errCoffreVerrouille) || !attendre(func() bool { return !alerte() }) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

// TestInfraMissionAccordee: a running mission granted the infra key for 4 hours keeps it open through the
// automatic lock until its deadline; the padlock still closes everything.
func TestInfraMissionAccordee(t *testing.T) {
	s := serveurCoffreTest(t)
	maintenant := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	avant := horlogeVerrou
	horlogeVerrou = func() time.Time { return maintenant }
	t.Cleanup(func() { horlogeVerrou = avant })
	ouvrir := func() {
		avecMdpPreuve(s)
		s.mu.Lock()
		s.actifLe = maintenant
		s.mu.Unlock()
		if err := s.coffre.Deverrouiller(mdpPreuveTest); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.coffre.ChangerMotDePasse("", mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	ouvrir()
	if err := enregistrerMission(Mission{ID: "mission01", Etat: "lancee", Politique: "prod"}); err != nil {
		t.Fatal(err)
	}
	if err := garderInfraMission("mission01", "4h", maintenant); err != nil {
		t.Fatal(err)
	}
	if !s.verifierVerrouAuto(maintenant.Add(61*time.Minute)) || !s.etatCoffre().Infra {
		t.Fatal("the granted mission lost the infra key at the automatic lock")
	}
	if s.verifierVerrouAuto(maintenant.Add(3 * time.Hour)); !s.etatCoffre().Infra {
		t.Fatal("infra key closed before the deadline")
	}
	if s.verifierVerrouAuto(maintenant.Add(4*time.Hour + time.Minute)); s.etatCoffre().Infra {
		t.Fatal("infra key still open after the deadline")
	}

	// without a grant (the mission ended), the automatic lock closes it; with one, the padlock closes it
	ouvrir()
	if _, err := modifierMission("mission01", func(m *Mission) error { m.Etat = "terminee"; return nil }); err != nil {
		t.Fatal(err)
	}
	if !s.verifierVerrouAuto(maintenant.Add(2*time.Hour)) || s.etatCoffre().Infra {
		t.Fatal("an ended mission kept the infra key")
	}
	ouvrir()
	if _, err := modifierMission("mission01", func(m *Mission) error { m.Etat = "lancee"; m.InfraJusqua = "mission"; return nil }); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	r := httptest.NewRequest("POST", "/api/verrouiller", nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	r.AddCookie(cookieSessionTest)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || s.etatCoffre().Infra {
		t.Fatalf("the padlock left the infra key open: %d", w.Code)
	}
}
