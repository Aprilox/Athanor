//go:build windows

package main

import (
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// versWindowsP: a path as Windows names it: C:\x for C:/x, and Git Bash's /c/x (what Claude's shell may give).
func versWindowsP(p string) string {
	if m := reCheminGitBash.FindStringSubmatch(p); m != nil {
		return strings.ToUpper(m[1]) + ":" + filepath.FromSlash(firstNonEmpty(m[2], "/"))
	}
	return filepath.FromSlash(p)
}

// prefixeP: chemin starts with base, Windows' way: either slash, any case.
func prefixeP(chemin, base string) bool {
	n := func(s string) string { return strings.ToLower(filepath.ToSlash(versWindowsP(s))) }
	b := n(base)
	if strings.HasSuffix(base, "/") || strings.HasSuffix(base, `\`) {
		b = strings.TrimSuffix(b, "/") + "/"
	}
	return strings.HasPrefix(n(chemin), b)
}

func filepathToSlashP(p string) string { return filepath.ToSlash(p) }

// montageDockerP: docker.exe wants C:/... for a bind mount.
func montageDockerP(p string) string { return filepath.ToSlash(versWindowsP(p)) }

// dansC: a Windows command (native Windows: simply run).
func dansC(prog string, args ...string) *exec.Cmd { return commande(prog, args...) }

// existeArgP: an argument that names something on the PC (Git Bash's /c/x form included).
func existeArgP(p string) bool { return existeP(versWindowsP(p)) }

func dirArgP(p string) string { return path.Dir(filepath.ToSlash(p)) }
