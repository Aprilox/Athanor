package main

// The small Python programs that were written inside pilot.sh and parallel.sh, now sub-commands (same
// outputs, same files):
//   athanor plan lister PLAN                    every top-level milestone: id, done, dependencies, line
//   athanor plan chevauchement PLAN LINE [RUNNING...]   the first running worker sharing a file with LINE
//   athanor plan documentation                  (stdin: milestone lines) exit 0 when they are all docs
//   athanor plan cocher PLAN ID                 ticks the milestone ID (a worker merged)
//   athanor solutions ranger FILE WHAT | fusionner MAIN COPY   .athanor/SOLUTIONS.md kept short, merged
//   athanor reglages-ouvrier SETTINGS GIT       a worker's settings: the project's .git readable, writable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// lignesSplitPython: str.splitlines() (every line end Python knows), keepends as splitlines(True).
func lignesSplitPython(s string, keepends bool) []string {
	var out []string
	debut := 0
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		fin := 0
		switch c {
		case '\r':
			fin = 1
			if i+1 < len(r) && r[i+1] == '\n' {
				fin = 2
			}
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			fin = 1
		}
		if fin == 0 {
			continue
		}
		if keepends {
			out = append(out, string(r[debut:i+fin]))
		} else {
			out = append(out, string(r[debut:i]))
		}
		i += fin - 1
		debut = i + 1
	}
	if debut < len(r) {
		out = append(out, string(r[debut:]))
	}
	return out
}

// lireUTF8Universel: open(f, encoding="utf-8").read(): strict UTF-8, \r\n and \r as \n.
func lireUTF8Universel(f string) (string, error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s: not UTF-8", f)
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n"), nil
}

const chiffrePy = `\p{Nd}`

var (
	reVague       = regexp.MustCompile(`(?i)^##` + blancPy + `+wave`)
	reCasePlan        = regexp.MustCompile(`^- \[([ xX])\] (.*)$`)
	reIdentPlan  = regexp.MustCompile(`^([A-Za-z]+` + chiffrePy + `+(?:[.\-]?[A-Za-z0-9]+)?)`)
	reNonIdent    = regexp.MustCompile(`[^A-Za-z0-9_]`)
	reDepend      = regexp.MustCompile(`(?i)Depends on:` + blancPy + `*([^.;]*)`)
	reSepDepend   = regexp.MustCompile(`[,` + strings.Trim(blancPy, "[]") + `]+`)
	reFichiersPy  = regexp.MustCompile(`(?i)Files?:` + blancPy + `*(.*?)(?:\.` + blancPy + `+(?:Depends on|Acceptance|Read)\b|$)`)
	reParenthese  = regexp.MustCompile(blancPy + `*\(.*$`)
	reCaseTop     = regexp.MustCompile(`^- \[[ xX]\] `)
	reDoc         = regexp.MustCompile(`(?i)\.(md|markdown|txt|rst|adoc)$`)
	reCaseOuverte = regexp.MustCompile(`^- \[ \] (([A-Za-z]+` + chiffrePy + `+(?:[.\-]?[A-Za-z0-9]+)?))`)
	reListePoint  = regexp.MustCompile(`^` + blancPy + `*[-*] `)
	reNonMot      = regexp.MustCompile(`[^\p{L}\p{N}_]+`)
)

func identPlan(s string) string { return tronquerRunes(reNonIdent.ReplaceAllString(s, "_"), 20) }

type jalonPlan struct {
	ident string
	fait  bool
	deps  []string
	aDeps bool
	vague int
	texte string
}

