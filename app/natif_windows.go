//go:build windows

package main

// Native Windows mode: the pilot is Athanor.exe itself instead of WSL's athanor, and Claude Code is the
// Windows program (claude.exe), whose Bash tool is Git for Windows' Git Bash. No WSL needed, Windows
// programs and drives are reached directly, but Claude Code has no sandbox there: the user chooses it in
// Options, which says so.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var modeNatif atomic.Bool

// bashNatif: the bash.exe of Git for Windows, "" when it is not installed.
func bashNatif() string {
	existe := func(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && existe(p) {
		return p
	}
	candidats := []string{}
	for _, v := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if d := os.Getenv(v); d != "" {
			candidats = append(candidats, filepath.Join(d, "Git", "bin", "bash.exe"))
		}
	}
	if d := os.Getenv("LocalAppData"); d != "" {
		candidats = append(candidats, filepath.Join(d, "Programs", "Git", "bin", "bash.exe"))
	}
	// git.exe in the PATH: ...\Git\cmd\git.exe or ...\Git\bin\git.exe, bash.exe in ...\Git\bin
	if g, err := exec.LookPath("git.exe"); err == nil {
		racine := filepath.Dir(filepath.Dir(g))
		candidats = append(candidats, filepath.Join(racine, "bin", "bash.exe"))
	}
	for _, c := range candidats {
		if existe(c) {
			return c
		}
	}
	return ""
}

// cheminPosix: C:\Users\x -> /c/Users/x, as Git Bash names it.
func cheminPosix(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

// cheminFrais: the PATH as Windows has it now (system, then user, from the registry), followed by the
// application's own: a program installed while Athanor runs (winget, Install everything) is found at
// once, without restarting Athanor.
func cheminFrais() string {
	vus := map[string]bool{}
	var parties []string
	ajouter := func(liste string) {
		for _, d := range strings.Split(liste, ";") {
			d = strings.TrimSpace(d)
			if d == "" || vus[strings.ToLower(d)] {
				continue
			}
			vus[strings.ToLower(d)] = true
			parties = append(parties, d)
		}
	}
	lire := func(racine registry.Key, cle string) string {
		k, err := registry.OpenKey(racine, cle, registry.QUERY_VALUE)
		if err != nil {
			return ""
		}
		defer k.Close()
		v, _, err := k.GetStringValue("Path")
		if err != nil {
			return ""
		}
		if e, err := registry.ExpandString(v); err == nil {
			return e
		}
		return v
	}
	ajouter(lire(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`))
	ajouter(lire(registry.CURRENT_USER, `Environment`))
	ajouter(os.Getenv("PATH"))
	return strings.Join(parties, ";")
}

var (
	pMessageBoxW = user32.NewProc("MessageBoxW")
)

// questionOuiNon: a Windows dialog, true for "Yes".
func questionOuiNon(titre, texte string) bool {
	t, _ := syscall.UTF16PtrFromString(titre)
	m, _ := syscall.UTF16PtrFromString(texte)
	const mbYesNo, mbIconWarning, mbTopmost, idYes = 0x4, 0x30, 0x40000, 6
	r, _, _ := pMessageBoxW.Call(fenetreHWND.Load(), uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbYesNo|mbIconWarning|mbTopmost)
	return r == idYes
}
