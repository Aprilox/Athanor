package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTUILargeursEtLignes(t *testing.T) {
	for r, l := range map[rune]int{'a': 1, 'é': 1, '⏳': 2, '漢': 2, '🙋': 2, '́': 0, '‍': 0, '─': 1, '✔': 1} {
		if g := largeurRune(r); g != l {
			t.Errorf("largeurRune(%q) = %d, want %d", r, g, l)
		}
	}
	if g := couperTexte("abcdefgh", 5); g != "abcd…" {
		t.Errorf("couperTexte: %q", g)
	}
	if g := couperTexte("漢字漢字", 5); largeurTexte(g) > 5 || !strings.HasSuffix(g, "…") {
		t.Errorf("couperTexte wide: %q", g)
	}
	if g := completerTexte("ab", 4); g != "ab  " {
		t.Errorf("completerTexte: %q", g)
	}
	if g := envelopper("aaa bbb ccc", 7); strings.Join(g, "|") != "aaa bbb|ccc" {
		t.Errorf("envelopper: %q", g)
	}
	if g := envelopper("abcdefghij", 4); strings.Join(g, "|") != "abcd|efgh|ij" {
		t.Errorf("envelopper (long word): %q", g)
	}
	if g := envelopper("un\n\ndeux", 10); strings.Join(g, "|") != "un||deux" {
		t.Errorf("envelopper (paragraphs): %q", g)
	}
	for _, l := range envelopper("Le pilote démarre ; le premier cycle (cadrage) prend quelques minutes, puis Claude avance jalon par jalon.", 17) {
		if largeurTexte(l) > 17 {
			t.Errorf("line too long: %q", l)
		}
	}
	if g := nettoyerLigne("a\tb\x1b[31mc"); strings.ContainsRune(g, 0x1b) {
		t.Errorf("an escape left in the text: %q", g)
	}
}

func TestTUIEcranRendu(t *testing.T) {
	e := nouvelEcran(20, 4)
	st := styleT{av: hexCouleur("#ffffff"), ar: hexCouleur("#000000")}
	e.cadre(0, 0, 20, 4, "Titre", st, st)
	e.texte(2, 1, 0, "⏳ attente", st)
	if g := e.ligneTexte(0); !strings.HasPrefix(g, "╭ Titre ─") || !strings.HasSuffix(g, "╮") {
		t.Fatalf("top edge: %q", g)
	}
	if g := e.ligneTexte(1); g != "│ ⏳ attente       │" {
		t.Fatalf("a wide character: %q", g)
	}
	// a wide character overwritten in its middle: no half left
	e.mettre(3, 1, 'x', st)
	if g := e.ligneTexte(1); !strings.HasPrefix(g, "│  x attente") {
		t.Fatalf("half of a wide character: %q", g)
	}
	tout := string(e.rendu(nil, true))
	if !strings.Contains(tout, "\x1b[1;1H") || !strings.Contains(tout, "38;2;255;255;255") || !strings.Contains(tout, "Titre") {
		t.Fatalf("first frame: %q", tout)
	}
	prec := e.copie()
	e.texte(2, 2, 0, "neuf", st)
	diff := string(e.rendu(prec, false))
	if strings.Contains(diff, "\x1b[1;1H") || !strings.Contains(diff, "\x1b[3;1H") || !strings.Contains(diff, "38;5;") {
		t.Fatalf("only the row that changed, in 256 colours: %q", diff)
	}
	if c := couleur256(255, 0, 0); c != 196 {
		t.Errorf("couleur256(red) = %d", c)
	}
	if c := couleur256(128, 128, 128); c < 232 {
		t.Errorf("couleur256(grey) = %d", c)
	}
}

