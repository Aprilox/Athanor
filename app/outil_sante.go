package main

// athanor sante ID [KIT]: one check of Setup & health, on the pilot's side (WSL or Linux, native Windows,
// macOS), with the lines and exit codes the application reads (its summary picks the lines that say
// MISSING, FAIL, ERROR, NOT FOUND, ABSENT, UNREACHABLE…). athanor sante --reparer ID [KIT]: the automatic
// repair of that check. athanor jeton-claude: Claude Code's own sign-in (claude setup-token), in a window.
// The commands run in the kit folder (the application starts them there), with the kit's .env loaded
// (KEY=VALUE lines, never executed) and ~/.local/bin and system/bin first in the PATH.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("sante", "sante ID [KIT] | sante --reparer ID [KIT]: one check of Setup & health and its automatic repair (the application)", lancerSante)
	enregistrerCommande("jeton-claude", "jeton-claude [KIT]: Claude Code's sign-in, which shows the subscription's token (claude setup-token; the application, in a window)", lancerJetonClaude)
	enregistrerCommande("reparation", "reparation INSTRUCTION_FILE: a Claude session that repairs the kit, outside the sandbox (the application's Setup & health)", lancerReparation)
	enregistrerCommande("tester-cage", "tester-cage: a short Claude session really tries to leave the sandbox (the application's Setup & health)", lancerTesterCage)
}

// kitSante: the kit folder of a check (its argument, else the current folder), with its .env loaded and
// the PATH completed.
func kitSante(args []string) string {
	kit := ""
	if len(args) > 0 && args[0] != "" {
		kit = cheminNatifPilote(args[0])
	} else if wd, err := os.Getwd(); err == nil {
		kit = wd
	}
	kit = abspathOS(kit)
	chargerEnvSante(kit)
	return kit
}

var reLigneEnvSante = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// chargerEnvSante: the kit's .env exported (as the scripts did: each KEY=VALUE line, never evaluated, the
// value as written), then ~/.local/bin and the kit's system/bin first in the PATH.
func chargerEnvSante(kit string) {
	if b, err := os.ReadFile(filepath.Join(kit, ".env")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSuffix(l, "\r")
			if m := reLigneEnvSante.FindStringSubmatch(l); m != nil {
				_ = os.Setenv(m[1], m[2])
			}
		}
	}
	prefixerPath(filepath.Join(maisonSante(), ".local", "bin"), filepath.Join(kit, "system", "bin"))
}

func maisonSante() string {
	if h := os.Getenv("HOME"); h != "" && runtime.GOOS != "windows" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// prefixerPath: these folders first in the PATH (in this order).
func prefixerPath(dossiers ...string) {
	sep := string(os.PathListSeparator)
	_ = os.Setenv("PATH", strings.Join(dossiers, sep)+sep+os.Getenv("PATH"))
}

// sortieSante: what a check writes (the application reads both streams together).
var sortieSante io.Writer = os.Stdout

func direSante(format string, a ...any) { fmt.Fprintf(sortieSante, format+"\n", a...) }

// executerSante: a program with this command's outputs; its exit code (127: not found, as a shell says).
func executerSante(nom string, args ...string) int {
	chemin, err := exec.LookPath(nom)
	if err != nil {
		direSante("%s: command not found", nom)
		return 127
	}
	c := commande(chemin, args...)
	c.Stdout, c.Stderr = sortieSante, sortieSante
	return codeSortieSante(c.Run(), c)
}

func codeSortieSante(err error, c *exec.Cmd) int {
	if err == nil {
		return 0
	}
	if c != nil && c.ProcessState != nil {
		if code := c.ProcessState.ExitCode(); code >= 0 {
			return code
		}
		return 1
	}
	if errors.Is(err, exec.ErrNotFound) {
		return 127
	}
	return 126
}

// sortieDe: a program's output (both streams) and its exit code, stopped after delai (code 124, as timeout).
func sortieDe(delai time.Duration, nom string, args ...string) (string, int) {
	chemin, err := exec.LookPath(nom)
	if err != nil {
		return nom + ": command not found", 127
	}
	ctx, fin := context.WithTimeout(context.Background(), delai)
	defer fin()
	c := commandeContexte(ctx, chemin, args...)
	c.WaitDelay = 5 * time.Second
	var b strings.Builder
	c.Stdout, c.Stderr = &b, &b
	err = c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return b.String(), 124
	}
	return b.String(), codeSortieSante(err, c)
}

