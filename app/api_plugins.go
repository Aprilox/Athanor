package main

// Plugins (Skills & MCP page, Plugins tab): Claude Code plugins listed by marketplaces (the official
// one, plus those the user adds), installed once into extensions/plugins/<id>/ and switched on per
// project. A plugin is taken apart the Athanor way:
//   - its skills, commands and sub-agents are loaded with the plugin (--plugin-dir, a copy per project,
//     read-only for Claude: system/extensions.py);
//   - its MCP servers join the MCP library (they run in the cage, with the keys shared by the whole
//     application, and a remote server is signed in to once for every project);
//   - its hooks, LSP servers and monitors are not kept: they would run outside Claude's sandbox.

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var githubRaw = "https://raw.githubusercontent.com"

func init() {
	if v := os.Getenv("ATHANOR_GITHUB_RAW"); v != "" { // tests only
		githubRaw = v
	}
}

const (
	maxPlugin         = 100 << 20 // one plugin, unpacked
	maxFichiersPlugin = 5000
	maxMarche         = 8 << 20 // one marketplace.json
	marcheOfficiel    = "claude-plugins-official"
)

// MarcheInstalle: a marketplace (a catalog of plugins): a GitHub repository holding
// .claude-plugin/marketplace.json, or the address of such a file.
type MarcheInstalle struct {
	ID    string `json:"id"`
	Depot string `json:"depot,omitempty"` // owner/repo
	URL   string `json:"url,omitempty"`   // https://…/marketplace.json
	MAJ   string `json:"maj,omitempty"`
}

