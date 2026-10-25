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
