package main

// Migration from the previous name of the tool (Claude Autopilote → Athanor).
// Everything here only renames what an older version left behind, never deletes user data,
// and is a no-op on a fresh installation.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ancienConfig  = "autopilote-config.json"
	ancienAtelier = ".autopilote"
)

// migrerConfig renames autopilote-config.json (next to the exe) to athanor-config.json.
func migrerConfig() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	ancien := filepath.Join(filepath.Dir(exe), ancienConfig)
	if _, err := os.Stat(cheminConfig()); err == nil {
		return
	}
	if _, err := os.Stat(ancien); err == nil {
		_ = os.Rename(ancien, cheminConfig())
	}
}

// MigrerAtelier renames the project's old .autopilote/ workshop to .athanor/, unless a pilot
// of the old version is still running in it (its heartbeat is fresh) or .athanor/ already exists.
// The pilot does the same at startup (and repairs git worktrees, which needs git).
func (p Projet) MigrerAtelier() {
	ancien := filepath.Join(p.Dossier, ancienAtelier)
	st, err := os.Lstat(ancien)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return
	}
	if fi, err := os.Stat(filepath.Join(ancien, "pilot.pid")); err == nil && time.Since(fi.ModTime()) < 90*time.Second {
		return // an old pilot is alive: it will be migrated at its next start
	}
	if st, err := os.Lstat(p.Atelier()); err == nil {
		// a near-empty .athanor/ (a STOP request made before the migration) is folded into the old one
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || existe(p.Demande()) || existe(p.Plan()) {
			return
		}
		entrees, _ := os.ReadDir(p.Atelier())
		for _, e := range entrees {
			if e.Type()&os.ModeSymlink == 0 && !existe(filepath.Join(ancien, e.Name())) {
				_ = os.Rename(filepath.Join(p.Atelier(), e.Name()), filepath.Join(ancien, e.Name()))
			}
		}
		if os.RemoveAll(p.Atelier()) != nil {
			return
		}
	}
	if os.Rename(ancien, p.Atelier()) != nil {
		return
	}
	// .git/info/exclude: the workshop stays out of git under its new name
	exclude := filepath.Join(p.Dossier, ".git", "info", "exclude")
	if fi, err := os.Lstat(exclude); err == nil && fi.Mode().IsRegular() {
		if b, err := os.ReadFile(exclude); err == nil {
			lignes := strings.Split(string(b), "\n")
			for i, l := range lignes {
				if strings.TrimSpace(strings.TrimSuffix(l, "\r")) == ancienAtelier+"/" {
					lignes[i] = ".athanor/"
				}
			}
			_ = os.WriteFile(exclude, []byte(strings.Join(lignes, "\n")), fi.Mode().Perm())
		}
	}
}

func existe(chemin string) bool { _, err := os.Lstat(chemin); return err == nil }
