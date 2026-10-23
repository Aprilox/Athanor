package main

// Claude's AI-free tools of the workshop (replaces system/athanor.py, same files and outputs):
//   athanor screenshots [folder] | --accept   new, changed or suspicious screenshots, sheets to look at
//   athanor map                               .athanor/MAP.md: files, functions, outlines
//   athanor archive                           old checked milestones and old log entries into archives/
// The project is $PROJECT (/projet when unset). Nothing is summarised or guessed, nothing deleted.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func cheminRelatifPur(p, racine string) string {
	if p == racine {
		return "."
	}
	sep := "/"
	if runtimeWindows() {
		sep = `\`
	}
	if strings.HasPrefix(p, strings.TrimSuffix(racine, sep)+sep) {
		return p[len(strings.TrimSuffix(racine, sep))+1:]
	}
	return p
}

func estDossier(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
func estFichier(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
func existePy(p string) bool   { _, err := os.Stat(p); return err == nil }

// pngsDe: dossier.glob("*.png"), sorted (case-insensitive in native Windows, as pathlib there).
func pngsDe(dossier string) []string {
	var out []string
	if !estDossier(dossier) {
		return out
	}
	es, _ := os.ReadDir(dossier)
	for _, e := range es {
		n := e.Name()
		if strings.HasSuffix(n, ".png") || (runtimeWindows() && strings.HasSuffix(strings.ToLower(n), ".png")) {
			out = append(out, joindrePur(dossier, n))
		}
	}
	sort.Strings(out)
	return out
}

// ============================================================================ screenshots

const seuilChangement, seuilUniforme = 1.0, 3.0

type signature struct {
	l, h  int
	petit *imageRGB
}

func signatureImage(chemin string) (signature, error) {
	im, err := lireRGB(chemin)
	if err != nil {
		return signature{}, err
	}
	return signature{im.l, im.h, redimensionner(im, 160, 90)}, nil
}

func ecartImages(a, b *imageRGB) float64 {
	m, _ := statistiques(difference(a, b))
	return (m[0] + m[1] + m[2]) / 3 / 255 * 100
}

func ecartType(im *imageRGB) float64 {
	_, e := statistiques(im)
	return (e[0] + e[1] + e[2]) / 3
}

// tailleVignette: Image.thumbnail's size for the box (l, h), nil when the image already fits.
func tailleVignette(w, h, l, H int) (int, int, bool) {
	if l >= w && H >= h {
		return w, h, false
	}
	aspect := float64(w) / float64(h)
	arrondi := func(n float64, cle func(int) float64) int {
		a, b := int(math.Floor(n)), int(math.Ceil(n))
		r := a
		if cle(b) < cle(a) {
			r = b
		}
		return max(r, 1)
	}
	x, y := l, H
	if float64(x)/float64(y) >= aspect {
		x = arrondi(float64(y)*aspect, func(n int) float64 { return math.Abs(aspect - float64(n)/float64(y)) })
	} else {
		y = arrondi(float64(x)/aspect, func(n int) float64 {
			if n == 0 {
				return 0
			}
			return math.Abs(aspect - float64(x)/float64(n))
		})
	}
	return x, y, true
}

func ecrireTexte(im *image.RGBA, x, y int, texte string, c color.RGBA) {
	for _, r := range texte {
		if r < 32 || r > 126 {
			r = '?'
		}
		g := policePlanches[r-32]
		for dy := 0; dy < 11; dy++ {
			for dx := 0; dx < 6; dx++ {
				if g[dy]&(1<<(5-dx)) != 0 {
					im.SetRGBA(x+dx, y+dy, c)
				}
			}
		}
		x += 6
	}
}

// planches: the screenshots to look at, 4 per sheet, each with its name (.athanor/sheets/planche_N.png).
func planches(etat string, fichiers []string) []string {
	dossier := joindrePur(etat, "sheets")
	if existePy(dossier) {
		os.RemoveAll(dossier)
	}
	if len(fichiers) == 0 {
		return nil
	}
	_ = os.MkdirAll(dossier, 0o777)
	var sorties []string
	const L, H, marge = 640, 360, 4
	for i := 0; i < len(fichiers); i += 4 {
		lot := fichiers[i:min(i+4, len(fichiers))]
		cols := 1
		if len(lot) > 1 {
			cols = 2
		}
		lignes := (len(lot) + 1) / 2
		p := image.NewRGBA(image.Rect(0, 0, cols*(L+marge)+marge, lignes*(H+22+marge)+marge))
		fond := color.RGBA{24, 24, 24, 255}
		for k := 0; k < len(p.Pix); k += 4 {
			p.Pix[k], p.Pix[k+1], p.Pix[k+2], p.Pix[k+3] = fond.R, fond.G, fond.B, 255
		}
		for j, f := range lot {
			im, err := lireRGB(f)
			if err != nil {
				continue
			}
			if l, h, ok := tailleVignette(im.l, im.h, L, H); ok {
				im = redimensionner(im, l, h)
			}
			x := marge + (j%2)*(L+marge)
			y := marge + (j/2)*(H+22+marge)
			ecrireTexte(p, x+2, y+4, path.Base(filepath.ToSlash(f)), color.RGBA{235, 235, 235, 255})
			for yy := 0; yy < im.h; yy++ {
				for xx := 0; xx < im.l; xx++ {
					c := im.at(xx, yy)
					p.SetRGBA(x+xx, y+20+yy, color.RGBA{c[0], c[1], c[2], 255})
				}
			}
		}
		sortie := joindrePur(dossier, fmt.Sprintf("planche_%d.png", i/4+1))
		var b bytes.Buffer
		if png.Encode(&b, p) == nil {
			_ = os.WriteFile(sortie, b.Bytes(), 0o666)
		}
		sorties = append(sorties, sortie)
	}
	return sorties
}

func captures(racine string, args []string) string {
	etat := joindrePur(racine, ".athanor")
	ref := joindrePur(etat, "screenshots_ref")
	valider := false
	var noms []string
	for _, a := range args {
		if a == "--accept" {
			valider = true
		}
		if !strings.HasPrefix(a, "--") {
			noms = append(noms, a)
		}
	}
	dossier := joindrePur(etat, "screenshots")
	if len(noms) > 0 {
		dossier = joindrePur(racine, noms[0])
	}
	images := pngsDe(dossier)
	nom := func(p string) string { return path.Base(filepath.ToSlash(p)) }
	if valider {
		_ = os.MkdirAll(ref, 0o777)
		for _, v := range pngsDe(ref) {
			os.Remove(v)
		}
		for _, p := range images {
			if st, err := os.Stat(p); err == nil {
				if b, err := os.ReadFile(p); err == nil {
					cible := joindrePur(ref, nom(p))
					_ = os.WriteFile(cible, b, 0o666)
					_ = os.Chmod(cible, st.Mode().Perm())
					_ = os.Chtimes(cible, time.Now(), st.ModTime())
				}
			}
		}
		return fmt.Sprintf("%d screenshots saved as the reference.", len(images))
	}
	if len(images) == 0 {
		return "No .png screenshot in " + cheminRelatifPur(dossier, racine) + "."
	}
	type raison struct{ p, r string }
	var nouvelles []string
	var changees, suspectes []raison
	identiques := 0
	for _, p := range images {
		s, err := signatureImage(p)
		if err != nil {
			suspectes = append(suspectes, raison{p, "unreadable image (" + err.Error() + ")"})
			continue
		}
		if et := ecartType(s.petit); et < seuilUniforme {
			suspectes = append(suspectes, raison{p, fmt.Sprintf("almost uniform image (standard deviation %.1f): empty or black screen?", et)})
		}
		r := joindrePur(ref, nom(p))
		if !existePy(r) {
			nouvelles = append(nouvelles, p)
			continue
		}
		sr, err := signatureImage(r)
		if err != nil {
			nouvelles = append(nouvelles, p)
			continue
		}
		if s.l != sr.l || s.h != sr.h {
			changees = append(changees, raison{p, fmt.Sprintf("size %dx%d -> %dx%d", sr.l, sr.h, s.l, s.h)})
			continue
		}
		if e := ecartImages(s.petit, sr.petit); e >= seuilChangement {
			changees = append(changees, raison{p, fmt.Sprintf("%.1f %% difference", e)})
		} else {
			identiques++
		}
	}
	var disparues []string
	if estDossier(ref) {
		for _, r := range pngsDe(ref) {
			if !existePy(joindrePur(dossier, nom(r))) {
				disparues = append(disparues, nom(r))
			}
		}
		sort.Strings(disparues)
	}
	out := []string{fmt.Sprintf("Screenshots: %d | identical to the reference: %d | changed: %d | new: %d | suspicious: %d",
		len(images), identiques, len(changees), len(nouvelles), len(suspectes))}
	for _, s := range suspectes {
		out = append(out, fmt.Sprintf("  SUSPICIOUS  %s: %s", nom(s.p), s.r))
	}
	for _, c := range changees {
		out = append(out, fmt.Sprintf("  changed     %s (%s)", nom(c.p), c.r))
	}
	for _, p := range nouvelles {
		out = append(out, "  new         "+nom(p))
	}
	for _, n := range disparues {
		out = append(out, "  gone        "+n)
	}
	var aVoir []string
	vu := map[string]bool{}
	ajouter := func(p string) {
		if !vu[p] {
			vu[p] = true
			aVoir = append(aVoir, p)
		}
	}
	for _, s := range suspectes {
		ajouter(s.p)
	}
	for _, c := range changees {
		ajouter(c.p)
	}
	for _, p := range nouvelles {
		ajouter(p)
	}
	if sorties := planches(etat, aVoir); len(sorties) > 0 {
		var rel []string
		for _, s := range sorties {
			rel = append(rel, cheminRelatifPur(s, racine))
		}
		out = append(out, "To look at (4 screenshots per sheet): "+strings.Join(rel, ", "))
	} else {
		out = append(out, "Nothing new to look at: all screenshots are identical to the reference.")
	}
	if !estDossier(ref) {
		out = append(out, "(No reference yet: it is saved after each successful cycle.)")
	}
	return strings.Join(out, "\n")
}

// ============================================================================ map

var extensionsCode = map[string]bool{".gd": true, ".gdshader": true, ".py": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true,
	".tsx": true, ".jsx": true, ".vue": true, ".svelte": true, ".rs": true, ".go": true, ".java": true, ".kt": true, ".cs": true,
	".cpp": true, ".cc": true, ".c": true, ".h": true, ".hpp": true, ".rb": true, ".php": true, ".lua": true, ".sh": true,
	".glsl": true, ".frag": true, ".vert": true, ".wgsl": true, ".swift": true, ".dart": true, ".sql": true}

// Python's \s and \w (Unicode) in the same patterns as athanor.py.
const blancPy = `[\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`
const motPy = `[\p{L}\p{N}_]`

var (
	reDefinition = regexp.MustCompile(strings.NewReplacer(`[\w.]`, `[\p{L}\p{N}_.]`, `\s`, blancPy, `\w`, motPy).Replace(
		`^\s*(?:export\s+)?(?:default\s+)?(?:pub(?:\(\w+\))?\s+)?(?:static\s+)?(?:async\s+)?` +
			`(?:func|def|function|fn|class|class_name|struct|interface|enum|signal|trait|impl|type)\s+([A-Za-z_][\w.]*)` +
			`|^\s*(?:export\s+)?const\s+([A-Za-z_]\w*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_]\w*)\s*=>` +
			`|^\s*(?:uniform)\s+\w+\s+([A-Za-z_]\w*)` +
			`|^\s*(?:void|vec[234]|float|int|bool|mat[234])\s+([A-Za-z_]\w*)\s*\(`))
	reTitreMD = regexp.MustCompile(`^(#{1,3})` + blancPy + `+(.*)`)
)

const tailleMaxCarte = 400_000

// suffixePur: PurePath(p).suffix.
func suffixePur(p string) string {
	n := path.Base(filepath.ToSlash(p))
	i := strings.LastIndexByte(n, '.')
	if i <= 0 || i == len(n)-1 {
		return ""
	}
	return n[i:]
}

func fichiersSuivis(racine string) []string {
	c := exec.Command("git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false",
		"-C", racine, "ls-files", "-co", "--exclude-standard")
	var sortie bytes.Buffer
	c.Stdout = &sortie
	err := c.Start()
	if err == nil {
		fini := make(chan error, 1)
		go func() { fini <- c.Wait() }()
		select {
		case <-fini:
		case <-time.After(60 * time.Second):
			_ = c.Process.Kill()
			<-fini
			err = os.ErrDeadlineExceeded
		}
	}
	vu := map[string]bool{}
	var out []string
	if err == nil {
		for _, l := range strings.Split(sortie.String(), "\n") {
			if l != "" && !vu[l] {
				vu[l] = true
				out = append(out, l)
			}
		}
	} else { // git missing or too slow (a folder that is not a repository gives nothing, as before): every file
		_ = filepath.WalkDir(racine, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && estFichier(p) {
				r, _ := filepath.Rel(racine, p)
				out = append(out, filepath.ToSlash(r))
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

type entreeCarte struct {
	rel    string
	n      int
	elems  []string
	defini bool
}

func carte(racine string) string {
	etat := joindrePur(racine, ".athanor")
	var code, docs []entreeCarte
	var atelier []string
	if d := joindrePur(etat, "docs"); estDossier(d) {
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && e.Type()&os.ModeSymlink == 0 && strings.HasSuffix(e.Name(), ".md") && e.Type().IsRegular() {
				r, _ := filepath.Rel(racine, p)
				atelier = append(atelier, filepath.ToSlash(r))
			}
			return nil
		})
		sort.Strings(atelier)
	}
	vu := map[string]bool{}
	for _, rel := range append(fichiersSuivis(racine), atelier...) {
		if vu[rel] {
			continue
		}
		vu[rel] = true
		ignore := false
		for _, pre := range []string{".athanor/", ".pilote/", ".git/", ".claude/", "node_modules/", ".godot/", ".verif/"} {
			ignore = ignore || strings.HasPrefix(rel, pre)
		}
		if ignore && !strings.HasPrefix(rel, ".athanor/docs/") {
			continue
		}
		p := joindrePur(racine, rel)
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() > tailleMaxCarte {
			continue
		}
		ext := strings.ToLower(suffixePur(rel))
		if !extensionsCode[ext] && ext != ".md" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lignes := strings.Split(texteUniverselPython(b), "\n")
		if ext == ".md" {
			var titres []string
			dansCode := false
			for i, l := range lignes {
				if strings.HasPrefix(lstripPython(l), "```") {
					dansCode = !dansCode
				} else if m := reTitreMD.FindStringSubmatch(l); !dansCode && m != nil {
					titres = append(titres, fmt.Sprintf("%s %s (%d)", m[1], stripPython(m[2]), i+1))
				}
			}
			docs = append(docs, entreeCarte{rel, len(lignes), titres, false})
		} else {
			var defs []string
			for i, l := range lignes {
				if m := reDefinition.FindStringSubmatch(l); m != nil {
					for _, g := range m[1:] {
						if g != "" {
							defs = append(defs, fmt.Sprintf("%s %d", g, i+1))
							break
						}
					}
				}
			}
			code = append(code, entreeCarte{rel, len(lignes), defs, true})
		}
	}
	_ = os.MkdirAll(etat, 0o777)
	var b strings.Builder
	b.WriteString("# Project map (written by the pilot before each cycle, do not edit)\n\n")
	b.WriteString("Format: `path (lines): name line, name line...`. Search with `grep -n word .athanor/MAP.md`,\nthen read only the useful lines of the file (Read with offset and limit).\n\n")
	b.WriteString("## Documents (headings and line numbers)\n\n")
	for _, d := range docs {
		t := "no heading"
		if len(d.elems) > 0 {
			t = strings.Join(d.elems, " · ")
		}
		fmt.Fprintf(&b, "- `%s` (%d): %s\n", d.rel, d.n, t)
	}
	b.WriteString("\n## Code (definitions and line numbers)\n\n")
	for _, c := range code {
		fmt.Fprintf(&b, "- `%s` (%d)", c.rel, c.n)
		if len(c.elems) > 0 {
			b.WriteString(": " + strings.Join(c.elems, ", "))
		}
		b.WriteString("\n")
	}
	sortie := joindrePur(etat, "MAP.md")
	if err := os.WriteFile(sortie, []byte(b.String()), 0o666); err != nil {
		return "Map not written: " + err.Error()
	}
	return fmt.Sprintf("Map written: %s (%d code files, %d documents)", cheminRelatifPur(sortie, racine), len(code), len(docs))
}

// ============================================================================ archive

const (
	garderJalons, garderEntrees, seuilPlan, seuilJournal = 3, 6, 6, 10
	notePlan                                             = "> Older completed milestones: `.athanor/archives/PLAN_TERMINE.md`."
	noteJournal                                          = "> Older entries: `.athanor/archives/JOURNAL_ANCIEN.md`."
)

var reJalonPlan = regexp.MustCompile(`^- \[([ xX])\] `)
var reJalonOuvert = regexp.MustCompile(`^- \[ \] `)

func ecrireArchive(chemin, titre, texte string) error {
	_ = os.MkdirAll(filepath.Dir(chemin), 0o777)
	debut := ""
	if !existePy(chemin) {
		debut = "# " + titre + "\n\nArchived automatically by the pilot. Nothing in it is modified.\n\n"
	}
	f, err := os.OpenFile(chemin, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(debut + strings.TrimRight(texte, "\n") + "\n\n")
	return err
}

func insererNote(lignes []string, note string) []string {
	for _, l := range lignes {
		if stripPython(l) == note {
			return lignes
		}
	}
	for i, l := range lignes {
		if strings.HasPrefix(l, "# ") {
			return append(append(append([]string(nil), lignes[:i+1]...), "", note), lignes[i+1:]...)
		}
	}
	return append([]string{note, ""}, lignes...)
}

func compacter(lignes []string) []string {
	var out []string
	vides := 0
	for _, l := range lignes {
		if stripPython(l) == "" {
			vides++
		} else {
			vides = 0
		}
		if vides <= 1 {
			out = append(out, l)
		}
	}
	return out
}

// lireTexteStrict: a UTF-8 text file read as Python reads it (universal newlines); not UTF-8: an error.
func lireTexteStrict(chemin string) (string, error) {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("not UTF-8")
	}
	return texteUniverselPython(b), nil
}

func archiverPlan(etat string) string {
	plan := joindrePur(etat, "PLAN.md")
	if !existePy(plan) {
		return "PLAN.md missing"
	}
	t, err := lireTexteStrict(plan)
	if err != nil {
		return "PLAN.md: unreadable (" + err.Error() + "), nothing modified"
	}
	lignes := strings.Split(t, "\n")
	type bloc struct {
		d, f  int
		coche bool
	}
	var blocs []bloc
	for i := 0; i < len(lignes); {
		if m := reJalonPlan.FindStringSubmatch(lignes[i]); m != nil {
			j := i + 1
			for j < len(lignes) && (strings.HasPrefix(lignes[j], " ") || strings.HasPrefix(lignes[j], "\t")) && stripPython(lignes[j]) != "" {
				j++
			}
			blocs = append(blocs, bloc{i, j, m[1] != " "})
			i = j
		} else {
			i++
		}
	}
	var coches []bloc
	for _, b := range blocs {
		if b.coche {
			coches = append(coches, b)
		}
	}
	var aArchiver []bloc
	if len(coches) > garderJalons {
		aArchiver = coches[:len(coches)-garderJalons]
	}
	if len(aArchiver) < seuilPlan {
		return fmt.Sprintf("PLAN.md: %d checked milestones, nothing to archive", len(coches))
	}
	retires := map[int]bool{}
	var texte []string
	for _, b := range aArchiver {
		texte = append(append(texte, lignes[b.d:b.f]...), "")
		for k := b.d; k < b.f; k++ {
			retires[k] = true
		}
	}
	var gardees []string
	for k, l := range lignes {
		if !retires[k] {
			gardees = append(gardees, l)
		}
	}
	restant := compacter(insererNote(gardees, notePlan))
	ouverts := func(l []string) []string {
		var o []string
		for _, x := range l {
			if reJalonOuvert.MatchString(x) {
				o = append(o, x)
			}
		}
		return o
	}
	if strings.Join(ouverts(lignes), "\n") != strings.Join(ouverts(restant), "\n") || len(ouverts(lignes)) != len(ouverts(restant)) {
		return "PLAN.md: check failed, nothing modified"
	}
	if err := ecrireArchive(joindrePur(joindrePur(etat, "archives"), "PLAN_TERMINE.md"), "Completed milestones", strings.Join(texte, "\n")); err != nil {
		return "PLAN.md: archive not written, nothing modified"
	}
	_ = os.WriteFile(plan, []byte(strings.Join(restant, "\n")), 0o666)
	return fmt.Sprintf("PLAN.md: %d checked milestones archived", len(aArchiver))
}

func archiverJournal(etat string) string {
	journal := joindrePur(etat, "LOG.md")
	if !existePy(journal) {
		return "LOG.md missing"
	}
	t, err := lireTexteStrict(journal)
	if err != nil {
		return "LOG.md: unreadable (" + err.Error() + "), nothing modified"
	}
	lignes := strings.Split(t, "\n")
	var debuts []int
	for i, l := range lignes {
		if strings.HasPrefix(l, "## ") {
			debuts = append(debuts, i)
		}
	}
	if len(debuts) <= seuilJournal {
		return fmt.Sprintf("LOG.md: %d entries, nothing to archive", len(debuts))
	}
	coupe := debuts[len(debuts)-garderEntrees]
	entete, vieux, recent := lignes[:debuts[0]], lignes[debuts[0]:coupe], lignes[coupe:]
	if err := ecrireArchive(joindrePur(joindrePur(etat, "archives"), "JOURNAL_ANCIEN.md"), "Log: older entries", strings.Join(vieux, "\n")); err != nil {
		return "LOG.md: archive not written, nothing modified"
	}
	nouveau := compacter(append(append(insererNote(append([]string(nil), entete...), noteJournal), ""), recent...))
	_ = os.WriteFile(journal, []byte(strings.Join(nouveau, "\n")), 0o666)
	return fmt.Sprintf("LOG.md: %d entries archived", len(debuts)-garderEntrees)
}

func lancerAtelier(nom string) func([]string) int {
	return func(args []string) int {
		racine := projetAtelier()
		switch nom {
		case "screenshots":
			fmt.Println(captures(racine, args))
		case "map":
			fmt.Println(carte(racine))
		case "archive":
			etat := joindrePur(racine, ".athanor")
			fmt.Println(archiverPlan(etat))
			fmt.Println(archiverJournal(etat))
		}
		return 0
	}
}
