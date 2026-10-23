//go:build !linux

package main

import (
	"fmt"
	"os"
)

// The cage, Claude's Linux screen and the Windows MCP servers through WSL are Linux things: elsewhere the
// scripts (and native Windows' own way) stay.
func pasIci(nom string) int {
	fmt.Fprintln(os.Stderr, "athanor "+nom+": WSL and Linux only")
	return 2
}

func lancerMCPCage(args []string) int         { return pasIci("mcp-cage") }
func lancerEcran(args []string) int           { return pasIci("ecran") }
func lancerMCPWindows(args []string) int      { return pasIci("mcp-windows") }
func lancerGardeMCPWindows(args []string) int { return pasIci("mcp-windows-garde") }
