package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIAccueilServeur: the terminal interface against a server with 20 projects (2 of them with a
// task for the user): starts on the Home, shows it at 80x24, 120x40 and tiny sizes without panic, and the keys
// 1…9, 0, H, Esc, Tab, Shift+Tab, ? keep their meaning. The screens go to TUI_OUT (a folder) when set.
func TestTUIAccueilServeur(t *testing.T) {
	d, kit := t.TempDir(), t.TempDir()
	_ = os.MkdirAll(filepath.Join(kit, "template"), 0o755)
	_ = os.WriteFile(filepath.Join(kit, "template", "CLAUDE.md"), []byte("# x\n"), 0o644)
	var projets []Projet
	for i := 0; i < 20; i++ {
		p := fauxProjet(t, d, fmt.Sprintf("projet-%02d", i), 40, i*2)
		projets = append(projets, p)
		if i == 3 || i == 7 {
			tache := `{"taches":[{"id":"T1","titre":"Creer un compte","details":"x","attendu":"une cle","statut":"ouverte"}]}`
			_ = os.WriteFile(filepath.Join(p.Atelier(), "for-you", "tasks.json"), []byte(tache), 0o644)
		}
	}
	s := nouveauServeur(Config{Kit: kit, Projets: projets, ProjetActif: "projet-03", Langue: "fr"})
	s.cleAcces = aleatoire(24)
	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !s.pageAcces(w, r) {
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><meta name="csrf" content="` + s.csrf + `">`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := nouveauClientTUI(s.lienOuverture(srv.URL + "/"))
	if err != nil {
		t.Fatal(err)
	}
	a := nouvelleAppTUI(c, true)
	if a.page != pAccueil {
		t.Fatalf("starts on page %d, not the Home", a.page)
	}
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.hub != nil })
	sortie := os.Getenv("TUI_OUT")
	if sortie != "" {
		_ = os.MkdirAll(sortie, 0o755)
	}
	voir := func(nom string, l, h int) string {
		e := nouvelEcran(l, h)
		a.dessiner(e)
		txt := e.texteEcran()
		if sortie != "" {
			_ = os.WriteFile(filepath.Join(sortie, nom+".txt"), []byte(txt), 0o644)
		}
		return txt
	}
	for _, taille := range [][2]int{{80, 24}, {120, 40}, {60, 18}, {40, 10}, {20, 5}, {200, 60}} {
		txt := voir(fmt.Sprintf("accueil-%dx%d", taille[0], taille[1]), taille[0], taille[1])
		for i, ligne := range strings.Split(txt, "\n") {
			if n := len([]rune(ligne)); n > taille[0] {
				t.Errorf("%v: line %d is %d wide", taille, i, n)
			}
		}
		if taille[0] >= 80 {
			for _, x := range []string{"Studio", "Coffre", "Besoin de vous : 2"} {
				if !strings.Contains(txt, x) {
					t.Errorf("%v: no %q in\n%s", taille, x, txt)
				}
			}
		}
	}
	// "In progress" and the bell's alerts with their reason; a click on a line opens its project's page
	txt := voir("accueil-120x40", 120, 40)
	for _, x := range []string{"En cours", "! Studio, projet-03 : 1 tâche sur la page Pour vous",
		"! Studio, projet-07 : 1 tâche", "projet-07  Studio  1 tâche sur la page Pour vous", "Accueil", "À distance"} {
		if !strings.Contains(txt, x) {
			t.Errorf("no %q in\n%s", x, txt)
		}
	}
	// read only: a click on a line opens nothing (the Studio's pages are the window's and the phone's)
	e := nouvelEcran(120, 40)
	a.dessiner(e)
	for y := 0; y < 40; y++ {
		if k := strings.Index(e.ligneTexte(y), "projet-07  Studio"); k >= 0 {
			a.souris(evenementTUI{genre: evSouris, x: largeurTexte(e.ligneTexte(y)[:k]) + 1, y: y, appui: true})
		}
	}
	if a.page != pAccueil || a.dialogue != nil || a.form != nil {
		t.Errorf("a click on a line changed the page: %d", a.page)
	}
	// an ignored alert leaves the home's alerts (the bell's list), its project stays in "In progress"
	a.hub.Alertes = a.hub.Alertes[1:]
	if txt := voir("accueil-ignoree-120x40", 120, 40); strings.Contains(txt, "! Studio, projet-03") || !strings.Contains(txt, "projet-03  Studio") {
		t.Errorf("ignored alert:\n%s", txt)
	}
	// two pages: 1 (and H, Esc) the Home, 2 Remote; Tab goes from one to the other
	for k, pg := range map[string]int{"2": pDistance, "1": pAccueil, "H": pAccueil} {
		taper(a, k)
		if a.page != pg {
			t.Errorf("key %s: page %d, want %d", k, a.page, pg)
		}
	}
	taper(a, "2")
	voir("page-2-80x24", 80, 24)
	taper(a, "\x1b")
	if a.page != pAccueil {
		t.Errorf("Esc from Remote: %d", a.page)
	}
	taper(a, "\t")
	if a.page != pDistance {
		t.Errorf("Tab: %d", a.page)
	}
	taper(a, "\x1b[Z")
	if a.page != pAccueil {
		t.Errorf("Shift+Tab: %d", a.page)
	}
	// the keys of the Studio do nothing here any more (s would have started a pilot)
	for _, k := range []string{"s", "a", "x", "p", "N", "3", "0"} {
		taper(a, k)
		if a.page != pAccueil || a.dialogue != nil || a.form != nil {
			t.Errorf("key %s: page %d, dialog %v, form %v", k, a.page, a.dialogue != nil, a.form != nil)
		}
	}
	// "h" and "?" are the help; Esc closes it without leaving the page
	taper(a, "h")
	if !a.aide {
		t.Error("h: no help")
	}
	if txt := voir("aide-120x40", 120, 40); !strings.Contains(txt, "Arrêt d'urgence") || strings.Contains(txt, "Démarrer") {
		t.Errorf("the help:\n%s", txt)
	}
	taper(a, "\x1b")
	if a.aide || a.page != pAccueil {
		t.Errorf("Esc in the help: help %v, page %d", a.aide, a.page)
	}
}
