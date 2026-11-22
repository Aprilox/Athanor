package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	p3, _ := os.ReadFile("tui_distance.go")
	u = append(u, p3...)
	p4, _ := os.ReadFile("tui_accueil.go")
	u = append(u, p4...)
	p5, _ := os.ReadFile("tui_missions.go")
	u = append(u, p5...)
	p6, _ := os.ReadFile("tui_approbations.go")
	u = append(u, p6...)
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
		cle := cleJS(m[1])
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
	return append(l, aideFormulaireLigne)
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
	return s, clientTUIPour(t, s), p
}

// clientTUIPour: the terminal's access to s, served by a test server (the address alone refused).
func clientTUIPour(t *testing.T, s *Serveur) *clientTUI {
	t.Helper()
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
	return c
}

// attendreTUI: the server's answers applied until cond holds.
func attendreTUI(t *testing.T, a *appTUI, cond func() bool) {
	t.Helper()
	fin := time.After(30 * time.Second) // under the full check's parallel shards a TUI step took over 10 s (M267, M271)
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
	_, c, _ := essaiServeurTUI(t)
	a := nouvelleAppTUI(c, true)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.c.projet == "essai" })
	taper(a, "1") // the launch shows the home (TestTUIAccueil)
	e := nouvelEcran(120, 32)
	a.dessiner(e)
	ecran := e.texteEcran()
	for _, s := range []string{"Athanor", "essai", "Accueil", "À distance", "Arrêt d'urgence"} {
		if !strings.Contains(ecran, s) {
			t.Fatalf("home: no %q in\n%s", s, ecran)
		}
	}
	// the state's sentence, in French (the tooling is seen installed or not, depending on the computer)
	if !strings.Contains(ecran, "En attente de votre première demande") && !strings.Contains(ecran, "Prêt à installer") {
		t.Fatalf("home: the state's sentence\n%s", ecran)
	}
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
	a.page = pAccueil
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
	s.config.MotDePasseHash = hacherMotDePasse("secret")
	s.mu.Unlock()
	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.form != nil })
	// the locked screen: the form alone, its button says what it does
	ev := nouvelEcran(80, 24)
	a.dessiner(ev)
	if tx := ev.texteEcran(); !strings.Contains(tx, a.t("Unlock")) || strings.Contains(tx, a.t("Save")) || strings.Contains(tx, a.t("Updating…")) || strings.Contains(tx, "1…0") {
		t.Fatalf("locked screen:\n%s", tx)
	}
	a.abandonnerFormulaire() // Escape: the prompt comes back at the next refresh
	a.derniere["etat"] = time.Time{}
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

