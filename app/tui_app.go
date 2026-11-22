package main

// The terminal interface (athanor tui): what a computer without a screen needs (a server through SSH): the
// state of every module and what waits (Home), pairing a phone (Remote: the QR code, the 6 digits, the trusted
// devices), the emergency stop. Everything else is the window's, or a paired phone's (3.1.4: the Studio's pages
// left the terminal). Its state and its loop; the pages are drawn by tui_pages.go, the keys by tui_actions.go.

import (
	"strings"
	"time"
)

const (
	pAccueil  = iota // the hub's home (keys 1 and H), where the terminal opens
	pDistance        // Remote (key 2): pairing a phone
	nbPages
)

// clesPages: the pages' names (keys of the app's translations).
var clesPages = [nbPages]string{"Home", "Remote"}

type paletteTUI struct {
	fond, panneau, ligne, bord, encre, muet, accent, surAccent, ok, attention, erreur, violet couleurT
}

func hexCouleur(h string) couleurT {
	h = strings.TrimPrefix(h, "#")
	var v int32
	for _, c := range h {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= c - '0'
		case c >= 'a' && c <= 'f':
			v |= c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v |= c - 'A' + 10
		}
	}
	return couleurT(v)
}

// melange: a colour between a and b (f: 0 = a, 1 = b).
func melange(a, b couleurT, f float64) couleurT {
	c := func(x couleurT, d uint) float64 { return float64((x >> d) & 0xff) }
	m := func(d uint) couleurT { return couleurT(int32(c(a, d)+(c(b, d)-c(a, d))*f+0.5)) << d }
	return m(16) | m(8) | m(0)
}

// paletteDe: the app's palette and accent (static/i18n.js THEMES and ACCENTS, app.css).
func paletteDe(theme, accent string) paletteTUI {
	themes := map[string][5]string{
		"ardoise": {"#141b24", "#1b2430", "#2d3a49", "#e9eef3", "#98a6b4"},
		"minuit":  {"#0b0d11", "#12151b", "#242933", "#eceff4", "#8f96a4"},
		"ocean":   {"#0e1a2b", "#132238", "#27405f", "#e6eef8", "#90a7c3"},
		"clair":   {"#f3f5f8", "#ffffff", "#d9dfe7", "#1b2430", "#525e6c"},
		"sable":   {"#f6f1e8", "#fffdf8", "#e0d6c5", "#2b2620", "#5f574d"},
	}
	t, ok := themes[theme]
	if !ok {
		theme, t = "ardoise", themes["ardoise"]
	}
	clair := theme == "clair" || theme == "sable"
	accents := map[string]string{"sarcelle": "#3fc6a5", "bleu": "#5b9dff", "violet": "#a98bff", "ambre": "#f2b441", "rose": "#ef7aa6"}
	if clair {
		accents = map[string]string{"sarcelle": "#137a62", "bleu": "#2c69cb", "violet": "#7a4fd6", "ambre": "#96600c", "rose": "#b83f6d"}
	}
	a, ok := accents[accent]
	if !ok {
		a = accents["sarcelle"]
	}
	p := paletteTUI{fond: hexCouleur(t[0]), panneau: hexCouleur(t[1]), ligne: hexCouleur(t[2]), encre: hexCouleur(t[3]), muet: hexCouleur(t[4]), accent: hexCouleur(a)}
	switch theme {
	case "clair":
		p.ok, p.attention, p.erreur, p.violet = hexCouleur("#137a62"), hexCouleur("#946219"), hexCouleur("#c53d35"), hexCouleur("#7a4fd6")
	case "sable":
		p.ok, p.attention, p.erreur, p.violet = hexCouleur("#26754f"), hexCouleur("#955a0f"), hexCouleur("#bf3b2e"), hexCouleur("#7048c0")
	default:
		p.ok, p.attention, p.erreur, p.violet = hexCouleur("#3fc6a5"), hexCouleur("#f2b441"), hexCouleur("#e5584f"), hexCouleur("#a98bff")
	}
	p.bord = melange(p.ligne, p.muet, 0.45) // the boxes' edges: seen, never louder than the text
	p.surAccent = p.fond
	if clair {
		p.surAccent = hexCouleur("#ffffff")
	}
	return p
}

type choixTUI struct {
	id, texte string
	danger    bool
}

// dialogueTUI: a question and its answers (Escape: "").
type dialogueTUI struct {
	titre, texte string
	choix        []choixTUI
	sel          int
	fn           func(id string)
}

type champTUI struct {
	lib string
	s   *saisieT
	h   int // lines (1: one line)
}

