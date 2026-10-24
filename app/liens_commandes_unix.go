//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// dossierCommandesClaude: system/bin (also where RTK is installed).
func dossierCommandesClaude(kit string) string { return filepath.Join(kit, "system", "bin") }

// preparerCommandesClaude: system/bin/NAME -> ../../athanor (relative: the kit can move), the scripts of
// an older kit replaced (and their shared file, _common.sh, removed).
func preparerCommandesClaude(kit string) error {
	d := dossierCommandesClaude(kit)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	if c := filepath.Join(d, "_common.sh"); ancienScript(c) {
		_ = os.Remove(c)
	}
	var premiere error
	for _, n := range nomsCommandesClaude() {
		l := filepath.Join(d, n)
		if cible, err := os.Readlink(l); err == nil && cible == filepath.Join("..", "..", "athanor") {
			continue
		}
		if st, err := os.Lstat(l); err == nil {
			if st.Mode()&os.ModeSymlink == 0 && !ancienScript(l) {
				continue // a file of the user's: never replaced
			}
			_ = os.Remove(l)
		}
		if err := os.Symlink(filepath.Join("..", "..", "athanor"), l); err != nil && premiere == nil {
			premiere = err
		}
	}
	return premiere
}
