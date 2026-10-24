//go:build windows

package main

import "os/exec"

func groupeSante(c *exec.Cmd) {}

// tuerGroupeSante: the program and its whole Windows process tree.
func tuerGroupeSante(c *exec.Cmd) { arreterArbre(c) }

// lancerTesterCage: Claude Code has no sandbox on Windows.
func lancerTesterCage(args []string) int {
	direSante("Native Windows mode: Claude Code has no sandbox on Windows, so there is nothing to test here.")
	direSante("Claude can read and change any file of this PC; switch back to the WSL mode (Options) for the sandbox.")
	return 0
}
