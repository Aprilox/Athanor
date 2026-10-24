package main

// Reading and writing the kit files: .env, REQUEST.md, queue, pilot state (STATE.json),
// statistics (stats.tsv), plan.

import (
	"bufio"
	"encoding/json"
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
	"sync"
	"time"
)

// Kit: the tool folder (where the executable is). It contains no project.
type Kit struct{ Racine string }

func (k Kit) Valide() bool { return estDossierKit(k.Racine) }

// estDossierKit: d is a tool folder (its texts for Claude are there; a kit of any version has them).
func estDossierKit(d string) bool {
	st, err := os.Stat(filepath.Join(d, "template", "CLAUDE.md"))
	return err == nil && st.Mode().IsRegular()
}

// Installe: the tooling of the mode in use is there. WSL: install.ps1 wrote the distribution's name.
// Native Windows: Git Bash and Claude Code for Windows (commands\install-natif.ps1). Linux and macOS:
// Claude Code (commands/install.sh), the pilot running on this very system.
func (k Kit) Installe() bool {
	if _, err := os.Stat(filepath.Join(k.Racine, "system", "distro.txt")); err == nil && !modeNatif.Load() {
		return true
	}
	if runtime.GOOS == "windows" {
		return modeNatif.Load() && bashNatif() != "" && claudePresent("claude.exe")
	}
	return claudePresent("claude")
}

