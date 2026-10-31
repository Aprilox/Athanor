package main

// "Fix with Claude": an MCP server whose test fails on this computer (a command that does not exist here,
// arguments written for another system, a package that changed its name…). Claude Code is given the
// server's definition (the names of its keys, never their values), this system, the programs found here
// and the error; it may read the server's page on the web, and answers with a configuration only (it
// changes no file). Athanor tests that configuration exactly as it would run it: kept when the server
// answers, else the error goes back to Claude, three tries at most. When a program is missing here, or
// when Athanor's own way of starting servers is at fault, Claude says so instead of working around it.
//
// Every fix, and every fault of Athanor's that Claude finds, is also written for everyone in
// logs/corrections/ (ext_corrections.go): the verified catalog is corrected from it.
//
//   athanor extensions corriger KIT PROJECT ID [--appliquer]
//     prints "ETAPE claude N" / "ETAPE test N" while it works, then one line of JSON; --appliquer writes
//     the fix into the library (the application writes it itself, under its own lock)

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
	"runtime"
	"sort"
	"strings"
	"time"
)

// essaisCorrection: Claude's proposals tried, at most.
const essaisCorrection = 3

// PropositionMCP: Claude's answer.
type PropositionMCP struct {
	Config      *ConfigMCP        `json:"config"`
	Reglages    map[string]string `json:"reglages,omitempty"` // settings' new defaults
	Explication string            `json:"explication"`
	Manque      string            `json:"manque,omitempty"`  // a program missing on this computer
	Athanor     string            `json:"athanor,omitempty"` // what Athanor's integration should change
}

// EssaiCorrection: one proposal and what its test said.
type EssaiCorrection struct {
	Config      *ConfigMCP        `json:"config,omitempty"`
	Reglages    map[string]string `json:"reglages,omitempty"`
	Explication string            `json:"explication,omitempty"`
	Erreur      string            `json:"erreur"`
	Details     []string          `json:"details,omitempty"`
}

// ResultatCorrection: what "Fix with Claude" found.
type ResultatCorrection struct {
	OK          bool              `json:"ok"`
	Deja        bool              `json:"deja,omitempty"` // the test passed already: nothing to fix
	Essais      []EssaiCorrection `json:"essais,omitempty"`
	Config      *ConfigMCP        `json:"config,omitempty"` // the configuration that works
	Avant       *ConfigMCP        `json:"avant,omitempty"`
	Reglages    map[string]string `json:"reglages,omitempty"`
	Explication string            `json:"explication,omitempty"`
	Manque      string            `json:"manque,omitempty"`
	Athanor     string            `json:"athanor,omitempty"`
	Erreur      string            `json:"erreur,omitempty"` // why nothing was found
	Initiale    string            `json:"initiale,omitempty"`
	Details     []string          `json:"details,omitempty"` // the first test's last lines
	Outils      []string          `json:"outils,omitempty"`
	Systeme     string            `json:"systeme"`
}

var languesCorrection = map[string]string{"fr": "French", "es": "Spanish", "de": "German", "it": "Italian", "pt": "European Portuguese"}

// descriptionSysteme: this computer, as Claude needs it.
func descriptionSysteme() string {
	arch := runtime.GOARCH
	switch runtime.GOOS {
	case "darwin":
		v := ""
		if b, err := commande("sw_vers", "-productVersion").Output(); err == nil {
			v = " " + strings.TrimSpace(string(b))
		}
		return "macOS" + v + " (" + arch + ")"
	case "windows":
		return "Windows (" + arch + "), servers run directly on Windows"
	}
	nom := "Linux"
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				nom = "Linux, " + strings.Trim(v, `"`)
			}
		}
	}
	if interopWindows() {
		nom += ", in WSL on Windows (a server of type \"windows\" runs on Windows itself)"
	}
	return nom + " (" + arch + ")"
}

