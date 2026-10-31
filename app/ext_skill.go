package main

// A skill tested and fixed on this computer. Its test reads what Claude will run: SKILL.md's header, and
// each script (bash, Python, Node…): is its program here, does it parse, is it written for another system
// (Windows line endings, GNU-only options on a Mac, python where only python3 exists…). "Fix with
// Claude" lets Claude Code rewrite a copy of the skill so that it runs everywhere (Windows' Git Bash, macOS,
// Linux) and does the same; the copy is tested again and shown as a diff before anything is replaced.
// Applying keeps the skill as it was (Undo) and writes a report for everyone (ext_corrections.go).
//
//   athanor extensions tester-skill KIT ID     JSON
//   athanor extensions corriger-skill KIT ID   "ETAPE …" lines, then JSON (the proposal, in tools/corrections/)
//   athanor extensions appliquer-skill KIT ID  the proposal put in place
//   athanor extensions annuler-skill KIT ID    the skill as it was before

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProblemeSkill: what would fail (Grave) or may fail on another system.
type ProblemeSkill struct {
	Fichier string   `json:"fichier"`
	Message string   `json:"message"` // in English
	Cle     string   `json:"cle"`     // the same, as the interface translates it ({0}, {1}…)
	Args    []string `json:"args,omitempty"`
	Grave   bool     `json:"grave"`
}

// TestSkill: the test of a skill.
type TestSkill struct {
	OK        bool            `json:"ok"`
	Scripts   int             `json:"scripts"`
	Problemes []ProblemeSkill `json:"problemes"`
	Erreur    string          `json:"erreur,omitempty"`
}

// CorrectionSkill: Claude's proposal for a skill, tested.
type CorrectionSkill struct {
	OK          bool            `json:"ok"`   // the copy passes the test
	Deja        bool            `json:"deja"` // nothing to fix
	Explication string          `json:"explication,omitempty"`
	Manque      string          `json:"manque,omitempty"`
	Athanor     string          `json:"athanor,omitempty"`
	Fichiers    []string        `json:"fichiers,omitempty"` // the files changed
	Diff        string          `json:"diff,omitempty"`
	Avant       []ProblemeSkill `json:"avant,omitempty"`
	Apres       []ProblemeSkill `json:"apres,omitempty"`
	Erreur      string          `json:"erreur,omitempty"`
	Systeme     string          `json:"systeme"`
}

func dossierSkill(kit, id string) string { return filepath.Join(kit, "extensions", "skills", id) }
func propositionSkill(kit, id string) string {
	return filepath.Join(kit, "tools", "corrections", "skill-"+id)
}
func sauvegardeSkill(kit, id string) string {
	return filepath.Join(kit, "tools", "corrections", "avant-skill-"+id)
}

var reShebang = regexp.MustCompile(`^#!\s*(\S+)(?:\s+(?:-S\s+)?(\S+))?`)

// interpreteScript: the program a script needs ("" when it is not one Claude runs).
func interpreteScript(nom string, debut []byte) string {
	if m := reShebang.FindSubmatch(bytes.SplitN(debut, []byte("\n"), 2)[0]); m != nil {
		p := strings.TrimRight(string(m[1]), "\r")
		if filepath.Base(p) == "env" && len(m[2]) > 0 {
			return strings.TrimRight(string(m[2]), "\r")
		}
		return p
	}
	switch strings.ToLower(filepath.Ext(nom)) {
	case ".sh", ".bash":
		return "bash"
	case ".py":
		return "python3"
	case ".js", ".mjs", ".cjs":
		return "node"
	case ".ts":
		return "npx"
	case ".rb":
		return "ruby"
	case ".pl":
		return "perl"
	case ".php":
		return "php"
	case ".ps1":
		return "pwsh"
	case ".bat", ".cmd":
		return "cmd"
	}
	return ""
}

// cheminProgrammes: the PATH Claude's commands find their programs on (the user's, and the home's toolchains).
func cheminProgrammes() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PATH")
	}
	return cheminCage(os.Getenv("PATH"), os.Getenv("HOME"), dossierExiste)
}

// trouverProgramme: a program as Claude's commands find it here ("" when missing); on Windows, python3 is
// often only python (or py), and bash is Git Bash's.
func trouverProgramme(nom string) string {
	if filepath.IsAbs(nom) {
		if st, err := os.Stat(nom); err == nil && !st.IsDir() {
			return nom
		}
		return ""
	}
	ancien := os.Getenv("PATH")
	os.Setenv("PATH", cheminProgrammes())
	defer os.Setenv("PATH", ancien)
	if p, err := exec.LookPath(nom); err == nil {
		return p
	}
	if runtime.GOOS == "windows" && nom == "bash" {
		return trouverBashNatifPilote()
	}
	if runtime.GOOS == "windows" && nom == "pwsh" {
		if p, err := exec.LookPath("powershell"); err == nil {
			return p
		}
	}
	return ""
}

