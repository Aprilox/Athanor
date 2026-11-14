//go:build windows

package main

import (
	"path/filepath"
	"strings"
)

// arreterProgrammesSous: ends, with their children, the programs whose executable lies under dossier
// (a test kit's Athanor.exe and its command links); never a program from anywhere else.
func arreterProgrammesSous(dossier string) {
	pref := strings.ToLower(filepath.Clean(dossier)) + `\`
	for _, p := range processusMachine() {
		if strings.HasPrefix(strings.ToLower(p.exe), pref) {
			terminerProcessus(int(p.pid))
		}
	}
}