// descriptionCage: how Athanor starts a local server here.
func descriptionCage() string {
	switch runtime.GOOS {
	case "windows":
		return "A local (stdio) server is started directly on Windows, from the project's folder, with no cage: a .cmd or .bat launcher (npx, uvx…) goes through cmd /c."
	case "darwin":
		return "A local (stdio) server runs in Athanor's cage (macOS sandbox-exec): the system readable, only the project's folder and Athanor's tool folder writable, HOME a private empty folder, npm's and uv's caches in Athanor's tool folder, the network open. Its PATH: /usr/local/bin, /opt/homebrew/bin and the system's, then the toolchains of the user's home (~/.local/bin, nvm's node, ~/.cargo/bin, bun, deno, volta)."
	}
	return "A local (stdio) server runs in Athanor's cage (bubblewrap): the whole system read-only, only the project's folder and Athanor's tool folder writable, HOME a private empty folder, /tmp private, npm's and uv's caches in Athanor's tool folder, the network open. Its PATH: the system's, then the toolchains of the user's home (~/.local/bin, nvm's node, ~/.cargo/bin, bun, deno, volta)."
}

// programmesTrouves: the usual launchers, as a server's PATH finds them here (with node's and python's
// versions).
func programmesTrouves() string {
	chemin := os.Getenv("PATH")
	switch runtime.GOOS {
	case "darwin":
		chemin = cheminCage("/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin", os.Getenv("HOME"), estDossierCage)
	case "linux":
		chemin = cheminCage("/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", os.Getenv("HOME"), dossierExiste)
	}
	ancien := os.Getenv("PATH")
	os.Setenv("PATH", chemin)
	defer os.Setenv("PATH", ancien)
	var trouves, absents []string
	for _, n := range []string{"npx", "node", "uvx", "uv", "python3", "python", "pipx", "docker", "bunx", "deno", "java", "go"} {
		p, err := exec.LookPath(n)
		if err != nil {
			absents = append(absents, n)
			continue
		}
		v := ""
		if n == "node" || n == "python3" || n == "uv" || n == "docker" {
			ctx, fin := context.WithTimeout(context.Background(), 5*time.Second)
			if b, err := exec.CommandContext(ctx, p, "--version").CombinedOutput(); err == nil {
				v = " " + strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
			}
			fin()
		}
		trouves = append(trouves, n+" ("+p+v+")")
	}
	return "found: " + firstNonEmpty(strings.Join(trouves, ", "), "none") + "; not found: " + firstNonEmpty(strings.Join(absents, ", "), "none")
}

