//go:build !windows

package main

import (
	"os"
	"syscall"
)

// ouvrirSansLien: opens chemin never through a link and never blocking on a pipe put in its place.
func ouvrirSansLien(chemin string, mode int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(chemin, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, perm)
}

// verrouillerFd: an exclusive lock on f, until it is closed (flock, as the Python scripts take it).
func verrouillerFd(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

func runtimeWindows() bool { return false }