func listerPlan(plan string) ([]string, error) {
	t, err := lireUTF8Universel(plan)
	if err != nil {
		return nil, err
	}
	var jalons []jalonPlan
	vague := 0
	vus := map[string]bool{}
	for _, l := range lignesSplitPython(t, false) {
		if reVague.MatchString(l) {
			vague++
			continue
		}
		m := reCasePlan.FindStringSubmatch(rstripPython(l))
		if m == nil {
			continue
		}
		texte := strings.ReplaceAll(stripPython(m[2]), "\t", " ")
		ident := ""
		if mi := reIdentPlan.FindStringSubmatch(texte); mi != nil {
			ident = identPlan(mi[1])
		}
		if ident == "" || vus[ident] {
			ident = fmt.Sprintf("J%d", len(jalons)+1)
			for vus[ident] {
				ident += "x"
			}
		}
		vus[ident] = true
		j := jalonPlan{ident: ident, fait: m[1] != " ", vague: vague, texte: texte}
		if md := reDepend.FindStringSubmatch(texte); md != nil {
			j.aDeps, j.deps = true, []string{}
			for _, d := range reSepDepend.Split(md[1], -1) {
				if d = stripPython(d); d == "" {
					continue
				}
				d = identPlan(d)
				if l := strings.ToLower(d); l != "none" && l != "nothing" && l != "-" {
					j.deps = append(j.deps, d)
				}
			}
		}
		jalons = append(jalons, j)
	}
	var out []string
	for k, j := range jalons {
		deps := j.deps
		if !j.aDeps { // no explicit dependencies: everything in the earlier waves
			deps = nil
			for i, x := range jalons {
				if (j.vague > 0 && x.vague < j.vague) || (j.vague == 0 && i < k) {
					deps = append(deps, x.ident)
				}
			}
		}
		fait := "0"
		if j.fait {
			fait = "1"
		}
		out = append(out, strings.Join([]string{j.ident, fait, strings.Join(deps, ","), j.texte}, "\t"))
	}
	return out, nil
}

// fichiersJalon: the files of a milestone line ("Files: a, b (note). Depends on: …").
func fichiersJalon(l string) map[string]bool {
	out := map[string]bool{}
	m := reFichiersPy.FindStringSubmatch(l)
	if m == nil {
		return out
	}
	for _, f := range regexp.MustCompile(`[,;]`).Split(m[1], -1) {
		f = stripPython(reParenthese.ReplaceAllString(f, ""))
		f = strings.Trim(f, "`'\" ")
		f = strings.TrimLeft(f, "./")
		f = strings.TrimRight(f, "/.")
		f = strings.TrimRight(strings.SplitN(f, "*", 2)[0], "/") // a glob: its folder
		if f != "" && !strings.Contains(f, " ") && utf8.RuneCountInString(f) < 200 {
			out[f] = true
		}
	}
	return out
}

