//go:build !linux

package main

// Native Windows and macOS: no *at system calls here, the links are checked by path (the Linux version
// opens every folder without following a link).

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func remplacerFichier(cible string, perm os.FileMode, contenu []byte) error {
	tmp := fmt.Sprintf("%s.%d", cible, os.Getpid())
	if err := os.WriteFile(tmp, contenu, perm); err != nil {
		return err
	}
	return os.Rename(tmp, cible)
}

func fichierAtelier(projet, nom string, effacer bool, r io.Reader) error {
	d := filepath.Join(projet, ".athanor")
	if st, err := os.Stat(d); estLien(d) || err != nil || !st.IsDir() {
		return errors.New("no workshop")
	}
	cible := filepath.Join(d, nom)
	if estLien(cible) {
		_ = os.Remove(cible)
	}
	if effacer {
		if st, err := os.Lstat(cible); err == nil && st.IsDir() {
			return errors.New("a folder: not removed") // as unlink: never a folder
		}
		if err := os.Remove(cible); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	contenu, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	tmp := filepath.Join(d, fmt.Sprintf(".%s.%d", nom, os.Getpid()))
	if err := os.WriteFile(tmp, contenu, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cible)
}

func crochetCommit(projet string, actif bool) error {
	g := filepath.Join(projet, ".git")
	h := filepath.Join(g, "hooks")
	// as on Linux: only a real .git folder (never a link, a gitfile or nothing), a hooks folder that is
	// one or that is missing (created then), never a link
	if st, err := os.Lstat(g); err != nil || !st.IsDir() {
		return nil
	}
	if st, err := os.Lstat(h); err == nil && !st.IsDir() {
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	f := filepath.Join(h, "commit-msg")
	if estLien(f) {
		return nil
	}
	var actuel []byte
	if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
		o, err := os.Open(f)
		if err != nil {
			return err
		}
		b := make([]byte, 4096)
		n, _ := o.Read(b)
		o.Close()
		actuel = b[:n]
	}
	if actuel != nil && !crochetAthanor(actuel) {
		return nil
	}
	if actif {
		if err := os.Mkdir(h, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		return remplacerFichier(f, 0o755, texteCrochet)
	}
	if actuel != nil {
		return os.Remove(f)
	}
	return nil
}
