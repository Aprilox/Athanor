//go:build windows

package main

import (
	"io"
	"os"
	"path/filepath"
)

// dossierCommandesClaude: system\bin-windows: NAME.exe, hard links of Athanor.exe (Git Bash finds "blender"
// as blender.exe). Not system\bin, where the WSL mode of the same kit keeps its own links (Linux ones).
func dossierCommandesClaude(kit string) string { return filepath.Join(kit, "system", "bin-windows") }

// preparerCommandesClaude: each NAME.exe the very Athanor.exe of the kit (a hard link: no copy; a copy on
// a volume without hard links). An updated Athanor.exe is a new file: its links are made again.
func preparerCommandesClaude(kit string) error {
	d := dossierCommandesClaude(kit)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(kit, "Athanor.exe")
	ref, err := os.Stat(exe)
	if err != nil {
		return err
	}
	var premiere error
	for _, n := range nomsCommandesClaude() {
		l := filepath.Join(d, n+".exe")
		if st, err := os.Stat(l); err == nil {
			if os.SameFile(st, ref) {
				continue
			}
			if os.Remove(l) != nil {
				continue // in use (a command running): the next start makes it again
			}
		}
		if err := os.Link(exe, l); err != nil {
			if err := copierFichierExe(exe, l); err != nil && premiere == nil {
				premiere = err
			}
		}
	}
	return premiere
}

func copierFichierExe(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	t, err := os.CreateTemp(filepath.Dir(dst), ".copie-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(t, s); err != nil {
		t.Close()
		os.Remove(t.Name())
		return err
	}
	t.Close()
	return os.Rename(t.Name(), dst)
}
