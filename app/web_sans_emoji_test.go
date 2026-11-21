package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// symbolesPermis: the only symbols the interface's own files may hold (M239). Everything else (emoji, ticks, crosses,
// warning signs, triangles, dots, stars…) is drawn by the CSS icons (.ic-*, app.css). Each exception is text, not decoration:
var symbolesPermis = map[rune]string{
	'→': "a path in a text (Settings → Lock) and the right arrow key",
	'←': "the left arrow key in shortcuts (Ctrl + Win + ← / →, Previous milestone (←))",
	'−': "the minus sign of a number (−12 %)",
	'≈': "an estimate (≈ 3 $)",
	'±': "a margin in a number",
	'×': "a multiplication or a size (1280 × 720)",
	'≥': "a comparison", '≤': "a comparison",
	'°': "degrees", '€': "a price", '$': "a price",
	'©': "a brand sign, kept by the chat's emoji filter (RE_EMOJI)", '®': "a brand sign", '™': "a brand sign",
	'␍': "shows a carriage return in a diff (the character itself is invisible)",
}

// dossiersTiers: third-party code shipped as is (the terminal's xterm.js draws box characters itself).
var dossiersTiers = []string{"xterm"}

func TestWebSansEmoji(t *testing.T) {
	err := filepath.WalkDir("static", func(chemin string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, x := range dossiersTiers {
				if d.Name() == x {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(chemin) {
		case ".js", ".css", ".html", ".svg":
		default:
			return nil
		}
		b, err := os.ReadFile(chemin)
		if err != nil {
			return err
		}
		for i, ligne := range strings.Split(string(b), "\n") {
			for _, r := range ligne {
				if r < 128 || symbolesPermis[r] != "" {
					continue
				}
				// symbols, emoji presentation selectors and private-use glyphs: never in the interface's own text
				if unicode.IsSymbol(r) || r == 0xFE0F || r == 0x200D || unicode.Is(unicode.Co, r) {
					t.Errorf("%s:%d: symbole %q (U+%04X) : utiliser une icône CSS (ic(\"…\"), .ic-*) ou l'ajouter à symbolesPermis avec sa raison", chemin, i+1, r, r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestWebIconesDefinies: every ic("name") of the pages has its .ic-name in app.css (a missing one would draw a blank).
func TestWebIconesDefinies(t *testing.T) {
	css, err := os.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	vus := 0
	for _, f := range []string{"static/app.js"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`\bic\("([a-z-]+)"\)`).FindAllStringSubmatch(string(b), -1) {
			vus++
			if !strings.Contains(string(css), ".ic-"+m[1]+" {") {
				t.Errorf("%s : ic(%q) sans .ic-%s dans app.css", f, m[1], m[1])
			}
		}
	}
	if vus < 50 {
		t.Errorf("seulement %d icônes ic(…) trouvées : le motif ne lit plus app.js ?", vus)
	}
}
