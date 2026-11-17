package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestAncienSiteProduction (M302): a site that is not the project's deployment, marked Production and deployed
// before (by a mission), is said in the Deployment tab's data and in the bell as an information (never the
// relay's failure, and it can be ignored); a site never deployed or not Production says nothing; the information
// goes once the owner chooses the site as the project's deployment.
func TestAncienSiteProduction(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	p := fauxProjet(t, t.TempDir(), "demo", 3, 1)
	s.mu.Lock()
	s.config.Projets = []Projet{p}
	s.mu.Unlock()
	for _, nom := range []string{"dev", "staging", "prod"} { // written by an older version
		e := envDefaut()
		e.URL, e.Production = "https://"+nom+".exemple.fr", nom == "prod"
		if err := s.acces.definirAncien("demo", nom, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.acces.ChoisirDeploiement("demo", "dev"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	anciens := func() string {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/projets/demo/deploiement", nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v struct{ Anciens []string }
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || w.Code != http.StatusOK {
			t.Fatalf("the tab's data: %d %v", w.Code, err)
		}
		return strings.Join(v.Anciens, ",")
	}
	cloche := func() []AlerteHub {
		actives, _ := s.alertesHub(nil)
		return slices.DeleteFunc(actives, func(a AlerteHub) bool { return a.Raison != texteAncienSite })
	}

	if a := s.alertesAnciensSites(); len(a) != 0 || anciens() != "" {
		t.Fatalf("a Production site never deployed: %+v, tab %q", a, anciens())
	}
	for _, nom := range []string{"staging", "prod"} { // both were deployed before, by a mission
		t.Cleanup(func() { _ = os.Remove(cheminHistorique("demo", nom)) })
		if err := noterHistorique(Deploiement{ID: "m302" + nom, Projet: "demo", Env: nom, Commit: strings.Repeat("ab12", 10), Etat: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	a := cloche()
	if len(a) != 1 || !a[0].Info || a[0].Element != "demo" || len(a[0].Args) != 1 || a[0].Args[0] != "prod.exemple.fr" || anciens() != "prod" || len(s.alertesRelais()) != 0 {
		t.Fatalf("the older Production site: bell %s, tab %q, relay %+v", enJSON(a), anciens(), s.alertesRelais())
	}
	if b, _ := json.Marshal(a[0]); !strings.Contains(string(b), `"info":true`) || strings.Contains(strings.ToLower(a[0].Raison), "fail") {
		t.Errorf("shown as a failure: %s", b)
	}
	if !ignorerAlerte(s.alertesIgnorables(nil), a[0].Empreinte, true) || len(cloche()) != 0 {
		t.Errorf("the information cannot be ignored: %s", enJSON(cloche()))
	}
	if !ignorerAlerte(s.alertesIgnorables(nil), a[0].Empreinte, false) || len(cloche()) != 1 {
		t.Errorf("the information cannot be reopened: %s", enJSON(cloche()))
	}

	if err := s.acces.ChoisirDeploiement("demo", "prod"); err != nil { // chosen: deployed by the project from now on
		t.Fatal(err)
	}
	if a := cloche(); len(a) != 0 || anciens() != "" {
		t.Fatalf("after the choice: bell %s, tab %q", enJSON(a), anciens())
	}
}