func premiereLigneSante(s string) string {
	l, _, _ := strings.Cut(strings.ReplaceAll(s, "\r", ""), "\n")
	return l
}

func lancerSante(args []string) int {
	reparer := false
	if len(args) > 0 && args[0] == "--reparer" {
		reparer, args = true, args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: athanor sante [--reparer] ID [KIT]")
		return 2
	}
	id := args[0]
	kit := kitSante(args[1:])
	if err := os.Chdir(kit); err != nil {
		fmt.Fprintln(os.Stderr, "Tool folder not accessible: "+kit)
		return 1
	}
	if reparer {
		return reparerSante(id, kit)
	}
	f := verifSante(runtime.GOOS, id)
	if f == nil {
		fmt.Fprintf(os.Stderr, "athanor sante: unknown check %q\n", id)
		return 2
	}
	return f(kit)
}

// verifSante: the check ID of this system (the lists of sante.go, natif.go and sante_mac.go).
func verifSante(goos, id string) func(kit string) int {
	communs := map[string]func(string) int{"jeton": santeJeton, "outils_pc": santeOutilsPC, "outils_kit": santeOutilsKit}
	var propres map[string]func(string) int
	switch goos {
	case "windows":
		propres = map[string]func(string) int{"git_bash": santeGitBash, "outils": santeOutilsNatifs, "claude": santeClaudeNatif}
	case "darwin":
		propres = map[string]func(string) int{"macos": santeMacOS, "internet": santeInternetMac, "paquets": santePaquetsMac, "barriere": santeBarriereMac, "claude": santeClaude}
	default:
		propres = map[string]func(string) int{"wsl": santeWSL, "internet": santeInternet, "paquets": santePaquets, "barriere": santeBarriere,
			"claude": santeClaude, "rtk": santeRTK, "docker": santeDocker, "admin": santeAdmin}
	}
	if f := propres[id]; f != nil {
		return f
	}
	return communs[id]
}

// ---------------------------------------------------------------------------- the checks

func santeWSL(string) int {
	noyau, _ := sortieDe(10*time.Second, "uname", "-r")
	nom := ""
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, "PRETTY") {
				if _, v, ok := strings.Cut(l, "="); ok {
					nom = v
				}
				break
			}
		}
	}
	direSante("WSL_OK %s %s", strings.TrimSpace(noyau), nom)
	return 0
}

// santeInternet: claude.ai's name resolves (as getent hosts), then an answer from it (as curl -s, no redirect
// followed: its HTTP code; 000 when nothing answered).
func santeInternet(string) int {
	if _, err := net.LookupHost("claude.ai"); err != nil {
		return 2
	}
	return santeInternetMac("")
}

func santeInternetMac(string) int {
	cl := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := cl.Get("https://claude.ai/")
	if err != nil {
		direSante("claude.ai reachable (code 000)")
		return 1
	}
	io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
	direSante("claude.ai reachable (code %03d)", r.StatusCode)
	return 0
}

func listerProgrammes(noms []string) int {
	m := 0
	for _, p := range noms {
		if _, err := exec.LookPath(p); err == nil {
			direSante("ok  %s", p)
		} else {
			direSante("MISSING %s", p)
			m = 1
		}
	}
	return m
}

func santePaquets(string) int {
	return listerProgrammes([]string{"git", "jq", "curl", "node", "npm", "bwrap", "socat", "rg", "convert", "ffmpeg", "zip", "Xvfb", "ssh", "gh"})
}

func santePaquetsMac(string) int {
	prefixerPath("/opt/homebrew/bin", "/usr/local/bin")
	return listerProgrammes([]string{"brew", "git", "jq", "curl", "node", "npm", "rg", "magick", "ffmpeg", "gh"})
}

func santeBarriere(string) int {
	if c := executerSante("bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "true"); c != 0 {
		return c
	}
	direSante("bubblewrap OK")
	if _, c := sortieDe(2*time.Minute, "npm", "ls", "-g", "@anthropic-ai/sandbox-runtime"); c != 0 {
		direSante("seccomp filter MISSING")
		return 2
	}
	direSante("seccomp filter OK")
	return 0
}

