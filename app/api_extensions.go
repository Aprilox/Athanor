package main

// Skills & MCP page: a library of skills and MCP servers kept in the tool folder (extensions/), and the
// choice of what each project uses. The pilot reads the same files at the start of every cycle
// (system/extensions.py): skills become a plugin loaded with --plugin-dir, MCP servers go into the
// session's MCP configuration, the local ones in a cage (system/mcp-jail.sh).
//
// Files (all out of Claude's reach: the sandbox only opens the project and tools/):
//   extensions/bibliotheque.json   what is installed
//   extensions/skills/<id>/         the skill folders
//   extensions/projets.json         {project folder: {"skills": [...], "mcp": [...]}}
//   extensions/secrets.json         {mcp id: {VARIABLE: value}}, never sent back to the interface

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed catalogue.json
var catalogueJSON []byte

type texteLangues map[string]string

func (t texteLangues) en(langue string) string {
	if v := t[langue]; v != "" {
		return v
	}
	return t["en"]
}

type SecretCatalogue struct {
	Nom       string `json:"nom"`
	Optionnel bool   `json:"optionnel"`
	Lien      string `json:"lien,omitempty"`
}

type ConfigMCP struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type Catalogue struct {
	Skills []struct {
		ID          string       `json:"id"`
		Nom         string       `json:"nom"`
		Depot       string       `json:"depot"`
		Branche     string       `json:"branche"`
		Chemin      string       `json:"chemin"`
		Licence     string       `json:"licence"`
		Scripts     bool         `json:"scripts"`
		Description texteLangues `json:"description"`
	} `json:"skills"`
	MCP []struct {
		ID          string            `json:"id"`
		Nom         string            `json:"nom"`
		Depot       string            `json:"depot"`
		Licence     string            `json:"licence"`
		Prerequis   string            `json:"prerequis"`
		Config      ConfigMCP         `json:"config"`
		Secrets     []SecretCatalogue `json:"secrets"`
		Description texteLangues      `json:"description"`
	} `json:"mcp"`
}

func lireCatalogue() Catalogue {
	var c Catalogue
	_ = json.Unmarshal(catalogueJSON, &c)
	return c
}

type SkillInstalle struct {
	Nom         string `json:"nom"`
	Description string `json:"description"`
	Source      string `json:"source"`    // "github.com/owner/repo/path", or "" for a skill written here
	Catalogue   string `json:"catalogue"` // id in the verified catalog, if it comes from there
	Scripts     bool   `json:"scripts"`
	Ajoute      string `json:"ajoute"`
}

type MCPInstalle struct {
	Nom         string            `json:"nom"`
	Description string            `json:"description"`
	Source      string            `json:"source"`
	Catalogue   string            `json:"catalogue"`
	Config      ConfigMCP         `json:"config"`
	Optionnels  []string          `json:"optionnels,omitempty"` // keys the server works without
	Liens       map[string]string `json:"liens,omitempty"`      // where to get each key
	Ajoute      string            `json:"ajoute"`
}

type Bibliotheque struct {
	Skills map[string]SkillInstalle `json:"skills"`
	MCP    map[string]MCPInstalle   `json:"mcp"`
}

type ChoixProjet struct {
	Skills []string `json:"skills"`
	MCP    []string `json:"mcp"`
}

var extMu sync.Mutex // the library files: one writer at a time

func dossierExt(k Kit) string { return filepath.Join(k.Racine, "extensions") }

func lireJSONFichier(chemin string, v any) {
	if b, err := os.ReadFile(chemin); err == nil {
		_ = json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), v)
	}
}

func ecrireJSONFichier(chemin string, v any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	t := chemin + ".tmp"
	if err := os.WriteFile(t, b, mode); err != nil {
		return err
	}
	return os.Rename(t, chemin)
}

func lireBiblio(k Kit) Bibliotheque {
	b := Bibliotheque{}
	lireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), &b)
	if b.Skills == nil {
		b.Skills = map[string]SkillInstalle{}
	}
	if b.MCP == nil {
		b.MCP = map[string]MCPInstalle{}
	}
	return b
}

func lireChoix(k Kit) map[string]ChoixProjet {
	c := map[string]ChoixProjet{}
	lireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), &c)
	return c
}

func lireSecrets(k Kit) map[string]map[string]string {
	c := map[string]map[string]string{}
	lireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), &c)
	return c
}

var reIDExt = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// identExt turns a name into a library id (the folder name of a skill, the key of a server).
func identExt(nom string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(nom)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	id := strings.Trim(regexp.MustCompile(`-+`).ReplaceAllString(b.String(), "-"), "-._")
	if len(id) > 64 {
		id = strings.Trim(id[:64], "-._")
	}
	return id
}

// cleProjet: the project's entry in projets.json (the folder as the app knows it).
func cleProjet(p Projet) string { return p.Dossier }

