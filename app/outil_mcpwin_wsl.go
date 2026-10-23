//go:build linux

package main

// athanor mcp-windows [--env NAME VALUE]... -- PROGRAM [ARGS...]: system/windows-mcp.sh in Go (WSL): a local
// MCP server that is a Windows program (Roblox Studio's, a .exe, a .bat…), started for Claude Code through
// WSL interop; its MCP messages go through its standard input and output. It runs on Windows, with the
// user's rights and outside any cage: only the servers the user added in the app ("Windows program") get
// here. The server's variables reach the Windows program only (WSLENV). When Claude's session ends (or
// the pilot stops), the program and everything it started are stopped, even if this process is killed
// outright (a watchdog in a session of its own).

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	reVarWindows    = regexp.MustCompile(`^([^%]*)%([A-Za-z_][A-Za-z0-9_()]{0,63})%(.*)$`)
	reNomVarServeur = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	reCheminWin     = regexp.MustCompile(`^([A-Za-z]:\\|\\\\)`)
	reNombre        = regexp.MustCompile(`^[0-9]+$`)
)

func echecMCPWindows(m string) int {
	fmt.Fprintln(os.Stderr, "windows-mcp: "+m)
	return 1
}

// commandeWin: a Windows command of this program's own, never reading the MCP input (interop would hand it over).
func commandeWin(prog string, args ...string) *exec.Cmd {
	c := exec.Command(prog, args...)
	c.Env = append(os.Environ(), "WSLENV=")
	return c
}