// claudePresent: Claude Code's program in the PATH or where its installer puts it (~/.local/bin).
func claudePresent(nom string) bool {
	if _, err := exec.LookPath(nom); err == nil {
		return true
	}
	if d, err := os.UserHomeDir(); err == nil {
		if st, err := os.Stat(filepath.Join(d, ".local", "bin", nom)); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}
func (k Kit) Env() string        { return filepath.Join(k.Racine, ".env") }
func (k Kit) EnvExemple() string { return filepath.Join(k.Racine, ".env.example") }
func (k Kit) Journaux() string   { return filepath.Join(k.Racine, "logs") }
func (k Kit) Distro() string {
	b, err := os.ReadFile(filepath.Join(k.Racine, "system", "distro.txt"))
	if err != nil {
		return "Ubuntu"
	}
	return strings.TrimPrefix(strings.TrimSpace(string(b)), "\ufeff")
}

// Projet: a development folder of the user. The tool only writes into .athanor/ (the workshop).
type Projet struct {
	Nom     string `json:"nom"`
	Dossier string `json:"dossier"`
	Archive bool   `json:"archive,omitempty"` // archived: out of the selector and of the automatic restarts
}

func (p Projet) Atelier() string        { return filepath.Join(p.Dossier, ".athanor") }
func (p Projet) Demande() string        { return filepath.Join(p.Atelier(), "REQUEST.md") }
func (p Projet) FileAttente() string    { return filepath.Join(p.Atelier(), "queue") }
func (p Projet) DemandesFaites() string { return filepath.Join(p.Atelier(), "done") }
func (p Projet) EtatJSON() string       { return filepath.Join(p.Atelier(), "STATE.json") }
func (p Projet) StatsTSV() string       { return filepath.Join(p.Atelier(), "stats.tsv") }
func (p Projet) PiloteLog() string      { return filepath.Join(p.Atelier(), "pilot.log") }
func (p Projet) LogsDir() string        { return filepath.Join(p.Atelier(), "logs") }
func (p Projet) Plan() string           { return filepath.Join(p.Atelier(), "PLAN.md") }
func (p Projet) Existe() bool           { st, err := os.Stat(p.Dossier); return err == nil && st.IsDir() }

// The pilot writes pilot.pid at startup, touches it regularly and removes it when it stops.
func (p Projet) PiloteActif() bool {
	fi, err := os.Stat(filepath.Join(p.Atelier(), "pilot.pid"))
	// the pilot touches the file every 20 s: beyond 90 s, it is dead (WSL cut off, PC switched off)
	return err == nil && time.Since(fi.ModTime()) < 90*time.Second
}

// JournauxCycle returns the logs of the current cycle: "cycle-<date>-<n>.jsonl" (lead) and, in parallel
// mode, "cycle-<date>-<n>-<milestone>.jsonl" (one per worker). The lead first, then the workers.
func (p Projet) JournauxCycle() []string {
	entrees, err := os.ReadDir(p.LogsDir())
	if err != nil {
		return nil
	}
	groupes := map[string][]string{}
	var prefixes []string
	for _, e := range entrees {
		n := e.Name()
		if !strings.HasSuffix(n, ".jsonl") || !strings.HasPrefix(n, "cycle-") {
			continue
		}
		pref := strings.TrimSuffix(n, ".jsonl")
		if i := strings.LastIndex(pref, "-"); i > 0 {
			reste := pref[i+1:]
			if _, err := strconv.Atoi(reste); err != nil { // non-numeric suffix = worker
				pref = pref[:i]
			}
		}
		if _, ok := groupes[pref]; !ok {
			prefixes = append(prefixes, pref)
		}
		groupes[pref] = append(groupes[pref], n)
	}
	if len(prefixes) == 0 {
		return nil
	}
	sort.Strings(prefixes)
	dernier := prefixes[len(prefixes)-1]
	noms := groupes[dernier]
	sort.Slice(noms, func(i, j int) bool {
		ci, cj := noms[i] == dernier+".jsonl", noms[j] == dernier+".jsonl"
		if ci != cj {
			return ci
		}
		return noms[i] < noms[j]
	})
	var out []string
	for _, n := range noms {
		out = append(out, filepath.Join(p.LogsDir(), n))
	}
	return out
}

// DernierJournalCycle: the main log (lead) of the current cycle.
func (p Projet) DernierJournalCycle() string {
	j := p.JournauxCycle()
	if len(j) == 0 {
		return ""
	}
	return j[0]
}

// OuvrierDe returns the worker id of a log ("M4"), or "" for the lead's log.
func OuvrierDe(chemin string) string {
	n := strings.TrimSuffix(filepath.Base(chemin), ".jsonl")
	i := strings.LastIndex(n, "-")
	if i < 0 {
		return ""
	}
	if _, err := strconv.Atoi(n[i+1:]); err == nil {
		return ""
	}
	return n[i+1:]
}

// ----------------------------------------------------------------------------- state

type Etat struct {
	Maj               string `json:"maj"`
	Pid               int64  `json:"pid"`
	Phase             string `json:"phase"`
	Etat              string `json:"etat"`
	Cycle             int64  `json:"cycle"`
	Modele            string `json:"template"`
	Journal           string `json:"journal"`
	JalonEnCours      string `json:"jalon_en_cours"`
	DernierCommit     string `json:"dernier_commit"`
	ReprisePrevue     string `json:"reprise_prevue"`
	DebutCycle        string `json:"debut_cycle"`
	DemandesEnAttente int64  `json:"demandes_en_attente"`
	Ouvriers          string `json:"ouvriers"` // parallel mode: milestones being built by workers, separated by spaces
}

func (p Projet) LireEtat() (Etat, bool) {
	var e Etat
	b, err := os.ReadFile(p.EtatJSON())
	if err != nil {
		return e, false
	}
	if json.Unmarshal(b, &e) != nil {
		return e, false
	}
	return e, true
}

// ----------------------------------------------------------------------------- statistics

type LigneStat struct {
	Horodatage   string  `json:"horodatage"`
	Cycle        string  `json:"cycle"` // "3", or "3·M4" for a parallel worker
	Modele       string  `json:"template"`
	Etat         string  `json:"etat"`
	Minutes      float64 `json:"minutes"`
	MinutesAPI   float64 `json:"minutes_api"`
	Tours        int64   `json:"tours"`
	CoutUSD      float64 `json:"cout_usd"`
	EntreeTokens int64   `json:"entree_tokens"`
	CacheLu      int64   `json:"cache_lu"`
	CacheEcrit   int64   `json:"cache_ecrit"`
	SortieTokens int64   `json:"sortie_tokens"`
	ContexteMax  int64   `json:"contexte_max"`
	Commit       string  `json:"commit"`
	Estime       bool    `json:"estime"`          // cost worked out by the pilot from the turns of the lead and its sub-agents (no "result" line)
	Titre        string  `json:"titre,omitempty"` // the cycle's milestone, as the pilot's log says it ("Cycle 12 succeeded: M8a: …")
}

func (p Projet) LireStats() []LigneStat {
	b, err := os.ReadFile(p.StatsTSV())
	if err != nil {
		return nil
	}
	var out []LigneStat
	for i, l := range strings.Split(string(b), "\n") {
		if i == 0 || strings.TrimSpace(l) == "" {
			continue
		}
		c := strings.Split(l, "\t")
		g := func(n int) string {
			if n < len(c) {
				return strings.TrimSpace(c[n])
			}
			return ""
		}
		f := func(n int) float64 { v, _ := strconv.ParseFloat(g(n), 64); return v }
		e := func(n int) int64 { v, _ := strconv.ParseInt(g(n), 10, 64); return v }
		out = append(out, LigneStat{g(0), g(1), g(2), g(3), f(4), f(5), e(6), f(7), e(8), e(9), e(10), e(11), e(12), g(13), g(14) == "1", ""})
	}
	if len(out) > 0 {
		titres := titresCycles(p.PiloteLog())
		for i := range out {
			out[i].Titre = titres[out[i].Cycle]
		}
	}
	return out
}

var reCycleReussi = regexp.MustCompile(`^\[[^\]]*\] Cycle ([0-9]{1,6}) succeeded: (.{1,300})$`)

type titresEnCache struct {
	taille int64
	date   time.Time
	titres map[string]string
}

var (
	titresMu    sync.Mutex
	titresCache = map[string]titresEnCache{}
)

// titresCycles: cycle number -> what the pilot's log says that cycle delivered (the end of the log only),
// read again only when the log changes (the page asks every 1.5 s).
func titresCycles(chemin string) map[string]string {
	st, err := os.Stat(chemin)
	if err != nil {
		return map[string]string{}
	}
	titresMu.Lock()
	defer titresMu.Unlock()
	if c, ok := titresCache[chemin]; ok && c.taille == st.Size() && c.date.Equal(st.ModTime()) {
		return c.titres
	}
	out := lireTitresCycles(chemin)
	titresCache[chemin] = titresEnCache{st.Size(), st.ModTime(), out}
	return out
}

func lireTitresCycles(chemin string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(chemin)
	if err != nil {
		return out
	}
	defer f.Close()
	const fin = 4 << 20
	if st, err := f.Stat(); err == nil && st.Size() > fin {
		_, _ = f.Seek(st.Size()-fin, io.SeekStart)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if m := reCycleReussi.FindStringSubmatch(strings.TrimRight(sc.Text(), "\r")); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out
}

// ----------------------------------------------------------------------------- .env

type LigneEnv struct {
	Cle    string `json:"cle"`
	Valeur string `json:"valeur"`
	Aide   string `json:"aide"`
	Secret bool   `json:"secret"`
	brut   string // raw text line when it is not a setting
}

var reCle = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func EstSecret(cle string) bool {
	c := strings.ToUpper(cle)
	return strings.Contains(c, "TOKEN") || strings.Contains(c, "KEY") || strings.Contains(c, "SECRET") || strings.Contains(c, "PASSWORD") || strings.Contains(c, "MOT_DE_PASSE")
}

func LireEnv(chemin string) []LigneEnv {
	b, _ := os.ReadFile(chemin)
	var lignes []LigneEnv
	var aide []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r")
		if i := strings.Index(l, "="); i > 0 {
			cle := strings.TrimSpace(l[:i])
			if reCle.MatchString(cle) {
				lignes = append(lignes, LigneEnv{Cle: cle, Valeur: l[i+1:], Aide: strings.Join(aide, " "), Secret: EstSecret(cle)})
				aide = nil
				continue
			}
		}
		if strings.HasPrefix(l, "#") {
			aide = append(aide, strings.TrimSpace(l[1:]))
		} else if strings.TrimSpace(l) == "" {
			aide = nil
		}
		lignes = append(lignes, LigneEnv{brut: l})
	}
	return lignes
}

// EcrireEnv rewrites the file keeping comments and order; valeurs = key → new value.
func EcrireEnv(chemin string, valeurs map[string]string) error {
	lignes := LireEnv(chemin)
	var sb strings.Builder
	vus := map[string]bool{}
	for _, l := range lignes {
		if l.Cle != "" {
			v := l.Valeur
			if nv, ok := valeurs[l.Cle]; ok {
				v = nv
			}
			vus[l.Cle] = true
			sb.WriteString(l.Cle + "=" + v + "\n")
		} else {
			sb.WriteString(l.brut + "\n")
		}
	}
	for cle, v := range valeurs {
		if !vus[cle] {
			sb.WriteString(cle + "=" + v + "\n")
		}
	}
	return os.WriteFile(chemin, []byte(sb.String()), 0o600)
}

// ----------------------------------------------------------------------------- requests

type DemandeAttente struct {
	Fichier string `json:"fichier"`
	Nom     string `json:"nom"`
	Apercu  string `json:"apercu"`
}

func (p Projet) LireFileAttente() []DemandeAttente {
	entrees, err := os.ReadDir(p.FileAttente())
	if err != nil {
		return nil
	}
	var v []DemandeAttente
	for _, e := range entrees {
		if !strings.HasSuffix(e.Name(), ".md") || strings.EqualFold(e.Name(), "README.fr.md") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(p.FileAttente(), e.Name()))
		apercu := ""
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(strings.TrimLeft(l, "# "))
			if l != "" {
				apercu = l
				break
			}
		}
		if len([]rune(apercu)) > 120 {
			apercu = string([]rune(apercu)[:120]) + "…"
		}
		v = append(v, DemandeAttente{Fichier: e.Name(), Nom: strings.TrimSuffix(e.Name(), ".md"), Apercu: apercu})
	}
	sort.Slice(v, func(i, j int) bool { return v[i].Nom < v[j].Nom })
	return v
}