func executerCourt(p string, args ...string) (string, error) {
	ctx, fin := context.WithTimeout(context.Background(), 15*time.Second)
	defer fin()
	c := exec.CommandContext(ctx, p, args...)
	cacherFenetre(c)
	b, err := c.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

var (
	reCommandePython = regexp.MustCompile("(?m)(?:^|[\\s`(])(python3?|py)\\s+[^\\s`]+\\.py")
	reSedSansSuffixe = regexp.MustCompile(`\bsed\s+(?:-[a-zA-Z]*\s+)*-i\s+(?:-e\s+)?['"]`)
	reGrepPerl       = regexp.MustCompile(`\bgrep\s+(?:-[a-zA-Z]*P|-P)`)
	reReadlinkF      = regexp.MustCompile(`\breadlink\s+-f\b`)
	reDateGNU        = regexp.MustCompile(`\bdate\s+(?:-[a-zA-Z]+\s+)*-d\b|\bdate\s+--date\b`)
	reCheminWindows  = regexp.MustCompile(`(?i)\b[A-Z]:\\[^\s"'` + "`" + `]+`)
)

// testerDossierSkill: the test of the skill in folder d.
func testerDossierSkill(d string) TestSkill {
	r := TestSkill{Problemes: []ProblemeSkill{}}
	pb := func(f, cle string, grave bool, args ...string) {
		m := cle
		for i, a := range args {
			m = strings.ReplaceAll(m, "{"+strconv.Itoa(i)+"}", a)
		}
		r.Problemes = append(r.Problemes, ProblemeSkill{Fichier: f, Message: m, Cle: cle, Args: args, Grave: grave})
	}
	texte, err := os.ReadFile(filepath.Join(d, "SKILL.md"))
	if err != nil {
		r.Erreur = "no SKILL.md"
		return r
	}
	if nom, desc := enTete(string(texte)); nom == "" || desc == "" {
		pb("SKILL.md", "its header (--- name: … description: … ---) lacks the name or the description: Claude never picks the skill", true)
	}
	// the programs SKILL.md runs
	vu := map[string]bool{}
	for _, m := range reCommandePython.FindAllStringSubmatch(string(texte), -1) {
		p := m[1]
		if vu[p] {
			continue
		}
		vu[p] = true
		if trouverProgramme(p) == "" {
			autre := map[string]string{"python": "python3", "python3": "python", "py": "python3"}[p]
			if autre != "" && trouverProgramme(autre) != "" {
				pb("SKILL.md", "it runs “{0} …”: this computer has {1}, not {0}", true, p, autre)
			} else {
				pb("SKILL.md", "it runs “{0} …”: Python is not on this computer (Setup & health)", true, p)
			}
		}
	}
	scripts := 0
	_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || scripts >= 60 {
			if e != nil && e.IsDir() && (e.Name() == "node_modules" || e.Name() == ".git" || e.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(d, p)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil || len(b) > 2<<20 {
			return nil
		}
		prog := interpreteScript(p, b)
		if prog == "" {
			return nil
		}
		scripts++
		base := filepath.Base(prog)
		texteScript := string(b)
		if bytes.Contains(b, []byte("\r\n")) && (strings.HasPrefix(texteScript, "#!") || base == "bash" || base == "sh") {
			pb(rel, "Windows line endings (CRLF): bash and the #! line fail on macOS and Linux", runtime.GOOS != "windows")
		}
		if base == "cmd" {
			pb(rel, "a Windows script (.bat/.cmd): it cannot run on macOS or Linux", runtime.GOOS != "windows")
			return nil
		}
		chemin := trouverProgramme(prog)
		if chemin == "" && filepath.IsAbs(prog) { // #!/usr/local/bin/python3: the same program elsewhere?
			if ailleurs := trouverProgramme(base); ailleurs != "" {
				pb(rel, "its #! line asks for {0}, which is not here ({1} is at {2}): write #!/usr/bin/env {1}", true, prog, base, ailleurs)
				chemin = ailleurs
			}
		}
		if chemin == "" {
			if autre := map[string]string{"python3": "python", "python": "python3"}[base]; autre != "" && trouverProgramme(autre) != "" {
				pb(rel, "it needs {0}: this computer has {1} only", true, base, autre)
			} else {
				pb(rel, "it needs {0}, not found on this computer", true, base)
			}
			return nil
		}
		// does it parse here
		switch {
		case base == "bash" || base == "sh":
			if out, err := executerCourt(chemin, "-n", p); err != nil {
				pb(rel, "bash cannot read it: {0}", true, court(derniereLigneTexte(out), 200))
			}
		case strings.HasPrefix(base, "python"):
			if out, err := executerCourt(chemin, "-c", "import ast,sys;ast.parse(open(sys.argv[1],encoding='utf-8').read(),sys.argv[1])", p); err != nil {
				pb(rel, "Python cannot read it: {0}", true, court(derniereLigneTexte(out), 200))
			}
		case base == "node" && strings.ToLower(filepath.Ext(p)) != ".ts":
			if out, err := executerCourt(chemin, "--check", p); err != nil {
				pb(rel, "Node cannot read it: {0}", true, court(derniereLigneTexte(out), 200))
			}
		}
		// written for one system only
		if base == "bash" || base == "sh" {
			mac := runtime.GOOS == "darwin"
			if reSedSansSuffixe.MatchString(texteScript) {
				pb(rel, "“sed -i” without a suffix: fails with macOS' sed (write “sed -i.bak … && rm ….bak”)", mac)
			}
			if reGrepPerl.MatchString(texteScript) {
				pb(rel, "“grep -P”: macOS' grep does not have it", mac)
			}
			if reReadlinkF.MatchString(texteScript) {
				pb(rel, "“readlink -f”: missing on older macOS", false)
			}
			if reDateGNU.MatchString(texteScript) {
				pb(rel, "“date -d”: GNU only, fails on macOS", mac)
			}
		}
		if reCheminWindows.MatchString(texteScript) && base != "pwsh" {
			pb(rel, "a Windows path (C:\\…) written in it", runtime.GOOS != "windows")
		}
		return nil
	})
	r.Scripts = scripts
	r.OK = true
	for _, x := range r.Problemes {
		r.OK = r.OK && !x.Grave
	}
	return r
}

// testerSkill: athanor extensions tester-skill KIT ID.
func testerSkill(kit, id string) TestSkill {
	if !reIdSur.MatchString(id) {
		return TestSkill{Erreur: "invalid id", Problemes: []ProblemeSkill{}}
	}
	d := dossierSkill(kit, id)
	if !estDossierSansLien(d) {
		return TestSkill{Erreur: "unknown skill", Problemes: []ProblemeSkill{}}
	}
	return testerDossierSkill(d)
}

// demanderClaudeDossier: Claude Code working in folder d (its files only, and the web); its last answer.
var demanderClaudeDossier = func(d, consigne string, delai time.Duration) (string, error) {
	claude := trouverClaudePilote(estWindows)
	if claude == "" {
		claude = "claude"
	}
	a := []string{"-p", consigne, "--output-format", "text", "--max-turns", "60", "--strict-mcp-config",
		"--allowedTools", "Read,Glob,Grep,Edit(./**),Write(./**),MultiEdit(./**),WebFetch,WebSearch"}
	if m := os.Getenv("MODEL"); m != "" {
		a = append(a, "--model", m)
	}
	ctx, fin := context.WithTimeout(context.Background(), delai)
	defer fin()
	c := commandeContexte(ctx, claude, a...)
	c.Dir = d
	c.Env = append(os.Environ(), "ENABLE_CLAUDEAI_MCP_SERVERS=false")
	var sortie, erreurs bytes.Buffer
	c.Stdout, c.Stderr = &sortie, &limiteur{b: &erreurs, max: 1 << 16}
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("Claude took too long")
		}
		return "", errors.New(court(derniereLigneTexte(firstNonEmpty(strings.TrimSpace(erreurs.String()), err.Error())), 300))
	}
	return sortie.String(), nil
}

