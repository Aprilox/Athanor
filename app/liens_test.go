package main

import (
	"os"
	"testing"
)

// lienEssai: a symbolic link for a test, or false when this system cannot make one. Windows without the
// right refuses it; Wine (the Windows tests run on Linux) says yes and makes nothing. The checks about
// links are then skipped, rather than failing on a link that never existed.
func lienEssai(t *testing.T, cible, lien string) bool {
	t.Helper()
	if err := os.Symlink(cible, lien); err != nil {
		t.Logf("no symbolic link on this system (%v): the checks about links are skipped", err)
		return false
	}
	if st, err := os.Lstat(lien); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Logf("the symbolic link %s was not made (%v): the checks about links are skipped", lien, err)
		return false
	}
	return true
}
