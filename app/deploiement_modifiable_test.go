package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeploiementModifiableArrete (M292, note 16h11 §1): the Deployment tab saves by its route whatever the
// project's pilot does: running, stopped, paused (stopped with its STOP file) or failed.
func TestDeploiementModifiableArrete(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	p := fauxProjet(t, t.TempDir(), "demo", 3, 1)
	s.mu.Lock()
	s.config.Projets = []Projet{p}
	s.mu.Unlock()
	id := aleatoire(8)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: id, Nom: "vps", Hote: "203.0.113.7", Port: 22, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
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
	pid, stop := filepath.Join(p.Atelier(), "pilot.pid"), filepath.Join(p.Atelier(), "STOP")
	for i, c := range []struct {
		nom, phase string
		pid, stop  bool
	}{
		{"running", "travail", true, false},
		{"stopped", "arrete", false, false},
		{"paused", "arrete", false, true},
		{"failed", "erreur", false, false},
	} {
		_, _ = os.Remove(pid), os.Remove(stop)
		fichiers := map[string]string{p.EtatJSON(): `{"phase":"` + c.phase + `","cycle":3}`}
		if c.pid {
			fichiers[pid] = "999999\n"
		}
		if c.stop {
			fichiers[stop] = ""
		}
		for f, texte := range fichiers {
			if err := os.WriteFile(f, []byte(texte), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if e, _ := p.LireEtat(); p.PiloteActif() != c.pid || e.Phase != c.phase {
			t.Fatalf("%s: the state was not set (active %v, phase %q)", c.nom, p.PiloteActif(), e.Phase)
		}
		url, moment := fmt.Sprintf("https://site%d.example.com", i), []string{"jalon", "livraison"}[i%2]
		corps, _ := json.Marshal(map[string]any{"serveurs": []string{id}, "url": url, "plan": "nginx, pilot " + c.nom, "moment": moment})
		if code, v := appel("POST", string(corps)); code != 200 {
			t.Errorf("%s: POST refused: %d %v", c.nom, code, v)
			continue
		}
		_, v := appel("GET", "")
		d, _ := v["deploiement"].(map[string]any)
		if d["url"] != url || d["moment"] != moment || d["plan"] != "nginx, pilot "+c.nom || fmt.Sprint(d["serveurs"]) != "["+id+"]" {
			t.Errorf("%s: saved deployment: %v", c.nom, d)
		}
	}
}
