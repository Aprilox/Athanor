package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValiderEnvironnement(t *testing.T) {
	inv := []ServeurInventaire{{ID: "a1", Nom: "vps-prod", Roles: []string{"prod"}}, {ID: "b2", Nom: "vps-dev", Roles: []string{"dev"}},
		{ID: "c3", Nom: "nu", Roles: []string{}}, {ID: "d4", Nom: "mixte", Roles: []string{"dev", "prod"}}}
	ok := func(nom string, e Environnement) Environnement {
		t.Helper()
		r, err := validerEnvironnement(nom, e, inv)
		if err != nil {
			t.Fatalf("%s %+v: %v", nom, e, err)
		}
		return r
	}
	r := ok("dev", Environnement{URL: " https://dev.exemple.fr/app ", Serveurs: []string{"b2", "c3", "b2"}})
	if r.URL != "https://dev.exemple.fr/app" || !slices.Equal(r.Serveurs, []string{"b2", "c3"}) || r.Protection != "aucune" || r.Politique != "accord" {
		t.Errorf("defaults: %+v", r)
	}
	ok("staging", Environnement{URL: "http://10.0.0.5:8080", Politique: "auto", Protection: "auth_proxy"})
	ok("prod", Environnement{URL: "https://Exemple.fr", Serveurs: []string{"a1", "d4"}, Protection: "access"})
	if r := ok("dev", Environnement{}); r.URL != "" || r.Serveurs == nil {
		t.Errorf("empty environment: %+v", r)
	}
	for _, c := range []struct {
		nom string
		e   Environnement
		mot string
	}{
		{"test", Environnement{}, "Unknown environment"},
		{"dev", Environnement{URL: "ftp://exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://moi:secret@exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://"}, "Invalid URL"},
		{"dev", Environnement{URL: "exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://ex_emple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "javascript:alert(1)"}, "Invalid URL"},
		{"dev", Environnement{Protection: "mur"}, "Unknown protection"},
		{"dev", Environnement{Politique: "jamais"}, "Unknown policy"},
		{"prod", Environnement{Politique: "auto"}, "always asks for your approval"},
		{"dev", Environnement{Serveurs: []string{"zz"}}, "Unknown server"},
		{"prod", Environnement{Serveurs: []string{"b2"}}, "vps-dev is not a production server"},
		{"prod", Environnement{Serveurs: []string{"c3"}}, "nu is not a production server"},
		{"dev", Environnement{Serveurs: []string{"a1"}}, "vps-prod is a production server"},
		{"staging", Environnement{Serveurs: []string{"d4"}}, "mixte is a production server"},
		{"dev", Environnement{Fournisseur: "../x"}, "Unknown provider"},
	} {
		_, err := validerEnvironnement(c.nom, c.e, inv)
		if _, entree := err.(erreurEntree); err == nil || !entree || !strings.Contains(err.Error(), c.mot) {
			t.Errorf("%s %+v: %v, want %q", c.nom, c.e, err, c.mot)
		}
	}
}

// An access file written before stage 7 is read without loss, and keeps its grants once environments are added.
func TestEnvironnementsAncienFichier(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "acces.json")
	ancien := `{"version":1,"projets":{"demo":{"env":"staging","forcer_infra_natif":true,"acces":[{"entree":"e1","env":"dev","variable":"API_KEY","cree":"2026-11-01T00:00:00Z"}]}},"auto_faits":["e1"]}`
	if err := os.WriteFile(chemin, []byte(ancien), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if e := a.Environnements("demo"); len(e) != 3 || e["prod"].Politique != "accord" || e["dev"].Serveurs == nil {
		t.Fatalf("defaults of an old file: %+v", e)
	}
	if err := a.DefinirEnvironnement("demo", "dev", Environnement{URL: "https://dev.exemple.fr", Serveurs: []string{"b2"}, Protection: "aucune", Politique: "auto", CleDeploiement: "volee"}); err != nil {
		t.Fatal(err)
	}
	a, err = ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	p := a.Projet("demo")
	if p.Env != "staging" || !p.ForcerInfraNatif || len(p.Acces) != 1 || p.Acces[0].Variable != "API_KEY" || !slices.Contains(a.f.AutoFaits, "e1") {
		t.Errorf("grants lost: %+v %v", p, a.f.AutoFaits)
	}
	if e := a.Environnements("demo")["dev"]; e.URL != "https://dev.exemple.fr" || e.Politique != "auto" || e.CleDeploiement != "" {
		t.Errorf("environment read back (a request never sets the deployment key): %+v", e)
	}
}

func TestEnvironnementsAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	prod, dev := aleatoire(8), aleatoire(8)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: prod, Nom: "vps-prod", Hote: "203.0.113.5", Port: 22, Roles: []string{"prod"}, Cree: time.Now()},
			ServeurInventaire{ID: dev, Nom: "vps-dev", Hote: "203.0.113.6", Port: 22, Roles: []string{"dev"}, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	env := func(v map[string]any, nom string) map[string]any {
		e, _ := v["environnements"].(map[string]any)
		x, _ := e[nom].(map[string]any)
		return x
	}

	code, v := appel("GET", "/api/projets/demo/environnements", "")
	if code != 200 || len(v["envs"].([]any)) != 3 || env(v, "prod")["politique"] != "accord" || env(v, "dev")["serveurs"] == nil {
		t.Fatalf("GET: %d %v", code, v)
	}
	if code, _ := appel("GET", "/api/projets/inconnu/environnements", ""); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
	for _, c := range []string{
		`{"env":"prod","serveurs":["` + dev + `"]}`, `{"env":"prod","serveurs":["` + prod + `"],"politique":"auto"}`,
		`{"env":"dev","serveurs":["` + prod + `"]}`, `{"env":"dev","serveurs":["inconnu"]}`,
		`{"env":"dev","url":"https://a:b@exemple.fr"}`, `{"env":"qa"}`, `pas du json`,
	} {
		if code, v := appel("POST", "/api/projets/demo/environnements", c); code != 400 || v["erreur"] == nil {
			t.Errorf("%s: %d %v", c, code, v)
		}
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"prod","url":"https://exemple.fr","serveurs":["`+prod+`"],"protection":"access","cle_deploiement":"x"}`); code != 200 ||
		env(v, "prod")["url"] != "https://exemple.fr" || env(v, "prod")["cle_deploiement"] != nil {
		t.Errorf("prod: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"dev","url":"https://dev.exemple.fr","serveurs":["`+dev+`"],"politique":"auto"}`); code != 200 ||
		env(v, "dev")["politique"] != "auto" || env(v, "prod")["protection"] != "access" {
		t.Errorf("dev: %d %v", code, v)
	}
	if code, _ := appel("POST", "/api/projets/inconnu/environnements", `{"env":"dev"}`); code != 404 {
		t.Errorf("unknown project: %d", code)
	}

	// a role change that would break an environment is refused, naming it
	if code, v := appel("POST", "/api/serveurs/"+dev, `{"roles":["prod"]}`); code != 409 || !strings.Contains(v["erreur"].(string), "demo/dev") {
		t.Errorf("dev server made prod: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/serveurs/"+prod, `{"roles":["dev"],"mot_de_passe":"`+mdpPreuveTest+`"}`); code != 409 || !strings.Contains(v["erreur"].(string), "demo/prod") {
		t.Errorf("prod server made dev: %d %v", code, v)
	}

	// renaming then deleting a project carries its environments along
	if err := s.acces.RenommerProjet("demo", "demo2"); err != nil {
		t.Fatal(err)
	}
	if e := s.acces.Environnements("demo2"); e["dev"].URL != "https://dev.exemple.fr" || s.acces.Environnements("demo")["dev"].URL != "" {
		t.Errorf("rename: %+v", e)
	}
	if err := s.acces.RenommerProjet("demo2", "demo"); err != nil {
		t.Fatal(err)
	}

	// removing a server takes it out of the environments and says so
	if code, v := appel("DELETE", "/api/serveurs/"+dev, `{"mot_de_passe":"`+mdpPreuveTest+`"}`); code != 200 || len(v["environnements"].([]any)) != 1 || v["environnements"].([]any)[0] != "demo/dev" {
		t.Errorf("server removed: %d %v", code, v)
	}
	if e := s.acces.Environnements("demo"); len(e["dev"].Serveurs) != 0 || e["dev"].URL != "https://dev.exemple.fr" || len(e["prod"].Serveurs) != 1 {
		t.Errorf("after the removal: %+v", e)
	}
	if err := s.acces.SupprimerProjet("demo"); err != nil {
		t.Fatal(err)
	}
	if e := s.acces.Environnements("demo"); e["prod"].URL != "" {
		t.Errorf("deleted project kept its environments: %+v", e)
	}
}
