package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIImageDoc: the terminal interface drawn as HTML for docs/images/terminal.png (README).
// Only with ATHANOR_DOC_TUI=<file.html>; the HTML is turned into a PNG by the docs' image script.
func TestTUIImageDoc(t *testing.T) {
	sortie := os.Getenv("ATHANOR_DOC_TUI")
	if sortie == "" {
		t.Skip("ATHANOR_DOC_TUI not set")
	}
	dossier := t.TempDir()
	p := Projet{Nom: "sky-garden", Dossier: dossier}
	_ = os.MkdirAll(p.Atelier(), 0o755)
	plan := "# Plan\n\n- [x] M1: Game design document\n- [x] M2: Project skeleton\n- [x] M3: Player controller\n- [ ] M4: Tiles and level 1\n- [ ] M5: Enemies\n- [ ] M6: HUD and save\n"
	if err := os.WriteFile(filepath.Join(p.Atelier(), "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	kit := t.TempDir()
	_ = os.MkdirAll(filepath.Join(kit, "template"), 0o755)
	_ = os.WriteFile(filepath.Join(kit, "template", "CLAUDE.md"), []byte("# x\n"), 0o644)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: p.Nom, Langue: "en"})
	a := nouvelleAppTUI(clientTUIPour(t, s), true)
	a.tic()
	attendreTUI(t, a, func() bool { return a.vue != nil && a.c.projet == p.Nom })
	taper(a, "1")
	e := nouvelEcran(120, 32)
	a.dessiner(e)
	if !strings.Contains(e.texteEcran(), "sky-garden") {
		t.Fatalf("no project on the screen:\n%s", e.texteEcran())
	}
	if err := os.WriteFile(sortie, []byte(ecranHTML(e)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ecranHTML: one <span> per run of cells of the same style, colors of a dark terminal.
func ecranHTML(e *ecranT) string {
	coul := func(c couleurT, defaut string) string {
		if c == aucuneCouleur {
			return defaut
		}
		return fmt.Sprintf("#%06x", int32(c))
	}
	var b strings.Builder
	b.WriteString(`<pre style="margin:0;padding:12px;background:#0f1419;color:#d6dde6;font:15px/1.3 'Cascadia Mono',Consolas,monospace">`)
	for y := 0; y < e.h; y++ {
		for x := 0; x < e.l; {
			c := e.c[y*e.l+x]
			var run strings.Builder
			for ; x < e.l && e.c[y*e.l+x].s == c.s; x++ {
				if !e.c[y*e.l+x].suite {
					run.WriteRune(e.c[y*e.l+x].r)
				}
			}
			av, ar := coul(c.s.av, "#d6dde6"), coul(c.s.ar, "transparent")
			if c.s.inverse {
				av, ar = coul(c.s.ar, "#0f1419"), coul(c.s.av, "#d6dde6")
			}
			style := "color:" + av + ";background:" + ar
			if c.s.gras {
				style += ";font-weight:bold"
			}
			if c.s.faible {
				style += ";opacity:.6"
			}
			if c.s.souligne {
				style += ";text-decoration:underline"
			}
			fmt.Fprintf(&b, `<span style="%s">%s</span>`, style, html.EscapeString(run.String()))
		}
		b.WriteString("\n")
	}
	b.WriteString("</pre>")
	return b.String()
}
