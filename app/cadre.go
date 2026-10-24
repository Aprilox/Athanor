package main

// The geometry of the application's own title bar (Windows, see cadre_windows.go): which part of the bar
// a point is on, and where the page's three window buttons are in the window's pixels. Pure functions,
// tested on every system.

import "math"

// Windows' hit-test codes (WM_NCHITTEST) the bar answers with.
const (
	htCaption  = 2
	htMinimize = 8
	htMaximize = 9
	htTop      = 12
	htTopLeft  = 13
	htTopRight = 14
	htClose    = 20
)

// rectC: a rectangle in the window's client pixels, right and bottom excluded.
type rectC struct{ G, H, D, B int32 }

func (r rectC) contient(x, y int32) bool { return x >= r.G && x < r.D && y >= r.H && y < r.B }
func (r rectC) vide() bool               { return r.D <= r.G || r.B <= r.H }

// zonesPage: the three buttons as the page reports them, in CSS pixels: [distance from the right edge
// of the page to the button's left side, top, width, height], and the page's width. Measured from the
// right, they stay right when the window is resized before the page reports them again.
type zonesPage struct {
	Largeur float64    `json:"largeur"`
	Min     [4]float64 `json:"min"`
	Max     [4]float64 `json:"max"`
	Fermer  [4]float64 `json:"fermer"`
}

// echelle: window pixels per CSS pixel (the page's zoom and the screen's scaling), 0 when the page's
// report makes no sense.
func (z zonesPage) echelle(largeurClient int32) float64 {
	if largeurClient <= 0 || !(z.Largeur >= 100 && z.Largeur <= 100000) {
		return 0
	}
	e := float64(largeurClient) / z.Largeur
	if e < 0.25 || e > 8 {
		return 0
	}
	for _, b := range [][4]float64{z.Min, z.Max, z.Fermer} {
		for _, v := range b {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1000 {
				return 0
			}
		}
		if b[2] < 8 || b[3] < 8 || b[2] > 200 || b[3] > 100 || b[0] < b[2] || b[0] > 1000 {
			return 0
		}
	}
	return e
}

// boutons: the three buttons (minimize, maximize, close) in client pixels for a client area
// largeurClient wide, with the scale e learned when the page reported them.
func (z zonesPage) boutons(largeurClient int32, e float64) (r [3]rectC) {
	if e <= 0 {
		return
	}
	for i, b := range [][4]float64{z.Min, z.Max, z.Fermer} {
		g := largeurClient - int32(math.Round(b[0]*e))
		h := int32(math.Round(b[1] * e))
		r[i] = rectC{g, h, g + int32(math.Round(b[2]*e)), h + int32(math.Round(b[3]*e))}
	}
	return
}

// union: the smallest rectangle around the three buttons.
func union(r [3]rectC) rectC {
	u := r[0]
	for _, x := range r[1:] {
		if x.vide() {
			continue
		}
		if u.vide() {
			u = x
			continue
		}
		u.G, u.H = min(u.G, x.G), min(u.H, x.H)
		u.D, u.B = max(u.D, x.D), max(u.B, x.B)
	}
	return u
}

// zoneTitre: what Windows must be told about the point (x, y) of the bar, in client pixels: the top edge
// resizes the window (bord pixels high, not when maximized: Windows has no edge there), then the three
// buttons, and everywhere else the bar moves the window.
func zoneTitre(x, y, largeur, bord int32, agrandie bool, b [3]rectC) uintptr {
	if !agrandie && y >= 0 && y < bord {
		switch {
		case x < 2*bord:
			return htTopLeft
		case x >= largeur-2*bord:
			return htTopRight
		}
		return htTop
	}
	for i, code := range []uintptr{htMinimize, htMaximize, htClose} {
		if b[i].contient(x, y) {
			return code
		}
	}
	return htCaption
}

// nomBouton: the name the page gives the button of a hit-test code ("" for none).
func nomBouton(code uintptr) string {
	switch code {
	case htMinimize:
		return "reduire"
	case htMaximize:
		return "agrandir"
	case htClose:
		return "fermer"
	}
	return ""
}