func TestTUITouches(t *testing.T) {
	noms := func(b string, fin bool) []string {
		evs, _ := analyserTouches([]byte(b), fin)
		var l []string
		for _, e := range evs {
			switch e.genre {
			case evSouris:
				l = append(l, "souris")
			case evCollage:
				l = append(l, "collage:"+e.texte)
			default:
				l = append(l, e.touche())
			}
		}
		return l
	}
	cas := map[string]string{
		"q":                      "q",
		"é":                      "é",
		"\x1b[A":                 "haut",
		"\x1b[1;5C":              "ctrl+droite",
		"\x1b[1;3A":              "alt+haut",
		"\x1bOP":                 "f1",
		"\x1b[5~":                "pgprec",
		"\x1b[3~":                "suppr",
		"\x1b[Z":                 "stab",
		"\x13":                   "ctrl+s",
		"\x03":                   "ctrl+c",
		"\r":                     "entree",
		"\x1b\r":                 "alt+entree",
		"\x7f":                   "retour",
		"\t":                     "tab",
		"\x1bb":                  "alt+b",
		"ab":                     "a b",
		"\x1b[200~x\ny\x1b[201~": "collage:x\ny",
	}
	for b, attendu := range cas {
		if g := strings.Join(noms(b, true), " "); g != attendu {
			t.Errorf("%q: %q, want %q", b, g, attendu)
		}
	}
	// Enter sent as CR LF or CR NUL (some terminals, SSH clients), the keypad's Enter: one Enter each
	for _, b := range []string{"\r\n", "\r\x00", "\x1bOM", "\r", "\n"} {
		if g := strings.Join(noms(b, true), " "); g != "entree" {
			t.Errorf("%q: %q, want one Enter", b, g)
		}
	}
	if g := strings.Join(noms("\r\r", true), " "); g != "entree entree" {
		t.Errorf("two Enters: %q", g)
	}
	// incomplete: waits (unless nothing more comes)
	if evs, reste := analyserTouches([]byte("\x1b"), false); len(evs) != 0 || len(reste) != 1 {
		t.Fatalf("a lone Escape while more may come: %v %q", evs, reste)
	}
	if g := noms("\x1b", true); strings.Join(g, " ") != "echap" {
		t.Fatalf("a lone Escape: %v", g)
	}
	if evs, reste := analyserTouches([]byte("a\x1b[1;"), false); len(evs) != 1 || string(reste) != "\x1b[1;" {
		t.Fatalf("a sequence cut in two: %v %q", evs, reste)
	}
	if evs, reste := analyserTouches([]byte{0xc3}, false); len(evs) != 0 || len(reste) != 1 {
		t.Fatalf("half a character: %v %q", evs, reste)
	}
	// the mouse
	evs, _ := analyserTouches([]byte("\x1b[<0;10;5M\x1b[<65;3;4M\x1b[<0;10;5m"), true)
	if len(evs) != 3 || evs[0].x != 9 || evs[0].y != 4 || !evs[0].appui || evs[0].bouton != 0 ||
		evs[1].nom != "molette" || evs[1].bouton != 1 || evs[2].appui {
		t.Fatalf("mouse: %+v", evs)
	}
}

func TestTUISaisie(t *testing.T) {
	s := &saisieT{multi: true}
	tape := func(b string) {
		evs, _ := analyserTouches([]byte(b), true)
		for _, e := range evs {
			if e.touche() == "entree" {
				s.inserer("\n")
				continue
			}
			s.touche(e)
		}
	}
	tape("bonjour\rmonde")
	if s.texte() != "bonjour\nmonde" {
		t.Fatalf("typed: %q", s.texte())
	}
	tape("\x1b[A") // up: same column of the line above
	tape("X")
	if s.texte() != "bonjoXur\nmonde" {
		t.Fatalf("up then X: %q", s.texte())
	}
	tape("\x7f\x7f")
	if s.texte() != "bonjur\nmonde" {
		t.Fatalf("backspace: %q", s.texte())
	}
	tape("\x01\x0b") // Ctrl+A, Ctrl+K
	if s.texte() != "\nmonde" {
		t.Fatalf("Ctrl+A Ctrl+K: %q", s.texte())
	}
	s.mettreTexte("abcdefghij")
	lignes, cl, cc := s.lignesSaisie(4)
	if strings.Join(lignes, "|") != "abcd|efgh|ij" || cl != 2 || cc != 2 {
		t.Fatalf("wrapped: %q %d %d", lignes, cl, cc)
	}
	un := &saisieT{}
	un.inserer("a\nb")
	if un.texte() != "a b" {
		t.Fatalf("one line field: %q", un.texte())
	}
	m := &saisieT{masque: true}
	m.inserer("secret")
	if l, _, _ := m.lignesSaisie(20); strings.Contains(l[0], "secret") {
		t.Fatalf("a password shown: %q", l)
	}
}

