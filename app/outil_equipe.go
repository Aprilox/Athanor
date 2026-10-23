package main

// athanor equipe importer KIT PROJECT | json KIT PROJECT AGENTS_DIR [--ouvrier] | fiche KIT PROJECT AGENTS_DIR:
// the team of sub-agents (replaces system/equipe.py, same files and outputs): Athanor's own (template copies
// in AGENTS_DIR), the ones the user creates in the app, the ones the lead proposes in .athanor/agents/.
//   importer  imports the lead's proposals (validated) into extensions/agents.json, one line each
//   json      the --agents JSON (Athanor's agents + the project's own); --ouvrier: executor + the project's
//   fiche     writes PROJECT/.athanor/TEAM.md (the team, the refused proposals)
// The proposals are data written by Claude: names, sizes and fields are validated, links never followed.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxDescriptionAgent = 400
	maxConsignesAgent   = 6000
	maxAgentsParProjet  = 12
	maxAgentsDuChef     = 5
)

const piedAgent = "\n\nNobody will answer your questions: when a detail is missing, choose the simplest option that fits " +
	"`.athanor/REQUEST.md` and say so in your report. The files the user attached to the request are in " +
	"`.athanor/attachments/` (listed in its \"Attached files\" section): read those that concern your task. " +
	"The skills and MCP tools of the project are available to you as well (the Skill tool, `mcp__…` tools): " +
	"use them whenever they fit the task. End with a short report: what you did, what you checked, what is left."

// formesProjet: every form under which the app may have written the project's folder.
func formesProjet(projet string) map[string]bool {
	n := map[string]bool{normaliserDossier(projet): true, normaliserDossier(realpathPython(projet)): true}
	if w := wslpathW(projet); w != "" {
		n[normaliserDossier(w)] = true
	}
	return n
}