// The mouse: a click on a tab changes the page; the wheel moves nothing (nothing scrolls by it any more).
func TestTUIClicsEtMolette(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, KitInstalle: true, Projets: []ProjetVue{{Nom: "a", Courant: true}}, Projet: &ProjetVue{Nom: "a"}}
	e := nouvelEcran(140, 30)
	cliquer := func(motif string) {
		a.dessiner(e)
		if x := strings.Index(e.ligneTexte(1), motif); x >= 0 {
			a.souris(evenementTUI{genre: evSouris, x: largeurTexte(e.ligneTexte(1)[:x]) + 1, y: 1, appui: true})
			return
		}
		t.Fatalf("no tab %q: %q", motif, e.ligneTexte(1))
	}
	cliquer("2 Remote")
	if a.page != pDistance {
		t.Fatalf("the click on Remote: page %d", a.page)
	}
	cliquer("1 Home")
	if a.page != pAccueil {
		t.Fatalf("the click on Home: page %d", a.page)
	}
	a.souris(evenementTUI{genre: evSouris, nom: "molette", bouton: 1, x: 70, y: 10})
	if a.page != pAccueil || a.dialogue != nil {
		t.Fatalf("the wheel: page %d", a.page)
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

// Remote: the switch, the pairing link and its QR code drawn in the terminal, the digits hidden until v, the
// trusted devices renamed or removed (asked first); every state draws, wide and narrow.
func TestTUIDistance(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, Projets: []ProjetVue{{Nom: "a"}}, Projet: &ProjetVue{Nom: "a"}}
	a.page = pDistance
	vues := []*DistanceVue{nil, {Etat: "arret", Appareils: []AppareilVue{}}, {Actif: true, Etat: "erreur", Message: "no network"},
		{Actif: true, Etat: "pret", Adresse: "https://a-b-c-d.trycloudflare.com", Lien: "https://a-b-c-d.trycloudflare.com/appairer?c=0123456789abcdef0123456789abcdef",
			Expire: 100, Chiffres: "123456", ChiffresExpire: 15000,
			Appareils: []AppareilVue{{ID: "x1", Nom: "iPhone", EnLigne: true, Lieu: "1.2.3.4"}, {ID: "x2", Nom: "Pixel", VuLe: "2026-10-25T21:00:00Z", Notifs: true}}}}
	for _, v := range vues {
		a.dist = v
		for _, taille := range [][2]int{{160, 45}, {90, 26}, {60, 16}, {20, 5}} {
			a.dessiner(nouvelEcran(taille[0], taille[1]))
		}
	}
	e := nouvelEcran(150, 45)
	a.dessiner(e)
	s := e.texteEcran()
	if !strings.Contains(s, "▀") || !strings.Contains(s, "appairer?c=") || !strings.Contains(s, "iPhone") || !strings.Contains(s, "Connected now") {
		t.Fatalf("pairing:\n%s", s)
	}
	if strings.Contains(s, "123 456") {
		t.Fatal("the digits shown before v")
	}
	taper(a, "v")
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "123 456") {
		t.Fatalf("v: the digits not shown:\n%s", e.texteEcran())
	}
	if m := matriceQR(a.dist.Lien); len(m) == 0 || len(m) != len(m[0]) || m[0][0] || !m[2][2] {
		t.Fatalf("QR code: %d", len(m))
	}
	taper(a, "\x1b[B")
	if a.selApp != 1 {
		t.Fatalf("down: %d", a.selApp)
	}
	taper(a, "d")
	if a.dialogue == nil || a.dialogue.titre != "Remove from the trusted devices" || !strings.Contains(a.dialogue.texte, "Pixel") {
		t.Fatalf("d: %+v", a.dialogue)
	}
	a.dialogue = nil
	taper(a, "r")
	if a.form == nil || a.form.champs[0].s.texte() != "Pixel" {
		t.Fatalf("r: %+v", a.form)
	}
	a.form = nil
	taper(a, "o")
	if a.dialogue == nil || a.dialogue.titre != "Remote access" {
		t.Fatalf("o (switching off is asked first): %+v", a.dialogue)
	}
	a.dialogue = nil
	// the QR code first, whole and at the same place whatever is written beside it, down to 80 × 24
	pret := *vues[len(vues)-1]
	for _, m := range []string{"", "a long error message that wraps over several lines of the column beside the code"} {
		pret.Message = m
		a.dist, a.page = &pret, pDistance
		e := nouvelEcran(80, 24)
		a.dessiner(e)
		q := matriceQR(pret.Lien)
		for r := 0; r < (len(q)+1)/2; r++ {
			if l := []rune(e.ligneTexte(2 + r)); len(l) < 1+len(q) || strings.Trim(string(l[1:1+len(q)]), "▀") != "" {
				t.Fatalf("message %q: QR row %d not whole at the top left: %q", m, r, e.ligneTexte(2+r))
			}
		}
		if strings.Contains(e.texteEcran(), "too small") {
			t.Fatalf("80 × 24 said too small:\n%s", e.texteEcran())
		}
	}
	// the address: m asks for your domain; while signing in, the link to open on another device; t goes back
	// to a temporary address, asked first
	dv := DistanceVue{Actif: true, Etat: "arret", Appareils: []AppareilVue{}}
	a.dist, a.page = &dv, pDistance
	taper(a, "m")
	if a.form == nil || !strings.Contains(a.form.champs[0].lib, "athanor.example.com") {
		t.Fatalf("m: %+v", a.form)
	}
	a.form = nil
	dv.DomaineEtape, dv.DomaineLien = "connexion", "https://dash.cloudflare.com/argotunnel?aud=&callback=https%3A%2F%2Flogin.cloudflareaccess.org%2Fx"
	e = nouvelEcran(120, 40)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "https://dash.cloudflare.com/argotunnel") || !strings.Contains(e.texteEcran(), "c Cancel") {
		t.Fatalf("signing in:\n%s", e.texteEcran())
	}
	dv.DomaineEtape, dv.DomaineLien, dv.Domaine = "", "", "athanor.example.com"
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "https://athanor.example.com") {
		t.Fatalf("the domain:\n%s", e.texteEcran())
	}
	taper(a, "t")
	if a.dialogue == nil || a.dialogue.titre != "Back to a temporary address" {
		t.Fatalf("t: %+v", a.dialogue)
	}
	a.dialogue = nil
	// 2 goes to Remote, from the Home
	a.page = pAccueil
	taper(a, "2")
	if a.page != pDistance {
		t.Fatalf("2: page %d", a.page)
	}
}

