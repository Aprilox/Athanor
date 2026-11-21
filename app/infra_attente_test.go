package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

// TestVerrouAutoGardeInfra (note 19h03 §3): the automatic lock locks the interface only. A task then gets the
// infra key at once (no wait, no alert) and closing the remote access leaves it open; the padlock still
// closes the vault, and the audit logs the lock but no secret.
func TestVerrouAutoGardeInfra(t *testing.T) {
	s := serveurCoffreTest(t)
	maintenant := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	avant := horlogeVerrou
	horlogeVerrou = func() time.Time { return maintenant }
	t.Cleanup(func() { horlogeVerrou = avant })
	avecMdpPreuve(s)
	s.mu.Lock()
	s.actifLe = maintenant
	s.mu.Unlock()
	if err := s.coffre.ChangerMotDePasse("", mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller(mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH_WEB", Famille: "secret", Type: "ssh"}, "cle-temoin-verrou")
	if err != nil {
		t.Fatal(err)
	}
	if !s.verifierVerrouAuto(maintenant.Add(61*time.Minute)) || !s.fenetreVerrouillee() {
		t.Fatal("the window did not lock after its delay")
	}
	if !s.etatCoffre().Infra {
		t.Fatal("the automatic lock closed the infra key")
	}
	ctx, annuler := context.WithTimeout(context.Background(), time.Second)
	defer annuler()
	if v, err := s.valeurInfra(ctx, cle.ID, "missions", "mission01"); err != nil || v != "cle-temoin-verrou" {
		t.Fatalf("a task did not get the infra key after the automatic lock: %q %v", v, err)
	}
	if a, _ := s.alertesHub(nil); slices.ContainsFunc(a, func(x AlerteHub) bool { return x.Empreinte == "infra-attente-missions-mission01" }) {
		t.Fatal("a wait alert after the automatic lock")
	}
	s.fermerEcouteDistance() // the remote access closed meanwhile: the window still holds the vault
	if !s.etatCoffre().Infra {
		t.Fatal("closing the remote access closed the vault during the automatic lock")
	}
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(chemin string) int {
		r := httptest.NewRequest("POST", chemin, nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	// the padlock, like the timer, locks the window only (3.1): a task in the background keeps the infra key
	if c := appel("/api/verrouiller"); c != 200 || !s.etatCoffre().Infra || !s.fenetreVerrouillee() {
		t.Fatalf("the padlock closed the infra key or left the window open: %d", c)
	}
	ctx2, annuler2 := context.WithTimeout(context.Background(), time.Second)
	defer annuler2()
	if v, err := s.valeurInfra(ctx2, cle.ID, "missions", "mission02"); err != nil || v != "cle-temoin-verrou" {
		t.Fatalf("a task did not get the infra key after the padlock: %q %v", v, err)
	}
	if j := lireJournalAuditTest(); !strings.Contains(j, "verrouillage automatique") || strings.Contains(j, "cle-temoin-verrou") {
		t.Fatal("a secret in the audit")
	}
}
