package main

// The terminal interface: what each frame shows (the bars, the projects, the pages, the dialogs).

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func (a *appTUI) st(av, ar couleurT) styleT { return styleT{av: av, ar: ar} }

func (a *appTUI) gras(av, ar couleurT) styleT { return styleT{av: av, ar: ar, gras: true} }

// boite: a panel with its border and title; its inside (x, y, l, h).
func (a *appTUI) boite(e *ecranT, x, y, l, h int, titre string, actif bool) (int, int, int, int) {
	p := a.pal
	e.rect(x, y, l, h, a.st(p.encre, p.panneau))
	bord := p.bord
	if actif {
		bord = p.accent
	}
	e.cadre(x, y, l, h, titre, a.st(bord, p.panneau), a.gras(p.encre, p.panneau))
	return x + 2, y + 1, l - 4, h - 2
}

// dessiner: the whole frame.
func (a *appTUI) dessiner(e *ecranT) {
	p := a.pal
	a.l, a.h = e.l, e.h
	e.effacer(a.st(p.encre, p.fond))
	a.zones = a.zones[:0]
	if a.vue == nil {
		msg := a.t("Updating…")
		if a.horsLigne != "" {
			msg = a.t("Athanor does not answer: {0}", a.horsLigne)
		}
		e.texte(max(0, (e.l-largeurTexte(msg))/2), e.h/2, 0, msg, a.st(p.muet, p.fond))
		a.zonesHaut = len(a.zones)
		a.dessinerCouches(e)
		return
	}
	if a.vue.Verrouille { // only the password form and its refusal, on a plain background
		a.dessinerToast(e)
		a.zonesHaut = len(a.zones)
		a.dessinerCouches(e)
		return
	}
	a.barreHaut(e)
	a.onglets(e, 1)
	y, h, l := 2, e.h-3, e.l
	if h >= 3 && l >= 20 {
		switch a.page {
		case pDistance:
			a.pageDistance(e, 0, y, l, h)
		default:
			a.pageAccueil(e, 0, y, l, h)
		}
	}
	a.barreBas(e)
	a.dessinerToast(e)
	a.zonesHaut = len(a.zones)
	a.dessinerCouches(e)
}

// dessinerToast: the last announcement, bottom right.
func (a *appTUI) dessinerToast(e *ecranT) {
	p := a.pal
	if a.toast != "" {
		coul := p.accent
		if a.toastErr {
			coul = p.erreur
		}
		lignes := envelopper(a.toast, min(70, e.l-6))
		if len(lignes) > 4 {
			lignes = lignes[:4]
		}
		lt := 0
		for _, li := range lignes {
			lt = max(lt, largeurTexte(li))
		}
		tx, ty := e.l-lt-4, e.h-2-len(lignes)
		e.rect(tx, ty, lt+3, len(lignes), a.st(p.encre, p.panneau))
		for i, li := range lignes {
			e.mettre(tx, ty+i, '▌', a.st(coul, p.panneau))
			e.texte(tx+2, ty+i, lt, li, a.st(p.encre, p.panneau))
		}
	}
}

// couleurPhase: the colour of a project's state (the app's dot).
func (a *appTUI) couleurPhase(pv ProjetVue) couleurT {
	p := a.pal
	if !pv.Actif {
		return p.muet
	}
	switch pv.Phase {
	case "quota", "budget", "attente_vous":
		return p.attention
	case "erreur", "connexion":
		return p.erreur
	}
	return p.ok
}