func wslpathW(p string) string {
	if _, err := exec.LookPath("wslpath"); err != nil {
		return ""
	}
	b, err := exec.Command("wslpath", "-w", p).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// cleProjetApp: the folder as the app writes it (the Windows form under WSL, else the path itself).
func cleProjetApp(projet string) string {
	if w := wslpathW(projet); w != "" {
		return w
	}
	return projet
}

// equipeFichier: extensions/agents.json as equipe.py reads it: agents kept in their raw form (their
// order and unknown fields stay), the invalid ones dropped, the other keys kept.
type equipeFichier struct {
	brut    objetJSON
	agents  []map[string]any
	bruts   []json.RawMessage
	refuses objetJSON
}

func lireEquipe(kit string) equipeFichier {
	var e equipeFichier
	b, err := os.ReadFile(filepath.Join(kit, "extensions", "agents.json"))
	if err == nil {
		b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
		if json.Unmarshal(b, &e.brut) != nil {
			e.brut = objetJSON{}
		}
	}
	var l []json.RawMessage
	_ = json.Unmarshal(e.brut.valeur["agents"], &l)
	for _, x := range l {
		var a map[string]any
		dec := json.NewDecoder(bytes.NewReader(x))
		dec.UseNumber()
		if dec.Decode(&a) == nil && a != nil && valideAgent(a) == "" {
			e.agents = append(e.agents, a)
			e.bruts = append(e.bruts, x)
		}
	}
	var r objetJSON
	_ = json.Unmarshal(e.brut.valeur["refuses"], &r)
	for _, k := range r.cles {
		var v []any
		if json.Unmarshal(r.valeur[k], &v) != nil {
			continue
		}
		noms := []string{}
		for _, n := range v {
			if s, ok := n.(string); ok {
				noms = append(noms, s)
			}
		}
		e.refuses.mettre(k, noms)
	}
	return e
}

func ecrireEquipe(kit string, e equipeFichier) error {
	f := filepath.Join(kit, "extensions", "agents.json")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	bruts := e.bruts
	if bruts == nil {
		bruts = []json.RawMessage{}
	}
	e.brut.mettre("agents", bruts)
	if e.refuses.valeur == nil {
		e.refuses = objetJSON{cles: []string{}, valeur: map[string]json.RawMessage{}}
	}
	e.brut.mettre("refuses", e.refuses)
	t := fmt.Sprintf("%s.%d.tmp", f, os.Getpid())
	if err := ecrireJSONPython(t, e.brut, "  "); err != nil {
		return err
	}
	return os.Rename(t, f)
}

// valideAgent: "" when the definition is acceptable, else the reason (English, for the log).
func valideAgent(a map[string]any) string {
	nom, ok := a["nom"].(string)
	if !ok || !reNomAgent.MatchString(nom) {
		return "invalid name (lowercase letters, digits and dashes, 2 to 31 characters)"
	}
	if agentsReserves[nom] {
		return "name taken by a built-in sub-agent"
	}
	for _, c := range []struct {
		cle string
		max int
	}{{"description", maxDescriptionAgent}, {"consignes", maxConsignesAgent}} {
		v, ok := a[c.cle].(string)
		if !ok || strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > c.max {
			return fmt.Sprintf("%s missing or longer than %d characters", c.cle, c.max)
		}
	}
	if m, ok := a["modele"]; ok {
		if s, ok := m.(string); !ok || (s != "" && s != "eco" && s != "fort") {
			return "unknown model"
		}
	}
	if p, ok := a["projet"]; ok {
		if _, ok := p.(string); !ok {
			return "invalid project"
		}
	}
	return ""
}

func (e equipeFichier) duProjet(projet string) []map[string]any {
	noms := formesProjet(projet)
	var out []map[string]any
	for _, a := range e.agents {
		p, _ := a["projet"].(string)
		if p == "" || noms[normaliserDossier(p)] {
			out = append(out, a)
		}
	}
	if len(out) > maxAgentsParProjet {
		out = out[:maxAgentsParProjet]
	}
	return out
}

func (e equipeFichier) refusesDuProjet(projet string) map[string]bool {
	noms := formesProjet(projet)
	r := map[string]bool{}
	for _, k := range e.refuses.cles {
		if noms[normaliserDossier(k)] {
			var v []string
			_ = json.Unmarshal(e.refuses.valeur[k], &v)
			for _, n := range v {
				r[n] = true
			}
		}
	}
	return r
}

// lireSansLien: at most max bytes of a file that is not a link (refused: a link, a larger file).
func lireSansLien(chemin string, max int64) (string, error) {
	st, err := os.Lstat(chemin)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s is a link", chemin)
	}
	f, err := os.Open(chemin)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, max+1)
	n, _ := f.Read(b)
	for n < len(b) {
		m, err := f.Read(b[n:])
		n += m
		if err != nil || m == 0 {
			break
		}
	}
	if int64(n) > max {
		return "", errTropGrand
	}
	return strings.ToValidUTF8(string(b[:n]), "�"), nil
}

type erreurValeur string

func (e erreurValeur) Error() string { return string(e) }

var errTropGrand = erreurValeur("file too large")

var reProposition = regexp.MustCompile(`(?s)^\x{feff}?---\r?\n(.*?)\r?\n---\r?\n(.*)$`)

// lireProposition: a sub-agent the lead proposes (front matter name / description / model / read_only).
func lireProposition(t string) (map[string]any, error) {
	m := reProposition.FindStringSubmatch(t)
	if m == nil {
		return nil, erreurValeur("no front matter (--- name / description / model / read_only ---)")
	}
	meta := map[string]string{}
	for _, l := range lignesPython(m[1]) {
		if k, v, ok := strings.Cut(l, ":"); ok {
			meta[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.Trim(strings.TrimSpace(v), `"`), "'")
		}
	}
	premier := func(k string) string {
		f := strings.Fields(meta[k])
		if len(f) == 0 {
			return ""
		}
		return strings.ToLower(f[0])
	}
	modele, ok := map[string]string{"": "", "default": "", "inherit": "", "eco": "eco", "cheap": "eco", "strong": "fort", "fort": "fort"}[premier("model")]
	if !ok {
		return nil, erreurValeur("unknown model (default, cheap or strong)")
	}
	ls := premier("read_only")
	return map[string]any{"nom": strings.ToLower(meta["name"]), "description": meta["description"], "consignes": strings.TrimSpace(m[2]),
		"modele": modele, "lecture_seule": ls == "yes" || ls == "true" || ls == "1" || ls == "oui"}, nil
}

