package main

// The terminal interface: a text field (one line, or several: a request, a chat message), with the
// keys of a terminal's editors (arrows, Home/End, Ctrl+A/E/U/K/W, paste).

import (
	"strings"
	"unicode"
)

type saisieT struct {
	t        []rune
	pos      int
	multi    bool // several lines (Enter adds one where the caller says so)
	masque   bool // a password: dots
	defil    int  // the first line shown (multi)
	maxRunes int
}

func (s *saisieT) texte() string { return string(s.t) }

func (s *saisieT) mettreTexte(t string) {
	s.t = []rune(t)
	s.pos = len(s.t)
}

func (s *saisieT) inserer(t string) {
	if !s.multi {
		t = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(t)
	}
	r := []rune(strings.ReplaceAll(t, "\r\n", "\n"))
	if s.maxRunes > 0 && len(s.t)+len(r) > s.maxRunes {
		r = r[:max(0, s.maxRunes-len(s.t))]
	}
	n := append([]rune{}, s.t[:s.pos]...)
	n = append(n, r...)
	s.t = append(n, s.t[s.pos:]...)
	s.pos += len(r)
}

// touche: a key for the field; false: not one of its keys (the caller handles it).
func (s *saisieT) touche(ev evenementTUI) bool {
	if ev.genre == evCollage {
		s.inserer(ev.texte)
		return true
	}
	if ev.genre != evTouche {
		return false
	}
	debutLigne := func(p int) int {
		for p > 0 && s.t[p-1] != '\n' {
			p--
		}
		return p
	}
	finLigne := func(p int) int {
		for p < len(s.t) && s.t[p] != '\n' {
			p++
		}
		return p
	}
	switch ev.touche() {
	case "gauche", "ctrl+b":
		if s.pos > 0 {
			s.pos--
		}
	case "droite", "ctrl+f":
		if s.pos < len(s.t) {
			s.pos++
		}
	case "ctrl+gauche", "alt+b":
		for s.pos > 0 && unicode.IsSpace(s.t[s.pos-1]) {
			s.pos--
		}
		for s.pos > 0 && !unicode.IsSpace(s.t[s.pos-1]) {
			s.pos--
		}
	case "ctrl+droite", "alt+f":
		for s.pos < len(s.t) && unicode.IsSpace(s.t[s.pos]) {
			s.pos++
		}
		for s.pos < len(s.t) && !unicode.IsSpace(s.t[s.pos]) {
			s.pos++
		}
	case "debut", "ctrl+a":
		s.pos = debutLigne(s.pos)
	case "fin", "ctrl+e":
		s.pos = finLigne(s.pos)
	case "ctrl+debut":
		s.pos = 0
	case "ctrl+fin":
		s.pos = len(s.t)
	case "retour", "ctrl+h":
		if s.pos > 0 {
			s.t = append(s.t[:s.pos-1], s.t[s.pos:]...)
			s.pos--
		}
	case "suppr", "ctrl+d":
		if s.pos < len(s.t) {
			s.t = append(s.t[:s.pos], s.t[s.pos+1:]...)
		}
	case "ctrl+u":
		d := debutLigne(s.pos)
		s.t = append(s.t[:d], s.t[s.pos:]...)
		s.pos = d
	case "ctrl+k":
		f := finLigne(s.pos)
		s.t = append(s.t[:s.pos], s.t[f:]...)
	case "ctrl+w", "alt+retour":
		d := s.pos
		for d > 0 && unicode.IsSpace(s.t[d-1]) {
			d--
		}
		for d > 0 && !unicode.IsSpace(s.t[d-1]) {
			d--
		}
		s.t = append(s.t[:d], s.t[s.pos:]...)
		s.pos = d
	case "haut", "bas":
		if !s.multi {
			return false
		}
		d := debutLigne(s.pos)
		col := s.pos - d
		if ev.nom == "haut" {
			if d == 0 {
				s.pos = 0
				return true
			}
			pd := debutLigne(d - 1)
			s.pos = min(pd+col, d-1)
		} else {
			f := finLigne(s.pos)
			if f >= len(s.t) {
				s.pos = len(s.t)
				return true
			}
			nf := finLigne(f + 1)
			s.pos = min(f+1+col, nf)
		}
	default:
		if ev.nom == "" && !ev.ctrl && !ev.alt && ev.r >= 32 {
			s.inserer(string(ev.r))
			return true
		}
		return false
	}
	return true
}

// lignesSaisie: the text cut into lines of l columns (hard wrap), and where the cursor falls (line, column).
func (s *saisieT) lignesSaisie(l int) (lignes []string, cl, cc int) {
	if l < 1 {
		l = 1
	}
	var cour []rune
	n := 0
	ligne := 0
	fixer := func(i int) {
		if i == s.pos {
			cl, cc = ligne, n
		}
	}
	for i, r := range s.t {
		fixer(i)
		if s.masque {
			r = '•'
		}
		if r == '\n' {
			lignes = append(lignes, string(cour))
			cour, n = nil, 0
			ligne++
			continue
		}
		w := largeurRune(r)
		if n+w > l {
			lignes = append(lignes, string(cour))
			cour, n = nil, 0
			ligne++
			if i == s.pos {
				cl, cc = ligne, 0
			}
		}
		cour = append(cour, r)
		n += w
	}
	if s.pos >= len(s.t) {
		cl, cc = ligne, n
		if n >= l { // the cursor after a full line: at the start of the next one
			lignes = append(lignes, string(cour))
			cour = nil
			cl, cc = ligne+1, 0
		}
	}
	lignes = append(lignes, string(cour))
	return lignes, cl, cc
}

// dessiner: the field in the rectangle (x, y, l, h), its cursor shown when actif.
func (s *saisieT) dessiner(e *ecranT, x, y, l, h int, st styleT, actif bool, vide string, sVide styleT) {
	e.rect(x, y, l, h, st)
	if len(s.t) == 0 && vide != "" {
		for i, ligne := range envelopper(vide, l) {
			if i >= h {
				break
			}
			e.texte(x, y+i, l, ligne, sVide)
		}
	}
	lignes, cl, cc := s.lignesSaisie(l)
	if cl < s.defil {
		s.defil = cl
	}
	if cl >= s.defil+h {
		s.defil = cl - h + 1
	}
	if len(s.t) > 0 {
		for i := 0; i < h && s.defil+i < len(lignes); i++ {
			e.texte(x, y+i, l, lignes[s.defil+i], st)
		}
	}
	if actif {
		e.curX, e.curY = x+min(cc, l-1), y+cl-s.defil
	}
}