type PluginInstalle struct {
	Nom         string   `json:"nom"`
	Description string   `json:"description"`
	Version     string   `json:"version,omitempty"`
	Auteur      string   `json:"auteur,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Marche      string   `json:"marche"`
	Entree      string   `json:"entree,omitempty"` // its name in the marketplace (the manifest's may differ)
	Source      string   `json:"source"`
	Skills      []string `json:"skills"`
	Commandes   []string `json:"commandes"`
	Agents      []string `json:"agents"`
	MCP         []string `json:"mcp"`     // ids in the MCP library
	Ecartes     []string `json:"ecartes"` // hooks, lsp, monitors: not kept
	Ajoute      string   `json:"ajoute"`
}

var (
	reDepotCourt = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	reSHA        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	reUserConfig = regexp.MustCompile(`\$\{user_config\.([A-Za-z0-9_]+)\}`)
	reNomPlugin  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

func marcheParDefaut() MarcheInstalle {
	return MarcheInstalle{ID: marcheOfficiel, Depot: "anthropics/claude-plugins-official"}
}

// marchesDe: the official marketplace first, then the user's.
func marchesDe(b Bibliotheque) []MarcheInstalle {
	l := []MarcheInstalle{marcheParDefaut()}
	for _, m := range b.Marches {
		if m.ID == marcheOfficiel {
			l[0].MAJ = m.MAJ
			continue
		}
		l = append(l, m)
	}
	return l
}

func cheminMarche(k Kit, id string) string {
	return filepath.Join(dossierExt(k), "marketplaces", id+".json")
}

// lireMarche: the cached marketplace.json (name, pluginRoot, entries as raw objects).
type marcheLu struct {
	Nom        string
	PluginRoot string
	Plugins    []map[string]any
}

func decoderMarche(b []byte) (marcheLu, error) {
	var brut map[string]any
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &brut); err != nil {
		return marcheLu{}, errors.New("This marketplace.json cannot be read")
	}
	m := marcheLu{Nom: texteDe(brut["name"])}
	if md, ok := brut["metadata"].(map[string]any); ok {
		m.PluginRoot = texteDe(md["pluginRoot"])
	}
	l, _ := brut["plugins"].([]any)
	for _, x := range l {
		if e, ok := x.(map[string]any); ok && reNomPlugin.MatchString(texteDe(e["name"])) {
			m.Plugins = append(m.Plugins, e)
		}
	}
	if len(m.Plugins) == 0 {
		return m, errors.New("No plugin in this marketplace")
	}
	return m, nil
}

func lireMarche(k Kit, id string) (marcheLu, error) {
	b, err := os.ReadFile(cheminMarche(k, id))
	if err != nil {
		return marcheLu{}, err
	}
	return decoderMarche(b)
}

func texteDe(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func auteurDe(v any) string {
	if m, ok := v.(map[string]any); ok {
		return texteDe(m["name"])
	}
	return texteDe(v)
}

// telechargerMarche fetches a marketplace's marketplace.json.
func telechargerMarche(m MarcheInstalle) ([]byte, error) {
	if m.Depot != "" {
		return telecharger(githubRaw+"/"+m.Depot+"/HEAD/.claude-plugin/marketplace.json", maxMarche)
	}
	return telecharger(m.URL, maxMarche)
}

// lireMarcheSaisi understands owner/repo, a GitHub link to a repository, or the https address of a
// marketplace.json.
func lireMarcheSaisi(s string) (MarcheInstalle, error) {
	s = strings.TrimSpace(s)
	if reDepotCourt.MatchString(s) {
		return MarcheInstalle{Depot: strings.TrimSuffix(s, ".git")}, nil
	}
	if l, err := lireLienGitHub(s); err == nil && l.Chemin == "" {
		return MarcheInstalle{Depot: l.Proprio + "/" + l.Depot}, nil
	}
	if strings.HasPrefix(s, "https://") && strings.HasSuffix(strings.SplitN(s, "?", 2)[0], ".json") && !strings.ContainsAny(s, " \n\"'<>") {
		return MarcheInstalle{URL: s}, nil
	}
	return MarcheInstalle{}, errors.New("Give a GitHub repository (owner/repo or its link) holding .claude-plugin/marketplace.json, or the https address of a marketplace.json.")
}

// ----------------------------------------------------------------------------- sources

// sourcePlugin: where a plugin's files come from (a GitHub repository at a commit/branch, with a
// sub-folder; or a zip archive), or why it cannot be installed here.
type sourcePlugin struct {
	GH      *lienGH
	Archive string
	SHA256  string
	Texte   string // shown to the user
	Raison  string // not installable: why
}

func lienDepotGitHub(u string) (*lienGH, bool) {
	if reDepotCourt.MatchString(u) {
		p := strings.SplitN(strings.TrimSuffix(u, ".git"), "/", 2)
		return &lienGH{Proprio: p[0], Depot: p[1]}, true
	}
	l, err := lireLienGitHub(u)
	if err != nil || l.Chemin != "" {
		return nil, false
	}
	return &l, true
}

func sourceDe(e map[string]any, m MarcheInstalle, lu marcheLu) sourcePlugin {
	refDe := func(o map[string]any) string {
		if s := texteDe(o["sha"]); reSHA.MatchString(s) {
			return s
		}
		if r := texteDe(o["ref"]); r != "" && !strings.Contains(r, "..") && !strings.ContainsAny(r, " \n") {
			return r
		}
		return ""
	}
	cheminSur := func(c string) (string, bool) {
		c = strings.Trim(path.Clean("/"+strings.ReplaceAll(c, "\\", "/")), "/")
		return c, !strings.Contains(c, "..")
	}
	switch s := e["source"].(type) {
	case string:
		rel := ""
		switch {
		case s == ".":
		case strings.HasPrefix(s, "./"):
			rel = s[2:]
		case !strings.Contains(s, "/") && lu.PluginRoot != "":
			rel = strings.TrimPrefix(lu.PluginRoot, "./") + "/" + s
		default:
			return sourcePlugin{Raison: "Unknown source."}
		}
		if strings.Contains(rel, "..") {
			return sourcePlugin{Raison: "Unknown source."}
		}
		if m.Depot == "" {
			return sourcePlugin{Raison: "Its files are in the marketplace's repository, which Athanor cannot reach from a marketplace.json address."}
		}
		p := strings.SplitN(m.Depot, "/", 2)
		c, _ := cheminSur(rel)
		return sourcePlugin{GH: &lienGH{Proprio: p[0], Depot: p[1], Chemin: c}, Texte: "github.com/" + m.Depot + "/" + c}
	case map[string]any:
		switch texteDe(s["source"]) {
		case "github":
			l, ok := lienDepotGitHub(texteDe(s["repo"]))
			if !ok {
				return sourcePlugin{Raison: "Unknown source."}
			}
			l.Ref = refDe(s)
			return sourcePlugin{GH: l, Texte: "github.com/" + l.Proprio + "/" + l.Depot}
		case "url", "git-subdir":
			l, ok := lienDepotGitHub(texteDe(s["url"]))
			if !ok {
				return sourcePlugin{Raison: "Hosted outside GitHub: Athanor installs plugins from GitHub or from a zip archive."}
			}
			l.Ref = refDe(s)
			t := "github.com/" + l.Proprio + "/" + l.Depot
			if texteDe(s["source"]) == "git-subdir" {
				c, ok := cheminSur(texteDe(s["path"]))
				if !ok || c == "" {
					return sourcePlugin{Raison: "Unknown source."}
				}
				l.Chemin = c
				t += "/" + c
			}
			return sourcePlugin{GH: l, Texte: t}
		case "archive":
			u := texteDe(s["url"])
			if !strings.HasPrefix(u, "https://") || strings.ContainsAny(u, " \n\"'<>") {
				return sourcePlugin{Raison: "Unknown source."}
			}
			return sourcePlugin{Archive: u, SHA256: strings.ToLower(texteDe(s["sha256"])), Texte: u}
		case "npm":
			return sourcePlugin{Raison: "Published on npm: not supported by Athanor yet."}
		case "command":
			return sourcePlugin{Raison: "Built by a command on the PC: never run by Athanor."}
		}
	}
	return sourcePlugin{Raison: "Unknown source."}
}

// ----------------------------------------------------------------------------- download and unpack

type fichierZip struct {
	f   *zip.File
	rel string
}

// fichiersPlugin: the files of the plugin inside the archive, with their path from the plugin's root.
func fichiersPlugin(z *zip.Reader, src sourcePlugin) []fichierZip {
	var l []fichierZip
	if src.GH != nil {
		prefixe := ""
		if src.GH.Chemin != "" {
			prefixe = src.GH.Chemin + "/"
		}
		for _, f := range z.File {
			c := cheminDansArchive(f.Name)
			if strings.HasPrefix(c, prefixe) {
				l = append(l, fichierZip{f, strings.TrimPrefix(c, prefixe)})
			}
		}
		return l
	}
	// a zip archive: the plugin's root is its top, or a single folder at its top
	racine, unique := "", ""
	for _, f := range z.File {
		n := strings.TrimPrefix(f.Name, "./")
		if strings.HasPrefix(n, ".claude-plugin/") || strings.HasPrefix(n, "skills/") || strings.HasPrefix(n, "commands/") || strings.HasPrefix(n, "agents/") {
			racine = "-"
		}
		haut := strings.SplitN(n, "/", 2)[0]
		if unique == "" {
			unique = haut
		} else if unique != haut {
			unique = "*"
		}
	}
	prefixe := ""
	if racine == "" && unique != "" && unique != "*" {
		prefixe = unique + "/"
	}
	for _, f := range z.File {
		n := strings.TrimPrefix(f.Name, "./")
		if strings.HasPrefix(n, prefixe) {
			l = append(l, fichierZip{f, strings.TrimPrefix(n, prefixe)})
		}
	}
	return l
}

func extrairePlugin(fichiers []fichierZip, dest string) error {
	var total int64
	n := 0
	for _, x := range fichiers {
		rel := x.rel
		if rel == "" || strings.HasSuffix(rel, "/") || strings.Contains(rel, "..") || path.IsAbs(rel) || strings.HasPrefix(rel, ".git/") || !x.f.Mode().IsRegular() {
			continue
		}
		n++
		total += int64(x.f.UncompressedSize64)
		if n > maxFichiersPlugin || total > maxPlugin {
			return errors.New("This plugin is too large (more than 100 MB or 5000 files)")
		}
		cible := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(cible, dest+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cible), 0o755); err != nil {
			return err
		}
		rc, err := x.f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if x.f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(cible, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, maxPlugin))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	if n == 0 {
		return errors.New("This plugin's folder is empty or was not found")
	}
	return nil
}

// ----------------------------------------------------------------------------- take a plugin apart

// clesManifeste: what a plugin's manifest keeps (everything else is dropped by analyserPlugin).
var clesManifeste = map[string]bool{"name": true, "displayName": true, "description": true, "version": true, "author": true,
	"homepage": true, "repository": true, "license": true, "keywords": true, "skills": true, "commands": true, "agents": true, "outputStyles": true}

// retirerCrochetsMd removes the "hooks:" key (and its indented block) from the front matter of every
// Markdown file of the plugin; true when one was found.
func retirerCrochetsMd(dossier string) bool {
	trouve := false
	_ = filepath.WalkDir(dossier, func(c string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") || !e.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(c)
		if err != nil {
			return nil
		}
		t := strings.ReplaceAll(string(b), "\r\n", "\n")
		if !strings.HasPrefix(t, "---\n") {
			return nil
		}
		fin := strings.Index(t[4:], "\n---")
		if fin < 0 {
			return nil
		}
		entete := strings.Split(t[4:4+fin], "\n")
		garde, dans, change := []string{}, false, false
		for _, l := range entete {
			cle := strings.TrimSpace(strings.SplitN(l, ":", 2)[0])
			haut := l != "" && l[0] != ' ' && l[0] != '\t' && l[0] != '-'
			if haut {
				dans = strings.EqualFold(strings.Trim(cle, `"'`), "hooks")
			}
			if dans {
				change = true
				continue
			}
			garde = append(garde, l)
		}
		if change {
			trouve = true
			_ = os.WriteFile(c, []byte("---\n"+strings.Join(garde, "\n")+t[4+fin:]), 0o644)
		}
		return nil
	})
	return trouve
}