func equipeImporter(kit, projet string) []string {
	var lignes []string
	dossier := filepath.Join(projet, ".athanor", "agents")
	if st, err := os.Lstat(dossier); err != nil || !st.IsDir() {
		return nil
	}
	f := filepath.Join(kit, "extensions", "agents.json")
	if _, err := os.Stat(f); err == nil { // an unreadable file is never overwritten (the user's sub-agents are in it)
		b, err := os.ReadFile(f)
		var o map[string]any
		if err != nil || json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &o) != nil || o == nil {
			return nil
		}
	}
	e := lireEquipe(kit)
	refuses := e.refusesDuProjet(projet)
	existants := map[string]bool{}
	nChef := 0
	for _, a := range e.duProjet(projet) {
		existants[a["nom"].(string)] = true
		if a["origine"] == "chef" {
			nChef++
		}
	}
	cle := cleProjetApp(projet)
	change := false
	noms, _ := os.ReadDir(dossier)
	var tries []string
	for _, n := range noms {
		tries = append(tries, n.Name())
	}
	sort.Strings(tries)
	for _, nom := range tries {
		chemin := filepath.Join(dossier, nom)
		if !strings.HasSuffix(nom, ".md") || strings.HasPrefix(nom, ".") {
			continue
		}
		var a map[string]any
		raison := ""
		texte, err := lireSansLien(chemin, 40000)
		if err != nil && err != errTropGrand {
			a, raison = map[string]any{"nom": strings.TrimSuffix(nom, ".md")}, "unreadable, or a link in place of the file"
		} else if err == errTropGrand {
			a, raison = map[string]any{"nom": strings.TrimSuffix(nom, ".md")}, err.Error()
		} else if p, e2 := lireProposition(texte); e2 != nil {
			a, raison = map[string]any{"nom": strings.TrimSuffix(nom, ".md")}, e2.Error()
		} else {
			a = p
			raison = valideAgent(a)
			n := a["nom"].(string)
			if raison == "" && existants[n] {
				raison = "a sub-agent already has this name"
			}
			if raison == "" && refuses[n] {
				raison = "the user removed this sub-agent: do not propose it again"
			}
			if raison == "" && nChef >= maxAgentsDuChef {
				raison = fmt.Sprintf("already %d sub-agents proposed by the lead in this project", maxAgentsDuChef)
			}
			if raison == "" && len(existants) >= maxAgentsParProjet {
				raison = fmt.Sprintf("the team is full (%d sub-agents of the project)", maxAgentsParProjet)
			}
		}
		if raison == "" {
			var o objetJSON
			for _, k := range []string{"nom", "description", "consignes", "modele", "lecture_seule"} {
				o.mettre(k, a[k])
			}
			o.mettre("projet", cle)
			o.mettre("origine", "chef")
			o.mettre("cree", time.Now().Format("2006-01-02"))
			b, _ := json.Marshal(o)
			a["projet"], a["origine"] = cle, "chef"
			e.agents = append(e.agents, a)
			e.bruts = append(e.bruts, b)
			existants[a["nom"].(string)] = true
			nChef++
			change = true
			lignes = append(lignes, fmt.Sprintf("Sub-agent proposed by the lead added to the team: %s (%s)", a["nom"], tronquerRunes(a["description"].(string), 120)))
		} else {
			lignes = append(lignes, fmt.Sprintf("Sub-agent proposal refused (%s): %s", nom, raison))
			n, _ := a["nom"].(string)
			journalRefus(projet, firstNonEmpty(n, nom), raison)
		}
		os.Remove(chemin)
	}
	if change {
		_ = ecrireEquipe(kit, e)
	}
	return lignes
}

// texteUniversel: a text file as Python's open() reads it (\r\n and \r become \n).
func texteUniversel(b []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToValidUTF8(string(b), "\uFFFD"), "\r\n", "\n"), "\r", "\n")
}

func tronquerRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

