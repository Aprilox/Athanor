//go:build windows

package main

import (
	"context"
	"os/exec"
	"syscall"
)

// commande: exec.Command, with the program's console hidden when this process has none to share (the
// application, a pilot it started): Windows would otherwise open a console window for each console program
// started (git, bash, the tools), which flashes on the screen. Its children share that hidden console.
func commande(nom string, args ...string) *exec.Cmd {
	c := exec.Command(nom, args...)
	masquerConsole(c)
	return c
}

func commandeContexte(ctx context.Context, nom string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, nom, args...)
	masquerConsole(c)
	return c
}

func masquerConsole(c *exec.Cmd) {
	if !consolePartagee && c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	}
}
