package main

// The terminal interface (athanor tui): its screen. A grid of cells drawn in memory, then only the rows
// that changed since the last frame are sent to the terminal, in one write (no flicker). Colours are the
// app's palette (truecolor when the terminal says it has it, else the nearest of the 256 colours).

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// couleurT: 0xRRGGBB, or aucuneCouleur (the terminal's own).
type couleurT int32

const aucuneCouleur couleurT = -1

type styleT struct {
	av, ar   couleurT // ink, background
	gras     bool
	faible   bool
	souligne bool
	inverse  bool
}

type celluleT struct {
	r     rune
	s     styleT
	suite bool // the right half of a wide character
}

type ecranT struct {
	l, h     int
	c        []celluleT
	curX     int // the text cursor (a field being typed in), -1: hidden
	curY     int
	fondBase styleT
}

func nouvelEcran(l, h int) *ecranT {
	if l < 1 {
		l = 1
	}
	if h < 1 {
		h = 1
	}
	e := &ecranT{l: l, h: h, c: make([]celluleT, l*h), curX: -1, curY: -1}
	e.effacer(styleT{av: aucuneCouleur, ar: aucuneCouleur})
	return e
}

func (e *ecranT) effacer(s styleT) {
	e.fondBase = s
	for i := range e.c {
		e.c[i] = celluleT{r: ' ', s: s}
	}
	e.curX, e.curY = -1, -1
}

// largeurRune: the columns a character takes (0: combining marks, joiners; 2: wide East Asian characters
// and emoji).
func largeurRune(r rune) int {
	switch {
	case r == 0 || r == 0x200b || r == 0x200c || r == 0x200d || r == 0x2060 || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0xe0100 && r <= 0xe01ef):
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	}
	for _, p := range plagesLarges {
		if r < p[0] {
			return 1
		}
		if r <= p[1] {
			return 2
		}
	}
	return 1
}