func (a *appTUI) barreHaut(e *ecranT) {
	p := a.pal
	v := a.vue
	e.rect(0, 0, e.l, 1, a.st(p.encre, p.panneau))
	x := 0
	x += e.texte(x, 0, 0, " ◆ Athanor Hub ", a.gras(p.surAccent, p.accent))
	x += e.texte(x, 0, 0, " v"+v.Version+" ", a.st(p.muet, p.panneau))
	droite := ""
	if v.PourVous > 0 {
		droite = "🙋 " + strconv.Itoa(v.PourVous) + "  "
	}
	if v.Distant || v.Connectes > 0 {
		droite += "📱 " + strconv.Itoa(v.Connectes) + "  "
	}
	droite += time.Now().Format(a.formatHeure()) + " "
	ld := largeurTexte(droite)
	if v.Projet != nil {
		titre, _, coul := a.phrase()
		x += e.texte(x, 0, 0, "│ ", a.st(p.ligne, p.panneau))
		x += e.texte(x, 0, 0, "● ", a.st(coul, p.panneau))
		x += e.texte(x, 0, max(1, e.l-x-ld-2), v.Projet.Nom, a.gras(p.encre, p.panneau))
		if e.l-x-ld-4 > 8 {
			x += e.texte(x, 0, 0, "  ", a.st(p.encre, p.panneau))
			e.texte(x, 0, e.l-x-ld-2, titre, a.st(coul, p.panneau))
		}
	}
	if a.horsLigne != "" {
		msg := " ⚠ " + a.t("Athanor does not answer: {0}", a.horsLigne) + " "
		e.texte(max(0, e.l-ld-largeurTexte(msg)-1), 0, 0, msg, a.gras(p.surAccent, p.erreur))
	}
	e.texte(e.l-ld, 0, 0, droite, a.st(p.muet, p.panneau))
}

func (a *appTUI) formatHeure() string {
	if a.langue == "en" || a.langue == "" {
		return "3:04 PM"
	}
	return "15:04"
}

// onglets: the two pages in one row, and what is not here any more.
func (a *appTUI) onglets(e *ecranT, y int) {
	p := a.pal
	e.rect(0, y, e.l, 1, a.st(p.encre, p.fond))
	x := 1
	for i := range nbPages {
		lib := " " + numeroPage(i) + " " + a.t(clesPages[i]) + " "
		st := a.st(p.muet, p.fond)
		if i == a.page {
			st = a.gras(p.surAccent, p.accent)
		}
		n := e.texte(x, y, 0, lib, st)
		page := i
		a.zone(x, y, n, 1, func() { a.allerPage(page) })
		x += n + 1
	}
	// the rest of Athanor: the window, or a paired phone
	if reste := e.l - x - 2; reste > 20 {
		note := a.t("Projects, the Studio, servers and the vault: in the window (w) or on a paired phone (2).")
		e.texte(max(x+1, e.l-1-largeurTexte(note)), y, reste, note, a.st(p.muet, p.fond))
	}
}

// numeroPage: the key of a page.
func numeroPage(i int) string { return strconv.Itoa(i + 1) }

// raccourci: one key and what it does, for the bottom bar.
type raccourci struct{ k, lib string }

func (a *appTUI) raccourcisPage() []raccourci {
	if a.page == pDistance {
		return a.raccourcisDistance()
	}
	return []raccourci{{"2", a.t("Pair a device")}}
}

func (a *appTUI) barreBas(e *ecranT) {
	p := a.pal
	y := e.h - 1
	e.rect(0, y, e.l, 1, a.st(p.encre, p.panneau))
	t := a.t
	glob := []raccourci{{"w", t("Open Athanor's window")}, {"?", t("Help")}, {"q", t("Quit")}, {"!", t("Emergency stop")}}
	x := 1
	ecrire := func(r raccourci, fin bool) bool {
		lt := largeurTexte(r.k) + 1 + largeurTexte(r.lib) + 2
		if x+lt > e.l && !fin {
			return false
		}
		x += e.texte(x, y, 0, r.k, a.gras(p.accent, p.panneau))
		x += e.texte(x, y, 0, " "+r.lib+"  ", a.st(p.muet, p.panneau))
		return true
	}
	// the page's keys first, then the global ones, as many as fit
	tous := append(a.raccourcisPage(), glob...)
	for _, r := range tous {
		if !ecrire(r, false) {
			break
		}
	}
}

// ----------------------------------------------------------------------------- the state's sentence