func contient(l []string, x string) bool {
	for _, v := range l {
		if v == x {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------- SKILL.md

// enTete reads the name and description of a SKILL.md (YAML front matter, simple and folded values).
func enTete(texte string) (nom, description string) {
	texte = strings.TrimPrefix(strings.ReplaceAll(texte, "\r\n", "\n"), "\uFEFF")
	if !strings.HasPrefix(texte, "---\n") {
		return "", ""
	}
	fin := strings.Index(texte[4:], "\n---")
	if fin < 0 {
		return "", ""
	}
	lignes := strings.Split(texte[4:4+fin], "\n")
	valeur := func(i int, v string) string {
		v = strings.TrimSpace(v)
		if v == ">" || v == "|" || v == ">-" || v == "|-" || v == "" {
			var suite []string
			for j := i + 1; j < len(lignes) && (strings.HasPrefix(lignes[j], " ") || strings.HasPrefix(lignes[j], "\t")); j++ {
				suite = append(suite, strings.TrimSpace(lignes[j]))
			}
			v = strings.Join(suite, " ")
		}
		return strings.Trim(v, `"'`)
	}
	for i, l := range lignes {
		if strings.HasPrefix(l, "name:") {
			nom = valeur(i, l[5:])
		} else if strings.HasPrefix(l, "description:") {
			description = valeur(i, l[12:])
		}
	}
	return nom, description
}

var extensionsScripts = map[string]bool{".py": true, ".sh": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".ps1": true, ".bat": true, ".cmd": true, ".exe": true, ".go": true, ".php": true}

// ----------------------------------------------------------------------------- GitHub

var (
	githubAPI     = "https://api.github.com"
	githubArchive = "https://github.com" // …/OWNER/REPO/archive/REF.zip (documented, redirects to codeload)
	clientGitHub  = &http.Client{Timeout: 3 * time.Minute}
)

func init() {
	// tests only: a local copy of GitHub
	if v := os.Getenv("ATHANOR_GITHUB_API"); v != "" {
		githubAPI = v
	}
	if v := os.Getenv("ATHANOR_GITHUB_ARCHIVE"); v != "" {
		githubArchive = v
	}
}

const (
	maxArchive      = 150 << 20 // a whole repository, zipped
	maxSkill        = 20 << 20  // one skill folder, unpacked
	maxFichiersSkil = 1000
)

type lienGH struct{ Proprio, Depot, Ref, Chemin string }

var reNomGH = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// lireLienGitHub accepts github.com/OWNER/REPO, …/tree/REF/PATH and …/blob/REF/PATH/SKILL.md.
func lireLienGitHub(u string) (lienGH, error) {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	u = strings.TrimPrefix(u, "www.")
	if !strings.HasPrefix(strings.ToLower(u), "github.com/") {
		return lienGH{}, errors.New("Paste a link to a GitHub repository or folder (https://github.com/…)")
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	p := strings.Split(strings.Trim(u[len("github.com/"):], "/"), "/")
	if len(p) < 2 || !reNomGH.MatchString(p[0]) || !reNomGH.MatchString(strings.TrimSuffix(p[1], ".git")) {
		return lienGH{}, errors.New("Paste a link to a GitHub repository or folder (https://github.com/…)")
	}
	l := lienGH{Proprio: p[0], Depot: strings.TrimSuffix(p[1], ".git")}
	if len(p) >= 4 && (p[2] == "tree" || p[2] == "blob") {
		l.Ref = p[3]
		reste := p[4:]
		if p[2] == "blob" && len(reste) > 0 {
			reste = reste[:len(reste)-1] // the folder of the file
		}
		l.Chemin = strings.Join(reste, "/")
	}
	for _, s := range strings.Split(l.Chemin, "/") {
		if s == ".." {
			return lienGH{}, errors.New("Paste a link to a GitHub repository or folder (https://github.com/…)")
		}
	}
	return l, nil
}

func telecharger(url string, max int64) ([]byte, error) {
	r, err := clientGitHub.Get(url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub answered %d", r.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("Repository too large")
	}
	return b, nil
}

// archiveGitHub downloads the repository at the link's ref (default branch if none: GitHub API,
// failing that main, then master).
// A few archives stay in memory for ten minutes: a preview then an install download only once.
type archiveMemo struct {
	octets []byte
	ref    string
	quand  time.Time
}

var (
	memoMu       sync.Mutex
	memoArchives = map[string]archiveMemo{}
)

func archiveGitHub(l *lienGH) (*zip.Reader, error) {
	cle := strings.ToLower(l.Proprio + "/" + l.Depot + "@" + l.Ref)
	memoMu.Lock()
	for k, m := range memoArchives {
		if time.Since(m.quand) > 10*time.Minute {
			delete(memoArchives, k)
		}
	}
	m, ok := memoArchives[cle]
	memoMu.Unlock()
	if ok {
		l.Ref = m.ref
		return zip.NewReader(bytes.NewReader(m.octets), int64(len(m.octets)))
	}
	octets, err := telechargerArchive(l)
	if err != nil {
		return nil, err
	}
	memoMu.Lock()
	if len(memoArchives) >= 4 {
		for k := range memoArchives {
			delete(memoArchives, k)
			break
		}
	}
	memoArchives[cle] = archiveMemo{octets, l.Ref, time.Now()}
	memoMu.Unlock()
	return zip.NewReader(bytes.NewReader(octets), int64(len(octets)))
}

func telechargerArchive(l *lienGH) ([]byte, error) {
	refs := []string{l.Ref}
	if l.Ref == "" {
		refs = []string{"main", "master"}
		if b, err := telecharger(githubAPI+"/repos/"+l.Proprio+"/"+l.Depot, 1<<20); err == nil {
			var info struct {
				Branche string `json:"default_branch"`
			}
			if json.Unmarshal(b, &info) == nil && reNomGH.MatchString(info.Branche) {
				refs = []string{info.Branche}
			}
		}
	}
	var dernier error
	for _, ref := range refs {
		b, err := telecharger(githubArchive+"/"+l.Proprio+"/"+l.Depot+"/archive/"+ref+".zip", maxArchive)
		if err != nil {
			dernier = err
			continue
		}
		if _, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
			return nil, err
		}
		l.Ref = ref
		return b, nil
	}
	return nil, fmt.Errorf("Cannot download the repository: %v", dernier)
}

// cheminDansArchive: the path of an entry without the archive's root folder ("repo-main/").
func cheminDansArchive(nom string) string {
	if i := strings.Index(nom, "/"); i >= 0 {
		return nom[i+1:]
	}
	return ""
}

type SkillTrouve struct {
	Chemin      string `json:"chemin"`
	Nom         string `json:"nom"`
	Description string `json:"description"`
	Scripts     bool   `json:"scripts"`
	ID          string `json:"id"`
	Installe    bool   `json:"installe"`
}

// skillsDansArchive lists the skill folders (with a SKILL.md) under the link's path.
func skillsDansArchive(z *zip.Reader, base string) []SkillTrouve {
	base = strings.Trim(base, "/")
	var trouves []SkillTrouve
	for _, f := range z.File {
		c := cheminDansArchive(f.Name)
		if path.Base(c) != "SKILL.md" || (base != "" && c != base+"/SKILL.md" && !strings.HasPrefix(c, base+"/")) || f.UncompressedSize64 > 512<<10 {
			continue
		}
		dossier := path.Dir(c)
		if dossier == "." {
			dossier = ""
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 512<<10))
		rc.Close()
		nom, desc := enTete(string(b))
		if nom == "" {
			nom = path.Base(dossier)
			if dossier == "" {
				nom = ""
			}
		}
		t := SkillTrouve{Chemin: dossier, Nom: nom, Description: desc, ID: identExt(nom)}
		for _, g := range z.File {
			gc := cheminDansArchive(g.Name)
			if (dossier == "" || strings.HasPrefix(gc, dossier+"/")) && extensionsScripts[strings.ToLower(path.Ext(gc))] {
				t.Scripts = true
				break
			}
		}
		if t.ID != "" {
			trouves = append(trouves, t)
		}
	}
	sort.Slice(trouves, func(i, j int) bool { return trouves[i].Chemin < trouves[j].Chemin })
	return trouves
}

// extraireSkill copies a skill folder of the archive to extensions/skills/<id> (replacing an older
// copy). No links, no path leaving the folder, size and file count limited.
func extraireSkill(k Kit, z *zip.Reader, dossier, id string) error {
	if !reIDExt.MatchString(id) {
		return errors.New("Invalid name")
	}
	racine := filepath.Join(dossierExt(k), "skills")
	tmp := filepath.Join(racine, "."+id+".tmp")
	_ = os.RemoveAll(tmp)
	var total int64
	n := 0
	prefixe := ""
	if dossier != "" {
		prefixe = dossier + "/"
	}
	for _, f := range z.File {
		c := cheminDansArchive(f.Name)
		if !strings.HasPrefix(c, prefixe) || strings.HasSuffix(c, "/") {
			continue
		}
		rel := strings.TrimPrefix(c, prefixe)
		if rel == "" || strings.Contains(rel, "..") || path.IsAbs(rel) || strings.HasPrefix(rel, ".git/") {
			continue
		}
		if !f.Mode().IsRegular() {
			continue // links and special files are not copied
		}
		n++
		total += int64(f.UncompressedSize64)
		if n > maxFichiersSkil || total > maxSkill {
			_ = os.RemoveAll(tmp)
			return errors.New("This skill is too large (more than 20 MB or 1000 files)")
		}
		dest := filepath.Join(tmp, filepath.FromSlash(rel))
		if !strings.HasPrefix(dest, tmp+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, maxSkill))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "SKILL.md")); err != nil {
		_ = os.RemoveAll(tmp)
		return errors.New("No SKILL.md in this folder")
	}
	final := filepath.Join(racine, id)
	_ = os.RemoveAll(final)
	return os.Rename(tmp, final)
}

