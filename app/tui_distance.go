package main

// The terminal interface: Remote (distance.go), as in the window: the switch, the pairing link and its QR
// code drawn in the terminal (two modules per character), the 6 digits shown on demand, the trusted devices.
// A computer without a screen (SSH, a server) pairs a phone from here.

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

const montrerChiffresTUI = 10 * time.Second // as a click in the window

func (a *appTUI) chargerDistance() {
	tous := 2 * time.Second
	if a.dist != nil && a.dist.Actif && a.dist.Etat == "pret" {
		tous = time.Second // the digits change every 20 seconds, the link every 2 minutes
	}
	a.charger("distance", tous, func(c clientTUI) (func(*appTUI), error) {
		var v DistanceVue
		if err := c.lire("distance", &v); err != nil {
			return nil, err
		}
		return func(a *appTUI) { a.dist = &v }, nil
	})
}

func (a *appTUI) raccourcisDistance() []raccourci {
	t := a.t
	if a.dist == nil {
		return nil
	}
	r := []raccourci{{"o", t("Remote access")}}
	if a.dist.Chiffres != "" {
		r = append(r, raccourci{"v", t("Show the code")})
	}
	if len(a.dist.Appareils) > 0 {
		r = append(r, raccourci{"r", t("Rename")}, raccourci{"d", t("Remove")})
	}
	return r
}

// matriceQR: the QR code of a text, its modules (true: black) with a white margin of 2 around.
func matriceQR(texte string) [][]bool {
	c, err := qr.Encode(texte, qr.M)
	if err != nil {
		return nil
	}
	const marge = 2
	n := c.Size + 2*marge
	m := make([][]bool, n)
	for y := range m {
		m[y] = make([]bool, n)
		for x := range m[y] {
			m[y][x] = c.Black(x-marge, y-marge)
		}
	}
	return m
}

// dessinerQR: the modules two rows per character (▀: the upper one in the ink, the lower one in the
// background), black on white whatever the palette, so that a camera reads it.
func dessinerQR(e *ecranT, x, y int, m [][]bool) {
	coul := func(noir bool) couleurT {
		if noir {
			return 0x000000
		}
		return 0xffffff
	}
	for r := 0; r*2 < len(m); r++ {
		for i := range m[r*2] {
			bas := false
			if r*2+1 < len(m) {
				bas = m[r*2+1][i]
			}
			e.mettre(x+i, y+r, '▀', styleT{av: coul(m[r*2][i]), ar: coul(bas)})
		}
	}
}

