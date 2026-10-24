package main

// athanor configurer: the configuration assistant (commands/configure.sh hands over to it). A few questions,
// written into the tool's .env (settings shared by all projects); the current answers are offered as
// defaults (Enter). The app does the same from its Settings page.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type assistantEnv struct {
	env                          string // the .env file
	lecteur                      *bufio.Reader
	err                          io.Writer
	gras, vert, jaune, gris, fin string
	existe                       bool
	reponse                      string
}

func (a *assistantEnv) valeur(cle string) string {
	b, err := os.ReadFile(a.env)
	if err != nil {
		return ""
	}
	v := ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, cle+"=") {
			v = strings.ReplaceAll(strings.TrimPrefix(l, cle+"="), "\r", "")
		}
	}
	return v
}

func (a *assistantEnv) titre(t, sous string) {
	fmt.Fprintf(a.err, "\n%s%s%s\n", a.gras, t, a.fin)
	if sous != "" {
		fmt.Fprintf(a.err, "%s%s%s\n", a.gris, sous, a.fin)
	}
}

// ligne: one line of the input (read's way: false at the end of the input, what was read kept).
func (a *assistantEnv) ligne() (string, bool) {
	s, err := a.lecteur.ReadString('\n')
	if err != nil {
		return s, false
	}
	return strings.TrimSuffix(s, "\n"), true
}

// choisir: the options "value|label"; "__autre__" as the value = free input.
func (a *assistantEnv) choisir(actuelle string, opts ...string) {
	defaut := 0
	for i, o := range opts {
		if strings.SplitN(o, "|", 2)[0] == actuelle {
			defaut = i + 1
		}
	}
	if !a.existe {
		defaut = 0
	}
	if defaut == 0 && actuelle != "" {
		opts = append([]string{actuelle + "|Keep the current value: " + actuelle}, opts...)
		defaut = 1
	}
	if defaut == 0 {
		for i, o := range opts {
			if strings.Contains(o, "(recommended)") {
				defaut = i + 1
				break
			}
		}
	}
	if defaut == 0 {
		defaut = 1
	}
	libelle := func(o string) string {
		if i := strings.Index(o, "|"); i >= 0 {
			return o[i+1:]
		}
		return o
	}
	for i, o := range opts {
		if i+1 == defaut {
			fmt.Fprintf(a.err, "  %s%2d) %s%s  %s← Enter%s\n", a.vert, i+1, libelle(o), a.fin, a.gris, a.fin)
		} else {
			fmt.Fprintf(a.err, "  %2d) %s\n", i+1, libelle(o))
		}
	}
	n := len(opts)
	for {
		fmt.Fprintf(a.err, "Your choice [%d]: ", defaut)
		rep, _ := a.ligne()
		if rep == "" {
			rep = fmt.Sprint(defaut)
		}
		var k int
		if regexp.MustCompile(`^[0-9]+$`).MatchString(rep) {
			fmt.Sscan(rep, &k)
		}
		if k >= 1 && k <= n {
			a.reponse = strings.SplitN(opts[k-1], "|", 2)[0]
			if a.reponse == "__autre__" {
				fmt.Fprint(a.err, "Enter the value: ")
				v, _ := a.ligne()
				a.reponse = strings.Join(strings.Fields(v), "")
			}
			return
		}
		fmt.Fprintf(a.err, "  %sType a number between 1 and %d.%s\n", a.jaune, n, a.fin)
	}
}

func (a *assistantEnv) demander(question, defaut string) {
	fmt.Fprintf(a.err, "%s [%s]: ", question, defaut)
	rep, _ := a.ligne()
	if rep == "" {
		rep = defaut
	}
	a.reponse = strings.ReplaceAll(rep, "\r", "")
}

func (a *assistantEnv) ouiNon(question, defaut string) bool {
	invite := "[y/N]"
	if defaut == "o" {
		invite = "[Y/n]"
	}
	fmt.Fprintf(a.err, "%s %s: ", question, invite)
	rep, _ := a.ligne()
	if rep == "" {
		rep = defaut
	}
	return strings.ContainsAny(rep[:1], "oOyY")
}

// secret: a line typed without echo (the token) when the input is a terminal.
func (a *assistantEnv) secret() (string, bool) {
	terminal := false
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		terminal = commande("stty", "-echo").Run() == nil
	}
	if terminal {
		c := commande("stty", "-echo")
		c.Stdin = os.Stdin
		_ = c.Run()
		defer func() { c := commande("stty", "echo"); c.Stdin = os.Stdin; _ = c.Run() }()
	}
	return a.ligne()
}

func estTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func lancerConfigurer(args []string) int {
	kit := ""
	if len(args) > 0 {
		kit = args[0]
	} else if exe, err := os.Executable(); err == nil {
		kit = filepath.Dir(exe)
	}
	if err := os.Chdir(kit); err != nil {
		fmt.Fprintln(os.Stderr, "athanor configurer: "+err.Error())
		return 1
	}
	if h, err := os.UserHomeDir(); err == nil {
		os.Setenv("PATH", filepath.Join(h, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH")) // Claude Code (native install)
	}
	a := &assistantEnv{env: ".env", lecteur: bufio.NewReader(os.Stdin), err: os.Stderr}
	if estTerminal(os.Stdout) {
		a.gras, a.vert, a.jaune, a.gris, a.fin = "\033[1m", "\033[32m", "\033[33m", "\033[90m", "\033[0m"
	}
	_, err := os.Stat(a.env)
	a.existe = err == nil

	fmt.Fprintf(a.err, "%s=== Athanor: configuration ===%s\n", a.gras, a.fin)
	if a.existe {
		fmt.Fprintf(a.err, "%sA .env file exists: its values are offered as defaults.%s\n", a.gris, a.fin)
	}

	// 1. Token
	a.titre("1. Login token for your Claude subscription", `Obtained with "claude setup-token" (Claude Code installed on this PC), valid for 1 year.`)
	jeton := a.valeur("CLAUDE_CODE_OAUTH_TOKEN")
	invite := ""
	if jeton != "" {
		debut, fin := jeton, jeton
		if len(debut) > 14 {
			debut = debut[:14]
		}
		if len(fin) > 4 {
			fin = fin[len(fin)-4:]
		}
		fmt.Fprintf(a.err, "Current token: %s...%s\n", debut, fin)
		invite = "Paste a new token, or Enter to keep the current one: "
	} else {
		if aCommande("claude") && estTerminal(os.Stdin) && a.ouiNon(`Run "claude setup-token" now?`, "o") {
			c := commande("claude", "setup-token")
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			_ = c.Run()
		}
		invite = "Paste the token (it is not echoed): "
	}
	for {
		fmt.Fprint(a.err, invite)
		nouveau, ok := a.secret()
		fmt.Fprintln(a.err)
		if !ok { // end of input: keep the current token
			break
		}
		nouveau = strings.Join(strings.Fields(nouveau), "")
		if nouveau != "" {
			jeton = nouveau
		}
		if jeton == "" {
			if a.ouiNon("No token. Continue without one (to add later in .env)?", "n") {
				break
			}
			continue
		}
		if strings.HasPrefix(jeton, "sk-ant-") {
			break
		}
		if a.ouiNon(a.jaune+`This token does not start with "sk-ant-". Keep it anyway?`+a.fin, "n") {
			break
		}
		jeton = ""
	}

	// 2. Main model
	a.titre("2. Project lead model", "It plans, codes and judges the result. The more powerful it is, the faster the quota goes.")
	a.choisir(a.valeur("MODEL"),
		"claude-opus-5-5|Opus 5.5: best quality for a complete project (recommended)",
		"claude-sonnet-5-5|Sonnet 5.5: very good and more frugal, the quota lasts longer",
		"claude-haiku-5-5|Haiku 5.5: fast and very frugal, for simple projects",
		"claude-fable-5-1|Fable 5.1: the most powerful, uses the most quota (Max subscription; plan a fallback model)",
		`opus|Always the latest Opus (alias "opus")`,
		`sonnet|Always the latest Sonnet (alias "sonnet")`,
		"|Your account's default model",
		"__autre__|Another identifier (an older version, for example)")
	modele := a.reponse

	// 2b. Fallback model
	a.titre("2b. Fallback model", "Used automatically when the main model's quota is exhausted, until it reopens, then back to the main one. Essential with Fable.")
	conseil := ""
	switch {
	case strings.Contains(modele, "fable"):
		conseil = "claude-opus-5-5"
	case strings.Contains(modele, "opus"):
		conseil = "claude-sonnet-5-5"
	}
	var opts []string
	for _, o := range []string{"|None: pause until the quota reopens", "claude-opus-5-5|Opus 5.5", "claude-sonnet-5-5|Sonnet 5.5", "claude-haiku-5-5|Haiku 5.5", "__autre__|Another identifier"} {
		if strings.SplitN(o, "|", 2)[0] == conseil {
			o += " (recommended)"
		}
		opts = append(opts, o)
	}
	a.choisir(a.valeur("FALLBACK_MODEL"), opts...)
	secours := a.reponse
	if secours != "" && secours == modele {
		fmt.Fprintf(a.err, "%sThe fallback model is the same as the main one: no fallback.%s\n", a.jaune, a.fin)
		secours = ""
	}

	// 3. Effort
	a.titre("3. Thinking effort", "How much the model thinks before acting. More effort = slower and more quota.")
	a.choisir(a.valeur("EFFORT"),
		"|Normal: Claude Code's default setting (recommended)",
		"low|Low: the fastest, for simple tasks",
		"medium|Medium: faster and more frugal, good quality",
		"high|High: thinks more",
		"xhigh|Extra: thinks a lot, noticeably slower",
		"max|Maximum: the slowest and most expensive")
	effort := a.reponse

	// 4. Subagents
	a.titre("4. Subagent model", "They receive the big chunks of work (a level, a module) from the project lead.")
	a.choisir(a.valeur("SUBAGENT_MODEL"),
		"|The same as the project lead",
		"claude-sonnet-5-5|Sonnet 5.5: saves quota, Opus keeps the lead (recommended)",
		"claude-opus-5-5|Opus 5.5",
		"claude-haiku-5-5|Haiku 5.5: very frugal",
		"__autre__|Another identifier")
	sousAgents := a.reponse

	// 5. Duration
	a.titre("5. Maximum working time", "Safety stop, counted from the project's first start (quota pauses included).")
	a.choisir(a.valeur("MAX_HOURS"), "24|1 day", "72|3 days", "168|1 week (recommended)", "336|2 weeks", "__autre__|Other (in hours)")
	duree := a.reponse
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(duree) {
		duree = "168"
	}

	// 6. RTK
	a.titre("6. Command output compression (RTK)", "Reduces what Claude reads for git, ls, grep, find... No risk for the code.")
	rtkDefaut := "o"
	if a.valeur("RTK") == "0" {
		rtkDefaut = "n"
	}
	rtk := "0"
	if a.ouiNon("Enable RTK?", rtkDefaut) {
		rtk = "1"
	}

	// 7. Time zone
	a.titre("7. Time zone of the logs", "")
	tzDefaut := a.valeur("TZ")
	if tzDefaut == "" {
		tzDefaut = fuseauSysteme()
	}
	if tzDefaut == "" {
		tzDefaut = "UTC"
	}
	a.demander("Time zone", tzDefaut)
	tz := a.reponse

	// 8. Unreal Engine
	a.titre("8. Driving the Unreal editor (MCP)", `Unreal Engine 5.8: enable the "Unreal MCP" and "All Toolsets" plugins, and the server's automatic start (Editor Preferences > Model Context Protocol). On Windows, run unreal-network.cmd once ("mirrored" network or local relay).`)
	wd, _ := os.Getwd()
	os.Setenv("PROJECT", filepath.Join(wd, "logs", "detection"))
	_ = detecterSilencieux(wd)
	ueDefaut := "n"
	if a.valeur("UNREAL_MCP") == "1" {
		ueDefaut = "o"
	}
	if b, _ := os.ReadFile(filepath.Join("system", "pc_tools.env")); regexp.MustCompile(`(?m)^UE_DIR='.+'`).Match(b) {
		fmt.Fprintf(a.err, "%sUnreal detected on this PC.%s\n", a.vert, a.fin)
		if !a.existe {
			ueDefaut = "o"
		}
	}
	unreal := "0"
	if a.ouiNon("Let Claude drive the open Unreal editor?", ueDefaut) {
		unreal = "1"
	}

	// write
	pause, inact, cmax := a.valeur("QUOTA_WAIT_MIN"), a.valeur("IDLE_MAX_MIN"), a.valeur("CYCLE_MAX_MIN")
	geres := " CLAUDE_CODE_OAUTH_TOKEN MODEL FALLBACK_MODEL SUBAGENT_MODEL EFFORT MAX_HOURS RTK TZ UNREAL_MCP QUOTA_WAIT_MIN IDLE_MAX_MIN CYCLE_MAX_MIN "
	sauvegarde := a.env + ".sauvegarde"
	if a.existe {
		if b, err := os.ReadFile(a.env); err == nil {
			_ = os.WriteFile(sauvegarde, b, 0o600)
			_ = os.Chmod(sauvegarde, 0o600)
		}
	}
	defaut := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	w("# Written by athanor configurer on " + time.Now().Format("2006-01-02 15:04") + ". Run it again to change these settings.")
	w("# Never share this file: the token gives access to your Claude subscription.")
	w("")
	w("# Login token (valid for 1 year), obtained with: claude setup-token")
	w("CLAUDE_CODE_OAUTH_TOKEN=" + jeton)
	w("")
	w("# Project lead model (empty = account default)")
	w("MODEL=" + modele)
	w("# Fallback model when the main one's quota is exhausted (empty = pause)")
	w("FALLBACK_MODEL=" + secours)
	w("# Subagent model (empty = the same)")
	w("SUBAGENT_MODEL=" + sousAgents)
	w("# Thinking effort: empty = normal; low, medium, high, xhigh, max")
	w("EFFORT=" + effort)
	w("")
	w("# Safety stop after this number of hours")
	w("MAX_HOURS=" + duree)
	w("# Wait when the quota reset time cannot be read (minutes)")
	w("QUOTA_WAIT_MIN=" + defaut(pause, "20"))
	w("# Watchdog: a cycle is stopped if it stays silent or runs too long (minutes)")
	w("IDLE_MAX_MIN=" + defaut(inact, "30"))
	w("CYCLE_MAX_MIN=" + defaut(cmax, "180"))
	w("")
	w("# Command output compression (1 = on, 0 = off)")
	w("RTK=" + rtk)
	w("# Time zone of the logs")
	w("TZ=" + tz)
	w("# Driving the Unreal editor through its MCP server (1 = yes)")
	w("UNREAL_MCP=" + unreal)
	if bs, err := os.ReadFile(sauvegarde); err == nil && a.existe {
		var autres []string
		for _, l := range strings.Split(strings.ReplaceAll(string(bs), "\r", ""), "\n") {
			if m := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`).FindStringSubmatch(l); m != nil && !strings.Contains(geres, " "+m[1]+" ") {
				autres = append(autres, l)
			}
		}
		if len(autres) > 0 {
			w("")
			w("# Other settings kept")
			for _, l := range autres {
				w(l)
			}
		}
	}
	if err := os.WriteFile(a.env+".tmp", []byte(b.String()), 0o600); err != nil || os.Rename(a.env+".tmp", a.env) != nil {
		fmt.Fprintln(a.err, "Cannot write .env")
		return 1
	}
	_ = os.Chmod(a.env, 0o600)

	a.titre("Configuration saved to .env", "")
	fmt.Fprintf(a.err, "  Model: %s | Fallback: %s | Effort: %s | Subagents: %s\n", defaut(modele, "account default"), defaut(secours, "none"), defaut(effort, "normal"), defaut(sousAgents, "the same"))
	fmt.Fprintf(a.err, "  Max time: %s h | RTK: %s | Time zone: %s | Unreal MCP: %s\n", duree, rtk, tz, unreal)
	if a.existe {
		fmt.Fprintf(a.err, "%s  Previous version kept in .env.sauvegarde%s\n", a.gris, a.fin)
	}
	if jeton == "" {
		fmt.Fprintf(a.err, "%s  Remember to add the token to .env before starting.%s\n", a.jaune, a.fin)
	}

	fmt.Println("Requests are made per project, in the app (Athanor.exe): Requests tab.")
	a.titre("Starting", "")
	if v, _ := os.ReadFile("/proc/version"); strings.Contains(strings.ToLower(string(v)), "microsoft") {
		fmt.Fprintf(a.err, "To start: in the app (%sAthanor.exe%s), %sStart%s on the project.\n", a.gras, a.fin, a.gras, a.fin)
		fmt.Fprint(a.err, "To test the sandbox first (recommended the first time): Setup & health → Test the sandbox.\n")
	} else if a.ouiNon("Start the pilot now, in the background?", "o") {
		exe, _ := os.Executable()
		c := commande(exe, "demarrer", "--background")
		// the tool's own project folder (KIT/projet), never the detection's one set above
		c.Env = []string{}
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "PROJECT=") {
				c.Env = append(c.Env, e)
			}
		}
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				return e.ExitCode()
			}
			return 127
		}
	} else {
		fmt.Fprint(a.err, "To start later: the app (./athanor), or ./athanor demarrer [--background] PROJECT\n")
	}
	return 0
}

// fuseauSysteme: the system's time zone (timedatectl, /etc/timezone, macOS' /etc/localtime link).
func fuseauSysteme() string {
	if out, err := commande("timedatectl", "show", "-p", "Timezone", "--value").Output(); err == nil {
		if s := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); s != "" {
			return s
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0]); s != "" {
			return s
		}
	}
	if l, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 {
			return l[i+len("zoneinfo/"):]
		}
	}
	return ""
}

// detecterSilencieux: athanor detecter without its output.
func detecterSilencieux(kit string) error {
	ancien := os.Stdout
	if n, err := os.Open(os.DevNull); err == nil {
		os.Stdout = n
		defer func() { os.Stdout = ancien; n.Close() }()
	}
	return detecter(kit)
}
