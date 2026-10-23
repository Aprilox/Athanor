package main

// athanor hook fichiers ALLOWED… [--lecture=DIR…] | hook bash: the PreToolUse hooks of Claude's tools
// (replace system/guard.py and system/bash_guard.py, same decisions and messages). Exit code 2 = the
// action is refused and Claude reads why on stderr; any unreadable input lets the call through, as the
// scripts did (a hook never blocks Claude by accident).
//   fichiers  Claude's file tools (Read, Edit, Write, Glob, Grep…) only inside the allowed folders; the
//             --lecture folders are readable only; no writing inside .git, Claude Code's settings, .mcp.json
//   bash      no wait of more than 120 s, no closing Roblox Studio or Unreal by program name

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var outilsEcriture = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

var reLecteurGitBash = regexp.MustCompile(`^/([a-zA-Z])(/|$)`)

// windowsGarde: guard.py's windows(): on Windows, /c/x (Git Bash) is c:/x.
func windowsGarde(chemin string) string {
	if runtime.GOOS == "windows" {
		if m := reLecteurGitBash.FindStringSubmatch(chemin); m != nil {
			return m[1] + ":/" + chemin[min(3, len(chemin)):]
		}
	}
	return chemin
}

// realpathPython: Python's os.path.realpath (not strict): each existing link resolved where it stands,
// ".." applied after the links before it, what does not exist kept as written; then made absolute.
func realpathPython(p string) string {
	if runtime.GOOS == "windows" {
		return realpathWindows(p)
	}
	chemin, _ := joinRealpath("", p, map[string]*string{})
	return abspathPython(chemin)
}

func abspathPython(p string) string {
	if !strings.HasPrefix(p, "/") {
		wd, _ := os.Getwd()
		p = wd + "/" + p
	}
	return normpathPosix(p)
}

// normpathPosix: posixpath.normpath (a leading "//" stays, as POSIX allows).
func normpathPosix(p string) string {
	if p == "" {
		return "."
	}
	debut := ""
	if strings.HasPrefix(p, "/") {
		debut = "/"
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			debut = "//"
		}
	}
	var parts []string
	for _, c := range strings.Split(p, "/") {
		switch {
		case c == "" || c == ".":
		case c == "..":
			if debut == "" && (len(parts) == 0 || parts[len(parts)-1] == "..") {
				parts = append(parts, c)
			} else if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, c)
		}
	}
	r := debut + strings.Join(parts, "/")
	if r == "" {
		return "."
	}
	return r
}

func joinPosix(a, b string) string {
	if strings.HasPrefix(b, "/") || a == "" {
		return b
	}
	if strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

func splitPosix(p string) (string, string) {
	i := strings.LastIndex(p, "/") + 1
	tete, queue := p[:i], p[i:]
	if tete != "" && strings.Trim(tete, "/") != "" {
		tete = strings.TrimRight(tete, "/")
	}
	return tete, queue
}

// joinRealpath: posixpath._joinrealpath.
func joinRealpath(chemin, reste string, vus map[string]*string) (string, bool) {
	if strings.HasPrefix(reste, "/") {
		reste = reste[1:]
		chemin = "/"
	}
	for reste != "" {
		nom := reste
		if i := strings.Index(reste, "/"); i >= 0 {
			nom, reste = reste[:i], reste[i+1:]
		} else {
			reste = ""
		}
		if nom == "" || nom == "." {
			continue
		}
		if nom == ".." {
			if chemin != "" {
				var n string
				chemin, n = splitPosix(chemin)
				if n == ".." {
					chemin = joinPosix(joinPosix(chemin, ".."), "..")
				}
			} else {
				chemin = ".."
			}
			continue
		}
		nouveau := joinPosix(chemin, nom)
		st, err := os.Lstat(nouveau)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			chemin = nouveau
			continue
		}
		if v, deja := vus[nouveau]; deja {
			if v != nil {
				chemin = *v
				continue
			}
			return joinPosix(nouveau, reste), false // a loop of links
		}
		vus[nouveau] = nil
		cible, err := os.Readlink(nouveau)
		if err != nil {
			chemin = nouveau
			continue
		}
		var ok bool
		chemin, ok = joinRealpath(chemin, cible, vus)
		if !ok {
			return joinPosix(chemin, reste), false
		}
		c := chemin
		vus[nouveau] = &c
	}
	return chemin, true
}

// realpathWindows: the existing part of the path resolved by Windows, the rest as written.
func realpathWindows(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	tete, queue := a, ""
	for {
		if r, err := filepath.EvalSymlinks(tete); err == nil {
			if queue != "" {
				return filepath.Join(r, queue)
			}
			return r
		}
		d := filepath.Dir(tete)
		if d == tete {
			return a
		}
		queue = filepath.Join(filepath.Base(tete), queue)
		tete = d
	}
}