var reSlug = regexp.MustCompile(`[^a-z0-9àâçéèêëîïôûùüÿñæœ]+`)

func (p Projet) AjouterDemande(titre, texte string) (string, error) {
	if err := os.MkdirAll(p.FileAttente(), 0o755); err != nil {
		return "", err
	}
	slug := strings.Trim(reSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(titre)), "-"), "-")
	if slug == "" {
		slug = "demande"
	}
	if len([]rune(slug)) > 40 {
		slug = string([]rune(slug)[:40])
	}
	// number following the largest existing one: a new request always goes to the end of the queue
	n := 1
	for _, d := range p.LireFileAttente() {
		if i := strings.Index(d.Fichier, "-"); i > 0 {
			if k, err := strconv.Atoi(d.Fichier[:i]); err == nil && k >= n {
				n = k + 1
			}
		}
	}
	var chemin string
	for {
		chemin = filepath.Join(p.FileAttente(), fmt.Sprintf("%02d-%s.md", n, slug))
		if _, err := os.Stat(chemin); err != nil {
			break
		}
		n++
	}
	contenu := texte
	if !strings.HasPrefix(strings.TrimSpace(texte), "#") {
		contenu = "# " + strings.TrimSpace(titre) + "\n\n" + strings.TrimSpace(texte) + "\n"
	}
	return filepath.Base(chemin), os.WriteFile(chemin, []byte(contenu), 0o644)
}

