//go:build !windows

package main

import (
	"os"
	"syscall"
)

// verrouillerFichier: an exclusive lock on the file chemin (created when missing), held until the
// returned function is called; waits for the other holders (pilots writing the same file).
func verrouillerFichier(chemin string) (func(), error) {
	f, err := os.OpenFile(chemin, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