func TestTUITraduction(t *testing.T) {
	fr := dicoTUI("fr")
	if fr == nil || fr["Start"] != "Démarrer" {
		t.Fatalf("French: %v", fr["Start"])
	}
	// i18n.js checked out on Windows (CRLF line ends): read all the same
	if b, err := statique.ReadFile("static/i18n.js"); err != nil {
		t.Fatal(err)
	} else if d := lireDicoJS(strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n", "\r\n"), "fr"); d["Start"] != "Démarrer" || len(d) != len(fr) {
		t.Fatalf("CRLF: %v, %d keys of %d", d["Start"], len(d), len(fr))
	}
	if g := traduireTUI(fr, "{0} / {1} milestones done", 3, 10); !strings.Contains(g, "3") || !strings.Contains(g, "10") || strings.Contains(g, "milestones") {
		t.Fatalf("arguments: %q", g)
	}
	if g := traduireTUI(nil, "cycle {0}", int64(4)); g != "cycle 4" {
		t.Fatalf("English: %q", g)
	}
	for _, l := range []string{"es", "de", "it", "pt"} {
		if d := dicoTUI(l); len(d) < 1000 {
			t.Fatalf("%s: %d keys", l, len(d))
		}
	}
	// every text the terminal shows is translated (its own keys included)
	b, _ := os.ReadFile("tui_pages.go")
	c, _ := os.ReadFile("tui_actions.go")
	d, _ := os.ReadFile("tui_app.go")
	u, _ := os.ReadFile("tui_terminal_unix.go")
	p2, _ := os.ReadFile("tui_pages2.go")
	u = append(u, p2...)
	src := string(b) + string(c) + string(d) + strings.ReplaceAll(string(u), "app.t(", "a.t(")
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, cle := range clesTUI(src) {
			if _, ok := dico[cle]; !ok {
				t.Errorf("%s: no translation of %q", l, cle)
			}
		}
	}
}

// clesTUI: the keys given to t("…") / a.t("…") in the terminal's sources.
func clesTUI(src string) []string {
	var l []string
	vu := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:a\.|[^A-Za-z0-9_.])t\("((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(src, -1) {
		cle := strings.NewReplacer(`\"`, `"`, `\n`, "\n").Replace(m[1])
		if !vu[cle] {
			vu[cle] = true
			l = append(l, cle)
		}
	}
	for _, cle := range clesPages {
		if !vu[cle] {
			l = append(l, cle)
		}
	}
	return append(l, aideFormulaireMulti, aideFormulaireLigne)
}

func TestTUIInterface(t *testing.T) {
	cas := []struct {
		args, reste []string
		voulue      string
	}{
		{[]string{"tui"}, nil, "tui"},
		{[]string{"--gui"}, nil, "gui"},
		{[]string{"--kit=/x", "terminal"}, []string{"--kit=/x"}, "tui"},
		{[]string{"github", "lier", "tui"}, []string{"github", "lier", "tui"}, ""},
		{nil, nil, ""},
	}
	for _, c := range cas {
		interfaceVoulue = ""
		r := lireInterface(c.args)
		if strings.Join(r, " ") != strings.Join(c.reste, " ") || interfaceVoulue != c.voulue {
			t.Errorf("%v: %v %q", c.args, r, interfaceVoulue)
		}
	}
	interfaceVoulue = "tui"
	if g := avecInterface([]string{"x"}); strings.Join(g, " ") != "tui x" {
		t.Errorf("avecInterface: %v", g)
	}
	interfaceVoulue = ""
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if !devinerTerminal("linux", true, env(nil)) {
		t.Error("Linux without a screen, from a terminal: the terminal interface")
	}
	if devinerTerminal("linux", true, env(map[string]string{"DISPLAY": ":0"})) {
		t.Error("a Linux desktop: the window")
	}
	if !devinerTerminal("darwin", true, env(map[string]string{"SSH_CONNECTION": "1 2 3 4"})) {
		t.Error("through SSH: the terminal interface")
	}
	if devinerTerminal("linux", false, env(nil)) || devinerTerminal("windows", true, env(nil)) {
		t.Error("no terminal, or Windows: the window")
	}
}