func consigneSkill(id string, t TestSkill) string {
	var b strings.Builder
	langue := languesCorrection[os.Getenv("LANGUAGE")]
	if langue == "" {
		langue = "English"
	}
	b.WriteString("This folder is a Claude Code skill (SKILL.md and its files), used by Athanor, a tool that drives Claude Code on Windows (Claude's commands run in Git Bash), macOS and Linux. On this computer, Athanor's test of the skill finds problems.\n\n")
	b.WriteString("## This computer\n" + descriptionSysteme() + "\nPrograms found: " + programmesTrouves() + "\n\n")
	b.WriteString("## The skill: " + id + "\nWhat the test found:\n")
	for _, p := range t.Problemes {
		g := "may fail elsewhere"
		if p.Grave {
			g = "fails here"
		}
		b.WriteString("- " + p.Fichier + ": " + p.Message + " (" + g + ")\n")
	}
	b.WriteString(`
## What to do
Change the files of this folder so that the skill works on this system AND on the others (Windows with Git Bash, macOS, Linux), doing exactly what it did. Rules:
- Same purpose, same steps, same results: fix how, never what.
- Prefer what runs everywhere: #!/usr/bin/env bash or python3; Unix line endings (LF) in scripts; portable options (sed -i.bak then remove the .bak, no grep -P, no readlink -f, no date -d); when SKILL.md tells Claude to run Python, say python3 and mention python as the fallback on Windows.
- Change only what the problems need. Keep SKILL.md's header (name, description).
- Write only inside this folder. Download nothing, install nothing.
- A program missing on this computer (python, node…): do not work around it, say which one in "manque".
- When the fault is Athanor's own (how it runs skills), say what Athanor should change in "athanor".
When you are done, answer with ONLY one JSON object, no code fence:
{"explication": "what was wrong and what you changed, two sentences at most, in ` + langue + `", "manque": "", "athanor": ""}`)
	return b.String()
}

