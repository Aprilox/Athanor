//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// ouvrirSansLien: native Windows has no O_NOFOLLOW: a link is refused before opening.
func ouvrirSansLien(chemin string, mode int, perm os.FileMode) (*os.File, error) {
	if st, err := os.Lstat(chemin); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New(chemin + " is a link")
	}
	return os.OpenFile(chemin, mode, perm)
}

// verrouillerFd: an exclusive lock on the first byte of f (what msvcrt.locking takes), until it is closed.
func verrouillerFd(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

func runtimeWindows() bool { return true }