// santeBarriereMac: Seatbelt really refuses a write it was told to refuse.
func santeBarriereMac(string) int {
	const se = "/usr/bin/sandbox-exec"
	if _, err := os.Stat(se); err != nil {
		direSante("sandbox-exec MISSING (%s)", se)
		return 1
	}
	d, err := os.MkdirTemp("", "athanor-seatbelt-")
	if err != nil {
		direSante("Seatbelt check IMPOSSIBLE: %v", err)
		return 1
	}
	defer os.RemoveAll(d)
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r // /var/folders is /private/var/folders: Seatbelt matches the real path
	}
	c := commande(se, "-p", `(version 1)(allow default)(deny file-write* (subpath "`+d+`"))`, "/usr/bin/touch", filepath.Join(d, "x"))
	_ = c.Run()
	if _, err := os.Stat(filepath.Join(d, "x")); err == nil {
		direSante("sandbox-exec does NOT block writing")
		return 1
	}
	direSante("Seatbelt OK")
	return 0
}

func santeClaude(string) int { return executerSante("claude", "--version") }

func santeRTK(kit string) int {
	r := filepath.Join(kit, "system", "bin", "rtk")
	if st, err := os.Stat(r); err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
		return 1
	}
	return executerSante(r, "--version")
}

func santeDocker(string) int {
	if _, c := sortieDe(2*time.Minute, "docker", "info"); c != 0 {
		direSante("Docker UNREACHABLE (optional)")
		return 1
	}
	direSante("Docker reachable")
	return 0
}

// santeAdmin: the scheduled task "Athanor - installations" exists (WSL: through schtasks.exe).
func santeAdmin(string) int {
	s := "/mnt/c/Windows/System32/schtasks.exe"
	if st, err := os.Stat(s); err != nil || st.Mode()&0o111 == 0 {
		direSante("schtasks UNREACHABLE")
		return 1
	}
	c := commande(s, "/query", "/tn", "Athanor - installations")
	if st, err := os.Stat("/mnt/c"); err == nil && st.IsDir() {
		c.Dir = "/mnt/c" // a WSL folder as the current one makes Windows warn
	}
	if c.Run() != nil {
		direSante("Task ABSENT: Claude can only install portable software")
		return 1
	}
	// a task made by an older version runs the former PowerShell script, removed since: allowed again
	if presente, aJour := tacheAdminEtat(); presente && !aJour {
		direSante("Task OUTDATED (made by an older version): Repair allows it again (one UAC prompt)")
		return 1
	}
	direSante("Task present")
	return 0
}

// santeOutilsPC: the PC tools detected (athanor detecter, into logs/detection), the head of PC_TOOLS.md.
func santeOutilsPC(kit string) int {
	projet := filepath.Join(kit, "logs", "detection")
	_ = os.MkdirAll(projet, 0o755)
	_ = os.MkdirAll(filepath.Join(kit, "tools"), 0o755)
	_ = os.Setenv("PROJECT", projet)
	if exe, err := os.Executable(); err == nil {
		c := commande(exe, "detecter", kit)
		c.Dir = kit
		_ = c.Run()
	}
	f := filepath.Join(projet, ".athanor", "PC_TOOLS.md")
	b, err := os.ReadFile(f)
	if err != nil {
		direSante("sed: can't read %s: No such file or directory", f)
		return 2
	}
	return imprimerLignes(string(b), 3, 25)
}

// imprimerLignes: lines de..a (from 1) of a text, as sed -n 'de,ap'.
func imprimerLignes(texte string, de, a int) int {
	sc := bufio.NewScanner(strings.NewReader(texte))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan() && n <= a; n++ {
		if n >= de {
			direSante("%s", sc.Text())
		}
	}
	return 0
}