// Reorders the queue: new list of file names in the wanted order (prefixes are renamed).
func (p Projet) ReordonnerFile(ordre []string) error {
	dir := p.FileAttente()
	tmp := []string{}
	for i, f := range ordre {
		src := filepath.Join(dir, f)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		t := filepath.Join(dir, fmt.Sprintf("__%02d.tmp", i))
		if err := os.Rename(src, t); err != nil {
			return err
		}
		tmp = append(tmp, t)
	}
	for i, t := range tmp {
		base := strings.TrimSuffix(ordre[i], ".md")
		if j := strings.Index(base, "-"); j > 0 && strings.Trim(base[:j], "0123456789") == "" {
			base = base[j+1:]
		}
		if err := os.Rename(t, filepath.Join(dir, fmt.Sprintf("%02d-%s.md", i+1, base))); err != nil {
			return err
		}
	}
	return nil
}

func DateCourte() string { return time.Now().Format("2006-01-02 15h04") }

// AjouterRemarque adds a timestamped note in the notes section of REQUEST.md
// (created if absent, in the current language; every variant of the heading is recognised).
func (p Projet) AjouterRemarque(texte, marque, aide string) error {
	b, _ := os.ReadFile(p.Demande())
	s := string(b)
	ligne := "- " + DateCourte() + " : " + strings.ReplaceAll(strings.TrimSpace(texte), "\n", " ") + "\n"
	i := -1
	for _, m := range marqueursRemarques() {
		if k := strings.Index(s, m); k >= 0 {
			i = k
			break
		}
	}
	if i < 0 {
		s += "\n\n" + marque + "\n" + aide + "\n\n" + ligne
		return os.WriteFile(p.Demande(), []byte(s), 0o644)
	}
	pos := i + strings.Index(s[i:], "\n") + 1
	for pos < len(s) { // skip the help line in parentheses and the blank line after it
		fin := strings.Index(s[pos:], "\n")
		if fin < 0 {
			break
		}
		l := strings.TrimSpace(s[pos : pos+fin])
		if strings.HasPrefix(l, "(") || l == "" {
			pos += fin + 1
			continue
		}
		break
	}
	s = s[:pos] + ligne + s[pos:]
	return os.WriteFile(p.Demande(), []byte(s), 0o644)
}

