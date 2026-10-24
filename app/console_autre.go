//go:build !windows

package main

// partagerConsoleParent: Windows only (console_windows.go); elsewhere a program keeps its terminal.
func partagerConsoleParent() bool { return true }

// ouvrirConsole: Windows only; elsewhere the window is the terminal the step was started in.
func ouvrirConsole(titre string) {}
