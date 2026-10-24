//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// versWindowsP: a WSL path as Windows names it (wslpath -w).
func versWindowsP(p string) string { return wslpathW(p) }

// prefixeP: chemin starts with base (paths as WSL writes them).
func prefixeP(chemin, base string) bool { return strings.HasPrefix(chemin, base) }

func filepathToSlashP(p string) string { return p }

// montageDockerP: the project folder for docker run -v.
func montageDockerP(p string) string { return p }

// dansC: a Windows command run from /mnt/c (a WSL folder as the current one makes Windows warn).
func dansC(prog string, args ...string) *exec.Cmd {
	c := commande(prog, args...)
	if st, err := os.Stat("/mnt/c"); err == nil && st.IsDir() {
		c.Dir = "/mnt/c"
	}
	c.Env = append(os.Environ(), "MSYS2_ARG_CONV_EXCL=*")
	return c
}

func existeArgP(p string) bool { return existeP(p) }

func dirArgP(p string) string { return filepath.Dir(p) }