// ----------------------------------------------------------------------------- plan

// Jalon: a checkable line of the plan. Claude writes "M1 : Title — detail… Acceptance: criterion" on a
// single line; it is split for display (short title, detail, acceptance criterion).
type Jalon struct {
	ID          string   `json:"id"` // "M4", "M4_1": the identifier the pilot uses for workers and merges
	Fait        bool     `json:"fait"`
	Titre       string   `json:"titre"`
	Detail      string   `json:"detail"`
	Fichiers    string   `json:"fichiers,omitempty"` // "Files: …" of a dependency plan
	Depend      []string `json:"depend,omitempty"`   // "Depends on: …" identifiers
	Acceptation string   `json:"acceptation"`
	Notes       []string `json:"notes,omitempty"` // indented sub-bullets under the milestone
	Complet     string   `json:"complet"`
	Vague       string   `json:"vague,omitempty"`   // parallel mode: title of the wave ("Wave 2") that starts at this milestone
	Archive     bool     `json:"archive,omitempty"` // done long ago: moved by the pilot to archives/PLAN_TERMINE.md
}

var reIdentJalon = regexp.MustCompile(`^([A-Za-z]+\d+(?:[.\-]?[A-Za-z0-9]+)?)`)

// identJalon normalises a milestone identifier like system/parallel.sh does ("M4.1" → "M4_1").
func identJalon(ligne string) string {
	m := reIdentJalon.FindStringSubmatch(ligne)
	if m == nil {
		return ""
	}
	id := regexp.MustCompile(`[^A-Za-z0-9_]`).ReplaceAllString(m[1], "_")
	if len(id) > 20 {
		id = id[:20]
	}
	return id
}