// Remote against the server: the link and the digits read on the terminal's screen pair a device (as a
// phone would, through the tunnel's server), which then shows in the trusted devices; d removes it (its key
// refused at once), o switches remote access off.
func TestTUIDistanceContreLeServeur(t *testing.T) {
	kit := t.TempDir()
	_ = os.MkdirAll(filepath.Join(kit, "template"), 0o755)
	_ = os.WriteFile(filepath.Join(kit, "template", "CLAUDE.md"), []byte("# x\n"), 0o644)
	s := nouveauServeur(Config{Kit: kit})
	s.cleAcces = aleatoire(24)
	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.pageAcces(w, r) {
			_, _ = w.Write([]byte(`<!doctype html><meta name="csrf" content="` + s.csrf + `">`))
		}
	})
	s.mux = mux
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := nouveauClientTUI(s.lienOuverture(srv.URL + "/"))
	if err != nil {
		t.Fatal(err)
	}
	// remote access on, its tunnel ready (no cloudflared in a test)
	s.config.Distance.Actif = true
	s.dist().tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
	tunnel := s.gestionDistance(mux)

	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil })
	taper(a, "2")
	attendreTUI(t, a, func() bool { return a.dist != nil && a.dist.Lien != "" })
	e := nouvelEcran(160, 45)
	a.dessiner(e)
	ecran := e.texteEcran()
	// the link, cut at the width of its column: its pieces on the lines under it
	lien := ""
	for _, li := range strings.Split(ecran, "\n") {
		_, texte, _ := strings.Cut(li, "│ ") // the page's box
		champs := strings.Fields(texte)
		if lien == "" && len(champs) > 0 && strings.HasPrefix(champs[0], "https://abc-def.trycloudflare.com/appairer?c=") {
			lien = champs[0]
		} else if lien != "" && len(champs) > 0 && regexp.MustCompile(`^[0-9a-f]+$`).MatchString(champs[0]) {
			lien += champs[0]
		} else if lien != "" {
			break
		}
	}
	if lien == "" || lien != a.dist.Lien {
		t.Fatalf("the link on the screen %q, the server's %q:\n%s", lien, a.dist.Lien, ecran)
	}
	// the QR code on the screen is the link's
	m := matriceQR(lien)
	trouve := false
	for y := 0; y < e.h && !trouve; y++ {
		for x := 0; x < e.l && !trouve; x++ {
			if e.c[y*e.l+x].r != '▀' {
				continue
			}
			trouve = true
			for r := 0; r < len(m); r++ {
				for i := range m[r] {
					cel := e.c[(y+r/2)*e.l+x+i]
					col := cel.s.av
					if r%2 == 1 {
						col = cel.s.ar
					}
					if cel.r != '▀' || (col == 0) != m[r][i] {
						t.Fatalf("QR code: module (%d, %d) differs", i, r)
					}
				}
			}
		}
	}
	if !trouve {
		t.Fatalf("no QR code on the screen:\n%s", ecran)
	}
	// the phone scans it, then types the digits the terminal shows on v
	w := appelDistance(tunnel, "GET", strings.TrimPrefix(lien, "https://abc-def.trycloudflare.com"), "", "")
	dem := valeurCookie(w, cookieAppairage)
	if w.Code != http.StatusOK || dem == "" {
		t.Fatalf("scan: %d", w.Code)
	}
	if strings.Contains(ecran, chiffresLisiblesTest(a.dist.Chiffres)) {
		t.Fatal("the digits shown before v")
	}
	taper(a, "v")
	a.dessiner(e)
	chiffres := regexp.MustCompile(`\b(\d{3}) (\d{3})\b`).FindStringSubmatch(e.texteEcran())
	if chiffres == nil {
		t.Fatalf("v: no digits:\n%s", e.texteEcran())
	}
	w = appelDistance(tunnel, "POST", routeAppairer, cookieAppairage+"="+dem, "chiffres="+url.QueryEscape(chiffres[0]), "Origin", "https://abc-def.trycloudflare.com")
	cle := valeurCookie(w, cookieAppareil)
	if w.Code != http.StatusSeeOther || cle == "" {
		t.Fatalf("digits: %d %s", w.Code, w.Body.String())
	}
	if w := appelDistance(tunnel, "GET", "/api/etat", cle, "", "X-Athanor", s.csrf); w.Code != http.StatusOK {
		t.Fatalf("the paired device: %d", w.Code)
	}
	a.derniere["distance"] = time.Time{}
	attendreTUI(t, a, func() bool { return len(a.dist.Appareils) == 1 })
	a.dessiner(e)
	if nom := a.dist.Appareils[0].Nom; !strings.Contains(e.texteEcran(), nom) || !strings.Contains(e.texteEcran(), "Connected now") {
		t.Fatalf("the device not listed:\n%s", e.texteEcran())
	}
	// removed from the terminal: its next request is refused
	taper(a, "d")
	if a.dialogue == nil {
		t.Fatal("d: not asked")
	}
	taper(a, "\x1b[B\r")
	attendreTUI(t, a, func() bool { return len(s.vueDistance().Appareils) == 0 })
	if w := appelDistance(tunnel, "GET", "/api/etat", cle, "", "X-Athanor", s.csrf); w.Code == http.StatusOK {
		t.Fatal("the removed device still gets in")
	}
	// o: switched off, asked first
	taper(a, "o")
	taper(a, "\x1b[B\r")
	attendreTUI(t, a, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.config.Distance.Actif
	})
	a.derniere["distance"] = time.Time{}
	attendreTUI(t, a, func() bool { return a.dist != nil && !a.dist.Actif && a.dist.Lien == "" })
}