func propreGarde(chemin string) string {
	c := realpathPython(chemin)
	if runtime.GOOS == "windows" {
		return strings.ToLower(strings.ReplaceAll(c, "\\", "/"))
	}
	return c
}

// expanduserPython: "~" and "~/…" (the home folder), "~name/…" (that user's), the rest as it is.
func expanduserPython(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	i := strings.IndexAny(p, "/"+map[bool]string{true: "\\", false: ""}[runtime.GOOS == "windows"])
	if i < 0 {
		i = len(p)
	}
	nom := p[1:i]
	var base string
	if runtime.GOOS == "windows" {
		profil := os.Getenv("USERPROFILE")
		if profil == "" {
			profil = os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		}
		if profil == "" {
			return p
		}
		base = profil
		if nom != "" {
			base = filepath.Join(filepath.Dir(profil), nom)
		}
	} else if nom == "" {
		base = os.Getenv("HOME")
		if base == "" {
			if u, err := user.Current(); err == nil {
				base = u.HomeDir
			}
		}
		if base == "" {
			return p
		}
	} else {
		u, err := user.Lookup(nom)
		if err != nil {
			return p
		}
		base = u.HomeDir
	}
	if runtime.GOOS != "windows" {
		if r := strings.TrimRight(base, "/") + p[i:]; r != "" {
			return r
		}
		return "/"
	}
	return base + p[i:]
}

// absoluGarde: Python's isabs; on Windows, a path rooted without a drive (/x, \x) counts as absolute.
func absoluGarde(p string) bool {
	if runtime.GOOS == "windows" {
		return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\")
	}
	return strings.HasPrefix(p, "/")
}

func normaliserGarde(chemin, base string) string {
	chemin = expanduserPython(windowsGarde(chemin))
	if !absoluGarde(chemin) {
		if runtime.GOOS == "windows" {
			chemin = filepath.Join(windowsGarde(base), chemin)
		} else {
			chemin = joinPosix(windowsGarde(base), chemin)
		}
	}
	return propreGarde(chemin)
}

func dedansGarde(chemin string, racines []string) bool {
	for _, r := range racines {
		if chemin == r || strings.HasPrefix(chemin, strings.TrimRight(r, "/")+"/") {
			return true
		}
	}
	return false
}

var reLecteurWindows = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// gardeFichiers: "" when the call may go on, else the reason it is refused.
func gardeFichiers(args []string, entree []byte) string {
	var racines, lecture []string
	for _, r := range args {
		if strings.HasPrefix(r, "--lecture=") {
			lecture = append(lecture, propreGarde(windowsGarde(strings.TrimPrefix(r, "--lecture="))))
		} else {
			racines = append(racines, propreGarde(windowsGarde(r)))
		}
	}
	var e map[string]any
	if json.Unmarshal(entree, &e) != nil || e == nil {
		return ""
	}
	outil, _ := e["tool_name"].(string)
	param := dict(e["tool_input"])
	base, _ := e["cwd"].(string)
	if base == "" {
		base, _ = os.Getwd()
	}
	var candidats []string
	for _, cle := range []string{"file_path", "notebook_path", "path"} {
		if v, ok := param[cle]; ok && vraiPython(v) {
			candidats = append(candidats, textePython(v))
		}
	}
	if motif, ok := param["pattern"].(string); ok && outil == "Glob" &&
		(strings.HasPrefix(motif, "/") || strings.HasPrefix(motif, "~") || strings.Contains(motif, "..") || reLecteurWindows.MatchString(motif)) {
		c := strings.SplitN(motif, "*", 2)[0]
		if c == "" {
			c = "/"
		}
		candidats = append(candidats, c)
	}
	if len(candidats) == 0 && (outil == "Glob" || outil == "Grep") {
		candidats = append(candidats, base)
	}
	for _, c := range candidats {
		chemin := normaliserGarde(c, base)
		if !outilsEcriture[outil] && dedansGarde(chemin, lecture) {
			continue
		}
		if !dedansGarde(chemin, racines) {
			return fmt.Sprintf("Refused by the guard: '%s' is outside the allowed folders (%s). Work only inside the project or in $TOOLS.", c, strings.Join(racines, ", "))
		}
		if outilsEcriture[outil] && strings.Contains(chemin+"/", "/.git/") {
			return "Refused by the guard: do not modify the inside of .git; use the git commands."
		}
		nom := chemin[strings.LastIndexAny(chemin, "/")+1:]
		if runtime.GOOS == "windows" {
			nom = filepath.Base(chemin)
		}
		if outilsEcriture[outil] && ((strings.Contains(chemin, "/.claude/") && strings.HasPrefix(nom, "settings")) ||
			strings.Contains(chemin, "/.claude/hooks/") || nom == ".mcp.json") {
			return "Refused by the guard: Claude Code settings and .mcp.json are managed by the pilot."
		}
	}
	return ""
}