// plagesLarges: East Asian Wide and Fullwidth, and the emoji shown wide (sorted).
var plagesLarges = [][2]rune{
	{0x1100, 0x115f}, {0x231a, 0x231b}, {0x2329, 0x232a}, {0x23e9, 0x23ec}, {0x23f0, 0x23f0}, {0x23f3, 0x23f3},
	{0x25fd, 0x25fe}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267f, 0x267f}, {0x2693, 0x2693}, {0x26a1, 0x26a1},
	{0x26aa, 0x26ab}, {0x26bd, 0x26be}, {0x26c4, 0x26c5}, {0x26ce, 0x26ce}, {0x26d4, 0x26d4}, {0x26ea, 0x26ea},
	{0x26f2, 0x26f3}, {0x26f5, 0x26f5}, {0x26fa, 0x26fa}, {0x26fd, 0x26fd}, {0x2705, 0x2705}, {0x270a, 0x270b},
	{0x2728, 0x2728}, {0x274c, 0x274c}, {0x274e, 0x274e}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27b0, 0x27b0}, {0x27bf, 0x27bf}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50}, {0x2b55, 0x2b55}, {0x2e80, 0x303e},
	{0x3041, 0x33ff}, {0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0xa000, 0xa4cf}, {0xa960, 0xa97f}, {0xac00, 0xd7a3},
	{0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f}, {0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x16fe0, 0x16fe4},
	{0x17000, 0x18cff}, {0x1b000, 0x1b2ff}, {0x1f004, 0x1f004}, {0x1f0cf, 0x1f0cf}, {0x1f18e, 0x1f18e},
	{0x1f191, 0x1f19a}, {0x1f200, 0x1f2ff}, {0x1f300, 0x1f320}, {0x1f32d, 0x1f335}, {0x1f337, 0x1f37c},
	{0x1f37e, 0x1f393}, {0x1f3a0, 0x1f3ca}, {0x1f3cf, 0x1f3d3}, {0x1f3e0, 0x1f3f0}, {0x1f3f4, 0x1f3f4},
	{0x1f3f8, 0x1f43e}, {0x1f440, 0x1f440}, {0x1f442, 0x1f4fc}, {0x1f4ff, 0x1f53d}, {0x1f54b, 0x1f54e},
	{0x1f550, 0x1f567}, {0x1f57a, 0x1f57a}, {0x1f595, 0x1f596}, {0x1f5a4, 0x1f5a4}, {0x1f5fb, 0x1f64f},
	{0x1f680, 0x1f6c5}, {0x1f6cc, 0x1f6cc}, {0x1f6d0, 0x1f6d2}, {0x1f6d5, 0x1f6d7}, {0x1f6dc, 0x1f6df},
	{0x1f6eb, 0x1f6ec}, {0x1f6f4, 0x1f6fc}, {0x1f7e0, 0x1f7eb}, {0x1f7f0, 0x1f7f0}, {0x1f90c, 0x1f93a},
	{0x1f93c, 0x1f945}, {0x1f947, 0x1f9ff}, {0x1fa70, 0x1faff}, {0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

// largeurTexte: the columns a text takes.
func largeurTexte(t string) int {
	n := 0
	for _, r := range t {
		n += largeurRune(r)
	}
	return n
}

// couperTexte: at most max columns, "…" at the end when cut.
func couperTexte(t string, max int) string {
	if max <= 0 {
		return ""
	}
	if largeurTexte(t) <= max {
		return t
	}
	var b strings.Builder
	n := 0
	for _, r := range t {
		l := largeurRune(r)
		if n+l > max-1 {
			break
		}
		b.WriteRune(r)
		n += l
	}
	b.WriteString("…")
	return b.String()
}

// completerTexte: t cut or padded with spaces to exactly l columns.
func completerTexte(t string, l int) string {
	t = couperTexte(t, l)
	if n := largeurTexte(t); n < l {
		t += strings.Repeat(" ", l-n)
	}
	return t
}

// nettoyerLigne: one line of text, its controls (tabs, escapes a log may hold) made harmless.
func nettoyerLigne(t string) string {
	if !strings.ContainsFunc(t, func(r rune) bool { return r < 32 || r == 0x7f || (r >= 0x80 && r < 0xa0) }) {
		return t
	}
	var b strings.Builder
	for _, r := range t {
		switch {
		case r == '\t':
			b.WriteString("  ")
		case r < 32 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// envelopper: the text in lines of at most l columns, cut at the spaces (a word longer than a line is cut).
func envelopper(t string, l int) []string {
	if l < 1 {
		l = 1
	}
	var lignes []string
	for _, para := range strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n") {
		para = nettoyerLigne(para)
		if para == "" {
			lignes = append(lignes, "")
			continue
		}
		var cour strings.Builder
		n := 0
		for _, mot := range strings.SplitAfter(para, " ") {
			lm := largeurTexte(mot)
			if n > 0 && n+largeurTexte(strings.TrimRight(mot, " ")) > l {
				lignes = append(lignes, strings.TrimRight(cour.String(), " "))
				cour.Reset()
				n = 0
			}
			for lm > l { // a word longer than the line: cut
				var debut strings.Builder
				k := 0
				reste := mot
				for i, r := range mot {
					if k+largeurRune(r) > l-n {
						reste = mot[i:]
						break
					}
					debut.WriteRune(r)
					k += largeurRune(r)
				}
				cour.WriteString(debut.String())
				lignes = append(lignes, cour.String())
				cour.Reset()
				n = 0
				mot = reste
				lm = largeurTexte(mot)
			}
			cour.WriteString(mot)
			n += lm
		}
		lignes = append(lignes, strings.TrimRight(cour.String(), " "))
	}
	return lignes
}

// mettre: one character at (x, y), its style; outside the screen: nothing.
func (e *ecranT) mettre(x, y int, r rune, s styleT) int {
	l := largeurRune(r)
	if l == 0 || y < 0 || y >= e.h || x < 0 || x+l > e.l {
		return l
	}
	i := y*e.l + x
	// a wide character cut in two by this one: its other half becomes a space
	if e.c[i].suite && x > 0 {
		e.c[i-1] = celluleT{r: ' ', s: e.c[i-1].s}
	}
	if x+l < e.l && e.c[i+l].suite {
		e.c[i+l] = celluleT{r: ' ', s: e.c[i+l].s}
	}
	e.c[i] = celluleT{r: r, s: s}
	if l == 2 {
		e.c[i+1] = celluleT{r: ' ', s: s, suite: true}
	}
	return l
}

// texte: t at (x, y), at most max columns (max <= 0: to the right edge); the columns used.
func (e *ecranT) texte(x, y, max int, t string, s styleT) int {
	if max <= 0 || x+max > e.l {
		max = e.l - x
	}
	if max <= 0 || y < 0 || y >= e.h {
		return 0
	}
	t = couperTexte(nettoyerLigne(t), max)
	n := 0
	for _, r := range t {
		n += e.mettre(x+n, y, r, s)
	}
	return n
}

// rect: a rectangle of spaces in this style.
func (e *ecranT) rect(x, y, l, h int, s styleT) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+l; i++ {
			e.mettre(i, j, ' ', s)
		}
	}
}

// cadre: a rounded box, its title in its top edge.
func (e *ecranT) cadre(x, y, l, h int, titre string, sc, st styleT) {
	if l < 2 || h < 2 {
		return
	}
	e.mettre(x, y, '╭', sc)
	e.mettre(x+l-1, y, '╮', sc)
	e.mettre(x, y+h-1, '╰', sc)
	e.mettre(x+l-1, y+h-1, '╯', sc)
	for i := x + 1; i < x+l-1; i++ {
		e.mettre(i, y, '─', sc)
		e.mettre(i, y+h-1, '─', sc)
	}
	for j := y + 1; j < y+h-1; j++ {
		e.mettre(x, j, '│', sc)
		e.mettre(x+l-1, j, '│', sc)
	}
	if titre != "" && l > 6 {
		e.mettre(x+1, y, ' ', sc)
		n := e.texte(x+2, y, l-5, titre, st)
		e.mettre(x+2+n, y, ' ', sc)
	}
}

// barre: a progress bar of l columns, filled to frac (0..1).
func (e *ecranT) barre(x, y, l int, frac float64, plein, vide styleT) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	huitiemes := int(frac*float64(l)*8 + 0.5)
	blocs := []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}
	for i := 0; i < l; i++ {
		switch {
		case huitiemes >= (i+1)*8:
			e.mettre(x+i, y, '█', plein)
		case huitiemes > i*8:
			e.mettre(x+i, y, blocs[huitiemes-i*8], styleT{av: plein.av, ar: vide.av})
		default:
			e.mettre(x+i, y, '─', vide)
		}
	}
}