// essaiServeurTUI: Athanor's server with one project, and the terminal's client on it.
func essaiServeurTUI(t *testing.T) (*Serveur, *clientTUI, Projet) {
	t.Helper()
	dossier := t.TempDir()
	p := Projet{Nom: "essai", Dossier: dossier}
	kit := t.TempDir()
	_ = os.MkdirAll(filepath.Join(kit, "template"), 0o755)
	_ = os.WriteFile(filepath.Join(kit, "template", "CLAUDE.md"), []byte("# x\n"), 0o644)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "essai", Langue: "fr"})
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
	if _, err := nouveauClientTUI(srv.URL + "/"); err == nil {
		t.Fatal("the address alone gave access")
	}
	c, err := nouveauClientTUI(s.lienOuverture(srv.URL + "/"))
	if err != nil {
		t.Fatal(err)
	}
	return s, c, p
}

// attendreTUI: the server's answers applied until cond holds.
func attendreTUI(t *testing.T, a *appTUI, cond func() bool) {
	t.Helper()
	fin := time.After(10 * time.Second)
	for !cond() {
		select {
		case f := <-a.majs:
			f(a)
		case <-fin:
			t.Fatalf("timed out (the last message: %q)", a.toast)
		case <-time.After(50 * time.Millisecond):
			a.tic()
		}
	}
}

func taper(a *appTUI, b string) {
	evs, _ := analyserTouches([]byte(b), true)
	for _, e := range evs {
		a.touche(e)
	}
}