// ----------------------------------------------------------------------------- MCP import

var reVarSecret = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// estNomSecret: the name of an API key, token or password (API_KEY, GITHUB_PAT, x-api-key…), not
// MEMORY_FILE_PATH or NODE_ENV.
func estNomSecret(nom string) bool {
	for _, m := range regexp.MustCompile(`[A-Za-z0-9]+`).FindAllString(strings.ToUpper(nom), -1) {
		switch m {
		case "KEY", "APIKEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PWD", "AUTH", "AUTHORIZATION", "PAT", "BEARER", "CREDENTIAL", "CREDENTIALS":
			return true
		}
		if strings.HasSuffix(m, "KEY") || strings.HasSuffix(m, "TOKEN") || strings.HasSuffix(m, "SECRET") {
			return true
		}
	}
	return false
}

var reFactice = regexp.MustCompile(`(?i)^(<.*>|\[.*\]|your[ _-].*|your_.*|.*_here|x{3,}|\.\.\.|changeme|replace.*|)$`)

// separerSecrets moves the secret values of a pasted configuration (API keys in env or headers) into
// the secrets file: the library keeps "${NAME}". Placeholders ("YOUR_API_KEY", "<key>") become a key
// to fill in.
func separerSecrets(c *ConfigMCP) map[string]string {
	secrets := map[string]string{}
	traiter := func(cle, v string, entete bool) string {
		if reVarSecret.MatchString(v) {
			return v
		}
		nom := strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(cle, "_"))
		if !estNomSecret(cle) {
			return v
		}
		valeur := v
		prefixe := ""
		if entete && strings.HasPrefix(strings.ToLower(v), "bearer ") {
			prefixe, valeur = v[:7], strings.TrimSpace(v[7:])
		}
		if !reFactice.MatchString(valeur) {
			secrets[nom] = valeur
		}
		return prefixe + "${" + nom + "}"
	}
	for k, v := range c.Env {
		c.Env[k] = traiter(k, v, false)
	}
	for k, v := range c.Headers {
		c.Headers[k] = traiter(k, v, true)
	}
	return secrets
}