// titreJalonTUI: a milestone's title from the plan (the pilot gives its whole line).
func titreJalonTUI(texte string, plan []Jalon) string {
	if texte == "" {
		return ""
	}
	texte = strings.TrimSpace(texte)
	for _, pre := range []string{"- [ ] ", "- [x] ", "- [X] "} {
		texte = strings.TrimPrefix(texte, pre)
	}
	for _, j := range plan {
		if j.Complet != "" && (j.Complet == texte || strings.HasPrefix(texte, j.Complet) || strings.HasPrefix(j.Complet, texte)) {
			return j.Titre
		}
	}
	coupe := texte
	for _, sep := range []string{" — ", " – "} {
		if i := strings.Index(coupe, sep); i >= 0 {
			coupe = coupe[:i]
		}
	}
	return couperTexte(coupe, 90)
}

// lireDate: the dates the pilot writes ("2026-10-25 18:04:02", ISO with or without zone).
func lireDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", strings.Replace(s, " ", "T", 1), time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func (a *appTUI) depuis(iso string) string {
	d, ok := lireDate(iso)
	if !ok {
		return ""
	}
	s := math.Max(0, time.Since(d).Seconds())
	if s < 3600 {
		return a.t("{0} min", int(s/60))
	}
	return a.t("{0} h {1} min", int(s/3600), fmt.Sprintf("%02d", int(math.Mod(s, 3600)/60)))
}

func fmtTokensTUI(n int64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.1f M", float64(n)/1e6)
	case n >= 1000:
		return strconv.FormatInt((n+500)/1000, 10) + " k"
	}
	return strconv.FormatInt(n, 10)
}

// phrase: the state in a sentence, as the app's banner says it: title, details, colour.
func (a *appTUI) phrase() (string, string, couleurT) {
	v, p, t := a.vue, a.pal, a.t
	e := Etat{}
	if v.Etat != nil {
		e = *v.Etat
	}
	arrete := func() (string, string, couleurT) {
		if e.DernierCommit != "" {
			return t("Pilot stopped"), t("Last saved work: {0}", e.DernierCommit), p.muet
		}
		return t("Ready to start"), t("Click Start: Claude begins with your request."), p.muet
	}
	if !v.KitValide {
		return t("Tool folder not found"), t("Set it in Options."), p.erreur
	}
	if v.Projet == nil {
		if v.KitInstalle {
			return t("No project yet"), t("Add a project: its development folder, and what you want."), p.muet
		}
		return t("Ready to install"), "", p.muet
	}
	if !v.Actif {
		if !v.KitInstalle {
			return t("Ready to install"), "", p.muet
		}
		if v.DemandeVide {
			return t("Waiting for your first request"), t("Describe what you want in Requests, then start."), p.muet
		}
		return arrete()
	}
	switch e.Phase {
	case "arrete":
		return arrete()
	case "travail":
		titre := t("Claude is working")
		if o := strings.Fields(e.Ouvriers); len(o) > 0 {
			titre = t("{0} workers in parallel: {1}", len(o), strings.Join(o, ", "))
		} else if strings.HasPrefix(e.Etat, "Merging") {
			titre = t("The lead merges the workers' branches")
		} else if e.JalonEnCours != "" {
			titre = t("Claude is working on {0}", titreJalonTUI(e.JalonEnCours, v.Plan))
		}
		var sous []string
		if d := a.depuis(e.DebutCycle); d != "" {
			sous = append(sous, t("for {0}", d))
		}
		sous = append(sous, t("cycle {0}", e.Cycle))
		if e.Modele != "" {
			sous = append(sous, e.Modele)
		}
		if v.Eco {
			sous = append(sous, t("eco mode"))
		}
		if v.Rapide {
			sous = append(sous, t("fast mode"))
		}
		if v.Automatisation {
			sous = append(sous, t("automation"))
		}
		if v.Contexte > 0 {
			sous = append(sous, t("context {0} tokens", fmtTokensTUI(v.Contexte)))
		}
		return titre, strings.Join(sous, " · "), p.ok
	case "quota":
		return t("Usage limit reached, Claude is waiting"), t("Resumes {0} · cycle {1}", firstNonEmpty(e.ReprisePrevue, t("as soon as possible")), e.Cycle), p.attention
	case "budget":
		return t("API budget or credit used up, Claude is waiting"), t("Raise the budget, count again (API spend, below) or put another API key in Settings: the pilot resumes at once."), p.attention
	case "attente_vous":
		return t("Claude is waiting for you"), tuiPluriel(t, v.PourVous, "1 task on the For you page: the pilot resumes as soon as you answer it.", "{0} tasks on the For you page: the pilot resumes as soon as you answer one."), p.attention
	case "entre_cycles":
		return t("Between two cycles"), t("Cycle {0} is done, the next one starts with a fresh context.", e.Cycle), p.ok
	case "erreur":
		return t("Error, retry scheduled"), e.Etat, p.erreur
	case "connexion":
		return t("Connection problem"), t("Check the token or the API key in Settings, or go through Setup & health: a new one is used at once."), p.erreur
	case "termine":
		return t("Project finished"), t("The deliverable and the final report are in the project folder. To go on, add a request or a note, then Start."), p.ok
	}
	if e.Etat != "" {
		return t(e.Etat), t("cycle {0}", e.Cycle), p.ok
	}
	return t("Pilot started"), t("cycle {0}", e.Cycle), p.ok
}

