package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func ecrire(t *testing.T, chemin, texte string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chemin, []byte(texte), 0o644); err != nil {
		t.Fatal(err)
	}
}

func coches(t *testing.T, p Projet) string {
	t.Helper()
	var s []string
	for _, j := range p.LirePlan() {
		if j.Fait {
			s = append(s, j.ID)
		}
	}
	return strings.Join(s, ",")
}

const planTest = "# Plan\n\n- [x] M1 : Engine — the loop. Files: src/a.txt, src/b.txt. Depends on: none.\n- [x] M2 : Docs — the guide\n  - Acceptance: README.md exists\n- [ ] M3 : Release — package it\n"

func TestVerifierJalons(t *testing.T) {
	d := t.TempDir()
	p := Projet{Nom: "x", Dossier: d}
	ecrire(t, p.Plan(), planTest)
	ecrire(t, filepath.Join(p.Atelier(), "FINAL_REPORT.md"), "# Report\n\n**STATUS: DONE**\n")

	// an empty folder: nothing is done any more, and the delivery is reopened
	dec, rec, err := p.VerifierJalons()
	if err != nil || len(dec) != 2 || len(rec) != 0 || coches(t, p) != "" {
		t.Fatalf("empty folder: %v %v %v %q", dec, rec, err, coches(t, p))
	}
	if b, _ := os.ReadFile(filepath.Join(p.Atelier(), "FINAL_REPORT.md")); strings.Contains(string(b), "STATUS: DONE") || !strings.Contains(string(b), "STATUS: REOPENED") {
		t.Fatalf("delivery not reopened: %s", b)
	}
	if !p.AVerifier() {
		t.Fatal("milestones should wait for their files")
	}
	// any file is not enough: M1's own files are needed, and M2 (no file list) is left to Claude
	ecrire(t, filepath.Join(d, "notes.txt"), "x")
	if _, rec, _ = p.VerifierJalons(); len(rec) != 0 || coches(t, p) != "" {
		t.Fatalf("a random file ticked something: %v %q", rec, coches(t, p))
	}
	// M1's files come back: M1 ticked again, M2 waits for Claude
	ecrire(t, filepath.Join(d, "src", "a.txt"), "a")
	ecrire(t, filepath.Join(d, "src", "b.txt"), "b")
	if dec, rec, _ = p.VerifierJalons(); len(dec) != 0 || len(rec) != 1 || coches(t, p) != "M1" || p.AVerifier() {
		t.Fatalf("files back: %v %v %q", dec, rec, coches(t, p))
	}
	// one listed file disappears: M1 again
	os.Remove(filepath.Join(d, "src", "b.txt"))
	if dec, _, _ = p.VerifierJalons(); len(dec) != 1 || !strings.Contains(dec[0], "src/b.txt") || coches(t, p) != "" {
		t.Fatalf("missing file: %v %q", dec, coches(t, p))
	}
	// back again; M3 (never done) is never ticked by the check
	ecrire(t, filepath.Join(d, "src", "b.txt"), "b")
	p.VerifierJalons()
	if coches(t, p) != "M1" {
		t.Fatalf("after the files came back: %q", coches(t, p))
	}
	// the plan edited by hand: nothing is ticked back any more
	os.Remove(filepath.Join(d, "src", "b.txt"))
	p.VerifierJalons()
	p.oublierVerif()
	ecrire(t, filepath.Join(d, "src", "b.txt"), "b")
	if p.VerifierJalons(); coches(t, p) != "" {
		t.Fatalf("ticked back after a manual edit: %q", coches(t, p))
	}
	if b, _ := os.ReadFile(filepath.Join(p.Atelier(), "LOG.md")); !strings.Contains(string(b), "folder check") {
		t.Fatal("no log entry")
	}
}