func TestTUIContreLeServeur(t *testing.T) {
	_, c, p := essaiServeurTUI(t)
	a := nouvelleAppTUI(c, true)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.c.projet == "essai" })
	e := nouvelEcran(120, 32)
	a.dessiner(e)
	ecran := e.texteEcran()
	for _, s := range []string{"Athanor", "essai", "Suivi", "Journal", "Demandes"} {
		if !strings.Contains(ecran, s) {
			t.Fatalf("overview: no %q in\n%s", s, ecran)
		}
	}
	// the state's sentence, in French (the tooling is seen installed or not, depending on the computer)
	if !strings.Contains(ecran, "En attente de votre première demande") && !strings.Contains(ecran, "Prêt à installer") {
		t.Fatalf("overview: the state's sentence\n%s", ecran)
	}
	// a request through the form: n, its title, Tab, its text, Ctrl+S
	taper(a, "3")
	attendreTUI(t, a, func() bool { return a.dem != nil })
	taper(a, "n")
	if a.form == nil {
		t.Fatal("no form")
	}
	taper(a, "Accueil\tUne page simple.\rAvec un titre.\x13")
	attendreTUI(t, a, func() bool {
		b, _ := os.ReadFile(filepath.Join(p.Atelier(), "REQUEST.md"))
		return strings.Contains(string(b), "Une page simple.\nAvec un titre.")
	})
	attendreTUI(t, a, func() bool {
		return strings.Contains(a.toast, "Demande enregistrée") || strings.Contains(a.toast, "enregistr")
	})
	// a second one goes to the queue, then is deleted (d, then the dialog's "Delete")
	taper(a, "nSuite\tEncore une chose.\x13")
	attendreTUI(t, a, func() bool { return len(p.LireFileAttente()) == 1 })
	a.derniere["demandes"] = time.Time{}
	attendreTUI(t, a, func() bool { return a.dem != nil && len(a.dem.Attente) == 1 })
	e = nouvelEcran(120, 32)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "Suite") {
		t.Fatalf("the queue:\n%s", e.texteEcran())
	}
	taper(a, "d")
	if a.dialogue == nil {
		t.Fatal("no question before deleting")
	}
	taper(a, "\x1b[B\r")
	attendreTUI(t, a, func() bool { return len(p.LireFileAttente()) == 0 })
	// every page draws, wide and narrow, without a panic
	for _, taille := range [][2]int{{160, 45}, {80, 24}, {60, 16}, {20, 5}} {
		for pg := 0; pg < nbPages; pg++ {
			a.page = pg
			e := nouvelEcran(taille[0], taille[1])
			a.dessiner(e)
			if taille[0] >= 80 && !strings.Contains(e.ligneTexte(0), "Athanor") {
				t.Fatalf("%v page %d: top bar %q", taille, pg, e.ligneTexte(0))
			}
		}
	}
	a.page = pSuivi
	// the working mode, from Manage (M): the eco mode, then seen ticked
	taper(a, "M")
	if a.dialogue == nil {
		t.Fatal("no Manage dialog")
	}
	taper(a, "\x1b[B\x1b[B\r")
	attendreTUI(t, a, func() bool { return a.dialogue != nil && a.dialogue.titre == "Mode de travail" })
	taper(a, "\x1b[B\x1b[B\r")
	attendreTUI(t, a, func() bool { return p.EcoActif() && strings.Contains(a.toast, "Mode éco activé") })
	taper(a, "M\x1b[B\x1b[B\r")
	attendreTUI(t, a, func() bool { return a.dialogue != nil && a.dialogue.titre == "Mode de travail" })
	if d := a.dialogue; d.sel != 2 || !strings.HasSuffix(d.choix[2].texte, "✓") || !strings.Contains(d.texte, "Sonnet") {
		t.Fatalf("working mode: %+v", d)
	}
	taper(a, "\x1b")
	if m := p.LireMode(); m.Parallele || !m.Eco {
		t.Fatalf("mode: %+v", m)
	}
	// the help, then quitting asks first
	taper(a, "?")
	if !a.aide {
		t.Fatal("no help")
	}
	taper(a, " q")
	if a.dialogue == nil || a.quitter {
		t.Fatal("quit without asking")
	}
	taper(a, "\r")
	if !a.quitter {
		t.Fatal("not quitting")
	}
}

// A locked Athanor (Settings › Lock): the password first, then the pages.
func TestTUIVerrou(t *testing.T) {
	s, c, _ := essaiServeurTUI(t)
	s.mu.Lock()
	s.config.Sel = "sel"
	s.config.MotDePasseHash = hacher("sel", "secret")
	s.mu.Unlock()
	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.form != nil })
	taper(a, "faux\r")
	attendreTUI(t, a, func() bool { return a.toastErr && a.form != nil })
	taper(a, "\x15secret\r") // Ctrl+U clears the field
	attendreTUI(t, a, func() bool { return a.vue != nil && !a.vue.Verrouille && a.c.projet == "essai" })
	e := nouvelEcran(100, 30)
	a.dessiner(e)
	if strings.Contains(e.texteEcran(), "secret") {
		t.Fatal("the password on the screen")
	}
}

