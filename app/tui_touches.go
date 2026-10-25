package main

// The terminal interface: what the keyboard and the mouse send, read from the terminal's bytes (keys,
// escape sequences, SGR mouse reports, bracketed paste).

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	evTouche = iota
	evSouris
	evCollage
)

type evenementTUI struct {
	genre  int
	nom    string // a named key: haut, bas, gauche, droite, pgprec, pgsuiv, debut, fin, entree, echap, tab, stab, retour, suppr, inser, f1…f12
	r      rune   // a character typed (nom == "")
	ctrl   bool
	alt    bool
	maj    bool
	x, y   int    // the mouse (0-based)
	bouton int    // 0 left, 1 middle, 2 right; molette: -1 up, +1 down
	appui  bool   // the mouse: pressed (false: released)
	texte  string // pasted
}

// touche: "q", "ctrl+c", "alt+entree", "haut"… (what the interface compares).
func (e evenementTUI) touche() string {
	n := e.nom
	if n == "" {
		n = string(e.r)
	}
	if e.maj && e.nom != "" {
		n = "maj+" + n
	}
	if e.alt {
		n = "alt+" + n
	}
	if e.ctrl {
		n = "ctrl+" + n
	}
	return n
}

// analyserTouches: the events in b; the bytes of a sequence not complete yet are given back (reste), unless
// fin says nothing more is coming (then a lone Escape is the Escape key).
func analyserTouches(b []byte, fin bool) (evs []evenementTUI, reste []byte) {
	for len(b) > 0 {
		ev, n, ok := uneTouche(b, fin)
		if !ok {
			return evs, b
		}
		if ev != nil {
			evs = append(evs, *ev)
		}
		b = b[n:]
	}
	return evs, nil
}

// uneTouche: the first event of b and its length; ok false: incomplete.
func uneTouche(b []byte, fin bool) (*evenementTUI, int, bool) {
	c := b[0]
	switch {
	case c == 0x1b:
		if len(b) == 1 {
			if fin {
				return &evenementTUI{nom: "echap"}, 1, true
			}
			return nil, 0, false
		}
		switch b[1] {
		case '[':
			return sequenceCSI(b, fin)
		case 'O':
			if len(b) < 3 {
				if fin {
					return &evenementTUI{r: 'O', alt: true}, 2, true
				}
				return nil, 0, false
			}
			if n, ok := map[byte]string{'A': "haut", 'B': "bas", 'C': "droite", 'D': "gauche", 'H': "debut", 'F': "fin", 'P': "f1", 'Q': "f2", 'R': "f3", 'S': "f4", 'M': "entree"}[b[2]]; ok {
				return &evenementTUI{nom: n}, 3, true
			}
			return nil, 3, true
		case 0x1b:
			return &evenementTUI{nom: "echap"}, 1, true
		}
		// Alt + a key
		ev, n, ok := uneTouche(b[1:], fin)
		if !ok {
			return nil, 0, false
		}
		if ev != nil {
			ev.alt = true
		}
		return ev, n + 1, true
	case c == '\r' || c == '\n':
		// one Enter: some terminals (and SSH clients) send it as CR LF, or CR NUL; taken as two, the form
		// a first Enter opens was saved at once by the second (an LF read apart: lireTouchesTUI)
		if c == '\r' && len(b) > 1 && (b[1] == '\n' || b[1] == 0) {
			return &evenementTUI{nom: "entree"}, 2, true
		}
		return &evenementTUI{nom: "entree"}, 1, true
	case c == '\t':
		return &evenementTUI{nom: "tab"}, 1, true
	case c == 0x7f || c == 0x08:
		return &evenementTUI{nom: "retour"}, 1, true
	case c == 0:
		return &evenementTUI{r: ' ', ctrl: true}, 1, true
	case c < 0x20:
		return &evenementTUI{r: rune('a' + c - 1), ctrl: true}, 1, true
	}
	if !utf8.FullRune(b) {
		if fin {
			return nil, len(b), true
		}
		return nil, 0, false
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError {
		return nil, n, true
	}
	return &evenementTUI{r: r}, n, true
}

// sequenceCSI: ESC [ … (keys with a modifier, the mouse, a paste).
func sequenceCSI(b []byte, fin bool) (*evenementTUI, int, bool) {
	// a paste: everything up to ESC [ 201 ~ is text
	if strings.HasPrefix(string(b), "\x1b[200~") {
		i := strings.Index(string(b), "\x1b[201~")
		if i < 0 {
			if fin && len(b) > 1<<20 {
				return &evenementTUI{genre: evCollage, texte: string(b[6:])}, len(b), true
			}
			return nil, 0, false
		}
		return &evenementTUI{genre: evCollage, texte: strings.ReplaceAll(string(b[6:i]), "\r\n", "\n")}, i + 6, true
	}
	i := 2
	for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
		i++
	}
	if i >= len(b) {
		if fin || len(b) > 64 {
			return nil, len(b), true
		}
		return nil, 0, false
	}
	final := b[i]
	params := string(b[2:i])
	n := i + 1
	// the mouse (SGR): ESC [ < bouton ; x ; y M|m
	if strings.HasPrefix(params, "<") && (final == 'M' || final == 'm') {
		p := strings.Split(params[1:], ";")
		if len(p) != 3 {
			return nil, n, true
		}
		bt, _ := strconv.Atoi(p[0])
		x, _ := strconv.Atoi(p[1])
		y, _ := strconv.Atoi(p[2])
		ev := &evenementTUI{genre: evSouris, x: x - 1, y: y - 1, appui: final == 'M', ctrl: bt&16 != 0, alt: bt&8 != 0, maj: bt&4 != 0}
		switch {
		case bt&64 != 0:
			ev.bouton = -1
			if bt&1 != 0 {
				ev.bouton = 1
			}
			ev.nom = "molette"
		case bt&32 != 0:
			return nil, n, true // a move with a button held: not used
		default:
			ev.bouton = bt & 3
		}
		return ev, n, true
	}
	champs := strings.Split(params, ";")
	mod := 1
	if len(champs) >= 2 {
		mod, _ = strconv.Atoi(champs[1])
	}
	ev := &evenementTUI{}
	if mod > 1 {
		m := mod - 1
		ev.maj, ev.alt, ev.ctrl = m&1 != 0, m&2 != 0, m&4 != 0
	}
	switch final {
	case 'A':
		ev.nom = "haut"
	case 'B':
		ev.nom = "bas"
	case 'C':
		ev.nom = "droite"
	case 'D':
		ev.nom = "gauche"
	case 'H':
		ev.nom = "debut"
	case 'F':
		ev.nom = "fin"
	case 'Z':
		ev.nom = "stab"
	case '~':
		k, _ := strconv.Atoi(champs[0])
		noms := map[int]string{1: "debut", 2: "inser", 3: "suppr", 4: "fin", 5: "pgprec", 6: "pgsuiv", 7: "debut", 8: "fin",
			11: "f1", 12: "f2", 13: "f3", 14: "f4", 15: "f5", 17: "f6", 18: "f7", 19: "f8", 20: "f9", 21: "f10", 23: "f11", 24: "f12"}
		nom, ok := noms[k]
		if !ok {
			return nil, n, true
		}
		ev.nom = nom
	case 'P', 'Q', 'R', 'S':
		ev.nom = "f" + string(rune('1'+final-'P'))
	case 'I', 'O': // focus in / out
		return nil, n, true
	default:
		return nil, n, true
	}
	return ev, n, true
}
