package main

import (
	"math"
	"testing"
)

// The page's buttons: 46 x 32 CSS pixels each, at the right of a 1200 px wide page.
func zonesTest() zonesPage {
	return zonesPage{Largeur: 1200, Min: [4]float64{138, 0, 46, 32}, Max: [4]float64{92, 0, 46, 32}, Fermer: [4]float64{46, 0, 46, 32}}
}

func TestEchelleCadre(t *testing.T) {
	z := zonesTest()
	if e := z.echelle(1200); e != 1 {
		t.Fatalf("echelle 100 %%: %v", e)
	}
	if e := z.echelle(1800); e != 1.5 {
		t.Fatalf("echelle 150 %%: %v", e)
	}
	mauvais := []zonesPage{
		{},
		{Largeur: 1200, Min: [4]float64{138, 0, 46, 32}, Max: [4]float64{92, 0, 46, 32}}, // close missing
		{Largeur: 1200, Min: [4]float64{math.NaN(), 0, 46, 32}, Max: [4]float64{92, 0, 46, 32}, Fermer: [4]float64{46, 0, 46, 32}},
		{Largeur: 1200, Min: [4]float64{138, 0, 46, 32}, Max: [4]float64{92, 0, 46, 32}, Fermer: [4]float64{20, 0, 46, 32}}, // past the right edge
		{Largeur: 1200, Min: [4]float64{138, 0, 46, 32}, Max: [4]float64{92, 0, 46, 32}, Fermer: [4]float64{900, 0, 900, 32}},
	}
	for i, m := range mauvais {
		if e := m.echelle(1200); e != 0 {
			t.Errorf("%d: echelle %v, want 0", i, e)
		}
	}
	if e := z.echelle(10); e != 0 { // 0.008: nonsense
		t.Errorf("tiny window: %v", e)
	}
}

func TestBoutonsCadre(t *testing.T) {
	z := zonesTest()
	b := z.boutons(1200, 1)
	if b[2] != (rectC{1154, 0, 1200, 32}) || b[0] != (rectC{1062, 0, 1108, 32}) {
		t.Fatalf("boutons: %+v", b)
	}
	// resized wider before the page reports again: still at the right edge
	b = z.boutons(1500, 1)
	if b[2] != (rectC{1454, 0, 1500, 32}) {
		t.Fatalf("after resize: %+v", b)
	}
	b = z.boutons(1800, 1.5)
	if b[2] != (rectC{1731, 0, 1800, 48}) {
		t.Fatalf("150 %%: %+v", b)
	}
	if u := union(z.boutons(1200, 1)); u != (rectC{1062, 0, 1200, 32}) {
		t.Fatalf("union: %+v", u)
	}
	if u := union(z.boutons(1200, 0)); !u.vide() {
		t.Fatalf("no scale: %+v", u)
	}
}

func TestZoneTitre(t *testing.T) {
	b := zonesTest().boutons(1200, 1)
	cas := []struct {
		x, y     int32
		agrandie bool
		code     uintptr
	}{
		{600, 2, false, htTop},
		{3, 2, false, htTopLeft},
		{1195, 2, false, htTopRight},
		{600, 2, true, htCaption}, // maximized: no edge
		{1170, 2, true, htClose},  // maximized: the very corner closes
		{1170, 20, false, htClose},
		{1120, 20, false, htMaximize},
		{1070, 20, false, htMinimize},
		{1060, 20, false, htCaption},
		{600, 20, false, htCaption},
	}
	for _, c := range cas {
		if g := zoneTitre(c.x, c.y, 1200, 8, c.agrandie, b); g != c.code {
			t.Errorf("(%d, %d, max %v): %d, want %d", c.x, c.y, c.agrandie, g, c.code)
		}
	}
	if nomBouton(htMaximize) != "agrandir" || nomBouton(htCaption) != "" {
		t.Fatal("nomBouton")
	}
}
