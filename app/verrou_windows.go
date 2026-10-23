//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// verrouillerFichier: an exclusive lock on the file chemin (created when missing), held until the
// returned function is called; waits for the other holders (pilots writing the same file).
func verrouillerFichier(chemin string) (func(), error) {
	if st, err := os.Lstat(chemin); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrPermission
	}
	f, err := os.OpenFile(chemin, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	var ol windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol); f.Close() }, nil
}
