//go:build !windows

package main

// The desktop trash on Linux (freedesktop.org trash: ~/.local/share/Trash): a deleted project can be
// restored from the file manager.

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func mettreALaCorbeille(chemin string) error {
	donnees := os.Getenv("XDG_DATA_HOME")
	if donnees == "" {
		maison, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		donnees = filepath.Join(maison, ".local", "share")
	}
	fichiers, infos := filepath.Join(donnees, "Trash", "files"), filepath.Join(donnees, "Trash", "info")
	if err := os.MkdirAll(fichiers, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(infos, 0o700); err != nil {
		return err
	}
	base := filepath.Base(chemin)
	nom := base
	for i := 2; ; i++ {
		_, e1 := os.Lstat(filepath.Join(fichiers, nom))
		_, e2 := os.Lstat(filepath.Join(infos, nom+".trashinfo"))
		if os.IsNotExist(e1) && os.IsNotExist(e2) {
			break
		}
		nom = fmt.Sprintf("%s.%d", base, i)
	}
	abs, _ := filepath.Abs(chemin)
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", (&url.URL{Path: abs}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
	if err := os.WriteFile(filepath.Join(infos, nom+".trashinfo"), []byte(info), 0o600); err != nil {
		return err
	}
	if err := os.Rename(chemin, filepath.Join(fichiers, nom)); err != nil {
		os.Remove(filepath.Join(infos, nom+".trashinfo"))
		return errors.New("the folder is on another disk than the trash: delete it yourself")
	}
	return nil
}
