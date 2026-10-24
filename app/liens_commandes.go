package main

// Claude's commands (adb, blender, tool, unreal-editor…, athanor) are links to the kit's executable in one
// folder of the kit, on Claude's PATH (passerelle_appel.go says what each does). liens_commandes_unix.go
// and liens_commandes_windows.go make them; the pilot does it as it starts.

import (
	"bytes"
	"os"
)

// nomsCommandesClaude: every command of that folder (the gateways and athanor).
func nomsCommandesClaude() []string { return append(append([]string{}, nomsPasserelles...), "athanor") }

// ancienScript: a script of an older kit (system/bin/NAME was a bash script): replaced by its link.
func ancienScript(chemin string) bool {
	st, err := os.Lstat(chemin)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64*1024 {
		return false
	}
	b, err := os.ReadFile(chemin)
	return err == nil && (bytes.HasPrefix(b, []byte("#!")) || bytes.HasPrefix(b, []byte("# shellcheck")))
}