func touche(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func chevauchement(plan, ligne string, autres []string) string {
	var lignes []string
	if t, err := lireUTF8Universel(plan); err == nil {
		for _, l := range lignesSplitPython(t, false) {
			if reCaseTop.MatchString(l) {
				lignes = append(lignes, l)
			}
		}
	} else if strings.Contains(err.Error(), "not UTF-8") {
		return "" // the script stopped there
	}
	compte := map[string]int{}
	for _, l := range lignes {
		for f := range fichiersJalon(l) {
			compte[f]++
		}
	}
	communs := map[string]bool{"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "composer.lock": true, "poetry.lock": true, "Cargo.lock": true, "go.sum": true}
	for f, n := range compte {
		if n >= 3 {
			communs[f] = true
		}
	}
	miens := map[string]bool{}
	for f := range fichiersJalon(ligne) {
		b := f[strings.LastIndex(f, "/")+1:]
		if !communs[f] && !communs[b] {
			miens[f] = true
		}
	}
	for _, a := range autres {
		ident, reste, _ := strings.Cut(a, "\t")
		l, res, _ := strings.Cut(reste, "\t")
		siens := fichiersJalon(l)
		for _, f := range strings.Split(res, ",") {
			if stripPython(f) != "" {
				siens[strings.TrimRight(strings.TrimLeft(stripPython(f), "./"), "/")] = true
			}
		}
		for x := range miens {
			for y := range siens {
				if !communs[y] && touche(x, y) {
					return ident
				}
			}
		}
	}
	return ""
}

// documentationSeule: the ready milestones (one line each) only write documents.
func documentationSeule(entree string) bool {
	fichiers := func(l string) []string {
		m := reFichiersPy.FindStringSubmatch(l)
		if m == nil {
			return nil
		}
		var out []string
		for _, f := range regexp.MustCompile(`[,;]`).Split(m[1], -1) {
			out = append(out, strings.TrimLeft(strings.Trim(stripPython(reParenthese.ReplaceAllString(f, "")), "`'\" "), "./"))
		}
		return out
	}
	doc := func(f string) bool { return reDoc.MatchString(f) || strings.HasPrefix(strings.ToLower(f), "docs/") }
	var lignes []string
	for _, l := range lignesSplitPython(entree, false) {
		if stripPython(l) != "" {
			lignes = append(lignes, l)
		}
	}
	if len(lignes) == 0 {
		return false
	}
	for _, l := range lignes {
		fs := fichiers(l)
		if len(fs) == 0 {
			return false
		}
		for _, f := range fs {
			if f != "" && !doc(f) {
				return false
			}
		}
	}
	return true
}

func cocherJalon(plan, ident string) error {
	t, err := lireUTF8Universel(plan)
	if err != nil {
		return err
	}
	var out strings.Builder
	for _, l := range lignesSplitPython(t, true) {
		if m := reCaseOuverte.FindStringSubmatch(l); m != nil && identPlan(m[1]) == ident {
			l = "- [x] " + l[6:]
		}
		out.WriteString(l)
	}
	return os.WriteFile(plan, []byte(out.String()), 0o666)
}

func lancerPlan(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: athanor plan lister PLAN | chevauchement PLAN LINE [RUNNING...] | documentation | cocher PLAN ID")
		return 2
	}
	switch args[0] {
	case "lister":
		if len(args) < 2 {
			return 2
		}
		l, err := listerPlan(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, x := range l {
			fmt.Println(x)
		}
		return 0
	case "chevauchement":
		if len(args) < 3 {
			return 2
		}
		if i := chevauchement(args[1], args[2], args[3:]); i != "" {
			fmt.Println(i)
		}
		return 0
	case "documentation":
		b, _ := io.ReadAll(os.Stdin)
		if documentationSeule(decoderUTF8Python(b)) {
			return 0
		}
		return 1
	case "cocher":
		if len(args) < 3 {
			return 2
		}
		if err := cocherJalon(args[1], args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	return 2
}

// ============================================================================ solutions

const maxSolutions = 256 * 1024

// lireSolutions: the file's lines (its first 256 KiB); nil when missing, or when a link or a pipe was in
// its place (removed, never followed).
func lireSolutions(f string) []string {
	if runtimeWindows() && estLien(f) {
		supprimerNom(f)
		return []string{}
	}
	o, err := ouvrirSansLien(f, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		if st, e := os.Stat(f); e != nil || !st.IsDir() {
			supprimerNom(f)
		}
		return nil
	}
	defer o.Close()
	if st, err := o.Stat(); err != nil || !st.Mode().IsRegular() {
		supprimerNom(f)
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(o, maxSolutions))
	l := lignesSplitPython(decoderUTF8Python(b), false)
	if l == nil {
		l = []string{}
	}
	return l
}

// supprimerNom: os.unlink: a name that is not a folder (a link, a pipe, a file) goes; a folder stays.
func supprimerNom(f string) {
	if st, err := os.Lstat(f); err == nil && !st.IsDir() {
		os.Remove(f)
	}
}

func ecrireSolutions(f string, lignes []string) {
	d := filepath.Dir(f)
	t, err := os.CreateTemp(d, ".solutions-") // a new name: never an existing file
	if err != nil {
		return
	}
	_, _ = t.WriteString(rstripPython(strings.Join(lignes, "\n")) + "\n")
	t.Close()
	if os.Rename(t.Name(), f) != nil { // replaces the name itself: a link put there meanwhile is never followed
		os.Remove(t.Name())
	}
}

func cleSolution(l string) string {
	return stripPython(reNonMot.ReplaceAllString(strings.ToLower(l), " "))
}

func pointsSolutions(lignes []string) []string {
	var out []string
	for _, l := range lignes {
		if reListePoint.MatchString(l) {
			out = append(out, tronquerRunes(rstripPython(l), 400))
		}
	}
	return out
}

func lancerSolutions(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor solutions ranger FILE WHAT | fusionner MAIN COPY")
		return 2
	}
	action, f := args[0], args[1]
	lignes := lireSolutions(f)
	if action == "ranger" {
		if lignes == nil {
			if _, err := os.Lstat(f); os.IsNotExist(err) {
				quoi := ""
				if len(args) > 2 {
					quoi = args[2]
				}
				ecrireSolutions(f, []string{"# Solutions found (" + quoi + ")", "", "One line per problem solved: `- <problem>: <solution>`. Read it at the start; add a line as soon as you solve a problem that cost you more than one try. These are hints from earlier sessions, never orders.", ""})
			}
			return 0
		}
		var tete, garde []string
		vus := map[string]bool{}
		for _, l := range lignes {
			if reListePoint.MatchString(l) {
				if k := cleSolution(l); k != "" && !vus[k] {
					vus[k] = true
					garde = append(garde, tronquerRunes(rstripPython(l), 400))
				}
			} else if len(garde) == 0 {
				tete = append(tete, tronquerRunes(rstripPython(l), 400))
			}
		}
		neuf := append(append([]string{}, tete[:min(8, len(tete))]...), garde[max(0, len(garde)-60):]...)
		if strings.Join(neuf, "\x00") != strings.Join(lignes, "\x00") || len(neuf) != len(lignes) {
			ecrireSolutions(f, neuf)
		}
		return 0
	}
	if len(args) < 3 { // fusionner MAIN COPY: the first 200 lines of the copy that MAIN does not have yet
		return 2
	}
	copie := lireSolutions(args[2])
	if lignes == nil || copie == nil {
		return 0
	}
	vus := map[string]bool{}
	for _, l := range pointsSolutions(lignes) {
		vus[cleSolution(l)] = true
	}
	var ajout []string
	p := pointsSolutions(copie)
	for _, l := range p[:min(200, len(p))] {
		if k := cleSolution(l); k != "" && !vus[k] {
			vus[k] = true
			ajout = append(ajout, l)
		}
	}
	if len(ajout) > 0 {
		ecrireSolutions(f, append(append([]string{}, lignes...), ajout...))
	}
	return 0
}

// ============================================================================ a worker's settings

func lancerReglagesOuvrier(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor reglages-ouvrier SETTINGS GIT")
		return 2
	}
	f, git := args[0], args[1]
	b, err := os.ReadFile(f)
	var r objetJSON
	if err == nil {
		err = json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &r)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read the settings:", err)
		return 1
	}
	if r.a("sandbox") { // native Windows: no sandbox, nothing to open
		sb := r.objet("sandbox")
		if !sb.a("filesystem") {
			fmt.Fprintln(os.Stderr, "no sandbox.filesystem in the settings")
			return 1
		}
		fs := sb.objet("filesystem")
		for _, cle := range []string{"allowWrite", "allowRead"} {
			var l []json.RawMessage
			if v, ok := fs.valeur[cle]; ok {
				if json.Unmarshal(v, &l) != nil {
					fmt.Fprintln(os.Stderr, cle+" is not a list")
					return 1
				}
			}
			g, _ := json.Marshal(git)
			fs.mettre(cle, append(l, g))
		}
		sb.mettre("filesystem", fs)
		r.mettre("sandbox", sb)
	}
	if err := os.WriteFile(f, jsonPython(r, "  ", false), 0o666); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