// ---------------------------------------------------------------- the Bash tool (bash_guard.py)

const limiteAttente = 120

var (
	reSleep        = regexp.MustCompile(`(?:^|[^\w.-])sleep\s+((?:\d+(?:\.\d+)?[smhd]?\s*)+)`)
	reSleepPart    = regexp.MustCompile(`(\d+(?:\.\d+)?)([smhd]?)`)
	reStartSleep   = regexp.MustCompile(`(?i)Start-Sleep\s+(?:-s(?:econds)?\s+)?(\d+(?:\.\d+)?)`)
	reStartSleepMs = regexp.MustCompile(`(?i)Start-Sleep\s+-m(?:illiseconds)?\s+(\d+)`)
	reTimeoutWin   = regexp.MustCompile(`(?i)(?:^|[^\w.-])timeout(?:\.exe)?\s+/+t\s+(\d+)`)
	reFermerProg   = regexp.MustCompile(`(?i)(taskkill|stop-process|pkill|killall|kill)\b[^\n;|&]*\b(robloxstudio|unrealeditor|ue4editor)`)
)

// secondesAttente: the longest single wait of a command (sleep, Start-Sleep, timeout /t), in seconds.
func secondesAttente(commande string) float64 {
	plus := 0.0
	unites := map[string]float64{"": 1, "s": 1, "m": 60, "h": 3600, "d": 86400}
	for _, m := range reSleep.FindAllStringSubmatch(commande, -1) {
		total := 0.0
		for _, p := range reSleepPart.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.ParseFloat(p[1], 64)
			total += n * unites[p[2]]
		}
		plus = max(plus, total)
	}
	// Start-Sleep N is in seconds unless it says -Milliseconds (bash_guard.py's (?!\s*-m) after the number)
	for _, ix := range reStartSleep.FindAllStringSubmatchIndex(commande, -1) {
		apres := strings.TrimLeft(commande[ix[1]:], " \t")
		if strings.HasPrefix(apres, "-m") {
			continue
		}
		n, _ := strconv.ParseFloat(commande[ix[2]:ix[3]], 64)
		plus = max(plus, n)
	}
	for _, m := range reStartSleepMs.FindAllStringSubmatch(commande, -1) {
		n, _ := strconv.ParseFloat(m[1], 64)
		plus = max(plus, n/1000)
	}
	for _, m := range reTimeoutWin.FindAllStringSubmatch(commande, -1) {
		n, _ := strconv.ParseFloat(m[1], 64)
		plus = max(plus, n)
	}
	return plus
}

func gardeBash(entree []byte) string {
	var e map[string]any
	if json.Unmarshal(entree, &e) != nil || e == nil {
		return ""
	}
	commande, _ := dict(e["tool_input"])["command"].(string)
	if commande == "" {
		return ""
	}
	if s := secondesAttente(commande); s > limiteAttente {
		return fmt.Sprintf("Refused by the pilot: a wait of %d s (more than %d s). Never wait for a screen, a "+
			"program or a reset: if a tool keeps failing (a Studio capture that times out, a program that does "+
			"not answer), try one quick fix, then note it in .athanor/BUGS.md and go on with what does not need "+
			"it. To wait for something that will come (a server starting), poll it with a short limit: "+
			"timeout 120 bash -c 'until CONDITION; do sleep 5; done'.", int(s), limiteAttente)
	}
	if reFermerProg.MatchString(commande) {
		return "Refused by the pilot: closing Roblox Studio or Unreal by program name would also close the user's " +
			"own windows. Roblox: roblox-studio --rebuild closes and reopens only this project's Studio; " +
			"roblox-studio --screen says whether Studio can draw right now."
	}
	return ""
}

func lancerHook(args []string) int {
	if len(args) == 0 || (args[0] != "fichiers" && args[0] != "bash") {
		fmt.Fprintln(os.Stderr, "usage: athanor hook fichiers ALLOWED... [--lecture=DIR...] | hook bash  (PreToolUse input on stdin)")
		return 2
	}
	entree, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<20))
	if err != nil {
		return 0
	}
	var raison string
	if args[0] == "fichiers" {
		raison = gardeFichiers(args[1:], entree)
	} else {
		raison = gardeBash(entree)
	}
	if raison != "" {
		fmt.Fprintln(os.Stderr, raison)
		return 2
	}
	return 0
}
