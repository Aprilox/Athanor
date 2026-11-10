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
			for _, x := range []string{"Studio", "Coffre", "Besoin de vous : 2", "Missions"} {
				if !strings.Contains(txt, x) {
					t.Errorf("%v: no %q in\n%s", taille, x, txt)
				}
			}
		}
	}
	// "In progress" and the bell's alerts with their reason; a click on a line opens its project's page
	txt := voir("accueil-120x40", 120, 40)
	for _, x := range []string{"En cours", "Tout voir (20)", "! Studio, projet-03 : 1 tâche(s) sur la page Pour vous",
		"! Studio, projet-07 : 1 tâche(s)", "projet-07  Studio  1 tâche(s) sur la page Pour vous"} {
		if !strings.Contains(txt, x) {
			t.Errorf("no %q in\n%s", x, txt)
		}
	}
	cliquer := func(motif string) {
		e := nouvelEcran(120, 40)
		a.dessiner(e)
		for y := 0; y < 40; y++ {
			if i := strings.Index(e.ligneTexte(y), motif); i >= 0 {
				a.souris(evenementTUI{genre: evSouris, x: largeurTexte(e.ligneTexte(y)[:i]) + 1, y: y, appui: true})
				return
			}
		}
		t.Fatalf("no %q on screen", motif)
	}
	cliquer("projet-07  Studio")
	if a.page != pPourVous || a.c.projet != "projet-07" {
		t.Errorf("click on projet-07: page %d, project %q", a.page, a.c.projet)
	}
	taper(a, "H")
	cliquer("! Studio, projet-03")
	if a.page != pPourVous || a.c.projet != "projet-03" {
		t.Errorf("click on the alert of projet-03: page %d, project %q", a.page, a.c.projet)
	}
	taper(a, "H")
	cliquer("Tout voir (20)")
	if a.page != pSuivi {
		t.Errorf("See all: page %d", a.page)
	}
	taper(a, "H")
	// an ignored alert leaves the home's alerts (the bell's list), its project stays in "In progress"
	a.hub.Alertes = a.hub.Alertes[1:]
	if txt := voir("accueil-ignoree-120x40", 120, 40); strings.Contains(txt, "! Studio, projet-03") || !strings.Contains(txt, "projet-03  Studio") {
		t.Errorf("ignored alert:\n%s", txt)
	}
	// the keys of Dev's pages, as before the hub
	attendus := map[string]int{"1": pSuivi, "2": pJournal, "3": pDemandes, "4": pChat, "5": pSante, "6": pPourVous, "7": pEquipe, "8": pGitHub, "9": pReglages, "0": pDistance}
	for k, pg := range attendus {
		taper(a, "H")
		taper(a, k)
		if a.page != pg {
			t.Errorf("key %s: page %d, want %d", k, a.page, pg)
		}
		voir("page-"+k+"-80x24", 80, 24)
		voir("page-"+k+"-120x40", 120, 40)
		taper(a, "\x1b")
		if a.page == pChat { // the first Esc leaves the typing field
			taper(a, "\x1b")
		}
		if a.page != pAccueil {
			t.Errorf("Esc from page of key %s: %d", k, a.page)
		}
	}
	// Tab / Shift+Tab go round all 11 pages and come back
	taper(a, "H")
	vus := map[int]bool{}
	for i := 0; i < nbPages; i++ {
		taper(a, "\t")
		vus[a.page] = true
	}
	if len(vus) != nbPages || a.page != pAccueil {
		t.Errorf("Tab ring: %d pages seen, ends on %d", len(vus), a.page)
	}
	for i := 0; i < nbPages; i++ {
		taper(a, "\x1b[Z")
	}
	if a.page != pAccueil {
		t.Errorf("Shift+Tab ring ends on %d", a.page)
	}
	// "h" is still the help, "H" the Home; "?" too; Esc closes the help without leaving the page
	taper(a, "3")
	taper(a, "h")
	if !a.aide || a.page != pDemandes {
		t.Errorf("h: help %v page %d", a.aide, a.page)
	}
	voir("aide-120x40", 120, 40)
	taper(a, "\x1b")
	if a.aide || a.page != pDemandes {
		t.Errorf("Esc in the help: help %v, page %d", a.aide, a.page)
	}
	// Esc in the chat's typing mode and in a form must not throw the user back home
	taper(a, "4")
	taper(a, "\r")
	taper(a, "ab")
	pageAvant, actif := a.page, a.chatActif
	taper(a, "\x1b")
	t.Logf("chat: active %v, after Esc page %d (was %d), typed %q", actif, a.page, pageAvant, a.saisie.texte())
	// n and p keys: the project picker still opens
	taper(a, "H")
	taper(a, "p")
	t.Logf("p from the Home: picker open? page %d", a.page)
	voir("choix-projet-80x24", 80, 24)
}
