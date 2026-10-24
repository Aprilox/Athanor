//go:build !windows

package main

import (
	"context"
	"os/exec"
)

// commande: exec.Command (Windows hides the console of the programs started, commande_windows.go).
func commande(nom string, args ...string) *exec.Cmd { return exec.Command(nom, args...) }

func commandeContexte(ctx context.Context, nom string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, nom, args...)
}
