//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// verrouFichierGH: the file opened and locked (flock, as github.sh took it); nil after attente.
func verrouFichierGH(chemin string, attente time.Duration) *os.File {
	f, err := os.OpenFile(chemin, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil
	}
	fin := time.Now().Add(attente)
	for {
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return f
		}
		if time.Now().After(fin) {
			f.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// cleProjetGH: the project's path as github.sh named it (its settings folder comes from it).
func cleProjetGH(p string) string { return p }

// programmeGH: the ssh or ssh-keygen to use.
func programmeGH(nom string) string { return nom }

func sshDeGitGH(string) bool { return true }

func presentGH(nom string) error {
	_, err := exec.LookPath(nom)
	return err
}