// fichierDansPlugin: a path the manifest gives ("./x", "x"), kept inside the plugin.
func fichierDansPlugin(dossier, rel string) (string, bool) {
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "./")
	if rel == "" || strings.Contains(rel, "..") || path.IsAbs(rel) {
		return "", false
	}
	c := filepath.Join(dossier, filepath.FromSlash(rel))
	return c, strings.HasPrefix(c, dossier+string(filepath.Separator))
}

func listeChemins(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var l []string
		for _, y := range x {
			if s, ok := y.(string); ok {
				l = append(l, s)
			}
		}
		return l
	}
	return nil
}

// serveursDe reads MCP servers from a .mcp.json-like value ({"mcpServers": {...}} or {name: {...}}).
func serveursDe(v any, dossier string, out map[string]ConfigMCP) {
	serveursA(v, dossier, out, 0, false)
}

// suivi: a path to another file was already followed (never twice: a file naming itself would loop)
func serveursA(v any, dossier string, out map[string]ConfigMCP, niveau int, suivi bool) {
	if niveau > 6 {
		return
	}
	switch x := v.(type) {
	case string:
		if suivi {
			return
		}
		if c, ok := fichierDansPlugin(dossier, x); ok {
			if b, err := os.ReadFile(c); err == nil && len(b) < 1<<20 {
				var y any
				if json.Unmarshal(b, &y) == nil {
					serveursA(y, dossier, out, niveau+1, true)
				}
			}
		}
	case []any:
		for _, y := range x {
			serveursA(y, dossier, out, niveau+1, suivi)
		}
	case map[string]any:
		if m, ok := x["mcpServers"]; ok {
			serveursA(m, dossier, out, niveau+1, suivi)
			return
		}
		for nom, conf := range x {
			b, _ := json.Marshal(conf)
			var c ConfigMCP
			if json.Unmarshal(b, &c) == nil && (c.Command != "" || c.URL != "") && reNomPlugin.MatchString(nom) {
				out[nom] = c
			}
		}
	}
}