func TestTUIClicsEtMolette(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, KitInstalle: true, Projets: []ProjetVue{{Nom: "a", Courant: true}, {Nom: "b"}}, Projet: &ProjetVue{Nom: "a"}}
	for i := 0; i < 30; i++ {
		a.vue.Plan = append(a.vue.Plan, Jalon{ID: "M" + itoa64(int64(i)), Titre: "Jalon " + itoa64(int64(i))})
	}
	e := nouvelEcran(140, 30)
	a.dessiner(e)
	// a click on project b in the left column
	y := -1
	for k := 0; k < e.h; k++ {
		if strings.Contains(e.ligneTexte(k), "● b") {
			y = k
		}
	}
	if y < 0 {
		t.Fatalf("project b not shown:\n%s", e.texteEcran())
	}
	a.souris(evenementTUI{genre: evSouris, x: 5, y: y, appui: true})
	if a.c.projet != "b" {
		t.Fatalf("the click: project %q", a.c.projet)
	}
	// a click on the page Log
	a.dessiner(e)
	for k := 0; k < e.h; k++ {
		if strings.Contains(e.ligneTexte(k), "2 Journal") || strings.Contains(e.ligneTexte(k), "2 Log") {
			a.souris(evenementTUI{genre: evSouris, x: 5, y: k, appui: true})
		}
	}
	if a.page != pJournal {
		t.Fatalf("the click on Log: page %d", a.page)
	}
	// the wheel scrolls the plan
	a.page = pSuivi
	a.c.projet = "a"
	a.dessiner(e)
	a.souris(evenementTUI{genre: evSouris, nom: "molette", bouton: 1, x: 120, y: 10})
	if a.defilPlan != 3 {
		t.Fatalf("the wheel: %d", a.defilPlan)
	}
}

// The form's buttons: Tab reaches Save and Cancel, Enter presses them; F2 and Alt+S save too (a terminal
// may keep Ctrl+S).
func TestTUIFormulaireBoutons(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{}, false)
	var recu []string
	ouvrir := func() {
		a.formulaire("x", "", []champTUI{{lib: "a", h: 1}, {lib: "b", h: 4}}, func(v []string) { recu = v })
	}
	ouvrir()
	taper(a, "un\tdeux\rtrois\t") // Tab past the two fields: Save
	if a.form == nil || a.form.sel != 2 {
		t.Fatalf("Tab to the Save button: %+v", a.form)
	}
	taper(a, "\r")
	if a.form != nil || strings.Join(recu, "|") != "un|deux\ntrois" {
		t.Fatalf("Save: %q", recu)
	}
	recu = nil
	ouvrir()
	taper(a, "x\t\t\t\r") // Cancel: what was typed is not thrown away without asking
	if a.form == nil || a.dialogue == nil {
		t.Fatal("Cancel with text: no question")
	}
	taper(a, "\r") // Keep editing (the default)
	if a.form == nil || a.dialogue != nil || a.form.champs[0].s.texte() != "x" {
		t.Fatal("Keep editing")
	}
	taper(a, "\x1b\x1b[B\r") // Escape, then Discard
	if a.form != nil || recu != nil {
		t.Fatalf("Discard: %q", recu)
	}
	ouvrir()
	taper(a, "\x1b") // an empty form closes at once
	if a.form != nil || a.dialogue != nil {
		t.Fatal("an empty form asked")
	}
	for _, k := range []string{"\x1bOQ", "\x1bs", "\x13"} { // F2, Alt+S, Ctrl+S
		recu = nil
		ouvrir()
		taper(a, "y"+k)
		if a.form != nil || len(recu) == 0 || recu[0] != "y" {
			t.Fatalf("%q: %q", k, recu)
		}
	}
	// the buttons are drawn and clickable
	ouvrir()
	a.vue = &EtatVue{Version: versionAthanor}
	e := nouvelEcran(100, 30)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "Save") || !strings.Contains(e.texteEcran(), "Cancel") {
		t.Fatalf("no buttons:\n%s", e.texteEcran())
	}
	for y := 0; y < e.h; y++ {
		if x := strings.Index(e.ligneTexte(y), " Save "); x >= 0 {
			recu = nil
			a.souris(evenementTUI{genre: evSouris, x: largeurTexte(e.ligneTexte(y)[:x]) + 2, y: y, appui: true})
		}
	}
	if a.form != nil || recu == nil {
		t.Fatal("a click on Save")
	}
}

