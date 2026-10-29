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
	a.barreHaut(e)
	y, h := 1, e.h-2
	x := 0
	if e.l >= 96 && e.h >= 14 {
		a.colonneGauche(e, 0, y, 26, h)
		x = 26
	} else {
		a.onglets(e, y)
		y, h = y+1, h-1
	}
	l := e.l - x
	if h >= 3 && l >= 20 {
		switch a.page {
		case pSuivi:
			a.pageSuivi(e, x, y, l, h)
		case pJournal:
			a.pageJournal(e, x, y, l, h)
		case pDemandes:
			a.pageDemandes(e, x, y, l, h)
		case pChat:
			a.pageChat(e, x, y, l, h)
		case pSante:
			a.pageSante(e, x, y, l, h)
		case pPourVous:
			a.pagePourVous(e, x, y, l, h)
		case pEquipe:
			a.pageEquipe(e, x, y, l, h)
		case pGitHub:
			a.pageGitHub(e, x, y, l, h)
		case pReglages:
			a.pageReglages(e, x, y, l, h)
		case pDistance:
			a.pageDistance(e, x, y, l, h)
		}
	}
	a.barreBas(e)
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
	a.zonesHaut = len(a.zones)
	a.dessinerCouches(e)
}

// couleurPhase: the colour of a project's state (the app's dot).
func (a *appTUI) couleurPhase(pv ProjetVue) couleurT {
	p := a.pal
	if !pv.Actif {
		return p.muet
	}
	switch pv.Phase {
	case "quota", "attente_vous":
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
	x += e.texte(x, 0, 0, " ◆ Athanor ", a.gras(p.surAccent, p.accent))
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

// colonneGauche: the projects, then the pages.
func (a *appTUI) colonneGauche(e *ecranT, x, y, l, h int) {
	p := a.pal
	projets := a.projetsVisibles()
	hp := min(len(projets)+3, h/2)
	if len(projets) == 0 {
		hp = 4
	}
	ix, iy, il, ih := a.boite(e, x, y, l, hp, a.t("Projects"), false)
	if len(projets) == 0 {
		e.texte(ix, iy, il, a.t("No project yet"), a.st(p.muet, p.panneau))
	}
	for i, pv := range projets {
		if i >= ih-1 && len(projets) > ih {
			e.texte(ix, iy+i, il, "… "+a.t("{0} more", len(projets)-i), a.st(p.muet, p.panneau))
			break
		}
		fond := p.panneau
		if pv.Nom == a.c.projet {
			fond = p.ligne
			e.rect(ix-1, iy+i, il+2, 1, a.st(p.encre, fond))
		}
		e.texte(ix, iy+i, 2, "● ", a.st(a.couleurPhase(pv), fond))
		droite := ""
		if pv.Attente > 0 {
			droite = "+" + strconv.Itoa(pv.Attente)
		}
		e.texte(ix+2, iy+i, il-2-largeurTexte(droite)-1, pv.Nom, a.st(p.encre, fond))
		if droite != "" {
			e.texte(ix+il-largeurTexte(droite), iy+i, 0, droite, a.st(p.attention, fond))
		}
		nom := pv.Nom
		a.zone(ix-1, iy+i, il+2, 1, func() { a.changerProjet(nom) })
	}
	e.texte(ix, iy+ih-1, il, "N "+a.t("New project"), a.st(p.muet, p.panneau))
	a.zone(ix, iy+ih-1, il, 1, func() { a.nouveauProjet() })
	// the pages
	yp := y + hp
	hpg := min(nbPages+2, h-hp)
	if hpg < 3 {
		return
	}
	ix, iy, il, _ = a.boite(e, x, yp, l, hpg, a.t("Pages"), false)
	for i := 0; i < nbPages && i < hpg-2; i++ {
		fond := p.panneau
		st := a.st(p.encre, fond)
		if i == a.page {
			fond = p.ligne
			e.rect(ix-1, iy+i, il+2, 1, a.st(p.encre, fond))
			e.mettre(ix-1, iy+i, '▌', a.st(p.accent, fond))
			st = a.gras(p.encre, fond)
		}
		e.texte(ix, iy+i, 2, numeroPage(i), a.st(p.accent, fond))
		lib := a.t(clesPages[i])
		if i == pDemandes && a.vue != nil && a.vue.Attente > 0 {
			lib += " (" + strconv.Itoa(a.vue.Attente) + ")"
		}
		e.texte(ix+2, iy+i, il-2, lib, st)
		page := i
		a.zone(ix-1, iy+i, il+2, 1, func() { a.allerPage(page) })
	}
	// below: Athanor's folder, and where the terminal's own messages go
	reste := y + h - (yp + hpg)
	if reste >= 2 && a.vue.Kit != "" {
		e.texte(x+1, y+h-1, l-2, a.vue.Kit, a.st(p.muet, p.fond))
	}
}

// onglets: the pages in one row (a narrow terminal).
func (a *appTUI) onglets(e *ecranT, y int) {
	p := a.pal
	e.rect(0, y, e.l, 1, a.st(p.encre, p.fond))
	x := 1
	for i := 0; i < nbPages; i++ {
		lib := " " + numeroPage(i) + " " + a.t(clesPages[i]) + " "
		st := a.st(p.muet, p.fond)
		if i == a.page {
			st = a.gras(p.surAccent, p.accent)
		}
		if x+largeurTexte(lib) > e.l {
			break
		}
		n := e.texte(x, y, 0, lib, st)
		page := i
		a.zone(x, y, n, 1, func() { a.allerPage(page) })
		x += n + 1
	}
}

// numeroPage: the key of a page (1…9, then 0 for the tenth).
func numeroPage(i int) string { return strconv.Itoa((i + 1) % 10) }

// raccourci: one key and what it does, for the bottom bar.
type raccourci struct{ k, lib string }

func (a *appTUI) raccourcisPage() []raccourci {
	t := a.t
	var r []raccourci
	switch a.page {
	case pJournal:
		r = []raccourci{{"o", t("Tools")}, {"r", t("Results")}, {"u", t("Sub-agents")}, {"/", t("filter…")}, {"f", t("Follow")}}
	case pDemandes:
		r = []raccourci{{"n", t("New request")}, {"e", t("Note along the way")}, {"d", t("Delete")}, {"alt+↑↓", t("Order")}}
	case pChat:
		if a.chatActif {
			return []raccourci{{"Enter", t("Send")}, {"alt+Enter", t("New line")}, {"Esc", t("Leave the field")}}
		}
		m := a.modeleChat
		if m == "" {
			m = t("account default")
		}
		r = []raccourci{{"i", t("Write")}, {"c", t("New conversation")}, {"m", t("Model") + ": " + m}}
	case pSante:
		r = []raccourci{{"v", t("Check everything")}, {"Enter", t("Check")}, {"r", t("Repair")}, {"I", t("Install everything")}, {"T", t("Token")}}
	case pSuivi:
		r = []raccourci{{"M", t("Manage")}, {"g", t("Get GitHub's work")}}
	case pDistance:
		r = a.raccourcisDistance()
	default:
		r = a.raccourcisPages2()
	}
	return r
}

func (a *appTUI) barreBas(e *ecranT) {
	p := a.pal
	y := e.h - 1
	e.rect(0, y, e.l, 1, a.st(p.encre, p.panneau))
	t := a.t
	glob := []raccourci{}
	if v := a.vue; v != nil && v.Projet != nil {
		if v.Actif {
			if v.ArretDemande != "" {
				glob = append(glob, raccourci{"a", t("Let it go on")})
			} else {
				glob = append(glob, raccourci{"a", t("Stop after cycle")})
			}
			glob = append(glob, raccourci{"x", t("Stop now")})
		} else {
			glob = append(glob, raccourci{"s", t("Start")})
		}
		glob = append(glob, raccourci{"p", t("Project")})
	}
	glob = append(glob, raccourci{"?", t("Help")}, raccourci{"q", t("Quit")})
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
		return t("Pilot stopped"), t("Click Start to resume."), p.muet
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
		if v.Contexte > 0 {
			sous = append(sous, t("context {0} tokens", fmtTokensTUI(v.Contexte)))
		}
		return titre, strings.Join(sous, " · "), p.ok
	case "quota":
		return t("Usage limit reached, Claude is waiting"), t("Resumes {0} · cycle {1}", firstNonEmpty(e.ReprisePrevue, t("as soon as possible")), e.Cycle), p.attention
	case "attente_vous":
		return t("Claude is waiting for you"), t("{0} task(s) on the For you page: the pilot resumes as soon as you answer one.", v.PourVous), p.attention
	case "entre_cycles":
		return t("Between two cycles"), t("Cycle {0} is done, the next one starts with a fresh context.", e.Cycle), p.ok
	case "erreur":
		return t("Error, retry scheduled"), e.Etat, p.erreur
	case "connexion":
		return t("Connection problem"), t("Check the token in Settings, or go through Setup & health."), p.erreur
	case "termine":
		return t("Project finished"), t("The deliverable and the final report are in the project folder. To go on, add a request or a note, then Start."), p.ok
	}
	if e.Etat != "" {
		return t(e.Etat), t("cycle {0}", e.Cycle), p.ok
	}
	return t("Pilot started"), t("cycle {0}", e.Cycle), p.ok
}

// ----------------------------------------------------------------------------- Overview

func (a *appTUI) pageSuivi(e *ecranT, x, y, l, h int) {
	large := l >= 100 && h >= 16
	lg := l
	if large {
		lg = l / 2
	}
	// Right now
	lignes := a.lignesMaintenant(lg - 4)
	hm := min(len(lignes)+2, h)
	if !large {
		hm = min(hm, h/2+1)
	}
	ix, iy, il, ih := a.boite(e, x, y, lg, hm, a.t("Right now"), false)
	for i, li := range lignes {
		if i >= ih {
			break
		}
		cx := ix
		for _, s := range li {
			cx += e.texte(cx, iy+i, il-(cx-ix), s.t, s.s)
		}
	}
	// the quota
	yq, hq := y+hm, 0
	nq := 0
	if a.vue.Quota != nil {
		nq = len(a.vue.Quota.Fenetres)
	}
	hq = 3 + max(1, nq*3)
	if large {
		hq = min(hq, h-hm)
	} else {
		hq = min(hq, max(0, (h-hm)/2))
	}
	if hq >= 4 {
		a.boiteQuota(e, x, yq, lg, hq)
	} else {
		hq = 0
	}
	// the progress (the cycles so far), under them when there is room
	if large && h-hm-hq >= 5 && len(a.vue.Stats) > 0 {
		a.boiteProgres(e, x, yq+hq, lg, h-hm-hq)
	}
	// the plan
	if large {
		a.boitePlan(e, x+lg, y, l-lg, h)
	} else if h-hm-hq >= 4 {
		a.boitePlan(e, x, yq+hq, l, h-hm-hq)
	}
}

type segTUI struct {
	t string
	s styleT
}

// lignesMaintenant: the "Right now" block's lines (each one of coloured pieces), wrapped to l columns.
func (a *appTUI) lignesMaintenant(l int) [][]segTUI {
	p, v, t := a.pal, a.vue, a.t
	fond := p.panneau
	var r [][]segTUI
	texte := func(s string, st styleT) {
		for _, li := range envelopper(s, l) {
			r = append(r, []segTUI{{li, st}})
		}
	}
	titre, sous, coul := a.phrase()
	r = append(r, []segTUI{{"● ", a.st(coul, fond)}, {couperTexte(titre, l-2), a.gras(p.encre, fond)}})
	if sous != "" {
		texte(sous, a.st(p.muet, fond))
	}
	if v.Projet == nil {
		r = append(r, nil)
		r = append(r, []segTUI{{"N ", a.gras(p.accent, fond)}, {t("New project"), a.st(p.encre, fond)}})
		return r
	}
	if v.ArretDemande != "" {
		lib := t("Stop requested: end of cycle")
		if v.ArretDemande == "relance" {
			lib = t("Restart requested: end of cycle")
		}
		r = append(r, []segTUI{{"⏳ " + lib, a.st(p.attention, fond)}})
	}
	if v.PourVous > 0 {
		texte("🙋 "+t("Claude needs you")+" · "+t("{0} task(s) on the For you page", v.PourVous), a.gras(p.attention, fond))
	}
	if v.Actif && v.PiloteAncien {
		if v.VersionPilote != "" {
			texte("ℹ "+t("Athanor {0} is installed and the app already uses it. This pilot keeps running on {1}, the version it started with, until it is started again: it then moves to {0} by itself (to do it now: Stop after cycle, then Start). Nothing to do, nothing is lost.", v.Version, v.VersionPilote), a.st(p.muet, fond))
		} else {
			texte("ℹ "+t("Athanor {0} is installed and the app already uses it. This pilot keeps running on the version it started with until it is started again: it then moves to {0} by itself (to do it now: Stop after cycle, then Start). Nothing to do, nothing is lost.", v.Version), a.st(p.muet, fond))
		}
	}
	r = append(r, nil)
	e := Etat{}
	if v.Etat != nil {
		e = *v.Etat
	}
	lw := 0
	for _, cle := range []string{"Milestone", "Local commit", "On GitHub", "Queued requests", "Updated"} {
		lw = max(lw, largeurTexte(t(cle)))
	}
	lw = min(lw+1, max(8, l/2))
	lib := func(cle string) string { return completerTexte(t(cle), lw) }
	champ := func(cle, val string, st styleT) {
		lignes := envelopper(firstNonEmpty(val, "—"), max(10, l-lw-1))
		for i, li := range lignes {
			if i > 2 {
				break
			}
			if i == 0 {
				r = append(r, []segTUI{{lib(cle) + " ", a.st(p.muet, fond)}, {li, st}})
			} else {
				r = append(r, []segTUI{{strings.Repeat(" ", lw+1), a.st(p.muet, fond)}, {li, st}})
			}
		}
	}
	jalon := titreJalonTUI(e.JalonEnCours, v.Plan)
	champ("Milestone", jalon, a.gras(p.encre, fond))
	if jalon != "" {
		for _, j := range v.Plan {
			if j.Titre == jalon && j.Detail != "" {
				champ("", j.Detail, a.st(p.muet, fond))
				break
			}
		}
	}
	champ("Local commit", e.DernierCommit, a.st(p.muet, fond))
	if g := v.GitHub; g != nil {
		switch {
		case !g.Lie:
			champ("On GitHub", t("not linked to GitHub"), a.st(p.muet, fond))
		default:
			val, st := t("nothing pushed yet"), a.st(p.muet, fond)
			d := g.Dernier
			echec := d != nil && d.Resultat != "ok" && d.Resultat != "attente" && (g.Envoye == nil || d.Quand > g.Envoye.Quand)
			if g.Envoye != nil {
				val = g.Envoye.Commit
				if q, ok := lireDate(g.Envoye.Quand); ok {
					val += " · " + q.Format(a.formatHeure())
				}
				if memeCommitTUI(g.Envoye.Commit, e.DernierCommit) {
					val += " ✓"
					st = a.st(p.ok, fond)
				} else if e.DernierCommit != "" && !echec {
					val += " · " + t("the local one is not pushed yet")
					st = a.st(p.attention, fond)
				}
			}
			if !g.Auto {
				val += " · " + t("automatic push off")
				st = a.st(p.attention, fond)
			}
			champ("On GitHub", val, st)
			if echec {
				args := make([]any, len(d.Args))
				for i, x := range d.Args {
					args[i] = x
				}
				champ("", t(d.Modele, args...), a.st(p.erreur, fond))
			}
		}
	}
	champ("Queued requests", strconv.Itoa(v.Attente), a.st(p.encre, fond))
	if d, ok := lireDate(e.Maj); ok {
		f := "15:04:05"
		if a.langue == "en" || a.langue == "" {
			f = "3:04:05 PM"
		}
		champ("Updated", d.Format(f), a.st(p.muet, fond))
	}
	return r
}

// memeCommitTUI: the same commit, from its short ids ("abc1234 subject"; two repositories may shorten
// them differently).
func memeCommitTUI(a, b string) bool {
	x, _, _ := strings.Cut(a, " ")
	y, _, _ := strings.Cut(b, " ")
	n := min(len(x), len(y))
	return n >= 7 && x[:n] == y[:n]
}

// jourSemaine: the weekday in the interface's language.
func (a *appTUI) jourSemaine(d time.Time) string {
	jours := map[string][7]string{
		"fr": {"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
		"es": {"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		"de": {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
		"it": {"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"},
		"pt": {"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
	}
	if j, ok := jours[a.langue]; ok {
		return j[d.Weekday()]
	}
	return d.Weekday().String()
}

// quandLisible: a time to come, as the app says it (today: the time; tomorrow; else the weekday).
func (a *appTUI) quandLisible(sec int64) string {
	d := time.Unix(sec, 0)
	heure := d.Format(a.formatHeure())
	auj := time.Now()
	jours := int(math.Round(time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local).Sub(time.Date(auj.Year(), auj.Month(), auj.Day(), 0, 0, 0, 0, time.Local)).Hours() / 24))
	switch jours {
	case 0:
		return heure
	case 1:
		return a.t("tomorrow {0}", heure)
	}
	return a.jourSemaine(d) + " " + heure
}

func (a *appTUI) dans(sec int64) string {
	m := int(math.Max(0, math.Round(float64(sec-time.Now().Unix())/60)))
	switch {
	case m < 60:
		return a.t("in {0} min", m)
	case m < 48*60:
		return a.t("in {0} h {1}", m/60, fmt.Sprintf("%02d", m%60))
	}
	return a.t("in {0} days", int(math.Round(float64(m)/1440)))
}

func (a *appTUI) nomFenetre(n string) string {
	switch n {
	case "five_hour":
		return a.t("Current session (5 h)")
	case "seven_day":
		return a.t("This week, all models")
	}
	if m, ok := strings.CutPrefix(n, "seven_day_"); ok {
		m = strings.ReplaceAll(m, "_", " ")
		if m != "" {
			m = strings.ToUpper(m[:1]) + m[1:]
		}
		return a.t("This week, {0}", m)
	}
	return strings.ReplaceAll(n, "_", " ")
}

func (a *appTUI) boiteQuota(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Subscription quota"), false)
	q := a.vue.Quota
	if q == nil || len(q.Fenetres) == 0 {
		for i, li := range envelopper(a.t("Shown once a cycle has run: Claude Code reports it as it works."), il) {
			if i < ih {
				e.texte(ix, iy+i, il, li, a.st(p.muet, fond))
			}
		}
		return
	}
	maintenant := time.Now().Unix()
	yy := iy
	for _, f := range q.Fenetres {
		if yy+2 > iy+ih {
			break
		}
		passe := f.Reinit > 0 && f.Reinit < maintenant
		pct := -1
		if f.Utilisation != nil && !passe {
			pct = int(math.Round(*f.Utilisation * 100))
		} else if passe {
			pct = 0
		}
		coul := p.accent
		if pct >= 90 {
			coul = p.erreur
		} else if pct >= 70 {
			coul = p.attention
		}
		val := "—"
		if pct >= 0 {
			val = strconv.Itoa(pct) + " %"
		}
		e.texte(ix, yy, il-8, a.nomFenetre(f.Nom), a.gras(p.encre, fond))
		e.texte(ix+il-largeurTexte(val), yy, 0, val, a.gras(coul, fond))
		e.barre(ix, yy+1, il, float64(max(0, pct))/100, a.st(coul, fond), a.st(p.ligne, fond))
		pied := ""
		if passe {
			pied = a.t("started again")
		} else if f.Reinit > 0 {
			pied = a.t("Resets: {0} ({1})", a.quandLisible(f.Reinit), a.dans(f.Reinit))
		}
		if yy+2 < iy+ih {
			e.texte(ix, yy+2, il, pied, a.st(p.muet, fond))
		}
		yy += 3
	}
	if q.Statut == "rejected" && yy < iy+ih {
		e.texte(ix, yy, il, a.t("Limit reached: the pilot waits for it to start again."), a.st(p.attention, fond))
	}
}

// boiteProgres: the cycles run (their cost as bars, the newest ones with their milestone).
func (a *appTUI) boiteProgres(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Progress"), false)
	stats := a.vue.Stats
	total, minutes, maxi := 0.0, 0.0, 0.0
	for _, st := range stats {
		total += st.CoutUSD
		minutes += st.Minutes
		maxi = math.Max(maxi, st.CoutUSD)
	}
	duree := a.t("{0} min", int(minutes))
	if minutes >= 60 {
		duree = a.t("{0} h {1} min", int(minutes/60), fmt.Sprintf("%02d", int(math.Mod(minutes, 60))))
	}
	e.texte(ix, iy, il, a.t("{0} cycle(s), {1} of work, ≈ {2} $ at list price", len(stats), duree, fmt.Sprintf("%.2f", total)), a.st(p.muet, fond))
	// one bar per cycle, its cost (the last ones that fit)
	barres := []rune("▁▂▃▄▅▆▇█")
	larg := max(1, min(4, il/max(1, len(stats))))
	debut := max(0, len(stats)-il/larg)
	for i, st := range stats[debut:] {
		k := 0
		if maxi > 0 {
			k = int(math.Round(st.CoutUSD / maxi * 7))
		}
		coul := p.accent
		if st.Etat != "" && st.Etat != "ok" {
			coul = p.attention
		}
		for j := 0; j < larg-1 || (larg == 1 && j == 0); j++ {
			e.mettre(ix+i*larg+j, iy+1, barres[min(7, max(0, k))], a.st(coul, fond))
		}
	}
	// the savings measured (gains.go), between the bars and the cycles
	haut := 3
	if g := a.vue.Gains; g != nil && ih >= 4 {
		var parts []string
		if g.RTKEntree > 0 {
			parts = append(parts, a.t("RTK: {0} tokens spared (−{1} %)", fmtTokensTUI(g.RTKEconomise), g.RTKPct))
		}
		if g.Ponytail {
			parts = append(parts, "Ponytail "+g.VersionPonytail)
		}
		comparePT := false
		for _, c := range g.Comparaisons {
			nom := map[string]string{"ponytail": "Ponytail", "eco": a.t("Eco mode")}[c.Option]
			parts = append(parts, a.t("{0}: {1} % per cycle", nom, fmt.Sprintf("%+d", c.Ecart)))
			comparePT = comparePT || c.Option == "ponytail"
		}
		// no comparison yet: the cycles with and without Ponytail (no gain without both kinds)
		switch {
		case comparePT || g.PonytailAvec+g.PonytailSans == 0:
		case g.PonytailSans == 0:
			parts = append(parts, a.t("all {0} cycles with Ponytail, none without: no gain measurable", g.PonytailAvec))
		case g.PonytailAvec == 0:
			parts = append(parts, a.t("all {0} cycles without Ponytail", g.PonytailSans))
		default:
			parts = append(parts, a.t("Ponytail: {0} cycles with, {1} without (3 of each needed)", g.PonytailAvec, g.PonytailSans))
		}
		var lignes []string
		for _, x := range parts { // one saving never cut in two
			if n := len(lignes); n > 0 && largeurTexte(lignes[n-1]+" · "+x) <= il {
				lignes[n-1] += " · " + x
			} else {
				lignes = append(lignes, envelopper(x, il)...)
			}
		}
		max := 2
		if g.CachePct > 0 && ih < 7 { // Anthropic's, always on: a line of its own, apart from what the options save
			max = 1
		}
		if len(lignes) > max {
			lignes = lignes[:max]
		}
		n := len(lignes)
		for i, li := range lignes {
			e.texte(ix, iy+2+i, il, li, a.st(p.ok, fond))
		}
		if g.CachePct > 0 {
			e.texte(ix, iy+2+n, il, a.t("Claude's own cache: reading −{0} %", g.CachePct), a.st(p.muet, fond))
			n++
		}
		if n > 0 {
			haut = 2 + n
		}
	}
	// the newest cycles
	for i := 0; i < ih-haut && i < len(stats); i++ {
		st := stats[len(stats)-1-i]
		droite := fmt.Sprintf("%s  %.2f $", a.t("{0} min", int(math.Round(st.Minutes))), st.CoutUSD)
		e.texte(ix, iy+haut+i, 6, st.Cycle, a.st(p.muet, fond))
		titre := st.Titre
		if _, id, ok := strings.Cut(st.Cycle, "·"); ok && titre == "" {
			for _, j := range a.vue.Plan {
				if j.ID == id {
					titre = j.Titre
				}
			}
		}
		e.texte(ix+6, iy+haut+i, il-6-largeurTexte(droite)-1, firstNonEmpty(titre, st.Commit), a.st(p.encre, fond))
		e.texte(ix+il-largeurTexte(droite), iy+haut+i, 0, droite, a.st(p.muet, fond))
	}
}

// enCoursJalon: the milestone is being built (by the lead or a worker).
func (a *appTUI) enCoursJalon(j Jalon) bool {
	v := a.vue
	if !v.Actif || v.Etat == nil {
		return false
	}
	for _, o := range strings.Fields(v.Etat.Ouvriers) {
		if o == j.ID {
			return true
		}
	}
	return v.Etat.JalonEnCours != "" && titreJalonTUI(v.Etat.JalonEnCours, v.Plan) == j.Titre
}

func (a *appTUI) boitePlan(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Project plan"), false)
	plan := a.vue.Plan
	if len(plan) == 0 {
		msg := a.t("No plan yet: Claude writes it during the first cycle.")
		for i, li := range envelopper(msg, il) {
			if i < ih {
				e.texte(ix, iy+i, il, li, a.st(p.muet, fond))
			}
		}
		return
	}
	faits := 0
	for _, j := range plan {
		if j.Fait {
			faits++
		}
	}
	e.barre(ix, iy, il, float64(faits)/float64(len(plan)), a.st(p.accent, fond), a.st(p.ligne, fond))
	e.texte(ix, iy+1, il, a.t("{0} / {1} milestones done", faits, len(plan)), a.st(p.muet, fond))
	// the lines: waves and milestones (the ones archived long ago left out)
	type lignePlan struct {
		t  string
		st styleT
		m  string
		sm styleT
	}
	var lignes []lignePlan
	courant := -1
	for _, j := range plan {
		if j.Archive {
			continue
		}
		if j.Vague != "" {
			lignes = append(lignes, lignePlan{t: strings.ToUpper(j.Vague), st: a.st(p.muet, fond)})
		}
		switch {
		case j.Fait:
			lignes = append(lignes, lignePlan{m: "✔ ", sm: a.st(p.accent, fond), t: j.Titre, st: a.st(p.accent, fond)})
		case a.enCoursJalon(j):
			if courant < 0 {
				courant = len(lignes)
			}
			lignes = append(lignes, lignePlan{m: "▶ ", sm: a.st(p.attention, fond), t: j.Titre, st: a.gras(p.attention, fond)})
		default:
			lignes = append(lignes, lignePlan{m: "○ ", sm: a.st(p.muet, fond), t: j.Titre, st: a.st(p.encre, fond)})
		}
	}
	zone := ih - 2
	if zone < 1 {
		return
	}
	maxDefil := max(0, len(lignes)-zone)
	a.defilPlan = min(max(0, a.defilPlan), maxDefil)
	for i := 0; i < zone && a.defilPlan+i < len(lignes); i++ {
		li := lignes[a.defilPlan+i]
		cx := ix
		if li.m != "" {
			cx += e.texte(cx, iy+2+i, 0, li.m, li.sm)
		}
		e.texte(cx, iy+2+i, il-(cx-ix), li.t, li.st)
	}
	if maxDefil > 0 {
		a.ascenseur(e, x+l-1, iy+2, zone, a.defilPlan, maxDefil)
	}
	_ = courant
	a.zoneMolette(x, y, l, h, func(s int) { a.defilPlan += 3 * s })
}

// ascenseur: where the list shown is in the whole (on the box's right edge).
func (a *appTUI) ascenseur(e *ecranT, x, y, h, pos, max int) {
	if h < 2 || max <= 0 {
		return
	}
	k := y + int(math.Round(float64(pos)/float64(max)*float64(h-1)))
	e.mettre(x, k, '┃', a.st(a.pal.accent, a.pal.panneau))
}

// ----------------------------------------------------------------------------- Log

func (a *appTUI) couleurGenre(g string) couleurT {
	p := a.pal
	switch g {
	case "pilote", "quota":
		return p.attention
	case "erreur":
		return p.erreur
	case "sous_agent":
		return p.violet
	case "fin":
		return p.ok
	case "outil", "resultat", "system":
		return p.muet
	}
	return p.encre
}

// evenementsVisibles: the log's events the filters keep.
func (a *appTUI) evenementsVisibles() []Evenement {
	q := strings.ToLower(a.recherche.texte())
	var l []Evenement
	for _, ev := range a.evs {
		if q != "" && !strings.Contains(strings.ToLower(ev.Titre+" "+ev.Detail), q) {
			continue
		}
		ok := map[string]bool{"pilote": true, "texte": true, "outil": a.outils, "resultat": a.resultats, "erreur": true, "sous_agent": a.sousAgents, "fin": true, "quota": true, "system": true}[ev.Genre]
		if !ok || (ev.SousAgent && !a.sousAgents) {
			continue
		}
		l = append(l, ev)
	}
	return l
}

func (a *appTUI) pageJournal(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Log"), false)
	// the filters
	cx := ix
	caseF := func(k string, on bool, lib string, fn func()) {
		m := "☐ "
		st := a.st(p.muet, fond)
		if on {
			m = "☑ "
			st = a.st(p.encre, fond)
		}
		debut := cx
		cx += e.texte(cx, iy, 0, m+lib, st)
		cx += e.texte(cx, iy, 0, " ("+k+")   ", a.st(p.muet, fond))
		a.zone(debut, iy, cx-debut, 1, fn)
	}
	caseF("o", a.outils, a.t("Tools"), func() { a.outils = !a.outils })
	caseF("r", a.resultats, a.t("Results"), func() { a.resultats = !a.resultats })
	caseF("u", a.sousAgents, a.t("Sub-agents"), func() { a.sousAgents = !a.sousAgents })
	rech := a.recherche.texte()
	if a.rechActive || rech != "" {
		lr := min(30, ix+il-cx-2)
		if lr > 6 {
			e.texte(cx, iy, 0, "/", a.st(p.accent, fond))
			a.recherche.dessiner(e, cx+2, iy, lr, 1, a.st(p.encre, p.ligne), a.rechActive, a.t("filter…"), a.st(p.muet, p.ligne))
		}
	}
	suivi := a.t("Follow")
	if a.defilJ == 0 {
		suivi = "● " + suivi
	} else {
		suivi = "○ " + suivi + " (f)"
	}
	e.texte(ix+il-largeurTexte(suivi), iy, 0, suivi, a.st(map[bool]couleurT{true: p.ok, false: p.muet}[a.defilJ == 0], fond))
	// the events, wrapped, the newest at the bottom
	zone := ih - 1
	if zone < 1 {
		return
	}
	evs := a.evenementsVisibles()
	type lj struct {
		h, t string
		c    couleurT
	}
	largeurT := il - 11
	var lignes []lj
	// only what can be shown: from the end, enough lines for the screen and the scroll
	besoin := zone + a.defilJ + 1
	for i := len(evs) - 1; i >= 0 && len(lignes) < besoin+200; i-- {
		ev := evs[i]
		titre := a.t(ev.Titre)
		if ev.SousAgent {
			titre = "↳ " + titre
		}
		w := envelopper(titre, max(10, largeurT))
		if len(w) > 6 {
			w = append(w[:5], "…")
		}
		bloc := make([]lj, len(w))
		for k, s := range w {
			bloc[k] = lj{t: s, c: a.couleurGenre(ev.Genre)}
		}
		bloc[0].h = ev.Heure
		lignes = append(bloc, lignes...)
	}
	if len(lignes) == 0 {
		e.texte(ix, iy+2, il, a.t("Nothing in the log yet."), a.st(p.muet, fond))
		return
	}
	maxDefil := max(0, len(lignes)-zone)
	a.defilJ = min(max(0, a.defilJ), maxDefil)
	debut := len(lignes) - zone - a.defilJ
	for i := 0; i < zone; i++ {
		k := debut + i
		if k < 0 || k >= len(lignes) {
			continue
		}
		li := lignes[k]
		if li.h != "" {
			e.texte(ix, iy+1+i, 9, li.h, a.st(p.muet, fond))
			e.mettre(ix+9, iy+1+i, '■', a.st(li.c, fond))
		}
		e.texte(ix+11, iy+1+i, largeurT, li.t, a.st(li.c, fond))
	}
	if maxDefil > 0 {
		a.ascenseur(e, x+l-1, iy+1, zone, maxDefil-a.defilJ, maxDefil)
	}
	a.zoneMolette(x, y, l, h, func(s int) { a.defilJ -= 3 * s })
}

// ----------------------------------------------------------------------------- Requests

// titreDemande: a request's title (its first heading, else its first line).
func titreDemande(texte string) string {
	for _, l := range strings.Split(texte, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		return strings.TrimSpace(strings.TrimLeft(l, "# "))
	}
	return ""
}

func (a *appTUI) pageDemandes(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	if a.dem == nil {
		ix, iy, il, _ := a.boite(e, x, y, l, h, a.t("Requests"), false)
		e.texte(ix, iy, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	hd := max(5, h*2/5)
	ix, iy, il, ih := a.boite(e, x, y, l, hd, a.t("Current request"), false)
	if demandeVide(a.dem.Demande) {
		for i, li := range envelopper(a.t("The project has no request yet: Claude would wait for one. Write what you want first.")+" (n)", il) {
			if i < ih {
				e.texte(ix, iy+i, il, li, a.st(p.muet, fond))
			}
		}
	} else {
		lignes := envelopper(a.dem.Demande, il)
		for i := 0; i < ih && i < len(lignes); i++ {
			st := a.st(p.encre, fond)
			if strings.HasPrefix(strings.TrimSpace(lignes[i]), "#") {
				st = a.gras(p.accent, fond)
			}
			e.texte(ix, iy+i, il, lignes[i], st)
		}
		if len(lignes) > ih {
			e.texte(ix+il-1, iy+ih-1, 1, "…", a.st(p.muet, fond))
		}
	}
	// the queue, then the requests done
	ix, iy, il, ih = a.boite(e, x, y+hd, l, h-hd, a.t("Queued requests"), true)
	n := len(a.dem.Attente)
	a.selDem = min(max(0, a.selDem), max(0, n-1))
	if n == 0 {
		e.texte(ix, iy, il, a.t("No request in the queue: n adds one, Claude takes it when the current one is done."), a.st(p.muet, fond))
	}
	type ld struct {
		t, d string
		sel  bool
		st   styleT
		idx  int
	}
	var lignes []ld
	for i, d := range a.dem.Attente {
		lignes = append(lignes, ld{t: fmt.Sprintf("%2d  %s", i+1, firstNonEmpty(d.Nom, d.Fichier)), d: d.Apercu, sel: i == a.selDem, st: a.st(p.encre, fond), idx: i})
	}
	if len(a.dem.Faites) > 0 {
		lignes = append(lignes, ld{t: "", idx: -1})
		for i := len(a.dem.Faites) - 1; i >= 0; i-- {
			f := a.dem.Faites[i]
			m := "✔ "
			if !f.Livree {
				m = "◌ "
			}
			lignes = append(lignes, ld{t: m + f.Titre, d: f.Date, st: a.st(p.muet, fond), idx: -1})
		}
	}
	// keep the selection in view
	if a.selDem < a.defilD {
		a.defilD = a.selDem
	}
	if a.selDem >= a.defilD+ih {
		a.defilD = a.selDem - ih + 1
	}
	a.defilD = min(max(0, a.defilD), max(0, len(lignes)-ih))
	for i := 0; i < ih && a.defilD+i < len(lignes); i++ {
		li := lignes[a.defilD+i]
		f := fond
		st := li.st
		if li.sel {
			f = p.ligne
			st = a.gras(p.encre, f)
			e.rect(ix-1, iy+i, il+2, 1, a.st(p.encre, f))
		}
		n := e.texte(ix, iy+i, il*3/5, li.t, st)
		if li.d != "" && il-n-3 > 8 {
			e.texte(ix+n+3, iy+i, il-n-3, strings.ReplaceAll(li.d, "\n", " "), a.st(p.muet, f))
		}
		if li.idx >= 0 {
			k := li.idx
			a.zone(ix-1, iy+i, il+2, 1, func() { a.selDem = k })
		}
	}
	a.zoneMolette(x, y+hd, l, h-hd, func(s int) { a.selDem += s })
}

// ----------------------------------------------------------------------------- Chat

func (a *appTUI) pageChat(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Chat with Claude"), a.chatActif)
	hs := min(5, max(1, ih/4))
	zone := ih - hs - 1
	// the field
	e.rect(ix-1, iy+zone, il+2, 1, a.st(p.ligne, fond))
	for i := ix - 1; i < ix+il+1; i++ {
		e.mettre(i, iy+zone, '─', a.st(p.ligne, fond))
	}
	vide := a.t("i: write a message")
	if a.chatActif {
		vide = a.t("Your question or the change you want… (Enter sends, Alt+Enter: new line)")
	}
	stS := a.st(p.encre, p.fond)
	a.saisie.dessiner(e, ix-1, iy+zone+1, il+2, hs, stS, a.chatActif && a.dialogue == nil && a.form == nil, vide, a.st(p.muet, p.fond))
	a.zone(ix-1, iy+zone+1, il+2, hs, func() { a.chatActif = true })
	if zone < 1 {
		return
	}
	if a.chat == nil {
		e.texte(ix, iy, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	if a.chat.PiloteActif {
		e.texte(ix, iy, il, "● "+a.t("The pilot is running: Claude only reads and answers. A change becomes a note for the pilot, sent with one click."), a.st(p.attention, fond))
	}
	haut := 0
	if a.chat.PiloteActif {
		haut = 1
	}
	zone -= haut
	type lc struct {
		t  string
		st styleT
	}
	var lignes []lc
	for _, m := range a.chat.Messages {
		lib, coul := a.t("Claude"), p.accent
		switch m.Role {
		case "vous":
			lib, coul = a.t("You"), p.violet
		case "erreur":
			lib, coul = "⚠", p.erreur
		}
		quand := ""
		if d, ok := lireDate(m.Quand); ok {
			quand = "  " + d.Format(a.formatHeure())
		}
		lignes = append(lignes, lc{lib + quand, a.gras(coul, fond)})
		for _, li := range envelopper(m.Texte, il-2) {
			lignes = append(lignes, lc{"  " + li, a.st(p.encre, fond)})
		}
		if len(m.Outils) > 0 {
			lignes = append(lignes, lc{"  ▸ " + a.t("{0} action(s)", len(m.Outils)), a.st(p.muet, fond)})
		}
		if m.Note {
			lignes = append(lignes, lc{"  ✓ " + a.t("Sent to the pilot"), a.st(p.ok, fond)})
		}
		lignes = append(lignes, lc{"", a.st(p.encre, fond)})
	}
	if a.chat.EnCours {
		lignes = append(lignes, lc{a.t("Claude") + "  …", a.gras(p.accent, fond)})
		if a.chat.Partiel != nil {
			for _, tx := range a.chat.Partiel.Textes {
				for _, li := range envelopper(tx, il-2) {
					lignes = append(lignes, lc{"  " + li, a.st(p.encre, fond)})
				}
			}
			for _, o := range a.chat.Partiel.Outils {
				lignes = append(lignes, lc{"  ▸ " + o, a.st(p.muet, fond)})
			}
		}
		lignes = append(lignes, lc{"  " + string("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"[(time.Now().UnixMilli()/100%10)*3:(time.Now().UnixMilli()/100%10)*3+3]), a.st(p.accent, fond)})
	}
	if len(a.chat.Messages) == 0 && !a.chat.EnCours {
		for i, li := range envelopper(a.t("Ask Claude about the project, or for a change: it reads the code, the plan and the log."), il) {
			if i < zone {
				e.texte(ix, iy+haut+i, il, li, a.st(p.muet, fond))
			}
		}
		return
	}
	maxDefil := max(0, len(lignes)-zone)
	a.defilC = min(max(0, a.defilC), maxDefil)
	debut := len(lignes) - zone - a.defilC
	for i := 0; i < zone; i++ {
		k := debut + i
		if k >= 0 && k < len(lignes) {
			e.texte(ix, iy+haut+i, il, lignes[k].t, lignes[k].st)
		}
	}
	if maxDefil > 0 {
		a.ascenseur(e, x+l-1, iy+haut, zone, maxDefil-a.defilC, maxDefil)
	}
	a.zoneMolette(x, y, l, h-hs, func(s int) { a.defilC -= 3 * s })
}

// ----------------------------------------------------------------------------- Setup & health

func (a *appTUI) pageSante(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	large := l >= 110
	ll := l
	if large {
		ll = l * 3 / 5
	}
	ix, iy, il, ih := a.boite(e, x, y, ll, h, a.t("Setup & health"), true)
	if len(a.verifs) == 0 {
		e.texte(ix, iy, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	a.selV = min(max(0, a.selV), len(a.verifs)-1)
	parLigne := 2
	vis := max(1, ih/parLigne)
	if a.selV < a.defilV {
		a.defilV = a.selV
	}
	if a.selV >= a.defilV+vis {
		a.defilV = a.selV - vis + 1
	}
	for i := 0; i < vis && a.defilV+i < len(a.verifs); i++ {
		k := a.defilV + i
		v := a.verifs[k]
		f := fond
		if k == a.selV {
			f = p.ligne
			e.rect(ix-1, iy+i*2, il+2, 2, a.st(p.encre, f))
		}
		ico, coul := "○", p.muet
		switch v.Statut {
		case "ok":
			ico, coul = "✔", p.ok
		case "en_cours":
			ico, coul = "◌", p.accent
		case "echec":
			ico, coul = "✖", p.erreur
			if v.Facultatif {
				ico, coul = "!", p.attention
			}
		}
		e.texte(ix, iy+i*2, 2, ico, a.gras(coul, f))
		e.texte(ix+3, iy+i*2, il-3, a.t(v.Titre), a.gras(p.encre, f))
		res := v.Resume
		if v.EnReparation {
			res = a.t("repair in progress…")
		}
		if res == "" {
			res = a.t(v.Explication)
		}
		stR := a.st(p.muet, f)
		if v.Statut == "echec" {
			stR = a.st(coul, f)
		}
		e.texte(ix+3, iy+i*2+1, il-3, res, stR)
		a.zone(ix-1, iy+i*2, il+2, 2, func() { a.selV = k })
	}
	a.zoneMolette(x, y, ll, h, func(s int) { a.selV += s })
	if !large {
		return
	}
	v := a.verifs[a.selV]
	jx, jy, jl, jh := a.boite(e, x+ll, y, l-ll, h, a.t(v.Titre), false)
	var lignes []segTUI
	for _, li := range envelopper(a.t(v.Explication), jl) {
		lignes = append(lignes, segTUI{li, a.st(p.encre, fond)})
	}
	if v.Resume != "" {
		lignes = append(lignes, segTUI{"", styleT{}})
		for _, li := range envelopper(v.Resume, jl) {
			lignes = append(lignes, segTUI{li, a.st(p.muet, fond)})
		}
	}
	if v.Statut == "echec" && v.ReparationAuto {
		lignes = append(lignes, segTUI{"", styleT{}})
		for _, li := range envelopper("r · "+a.t("Repair")+": "+a.t(v.ReparationInfo), jl) {
			lignes = append(lignes, segTUI{li, a.st(p.accent, fond)})
		}
	}
	for i, s := range lignes {
		if i < jh {
			e.texte(jx, jy+i, jl, s.t, s.s)
		}
	}
}

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
	for i, lib := range []string{a.t("Save"), a.t("Cancel")} {
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
		{"1…9, 0, Tab", t("Pages")},
		{"s", t("Start")},
		{"a", t("Stop after cycle") + " / " + t("Let it go on")},
		{"x", t("Stop now")},
		{"p, [ ]", t("Choose a project")},
		{"N · M", t("New project") + " · " + t("Manage")},
		{"g", t("Get GitHub's work")},
		{"↑ ↓ PgUp PgDn", t("Scroll")},
		{"n · e · d", t("New request") + " · " + t("Note along the way") + " · " + t("Delete")},
		{"o r u / f", t("Log") + ": " + t("Tools") + ", " + t("Results") + ", " + t("Sub-agents") + ", " + t("filter…") + ", " + t("Follow")},
		{"i · Enter · Esc", t("Chat") + ": " + t("Write") + " · " + t("Send") + " · " + t("Leave the field")},
		{"v · r · I · T", t("Setup & health") + ": " + t("Check everything") + " · " + t("Repair") + " · " + t("Install everything") + " · " + t("Token")},
		{"c · l · r · p · a", "GitHub: " + t("Connect to GitHub") + " · " + t("Use one of my repositories") + " · " + t("Create a private repository") + " · " + t("Push now") + " · " + t("Automatic push")},
		{"l · p · a", t("Settings") + ": " + t("Language") + " · " + t("Palette") + " · " + t("Accent")},
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
	pied := t("The mouse works too: click a project, a page, a line; the wheel scrolls.")
	e.texte(ix, iy+ih-1, il, pied, a.st(p.muet, p.panneau))
}
