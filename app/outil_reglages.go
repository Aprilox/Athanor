package main

// athanor reglages KIT OUT | chat settings PROJECT OUT: the Claude Code settings of a cycle (the pilot) and
// of the app's chat (replace system/settings.py and system/chat.py, same settings; the hooks they declare
// are this executable's: athanor hook fichiers | bash).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var reSurShlex = regexp.MustCompile(`[^\w@%+=:,./-]`)

// shlexQuote: Python's shlex.quote.
func shlexQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !reSurShlex.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func ligneShell(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shlexQuote(a)
	}
	return strings.Join(q, " ")
}

// cheminReglages: settings.py's chemin(): native Windows, a Git Bash path (/c/x) as Windows knows it (c:/x).
func cheminReglages(p string) string {
	if runtime.GOOS == "windows" && len(p) >= 2 && p[0] == '/' && estLettre(p[1]) && (len(p) == 2 || p[2] == '/') {
		return p[1:2] + ":/" + p[min(3, len(p)):]
	}
	return p
}

func estLettre(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// exeHook: this executable, as a hook command names it (forward slashes on Windows, for Git Bash).
func exeHook() string {
	e, err := os.Executable()
	if err != nil {
		e = "athanor"
	}
	if runtime.GOOS == "windows" {
		e = strings.ReplaceAll(e, "\\", "/")
	}
	return e
}

func joinOS(a ...string) string { return filepath.Join(a...) }

var reIdSur = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func normaliserExt(p string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/"))
}

// choixDuProjetKit: extensions.py's choix_du_projet: the project's skills, MCP servers and plugins in
// extensions/projets.json (the app writes the folder as it knows it; the WSL form is compared too).
func choixDuProjetKit(kit, projet string) map[string][]string {
	out := map[string][]string{"skills": {}, "mcp": {}, "plugins": {}}
	b, err := os.ReadFile(filepath.Join(kit, "extensions", "projets.json"))
	if err != nil {
		return out
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var projets objetJSON
	if json.Unmarshal(b, &projets) != nil {
		return out
	}
	noms := map[string]bool{normaliserExt(projet): true, normaliserExt(realpathPython(projet)): true}
	if _, err := exec.LookPath("wslpath"); err == nil {
		if w, err := exec.Command("wslpath", "-w", projet).Output(); err == nil {
			if s := strings.TrimSpace(string(w)); s != "" {
				noms[normaliserExt(s)] = true
			}
		}
	}
	for _, cle := range projets.cles {
		if !noms[normaliserExt(cle)] {
			continue
		}
		var v map[string]any
		if json.Unmarshal(projets.valeur[cle], &v) != nil || v == nil {
			continue
		}
		for _, k := range []string{"skills", "mcp", "plugins"} {
			for _, x := range liste(v[k]) {
				if s, ok := x.(string); ok && reIdSur.MatchString(s) {
					out[k] = append(out[k], s)
				}
			}
		}
		return out
	}
	return out
}

var reNonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func ecrireJSONPython(chemin string, v any, indent string) error {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", indent)
	if err := e.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(chemin, bytes.TrimRight(b.Bytes(), "\n"), 0o644)
}

func construireReglages(kitArg, sortie string) error {
	for _, v := range []string{"PROJECT", "ATHANOR_PROJET_PRINCIPAL", "ATHANOR_PLUGIN", "ATHANOR_PLUGINS"} {
		if x := os.Getenv(v); x != "" {
			os.Setenv(v, cheminReglages(x))
		}
	}
	natif := runtime.GOOS == "windows"
	kit := realpathPython(kitArg)
	tools, system := joinOS(kit, "tools"), joinOS(kit, "system")
	projetBrut := firstNonEmpty(os.Getenv("PROJECT"), joinOS(kit, "projet"))
	projet := realpathPython(projetBrut)
	ensemble := map[string]bool{projet: true, abspathOS(projetBrut): true}
	var projets []string
	for p := range ensemble {
		projets = append(projets, p)
	}
	sort.Strings(projets)
	unreal := false
	for _, m := range choixDuProjetKit(kit, firstNonEmpty(os.Getenv("ATHANOR_PROJET_PRINCIPAL"), firstNonEmpty(os.Getenv("PROJECT"), projet)))["mcp"] {
		unreal = unreal || m == "unreal"
	}
	plugin, tiers := os.Getenv("ATHANOR_PLUGIN"), os.Getenv("ATHANOR_PLUGINS")
	lectures := []string{system}
	if plugin != "" {
		lectures = append(lectures, plugin)
	}
	if tiers != "" {
		lectures = append(lectures, tiers)
	}
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = joinOS(expanduserPython("~"), ".claude")
	}
	vus := map[string]bool{os.Getenv("PROJECT"): true, projet: true}
	var ps []string
	for p := range vus {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	for _, p := range ps {
		if p != "" {
			lectures = append(lectures, joinOS(config, "projects", reNonAlnum.ReplaceAllString(p, "-")))
		}
	}
	rtkBin := joinOS(system, "bin", "rtk")
	rtk := os.Getenv("RTK") != "0"
	if st, err := os.Stat(rtkBin); err != nil || st.Mode()&0o111 == 0 {
		rtk = false
	}
	var logiciels []string
	if b, err := os.ReadFile(joinOS(system, "pc_tools.env")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			cle, val, _ := strings.Cut(strings.TrimSpace(l), "=")
			val = strings.Trim(val, "'")
			switch {
			case (cle == "UE_DIR" || cle == "ANDROID_SDK_DIR" || cle == "ANDROID_JDK_DIR") && val != "":
				logiciels = append(logiciels, val)
			case cle == "BLENDER_EXE" && val != "":
				logiciels = append(logiciels, dirnameOS(val))
			}
		}
	}
	passerelles := []string{"tool", "unreal-editor", "unreal-cmd", "unreal-uat", "blender", "roblox-studio", "install-windows", "pc-exec", "windows-var", "adb"}
	allow := []string{"WebFetch", "WebFetch(domain:*)", "WebSearch"}
	exclus := []string{}
	for _, p := range passerelles {
		allow = append(allow, "Bash("+p+" *)")
		exclus = append(exclus, p+" *")
	}
	if unreal {
		allow = append(allow, "mcp__unreal")
	}
	var permissions objetJSON
	permissions.mettre("defaultMode", "bypassPermissions")
	permissions.mettre("allow", allow)
	permissions.mettre("deny", []string{"Bash(git push --force *)", "Bash(git push -f *)"})
	permissions.mettre("additionalDirectories", []string{tools})
	var env objetJSON
	env.mettre("TOOLS", tools)
	env.mettre("PROJECT", projet)
	env.mettre("ATHANOR_PONT", joinOS(firstNonEmpty(os.Getenv("ATHANOR_PROJET_PRINCIPAL"), firstNonEmpty(os.Getenv("PROJECT"), joinOS(kit, "projet"))), ".athanor", "pont"))
	env.mettre("XDG_CACHE_HOME", joinOS(tools, "cache"))
	env.mettre("PIP_CACHE_DIR", joinOS(tools, "cache", "pip"))
	env.mettre("npm_config_cache", joinOS(tools, "cache", "npm"))
	env.mettre("PIP_DISABLE_PIP_VERSION_CHECK", "1")
	env.mettre("BASH_DEFAULT_TIMEOUT_MS", "600000")
	env.mettre("BASH_MAX_TIMEOUT_MS", "900000")
	var sandbox objetJSON
	sandbox.mettre("enabled", true)
	sandbox.mettre("failIfUnavailable", true)
	sandbox.mettre("autoAllowBashIfSandboxed", true)
	sandbox.mettre("allowUnsandboxedCommands", false)
	sandbox.mettre("excludedCommands", exclus)
	var fs objetJSON
	fs.mettre("allowWrite", []string{tools})
	fs.mettre("denyRead", []string{"~/", "/mnt"})
	fs.mettre("allowRead", append(append(append(append([]string{}, projets...), tools), lectures...), logiciels...))
	sandbox.mettre("filesystem", fs)
	sandbox.mettre("network", map[string]any{"allowedDomains": []string{"*"}})
	var creds []map[string]string
	for _, v := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		creds = append(creds, map[string]string{"mode": "deny", "name": v})
	}
	var credObj []objetJSON
	for _, c := range creds {
		var o objetJSON
		o.mettre("name", c["name"])
		o.mettre("mode", c["mode"])
		credObj = append(credObj, o)
	}
	sandbox.mettre("credentials", map[string]any{"envVars": credObj})
	tmp := "/tmp"
	if natif {
		tmp = os.TempDir()
	}
	exe := exeHook()
	garde := []string{exe, "hook", "fichiers", projet, tools, tmp}
	for _, d := range lectures {
		garde = append(garde, "--lecture="+d)
	}
	for _, d := range logiciels {
		garde = append(garde, "--lecture="+d)
	}
	crochet := func(matcher, commande string) objetJSON {
		var h objetJSON
		h.mettre("type", "command")
		h.mettre("timeout", 10)
		h.mettre("command", commande)
		var c objetJSON
		c.mettre("matcher", matcher)
		c.mettre("hooks", []objetJSON{h})
		return c
	}
	crochets := []objetJSON{
		crochet("Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep", ligneShell(garde...)),
		crochet("Bash", ligneShell(exe, "hook", "bash")),
	}
	if natif {
		rtk = false // RTK here is a Linux program
		env.mettre("PONT_DIRECT", "1")
		env.mettre("MSYS2_ARG_CONV_EXCL", "")
		for _, k := range env.cles {
			var s string
			if json.Unmarshal(env.valeur[k], &s) == nil {
				env.mettre(k, strings.ReplaceAll(s, "\\", "/"))
			}
		}
		permissions.mettre("additionalDirectories", []string{strings.ReplaceAll(tools, "\\", "/")})
	}
	if rtk {
		crochets = append(crochets, crochet("Bash", "exec "+shlexQuote(rtkBin)+" hook claude"))
	}
	var r objetJSON
	r.mettre("language", "english")
	r.mettre("permissions", permissions)
	r.mettre("env", env)
	if !natif {
		r.mettre("sandbox", sandbox)
	}
	r.mettre("enableAllProjectMcpServers", unreal)
	r.mettre("enabledPlugins", pluginsUtilisateur(config))
	r.mettre("hooks", map[string]any{"PreToolUse": crochets})
	if os.Getenv("ATHANOR_SANS_COAUTEUR") == "1" {
		r.mettre("attribution", map[string]string{"commit": "", "pr": ""})
		r.mettre("includeCoAuthoredBy", false)
	}
	if d := filepath.Dir(sortie); d != "" {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := ecrireJSONPython(sortie, r, "  "); err != nil {
		return err
	}
	fmt.Println(sortie)
	return nil
}

// abspathOS: Python's os.path.abspath.
func abspathOS(p string) string {
	if runtime.GOOS == "windows" {
		a, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return a
	}
	return abspathPython(p)
}

func dirnameOS(p string) string {
	if runtime.GOOS == "windows" {
		return filepath.Dir(p)
	}
	tete, _ := splitPosix(p)
	return tete
}

func lancerReglages(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor reglages KIT OUTPUT.json   (PROJECT in the environment)")
		return 2
	}
	if err := construireReglages(args[0], args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// pluginsUtilisateur: the plugins switched on in the user's own Claude Code (CONFIG/settings.json), all
// off in Athanor's sessions (their hooks would run outside the sandbox; Athanor's come from its Plugins tab).
func pluginsUtilisateur(config string) objetJSON {
	var r objetJSON
	r.valeur = map[string]json.RawMessage{}
	b, err := os.ReadFile(joinOS(config, "settings.json"))
	if err != nil {
		return r
	}
	var s objetJSON
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &s) != nil {
		return r
	}
	p := s.objet("enabledPlugins")
	for _, k := range p.cles {
		if v, ok := p.brut(k).(bool); ok && v {
			r.mettre(k, false)
		}
	}
	return r
}

// ---------------------------------------------------------------- the app's chat (chat.py)

// reglagesChat: Claude's file tools stay inside PROJECT (the file guard), no command, no web. The chat runs
// short sessions in the project's folder with read-only tools (and Edit/Write while the pilot is stopped).
func reglagesChat(projet, sortie string) error {
	commande := ligneShell(exeHook(), "hook", "fichiers", projet)
	if runtime.GOOS == "windows" {
		commande = ligneShell(exeHook(), "hook", "fichiers", strings.ReplaceAll(projet, "\\", "/"))
	}
	var h objetJSON
	h.mettre("type", "command")
	h.mettre("timeout", 10)
	h.mettre("command", commande)
	var c objetJSON
	c.mettre("matcher", "Read|Edit|Write|MultiEdit|NotebookEdit|Glob|Grep")
	c.mettre("hooks", []objetJSON{h})
	var r objetJSON
	r.mettre("permissions", map[string]any{"deny": []string{"Bash", "WebFetch", "WebSearch", "NotebookEdit"}})
	r.mettre("hooks", map[string]any{"PreToolUse": []objetJSON{c}})
	r.mettre("includeCoAuthoredBy", false)
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = joinOS(expanduserPython("~"), ".claude")
	}
	r.mettre("enabledPlugins", pluginsUtilisateur(config))
	d := filepath.Dir(sortie)
	if d != "" {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(firstNonEmpty(d, "."), ".chat-")
	if err != nil {
		return err
	}
	tmp.Close()
	if err := ecrireJSONPython(tmp.Name(), r, " "); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), sortie)
}

func lancerChatOutil(args []string) int {
	if len(args) != 3 || args[0] != "settings" {
		fmt.Fprintln(os.Stderr, "usage: athanor chat settings PROJECT OUT")
		return 2
	}
	if err := reglagesChat(args[1], args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