// A refused form (a system folder for a new project) stays open with the server's reason, what was typed
// kept.
func TestTUIFormulaireRefuse(t *testing.T) {
	_, c, _ := essaiServeurTUI(t)
	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil })
	taper(a, "N")
	taper(a, "essai2\t/usr/essai2\tune page\x1bOQ") // F2
	attendreTUI(t, a, func() bool { return a.form != nil && a.form.erreur != "" })
	// the reason itself is the server's, its words depend on the system (a Windows path is asked there)
	if a.form.champs[1].s.texte() != "/usr/essai2" || runtime.GOOS != "windows" && !strings.Contains(a.form.erreur, "système") {
		t.Fatalf("the form: %q %q", a.form.erreur, a.form.champs[1].s.texte())
	}
	e := nouvelEcran(110, 34)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "✖") {
		t.Fatalf("the reason not shown:\n%s", e.texteEcran())
	}
}

// The pages 6 to 9 draw in every state (nothing read yet, empty, filled), wide and narrow.
func TestTUIPages2(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, Projets: []ProjetVue{{Nom: "a"}}, Projet: &ProjetVue{Nom: "a"}}
	dessinerTout := func(etape string) {
		for _, taille := range [][2]int{{150, 40}, {90, 26}, {50, 14}} {
			for _, pg := range []int{pPourVous, pEquipe, pGitHub, pReglages} {
				a.page = pg
				e := nouvelEcran(taille[0], taille[1])
				a.dessiner(e)
				if taille[0] >= 90 && !strings.Contains(e.texteEcran(), "Athanor") {
					t.Fatalf("%s %v page %d", etape, taille, pg)
				}
			}
		}
	}
	dessinerTout("nothing read")
	a.taches, a.agents, a.env = []TacheUtilVue{}, []Agent{}, []LigneEnv{}
	a.gh = &GitHubVue{}
	dessinerTout("empty")
	a.taches = []TacheUtilVue{{TacheUtilisateur: TacheUtilisateur{ID: "T1", Titre: "Compte", Details: "Créez-le.", Attendu: "la clé", Secrets: []string{"K"}}, Etat: "pour_vous", Reponses: []ReponseTache{{Action: "fait", Texte: "fait"}}}}
	a.agents = []Agent{{ID: "c", Genre: "chef", Statut: "travaille", Tours: 3, Modele: "opus"}, {ID: "s", Nom: "executor", Genre: "sous_agent", Parent: "c", Statut: "termine"}, {ID: "o", Genre: "ouvrier", Jalon: "M5", Statut: "attend"}}
	a.env = []LigneEnv{{Cle: "CLAUDE_CODE_OAUTH_TOKEN", Valeur: "••••", Secret: true, Aide: "Login token"}, {Cle: "MODEL", Aide: "Model"}}
	a.gh = &GitHubVue{Lie: true, Depot: "moi/site", Branche: "main", Auto: true, ConnexionPC: ConnexionGH{Connecte: true, Login: "moi"}, Dernier: []byte(`{"quand":"2026-10-25","resultat":"ok"}`)}
	dessinerTout("filled")
	a.page = pEquipe
	e := nouvelEcran(150, 40)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "Worker M5") || !strings.Contains(e.texteEcran(), "↳ ● executor") {
		t.Fatalf("team:\n%s", e.texteEcran())
	}
	a.page = pReglages
	a.dessiner(e)
	if strings.Contains(e.texteEcran(), "sk-ant") {
		t.Fatal("a secret shown")
	}
	// a page's own letters come first, small or capital: p is the palette in Settings, never the project
	// chooser; a the accent, never Stop after cycle
	for _, k := range []string{"p", "P"} {
		a.page, a.dialogue = pReglages, nil
		taper(a, k)
		if a.dialogue == nil || a.dialogue.titre != "Palette" {
			t.Fatalf("%s in Settings: %+v", k, a.dialogue)
		}
	}
	a.dialogue = nil
	taper(a, "a")
	if a.dialogue == nil || a.dialogue.titre != "Accent" {
		t.Fatalf("a in Settings: %+v", a.dialogue)
	}
	a.dialogue = nil
	taper(a, "l")
	if a.dialogue == nil || a.dialogue.titre != "Language" {
		t.Fatalf("l in Settings: %+v", a.dialogue)
	}
	// on GitHub: d disconnects (asked first), r creates a repository (a form)
	a.page, a.dialogue = pGitHub, nil
	taper(a, "D")
	if a.dialogue == nil || a.dialogue.titre != "Disconnect" {
		t.Fatalf("D on GitHub: %+v", a.dialogue)
	}
	a.dialogue = nil
	taper(a, "r")
	if a.form == nil || a.form.titre != "Create a private repository" {
		t.Fatalf("r on GitHub: %+v", a.form)
	}
	a.form = nil
	// a setting's form, opened with its value: closed unchanged, nothing asked; changed, asked first
	a.page, a.selReglage = pReglages, 1
	a.env[1].Valeur = "opus"
	taper(a, "\r\n") // one Enter (CR LF): the form opens and stays open
	if a.form == nil || a.form.champs[0].s.texte() != "opus" {
		t.Fatalf("Enter on a setting: %+v", a.form)
	}
	taper(a, "\x1b")
	if a.form != nil || a.dialogue != nil {
		t.Fatalf("Esc on an unchanged form: %v %v", a.form != nil, a.dialogue != nil)
	}
	taper(a, "\r")
	taper(a, "x\x1b")
	if a.dialogue == nil || a.form == nil {
		t.Fatal("Esc on a changed form: not asked")
	}
	a.dialogue, a.form = nil, nil
	// elsewhere, p is still the project chooser
	a.page = pSuivi
	taper(a, "p")
	if a.dialogue == nil || a.dialogue.titre == "Palette" {
		t.Fatalf("p on the Overview: %+v", a.dialogue)
	}
}

