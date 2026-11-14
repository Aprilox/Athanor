package main

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestPalettesTerminal: the terminal's themes (app.js PALETTES_TERMINAL, M231d) are all there and readable: the text and
// the 14 coloured ANSI colours at 4.5:1 at least on the background (WCAG AA), the selected text too, the cursor at 3:1.
// black stays out: it is the background's own shade in a dark theme, as programs expect.
func TestPalettesTerminal(t *testing.T) {
	b, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := strings.ReplaceAll(string(b), "\r\n", "\n")
	i := strings.Index(js, "\nconst PALETTES_TERMINAL = {")
	j := strings.Index(js[i+1:], "\n};")
	if i < 0 || j < 0 {
		t.Fatal("no PALETTES_TERMINAL in app.js")
	}
	palettes := map[string]map[string]string{}
	for _, m := range regexp.MustCompile(`(?s)"?([\w-]+)"?: \{([^}]*)\}`).FindAllStringSubmatch(js[i:i+1+j], -1) {
		p := map[string]string{}
		for _, c := range regexp.MustCompile(`(\w+): "(#[0-9a-f]{6})"`).FindAllStringSubmatch(m[2], -1) {
			p[c[1]] = c[2]
		}
		palettes[m[1]] = p
	}
	lum := func(h string) float64 {
		var c [3]float64
		for k := range c {
			v, _ := strconv.ParseUint(h[1+2*k:3+2*k], 16, 8)
			x := float64(v) / 255
			if x <= 0.03928 {
				c[k] = x / 12.92
			} else {
				c[k] = math.Pow((x+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	}
	contraste := func(a, b string) float64 {
		x, y := lum(a), lum(b)
		return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
	}
	couleurs := []string{"foreground", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite"}
	for _, nom := range []string{"athanor-clair", "athanor-sombre", "solarized", "nord", "dracula", "gruvbox", "one-dark"} {
		p := palettes[nom]
		if len(p) != 21 {
			t.Errorf("%s: %d colours of 21", nom, len(p))
			continue
		}
		if !strings.Contains(js, `"`+nom+`": "`) && !strings.Contains(js, " "+nom+`: "`) {
			t.Errorf("%s: no name in NOMS_PALETTES", nom)
		}
		for _, c := range couleurs {
			if r := contraste(p[c], p["background"]); r < 4.5 {
				t.Errorf("%s: %s %s on %s = %.2f:1, 4.5 needed", nom, c, p[c], p["background"], r)
			}
		}
		if r := contraste(p["foreground"], p["selectionBackground"]); r < 4.5 {
			t.Errorf("%s: selected text %.2f:1, 4.5 needed", nom, r)
		}
		if r := contraste(p["cursor"], p["background"]); r < 3 {
			t.Errorf("%s: cursor %.2f:1, 3 needed", nom, r)
		}
		if r := contraste(p["cursorAccent"], p["cursor"]); r < 3 {
			t.Errorf("%s: letter under the cursor %.2f:1, 3 needed", nom, r)
		}
	}
	if len(palettes) != 7 {
		t.Errorf("%d palettes, 7 expected", len(palettes))
	}
}