// decouperCommande splits a shell-like command line (quotes and backslashes).
func decouperCommande(s string) []string {
	var out []string
	var b strings.Builder
	dans, guillemet, echappe := false, rune(0), false
	for _, r := range s {
		switch {
		case echappe:
			b.WriteRune(r)
			echappe, dans = false, true
		case r == '\\' && guillemet != '\'':
			echappe = true
		case guillemet != 0:
			if r == guillemet {
				guillemet = 0
			} else {
				b.WriteRune(r)
			}
		case r == '"' || r == '\'':
			guillemet, dans = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if dans {
				out = append(out, b.String())
				b.Reset()
				dans = false
			}
		default:
			b.WriteRune(r)
			dans = true
		}
	}
	if dans {
		out = append(out, b.String())
	}
	return out
}

// lireConfigCollee understands what MCP READMEs give for Claude Code: a JSON block
// ({"mcpServers": {...}}, {"name": {...}} or one server), or a `claude mcp add …` command line.
func lireConfigCollee(texte, nomDonne string) (map[string]ConfigMCP, error) {
	texte = strings.TrimSpace(texte)
	serveurs := map[string]ConfigMCP{}
	if strings.HasPrefix(texte, "{") {
		var brut map[string]json.RawMessage
		if err := json.Unmarshal([]byte(texte), &brut); err != nil {
			return nil, errors.New("This JSON cannot be read: check the braces and quotes")
		}
		if m, ok := brut["mcpServers"]; ok {
			_ = json.Unmarshal(m, &brut)
		} else if _, ok := brut["command"]; ok {
			brut = map[string]json.RawMessage{nomDonne: json.RawMessage(texte)}
		} else if _, ok := brut["url"]; ok {
			brut = map[string]json.RawMessage{nomDonne: json.RawMessage(texte)}
		}
		for nom, v := range brut {
			var c ConfigMCP
			if json.Unmarshal(v, &c) == nil && (c.Command != "" || c.URL != "") {
				serveurs[nom] = c
			}
		}
	} else {
		t := decouperCommande(strings.ReplaceAll(texte, "\\\n", " "))
		if len(t) < 3 || t[0] != "claude" || t[1] != "mcp" || (t[2] != "add" && t[2] != "add-json") {
			return nil, errors.New("Paste the JSON block or the “claude mcp add …” command given in the server's README")
		}
		if t[2] == "add-json" {
			var reste []string
			for _, x := range t[3:] {
				if !strings.HasPrefix(x, "-") {
					reste = append(reste, x)
				}
			}
			if len(reste) < 2 {
				return nil, errors.New("Paste the JSON block or the “claude mcp add …” command given in the server's README")
			}
			return lireConfigCollee(reste[1], reste[0])
		}
		c := ConfigMCP{Env: map[string]string{}, Headers: map[string]string{}}
		nom := ""
		var reste []string
		i := 3
		for ; i < len(t); i++ {
			x := t[i]
			if x == "--" {
				reste = append(reste, t[i+1:]...)
				break
			}
			valeur := func() string {
				if i+1 < len(t) {
					i++
					return t[i]
				}
				return ""
			}
			switch {
			case x == "--transport" || x == "-t":
				c.Type = valeur()
			case strings.HasPrefix(x, "--transport="):
				c.Type = strings.TrimPrefix(x, "--transport=")
			case x == "--env" || x == "-e":
				if kv := strings.SplitN(valeur(), "=", 2); len(kv) == 2 {
					c.Env[kv[0]] = kv[1]
				}
			case x == "--header" || x == "-H":
				if kv := strings.SplitN(valeur(), ":", 2); len(kv) == 2 {
					c.Headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
				}
			case x == "--scope" || x == "-s":
				valeur()
			case strings.HasPrefix(x, "-"):
			default:
				if nom == "" {
					nom = x
				} else {
					reste = append(reste, x)
				}
			}
		}
		if nom == "" || len(reste) == 0 {
			return nil, errors.New("Paste the JSON block or the “claude mcp add …” command given in the server's README")
		}
		if c.Type == "http" || c.Type == "sse" || (c.Type == "" && regexp.MustCompile(`^https?://`).MatchString(reste[0])) {
			if c.Type == "" {
				c.Type = "http"
			}
			c.URL = reste[0]
		} else {
			c.Type, c.Command, c.Args = "stdio", reste[0], reste[1:]
		}
		serveurs[nom] = c
	}
	if len(serveurs) == 0 {
		return nil, errors.New("No MCP server found in this text")
	}
	return serveurs, nil
}