// The Overview: the local commit, then the one GitHub has (✓ when the same, what is not pushed yet, the
// automatic push off, why the last push did not go).
func TestTUISuiviGitHub(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, KitInstalle: true, Projets: []ProjetVue{{Nom: "a"}}, Projet: &ProjetVue{Nom: "a"},
		Etat: &Etat{DernierCommit: "abc12345 M9: the shop"}}
	ecran := func() string {
		e := nouvelEcran(150, 40)
		a.dessiner(e)
		return e.texteEcran()
	}
	if s := ecran(); !strings.Contains(s, "Local commit") || strings.Contains(s, "On GitHub") {
		t.Fatalf("not linked yet, nothing read: %s", s)
	}
	a.vue.GitHub = &SuiviGitHub{Lie: true, Depot: "moi/site", Branche: "main", Auto: true, Envoye: &envoiGH{Quand: "2026-10-25T21:00:00", Commit: "abc1234 M9: the shop"}}
	if s := ecran(); !strings.Contains(s, "On GitHub") || !strings.Contains(s, "abc1234 M9: the shop") || !strings.Contains(s, "✓") {
		t.Fatalf("up to date: %s", s)
	}
	a.vue.Etat.DernierCommit = "def5678 M10: the cart"
	if s := ecran(); strings.Contains(s, "✓") || !strings.Contains(s, "local one is not pushed yet") {
		t.Fatalf("M10 not pushed yet: %s", s)
	}
	a.vue.GitHub.Auto = false
	a.vue.GitHub.Dernier = &struct {
		Quand    string   `json:"quand"`
		Resultat string   `json:"resultat"`
		Modele   string   `json:"modele"`
		Args     []string `json:"args"`
		Commit   string   `json:"commit"`
	}{"2026-10-25T22:00:00", "rejete", "GitHub's branch {0} has commits the project does not have. Athanor never overwrites them: choose another branch, or bring those commits into the project.", []string{"main"}, "def5678 M10: the cart"}
	if s := ecran(); !strings.Contains(s, "automatic push off") || !strings.Contains(s, "GitHub's branch main has commits") {
		t.Fatalf("off, refused: %s", s)
	}
	a.vue.GitHub = &SuiviGitHub{}
	if s := ecran(); !strings.Contains(s, "not linked to GitHub") {
		t.Fatalf("unlinked: %s", s)
	}
}