func decouperJalon(ligne string) Jalon {
	j := Jalon{Complet: ligne, Titre: ligne}
	for _, sep := range []string{" — ", " – ", " : ", ": "} {
		if i := strings.Index(ligne, sep); i > 0 && i < 80 {
			// "M1 : Title — detail": the first cut after the number, not the one of the number itself
			if (sep == " : " || sep == ": ") && len(strings.Fields(ligne[:i])) == 1 {
				if k := strings.Index(ligne[i+len(sep):], sep); k > 0 && i+len(sep)+k < 80 {
					i = i + len(sep) + k
				} else if k := strings.Index(ligne[i+len(sep):], " — "); k > 0 && i+len(sep)+k < 100 {
					i = i + len(sep) + k
					sep = " — "
				} else {
					continue
				}
			}
			j.Titre, j.Detail = strings.TrimSpace(ligne[:i]), strings.TrimSpace(ligne[i+len(sep):])
			break
		}
	}
	for _, m := range []string{"Acceptation :", "Acceptation:", "Acceptance:", "Acceptance :", "Critère :", "Critères :"} {
		if i := strings.Index(j.Detail, m); i >= 0 {
			j.Acceptation = strings.TrimSpace(j.Detail[i+len(m):])
			j.Detail = strings.TrimRight(strings.TrimSpace(j.Detail[:i]), ".;,")
			break
		}
	}
	// dependency plan: "Files: a, b. Depends on: M1, M2." carried in the detail
	if i := strings.Index(j.Detail, "Depends on:"); i >= 0 {
		reste := j.Detail[i+len("Depends on:"):]
		// the list ends at "; " or at a period followed by a space or the end ("M4.1" keeps its dot)
		fin := len(reste)
		for k := 0; k < len(reste); k++ {
			if reste[k] == ';' || (reste[k] == '.' && (k+1 == len(reste) || reste[k+1] == ' ')) {
				fin = k
				break
			}
		}
		for _, d := range strings.FieldsFunc(reste[:fin], func(r rune) bool { return r == ',' || r == ' ' }) {
			if d = identJalon(d); d != "" {
				j.Depend = append(j.Depend, d)
			}
		}
		j.Detail = strings.TrimRight(strings.TrimSpace(j.Detail[:i]), ".;,")
	}
	if i := strings.Index(j.Detail, "Files:"); i >= 0 {
		j.Fichiers = strings.TrimRight(strings.TrimSpace(j.Detail[i+len("Files:"):]), ".;,")
		j.Detail = strings.TrimRight(strings.TrimSpace(j.Detail[:i]), ".;,—–- ")
	}
	j.ID = identJalon(j.Complet)
	return j
}

// LirePlan: the plan's milestones, the archived ones first (the pilot moves the oldest done milestones to
// archives/PLAN_TERMINE.md to keep PLAN.md short: they are still part of the project's plan).
func (p Projet) LirePlan() []Jalon {
	b, err := os.ReadFile(p.Plan())
	if err != nil {
		return nil
	}
	plan := analyserPlan(string(b))
	a, err := os.ReadFile(filepath.Join(p.Atelier(), "archives", "PLAN_TERMINE.md"))
	if err != nil || len(a) > 8<<20 {
		return plan
	}
	presents := map[string]bool{}
	for _, j := range plan {
		if j.ID != "" {
			presents[j.ID] = true
		}
	}
	var v []Jalon
	for _, j := range analyserPlan(string(a)) {
		if !j.Fait || (j.ID != "" && presents[j.ID]) {
			continue
		}
		if j.ID != "" {
			presents[j.ID] = true
		}
		j.Archive, j.Vague = true, ""
		v = append(v, j)
	}
	return append(v, plan...)
}

