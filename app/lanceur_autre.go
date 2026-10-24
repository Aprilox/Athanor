//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// exeKit: the kit's executable, else this very program (a kit without its own copy: a development build).
func exeKit(k Kit) string {
	e := filepath.Join(k.Racine, "athanor")
	if st, err := os.Stat(e); err == nil && st.Mode().IsRegular() {
		return e
	}
	if s, err := os.Executable(); err == nil {
		return s
	}
	return e
}

// commandeKit: COMMAND ARGS run by the kit's executable, in the kit folder.
func commandeKit(k Kit, args ...string) *exec.Cmd {
	c := commande(exeKit(k), argsKit(args)...)
	c.Dir = k.Racine
	return c
}

// demarrerKitDetache: COMMAND ARGS in the background, in a session of its own: it outlives the application.
func demarrerKitDetache(k Kit, args ...string) error {
	c := commandeKit(k, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if nul, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		c.Stdin, c.Stdout, c.Stderr = nul, nul, nul
		defer nul.Close()
	}
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// fenetreKit: an interactive step (sudo, a token, a GitHub sign-in) in a terminal window of the desktop:
// Terminal on macOS, the first terminal emulator found on Linux.
func fenetreKit(k Kit, titre string, args ...string) error {
	ligne := append([]string{exeKit(k), "--fenetre", titre}, argsKit(args)...)
	if runtime.GOOS == "darwin" {
		return fenetreTerminalMacArgs(k, ligne)
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("Interactive window: no graphical session; run in a terminal: " + citerLigne(ligne))
	}
	for _, t := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xfce4-terminal", "-x"},
		{"mate-terminal", "-x"}, {"kitty"}, {"alacritty", "-e"}, {"xterm", "-e"}} {
		if chemin, err := exec.LookPath(t[0]); err == nil {
			c := commande(chemin, append(t[1:], ligne...)...)
			c.Dir = k.Racine
			return c.Start()
		}
	}
	return errors.New("Interactive window: no terminal found; run in a terminal: " + citerLigne(ligne))
}

// fenetreTerminalMacArgs: Terminal opens a .command file and runs it (the only way to give Terminal a
// command line); the file deletes itself.
func fenetreTerminalMacArgs(k Kit, ligne []string) error {
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(k.Journaux(), "window-*.command")
	if err != nil {
		return err
	}
	_, err = f.WriteString("#!/bin/sh\nrm -f -- \"$0\"\ncd " + citerShell(k.Racine) + " || exit 1\nexec " + citerLigne(ligne) + "\n")
	f.Close()
	if err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return err
	}
	return commande("open", "-a", "Terminal", f.Name()).Start()
}

// fenetreWindows: Windows only.
func fenetreWindows(k Kit, titre string, args ...string) error {
	return errors.New(titre + ": Windows only")
}

// optionFond: the pilot simply goes to the background (Linux, macOS).
func optionFond() string { return "--background" }

// garderWSLVivant: WSL only.
func garderWSLVivant(k Kit, p Projet) error { return nil }
