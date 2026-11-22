//go:build !linux && !darwin

package main

import (
	"fmt"
	"os"
)

// lancerTUI: the terminal interface is for Linux and macOS (on Windows, Athanor is its window).
func lancerTUI(lien string, proprio bool, journal string) int {
	fmt.Fprintln(os.Stderr, "Athanor: the terminal interface is for Linux and macOS.")
	return 1
}

func viderEntree(fd int) {}

// estTerminal: a console (Windows has no termios to ask; athanor configurer's prompts and colours).
func estTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// lancerEnFond: Athanor in the background is for the terminal of Linux and macOS.
func lancerEnFond() (int, bool) { return 0, false }
