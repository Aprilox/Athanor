package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestUnSeulDeploiement (M281, M293): a project has one deployment. A second name is refused; an older project
// keeps its several sites, and the route's {courant} makes another one of them the project's deployment: the
// route then reads and writes that site only, and the choice survives a restart.
func TestUnSeulDeploiement(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	p := fauxProjet(t, t.TempDir(), "demo", 3, 1)
	s.mu.Lock()
	s.config.Projets = []Projet{p}
	s.mu.Unlock()
	if err := s.acces.DefinirEnvironnement("neuf", "site", envDefaut()); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.DefinirEnvironnement("neuf", "autre", envDefaut()); !errors.Is(err, errEnvUnSeul) {
		t.Errorf("a second deployment: %v", err)
	}
	for _, nom := range []string{"dev", "staging", "prod"} { // written by an older version
		e := envDefaut()
		e.URL = "https://" + nom + ".exemple.fr"
		if err := s.acces.definirAncien("demo", nom, e); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, "/api/projets/demo/deploiement", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	// what the tab reads: the current site, every site by its internal name, the state, the cautious ones
	vue := func(v map[string]any) string {
		sites, _ := v["sites"].(map[string]any)
		d, _ := v["deploiement"].(map[string]any)
		urls := []string{}
		for _, nom := range []string{"dev", "staging", "prod"} {
			site, _ := sites[nom].(map[string]any)
			urls = append(urls, fmt.Sprint(site["url"]))
		}
		return fmt.Sprint(v["courant"], " ", d["url"], " ", len(sites), " ", urls, " ", v["prudence"], " ", v["relais"])
	}
	const depart = "dev https://dev.exemple.fr 3 [https://dev.exemple.fr https://staging.exemple.fr https://prod.exemple.fr] [prod] <nil>"
	if code, v := appel("GET", ""); code != 200 || vue(v) != depart {
		t.Fatalf("GET: %d %s", code, vue(v))
	}
	if code, v := appel("POST", `{"courant":"inconnu"}`); code != 400 || v["erreur"] == nil {
		t.Errorf("an unknown site: %d %v", code, v)
	}
	if code, v := appel("GET", ""); code != 200 || vue(v) != depart {
		t.Errorf("after the refusal: %d %s", code, vue(v))
	}
	// with {courant}, nothing but the choice: the other fields of the body are not written
	choisi := "prod https://prod.exemple.fr 3 [https://dev.exemple.fr https://staging.exemple.fr https://prod.exemple.fr] [prod] <nil>"
	if code, v := appel("POST", `{"courant":"prod","url":"https://ailleurs.exemple.fr"}`); code != 200 || vue(v) != choisi {
		t.Fatalf("choosing prod: %d %s", code, vue(v))
	}
	// the route now writes that site, the others stay as they are
	if code, v := appel("POST", `{"serveurs":[],"url":"https://neuf.exemple.fr","plan":"nginx","moment":"livraison"}`); code != 200 ||
		vue(v) != "prod https://neuf.exemple.fr 3 [https://dev.exemple.fr https://staging.exemple.fr https://neuf.exemple.fr] [prod] <nil>" {
		t.Errorf("saving the chosen site: %d %s", code, vue(v))
	}
	s.chargerAcces() // a restart
	if nom := s.deploiementProjet("demo"); nom != "prod" {
		t.Errorf("after a restart: %s", nom)
	}
}
