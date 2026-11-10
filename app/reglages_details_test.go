package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every text of the settings' "i" (DETAILS_REGLAGES in app.js) and of the project's framing time box is
// translated in the five languages of i18n.js.
func TestDetailsReglagesTraduits(t *testing.T) {
	app, err := statique.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	i18n := lireI18nEssai(t)
	debut := bytes.Index(app, []byte("const DETAILS_REGLAGES = {"))
	fin := bytes.Index(app[debut:], []byte("\n};"))
	if debut < 0 || fin < 0 {
		t.Fatal("DETAILS_REGLAGES not found in app.js")
	}
	var textes []string
	for _, m := range regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`).FindAllSubmatch(app[debut:debut+fin], -1) {
		textes = append(textes, string(m[1]))
	}
	if len(textes) < 30 {
		t.Fatalf("only %d texts read", len(textes))
	}
	textes = append(textes, "What a lower or a higher value changes", "the first cycle, which writes the plan",
		"Like Settings ({0} min)", "Like Settings (no limit)", "Framing time box saved: it applies to this project's next framing.",
		"The framing time box is between 10 and 240 minutes.")
	for _, x := range textes {
		cle, _ := json.Marshal(x)
		if n := strings.Count(i18n, string(cle)+": "); n < 5 {
			t.Errorf("translated in %d language(s) out of 5: %s", n, x)
		}
	}
}

// A project's own framing time box: written and read through /api/mode, bounded, back to the global value
// with 0, kept when a request leaves it out (the terminal interface), and the one the pilot uses.
func TestCadrageParProjet(t *testing.T) {
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	kit := t.TempDir()
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("FRAMING_MAX_MIN=30\n"), 0o600)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, corps string) (int, ModeVue) {
		r := httptest.NewRequest(methode, "/api/mode?p=a", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var m ModeVue
		if methode == "GET" {
			_ = json.Unmarshal(w.Body.Bytes(), &m)
		}
		return w.Code, m
	}
	if _, m := appel("GET", ""); m.CadrageMax != nil || m.CadrageGlobal != 30 {
		t.Fatalf("nothing set: %+v", m)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"cadrage_max":60}`); c != 200 {
		t.Fatalf("set: %d", c)
	}
	if _, m := appel("GET", ""); m.CadrageMax == nil || *m.CadrageMax != 60 || cadrageProjet(p.envProjet()) != 60 {
		t.Fatalf("after 60: %+v", m)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"cadrage_max":500}`); c != 400 || cadrageProjet(p.envProjet()) != 60 {
		t.Fatalf("out of bounds accepted: %d", c)
	}
	if c, _ := appel("POST", `{"parallele":true,"parallele_max":4}`); c != 200 || cadrageProjet(p.envProjet()) != 60 {
		t.Fatal("left out of a request, the time box changed")
	}
	if c, _ := appel("POST", `{"parallele_max":3,"cadrage_max":0}`); c != 200 || cadrageProjet(p.envProjet()) != 0 {
		t.Fatal("0 does not go back to the global value")
	}
	// the workshop is Claude's to write: a value out of bounds is ignored
	_ = os.WriteFile(p.envProjet(), []byte("FRAMING_MAX_MIN=5000\n"), 0o600)
	if cadrageProjet(p.envProjet()) != 0 {
		t.Fatal("out-of-bounds value used")
	}
}