func TestCopierAtelier(t *testing.T) {
	de, vers := t.TempDir(), filepath.Join(t.TempDir(), "neuf", ".athanor")
	ecrire(t, filepath.Join(de, "PLAN.md"), "plan")
	ecrire(t, filepath.Join(de, "queue", "a.md"), "demande")
	ecrire(t, filepath.Join(de, "logs", "cycle.jsonl"), "gros")
	ecrire(t, filepath.Join(de, "pilot.pid"), "1")
	if err := copierAtelier(de, vers); err != nil {
		t.Fatal(err)
	}
	for f, attendu := range map[string]bool{"PLAN.md": true, "queue/a.md": true, "logs/cycle.jsonl": false, "pilot.pid": false} {
		_, err := os.Stat(filepath.Join(vers, f))
		if (err == nil) != attendu {
			t.Errorf("%s: present=%v, expected %v", f, err == nil, attendu)
		}
	}
}

func TestGestionProjet(t *testing.T) {
	racine := t.TempDir()
	a, b := filepath.Join(racine, "a"), filepath.Join(racine, "b")
	pa := Projet{Nom: "alpha", Dossier: a}
	ecrire(t, pa.Plan(), planTest)
	ecrire(t, filepath.Join(a, "src", "a.txt"), "a")
	ecrire(t, filepath.Join(a, "src", "b.txt"), "b")
	os.MkdirAll(b, 0o755)
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{pa, {Nom: "beta", Dossier: b}}, ProjetActif: "alpha"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(corps string) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/api/projets/gestion", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}

	if c, _ := appel(`{"nom":"alpha","action":"renommer","valeur":"beta"}`); c == 200 {
		t.Fatal("rename onto an existing name accepted")
	}
	if c, v := appel(`{"nom":"alpha","action":"renommer","valeur":"gamma"}`); c != 200 || v["nom"] != "gamma" || s.config.ProjetActif != "gamma" {
		t.Fatalf("rename: %d %v %q", c, v, s.config.ProjetActif)
	}
	// another folder, empty: the workshop follows, the milestones are unticked, Claude checks again
	vide := filepath.Join(racine, "vide")
	if c, v := appel(`{"nom":"gamma","action":"dossier","valeur":` + jsonTexte(vide) + `}`); c != 200 {
		t.Fatalf("other folder: %d %v", c, v)
	}
	pg := Projet{Nom: "gamma", Dossier: vide}
	if s.config.Projets[0].Dossier != vide || coches(t, pg) != "" {
		t.Fatalf("after the change: %q %q", s.config.Projets[0].Dossier, coches(t, pg))
	}
	if _, err := os.Stat(filepath.Join(pg.Atelier(), "REVERIFY")); err != nil {
		t.Fatal("REVERIFY missing")
	}
	if _, err := os.Stat(filepath.Join(a, "src", "a.txt")); err != nil {
		t.Fatal("the old folder must stay intact")
	}
	// move (rename) the folder
	deplace := filepath.Join(racine, "renomme")
	if c, v := appel(`{"nom":"gamma","action":"deplacer","valeur":` + jsonTexte(deplace) + `}`); c != 200 {
		t.Fatalf("move: %d %v", c, v)
	}
	if _, err := os.Stat(filepath.Join(deplace, ".athanor", "PLAN.md")); err != nil || s.config.Projets[0].Dossier != deplace {
		t.Fatalf("move: %v %q", err, s.config.Projets[0].Dossier)
	}
	// archive: out of the selection, the active project switches
	if c, _ := appel(`{"nom":"gamma","action":"archiver"}`); c != 200 || !s.config.Projets[0].Archive || s.config.ProjetActif != "beta" {
		t.Fatalf("archive: %d %+v %q", c, s.config.Projets[0], s.config.ProjetActif)
	}
	if p, ok := s.projet(httptest.NewRequest("GET", "/?p=gamma", nil)); ok && p.Nom == "gamma" {
		t.Fatal("an archived project must not be selectable")
	}
	appel(`{"nom":"gamma","action":"desarchiver"}`)
	if s.config.Projets[0].Archive {
		t.Fatal("unarchive")
	}
	// remove from the list only: the folder stays
	if c, _ := appel(`{"nom":"gamma","action":"supprimer","mode":"liste"}`); c != 200 || len(s.config.Projets) != 1 {
		t.Fatalf("remove: %d %d", c, len(s.config.Projets))
	}
	if _, err := os.Stat(deplace); err != nil {
		t.Fatal("remove from the list must not touch the folder")
	}
}