// formulaireTUI: fields to fill in (a request, a note, a new project, the password).
type formulaireTUI struct {
	titre  string
	aide   string
	champs []champTUI
	sel    int
	fn     func(valeurs []string)
	bouton string   // the label of the confirm button ("Save" when empty)
	erreur string   // what the server answered: the form stays open with it
	debut  []string // the fields as the form opened: closing it unchanged asks nothing
}

type zoneTUI struct {
	x, y, l, h int
	fn         func()
}

type appTUI struct {
	c         *clientTUI
	d         map[string]string
	langue    string
	pal       paletteTUI
	l, h      int
	vue       *EtatVue
	horsLigne string // the server does not answer: why
	echecs    int
	page      int
	majs      chan func(*appTUI)
	enCours   map[string]bool
	derniere  map[string]time.Time
	gen       int // the project's generation: answers for a project left are dropped
	quitter   bool
	proprio   bool // this process serves the application (quitting stops it)

	dialogue *dialogueTUI
	form     *formulaireTUI
	aide     bool
	toast    string
	toastErr bool
	toastFin time.Time

	zones         []zoneTUI
	zonesHaut     int // zones of an overlay start here (the page's below are not clickable then)
	demarrage     time.Time
	verrouDemande bool

	formValide *formulaireTUI // the form whose fields are being saved
	// Remote (tui_distance.go)
	dist           *DistanceVue
	hub            *reponseHubTUI // the home's tiles (tui_accueil.go)
	selApp         int
	chiffresJusque time.Time // the 6 digits shown until then
}

func nouvelleAppTUI(c *clientTUI, proprio bool) *appTUI {
	a := &appTUI{c: c, majs: make(chan func(*appTUI), 64), enCours: map[string]bool{}, derniere: map[string]time.Time{}, proprio: proprio,
		l: 80, h: 24, page: pAccueil}
	a.pal = paletteDe("", "")
	return a
}

func (a *appTUI) t(cle string, args ...any) string { return traduireTUI(a.d, cle, args...) }

// annoncer: a short message at the bottom of the screen (an error in red).
func (a *appTUI) annoncer(texte string, erreur bool) {
	a.toast, a.toastErr, a.toastFin = texte, erreur, time.Now().Add(5*time.Second)
	if erreur {
		a.toastFin = time.Now().Add(8 * time.Second)
	}
}

// texteErreur: an error of the API in the user's language.
func (a *appTUI) texteErreur(err error) string {
	if e, ok := err.(*erreurAPI); ok {
		args := make([]any, len(e.Args))
		for i, x := range e.Args {
			args[i] = x
		}
		return a.t(e.Cle, args...)
	}
	return err.Error()
}

// charger: a read of the API in the background, at most every `tous`; fn gets its answer in the loop.
func (a *appTUI) charger(cle string, tous time.Duration, lire func(c clientTUI) (func(*appTUI), error)) {
	if a.enCours[cle] || time.Since(a.derniere[cle]) < tous {
		return
	}
	a.enCours[cle] = true
	a.derniere[cle] = time.Now()
	c, gen := *a.c, a.gen
	go func() {
		f, err := lire(c)
		a.majs <- func(a *appTUI) {
			a.enCours[cle] = false
			if gen != a.gen {
				return
			}
			if err != nil {
				if e, ok := err.(*erreurAPI); ok && e.Verrou {
					a.demanderMotDePasse()
					return
				}
				if cle == "etat" {
					a.echecs++
					if a.echecs >= 3 {
						a.horsLigne = err.Error()
					}
				}
				return
			}
			if f != nil {
				f(a)
			}
		}
	}()
}

// agir: a change through the API, its message announced; then (ok) runs after it.
func (a *appTUI) agir(methode, chemin string, corps any, ensuite func(rep map[string]any)) {
	a.agirAvec(methode, chemin, corps, ensuite, func(err error) { a.annoncer(a.texteErreur(err), true) })
}

// agirFormulaire: the change of the form just saved; an error shows in that form, opened again with what
// was typed (a wrong folder is fixed there).
func (a *appTUI) agirFormulaire(methode, chemin string, corps any, ensuite func(rep map[string]any)) {
	f := a.formValide
	a.agirAvec(methode, chemin, corps, ensuite, func(err error) {
		if f == nil {
			a.annoncer(a.texteErreur(err), true)
			return
		}
		f.erreur = a.texteErreur(err)
		a.form = f
	})
}