type infoPlugin struct {
	Nom, Description, Version, Auteur, Homepage string
	Skills, Commandes, Agents, Ecartes          []string
	MCP                                         map[string]ConfigMCP
}

// analyserPlugin reads the unpacked plugin, keeps its MCP servers apart, removes what would run
// outside the sandbox (hooks, LSP servers, monitors) and writes the manifest Claude Code will read.
func analyserPlugin(dossier string, entree map[string]any) (infoPlugin, error) {
	info := infoPlugin{MCP: map[string]ConfigMCP{}}
	cheminManifeste := filepath.Join(dossier, ".claude-plugin", "plugin.json")
	manifeste := map[string]any{}
	avecManifeste := false
	if b, err := os.ReadFile(cheminManifeste); err == nil {
		if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &manifeste) != nil {
			return info, errors.New("This plugin's plugin.json cannot be read")
		}
		avecManifeste = true
	}
	if !avecManifeste { // the marketplace entry is the manifest
		for k, v := range entree {
			switch k {
			case "source", "category", "tags", "strict", "relevance", "metadata", "headers", "headersHelper", "defaultEnabled", "dependencies":
			default:
				manifeste[k] = v
			}
		}
	} else if strict, ok := entree["strict"].(bool); !ok || strict {
		for _, k := range []string{"commands", "agents", "skills"} {
			if v, ok := entree[k]; ok {
				manifeste[k] = append(listeChemins(manifeste[k]), listeChemins(v)...)
			}
		}
	}
	// MCP servers: into the library (the cage, shared keys), never loaded by the plugin itself
	if _, err := os.Stat(filepath.Join(dossier, ".mcp.json")); err == nil {
		serveursDe(".mcp.json", dossier, info.MCP)
	}
	if v, ok := manifeste["mcpServers"]; ok {
		serveursDe(v, dossier, info.MCP)
	}
	if !avecManifeste {
		if v, ok := entree["mcpServers"]; ok {
			serveursDe(v, dossier, info.MCP)
		}
	}
	_ = os.Remove(filepath.Join(dossier, ".mcp.json"))
	delete(manifeste, "mcpServers")
	// what would run outside Claude's sandbox, or pull other plugins in: never kept. The manifest keeps
	// only what describes the plugin and where its skills, commands, sub-agents and styles are.
	ecarter := func(nom string) {
		if !contient(info.Ecartes, nom) {
			info.Ecartes = append(info.Ecartes, nom)
		}
	}
	for k := range manifeste {
		if clesManifeste[k] {
			continue
		}
		switch k {
		case "hooks":
			ecarter("hooks")
		case "lspServers":
			ecarter("lsp")
		case "monitors", "experimental":
			ecarter("monitors")
		case "channels", "workflows", "dependencies":
			ecarter(k)
		}
		delete(manifeste, k)
	}
	for _, f := range [][2]string{{"hooks/hooks.json", "hooks"}, {".lsp.json", "lsp"}, {"monitors", "monitors"}, {"workflows", "workflows"}, {"settings.json", "settings"}} {
		if _, err := os.Lstat(filepath.Join(dossier, filepath.FromSlash(f[0]))); err == nil {
			ecarter(f[1])
			_ = os.RemoveAll(filepath.Join(dossier, filepath.FromSlash(f[0])))
		}
	}
	// hooks declared in the front matter of a skill, a command or a sub-agent
	if retirerCrochetsMd(dossier) {
		ecarter("hooks")
	}
	sort.Strings(info.Ecartes)
	info.Nom = texteDe(manifeste["name"])
	if !reNomPlugin.MatchString(info.Nom) {
		info.Nom = texteDe(entree["name"])
		manifeste["name"] = info.Nom
	}
	info.Description = firstNonEmpty(texteDe(entree["description"]), texteDe(manifeste["description"]))
	info.Version = firstNonEmpty(texteDe(manifeste["version"]), texteDe(entree["version"]))
	info.Auteur = firstNonEmpty(auteurDe(entree["author"]), auteurDe(manifeste["author"]))
	info.Homepage = firstNonEmpty(texteDe(entree["homepage"]), texteDe(manifeste["homepage"]))
	// components, for the interface
	dossiers := func(defaut string, v any) []string {
		l := []string{defaut}
		for _, c := range listeChemins(v) {
			l = append(l, c)
		}
		return l
	}
	vus := map[string]bool{}
	for _, d := range dossiers("skills", manifeste["skills"]) {
		c, ok := fichierDansPlugin(dossier, d)
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "SKILL.md")); err == nil && !vus[c] {
			vus[c] = true
			info.Skills = append(info.Skills, filepath.Base(c))
			continue
		}
		entrees, _ := os.ReadDir(c)
		for _, e := range entrees {
			if _, err := os.Stat(filepath.Join(c, e.Name(), "SKILL.md")); err == nil && !vus[filepath.Join(c, e.Name())] {
				vus[filepath.Join(c, e.Name())] = true
				info.Skills = append(info.Skills, e.Name())
			}
		}
	}
	mds := func(defaut string, v any) []string {
		var l []string
		for _, d := range dossiers(defaut, v) {
			c, ok := fichierDansPlugin(dossier, d)
			if !ok {
				continue
			}
			if strings.HasSuffix(c, ".md") {
				if _, err := os.Stat(c); err == nil && !vus[c] {
					vus[c] = true
					l = append(l, strings.TrimSuffix(filepath.Base(c), ".md"))
				}
				continue
			}
			_ = filepath.WalkDir(c, func(p string, e os.DirEntry, err error) error {
				if err == nil && !e.IsDir() && strings.HasSuffix(p, ".md") && !vus[p] && !strings.EqualFold(e.Name(), "README.md") {
					vus[p] = true
					l = append(l, strings.TrimSuffix(e.Name(), ".md"))
				}
				return nil
			})
		}
		sort.Strings(l)
		return l
	}
	info.Commandes = mds("commands", manifeste["commands"])
	info.Agents = mds("agents", manifeste["agents"])
	sort.Strings(info.Skills)
	if len(info.Skills)+len(info.Commandes)+len(info.Agents)+len(info.MCP) == 0 {
		return info, errors.New("This plugin has nothing Athanor can use (no skill, command, sub-agent or MCP server).")
	}
	if err := os.MkdirAll(filepath.Dir(cheminManifeste), 0o755); err != nil {
		return info, err
	}
	b, _ := json.MarshalIndent(manifeste, "", "  ")
	return info, os.WriteFile(cheminManifeste, b, 0o644)
}

