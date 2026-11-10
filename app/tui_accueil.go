package main

// The terminal interface: the hub's home page (pAccueil), shown at launch. One tile per module of
// GET /api/hub (the same Resume() as the web home), the line of what needs the owner, the Missions
// block (empty at stage 1). Its key is H (Escape from a page too); 1…9, 0 keep Dev's pages.

import (
	"strconv"
	"time"
)

type reponseHubTUI struct {
	Modules []ModuleVue `json:"modules"`
}

// pagesHash: the web page a tile's figure opens, as a page of the terminal (#global: the projects are
// in the left column, the Overview is the closest page).
var pagesHash = map[string]int{"#global": pSuivi, "#suivi": pSuivi, "#equipe": pEquipe, "#demandes": pDemandes,
	"#chat": pChat, "#pourvous": pPourVous, "#journal": pJournal}

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

// ouvrirHash: the terminal's page of a web page (nothing for a page the terminal does not have).
func (a *appTUI) ouvrirHash(hash string) {
	if pg, ok := pagesHash[hash]; ok {
		a.allerPage(pg)
	}
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
	if l >= 84 {
		cols = min(2, len(mods))
	}
	if l >= 126 {
		cols = min(3, len(mods))
	}
	cols = max(cols, 1)
	lt := l / cols
	besoin := 0
	yc := y
	for i := 0; i < len(mods); i += cols {
		hr := 0 // a row of tiles: as tall as its tallest
		for j := i; j < min(i+cols, len(mods)); j++ {
			hr = max(hr, len(mods[j].Resume.Stats)+3)
		}
		hr = min(hr, y+h-yc)
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
	if yc < y+h {
		st := a.st(p.muet, p.fond)
		if besoin > 0 {
			st = a.gras(p.attention, p.fond)
		}
		e.texte(x+2, yc, l-4, t("Needs you: {0}", besoin), st)
		if besoin > 0 {
			a.zone(x+2, yc, l-4, 1, func() { a.allerPage(pPourVous) })
		}
		yc++
	}
	if hm := min(4, y+h-yc); hm >= 3 {
		ix, iy, il, _ := a.boite(e, x, yc, l, hm, t("Missions"), false)
		e.texte(ix, iy, il, "◇ "+t("No mission yet"), a.gras(p.encre, p.panneau))
		if hm >= 4 {
			e.texte(ix+2, iy+1, il-2, t("Coming soon: one sentence, and Dev and Servers work together."), a.st(p.muet, p.panneau))
		}
	}
}

// tuileTUI: a module's tile: its state, then one figure per line (a click opens its page).
func (a *appTUI) tuileTUI(e *ecranT, x, y, l, h int, m ModuleVue) {
	p := a.pal
	r := m.Resume
	coul, etat := p.muet, a.t("Idle")
	switch r.Etat {
	case "ok":
		coul, etat = p.ok, a.t("Working")
	case "attention":
		coul, etat = p.attention, a.t("Needs attention")
	}
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t(m.Nom), r.Etat == "attention")
	e.texte(ix, iy, 2, "● ", a.st(coul, p.panneau))
	e.texte(ix+2, iy, il-2, etat, a.st(p.muet, p.panneau))
	if len(m.Pages) > 0 {
		ouvrir := a.t("Open") + " →"
		lo := largeurTexte(ouvrir)
		if il > lo+largeurTexte(etat)+4 {
			e.texte(ix+il-lo, iy, 0, ouvrir, a.gras(p.accent, p.panneau))
		}
		hash := m.Pages[0].Hash
		a.zone(ix-1, iy, il+2, 1, func() { a.ouvrirHash(hash) })
	}
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
		hash := s.Page
		a.zone(ix-1, iy+1+i, il+2, 1, func() { a.ouvrirHash(hash) })
	}
}