// copierPropre: the skill copied as it is into dst (dst emptied first).
func copierPropre(src, dst string) error {
	_ = os.RemoveAll(dst)
	return copierDossier(src, dst, false)
}

// fichiersDifferents: the files added, changed or removed between two folders.
func fichiersDifferents(a, b string) []string {
	lister := func(d string) map[string][]byte {
		m := map[string][]byte{}
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && e.Name() != ".athanor-correction.json" {
				if x, err := os.ReadFile(p); err == nil {
					r, _ := filepath.Rel(d, p)
					m[filepath.ToSlash(r)] = x
				}
			}
			return nil
		})
		return m
	}
	ma, mb := lister(a), lister(b)
	var l []string
	for k, v := range mb {
		if w, ok := ma[k]; !ok || !bytes.Equal(v, w) {
			l = append(l, k)
		}
	}
	for k := range ma {
		if _, ok := mb[k]; !ok {
			l = append(l, k)
		}
	}
	sort.Strings(l)
	return l
}

// diffDossiers: the changes as a unified diff (git's, paths as in the skill), at most 200 kB.
func diffDossiers(a, b string) string {
	t, err := os.MkdirTemp("", "athanor-diff-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(t)
	if copierDossier(a, filepath.Join(t, "a"), false) != nil || copierDossier(b, filepath.Join(t, "b"), false) != nil {
		return ""
	}
	_ = os.Remove(filepath.Join(t, "b", ".athanor-correction.json"))
	c := exec.Command("git", "-c", "core.quotepath=false", "diff", "--no-index", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--", "a", "b")
	c.Dir = t
	cacherFenetre(c)
	out, _ := c.Output()
	s := strings.NewReplacer("a/a/", "a/", "b/b/", "b/", " a/a/", " a/", " b/b/", " b/").Replace(string(out))
	return tronquerRunes(s, 200000)
}

// corrigerSkill: Claude's proposal for a skill whose test finds problems, in a copy, tested again.
func corrigerSkill(kit, id string, etape func(string, int)) CorrectionSkill {
	res := CorrectionSkill{Systeme: systemeMCP()}
	etape("test", 0)
	t := testerSkill(kit, id)
	if t.Erreur != "" {
		res.Erreur = t.Erreur
		return res
	}
	res.Avant = t.Problemes
	if len(t.Problemes) == 0 {
		res.OK, res.Deja = true, true
		return res
	}
	copie := propositionSkill(kit, id)
	if err := copierPropre(dossierSkill(kit, id), copie); err != nil {
		res.Erreur = err.Error()
		return res
	}
	etape("claude", 1)
	texte, err := demanderClaudeDossier(copie, consigneSkill(id, t), 15*time.Minute)
	if err != nil {
		res.Erreur = "Claude could not answer: " + err.Error()
		return res
	}
	var p PropositionMCP
	if i, j := strings.Index(texte, "{"), strings.LastIndex(texte, "}"); i >= 0 && j > i {
		_ = json.Unmarshal([]byte(texte[i:j+1]), &p)
	}
	res.Explication, res.Manque, res.Athanor = court(strings.TrimSpace(p.Explication), 1200), court(strings.TrimSpace(p.Manque), 300), court(strings.TrimSpace(p.Athanor), 1500)
	res.Fichiers = fichiersDifferents(dossierSkill(kit, id), copie)
	if len(res.Fichiers) == 0 {
		_ = os.RemoveAll(copie)
		if res.Manque == "" && res.Athanor == "" {
			res.Erreur = "Claude changed nothing"
		}
		return res
	}
	etape("test", 1)
	apres := testerDossierSkill(copie)
	res.Apres, res.OK = apres.Problemes, apres.OK && apres.Erreur == ""
	res.Diff = diffDossiers(dossierSkill(kit, id), copie)
	b, _ := json.Marshal(res)
	_ = os.WriteFile(filepath.Join(copie, ".athanor-correction.json"), b, 0o644)
	return res
}

// appliquerSkill: the proposal put in place (the skill as it was kept for Undo), and its report.
func appliquerSkill(kit, id string) error {
	if !reIdSur.MatchString(id) {
		return errors.New("invalid id")
	}
	copie, d := propositionSkill(kit, id), dossierSkill(kit, id)
	var res CorrectionSkill
	b, err := os.ReadFile(filepath.Join(copie, ".athanor-correction.json"))
	if err != nil || json.Unmarshal(b, &res) != nil {
		return errors.New("no fix waiting for this skill")
	}
	if !estDossierSansLien(d) {
		return errors.New("unknown skill")
	}
	sauve := sauvegardeSkill(kit, id)
	if !estDossierSansLien(sauve) { // the skill before the first fix: what Undo puts back
		if err := copierPropre(d, sauve); err != nil {
			return err
		}
	}
	_ = os.Remove(filepath.Join(copie, ".athanor-correction.json"))
	neuf := d + ".neuf"
	if err := copierPropre(copie, neuf); err != nil {
		return err
	}
	vieux := d + ".vieux"
	_ = os.RemoveAll(vieux)
	if err := os.Rename(d, vieux); err != nil {
		return err
	}
	if err := os.Rename(neuf, d); err != nil {
		_ = os.Rename(vieux, d)
		return err
	}
	_ = os.RemoveAll(vieux)
	_ = os.RemoveAll(copie)
	bib := lireBiblio(Kit{Racine: kit})
	sk := bib.Skills[id]
	diff := strings.Split(res.Diff, "\n")
	if len(diff) > 400 {
		diff = diff[:400]
	}
	return ecrireRapport(kit, RapportCorrection{Genre: "skill", ID: id, Nom: sk.Nom, Catalogue: sk.Catalogue, Source: sk.Source, Systeme: res.Systeme,
		OS: descriptionSysteme(), Version: versionAthanor, Le: time.Now().Format("2006-01-02 15:04"), Erreur: resumeProblemes(res.Avant),
		Details: diff, Explication: res.Explication, Athanor: res.Athanor, Fichiers: res.Fichiers})
}

func resumeProblemes(l []ProblemeSkill) string {
	var s []string
	for _, p := range l {
		s = append(s, p.Fichier+": "+p.Message)
	}
	return court(strings.Join(s, "; "), 2000)
}

// annulerSkill: the skill as it was before the first fix.
func annulerSkill(kit, id string) error {
	sauve, d := sauvegardeSkill(kit, id), dossierSkill(kit, id)
	if !reIdSur.MatchString(id) || !estDossierSansLien(sauve) {
		return errors.New("nothing to undo")
	}
	vieux := d + ".vieux"
	_ = os.RemoveAll(vieux)
	if err := os.Rename(d, vieux); err != nil {
		return err
	}
	if err := os.Rename(sauve, d); err != nil {
		_ = os.Rename(vieux, d)
		return err
	}
	return os.RemoveAll(vieux)
}

// skillCorrige: the skill was changed by a fix (Undo is possible).
func skillCorrige(kit, id string) bool { return estDossierSansLien(sauvegardeSkill(kit, id)) }

func lancerSkillExt(action, kit, id string, w io.Writer) int {
	ecrire := func(v any) int {
		b, _ := json.Marshal(v)
		fmt.Fprintln(w, string(b))
		return 0
	}
	switch action {
	case "tester-skill":
		return ecrire(testerSkill(kit, id))
	case "corriger-skill":
		chargerEnvKit(kit)
		return ecrire(corrigerSkill(kit, id, func(q string, n int) { fmt.Fprintf(w, "ETAPE %s %d\n", q, n) }))
	case "appliquer-skill", "annuler-skill":
		f := appliquerSkill
		if action == "annuler-skill" {
			f = annulerSkill
		}
		if err := f(kit, id); err != nil {
			return ecrire(map[string]any{"ok": false, "erreur": err.Error()})
		}
		return ecrire(map[string]any{"ok": true})
	}
	return 2
}