func (a *appTUI) pageDistance(e *ecranT, x, y, l, h int) {
	p := a.pal
	fond := p.panneau
	ix, iy, il, ih := a.boite(e, x, y, l, h, a.t("Remote access"), false)
	d := a.dist
	if d == nil {
		e.texte(ix, iy, il, a.t("Updating…"), a.st(p.muet, fond))
		return
	}
	var qrM [][]bool
	if d.Lien != "" {
		qrM = matriceQR(d.Lien)
	}
	lq, hq := len(qrM), (len(qrM)+1)/2
	// the QR code on the right when there is room for it and for the texts, else under them
	cote := qrM != nil && il >= lq+40 && ih >= hq
	lt := il
	if cote {
		lt = il - lq - 3
	}
	var lignes []segTUI
	ajouter := func(t string, st styleT) {
		for _, li := range envelopper(t, lt) {
			lignes = append(lignes, segTUI{li, st})
		}
	}
	vide := func() { lignes = append(lignes, segTUI{"", styleT{}}) }
	interr := "○ " + a.t("off")
	if d.Actif {
		interr = "● " + a.t("on")
	}
	lignes = append(lignes, segTUI{a.t("Remote access") + ": " + interr + "   (o)", a.gras(p.encre, fond)})
	switch d.Etat {
	case "arret":
		ajouter(a.t("Off: no device can reach Athanor."), a.st(p.muet, fond))
		ajouter(a.t("Follow and drive Athanor from your phone or another computer, from anywhere: through a Cloudflare tunnel (https, nothing to set up). Only the devices you pair here get in."), a.st(p.muet, fond))
	case "installation":
		ajouter(a.t("Downloading Cloudflare's tunnel program (once)…"), a.st(p.muet, fond))
	case "demarrage":
		ajouter(a.t("Opening the tunnel…"), a.st(p.muet, fond))
	case "pret":
		ajouter("✔ "+a.t("Reachable at {0}", d.Adresse), a.st(p.ok, fond))
	case "erreur":
		ajouter("✖ "+firstNonEmpty(d.Message, a.t("The tunnel did not start.")), a.st(p.erreur, fond))
	}
	if d.Etat == "pret" && d.Lien != "" {
		vide()
		lignes = append(lignes, segTUI{a.t("Pair a device"), a.gras(p.encre, fond)})
		ajouter(a.t("Scan it with the phone's camera, or open this link on the device:"), a.st(p.muet, fond))
		// the link whole, cut at the width (no space to wrap it at), so that it can be selected and copied
		for r := []rune(d.Lien); len(r) > 0; {
			n := min(len(r), max(1, lt))
			lignes = append(lignes, segTUI{string(r[:n]), a.st(p.accent, fond)})
			r = r[n:]
		}
		ajouter(a.t("Single-use link, renewed by itself")+" ("+dureeCourteTUI(d.Expire)+"). "+a.t("Never send it to anyone: whoever opens it gets in."), a.st(p.muet, fond))
		vide()
		lignes = append(lignes, segTUI{a.t("Then, the code to type on the device"), a.gras(p.encre, fond)})
		if time.Now().Before(a.chiffresJusque) && len(d.Chiffres) == 6 {
			lignes = append(lignes, segTUI{"  " + d.Chiffres[:3] + " " + d.Chiffres[3:] + "   (" + dureeCourteTUI(max(1, d.ChiffresExpire/1000)) + ")", a.gras(p.accent, fond)})
		} else {
			lignes = append(lignes, segTUI{"  ••• •••   v · " + a.t("Show the code"), a.st(p.muet, fond)})
		}
		ajouter(a.t("Shown only here, renewed every 20 seconds: a photo of the QR code is not enough, the device must see this screen. Three wrong codes and it must scan again."), a.st(p.muet, fond))
	}
	vide()
	lignes = append(lignes, segTUI{a.t("Trusted devices"), a.gras(p.encre, fond)})
	debutApp := len(lignes)
	if len(d.Appareils) == 0 {
		ajouter(a.t("No device yet."), a.st(p.muet, fond))
	}
	a.selApp = min(max(0, a.selApp), max(0, len(d.Appareils)-1))
	for i, ap := range d.Appareils {
		marque, st := "  ", a.st(p.encre, fond)
		if i == a.selApp {
			marque, st = "▸ ", a.gras(p.encre, fond)
		}
		etat, cEtat := a.t("Not connected"), p.muet
		if ap.EnLigne {
			etat, cEtat = a.t("Connected now"), p.ok
		} else if ap.VuLe != "" {
			if t, err := time.Parse(time.RFC3339, ap.VuLe); err == nil {
				etat = a.t("Last seen {0}", t.Local().Format("2006-01-02 "+a.formatHeure()))
			}
		}
		if ap.Lieu != "" {
			etat += " · " + ap.Lieu
		}
		if ap.Notifs {
			etat += " · 🔔 " + a.t("notifications on")
		}
		lignes = append(lignes, segTUI{marque + ap.Nom, st}, segTUI{"    " + etat, a.st(cEtat, fond)})
	}
	// the devices kept in view: the lines above them scroll away first
	debut := 0
	if fin := debutApp + 2*a.selApp + 2; len(d.Appareils) > 0 && fin > ih {
		debut = min(fin-ih, debutApp)
	}
	for i := debut; i < len(lignes) && i-debut < ih; i++ {
		e.texte(ix, iy+i-debut, lt, lignes[i].t, lignes[i].s)
		if k := (i - debutApp) / 2; i >= debutApp && k < len(d.Appareils) {
			a.zone(ix-1, iy+i-debut, lt+1, 1, func() { a.selApp = k })
		}
	}
	if qrM == nil {
		return
	}
	switch {
	case cote:
		dessinerQR(e, ix+il-lq, iy, qrM)
	case len(lignes)-debut+1+hq <= ih && lq <= il:
		dessinerQR(e, ix, iy+len(lignes)-debut+1, qrM)
	default:
		if n := len(lignes) - debut; n < ih {
			e.texte(ix, iy+n, il, a.t("The terminal is too small for the QR code: open the link on the device, or make the terminal bigger."), a.st(p.attention, fond))
		}
	}
}

// dureeCourteTUI: seconds as "1 min 40 s" / "18 s".
func dureeCourteTUI(s int) string {
	if s >= 60 {
		return fmt.Sprintf("%d min %02d s", s/60, s%60)
	}
	return fmt.Sprintf("%d s", s)
}

func (a *appTUI) toucheDistance(k string, defiler func(*int, int)) {
	d := a.dist
	if d == nil {
		return
	}
	rafraichir := func(map[string]any) { a.derniere["distance"] = time.Time{} }
	switch k {
	case "o":
		if !d.Actif {
			a.agir("POST", "distance", map[string]bool{"actif": true}, rafraichir)
			return
		}
		a.demander(a.t("Remote access"), a.t("Off: no device can reach Athanor."),
			[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Remote access") + ": " + a.t("off"), danger: true}}, func(id string) {
				if id == "oui" {
					a.agir("POST", "distance", map[string]bool{"actif": false}, rafraichir)
				}
			})
	case "v":
		if d.Chiffres != "" {
			a.chiffresJusque = time.Now().Add(montrerChiffresTUI)
		}
	case "r", "d":
		if a.selApp < 0 || a.selApp >= len(d.Appareils) {
			return
		}
		ap := d.Appareils[a.selApp]
		chemin := "distance/appareils/" + url.PathEscape(ap.ID)
		if k == "d" {
			a.demander(a.t("Remove from the trusted devices"), a.t("Remove “{0}” from the trusted devices? It must scan the code again to come back.", ap.Nom),
				[]choixTUI{{id: "", texte: a.t("Cancel")}, {id: "oui", texte: a.t("Remove"), danger: true}}, func(id string) {
					if id == "oui" {
						a.agir("DELETE", chemin, nil, rafraichir)
					}
				})
			return
		}
		nom := &saisieT{}
		nom.mettreTexte(ap.Nom)
		a.formulaire(a.t("Rename"), a.t(aideFormulaireLigne), []champTUI{{lib: a.t("Name"), h: 1, s: nom}}, func(v []string) {
			if n := strings.TrimSpace(v[0]); n != "" {
				a.agirFormulaire("POST", chemin, map[string]string{"nom": n}, rafraichir)
			}
		})
	default:
		defiler(&a.selApp, -1)
	}
}