// santeOutilsKit: the executable passes its autotest, and the kit has its texts.
func santeOutilsKit(kit string) int {
	nom := "athanor"
	if runtime.GOOS == "windows" {
		nom = "Athanor.exe"
	}
	exe, err := os.Executable()
	if err != nil {
		direSante("%s ERROR: its autotest fails (Rebuild Athanor)", nom)
		return 1
	}
	c := commande(exe, "autotest", kit)
	c.Dir = kit
	if c.Run() != nil {
		direSante("%s ERROR: its autotest fails (Rebuild Athanor)", nom)
		return 1
	}
	if st, err := os.Stat(filepath.Join(kit, "template", "CLAUDE.md")); err != nil || !st.Mode().IsRegular() {
		direSante("ERROR: template/CLAUDE.md is missing (download the kit again)")
		return 1
	}
	direSante("kit OK")
	return 0
}

// santeJeton: the token is in .env, and a tiny Haiku session answers with it.
func santeJeton(kit string) int {
	b, _ := os.ReadFile(filepath.Join(kit, ".env"))
	if !regexp.MustCompile(`(?m)^CLAUDE_CODE_OAUTH_TOKEN=.{20,}`).Match(b) {
		direSante("Token ABSENT from .env (Settings)")
		return 3
	}
	_ = os.Setenv("ENABLE_CLAUDEAI_MCP_SERVERS", "false")
	sortie, code := sortieDe(120*time.Second, "claude", "-p", "Answer exactly: TOKEN_OK", "--model", "haiku", "--max-turns", "1")
	if strings.Contains(sortie, "TOKEN_OK") {
		direSante("Connection OK")
		return 0
	}
	lignes := strings.Split(strings.TrimRight(strings.ReplaceAll(sortie, "\r", ""), "\n"), "\n")
	derniere := ""
	for i := len(lignes) - 1; i >= 0; i-- {
		if strings.TrimSpace(lignes[i]) != "" {
			derniere = lignes[i]
			break
		}
	}
	direSante("Test session FAILED (code %d): %s", code, couperCaracteres(derniere, 200))
	direSante("--- full output ---")
	if len(lignes) > 12 {
		lignes = lignes[len(lignes)-12:]
	}
	for _, l := range lignes {
		direSante("%s", l)
	}
	return 1
}

// ---------------------------------------------------------------------------- native Windows

func santeGitBash(string) int {
	b := trouverBashNatifPilote()
	if b == "" {
		direSante("Native Windows mode: Git for Windows (Git Bash) is missing. Setup & health -> Install everything, or Options -> back to the WSL mode.")
		return 1
	}
	v, code := sortieDe(30*time.Second, b, "--version")
	if code != 0 || strings.TrimSpace(v) == "" {
		direSante("Git Bash ERROR: %s does not run (code %d)", b, code)
		return 1
	}
	direSante("Git Bash OK: %s", premiereLigneSante(v))
	return 0
}

// jqWindows: jq.exe in the PATH, else where winget puts it, else in the kit's tools/bin.
func jqWindows(kit string) string {
	if j, err := exec.LookPath("jq.exe"); err == nil {
		return j
	}
	la := os.Getenv("LOCALAPPDATA")
	var c []string
	if la != "" {
		c = append(c, filepath.Join(la, "Microsoft", "WinGet", "Links", "jq.exe"))
		for _, motif := range []string{"jq.exe", "jq-windows-amd64.exe"} {
			g, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "jqlang.jq_*", motif))
			c = append(c, g...)
		}
	}
	c = append(c, `C:\ProgramData\chocolatey\bin\jq.exe`, filepath.Join(kit, "tools", "bin", "jq.exe"))
	for _, f := range c {
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
			return f
		}
	}
	return ""
}

// santeOutilsNatifs: the Windows programs the pilot needs, with their version (python, node, gh and uv
// are optional). A program counts when it is found and answers --version.
func santeOutilsNatifs(kit string) int {
	m := 0
	for _, p := range []string{"git", "jq", "curl", "python", "node", "gh", "uv"} {
		prog := p
		if p == "jq" {
			prog = jqWindows(kit)
		}
		v, code := "", 127
		if prog != "" {
			v, code = sortieDe(60*time.Second, prog, "--version")
		}
		v = strings.TrimSpace(premiereLigneSante(v))
		if code == 0 && v != "" {
			direSante("ok  %s: %s", p, v)
			continue
		}
		switch p {
		case "python", "node", "gh", "uv":
			direSante("MISSING %s (optional)", p)
		default:
			direSante("MISSING %s", p)
			m = 1
		}
	}
	return m
}

