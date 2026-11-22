package main

// logs/ keeps a folder per project ever piloted (logs/pilotes/<id>) and per team tunnel ever opened
// (logs/equipe-directe/tunnel-<port>): nothing removed them, so they piled up for ever.

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ageJournauxOublies: a folder with nothing written for this long goes (a project piloted outside the list,
// a mission's, an old team tunnel), unless it belongs to a project of the list.
const ageJournauxOublies = 30 * 24 * time.Hour

// nettoyerJournaux: the pilot folders of the projects no longer listed and the team tunnels' folders,
// once nothing was written in them for ageJournauxOublies.
func nettoyerJournaux(kit string, projets []Projet) {
	garder := map[string]bool{}
	for _, p := range projets {
		garder[filepath.Base(priveDePilote(kit, p.Dossier, estWindows))] = true
	}
	seuil := time.Now().Add(-ageJournauxOublies)
	pilotes, _ := filepath.Glob(filepath.Join(kit, "logs", "pilotes", "*"))
	tunnels, _ := filepath.Glob(filepath.Join(kit, "logs", "equipe-directe", "tunnel-*"))
	for _, d := range append(pilotes, tunnels...) {
		if !garder[filepath.Base(d)] && estDossier(d) && !estLien(d) && !modifieDepuis(d, seuil) {
			_ = os.RemoveAll(d)
		}
	}
}

// modifieDepuis: something in d (d included) was written after t.
func modifieDepuis(d string, t time.Time) bool {
	recent := false
	_ = filepath.WalkDir(d, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if i, err := e.Info(); err == nil && i.ModTime().After(t) {
			recent = true
			return filepath.SkipAll
		}
		return nil
	})
	return recent
}