var reNomRefus = regexp.MustCompile(`[^a-z0-9.-]`)

func journalRefus(projet, nom, raison string) {
	f := filepath.Join(projet, ".athanor", "agents", ".refused.txt")
	if st, err := os.Lstat(f); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return
	}
	o, err := os.OpenFile(f, os.O_WRONLY|os.O_APPEND|os.O_CREATE|oSansLien, 0o644)
	if err != nil {
		return
	}
	defer o.Close()
	fmt.Fprintf(o, "%s: %s\n", tronquerRunes(reNomRefus.ReplaceAllString(strings.ToLower(nom), ""), 40), raison)
}

func sousAgentsModele() string {
	m := strings.TrimSpace(os.Getenv("SUBAGENT_MODEL"))
	if strings.ToLower(m) == "auto" {
		return ""
	}
	return m
}

func definitionAgent(a map[string]any) objetJSON {
	var x objetJSON
	x.mettre("description", strings.TrimSpace(a["description"].(string)))
	x.mettre("prompt", strings.TrimSpace(a["consignes"].(string))+piedAgent)
	if vraiPython(a["lecture_seule"]) {
		x.mettre("disallowedTools", []string{"Write", "Edit", "NotebookEdit"})
	}
	simple, defini := os.LookupEnv("CHEAP_MODEL")
	if !defini {
		simple = "haiku"
	}
	switch {
	case a["modele"] == "eco" && simple != "":
		x.mettre("model", simple)
	case a["modele"] == "fort":
		x.mettre("model", "opus")
	case sousAgentsModele() != "":
		x.mettre("model", sousAgentsModele())
	}
	return x
}