func analyserPlan(texte string) []Jalon {
	var v []Jalon
	vague := ""
	for _, l := range strings.Split(texte, "\n") {
		t := strings.TrimLeft(l, " \t")
		switch {
		case strings.HasPrefix(t, "## ") && (strings.HasPrefix(strings.ToLower(t), "## wave") || strings.HasPrefix(strings.ToLower(t), "## vague")):
			vague = strings.TrimSpace(t[3:])
		case (strings.HasPrefix(t, "- [x] ") || strings.HasPrefix(t, "- [X] ") || strings.HasPrefix(t, "- [ ] ")) && len(l) > 0 && l[0] != ' ' && l[0] != '\t':
			j := decouperJalon(strings.TrimSpace(t[6:]))
			j.Fait = t[3] != ' '
			j.Vague, vague = vague, ""
			v = append(v, j)
		case len(v) > 0 && len(l) > 0 && (l[0] == ' ' || l[0] == '\t') && strings.TrimSpace(l) != "":
			// indented line under the last milestone: a sub-bullet (criteria, files, notes)
			note := strings.TrimSpace(l)
			note = strings.TrimPrefix(strings.TrimPrefix(note, "- "), "* ")
			v[len(v)-1].Notes = append(v[len(v)-1].Notes, note)
		}
	}
	return v
}

func QueueFichier(chemin string, n int) []string {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return nil
	}
	lignes := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lignes) > n {
		lignes = lignes[len(lignes)-n:]
	}
	return lignes
}

// MembreEquipe: a sub-agent defined in template/.claude/agents/<name>.md (name / description header).
type MembreEquipe struct {
	Nom     string
	Mission string
	Modele  string // "model:" set in the agent file, if any
}

func (k Kit) Equipe() []MembreEquipe {
	entrees, err := os.ReadDir(filepath.Join(k.Racine, "template", ".claude", "agents"))
	if err != nil {
		return nil
	}
	var v []MembreEquipe
	for _, e := range entrees {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(k.Racine, "template", ".claude", "agents", e.Name()))
		if err != nil {
			continue
		}
		m := MembreEquipe{Nom: strings.TrimSuffix(e.Name(), ".md")}
		for _, l := range strings.SplitN(string(b), "\n", 12) {
			if strings.HasPrefix(l, "name:") {
				m.Nom = strings.TrimSpace(l[5:])
			} else if strings.HasPrefix(l, "description:") {
				m.Mission = strings.TrimSpace(l[12:])
			} else if strings.HasPrefix(l, "model:") {
				m.Modele = strings.TrimSpace(l[6:])
			}
		}
		v = append(v, m)
	}
	return v
}

// anciennesCles: French names of the settings (previous versions) and their current names.
var anciennesCles = map[string]string{
	"MODELE": "MODEL", "MODELE_SECOURS": "FALLBACK_MODEL", "MODELE_SOUS_AGENTS": "SUBAGENT_MODEL", "MODELE_TACHES_SIMPLES": "CHEAP_MODEL",
	"LANGUE": "LANGUAGE", "DUREE_MAX_HEURES": "MAX_HOURS", "PAUSE_QUOTA_MIN": "QUOTA_WAIT_MIN", "INACTIVITE_MAX_MIN": "IDLE_MAX_MIN",
	"CLORE_APRES_JALON": "CLOSE_AFTER_MILESTONE", "PARALLELE": "PARALLEL", "PARALLELE_MAX": "PARALLEL_MAX",
}

// MigrerEnv renames the old keys of a .env file in place; no effect when there are none.
func MigrerEnv(chemin string) {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return
	}
	lignes := strings.Split(string(b), "\n")
	change := false
	for i, l := range lignes {
		if k := strings.IndexByte(l, '='); k > 0 {
			if n, ok := anciennesCles[strings.TrimSpace(l[:k])]; ok {
				lignes[i] = n + l[k:]
				change = true
			}
		}
	}
	if change {
		_ = os.WriteFile(chemin, []byte(strings.Join(lignes, "\n")), 0o600)
	}
}
