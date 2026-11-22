package main

// The terminal interface: the hub's home page (pAccueil), shown at launch, to read: one tile per module of
// GET /api/hub (the same Resume() as the web home), the line of what needs the owner and the bell's
// alerts with their reason, "In progress" (the elements of every module, as the web home). Acting on them
// is the window's or a paired phone's. Its keys are 1 and H (Escape from Remote too).

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

type reponseHubTUI struct {
	Modules  []ModuleVue  `json:"modules"`
	Elements []ElementHub `json:"elements"`
	Alertes  []AlerteHub  `json:"alertes"`
}

func (a *appTUI) chargerHub() {
	a.charger("hub", 2*time.Second, func(c clientTUI) (func(*appTUI), error) {
		var r reponseHubTUI
		if err := c.lire("hub", &r); err != nil {
			return nil, err
		}
		return func(a *appTUI) { a.hub = &r }, nil
	})
}

// texteStat: a figure of a tile (the quota: a percentage, else the status the last cycle reported).
func (a *appTUI) texteStat(s StatModule) string {
	switch v := s.Valeur.(type) {
	case float64:
		if s.Cle == "Quota used" {
			return strconv.Itoa(int(v)) + " %"
		}
		return strconv.Itoa(int(v))
	case string:
		statuts := map[string]string{"allowed": "OK", "allowed_warning": "Near the limit", "rejected": "Limit reached"}
		if st, ok := statuts[v]; ok {
			return a.t(st)
		}
		return a.t(v) // a status figure is an i18n key
	}
	return ""
}

func (a *appTUI) pageAccueil(e *ecranT, x, y, l, h int) {
	p := a.pal
	t := a.t
	if a.hub == nil {
		e.texte(x+2, y+1, l-4, t("Updating…"), a.st(p.muet, p.fond))
		return
	}
	var mods []ModuleVue // a tile per module that has figures (the hub's and Settings' have none)
	for _, m := range a.hub.Modules {
		if len(m.Resume.Stats) > 0 {
			mods = append(mods, m)
		}
	}
	cols := 1
	if l >= 76 { // 80 columns: two tiles a row, or 4 modules leave no room for what needs the owner
		cols = min(2, len(mods))
	}
	if l >= 126 {
		cols = min(3, len(mods))
	}
	cols = max(cols, 1)
	lt := l / cols
	besoin := 0
	yc := y
	var hauteurs []int // a row of tiles: as tall as its tallest
	total := 0
	for i := 0; i < len(mods); i += cols {
		hr := 0
		for j := i; j < min(i+cols, len(mods)); j++ {
			hr = max(hr, len(mods[j].Resume.Stats)+3)
		}
		hauteurs, total = append(hauteurs, hr), total+hr
	}
	// a short screen: the rows lose their last figures, bottom row first, so that "Needs you" and Missions stay
	for k := len(hauteurs) - 1; total > h-4 && slices.Max(append([]int{0}, hauteurs...)) > 4; k-- {
		if k < 0 {
			k = len(hauteurs) - 1
		}
		if hauteurs[k] > 4 {
			hauteurs[k]--
			total--
		}
	}
	for r, i := 0, 0; i < len(mods); r, i = r+1, i+cols {
		hr := min(hauteurs[r], y+h-yc)
		if hr < 3 {
			break
		}
		for j := i; j < min(i+cols, len(mods)); j++ {
			a.tuileTUI(e, x+(j-i)*lt, yc, lt, hr, mods[j])
		}
		yc += hr
	}
	for _, m := range mods {
		besoin += m.Resume.Besoin
	}
	noms := map[string]string{}
	for _, m := range a.hub.Modules {
		noms[m.ID] = t(m.Nom)
	}
	if yc < y+h {
		st := a.st(p.muet, p.fond)
		if besoin > 0 {
			st = a.gras(p.attention, p.fond)
		}
		e.texte(x+2, yc, l-4, t("Needs you: {0}", besoin), st)
		yc++
	}
	// the bell's alerts (the ignored ones left out), with their reason, as the web's bell says them;
	// at most 3, leaving room for "In progress" and Missions
	for _, al := range a.hub.Alertes[:min(len(a.hub.Alertes), 3, max(0, y+h-yc-6))] {
		e.texte(x+4, yc, l-6, "! "+t("{0}, {1}: {2}", noms[al.Module], al.Titre, t(al.Raison, al.Args...)), a.st(p.attention, p.fond))
		yc++
	}
	// "In progress": every module's elements that wait for the owner (first, the API's order) or work
	var liste []ElementHub
	for _, el := range a.hub.Elements {
		if el.Attente != nil || el.Etat == "actif" {
			liste = append(liste, el)
		}
	}
	if he := min(2+max(1, len(liste)), y+h-yc-3); he >= 3 {
		ix, iy, il, ih := a.boite(e, x, yc, l, he, t("In progress"), false)
		if len(liste) == 0 {
			e.texte(ix, iy, il, t("Nothing in progress right now."), a.st(p.muet, p.panneau))
		}
		lt, lm := 0, 0 // the titles' and the modules' columns
		for _, el := range liste {
			lt, lm = max(lt, largeurTexte(el.Titre)+2), max(lm, largeurTexte(noms[el.Module])+2)
		}
		lt, lm = min(lt, il/3), min(lm, il/4)
		for i, el := range liste[:min(len(liste), ih)] {
			a.ligneElementTUI(e, ix, iy+i, il, lt, lm, el, noms[el.Module])
		}
		yc += he
	}
}