var reVersionClaudeSante = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// santeClaudeNatif: Claude Code for Windows, recent enough for the native mode.
func santeClaudeNatif(string) int {
	v, _ := sortieDe(60*time.Second, "claude", "--version")
	v = premiereLigneSante(v)
	direSante("%s", v)
	n := reVersionClaudeSante.FindString(v)
	if n == "" {
		return 1
	}
	if !versionPlusRecenteOuEgale(n, "2.1.281") {
		direSante("Claude Code %s is too OLD for the native mode (2.1.281 at least, and the latest models): Install everything updates it", n)
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------- macOS

func santeMacOS(string) int {
	v, _ := sortieDe(10*time.Second, "sw_vers", "-productVersion")
	m, _ := sortieDe(10*time.Second, "uname", "-m")
	direSante("macOS %s (%s)", strings.TrimSpace(v), strings.TrimSpace(m))
	return 0
}

// ---------------------------------------------------------------------------- repairs

func reparerSante(id, kit string) int {
	switch id {
	case "claude":
		return reparerClaude()
	case "rtk":
		return reparerRTK(kit)
	case "outils_kit":
		return reparerOutilsKit(kit)
	}
	fmt.Fprintf(os.Stderr, "athanor sante: no automatic repair for %q\n", id)
	return 2
}

// reparerClaude: Anthropic's installer (https://claude.ai/install.sh) run with sh, then ~/.local/bin in
// the PATH of the user's shells (Linux).
func reparerClaude() int {
	cl := &http.Client{Timeout: 2 * time.Minute}
	r, err := cl.Get("https://claude.ai/install.sh")
	if err != nil {
		direSante("Download of Claude Code's installer FAILED: %v", err)
		return 1
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		direSante("Download of Claude Code's installer FAILED: HTTP %d", r.StatusCode)
		return 1
	}
	f, err := os.CreateTemp("", "claude-install-*.sh")
	if err != nil {
		direSante("Claude Code's installer IMPOSSIBLE: %v", err)
		return 1
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, io.LimitReader(r.Body, 20<<20))
	f.Close()
	if err != nil {
		direSante("Download of Claude Code's installer FAILED: %v", err)
		return 1
	}
	if code := executerSante("sh", f.Name()); code != 0 {
		return code
	}
	if runtime.GOOS == "linux" {
		ajouterPathBashrc(filepath.Join(maisonSante(), ".bashrc"))
	}
	return 0
}

// ajouterPathBashrc: the line that puts ~/.local/bin in the PATH, unless .bashrc already names it.
func ajouterPathBashrc(f string) {
	b, _ := os.ReadFile(f)
	if strings.Contains(string(b), ".local/bin") {
		return
	}
	g, err := os.OpenFile(f, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer g.Close()
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		g.WriteString("\n")
	}
	g.WriteString(`export PATH="$HOME/.local/bin:$PATH"` + "\n")
}

func reparerRTK(kit string) int {
	cible := cibleRTK(runtime.GOOS, runtime.GOARCH)
	if cible == "" {
		direSante("RTK: no build for this system (%s/%s)", runtime.GOOS, runtime.GOARCH)
		return 1
	}
	dest := filepath.Join(kit, "system", "bin", "rtk")
	if err := telechargerRTK("https://github.com/rtk-ai/rtk/releases/download/"+versionRTK+"/rtk-"+cible+".tar.gz", dest); err != nil {
		direSante("RTK download FAILED: %v", err)
		return 1
	}
	return executerSante(dest, "--version")
}

// reparerOutilsKit: the kit's executable gets its execute bits back (an archive extracted without them).
func reparerOutilsKit(kit string) int {
	for _, nom := range []string{"athanor", filepath.Join("system", "bin", "rtk")} {
		f := filepath.Join(kit, nom)
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
			_ = os.Chmod(f, st.Mode()|0o111)
		}
	}
	direSante("permissions restored")
	return 0
}

// lancerJetonClaude: claude setup-token in this window (sign in in the browser, the token is shown).
func lancerJetonClaude(args []string) int {
	kitSante(args)
	chemin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Claude Code NOT FOUND: repair the \"Claude Code\" line first.")
		return 127
	}
	c := commande(chemin, "setup-token")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return codeSortieSante(c.Run(), c)
}
