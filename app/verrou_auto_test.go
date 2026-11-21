package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestVerrouAuto (note 12h28 §3): the window locks itself after the owner's inactivity (simulated clock), the
// refresh loop never renews the delay, its own actions do; locked, the data is refused and the infra key is
// gone; a longer delay asks for the password.
func TestVerrouAuto(t *testing.T) {
	s := serveurCoffreTest(t)
	maintenant := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	avant := horlogeVerrou
	horlogeVerrou = func() time.Time { return maintenant }
	t.Cleanup(func() { horlogeVerrou = avant })
	mux := http.NewServeMux()
	s.routes(mux)
	ouvrir := func() {
		avecMdpPreuve(s)
		s.mu.Lock()
		s.actifLe = maintenant
		s.mu.Unlock()
	}
	ouvrir()
	if err := s.coffre.ChangerMotDePasse("", mdpPreuveTest); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller(mdpPreuveTest); err != nil || !s.etatCoffre().Infra {
		t.Fatal("infra key not open:", err)
	}
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		v := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	reste := func() float64 {
		t.Helper()
		_, v := appel("GET", "/api/etat", "")
		f, _ := v["verrou_dans"].(float64)
		return f
	}

	if r := reste(); r != 3600 {
		t.Fatalf("1 hour by default, got %v s", r)
	}
	// 59 minutes of refresh loop only: the delay is not renewed
	maintenant = maintenant.Add(59 * time.Minute)
	for range 3 {
		appel("GET", "/api/etat", "")
	}
	if s.verifierVerrouAuto(maintenant) || reste() != 60 {
		t.Fatalf("the refresh loop renewed the delay or locked early: %v s", reste())
	}
	// an action of the owner starts it again
	if c, v := appel("POST", "/api/activite", ""); c != 200 || v["verrou_dans"] != float64(3600) {
		t.Fatalf("activity: %d %v", c, v)
	}
	maintenant = maintenant.Add(61 * time.Minute)
	if !s.verifierVerrouAuto(maintenant) {
		t.Fatal("not locked after the delay")
	}
	if !s.etatCoffre().Infra {
		t.Fatal("the automatic lock closed the infra key (interface only, note 19h03 §3)")
	}
	if _, v := appel("GET", "/api/etat", ""); v["verrouille"] != true || v["verrou_dans"] != float64(-1) {
		t.Fatalf("state once locked: %v", v)
	}
	if c, v := appel("GET", "/api/options", ""); c != http.StatusUnauthorized || v["erreur_key"] != "Locked" {
		t.Fatalf("data served once locked: %d %v", c, v)
	}
	if c, _ := appel("POST", "/api/activite", ""); c != http.StatusUnauthorized {
		t.Fatalf("activity accepted while locked: %d", c)
	}

	// the setting: shorter without the password, longer with it, unknown refused
	ouvrir()
	if c, _ := appel("POST", "/api/options/verrou-auto", `{"valeur":"15m"}`); c != 200 || reste() != 900 {
		t.Fatalf("15 minutes: %d %v", c, reste())
	}
	if c, _ := appel("POST", "/api/options/verrou-auto", `{"valeur":"8h"}`); c == 200 || s.config.VerrouAuto != "15m" {
		t.Fatalf("longer without the password: %d", c)
	}
	if c, _ := appel("POST", "/api/options/verrou-auto", `{"valeur":"zz","mot_de_passe":"`+mdpPreuveTest+`"}`); c != http.StatusBadRequest {
		t.Fatalf("unknown value: %d", c)
	}
	if c, v := appel("POST", "/api/options/verrou-auto", `{"valeur":"jamais","mot_de_passe":"`+mdpPreuveTest+`"}`); c != 200 || v["verrou_auto"] != "jamais" {
		t.Fatalf("never: %d %v", c, v)
	}
	if _, v := appel("GET", "/api/options", ""); v["verrou_auto"] != "jamais" {
		t.Fatalf("options: %v", v["verrou_auto"])
	}
	if s.verifierVerrouAuto(maintenant.Add(100*time.Hour)) || reste() != -1 {
		t.Fatal("never: locked or a timer shown")
	}
}

// TestDelaiVerrouAuto: the harness's shorter delay counts only under ATHANOR_TEST=1, never lengthens a delay
// and never gives a timer to a choice that has none.
func TestDelaiVerrouAuto(t *testing.T) {
	for _, c := range []struct {
		test, env, choix string
		attendu          time.Duration
	}{
		{"", "", "15m", 15 * time.Minute},
		{"", "20s", "15m", 15 * time.Minute},
		{"1", "", "1h", time.Hour},
		{"1", "20s", "15m", 20 * time.Second},
		{"1", "20s", "inconnu", 20 * time.Second},
		{"1", "30m", "15m", 15 * time.Minute},
		{"1", "-5s", "15m", 15 * time.Minute},
		{"1", "bientot", "15m", 15 * time.Minute},
		{"1", "20s", "jamais", 0},
		{"1", "20s", "fermeture", 0},
	} {
		t.Setenv("ATHANOR_TEST", c.test)
		t.Setenv("ATHANOR_VERROU_AUTO", c.env)
		if d := delaiVerrouAuto(c.choix); d != c.attendu {
			t.Errorf("ATHANOR_TEST=%q ATHANOR_VERROU_AUTO=%q %s: %v, expected %v", c.test, c.env, c.choix, d, c.attendu)
		}
	}
}