func dossierExiste(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// configDe: an entry's "config" as a ConfigMCP.
func configDe(def objetJSON) *ConfigMCP {
	var c ConfigMCP
	b, _ := json.Marshal(def.objet("config"))
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

// consigneCorrection: what Claude is asked.
func consigneCorrection(sid string, def, secrets objetJSON, initial map[string]any, essais []EssaiCorrection) string {
	var b strings.Builder
	langue := languesCorrection[os.Getenv("LANGUAGE")]
	if langue == "" {
		langue = "English"
	}
	nom, _ := def.brut("nom").(string)
	source, _ := def.brut("source").(string)
	desc, _ := def.brut("description").(string)
	b.WriteString("You fix how Athanor starts an MCP server on this computer. Athanor is a tool that drives Claude Code; it keeps each MCP server as a configuration in its library, and its Test button starts the server exactly as Claude Code will (the MCP handshake: initialize, then tools/list). On this computer, the test fails.\n\n")
	b.WriteString("## This computer\n" + descriptionSysteme() + "\n" + descriptionCage() + "\nPrograms on the server's PATH: " + programmesTrouves() + "\n\n")
	b.WriteString("## The server\nid: " + sid + "\nname: " + nom + "\n")
	if source != "" {
		b.WriteString("where it comes from: " + source + "\n")
	}
	if desc != "" {
		b.WriteString("what it does: " + court(desc, 400) + "\n")
	}
	if p, _ := def.brut("plugin").(string); p != "" {
		b.WriteString("it comes with the Claude Code plugin " + p + " (${CLAUDE_PLUGIN_ROOT} is the plugin's folder, ${CLAUDE_PLUGIN_DATA} a folder for its data)\n")
	}
	b.WriteString("\nIts configuration in Athanor's library (a key or a setting appears as ${NAME}; {PROJECT} is the project's folder):\n" + jsonLisible(def.objet("config")) + "\n")
	var reglages, cles []string
	r := def.objet("reglages")
	for _, k := range r.cles {
		v := textePython(r.brut(k))
		if x, ok := secrets.brut(k).(string); ok && x != "" {
			v = x + " (the default is " + textePython(r.brut(k)) + ")"
		}
		reglages = append(reglages, k+" = "+v)
	}
	for _, k := range secretsRequisDef(def) {
		if r.a(k) {
			continue
		}
		etat := "not saved"
		if x, ok := secrets.brut(k).(string); ok && x != "" {
			etat = "saved"
		}
		cles = append(cles, k+" ("+etat+")")
	}
	if len(reglages) > 0 {
		b.WriteString("Its settings (not secret, shown as they are): " + strings.Join(reglages, "; ") + "\n")
	}
	if len(cles) > 0 {
		b.WriteString("Its keys (their values are never shown to you): " + strings.Join(cles, ", ") + "\n")
	}
	erreur, _ := initial["erreur"].(string)
	b.WriteString("\n## The test's error\n" + erreur + "\n")
	if d := lignesDe(initial["details"]); len(d) > 0 {
		b.WriteString("The server's last lines (stderr):\n" + tronquerRunes(strings.Join(d, "\n"), 3000) + "\n")
	}
	if len(essais) > 0 {
		b.WriteString("\n## Your earlier proposals, tested here\n")
		for i, e := range essais {
			b.WriteString(fmt.Sprintf("%d. %s\n   result: %s\n", i+1, court(jsonCompact(e.Config, e.Reglages), 1500), e.Erreur))
			if len(e.Details) > 0 {
				b.WriteString("   last lines: " + court(strings.Join(e.Details, " | "), 800) + "\n")
			}
		}
	}
	b.WriteString(`
## What to do
Find why it fails here and give a configuration of the same server that works on this system. Read the server's README or package page (WebFetch, WebSearch) when its command, package name or arguments are in doubt. Rules:
- The same server, for the same purpose: never another one.
- Keep its keys and settings as ${NAME} with the same names; never write a key's value. You may change a setting's default with "reglages".
- "type": "stdio" (a program started as described above), "http" or "sse" (a remote address, with "url" and "headers")` + map[bool]string{true: `, "windows" (a Windows program)`, false: ""}[systemeMCP() == "windows"] + `.
- Prefer what works on every system: npx -y PACKAGE, uvx PACKAGE, a remote address the project publishes. No absolute path of this user's home.
- A program missing on this computer (node, uv, python, docker…): never work around it, say which one in "manque".
- When the fault is Athanor's own way of starting servers (its cage, its PATH, a folder it cannot write, how it passes arguments), say what Athanor should change in "athanor" (and still give a configuration if one works around it).
Answer with ONLY one JSON object, no code fence, no text around it:
{"config": {"type": "...", "command": "...", "args": ["..."], "env": {"NAME": "..."}, "url": "...", "headers": {"NAME": "..."}}, "reglages": {}, "explication": "what was wrong and what you changed, one or two sentences in ` + langue + `", "manque": "", "athanor": ""}
("config": null when you give no configuration.)`)
	return b.String()
}

func jsonCompact(c *ConfigMCP, r map[string]string) string {
	b, _ := json.Marshal(map[string]any{"config": c, "reglages": r})
	return string(b)
}

func lignesDe(v any) []string {
	var l []string
	switch x := v.(type) {
	case []string:
		l = x
	case []any:
		for _, y := range x {
			if s, ok := y.(string); ok {
				l = append(l, s)
			}
		}
	}
	return l
}

// secretsRequisDef: the ${NAME} of an entry's configuration.
func secretsRequisDef(def objetJSON) []string {
	if c := configDe(def); c != nil {
		return secretsRequis(*c)
	}
	return nil
}

// demanderClaude: one session of Claude Code in an empty folder, allowed to read the web only; its answer.
var demanderClaude = func(consigne string, delai time.Duration) (string, error) {
	d, err := os.MkdirTemp("", "athanor-corriger-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(d)
	claude := trouverClaudePilote(estWindows)
	if claude == "" {
		claude = "claude"
	}
	a := []string{"-p", consigne, "--output-format", "text", "--max-turns", "25", "--allowedTools", "WebFetch,WebSearch", "--strict-mcp-config"}
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
		detail := strings.TrimSpace(erreurs.String())
		if detail == "" {
			detail = strings.TrimSpace(sortie.String())
		}
		return "", errors.New(court(derniereLigneTexte(firstNonEmpty(detail, err.Error())), 300))
	}
	return sortie.String(), nil
}

// lirePropositionMCP: the JSON object of Claude's answer (a code fence or a sentence around it tolerated).
func lirePropositionMCP(texte string) (*PropositionMCP, error) {
	i, j := strings.Index(texte, "{"), strings.LastIndex(texte, "}")
	if i < 0 || j < i {
		return nil, errors.New("no JSON in Claude's answer")
	}
	var p PropositionMCP
	if err := json.Unmarshal([]byte(texte[i:j+1]), &p); err != nil {
		return nil, errors.New("Claude's answer is not valid JSON: " + court(err.Error(), 120))
	}
	p.Explication, p.Manque, p.Athanor = court(strings.TrimSpace(p.Explication), 1200), court(strings.TrimSpace(p.Manque), 300), court(strings.TrimSpace(p.Athanor), 1500)
	return &p, nil
}

// validerPropositionMCP: a proposal Athanor can run: the same kinds of keys, an allowed type.
func validerPropositionMCP(avant *ConfigMCP, def objetJSON, p *PropositionMCP) error {
	c := p.Config
	if c == nil {
		return errors.New("no configuration")
	}
	if c.Type == "" {
		c.Type = "stdio"
		if c.URL != "" {
			c.Type = "http"
		}
	}
	ancien := ""
	if avant != nil {
		ancien = avant.Type
	}
	switch c.Type {
	case "stdio", "http", "sse":
	case "windows", "hote":
		if c.Type != ancien || (c.Type == "windows" && systemeMCP() != "windows") {
			return errors.New("type " + c.Type + " not allowed here")
		}
	default:
		return errors.New("unknown type " + c.Type)
	}
	if (c.Type == "http" || c.Type == "sse") && c.URL == "" {
		return errors.New("a remote server without an address")
	}
	if c.Type != "http" && c.Type != "sse" {
		c.URL, c.Headers = "", nil
	}
	if err := verifierConfig(*c); err != nil {
		return err
	}
	if len(c.Args) > 64 || len(c.Env) > 64 || len(c.Headers) > 32 || len(p.Reglages) > 16 {
		return errors.New("too many arguments")
	}
	connus := map[string]bool{}
	for k := range variablesPlugin {
		connus[k] = true
	}
	for _, k := range secretsRequisDef(def) {
		connus[k] = true
	}
	r := def.objet("reglages")
	for _, k := range r.cles {
		connus[k] = true
	}
	for k := range p.Reglages {
		if !connus[k] || !reVariableSure.MatchString(k) {
			return errors.New("unknown setting " + k)
		}
	}
	for _, k := range secretsRequis(*c) {
		if !connus[k] {
			return errors.New("a key the server did not have: " + k)
		}
	}
	return nil
}

// definitionProposee: the entry with Claude's configuration (and its settings' new defaults), the stored
// values of those settings left out (the new default is what is tested).
func definitionProposee(def, secrets objetJSON, p *PropositionMCP) (objetJSON, objetJSON) {
	n := copieObjet(def)
	n.mettre("config", p.Config)
	if len(p.Reglages) > 0 {
		r := n.objet("reglages")
		var cles []string
		for k := range p.Reglages {
			cles = append(cles, k)
		}
		sort.Strings(cles)
		for _, k := range cles {
			r.mettre(k, p.Reglages[k])
		}
		n.mettre("reglages", r)
	}
	var s objetJSON
	for _, k := range secrets.cles {
		if _, change := p.Reglages[k]; change {
			continue
		}
		s.mettre(k, secrets.brut(k))
	}
	return n, s
}

// corrigerServeurMCP: Claude's fix of a server whose test fails, tested here; etape says where it is.
func corrigerServeurMCP(kit, projet, sid string, etape func(string, int)) ResultatCorrection {
	res := ResultatCorrection{Systeme: systemeMCP()}
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
	if !biblio.a(sid) {
		res.Erreur = "unknown server"
		return res
	}
	def := biblio.objet(sid)
	secrets := lireJSONOrdonne(filepath.Join(kit, "extensions", "secrets.json")).objet(sid)
	etape("test", 0)
	initial := testerDefinition(kit, projet, sid, def, secrets, 150*time.Second)
	if ok, _ := initial["ok"].(bool); ok {
		res.OK, res.Deja = true, true
		res.Outils = lignesDe(initial["outils"])
		return res
	}
	res.Avant = configDe(def)
	res.Initiale, _ = initial["erreur"].(string)
	res.Details = lignesDe(initial["details"])
	if strings.HasPrefix(res.Initiale, "missing key") {
		res.Erreur = res.Initiale // a key to give, not a fix to find
		return res
	}
	for i := 1; i <= essaisCorrection; i++ {
		etape("claude", i)
		texte, err := demanderClaude(consigneCorrection(sid, def, secrets, initial, res.Essais), 6*time.Minute)
		if err != nil {
			res.Erreur = "Claude could not answer: " + err.Error()
			return res
		}
		p, err := lirePropositionMCP(texte)
		if err != nil {
			res.Essais = append(res.Essais, EssaiCorrection{Erreur: err.Error()})
			continue
		}
		res.Athanor = firstNonEmpty(p.Athanor, res.Athanor)
		if p.Manque != "" {
			res.Manque, res.Explication = p.Manque, p.Explication
			return res
		}
		if p.Config == nil && p.Athanor != "" {
			res.Explication = p.Explication
			res.Erreur = "no configuration works around it"
			return res
		}
		if err := validerPropositionMCP(res.Avant, def, p); err != nil {
			res.Essais = append(res.Essais, EssaiCorrection{Config: p.Config, Reglages: p.Reglages, Explication: p.Explication, Erreur: "not usable: " + err.Error()})
			continue
		}
		nd, ns := definitionProposee(def, secrets, p)
		etape("test", i)
		r := testerDefinition(kit, projet, sid, nd, ns, 150*time.Second)
		if ok, _ := r["ok"].(bool); ok {
			res.OK = true
			res.Config, res.Reglages, res.Explication = p.Config, p.Reglages, p.Explication
			res.Outils = lignesDe(r["outils"])
			return res
		}
		e, _ := r["erreur"].(string)
		res.Essais = append(res.Essais, EssaiCorrection{Config: p.Config, Reglages: p.Reglages, Explication: p.Explication, Erreur: e, Details: lignesDe(r["details"])})
	}
	res.Erreur = "no working configuration after " + fmt.Sprint(essaisCorrection) + " tries"
	if n := len(res.Essais); n > 0 {
		res.Explication = res.Essais[n-1].Explication
	}
	return res
}

// lancerCorrigerExt: athanor extensions corriger KIT PROJECT ID [--appliquer].
func lancerCorrigerExt(kit, projet string, args []string, w io.Writer) int {
	sid, appliquer := "", false
	for _, a := range args {
		if a == "--appliquer" {
			appliquer = true
		} else if sid == "" {
			sid = a
		}
	}
	if !reIdSur.MatchString(sid) {
		b, _ := json.Marshal(ResultatCorrection{Erreur: "invalid id", Systeme: systemeMCP()})
		fmt.Fprintln(w, string(b))
		return 0
	}
	chargerEnvKit(kit)
	res := corrigerServeurMCP(kit, projet, sid, func(quoi string, n int) { fmt.Fprintf(w, "ETAPE %s %d\n", quoi, n) })
	_ = ecrireRapportCorrection(kit, sid, res) // for everyone (ext_corrections.go)
	if appliquer && res.OK && res.Config != nil {
		if err := appliquerCorrectionMCP(Kit{Racine: kit}, sid, res); err != nil {
			res.OK, res.Erreur = false, "cannot write the library: "+err.Error()
		}
	}
	b, _ := json.Marshal(res)
	fmt.Fprintln(w, string(b))
	return 0
}
