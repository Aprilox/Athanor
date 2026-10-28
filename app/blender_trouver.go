package main

// Blender on native Linux (not WSL, which drives the Windows Blender): the program found on this computer,
// never a path written in: BLENDER_EXE (pc_tools.env, athanor detecter), else on the PATH (not the kit's own
// "blender" gateway), the Snap (/snap/bin/blender), the Flatpak org.blender.Blender (its exported launcher,
// system-wide or the user's). passerelle blender (blender_natif.go) runs it. (Not blender_linux.go: that name builds for Linux only.)

import (
	"os"
	"path/filepath"
)

// trouverBlenderLinux: Blender's launcher among these PATH folders, then the Snap and the Flatpak; "" when none.
func trouverBlenderLinux(chemins []string, home string) string {
	var moi os.FileInfo
	if e, err := os.Executable(); err == nil {
		moi, _ = os.Stat(e)
	}
	executable := func(f string) bool {
		st, err := os.Stat(f)
		return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 && (moi == nil || !os.SameFile(st, moi))
	}
	for _, d := range chemins {
		if d == "" {
			continue
		}
		if f := filepath.Join(d, "blender"); executable(f) {
			return f
		}
	}
	for _, f := range []string{"/snap/bin/blender", "/var/lib/flatpak/exports/bin/org.blender.Blender",
		filepath.Join(home, ".local/share/flatpak/exports/bin/org.blender.Blender")} {
		if home == "" && f[0] != '/' {
			continue
		}
		if executable(f) {
			return f
		}
	}
	return ""
}
