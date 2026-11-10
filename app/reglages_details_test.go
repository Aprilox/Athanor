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
		"The framing time box is between 10 and 240 minutes.", "Other (min)",
		"Unlimited", "3 days", "1 week", "2 weeks", "30 days", "Like Settings ({0} h)", "Like Settings (unlimited)", "Other (h)",
		"counted from the project's first start", "Safety stop saved for this project: a running pilot applies it before its next cycle.",
		"The safety stop is between 1 and 87600 hours, or unlimited.",
		"It reached its safety stop (in Settings, or in Manage › Working mode for this project). The work in progress is in the project folder: raise the limit or choose Unlimited, then start it again.")
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

// A project's own safety stop: unlimited (0), hours, back to the global value (-1), kept when a request
// leaves it out, refused out of bounds; and the pilot's rule: the project's value first, 0 never stops.
func TestDureeMaxParProjet(t *testing.T) {
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	kit := t.TempDir()
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("MAX_HOURS=72\n"), 0o600)
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
	if _, m := appel("GET", ""); m.DureeMax != nil || m.DureeMaxGlobal != 72 {
		t.Fatalf("nothing set: %+v", m)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"duree_max":0}`); c != 200 {
		t.Fatalf("unlimited: %d", c)
	}
	if _, m := appel("GET", ""); m.DureeMax == nil || *m.DureeMax != 0 {
		t.Fatalf("after unlimited: %+v", m)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"duree_max":100000}`); c != 400 {
		t.Fatalf("out of bounds accepted: %d", c)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"cadrage_max":45}`); c != 200 {
		t.Fatal(c)
	}
	if n, ok := dureeMaxProjet(p.envProjet()); !ok || n != 0 {
		t.Fatalf("left out of a request, the safety stop changed: %d %v", n, ok)
	}

	// the pilot: started 100 h ago
	pl := &pilote{etat: p.Atelier(), maxHours: 72, debut: 1000}
	maintenant := int64(1000 + 100*3600)
	if _, fin := pl.dureeMaxAtteinte(maintenant); fin {
		t.Fatal("unlimited for the project, yet stopped")
	}
	if c, _ := appel("POST", `{"parallele_max":3,"duree_max":200}`); c != 200 {
		t.Fatal(c)
	}
	if h, fin := pl.dureeMaxAtteinte(maintenant); fin || h != 200 {
		t.Fatalf("200 h for the project: %d %v", h, fin)
	}
	if c, _ := appel("POST", `{"parallele_max":3,"duree_max":-1}`); c != 200 {
		t.Fatal(c)
	}
	if h, fin := pl.dureeMaxAtteinte(maintenant); !fin || h != 72 {
		t.Fatalf("back to the global 72 h: %d %v", h, fin)
	}
	pl.maxHours = 0
	if _, fin := pl.dureeMaxAtteinte(maintenant); fin {
		t.Fatal("global 0 (unlimited) stopped the pilot")
	}
}