// ----------------------------------------------------------------------------- Overview

// ascenseur: where the list shown is in the whole (on the box's right edge).
func (a *appTUI) ascenseur(e *ecranT, x, y, h, pos, max int) {
	if h < 2 || max <= 0 {
		return
	}
	k := y + int(math.Round(float64(pos)/float64(max)*float64(h-1)))
	e.mettre(x, k, '┃', a.st(a.pal.accent, a.pal.panneau))
}

// ----------------------------------------------------------------------------- Log

// ----------------------------------------------------------------------------- Requests

// ----------------------------------------------------------------------------- Chat

// ----------------------------------------------------------------------------- Setup & health

// ----------------------------------------------------------------------------- dialogs, forms, help

func (a *appTUI) dessinerCouches(e *ecranT) {
	switch {
	case a.form != nil:
		a.dessinerFormulaire(e)
		if a.dialogue != nil {
			a.zonesHaut = len(a.zones)
			a.dessinerDialogue(e)
		}
	case a.dialogue != nil:
		a.dessinerDialogue(e)
	case a.aide:
		a.dessinerAide(e)
	}
}

// voile: the frame behind a dialog, dimmed.
func (a *appTUI) voile(e *ecranT) {
	for i := range e.c {
		e.c[i].s.faible = true
		e.c[i].s.av = a.pal.muet
	}
}

func (a *appTUI) dessinerDialogue(e *ecranT) {
	p := a.pal
	d := a.dialogue
	a.voile(e)
	l := min(72, e.l-4)
	var lignes []string
	if d.texte != "" {
		lignes = envelopper(d.texte, l-4)
	}
	h := len(lignes) + len(d.choix) + 3
	if len(lignes) > 0 {
		h++
	}
	if h > e.h-2 {
		h = e.h - 2
		lignes = lignes[:max(0, h-len(d.choix)-4)]
	}
	x, y := (e.l-l)/2, (e.h-h)/2
	ix, iy, il, _ := a.boite(e, x, y, l, h, d.titre, true)
	for i, li := range lignes {
		e.texte(ix, iy+i, il, li, a.st(p.encre, p.panneau))
	}
	yc := iy
	if len(lignes) > 0 {
		yc += len(lignes) + 1
	}
	for i, c := range d.choix {
		st := a.st(p.encre, p.panneau)
		marque := "  "
		if c.danger {
			st = a.st(p.erreur, p.panneau)
		}
		if i == d.sel {
			marque = "▸ "
			if c.danger {
				st = a.gras(p.surAccent, p.erreur)
			} else {
				st = a.gras(p.surAccent, p.accent)
			}
			e.rect(ix, yc+i, il, 1, st)
		}
		e.texte(ix, yc+i, il, marque+c.texte, st)
		k := i
		a.zone(ix, yc+i, il, 1, func() {
			a.dialogue.sel = k
			a.choisirDialogue()
		})
	}
}