// ligneElementTUI: one element of "In progress" (as the web's ligneElement): its state, title, module,
// then why it needs the owner, or its milestone and what its worker does.
func (a *appTUI) ligneElementTUI(e *ecranT, x, y, l, lt, lm int, el ElementHub, module string) {
	p, t := a.pal, a.t
	coul, texte, st := p.muet, t("Idle"), a.st(p.muet, p.panneau)
	switch el.Etat {
	case "termine":
		texte = t("Finished")
	case "actif":
		coul, texte = p.ok, t("In progress")
		// "Milestone 3/40 : its title, what the pilot does", as the web's activiteElement says it
		j, quoi := "", strings.Join(slices.DeleteFunc([]string{el.Activite, t(el.Action)}, func(s string) bool { return s == "" }), ", ")
		if av := el.Avancement; av != nil && av.Total > 0 && av.Etape != "" {
			j = t(av.Etape, min(av.Fait+1, av.Total), av.Total)
		}
		switch {
		case j != "" && quoi != "":
			texte = j + " : " + quoi
		case j+quoi != "":
			texte = j + quoi
		}
	}
	if el.Attente != nil {
		coul, texte, st = p.attention, t(el.Attente.Raison, el.Attente.Args...), a.st(p.attention, p.panneau)
	}
	e.texte(x, y, 2, "● ", a.st(coul, p.panneau))
	e.texte(x+2, y, lt, el.Titre, a.gras(p.encre, p.panneau))
	e.texte(x+2+lt, y, lm, module, a.st(p.muet, p.panneau))
	e.texte(x+2+lt+lm, y, l-2-lt-lm, texte, st)
}

// tuileTUI: a module's tile: its state, then one figure per line.
func (a *appTUI) tuileTUI(e *ecranT, x, y, l, h int, m ModuleVue) {
	p := a.pal
	r := m.Resume
	coul, etat := p.muet, a.t("Idle")
	switch r.Etat {
	case "ok":
		coul, etat = p.ok, a.t("Active")
	case "attention":
		coul, etat = p.attention, a.t("Needs attention")
	}
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t(m.Nom), r.Etat == "attention")
	e.texte(ix, iy, 2, "● ", a.st(coul, p.panneau))
	e.texte(ix+2, iy, il-2, etat, a.st(p.muet, p.panneau))
	lv := 0 // the figures' column: as wide as the widest (a quota status is a word), at most half the tile
	for _, s := range r.Stats {
		lv = max(lv, largeurTexte(a.texteStat(s))+2)
	}
	lv = min(lv, il/2)
	for i, s := range r.Stats {
		if i+1 >= ih {
			break
		}
		e.texte(ix, iy+1+i, lv, a.texteStat(s), a.gras(p.encre, p.panneau))
		e.texte(ix+lv, iy+1+i, il-lv, a.t(s.Cle), a.st(p.muet, p.panneau))
	}
}
