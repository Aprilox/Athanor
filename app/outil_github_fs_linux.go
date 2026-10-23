//go:build linux

package main

// The workshop file and the commit-msg hook of github.sh, written as its Python did: every step through
// a folder opened without following a link (Claude can write the project, .athanor and .git included).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const ouvrirDossier = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC

// ecrireDansFd: nom written in the folder fd through a new temporary file, then renamed over it.
func ecrireDansFd(fd int, nom, tmp string, perm uint32, contenu []byte) error {
	f, err := syscall.Openat(fd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, perm)
	if err != nil {
		return err
	}
	o := os.NewFile(uintptr(f), tmp)
	if _, err := o.Write(contenu); err != nil {
		o.Close()
		return err
	}
	if err := o.Close(); err != nil {
		return err
	}
	return syscall.Renameat(fd, tmp, fd, nom)
}

// fichierAtelier: PROJECT/.athanor/NAME written (stdin) or deleted, never through a link.
func fichierAtelier(projet, nom string, effacer bool, r io.Reader) error {
	fd, err := syscall.Open(filepath.Join(projet, ".athanor"), ouvrirDossier, 0)
	if err != nil {
		return fmt.Errorf("no workshop: %w", err)
	}
	defer syscall.Close(fd)
	if effacer {
		if err := syscall.Unlinkat(fd, nom); err != nil && !errors.Is(err, syscall.ENOENT) {
			return err
		}
		return nil
	}
	contenu, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return ecrireDansFd(fd, nom, fmt.Sprintf(".%s.%d", nom, os.Getpid()), 0o644, contenu)
}

// crochetCommit: the project's commit-msg hook removing Claude's co-author lines, on (actif) or off.
// A hook of the user's (without Athanor's mark) and a link in the way are left alone.
func crochetCommit(projet string, actif bool) error {
	g, err := syscall.Open(filepath.Join(projet, ".git"), ouvrirDossier, 0)
	if err != nil {
		return nil
	}
	defer syscall.Close(g)
	h, err := syscall.Openat(g, "hooks", ouvrirDossier, 0)
	if errors.Is(err, syscall.ENOENT) {
		if !actif {
			return nil
		}
		if err := syscall.Mkdirat(g, "hooks", 0o755); err != nil {
			return err
		}
		if h, err = syscall.Openat(g, "hooks", ouvrirDossier, 0); err != nil {
			return err
		}
	} else if err != nil {
		return nil // a link in its place: left alone
	}
	defer syscall.Close(h)
	var actuel []byte
	f, err := syscall.Openat(h, "commit-msg", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err == nil {
		o := os.NewFile(uintptr(f), "commit-msg")
		b := make([]byte, 4096)
		n, _ := o.Read(b)
		o.Close()
		actuel = b[:n]
	} else if !errors.Is(err, syscall.ENOENT) {
		return nil // a link in its place: left alone
	}
	if actuel != nil && !crochetAthanor(actuel) {
		return nil // a hook of the user, not ours
	}
	if actif {
		return ecrireDansFd(h, "commit-msg", fmt.Sprintf(".commit-msg.%d", os.Getpid()), 0o755, texteCrochet)
	}
	if actuel != nil {
		return syscall.Unlinkat(h, "commit-msg")
	}
	return nil
}