// ----------------------------------------------------------------------------- sending to the terminal

// sgr: the escape sequence of a style.
func sgr(s styleT, vraies bool) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if s.gras {
		b.WriteString(";1")
	}
	if s.faible {
		b.WriteString(";2")
	}
	if s.souligne {
		b.WriteString(";4")
	}
	if s.inverse {
		b.WriteString(";7")
	}
	coul := func(c couleurT, base int) {
		if c == aucuneCouleur {
			return
		}
		r, g, bl := int(c>>16)&0xff, int(c>>8)&0xff, int(c)&0xff
		if vraies {
			fmt.Fprintf(&b, ";%d;2;%d;%d;%d", base, r, g, bl)
		} else {
			fmt.Fprintf(&b, ";%d;5;%d", base, couleur256(r, g, bl))
		}
	}
	coul(s.av, 38)
	coul(s.ar, 48)
	b.WriteByte('m')
	return b.String()
}

// couleur256: the nearest colour of the 256-colour palette (its 6×6×6 cube or its greys).
func couleur256(r, g, b int) int {
	niveau := func(v int) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (v - 35) / 40
	}
	valeur := []int{0, 95, 135, 175, 215, 255}
	ir, ig, ib := niveau(r), niveau(g), niveau(b)
	cube := 16 + 36*ir + 6*ig + ib
	dc := carre(valeur[ir]-r) + carre(valeur[ig]-g) + carre(valeur[ib]-b)
	moy := (r + g + b) / 3
	gi := 0
	if moy > 238 {
		gi = 23
	} else if moy > 3 {
		gi = (moy - 3) / 10
	}
	gv := 8 + 10*gi
	dg := carre(gv-r) + carre(gv-g) + carre(gv-b)
	if dg < dc {
		return 232 + gi
	}
	return cube
}

func carre(v int) int { return v * v }

// rendu: what to send for this frame, prec the frame on screen (nil: all of it).
func (e *ecranT) rendu(prec *ecranT, vraies bool) []byte {
	var b strings.Builder
	b.WriteString("\x1b[?2026h\x1b[?25l") // synchronized update (ignored where unknown), cursor hidden
	toutes := prec == nil || prec.l != e.l || prec.h != e.h
	dernier := ""
	for y := 0; y < e.h; y++ {
		ligne := e.c[y*e.l : (y+1)*e.l]
		if !toutes && egalLigne(ligne, prec.c[y*e.l:(y+1)*e.l]) {
			continue
		}
		b.WriteString("\x1b[" + strconv.Itoa(y+1) + ";1H")
		dernier = ""
		for x := 0; x < e.l; x++ {
			c := ligne[x]
			if c.suite {
				continue
			}
			if s := sgr(c.s, vraies); s != dernier {
				b.WriteString(s)
				dernier = s
			}
			r := c.r
			if r == 0 || !utf8.ValidRune(r) {
				r = ' '
			}
			// the last cell of the screen: written, the cursor does not wrap (no scrolling)
			b.WriteRune(r)
		}
	}
	b.WriteString("\x1b[0m")
	if e.curX >= 0 && e.curY >= 0 && e.curX < e.l && e.curY < e.h {
		fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[?25h", e.curY+1, e.curX+1)
	}
	b.WriteString("\x1b[?2026l")
	return []byte(b.String())
}

func egalLigne(a, b []celluleT) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copie: the frame as sent (compared with the next one).
func (e *ecranT) copie() *ecranT {
	c := *e
	c.c = append([]celluleT(nil), e.c...)
	return &c
}

// ligneTexte: row y as text (the tests read the screen with it).
func (e *ecranT) ligneTexte(y int) string {
	var b strings.Builder
	for x := 0; x < e.l; x++ {
		c := e.c[y*e.l+x]
		if !c.suite {
			b.WriteRune(c.r)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// texteEcran: the whole screen as text.
func (e *ecranT) texteEcran() string {
	l := make([]string, e.h)
	for y := range l {
		l[y] = e.ligneTexte(y)
	}
	return strings.Join(l, "\n")
}
