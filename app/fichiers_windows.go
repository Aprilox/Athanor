//go:build windows

package main

import (
	"errors"
	"os"
	"strings"

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

// cheminFinal: the path the system really opens (junctions, links and 8.3 short names resolved), with its drive
// letter, or \\server\share for a network one; an error if it does not exist. (filepath.EvalSymlinks no longer
// follows junctions and keeps the short names.)
func cheminFinal(chemin string) (string, error) {
	p, err := windows.UTF16PtrFromString(chemin)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]uint16, n+1)
	}
	s := windows.UTF16ToString(buf)
	if strings.HasPrefix(s, `\\?\UNC\`) {
		return `\\` + s[len(`\\?\UNC\`):], nil
	}
	return strings.TrimPrefix(s, `\\?\`), nil
}

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
