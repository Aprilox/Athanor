package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSourdineVoulue(t *testing.T) {
	cas := []struct {
		entendre, regarde, sur, veut bool
	}{
		{false, false, true, true},
		{true, false, true, false},   // the user hears Claude's desktop
		{false, true, true, false},   // the user looks at it
		{false, false, false, false}, // a program of the user's
	}
	for _, c := range cas {
		if g := sourdineVoulue(c.entendre, c.regarde, c.sur); g != c.veut {
			t.Fatalf("%+v: got %v", c, g)
		}
	}
}

func TestDescendants(t *testing.T) {
	parents := map[uint32]uint32{10: 1, 11: 10, 12: 11, 20: 1, 30: 30, 40: 41, 41: 40}
	r := descendants(map[uint32]bool{10: true, 40: true}, parents)
	for _, p := range []uint32{10, 11, 12, 40, 41} {
		if !r[p] {
			t.Fatalf("%d missing: %v", p, r)
		}
	}
	for _, p := range []uint32{1, 20, 30} {
		if r[p] {
			t.Fatalf("%d should not be there: %v", p, r)
		}
	}
}

func TestFichierSourdine(t *testing.T) {
	kit := t.TempDir()
	if err := os.MkdirAll(filepath.Join(kit, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if len(lireSourdine(kit)) != 0 {
		t.Fatal("nothing yet")
	}
	ecrireSourdine(kit, map[string]bool{`c:\program files\epic\unrealeditor.exe`: true, `c:\roblox\robloxstudiobeta.exe`: true})
	r := lireSourdine(kit)
	if len(r) != 2 || !r[`c:\roblox\robloxstudiobeta.exe`] {
		t.Fatalf("got %v", r)
	}
	ecrireSourdine(kit, nil)
	if _, err := os.Stat(fichierSourdine(kit)); !os.IsNotExist(err) {
		t.Fatal("the file stays when nothing is muted")
	}
}

func TestOptionEntendreBureau(t *testing.T) {
	kit := t.TempDir()
	s := nouveauServeur(Config{Kit: kit, Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode string, corps any) map[string]any {
		var r *http.Request
		if corps != nil {
			j, _ := json.Marshal(corps)
			r = httptest.NewRequest(methode, "/api/options", bytes.NewReader(j))
		} else {
			r = httptest.NewRequest(methode, "/api/options", nil)
		}
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", methode, w.Code, w.Body.String())
		}
		return v
	}
	if v := appel("GET", nil); v["entendre_bureau"] != false {
		t.Fatalf("muted by default: %v", v["entendre_bureau"])
	}
	appel("POST", map[string]any{"kit": kit, "entendre_bureau": true})
	if !s.config.EntendreBureauClaude || appel("GET", nil)["entendre_bureau"] != true {
		t.Fatal("not saved")
	}
	appel("POST", map[string]any{"kit": kit}) // a page without the switch changes nothing
	if !s.config.EntendreBureauClaude {
		t.Fatal("lost without the switch")
	}
}

func TestAnnulerArret(t *testing.T) {
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	os.MkdirAll(p.Atelier(), 0o755)
	stop := filepath.Join(p.Atelier(), "STOP")
	os.WriteFile(stop, nil, 0o644)
	s.relancer["a"] = true
	r := httptest.NewRequest("POST", "/api/pilote/annuler-arret?p=a", nil)
	r.Header.Set("X-Athanor", "jeton")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("Stop cancelled")) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if fichierExiste(stop) || s.relancer["a"] {
		t.Fatal("the stop request stays")
	}
}