func TestFichiersDuJalon(t *testing.T) {
	j := decouperJalon("M1 : Engine — loop. Files: src/a.go (e.g. v1.2 API), docs\\guide.md, assets/, README. Depends on: M0.")
	got := strings.Join(fichiersDuJalon(j), "|")
	want := strings.Join([]string{filepath.FromSlash("src/a.go"), filepath.FromSlash("docs/guide.md"), filepath.FromSlash("assets/")}, "|")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDossiersRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix folders")
	}
	home, _ := os.UserHomeDir()
	for _, d := range []string{"/usr/local/x", "/etc", filepath.Dir(home), home, "/"} {
		if dossierInterdit(d, "/opt/kit") == "" {
			t.Errorf("%s accepted", d)
		}
	}
	if dossierInterdit(filepath.Join(t.TempDir(), "projets", "a"), "/opt/kit") != "" {
		t.Error("a normal folder refused")
	}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{{Nom: "a", Dossier: "/srv/p/a"}}})
	for _, d := range []string{"/srv/p/a", "/srv/p/a/sous", "/srv/p"} {
		if s.conflitProjets(d, "b") == nil {
			t.Errorf("%s shares files with project a", d)
		}
	}
	if s.conflitProjets("/srv/p/b", "b") != nil {
		t.Error("a sibling folder refused")
	}
}

func jsonTexte(s string) string { b, _ := json.Marshal(s); return string(b) }

// TestModeEco: the eco mode and the parallel mode never together; the Overview says so.
func TestModeEco(t *testing.T) {
	a := t.TempDir()
	pa := Projet{Nom: "alpha", Dossier: a}
	ecrire(t, pa.Plan(), planTest)
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{pa}, ProjetActif: "alpha"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, corps string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(methode, "/api/mode?p=alpha", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %v", methode, corps, w.Code, v)
		}
		return v
	}
	env := func() string { b, _ := os.ReadFile(pa.envProjet()); return string(b) }
	appel("POST", `{"parallele":true,"parallele_max":6,"eco":false}`)
	if m := appel("GET", ""); m["parallele"] != true || m["eco"] != false || pa.EcoActif() {
		t.Fatalf("parallel: %v", m)
	}
	// eco asked with parallel still ticked: eco alone, the workers' maximum kept
	if v := appel("POST", `{"parallele":true,"parallele_max":6,"eco":true}`); !strings.Contains(texteRep(v), "Eco mode on") {
		t.Fatalf("eco message: %v", v)
	}
	if m := appel("GET", ""); m["parallele"] != false || m["eco"] != true || m["parallele_max"] != float64(6) || !pa.EcoActif() {
		t.Fatalf("eco: %v\n%s", m, env())
	}
	if e := env(); !strings.Contains(e, "ECO=1") || !strings.Contains(e, "PARALLEL=0") {
		t.Fatalf(".env: %s", e)
	}
	// hand-edited both on: still read as eco only
	_ = os.WriteFile(pa.envProjet(), []byte("PARALLEL=1\nECO=1\n"), 0o644)
	if m := pa.LireMode(); m.Parallele || !m.Eco {
		t.Fatalf("both by hand: %+v", m)
	}
	if v := appel("POST", `{"parallele":false,"parallele_max":6,"eco":false}`); !strings.Contains(texteRep(v), "Eco mode off") || pa.EcoActif() {
		t.Fatalf("eco off: %v", v)
	}
}

func texteRep(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
