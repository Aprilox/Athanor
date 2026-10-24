//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// verrouFichierGH: the file opened and locked (LockFileEx); nil after attente. The lock goes with the handle.
func verrouFichierGH(chemin string, attente time.Duration) *os.File {
	f, err := os.OpenFile(chemin, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil
	}
	fin := time.Now().Add(attente)
	for {
		var ol windows.Overlapped
		if windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol) == nil {
			return f
		}
		if time.Now().After(fin) {
			f.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// cleProjetGH: the project's path as github.sh saw it under Git Bash (/c/Users/x/site): the settings of
// the projects linked before keep their folder.
func cleProjetGH(p string) string {
	c := cheminPosix(p)
	if len(c) > 1 {
		c = strings.TrimSuffix(c, "/")
	}
	return c
}

// racineGitGH: Git for Windows' folder (its bash.exe is in bin\ or usr\bin\), "" when it is missing.
func racineGitGH() string {
	b := trouverBashNatifPilote()
	if b == "" {
		return ""
	}
	r := filepath.Dir(filepath.Dir(b))
	if strings.EqualFold(filepath.Base(r), "usr") {
		r = filepath.Dir(r)
	}
	return r
}

// programmeGH: Git's own ssh and ssh-keygen (those github.sh used: they read /dev/null and Git Bash's
// paths), else Windows' OpenSSH.
func programmeGH(nom string) string {
	if r := racineGitGH(); r != "" {
		p := filepath.Join(r, "usr", "bin", nom+".exe")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	if p, err := exec.LookPath(nom); err == nil {
		return p
	}
	return nom
}

func sshDeGitGH(p string) bool {
	r := racineGitGH()
	return r != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(r)+`\`)
}

func presentGH(nom string) error {
	p := programmeGH(nom)
	if filepath.IsAbs(p) {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
	}
	_, err := exec.LookPath(p)
	return err
}
