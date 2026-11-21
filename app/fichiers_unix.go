//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// ouvrirSansLien: opens chemin never through a link and never blocking on a pipe put in its place.
func ouvrirSansLien(chemin string, mode int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(chemin, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, perm)
}

// verrouillerFd: an exclusive lock on f, until it is closed (flock, as the Python scripts take it).
func verrouillerFd(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

func runtimeWindows() bool { return false }

// restreindreFichier: nothing to do, the file is created 0600.
func restreindreFichier(string) error { return nil }

// cheminFinal: the path the system really opens, links resolved; an error if it does not exist.
func cheminFinal(chemin string) (string, error) { return filepath.EvalSymlinks(chemin) }

// cheminLong: Windows' long names in place of the 8.3 short ones; nothing to do elsewhere.
func cheminLong(chemin string) string { return chemin }