func (a *appTUI) dessinerFormulaire(e *ecranT) {
	p := a.pal
	f := a.form
	a.voile(e)
	l := min(96, e.l-2)
	h := 5 // the edges, the buttons and the line under them
	for _, c := range f.champs {
		h += c.h + 2
	}
	var erreur []string
	if f.erreur != "" {
		erreur = envelopper("✖ "+f.erreur, l-4)
		h += len(erreur)
	}
	aide := envelopper(f.aide, l-4)
	h += len(aide)
	if h > e.h {
		// the multi-line fields shrink to fit
		trop := h - e.h
		for i := range f.champs {
			if f.champs[i].h > 2 && trop > 0 {
				r := min(trop, f.champs[i].h-2)
				f.champs[i].h -= r
				trop -= r
			}
		}
		h = min(h, e.h)
	}
	x, y := (e.l-l)/2, max(0, (e.h-h)/2)
	ix, iy, il, _ := a.boite(e, x, y, l, h, f.titre, true)
	yy := iy
	for i, c := range f.champs {
		st := a.st(p.muet, p.panneau)
		if i == f.sel {
			st = a.gras(p.accent, p.panneau)
		}
		e.texte(ix, yy, il, c.lib, st)
		k := i
		a.zone(ix, yy, il, c.h+1, func() { a.form.sel = k })
		c.s.dessiner(e, ix, yy+1, il, c.h, a.st(p.encre, p.fond), i == f.sel, "", styleT{})
		yy += c.h + 2
	}
	for _, li := range erreur {
		e.texte(ix, yy, il, li, a.gras(p.erreur, p.panneau))
		yy++
	}
	// the buttons: Save, Cancel (Tab reaches them, a click presses them)
	bx := ix
	valider := a.t("Save")
	if f.bouton != "" {
		valider = f.bouton
	}
	for i, lib := range []string{valider, a.t("Cancel")} {
		texte := " " + lib + " "
		st := a.st(p.encre, p.ligne)
		if i == 0 {
			st = a.gras(p.surAccent, p.accent)
		}
		if f.sel == len(f.champs)+i {
			texte = "▸" + lib + "◂"
			st.souligne = true
			st.gras = true
		}
		n := e.texte(bx, yy, 0, texte, st)
		k := i
		a.zone(bx, yy, n, 1, func() {
			if k == 0 {
				a.validerFormulaire()
			} else {
				a.abandonnerFormulaire()
			}
		})
		bx += n + 2
	}
	yy += 2
	for i, li := range aide {
		e.texte(ix, yy+i, il, li, a.st(p.muet, p.panneau))
	}
}

func (a *appTUI) dessinerAide(e *ecranT) {
	p := a.pal
	t := a.t
	a.voile(e)
	lignes := [][2]string{
		{"1, H, Esc", t("Home")},
		{"2, Tab", t("Remote")},
		{"o · v", t("Remote") + ": " + t("Remote access") + " · " + t("Show the code")},
		{"↑ ↓ · r · d", t("Trusted devices") + ": " + t("Rename") + " · " + t("Remove")},
		{"m · c · t", t("Address") + ": " + t("Use my domain") + " · " + t("Cancel") + " · " + t("Back to a temporary address")},
		{"!", t("Emergency stop")},
		{"w", t("Open Athanor's window")},
		{"q, Ctrl+C", t("Quit")},
	}
	l := min(84, e.l-4)
	h := min(len(lignes)+5, e.h-2)
	x, y := (e.l-l)/2, (e.h-h)/2
	ix, iy, il, ih := a.boite(e, x, y, l, h, t("Keys"), true)
	for i, li := range lignes {
		if i >= ih-2 {
			break
		}
		e.texte(ix, iy+i, 18, li[0], a.gras(p.accent, p.panneau))
		e.texte(ix+18, iy+i, il-18, li[1], a.st(p.encre, p.panneau))
	}
	pied := t("Projects, the Studio, servers and the vault: in the window (w) or on a paired phone (2).")
	e.texte(ix, iy+ih-1, il, pied, a.st(p.muet, p.panneau))
}
