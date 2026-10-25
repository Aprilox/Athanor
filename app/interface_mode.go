package main

// Which interface Athanor opens: its window (gui) or the terminal one (tui). athanor tui / athanor gui
// choose; without either, the terminal one on a computer reached through SSH or a Linux without a screen
// (no X11 or Wayland display), the window everywhere else. gui is there in case the guess is wrong.

import (
	"os"
	"runtime"
)

// interfaceVoulue: "tui", "gui", or "" (guessed).
var interfaceVoulue string

// lireInterface: the arguments without tui / gui (kept in interfaceVoulue). Only before the arguments of
// a command (after --kit=…): "athanor github … tui" is the github command's business.
func lireInterface(args []string) []string {
	var reste []string
	commande := false
	for _, a := range args {
		if !commande {
			switch a {
			case "tui", "--tui", "terminal", "--terminal":
				interfaceVoulue = "tui"
				continue
			case "gui", "--gui":
				interfaceVoulue = "gui"
				continue
			}
			if len(a) > 0 && a[0] != '-' {
				commande = true
			}
		}
		reste = append(reste, a)
	}
	return reste
}

// avecInterface: args with the interface asked for, for the executable that runs in this one's place.
func avecInterface(args []string) []string {
	if interfaceVoulue == "" {
		return args
	}
	return append([]string{interfaceVoulue}, args...)
}

// terminalInteractif: Athanor was started from a terminal (its input and output are one).
func terminalInteractif() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		st, err := f.Stat()
		if err != nil || st.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

// interfaceTerminal: the terminal interface rather than the window.
func interfaceTerminal() bool {
	if runtime.GOOS == "windows" {
		return false // the window (a Windows executable has no terminal)
	}
	switch interfaceVoulue {
	case "tui":
		return true
	case "gui":
		return false
	}
	return devinerTerminal(runtime.GOOS, terminalInteractif(), os.Getenv)
}

// devinerTerminal: started from a terminal of a computer reached through SSH, or of a Linux without a
// screen.
func devinerTerminal(systeme string, interactif bool, env func(string) string) bool {
	if systeme == "windows" || !interactif {
		return false
	}
	if env("SSH_CONNECTION") != "" || env("SSH_TTY") != "" {
		return true
	}
	return systeme == "linux" && env("DISPLAY") == "" && env("WAYLAND_DISPLAY") == ""
}