// arreterArbreMCP: the Windows tree stopped (taskkill /T) from the PID Athanor.exe writes, the launcher killed.
func arreterArbreMCP(pidF, sys32 string, p int) {
	w := ""
	for i := 0; i < 10; i++ {
		if b, err := os.ReadFile(pidF); err == nil && len(strings.TrimSpace(string(b))) > 0 {
			w = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if reNombre.MatchString(w) {
		_ = commandeWin(sys32+"/taskkill.exe", "/T", "/F", "/PID", w).Run()
	}
	if p > 0 {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	os.Remove(pidF)
}

// lancerGardeMCPWindows: athanor mcp-windows-garde PIDFILE SYS32 LAUNCHER PARENT: while the launcher runs,
// if the parent (this server's mcp-windows) disappears, the program is stopped.
func lancerGardeMCPWindows(args []string) int {
	if len(args) < 4 {
		return 2
	}
	p, _ := strconv.Atoi(args[2])
	parent, _ := strconv.Atoi(args[3])
	vivant := func(n int) bool { return n > 0 && syscall.Kill(n, 0) == nil }
	for vivant(p) {
		if !vivant(parent) {
			if vivant(p) {
				arreterArbreMCP(args[0], args[1], p)
			}
			return 0
		}
		time.Sleep(2 * time.Second)
	}
	return 0
}

func lancerMCPWindows(args []string) int {
	// System32 (cmd.exe, taskkill.exe): the Windows entry of the inherited PATH (Windows is not always on C:)
	sys32 := "/mnt/c/Windows/System32"
	var cheminsWindows []string
	for _, d := range strings.Split(os.Getenv("PATH"), ":") {
		if regexp.MustCompile(`^/mnt/[a-z]/`).MatchString(d) {
			cheminsWindows = append(cheminsWindows, d)
			if regexp.MustCompile(`^/mnt/./windows/system32$`).MatchString(strings.ToLower(d)) && fichierP(d+"/cmd.exe") {
				sys32 = d
			}
		}
	}
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	for _, v := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "GCONV_PATH", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		os.Unsetenv(v)
	}
	kit := os.Getenv("KIT")
	if !existeP("/proc/sys/fs/binfmt_misc/WSLInterop") && !existeP("/proc/sys/fs/binfmt_misc/WSLInterop-late") && os.Getenv("WSL_INTEROP") == "" {
		return echecMCPWindows("Windows programs cannot be started from here (not on Windows, or WSL interoperability is off).")
	}
	// the server's variables: given to the Windows program alone (at launch), never set here
	var envServeur, noms []string
	for len(args) > 0 && args[0] != "--" {
		if args[0] != "--env" {
			return echecMCPWindows("unknown option " + args[0])
		}
		if len(args) < 3 {
			return echecMCPWindows("--env NAME VALUE")
		}
		if !reNomVarServeur.MatchString(args[1]) {
			return echecMCPWindows("invalid variable")
		}
		// names that would change how WSL interop itself starts the program
		n := strings.ToUpper(args[1])
		switch {
		case n == "PATH" || n == "HOME" || n == "SHELL" || n == "ENV" || n == "BASH_ENV" || n == "IFS" || n == "WSLENV" || n == "GCONV_PATH" ||
			n == "LOCPATH" || n == "LANG" || n == "LANGUAGE" || n == "POSIXLY_CORRECT" || n == "TMPDIR",
			strings.HasPrefix(n, "LD_"), strings.HasPrefix(n, "BASH_"), strings.HasPrefix(n, "WSL_"), strings.HasPrefix(n, "LC_"), strings.HasPrefix(n, "_WM_"):
			return echecMCPWindows("variable " + args[1] + " cannot be set for a Windows program")
		}
		envServeur = append(envServeur, args[1]+"="+args[2])
		noms = append(noms, args[1])
		args = args[3:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) == 0 {
		return echecMCPWindows("no program")
	}
	wslenv := strings.Join(noms, ":")
	// a Windows program started from a Linux folder would complain about the UNC path (\\wsl.localhost\…)
	if os.Chdir(sys32+"/../..") != nil && os.Chdir("/mnt/c") != nil {
		_ = os.Chdir("/")
	}
	// %NAME% -> its Windows value, read raw from "set NAME" (no cmd parsing of the value); unknown: as written
	valeurs := map[string]string{}
	developper := func(s string) string {
		out := ""
		for {
			m := reVarWindows.FindStringSubmatch(s)
			if m == nil {
				break
			}
			out += m[1]
			v := m[2]
			s = m[3]
			val, vu := valeurs[v]
			if !vu {
				c := commandeWin(sys32+"/cmd.exe", "/d", "/c", "set "+v)
				b, _ := c.Output()
				for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
					if i := strings.Index(l, "="); i > 0 && strings.EqualFold(l[:i], v) {
						val = l[i+1:]
						break
					}
				}
				if val == "" {
					val = "%" + v + "%"
				}
				valeurs[v] = val
			}
			out += val
		}
		return out + s
	}
	prog := developper(args[0])
	var reste []string
	for _, a := range args[1:] {
		reste = append(reste, developper(a))
	}
	if regexp.MustCompile(`^[A-Za-z]:/`).MatchString(prog) {
		prog = strings.ReplaceAll(prog, "/", `\`) // C:/Program Files/… as Claude Desktop configs write it
	}
	// a script runs through its interpreter (the name is then looked for by Windows itself)
	switch b := strings.ToLower(prog); {
	case strings.HasSuffix(b, ".bat") || strings.HasSuffix(b, ".cmd"):
		reste = append([]string{"/c", prog}, reste...)
		prog = "cmd.exe"
	case strings.HasSuffix(b, ".ps1"):
		reste = append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", prog}, reste...)
		prog = "powershell.exe"
	}
	// the program as WSL sees it (progL, always a Windows file: never a Linux program, which would run
	// uncaged) and as Windows starts it (progW: that same file whenever it is known)
	var progL, progW string
	switch {
	case regexp.MustCompile(`^/mnt/[a-z]/`).MatchString(prog):
		if !fichierP(prog) {
			return echecMCPWindows("program not found: " + prog)
		}
		progL, progW = prog, wslpathW(prog)
	case strings.HasPrefix(prog, "/"):
		return echecMCPWindows("not a Windows program: " + prog)
	case reCheminWin.MatchString(prog):
		progW = prog
		out, err := exec.Command("wslpath", "-u", prog).Output()
		progL = strings.TrimRight(string(out), "\n")
		if err != nil || !fichierP(progL) {
			return echecMCPWindows("program not found: " + prog)
		}
	case strings.ContainsAny(prog, `/\`):
		return echecMCPWindows(`give the program's full path (C:\…) or a name Windows finds (cmd.exe): ` + prog)
	default:
		for _, d := range append([]string{sys32, sys32 + "/WindowsPowerShell/v1.0"}, cheminsWindows...) {
			for _, c := range []string{prog, prog + ".exe"} {
				if progL == "" && fichierP(d+"/"+c) {
					progL = d + "/" + c
				}
			}
		}
		// known: Windows runs that very file; unknown to WSL: Windows looks for it
		progW = prog
		if progL != "" {
			progW = wslpathW(progL)
		}
	}
	// through Athanor.exe: on Claude's desktop when that option is on, and its whole process tree can be
	// stopped (WSL only stops the Linux side of interop)
	logs := kit + "/logs"
	_ = os.MkdirAll(logs, 0o777)
	// pid files of launchers that died with the computer: removed once their Windows process is gone
	if l, _ := filepath.Glob(logs + "/.mcp-*"); len(l) > 0 {
		for _, f := range l {
			st, err := os.Stat(f)
			if err != nil || !st.Mode().IsRegular() || time.Since(st.ModTime()) <= 1440*time.Minute {
				continue
			}
			w, _ := os.ReadFile(f)
			n := strings.TrimSpace(string(w))
			out, _ := commandeWin(sys32+"/tasklist.exe", "/FI", "PID eq "+n, "/NH").Output()
			if !reNombre.MatchString(n) || !strings.Contains(string(out), " "+n+" ") {
				os.Remove(f)
			}
		}
	}
	envProg := append(os.Environ(), envServeur...)
	envProg = append(envProg, "WSLENV="+wslenv)
	exe := kit + "/Athanor.exe"
	if st, err := os.Stat(exe); err == nil && st.Mode()&0o111 != 0 {
		if f, err := os.CreateTemp(logs, ".mcp-"); err == nil {
			f.Close()
			pidF := f.Name()
			ici := []string{"--ici"}
			if existeP(logs + "/bureau-isole") {
				ici = nil
			}
			a := append([]string{"--isole"}, ici...)
			a = append(a, "--pid", wslpathW(pidF), "--", progW)
			c := exec.Command(exe, append(a, reste...)...)
			c.Env, c.Stdin, c.Stdout, c.Stderr = envProg, os.Stdin, os.Stdout, os.Stderr
			if err := c.Start(); err != nil {
				os.Remove(pidF)
				return echecMCPWindows(err.Error())
			}
			// watchdog, in a session of its own (a KILL of this process or of its group never reaches it)
			if moi, err := os.Executable(); err == nil {
				g := exec.Command(moi, "mcp-windows-garde", pidF, sys32, strconv.Itoa(c.Process.Pid), strconv.Itoa(os.Getpid()))
				g.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				if g.Start() == nil {
					_ = g.Process.Release()
				}
			}
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			fini := make(chan error, 1)
			go func() { fini <- c.Wait() }()
			select {
			case err := <-fini:
				os.Remove(pidF)
				return codeCommande(err, c)
			case <-sig:
				arreterArbreMCP(pidF, sys32, c.Process.Pid)
				return 143
			}
		}
	}
	if progL == "" {
		return echecMCPWindows("program not found: " + prog)
	}
	return executerP(progL, reste, envProg)
}
