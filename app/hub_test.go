package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubRegistre(t *testing.T) {
	ids := map[string]bool{}
	for _, m := range modulesHub() {
		if ids[m.ID()] || m.ID() != strings.ToLower(m.ID()) || m.Nom() == "" {
			t.Fatalf("bad module id or name: %q %q", m.ID(), m.Nom())
		}
		ids[m.ID()] = true
	}
	if !ids["dev"] {
		t.Fatal("the Dev module is not registered")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a module registered twice must panic")
		}
	}()
	enregistrerModule(moduleDev{})
}

func TestHubDossierModule(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, d)
	}
	m := dossierModule("essai")
	if filepath.Base(m) != "essai" || filepath.Base(filepath.Dir(m)) != "modules" {
		t.Fatalf("module folder: %s", m)
	}
	if st, err := os.Stat(m); err != nil || !st.IsDir() {
		t.Fatalf("module folder not created: %v", err)
	}
}

func TestHubAPI(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(entete, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/hub", nil)
		if entete != "" {
			r.Header.Set("X-Athanor", entete)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookieAcces, Value: cookie})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, c := range [][2]string{{"", ""}, {"jeton", ""}, {"", "cle"}, {"autre", "cle"}} {
		if w := appel(c[0], c[1]); w.Code != http.StatusForbidden {
			t.Fatalf("/api/hub without the header or the cookie (%q, %q): %d", c[0], c[1], w.Code)
		}
	}
	w := appel("jeton", "cle")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/hub: %d %s", w.Code, w.Body.String())
	}
	var v struct {
		Modules []ModuleVue `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	var dev *ModuleVue
	for i := range v.Modules {
		if v.Modules[i].ID == "dev" {
			dev = &v.Modules[i]
		}
	}
	if dev == nil || dev.Nom != (moduleDev{}).Nom() || len(dev.Pages) == 0 || dev.Pages[0].Hash != "#global" {
		t.Fatalf("Dev in /api/hub: %s", w.Body.String())
	}
	if dev.Resume.Etat != "inactif" || dev.Resume.Besoin != 0 || len(dev.Resume.Stats) == 0 {
		t.Fatalf("Dev summary without projects: %+v", dev.Resume)
	}
}

func TestHubBesoinProjet(t *testing.T) {
	for _, c := range []struct {
		pv   ProjetVue
		veut string
	}{
		{ProjetVue{PourVous: 2}, "pour_vous"},
		{ProjetVue{PourVous: 2, Archive: true}, ""},
		{ProjetVue{Actif: true, Phase: "attente_vous"}, "attente_vous"},
		{ProjetVue{Actif: true, Phase: "connexion"}, "connexion"},
		{ProjetVue{Actif: true, Phase: "budget"}, "budget"},
		{ProjetVue{Actif: false, Phase: "attente_vous"}, ""},
		{ProjetVue{Actif: true, Phase: "travail"}, ""},
		{ProjetVue{Actif: true, Phase: "quota"}, ""},
		{ProjetVue{Actif: true, Phase: "budget", PourVous: 1}, "pour_vous"},
	} {
		if g := besoinProjet(c.pv); g != c.veut {
			t.Errorf("besoinProjet(%+v) = %q, want %q", c.pv, g, c.veut)
		}
	}
}

// The display name of a module lives in one place: the Dev module's Nom(); the menu renders /api/hub.
func TestHubNomUnSeulEndroit(t *testing.T) {
	fichiers, _ := filepath.Glob("*.go")
	n := 0
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if c := strings.Count(string(b), `"Dev"`); c > 0 {
			if f != "hub_dev.go" {
				t.Errorf(`%s holds the literal "Dev"`, f)
			}
			n += c
		}
	}
	if n != 1 {
		t.Errorf(`the literal "Dev" appears %d times in the Go code, want 1 (hub_dev.go)`, n)
	}
	for _, f := range []string{"static/app.js", "static/index.html"} {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), `"Dev"`) || strings.Contains(string(b), ">Dev<") {
			t.Errorf(`%s names the Dev module itself (render the nom of /api/hub)`, f)
		}
	}
}
