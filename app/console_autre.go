//go:build !windows

package main

// partagerConsoleParent: Windows only (console_windows.go); elsewhere a program keeps its terminal.
func partagerConsoleParent() bool { return true }