// TestTUIAccueil: the hub's home at launch, with the tiles of GET /api/hub, its key H (free on every
// page), Escape back to it, 1…9, 0 unchanged.
func TestTUIAccueil(t *testing.T) {
	// B91 : un dossier d'Athanor à lui, sans les missions laissées par les tests précédents du même éclat
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	s, c, _ := essaiServeurTUI(t)
	a := nouvelleAppTUI(c, true)
	if a.page != pAccueil {
		t.Fatalf("launch page %d", a.page)
	}
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.hub != nil })
	// the same data as /api/hub
	attendu, _ := json.Marshal(moduleDev{}.Resume(s))
	dev := slices.IndexFunc(a.hub.Modules, func(m ModuleVue) bool { return m.ID == "dev" })
	if len(a.hub.Modules) != len(modulesHub()) || a.hub.Modules[0].ID != "hub" || dev < 0 {
		t.Fatalf("modules %+v", a.hub.Modules)
	}
	if lu, _ := json.Marshal(a.hub.Modules[dev].Resume); string(lu) != string(attendu) {
		t.Fatalf("tile %s, /api/hub %s", lu, attendu)
	}
	for _, taille := range [][2]int{{80, 24}, {120, 40}} {
		e := nouvelEcran(taille[0], taille[1])
		a.dessiner(e)
		ecran := e.texteEcran()
		for _, x := range []string{"Athanor", "Studio", "Inactif", "Projets en cours", "Besoin de vous : 0", "1 Accueil", "2 À distance", "Appairer un appareil"} {
			if !strings.Contains(ecran, x) {
				t.Fatalf("%v: no %q in\n%s", taille, x, ecran)
			}
		}
	}
	// H, 1 and Escape lead home from Remote; the page's letters never take them
	for _, k := range []string{"H", "1", "\x1b"} {
		a.page = pDistance
		if a.lettrePage(k) != "" {
			t.Fatalf("Remote takes %q", k)
		}
		taper(a, k)
		if a.page != pAccueil {
			t.Fatalf("%q from Remote: %d", k, a.page)
		}
	}
	// Enter opens nothing (reading only); the help shows the two pages' keys
	taper(a, "\r")
	if a.page != pAccueil || a.dialogue != nil {
		t.Fatalf("Enter: %d", a.page)
	}
	a.aide = true
	e := nouvelEcran(120, 40)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "1, H, Esc") || !strings.Contains(e.texteEcran(), "2, Tab") {
		t.Fatalf("the help:\n%s", e.texteEcran())
	}
	// "In progress": a working element says its milestone and what its pilot does, as the web does
	a.aide, a.page = false, pAccueil
	a.hub.Elements = []ElementHub{{Module: "dev", ID: "b", Titre: "b", Etat: "actif", Avancement: &Avancement{Fait: 3, Total: 10, Etape: "Milestone {0}/{1}"}, Activite: "Écrire la page", Action: "Tests"},
		{Module: "dev", ID: "c", Titre: "c", Etat: "actif", Activite: "Seul"}}
	e = nouvelEcran(120, 40)
	a.dessiner(e)
	for _, x := range []string{"b  Studio  Jalon 4/10 : Écrire la page, Tests", "c  Studio  Seul"} {
		if !strings.Contains(e.texteEcran(), x) {
			t.Fatalf("no %q in\n%s", x, e.texteEcran())
		}
	}
}

