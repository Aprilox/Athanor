//go:build !linux

package main

// Native Windows (and the Mac for now): no *at system calls here, the links are checked by path, as the
// Python of github.sh did on Windows.

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
	h := filepath.Join(projet, ".git", "hooks")
	if estLien(filepath.Join(projet, ".git")) || estLien(h) {
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
		if err := os.MkdirAll(h, 0o755); err != nil {
			return err
		}
		return remplacerFichier(f, 0o755, texteCrochet)
	}
	if actuel != nil {
		return os.Remove(f)
	}
	return nil
}