func idPlugin(marche, nom string) string {
	if marche != marcheOfficiel {
		return identExt(marche + "-" + nom)
	}
	return identExt(nom)
}

func hoteDe(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return strings.ToLower(strings.SplitN(strings.SplitN(u, "/", 2)[0], "?", 2)[0])
}

// ----------------------------------------------------------------------------- API

type vuePluginCatalogue struct {
	Marche      string `json:"marche"`
	Nom         string `json:"nom"`
	Description string `json:"description"`
	Categorie   string `json:"categorie,omitempty"`
	Auteur      string `json:"auteur,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Source      string `json:"source,omitempty"`
	Raison      string `json:"raison,omitempty"`
	Installe    bool   `json:"installe"`
}

type vuePlugin struct {
	ID string `json:"id"`
	PluginInstalle
	Actif bool `json:"actif"`
}

func (s *Serveur) apiPlugins(w http.ResponseWriter, r *http.Request) {
	k := s.kitCourant()
	extMu.Lock()
	b, choix := lireBiblio(k), lireChoix(k)
	extMu.Unlock()
	p, avecProjet := s.projet(r)
	ch := ChoixProjet{}
	if avecProjet {
		ch = choix[cleProjet(p)]
	}
	type vueMarche struct {
		MarcheInstalle
		Nom    string `json:"nom"`
		N      int    `json:"n"`
		Charge bool   `json:"charge"`
		Erreur string `json:"erreur,omitempty"`
	}
	marches, catalogue := []vueMarche{}, []vuePluginCatalogue{}
	installes := map[string]bool{}
	for id := range b.Plugins {
		installes[id] = true
	}
	for _, m := range marchesDe(b) {
		v := vueMarche{MarcheInstalle: m, Nom: m.ID}
		lu, err := lireMarche(k, m.ID)
		if err == nil {
			v.Charge, v.N = true, len(lu.Plugins)
			if lu.Nom != "" {
				v.Nom = lu.Nom
			}
			for _, e := range lu.Plugins {
				src := sourceDe(e, m, lu)
				catalogue = append(catalogue, vuePluginCatalogue{Marche: m.ID, Nom: texteDe(e["name"]), Description: texteDe(e["description"]),
					Categorie: texteDe(e["category"]), Auteur: auteurDe(e["author"]), Homepage: texteDe(e["homepage"]), Source: src.Texte, Raison: src.Raison,
					Installe: installes[idPlugin(m.ID, texteDe(e["name"]))]})
			}
		} else if !os.IsNotExist(err) {
			v.Erreur = err.Error()
		}
		marches = append(marches, v)
	}
	liste := []vuePlugin{}
	for id, x := range b.Plugins {
		liste = append(liste, vuePlugin{ID: id, PluginInstalle: x, Actif: contient(ch.Plugins, id)})
	}
	sort.Slice(liste, func(i, j int) bool { return strings.ToLower(liste[i].Nom) < strings.ToLower(liste[j].Nom) })
	nomProjet := ""
	if avecProjet {
		nomProjet = p.Nom
	}
	s.ecrireJSON(w, map[string]any{"projet": nomProjet, "marches": marches, "catalogue": catalogue, "plugins": liste})
}

// apiPluginsMarche: add, refresh or remove a marketplace.
func (s *Serveur) apiPluginsMarche(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Action string `json:"action"` // ajouter | actualiser | retirer
		Texte  string `json:"texte"`
		ID     string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	b := lireBiblio(k)
	extMu.Unlock()
	var m MarcheInstalle
	switch c.Action {
	case "ajouter":
		var err error
		if m, err = lireMarcheSaisi(c.Texte); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
	case "actualiser", "retirer":
		trouve := false
		for _, x := range marchesDe(b) {
			if x.ID == c.ID {
				m, trouve = x, true
			}
		}
		if !trouve {
			s.erreur(w, http.StatusNotFound, "Unknown")
			return
		}
		if c.Action == "retirer" {
			if m.ID == marcheOfficiel {
				s.erreur(w, http.StatusBadRequest, "The official marketplace stays: it is the one Claude Code offers.")
				return
			}
			extMu.Lock()
			b = lireBiblio(k)
			garde := []MarcheInstalle{}
			for _, x := range b.Marches {
				if x.ID != m.ID {
					garde = append(garde, x)
				}
			}
			b.Marches = garde
			err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
			extMu.Unlock()
			_ = os.Remove(cheminMarche(k, m.ID))
			if err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.msg(w, "Marketplace removed. The plugins installed from it stay.")
			return
		}
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	octets, err := telechargerMarche(m)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, "Cannot download the marketplace: "+err.Error())
		return
	}
	lu, err := decoderMarche(octets)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.ID == "" {
		m.ID = identExt(firstNonEmpty(lu.Nom, strings.ReplaceAll(m.Depot, "/", "-")))
		if m.ID == "" || m.ID == marcheOfficiel {
			m.ID = identExt(strings.ReplaceAll(firstNonEmpty(m.Depot, "marketplace"), "/", "-"))
		}
	}
	m.MAJ = maintenantISO()
	extMu.Lock()
	defer extMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(cheminMarche(k, m.ID)), 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(cheminMarche(k, m.ID), octets, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	b = lireBiblio(k)
	garde := []MarcheInstalle{}
	for _, x := range b.Marches {
		if x.ID != m.ID {
			garde = append(garde, x)
		}
	}
	b.Marches = append(garde, m)
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "{0} plugin(s) listed.", itoa(int64(len(lu.Plugins))))
}

// apiPluginsInstaller downloads a plugin of a marketplace, takes it apart and installs it.
func (s *Serveur) apiPluginsInstaller(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Marche string `json:"marche"`
		Nom    string `json:"nom"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	b := lireBiblio(k)
	extMu.Unlock()
	var m MarcheInstalle
	for _, x := range marchesDe(b) {
		if x.ID == c.Marche {
			m = x
		}
	}
	lu, err := lireMarche(k, m.ID)
	if m.ID == "" || err != nil {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	var entree map[string]any
	for _, e := range lu.Plugins {
		if texteDe(e["name"]) == c.Nom {
			entree = e
		}
	}
	if entree == nil {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	src := sourceDe(entree, m, lu)
	if src.Raison != "" {
		s.erreur(w, http.StatusBadRequest, src.Raison)
		return
	}
	var z *zip.Reader
	if src.GH != nil {
		z, err = archiveGitHub(src.GH)
	} else {
		var octets []byte
		octets, err = telecharger(src.Archive, maxArchive)
		if err == nil && src.SHA256 != "" {
			somme := sha256.Sum256(octets)
			if hex.EncodeToString(somme[:]) != src.SHA256 {
				err = errors.New("the archive does not match its sha256")
			}
		}
		if err == nil {
			z, err = zip.NewReader(bytes.NewReader(octets), int64(len(octets)))
		}
	}
	if err != nil {
		s.erreur(w, http.StatusBadGateway, "Cannot download the plugin: "+err.Error())
		return
	}
	id := idPlugin(m.ID, c.Nom)
	if !reIDExt.MatchString(id) {
		s.erreur(w, http.StatusBadRequest, "Invalid name")
		return
	}
	extMu.Lock()
	if x, ok := lireBiblio(k).Plugins[id]; ok && x.Marche != m.ID {
		extMu.Unlock()
		s.erreur(w, http.StatusConflict, "A plugin of another marketplace already has this name: remove it first.")
		return
	}
	extMu.Unlock()
	racine := filepath.Join(dossierExt(k), "plugins")
	tmp := filepath.Join(racine, "."+id+".tmp")
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := extrairePlugin(fichiersPlugin(z, src), tmp); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := analyserPlugin(tmp, entree)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	extMu.Lock()
	defer extMu.Unlock()
	b = lireBiblio(k)
	secrets := lireSecrets(k)
	// a reinstall (an update) replaces the plugin's servers; the keys already given stay
	anciens := map[string]bool{}
	_, existait := b.Plugins[id]
	if ancien, ok := b.Plugins[id]; ok {
		for _, x := range ancien.MCP {
			anciens[x] = true
		}
	}
	noms := make([]string, 0, len(info.MCP))
	for n := range info.MCP {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	var ids []string
	for _, n := range noms {
		conf := info.MCP[n]
		// ${user_config.KEY} (a setting asked when the plugin is enabled) becomes a key of Athanor's
		remplacer := func(v string) string { return reUserConfig.ReplaceAllString(v, "${$1}") }
		conf.URL, conf.Command = remplacer(conf.URL), remplacer(conf.Command)
		for i := range conf.Args {
			conf.Args[i] = remplacer(conf.Args[i])
		}
		for k2, v := range conf.Env {
			conf.Env[k2] = remplacer(v)
		}
		for k2, v := range conf.Headers {
			conf.Headers[k2] = remplacer(v)
		}
		if verifierConfig(conf) != nil {
			continue
		}
		if conf.URL != "" && conf.Type == "" {
			conf.Type = "http"
		}
		if conf.URL == "" {
			conf.Type = "stdio"
		}
		sid := identExt(id + "-" + n)
		for i := 2; ; i++ { // a server of someone else's keeps its name and its keys
			if x, existe := b.MCP[sid]; !existe || x.Plugin == id {
				break
			}
			sid = identExt(id + "-" + n + "-" + itoa(int64(i)))
		}
		// the keys given for a server stay only while it talks to the same place
		if x, existe := b.MCP[sid]; existe && (hoteDe(x.Config.URL) != hoteDe(conf.URL) || x.Config.Command != conf.Command) {
			delete(secrets, sid)
		}
		valeurs := separerSecrets(&conf)
		if len(valeurs) > 0 {
			if secrets[sid] == nil {
				secrets[sid] = map[string]string{}
			}
			for k2, v := range valeurs {
				secrets[sid][k2] = v
			}
		}
		b.MCP[sid] = MCPInstalle{Nom: n, Description: info.Nom, Source: "plugin:" + id, Plugin: id, Config: conf, Ajoute: maintenantISO()}
		ids = append(ids, sid)
		delete(anciens, sid)
	}
	for x := range anciens { // servers the new version no longer has
		delete(b.MCP, x)
		delete(secrets, x)
	}
	final := filepath.Join(racine, id)
	_ = os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if b.Plugins == nil {
		b.Plugins = map[string]PluginInstalle{}
	}
	if ids == nil {
		ids = []string{}
	}
	nonNul := func(l []string) []string {
		if l == nil {
			return []string{}
		}
		return l
	}
	b.Plugins[id] = PluginInstalle{Nom: info.Nom, Description: info.Description, Version: info.Version, Auteur: info.Auteur, Homepage: info.Homepage,
		Marche: m.ID, Entree: c.Nom, Source: src.Texte, Skills: nonNul(info.Skills), Commandes: nonNul(info.Commandes), Agents: nonNul(info.Agents), MCP: ids,
		Ecartes: nonNul(info.Ecartes), Ajoute: maintenantISO()}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existait {
		s.msg(w, "“{0}” updated (version {1}).", info.Nom, firstNonEmpty(info.Version, "?"))
		return
	}
	s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", info.Nom)
}

func (s *Serveur) apiPluginsProjet(w http.ResponseWriter, r *http.Request) {
	var c struct {
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
	if _, ok := b.Plugins[c.ID]; !ok {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	ch := choix[cleProjet(p)]
	garde := []string{}
	for _, x := range ch.Plugins {
		if x != c.ID {
			garde = append(garde, x)
		}
	}
	if c.Actif {
		garde = append(garde, c.ID)
	}
	ch.Plugins = garde
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

func (s *Serveur) apiPluginsRetirer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b, choix, secrets := lireBiblio(k), lireChoix(k), lireSecrets(k)
	x, ok := b.Plugins[c.ID]
	if !ok {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	for _, sid := range x.MCP {
		delete(b.MCP, sid)
		delete(secrets, sid)
		_ = os.RemoveAll(filepath.Join(k.Racine, "tools", "mcp", sid))
	}
	delete(b.Plugins, c.ID)
	for cle, ch := range choix {
		garde := []string{}
		for _, y := range ch.Plugins {
			if y != c.ID {
				garde = append(garde, y)
			}
		}
		ch.Plugins = garde
		choix[cle] = ch
	}
	_ = os.RemoveAll(filepath.Join(dossierExt(k), "plugins", c.ID))
	_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600)
	_ = ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644)
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Removed from Athanor and from every project.")
}

// ----------------------------------------------------------------------------- sign-in (OAuth)
// A remote MCP server that asks for a sign-in: Claude Code in a window, with only that server, where
// the user runs /mcp once. Claude Code keeps the sign-in in its configuration, the one every pilot
// uses: it is valid for every project.

var texteConnexion = map[string][3]string{
	"en": {"Sign-in to the MCP server “%s”, once for every project.", "In Claude below: type /mcp, choose “%s”, then Authenticate; finish in the browser.", "When it says connected, type /exit: this window closes."},
	"fr": {"Connexion au serveur MCP « %s », une fois pour tous les projets.", "Dans Claude ci-dessous : tapez /mcp, choisissez « %s », puis Authenticate ; terminez dans le navigateur.", "Quand il indique connecté, tapez /exit : cette fenêtre se ferme."},
	"es": {"Inicio de sesión en el servidor MCP «%s», una vez para todos los proyectos.", "En Claude, abajo: escriba /mcp, elija «%s» y luego Authenticate; termine en el navegador.", "Cuando indique conectado, escriba /exit: esta ventana se cierra."},
	"de": {"Anmeldung beim MCP-Server „%s“, einmal für alle Projekte.", "Unten in Claude: /mcp eingeben, „%s“ wählen, dann Authenticate; im Browser abschließen.", "Wenn „connected“ erscheint, /exit eingeben: dieses Fenster schließt sich."},
	"it": {"Accesso al server MCP «%s», una volta per tutti i progetti.", "In Claude qui sotto: digita /mcp, scegli «%s», poi Authenticate; completa nel browser.", "Quando indica connesso, digita /exit: questa finestra si chiude."},
	"pt": {"Início de sessão no servidor MCP «%s», uma vez para todos os projetos.", "No Claude abaixo: escreva /mcp, escolha «%s» e depois Authenticate; termine no navegador.", "Quando indicar ligado, escreva /exit: esta janela fecha-se."},
}

func (s *Serveur) apiExtConnexion(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID string `json:"id"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	x, ok := lireBiblio(k).MCP[c.ID]
	extMu.Unlock()
	if !ok || x.Config.URL == "" {
		s.erreur(w, http.StatusBadRequest, "Only a remote server can be signed in to.")
		return
	}
	t, ok := texteConnexion[s.langue()]
	if !ok {
		t = texteConnexion["en"]
	}
	ligne := func(f string) string { return "printf '%s\\n' " + citerShell(strings.ReplaceAll(f, "%s", c.ID)) + "; " }
	script := chargerEnv + `f=$(mktemp) || exit 1; python3 system/extensions.py connexion "$PWD" ` + c.ID + ` "$f" || { rm -f "$f"; sleep 8; exit 1; }; ` +
		ligne(t[0]) + ligne(t[1]) + ligne(t[2]) + `echo; "${CLAUDE_BIN:-claude}" --mcp-config "$f" --strict-mcp-config; rm -f "$f"`
	if err := FenetreWSL(k, "MCP", script); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "A window opens: sign in there once, it is valid for every project.")
}