func TestTUIEtroitEtCalme(t *testing.T) {
	a := nouvelleAppTUI(&clientTUI{projet: "a"}, false)
	a.vue = &EtatVue{Version: versionAthanor, KitValide: true, KitInstalle: true, Langue: "fr", Projets: []ProjetVue{{Nom: "a", Courant: true}}, Projet: &ProjetVue{Nom: "a"}}
	a.langue, a.d = "fr", dicoTUI("fr")
	// 80 columns: both tabs by their name, clickable
	e := nouvelEcran(80, 24)
	a.dessiner(e)
	onglets := e.ligneTexte(1)
	for _, k := range []string{" 1 Accueil ", " 2 À distance "} {
		if !strings.Contains(onglets, k) {
			t.Fatalf("tab %q missing: %q", k, onglets)
		}
	}
	x := strings.Index(onglets, " 2 ")
	a.souris(evenementTUI{genre: evSouris, x: len([]rune(onglets[:x])) + 1, y: 1, appui: true})
	if a.page != pDistance {
		t.Fatalf("click on tab 2: page %d", a.page)
	}
	// a still screen (no spinner), below the header (its clock may pass a minute during the wait)
	a.page = pAccueil
	a.hub = &reponseHubTUI{}
	a.dessiner(e)
	sousEntete := func() string { _, r, _ := strings.Cut(e.texteEcran(), "\n"); return r }
	avant := sousEntete()
	time.Sleep(250 * time.Millisecond)
	a.dessiner(e)
	if sousEntete() != avant || avant == "" || strings.ContainsAny(avant, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the home moves:\n%s", avant)
	}
}

// M313a: an alert's argument that is itself a message (the failed test e-mail) is translated, never dumped as JSON.
func TestTraduireTUIMessageImbrique(t *testing.T) {
	d := map[string]string{"The test e-mail of {0} failed: {1}": "L'e-mail de test de {0} a échoué : {1}", "No answer": "Pas de réponse"}
	if g := traduireTUI(d, "The test e-mail of {0} failed: {1}", "Pro", &MessageMoteur{Cle: "No answer"}); g != "L'e-mail de test de Pro a échoué : Pas de réponse" {
		t.Fatal(g)
	}
}

// q in a terminal that shows an Athanor in the background: leave it running (the default), or stop it
// completely (refused to an Athanor that has its window, which stops when its window closes).
func TestTUIQuitterFond(t *testing.T) {
	_, c, _ := essaiServeurTUI(t)
	a := nouvelleAppTUI(c, false)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil })
	taper(a, "q")
	if d := a.dialogue; d == nil || len(d.choix) != 3 || d.choix[0].id != "fond" || d.choix[1].id != "arret" {
		t.Fatalf("the question: %+v", a.dialogue)
	}
	taper(a, "\r")
	if !a.quitter {
		t.Fatal("Enter: not left running")
	}
	a.quitter = false
	taper(a, "q\x1b[B\r")
	attendreTUI(t, a, func() bool { return a.toastErr })
	if a.quitter {
		t.Fatal("an Athanor with its window stopped from the terminal")
	}
	athanorFond.Store(true)
	t.Cleanup(func() { athanorFond.Store(false) })
	taper(a, "q\x1b[B\r")
	attendreTUI(t, a, func() bool { return a.quitter })
	select {
	case <-arretFond:
	case <-time.After(3 * time.Second):
		t.Fatal("the Athanor in the background was not asked to stop")
	}
}
