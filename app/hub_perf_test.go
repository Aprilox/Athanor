package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fauxProjet: a project folder with a PLAN.md (n milestones, the first faits done), a STATE.json, a
// queue and a For you task.
func fauxProjet(t *testing.T, racine, nom string, n, faits int) Projet {
	t.Helper()
	p := Projet{Nom: nom, Dossier: filepath.Join(racine, nom)}
	var plan strings.Builder
	plan.WriteString("# Plan\n\n")
	for i := 1; i <= n; i++ {
		c := " "
		if i <= faits {
			c = "x"
		}
		fmt.Fprintf(&plan, "- [%s] M%d : Jalon %d — Files: a.go. Depends on: —. Acceptance: ok.\n", c, i, i)
	}
	for _, d := range []string{"queue", "for-you", "logs"} {
		if err := os.MkdirAll(filepath.Join(p.Atelier(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for f, c := range map[string]string{
		p.Plan():                                 plan.String(),
		p.EtatJSON():                             `{"phase":"travail","cycle":3}`,
		filepath.Join(p.FileAttente(), "001.md"): "Une demande\n",
	} {
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestHubJalonsDuJour(t *testing.T) {
	d := t.TempDir()
	fichier := filepath.Join(d, "hub", "jalons.json")
	p := fauxProjet(t, d, "a", 4, 1)
	maintenant := time.Now()
	faits := map[string]map[string]bool{}
	if n := observerPlan(p, faits, fichier, maintenant); len(n) != 0 || jalonsDuJour(fichier, maintenant) != 0 {
		t.Fatalf("the first look counted what was already done: %v", n)
	}
	// M2 ticked: one milestone today
	ecrire := func(faitsPlan int, suffixe string) {
		q := fauxProjet(t, d, "a", 4, faitsPlan)
		if suffixe != "" {
			b, _ := os.ReadFile(q.Plan())
			_ = os.WriteFile(q.Plan(), append(b, []byte(suffixe)...), 0o644)
		}
	}
	ecrire(2, "")
	n := observerPlan(p, faits, fichier, maintenant)
	if len(n) != 1 || jalonsDuJour(fichier, maintenant) != 1 {
		t.Fatalf("M2 done: %v, today %d", n, jalonsDuJour(fichier, maintenant))
	}
	// several commits of the same milestone, a "(in progress)" commit: the plan changes, M3 stays unticked
	ecrire(2, "\nNote (in progress) for M3\n")
	observerPlan(p, faits, fichier, maintenant)
	ecrire(2, "\nAnother commit of M2\n")
	observerPlan(p, faits, fichier, maintenant)
	if g := jalonsDuJour(fichier, maintenant); g != 1 {
		t.Fatalf("rewrites of the plan counted again: %d", g)
	}
	// Athanor restarted (a new look) then M2 seen again: not noted twice
	faits = map[string]map[string]bool{}
	observerPlan(p, faits, fichier, maintenant)
	if err := noterJalonFait(fichier, p.Dossier, n[0].Titre, maintenant); err != nil {
		t.Fatal(err)
	}
	if g := len(lireJalonsFaits(fichier)); g != 1 {
		t.Fatalf("a milestone noted twice: %d entries", g)
	}
	// yesterday's milestone is not today's; old entries are dropped
	_ = noterJalonFait(fichier, "b", "Ancien", maintenant.Add(-40*24*time.Hour))
	_ = noterJalonFait(fichier, "b", "Hier", maintenant.AddDate(0, 0, -1))
	if g := jalonsDuJour(fichier, maintenant); g != 1 {
		t.Fatalf("yesterday counted today: %d", g)
	}
	_ = noterJalonFait(fichier, "b", "Nouveau", maintenant)
	for _, j := range lireJalonsFaits(fichier) {
		if j.Jalon == "Ancien" {
			t.Fatal("an entry older than 30 days was kept")
		}
	}
	if g := jalonsDuJour(fichier, maintenant); g != 2 {
		t.Fatalf("today: %d, want 2", g)
	}
}

func TestHubLirePlanCache(t *testing.T) {
	d := t.TempDir()
	p := fauxProjet(t, d, "a", 5, 2)
	_ = os.MkdirAll(filepath.Join(p.Atelier(), "archives"), 0o755)
	_ = os.WriteFile(filepath.Join(p.Atelier(), "archives", "PLAN_TERMINE.md"), []byte("- [x] M0 : Ancien jalon\n"), 0o644)
	vieux := time.Now().Add(-time.Hour) // older than the mtime granularity: cached
	_ = os.Chtimes(p.Plan(), vieux, vieux)
	_ = os.Chtimes(filepath.Join(p.Atelier(), "archives", "PLAN_TERMINE.md"), vieux, vieux)
	for i := 0; i < 2; i++ {
		if g, w := p.LirePlan(), p.lirePlanSansCache(); !reflect.DeepEqual(g, w) || len(g) != 6 {
			t.Fatalf("cached plan differs (%d milestones): %+v", len(g), g)
		}
	}
	// the cached copy cannot be changed by a caller
	g := p.LirePlan()
	g[0].Titre = "changed"
	if p.LirePlan()[0].Titre == "changed" {
		t.Fatal("a caller changed the cached plan")
	}
	// a same-size change with the same mtime (2 s granularity on FAT): a recent file is not cached
	b, _ := os.ReadFile(p.Plan())
	recent := time.Now().Truncate(time.Second)
	_ = os.Chtimes(p.Plan(), recent, recent)
	_ = p.LirePlan()
	_ = os.WriteFile(p.Plan(), bytes.Replace(b, []byte("- [ ]"), []byte("- [x]"), 1), 0o644)
	_ = os.Chtimes(p.Plan(), recent, recent)
	if g := p.LirePlan(); !reflect.DeepEqual(g, p.lirePlanSansCache()) {
		t.Fatal("a milestone ticked within the mtime granularity is not seen")
	}
	// PLAN.md changes: read again
	fauxProjet(t, d, "a", 7, 3)
	if g := p.LirePlan(); len(g) != 8 || !reflect.DeepEqual(g, p.lirePlanSansCache()) {
		t.Fatalf("plan not read again after a change: %d milestones", len(g))
	}
	// the archive changes: read again
	_ = os.WriteFile(filepath.Join(p.Atelier(), "archives", "PLAN_TERMINE.md"), []byte("- [x] M0 : Ancien jalon\n- [x] M00 : Autre\n"), 0o644)
	if g := p.LirePlan(); len(g) != 9 {
		t.Fatalf("archive change not seen: %d milestones", len(g))
	}
	_ = os.Remove(p.Plan())
	if g := p.LirePlan(); g != nil {
		t.Fatalf("plan removed, still %d milestones", len(g))
	}
}

func TestHubValeurQuota(t *testing.T) {
	u := func(f float64) *float64 { return &f }
	for _, c := range []struct {
		q    *EtatQuota
		veut any
	}{
		{nil, nil},
		{&EtatQuota{Statut: "allowed"}, "allowed"},
		{&EtatQuota{Statut: "allowed", Fenetres: []FenetreQuota{{Nom: "five_hour", Utilisation: u(0.424)}, {Nom: "seven_day", Utilisation: u(0.61)}, {Nom: "x"}}}, 61},
	} {
		if g := valeurQuota(c.q); g != c.veut {
			t.Errorf("valeurQuota(%+v) = %v, want %v", c.q, g, c.veut)
		}
	}
}

// 20 projects: the projects' view and /api/hub stay well under a second.
func TestHubPerf20Projets(t *testing.T) {
	d := t.TempDir()
	var projets []Projet
	for i := 0; i < 20; i++ {
		projets = append(projets, fauxProjet(t, d, fmt.Sprintf("projet-%02d", i), 40, i))
	}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: projets, ProjetActif: "projet-00", Langue: "en"})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	debut := time.Now()
	if v := s.vuesProjets("projet-00"); len(v) != 20 || v[5].Faits != 5 || v[5].Jalons != 40 {
		t.Fatalf("projects' view: %d projects", len(v))
	}
	r := httptest.NewRequest("GET", "/api/hub", nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	duree := time.Since(debut)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Milestones done today"`) {
		t.Fatalf("/api/hub: %d %s", w.Code, w.Body.String())
	}
	if duree > time.Second {
		t.Fatalf("20 projects: %v, want < 1 s", duree)
	}
	// the Projects page lists /api/hub's elements: one per project, with its progress
	var v struct {
		Elements []ElementHub `json:"elements"`
	}
	dev := 0
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, e := range v.Elements {
		if e.Module == "dev" && e.Avancement != nil && e.Avancement.Total == 40 {
			dev++
		}
	}
	if dev != 20 {
		t.Fatalf("%d Dev elements with their progress, want 20", dev)
	}
	t.Logf("20 projects: vuesProjets + /api/hub in %v", duree)
}