var reEnteteAgent = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)$`)

// agentsAthanor: Athanor's sub-agents (the template copies of AGENTS_DIR), with their model; a copy without
// a model line gets one (when --agents does not exist, Claude Code reads these files).
func agentsAthanor(dossier string) objetJSON {
	var out objetJSON
	sous, simple := sousAgentsModele(), os.Getenv("CHEAP_MODEL")
	fs, _ := os.ReadDir(dossier)
	var noms []string
	for _, f := range fs {
		noms = append(noms, f.Name())
	}
	sort.Strings(noms)
	for _, f := range noms {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dossier, f))
		if err != nil {
			continue
		}
		m := reEnteteAgent.FindStringSubmatch(texteUniversel(b))
		if m == nil {
			continue
		}
		meta := map[string]string{}
		for _, l := range lignesPython(m[1]) {
			if k, v, ok := strings.Cut(l, ":"); ok {
				meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		var a objetJSON
		a.mettre("description", meta["description"])
		a.mettre("prompt", strings.TrimSpace(m[2]))
		separer := func(s string) []string {
			var l []string
			for _, x := range strings.Split(s, ",") {
				l = append(l, strings.TrimSpace(x))
			}
			return l
		}
		if meta["tools"] != "" {
			a.mettre("tools", separer(meta["tools"]))
		}
		if meta["disallowedTools"] != "" {
			a.mettre("disallowedTools", separer(meta["disallowedTools"]))
		}
		nom, ok := meta["name"]
		if !ok {
			nom = strings.TrimSuffix(f, ".md")
		}
		modele := meta["model"]
		if modele == "" && nom == "executor" {
			modele = simple
		}
		if modele == "" {
			modele = sous
		}
		if modele == "" && nom == "scout" {
			modele = "sonnet"
		}
		if modele != "" {
			a.mettre("model", modele)
			if meta["model"] == "" {
				_ = os.WriteFile(filepath.Join(dossier, f), []byte("---\n"+m[1]+"\nmodel: "+modele+"\n---\n"+m[2]), 0o644)
			}
		}
		out.mettre(nom, a)
	}
	return out
}

func equipeJSON(kit, projet, dossier string, ouvrier bool) string {
	out := agentsAthanor(dossier)
	if ouvrier {
		var o objetJSON
		if v, ok := out.valeur["executor"]; ok {
			o.cles, o.valeur = []string{"executor"}, map[string]json.RawMessage{"executor": v}
		}
		out = o
	}
	for _, a := range lireEquipe(kit).duProjet(projet) {
		out.mettre(a["nom"].(string), definitionAgent(a))
	}
	if out.valeur == nil {
		return "{}"
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return strings.TrimRight(b.String(), "\n")
}

var reDescriptionAgent = regexp.MustCompile(`(?m)^description:\s*(.*)$`)

func equipeFiche(kit, projet, dossier string) error {
	etat := filepath.Join(projet, ".athanor")
	if st, err := os.Lstat(etat); err != nil || !st.IsDir() {
		return nil
	}
	e := lireEquipe(kit)
	lignes := []string{"# Your team (written by the pilot before each cycle, do not edit)", "",
		"Call them with the Task tool by their name. Propose a new one in `.athanor/agents/` (see your instructions).", ""}
	fs, _ := os.ReadDir(dossier)
	var noms []string
	for _, f := range fs {
		noms = append(noms, f.Name())
	}
	sort.Strings(noms)
	for _, f := range noms {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dossier, f))
		d := ""
		if m := reDescriptionAgent.FindStringSubmatch(texteUniversel(b)); m != nil {
			d = strings.TrimSpace(m[1])
		}
		lignes = append(lignes, fmt.Sprintf("- `%s` (Athanor): %s", strings.TrimSuffix(f, ".md"), tronquerRunes(d, 300)))
	}
	for _, a := range e.duProjet(projet) {
		qui := "created by the user"
		if a["origine"] == "chef" {
			qui = "proposed by you, kept by the user"
		}
		ls := ""
		if vraiPython(a["lecture_seule"]) {
			ls = ", read only"
		}
		lignes = append(lignes, fmt.Sprintf("- `%s` (%s%s): %s", a["nom"], qui, ls, strings.ReplaceAll(strings.TrimSpace(a["description"].(string)), "\n", " ")))
	}
	var refus []string
	for n := range e.refusesDuProjet(projet) {
		refus = append(refus, n)
	}
	sort.Strings(refus)
	if len(refus) > 0 {
		var q []string
		for _, n := range refus {
			q = append(q, "`"+n+"`")
		}
		lignes = append(lignes, "", "Removed by the user, never propose them again: "+strings.Join(q, ", "))
	}
	r := filepath.Join(etat, "agents", ".refused.txt")
	if st, err := os.Lstat(r); err == nil && st.Mode().IsRegular() {
		if t, err := lireSansLien(r, 20000); err == nil {
			l := lignesPython(strings.TrimSpace(t))
			if len(l) > 5 {
				l = l[len(l)-5:]
			}
			if len(l) > 0 {
				lignes = append(lignes, "", "Your last refused proposals:")
				for _, x := range l {
					lignes = append(lignes, "- "+tronquerRunes(x, 200))
				}
			}
		}
	}
	t := filepath.Join(etat, fmt.Sprintf(".TEAM.md.%d", os.Getpid()))
	o, err := os.OpenFile(t, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oSansLien, 0o644)
	if err != nil {
		return err
	}
	_, err = o.WriteString(strings.Join(lignes, "\n") + "\n")
	o.Close()
	if err != nil {
		os.Remove(t)
		return err
	}
	return os.Rename(t, filepath.Join(etat, "TEAM.md"))
}

func lancerEquipe(args []string) int {
	args = cheminsOutil(args)
	switch {
	case len(args) >= 3 && args[0] == "importer":
		for _, l := range equipeImporter(args[1], abspathOS(args[2])) {
			fmt.Println(l)
		}
		return 0
	case len(args) >= 4 && args[0] == "json":
		ouvrier := false
		for _, a := range args {
			ouvrier = ouvrier || a == "--ouvrier"
		}
		fmt.Println(equipeJSON(args[1], abspathOS(args[2]), args[3], ouvrier))
		return 0
	case len(args) >= 4 && args[0] == "fiche":
		if err := equipeFiche(args[1], abspathOS(args[2]), args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: athanor equipe importer KIT PROJECT | json KIT PROJECT AGENTS_DIR [--ouvrier] | fiche KIT PROJECT AGENTS_DIR")
	return 2
}
