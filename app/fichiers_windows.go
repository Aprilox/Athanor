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

// restreindreFichier: the file readable and writable by the current account only (B20): its DACL no longer
// inherits the folder's (a project under C:\ gives "Authenticated Users" Modify); Go ignores 0600 on NTFS.
func restreindreFichier(chemin string) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(u.User.Sid),
		},
	}}, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(chemin, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