func (a *appTUI) agirAvec(methode, chemin string, corps any, ensuite func(rep map[string]any), echec func(error)) {
	c := *a.c
	go func() {
		var rep map[string]any
		err := c.appel(methode, chemin, corps, &rep)
		a.majs <- func(a *appTUI) {
			if err != nil {
				echec(err)
				return
			}
			if cle, _ := rep["message_key"].(string); cle != "" {
				var args []any
				if l, ok := rep["args"].([]any); ok {
					for _, x := range l {
						args = append(args, x)
					}
				}
				texte := a.t(cle, args...)
				if d, _ := rep["detail"].(string); d != "" { // what git or ssh said (GitHub's test)
					texte += " (" + d + ")"
				}
				echoue, _ := rep["ok"].(bool)
				_, aOk := rep["ok"]
				a.annoncer(texte, aOk && !echoue)
			}
			a.derniere["etat"] = time.Time{}
			if ensuite != nil {
				ensuite(rep)
			}
		}
	}()
}

// tic: what to read now (the state every second, the page's data at its own pace).
func (a *appTUI) tic() {
	a.charger("etat", time.Second, func(c clientTUI) (func(*appTUI), error) {
		var v EtatVue
		if err := c.lire("etat", &v); err != nil {
			return nil, err
		}
		return func(a *appTUI) { a.recevoirEtat(&v) }, nil
	})
	if a.vue == nil || a.vue.Verrouille {
		return
	}
	switch a.page {
	case pDistance:
		a.chargerDistance()
	case pAccueil:
		a.chargerHub()
	}
}

// recevoirEtat: the state read; the first one chooses the project (the app's current one).
func (a *appTUI) recevoirEtat(v *EtatVue) {
	a.echecs, a.horsLigne = 0, ""
	if v.Langue != a.langue || a.d == nil && v.Langue != "en" {
		a.langue = v.Langue
		a.d = dicoTUI(v.Langue)
	}
	a.pal = paletteDe(v.Theme, v.Accent)
	a.vue = v
	// the pilot seen running: its start is done, a later Start (after a stop) is not held back
	if v.Actif {
		a.demarrage = time.Time{}
	}
	if v.Verrouille {
		a.demanderMotDePasse()
		return
	}
	if a.c.projet == "" && v.Projet != nil {
		a.c.projet = v.Projet.Nom
		a.gen++
	}
	// the project shown no longer exists (removed, archived): the app's current one
	if a.c.projet != "" && v.Projet == nil {
		a.changerProjet("")
	}
}

// changerProjet: another project shown (its log, requests and chat read again).
func (a *appTUI) changerProjet(nom string) {
	if nom == a.c.projet {
		return
	}
	a.c.projet = nom
	a.gen++
	for k := range a.derniere {
		delete(a.derniere, k)
	}
	for k := range a.enCours {
		delete(a.enCours, k)
	}
}

// projetsVisibles: the projects in use (not archived).
func (a *appTUI) projetsVisibles() []ProjetVue {
	if a.vue == nil {
		return nil
	}
	var l []ProjetVue
	for _, p := range a.vue.Projets {
		if !p.Archive {
			l = append(l, p)
		}
	}
	return l
}

// zone: a clickable rectangle of the frame being drawn.
func (a *appTUI) zone(x, y, l, h int, fn func()) {
	a.zones = append(a.zones, zoneTUI{x: x, y: y, l: l, h: h, fn: fn})
}

// souris: a click or the wheel on the frame shown.
func (a *appTUI) souris(ev evenementTUI) {
	debut := 0
	if a.dialogue != nil || a.form != nil || a.aide {
		debut = a.zonesHaut
	}
	for i := len(a.zones) - 1; i >= debut; i-- {
		z := a.zones[i]
		if ev.x < z.x || ev.x >= z.x+z.l || ev.y < z.y || ev.y >= z.y+z.h {
			continue
		}
		if ev.nom == "molette" {
			continue
		}
		if z.fn != nil && ev.appui && ev.bouton == 0 {
			z.fn()
			return
		}
	}
	if (a.dialogue != nil || a.aide) && ev.appui && ev.bouton == 0 && ev.nom != "molette" {
		// a click beside a dialog: nothing (it stays until answered)
		return
	}
}

// boucle: the events and the server's answers until quitter; dessiner draws each frame.
func (a *appTUI) boucle(evs <-chan evenementTUI, taille <-chan [2]int, dessiner func()) {
	tic := time.NewTicker(200 * time.Millisecond)
	defer tic.Stop()
	a.tic()
	dessiner()
	for !a.quitter {
		select {
		case ev, ok := <-evs:
			if !ok {
				return
			}
			if ev.genre == evSouris {
				a.souris(ev)
			} else {
				a.touche(ev)
			}
		case f := <-a.majs:
			f(a)
		case t := <-taille:
			a.l, a.h = t[0], t[1]
		case <-tic.C:
			a.tic()
		}
		// what else is ready, then one frame
		for vide := false; !vide; {
			select {
			case f := <-a.majs:
				f(a)
			default:
				vide = true
			}
		}
		if a.toast != "" && time.Now().After(a.toastFin) {
			a.toast = ""
		}
		dessiner()
	}
}