func verifierConfig(c ConfigMCP) error {
	if c.URL != "" {
		if !regexp.MustCompile(`^https?://[^\s]+$`).MatchString(c.URL) {
			return errors.New("Invalid server address")
		}
		if c.Type == "" {
			c.Type = "http"
		}
		return nil
	}
	if strings.TrimSpace(c.Command) == "" || strings.ContainsAny(c.Command, "\n\r") {
		return errors.New("No command to start the server")
	}
	return nil
}

func secretsRequis(c ConfigMCP) []string {
	vus := map[string]bool{}
	var out []string
	ajouter := func(s string) {
		for _, m := range reVarSecret.FindAllStringSubmatch(s, -1) {
			if !vus[m[1]] {
				vus[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	ajouter(c.URL)
	ajouter(c.Command)
	for _, a := range c.Args {
		ajouter(a)
	}
	for _, v := range c.Env {
		ajouter(v)
	}
	for _, v := range c.Headers {
		ajouter(v)
	}
	sort.Strings(out)
	return out
}

// ----------------------------------------------------------------------------- views

type SecretVue struct {
	Nom       string `json:"nom"`
	Defini    bool   `json:"defini"`
	Optionnel bool   `json:"optionnel"`
	Lien      string `json:"lien,omitempty"`
}

type ExtVue struct {
	ID          string      `json:"id"`
	Nom         string      `json:"nom"`
	Description string      `json:"description"`
	Source      string      `json:"source"`
	Catalogue   string      `json:"catalogue"`
	Scripts     bool        `json:"scripts,omitempty"`
	Actif       bool        `json:"actif"`
	Type        string      `json:"type,omitempty"`
	Resume      string      `json:"resume,omitempty"`
	Secrets     []SecretVue `json:"secrets,omitempty"`
}

func (s *Serveur) apiExtensions(w http.ResponseWriter, r *http.Request) {
	k := s.kitCourant()
	langue := s.langue()
	extMu.Lock()
	b, choix, secrets := lireBiblio(k), lireChoix(k), lireSecrets(k)
	extMu.Unlock()
	cat := lireCatalogue()
	descCat := map[string]string{}
	for _, c := range cat.Skills {
		descCat["skill:"+c.ID] = c.Description.en(langue)
	}
	for _, c := range cat.MCP {
		descCat["mcp:"+c.ID] = c.Description.en(langue)
	}
	p, avecProjet := s.projet(r)
	ch := ChoixProjet{}
	if avecProjet {
		ch = choix[cleProjet(p)]
	}
	skills, mcp := []ExtVue{}, []ExtVue{}
	for id, x := range b.Skills {
		v := ExtVue{ID: id, Nom: x.Nom, Description: x.Description, Source: x.Source, Catalogue: x.Catalogue, Scripts: x.Scripts, Actif: contient(ch.Skills, id)}
		if d := descCat["skill:"+x.Catalogue]; d != "" {
			v.Description = d
		}
		skills = append(skills, v)
	}
	for id, x := range b.MCP {
		v := ExtVue{ID: id, Nom: x.Nom, Description: x.Description, Source: x.Source, Catalogue: x.Catalogue, Actif: contient(ch.MCP, id), Type: x.Config.Type}
		if d := descCat["mcp:"+x.Catalogue]; d != "" {
			v.Description = d
		}
		if x.Config.URL != "" {
			v.Resume = reVarSecret.ReplaceAllString(x.Config.URL, "…")
			if v.Type == "" {
				v.Type = "http"
			}
		} else {
			v.Resume = strings.TrimSpace(x.Config.Command + " " + strings.Join(x.Config.Args, " "))
			v.Type = "stdio"
		}
		for _, nom := range secretsRequis(x.Config) {
			v.Secrets = append(v.Secrets, SecretVue{Nom: nom, Defini: secrets[id][nom] != "", Optionnel: contient(x.Optionnels, nom), Lien: x.Liens[nom]})
		}
		mcp = append(mcp, v)
	}
	sort.Slice(skills, func(i, j int) bool { return strings.ToLower(skills[i].Nom) < strings.ToLower(skills[j].Nom) })
	sort.Slice(mcp, func(i, j int) bool { return strings.ToLower(mcp[i].Nom) < strings.ToLower(mcp[j].Nom) })
	type carte struct {
		ID          string            `json:"id"`
		Nom         string            `json:"nom"`
		Description string            `json:"description"`
		Depot       string            `json:"depot"`
		Licence     string            `json:"licence"`
		Scripts     bool              `json:"scripts,omitempty"`
		Prerequis   string            `json:"prerequis,omitempty"`
		Secrets     []SecretCatalogue `json:"secrets,omitempty"`
		Installe    bool              `json:"installe"`
	}
	installes := map[string]bool{}
	for id, x := range b.Skills {
		installes["skill:"+x.Catalogue] = true
		installes["skill:"+id] = true // the same skill installed from its GitHub link
	}
	for _, x := range b.MCP {
		installes["mcp:"+x.Catalogue] = true
	}
	var cs, cm []carte
	for _, c := range cat.Skills {
		cs = append(cs, carte{c.ID, c.Nom, c.Description.en(langue), c.Depot, c.Licence, c.Scripts, "", nil, installes["skill:"+c.ID]})
	}
	for _, c := range cat.MCP {
		cm = append(cm, carte{c.ID, c.Nom, c.Description.en(langue), c.Depot, c.Licence, false, c.Prerequis, c.Secrets, installes["mcp:"+c.ID]})
	}
	nomProjet := ""
	if avecProjet {
		nomProjet = p.Nom
	}
	s.ecrireJSON(w, map[string]any{"projet": nomProjet, "skills": skills, "mcp": mcp, "catalogue": map[string]any{"skills": cs, "mcp": cm}})
}

// ----------------------------------------------------------------------------- actions

func maintenantISO() string { return time.Now().Format("2006-01-02 15:04") }

func (s *Serveur) apiExtCatalogue(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	k := s.kitCourant()
	cat := lireCatalogue()
	if c.Type == "mcp" {
		for _, m := range cat.MCP {
			if m.ID != c.ID {
				continue
			}
			x := MCPInstalle{Nom: m.Nom, Source: "github.com/" + m.Depot, Catalogue: m.ID, Config: m.Config, Liens: map[string]string{}, Ajoute: maintenantISO()}
			for _, sec := range m.Secrets {
				if sec.Optionnel {
					x.Optionnels = append(x.Optionnels, sec.Nom)
				}
				if sec.Lien != "" {
					x.Liens[sec.Nom] = sec.Lien
				}
			}
			extMu.Lock()
			b := lireBiblio(k)
			b.MCP[m.ID] = x
			err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
			extMu.Unlock()
			if err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", m.Nom)
			return
		}
	} else {
		for _, sk := range cat.Skills {
			if sk.ID != c.ID {
				continue
			}
			parts := strings.SplitN(sk.Depot, "/", 2)
			l := lienGH{Proprio: parts[0], Depot: parts[1], Ref: sk.Branche, Chemin: sk.Chemin}
			z, err := archiveGitHub(&l)
			if err != nil {
				s.erreur(w, http.StatusBadGateway, err.Error())
				return
			}
			if err := s.installerSkill(k, z, sk.Chemin, sk.ID, sk.Nom, "github.com/"+sk.Depot+"/tree/"+l.Ref+"/"+sk.Chemin, sk.ID); err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", sk.Nom)
			return
		}
	}
	s.erreur(w, http.StatusNotFound, "Unknown")
}

func (s *Serveur) installerSkill(k Kit, z *zip.Reader, dossier, id, nom, source, catalogue string) error {
	extMu.Lock()
	defer extMu.Unlock()
	if err := extraireSkill(k, z, dossier, id); err != nil {
		return err
	}
	texte, _ := os.ReadFile(filepath.Join(dossierExt(k), "skills", id, "SKILL.md"))
	n, desc := enTete(string(texte))
	if nom == "" {
		nom = n
	}
	scripts := false
	_ = filepath.WalkDir(filepath.Join(dossierExt(k), "skills", id), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && extensionsScripts[strings.ToLower(filepath.Ext(p))] {
			scripts = true
		}
		return nil
	})
	b := lireBiblio(k)
	b.Skills[id] = SkillInstalle{Nom: nom, Description: desc, Source: source, Catalogue: catalogue, Scripts: scripts, Ajoute: maintenantISO()}
	return ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
}

// apiExtGitHub: without "chemins", lists the skills found behind the link; with, installs them.
func (s *Serveur) apiExtGitHub(w http.ResponseWriter, r *http.Request) {
	var c struct {
		URL     string   `json:"url"`
		Chemins []string `json:"chemins"`
		Nom     string   `json:"nom"`    // a search result: the skill of that name in the repository
		Apercu  bool     `json:"apercu"` // with nom: only return its SKILL.md
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err := lireLienGitHub(c.URL)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	z, err := archiveGitHub(&l)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, err.Error())
		return
	}
	k := s.kitCourant()
	trouves := skillsDansArchive(z, l.Chemin)
	if len(trouves) == 0 {
		s.erreur(w, http.StatusNotFound, "No skill (SKILL.md) in this repository or folder. For an MCP server, paste its configuration in the MCP tab.")
		return
	}
	if c.Nom != "" {
		// a search result points to a repository (or a folder of it): its skill of that name, or the only one
		// several copies of the same skill (skills/x and .tool/skills/x): the visible, shortest path wins
		var choisi *SkillTrouve
		cache := func(c string) bool { return strings.HasPrefix(c, ".") || strings.Contains(c, "/.") }
		for i := range trouves {
			if trouves[i].ID != identExt(c.Nom) && !strings.EqualFold(trouves[i].Nom, c.Nom) {
				continue
			}
			if choisi == nil || (cache(choisi.Chemin) && !cache(trouves[i].Chemin)) ||
				(cache(choisi.Chemin) == cache(trouves[i].Chemin) && len(trouves[i].Chemin) < len(choisi.Chemin)) {
				choisi = &trouves[i]
			}
		}
		if choisi == nil && len(trouves) == 1 {
			choisi = &trouves[0]
		}
		if choisi == nil {
			s.erreur(w, http.StatusNotFound, "This skill was not found in its repository (moved or renamed)")
			return
		}
		if c.Apercu {
			texte := ""
			for _, f := range z.File {
				if cheminDansArchive(f.Name) == strings.TrimPrefix(choisi.Chemin+"/SKILL.md", "/") {
					if rc, err := f.Open(); err == nil {
						b, _ := io.ReadAll(io.LimitReader(rc, 512<<10))
						rc.Close()
						texte = string(b)
					}
				}
			}
			s.ecrireJSON(w, map[string]any{"texte": texte, "chemin": choisi.Chemin, "scripts": choisi.Scripts})
			return
		}
		c.Chemins = []string{choisi.Chemin}
	}
	if len(c.Chemins) == 0 {
		extMu.Lock()
		b := lireBiblio(k)
		extMu.Unlock()
		for i := range trouves {
			_, trouves[i].Installe = b.Skills[trouves[i].ID]
		}
		s.ecrireJSON(w, map[string]any{"depot": l.Proprio + "/" + l.Depot, "ref": l.Ref, "skills": trouves})
		return
	}
	n := 0
	for _, t := range trouves {
		if !contient(c.Chemins, t.Chemin) {
			continue
		}
		source := "github.com/" + l.Proprio + "/" + l.Depot
		if t.Chemin != "" {
			source += "/tree/" + l.Ref + "/" + t.Chemin
		}
		if err := s.installerSkill(k, z, t.Chemin, t.ID, t.Nom, source, ""); err != nil {
			s.erreur(w, http.StatusInternalServerError, t.Nom+": "+err.Error())
			return
		}
		n++
	}
	s.msg(w, "{0} skill(s) installed. Switch them on for the projects that need them.", itoa(int64(n)))
}

// apiExtSkillPerso writes a skill by hand (or saves the edited SKILL.md of an installed one).
func (s *Serveur) apiExtSkillPerso(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID          string `json:"id"` // set: edit this skill's SKILL.md
		Nom         string `json:"nom"`
		Description string `json:"description"`
		Contenu     string `json:"contenu"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b := lireBiblio(k)
	if c.ID != "" {
		x, ok := b.Skills[c.ID]
		if !ok || !reIDExt.MatchString(c.ID) || len(c.Contenu) > 512<<10 {
			s.erreur(w, http.StatusNotFound, "Unknown")
			return
		}
		if err := os.WriteFile(filepath.Join(dossierExt(k), "skills", c.ID, "SKILL.md"), []byte(c.Contenu), 0o644); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, d := enTete(c.Contenu); d != "" {
			x.Description = d
		}
		b.Skills[c.ID] = x
		_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
		s.msg(w, "Skill saved. It applies from the next cycle.")
		return
	}
	id := identExt(c.Nom)
	desc := strings.Join(strings.Fields(c.Description), " ")
	if id == "" || desc == "" || strings.TrimSpace(c.Contenu) == "" {
		s.erreur(w, http.StatusBadRequest, "Give the skill a name, say when Claude should use it, and write its content")
		return
	}
	if _, existe := b.Skills[id]; existe {
		s.erreur(w, http.StatusConflict, "A skill with this name already exists")
		return
	}
	texte := "---\nname: " + id + "\ndescription: " + strings.ReplaceAll(desc, "\n", " ") + "\n---\n\n" + strings.TrimSpace(c.Contenu) + "\n"
	d := filepath.Join(dossierExt(k), "skills", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(texte), 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	b.Skills[id] = SkillInstalle{Nom: strings.TrimSpace(c.Nom), Description: desc, Ajoute: maintenantISO()}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", strings.TrimSpace(c.Nom))
}

// apiExtMCPPerso adds the MCP server(s) of a pasted configuration.
func (s *Serveur) apiExtMCPPerso(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom    string `json:"nom"`
		Texte  string `json:"texte"`
		Source string `json:"source"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	serveurs, err := lireConfigCollee(c.Texte, firstNonEmpty(c.Nom, "mcp"))
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b, secrets := lireBiblio(k), lireSecrets(k)
	var ajoutes []string
	for nom, conf := range serveurs {
		if len(serveurs) == 1 && strings.TrimSpace(c.Nom) != "" {
			nom = c.Nom
		}
		id := identExt(nom)
		if id == "" || id == "unreal-mcp" {
			s.erreur(w, http.StatusBadRequest, "Invalid name")
			return
		}
		if err := verifierConfig(conf); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		if conf.URL != "" && conf.Type == "" {
			conf.Type = "http"
		}
		if conf.URL == "" {
			conf.Type = "stdio"
		}
		valeurs := separerSecrets(&conf)
		if len(valeurs) > 0 {
			if secrets[id] == nil {
				secrets[id] = map[string]string{}
			}
			for k2, v := range valeurs {
				secrets[id][k2] = v
			}
		}
		b.MCP[id] = MCPInstalle{Nom: strings.TrimSpace(nom), Source: strings.TrimSpace(c.Source), Config: conf, Ajoute: maintenantISO()}
		ajoutes = append(ajoutes, strings.TrimSpace(nom))
	}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", strings.Join(ajoutes, ", "))
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// apiExtProjet switches a skill or a server on or off for the project.
func (s *Serveur) apiExtProjet(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Actif bool   `json:"actif"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b, choix := lireBiblio(k), lireChoix(k)
	ch := choix[cleProjet(p)]
	basculer := func(l []string) []string {
		var out []string
		for _, x := range l {
			if x != c.ID {
				out = append(out, x)
			}
		}
		if c.Actif {
			out = append(out, c.ID)
		}
		if out == nil {
			out = []string{}
		}
		return out
	}
	switch c.Type {
	case "skill":
		if _, ok := b.Skills[c.ID]; !ok {
			s.erreur(w, http.StatusNotFound, "Unknown")
			return
		}
		ch.Skills = basculer(ch.Skills)
	case "mcp":
		if _, ok := b.MCP[c.ID]; !ok {
			s.erreur(w, http.StatusNotFound, "Unknown")
			return
		}
		ch.MCP = basculer(ch.MCP)
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	if ch.Skills == nil {
		ch.Skills = []string{}
	}
	if ch.MCP == nil {
		ch.MCP = []string{}
	}
	choix[cleProjet(p)] = ch
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

func (s *Serveur) apiExtSecret(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID     string `json:"id"`
		Nom    string `json:"nom"`
		Valeur string `json:"valeur"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`).MatchString(c.Nom) || strings.ContainsAny(c.Valeur, "\n\r") {
		s.erreur(w, http.StatusBadRequest, "Invalid setting: "+c.Nom)
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	secrets := lireSecrets(k)
	if secrets[c.ID] == nil {
		secrets[c.ID] = map[string]string{}
	}
	if strings.TrimSpace(c.Valeur) == "" {
		delete(secrets[c.ID], c.Nom)
	} else {
		secrets[c.ID][c.Nom] = strings.TrimSpace(c.Valeur)
	}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Key saved. It applies from the next cycle.")
}

// apiExtRetirer removes a skill or a server from the library and from every project.
func (s *Serveur) apiExtRetirer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b, choix, secrets := lireBiblio(k), lireChoix(k), lireSecrets(k)
	for cle, ch := range choix {
		var garde []string
		liste := ch.Skills
		if c.Type == "mcp" {
			liste = ch.MCP
		}
		for _, x := range liste {
			if x != c.ID {
				garde = append(garde, x)
			}
		}
		if garde == nil {
			garde = []string{}
		}
		if c.Type == "mcp" {
			ch.MCP = garde
		} else {
			ch.Skills = garde
		}
		choix[cle] = ch
	}
	if c.Type == "mcp" {
		delete(b.MCP, c.ID)
		delete(secrets, c.ID)
		_ = os.RemoveAll(filepath.Join(k.Racine, "tools", "mcp", c.ID))
		_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600)
	} else {
		delete(b.Skills, c.ID)
		_ = os.RemoveAll(filepath.Join(dossierExt(k), "skills", c.ID))
	}
	_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644)
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Removed from Athanor and from every project.")
}

// apiExtVoir: the SKILL.md of a skill (editable), or the configuration of a server (keys hidden).
func (s *Serveur) apiExtVoir(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	k := s.kitCourant()
	if !reIDExt.MatchString(id) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	extMu.Lock()
	b := lireBiblio(k)
	extMu.Unlock()
	if r.URL.Query().Get("type") == "mcp" {
		x, ok := b.MCP[id]
		if !ok {
			s.erreur(w, http.StatusNotFound, "Unknown")
			return
		}
		j, _ := json.MarshalIndent(x.Config, "", "  ")
		s.ecrireJSON(w, map[string]string{"texte": string(j)})
		return
	}
	if _, ok := b.Skills[id]; !ok {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	t, _ := os.ReadFile(filepath.Join(dossierExt(k), "skills", id, "SKILL.md"))
	var fichiers []string
	racine := filepath.Join(dossierExt(k), "skills", id)
	_ = filepath.WalkDir(racine, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && len(fichiers) < 200 {
			rel, _ := filepath.Rel(racine, p)
			fichiers = append(fichiers, filepath.ToSlash(rel))
		}
		return nil
	})
	s.ecrireJSON(w, map[string]any{"texte": string(t), "fichiers": fichiers})
}

// apiExtTester starts a server in its cage, the way the pilot will, and lists its tools.
func (s *Serveur) apiExtTester(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	dossier := strings.ReplaceAll(p.Dossier, "'", "'\\''")
	sortie, _ := executerKit(s.kitCourant(), "P=\"$(wslpath -u '"+dossier+"' 2>/dev/null || printf '%s' '"+dossier+"')\"; python3 system/extensions.py tester \"$PWD\" \"$P\" "+c.ID)
	var res map[string]any
	for _, l := range strings.Split(sortie, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "{") && json.Unmarshal([]byte(strings.TrimSpace(l)), &res) == nil {
			s.ecrireJSON(w, res)
			return
		}
	}
	s.ecrireJSON(w, map[string]any{"ok": false, "erreur": court(sortie, 400)})
}
