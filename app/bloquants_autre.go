//go:build !windows

package main

// Linux and macOS move a folder whose files are open: nothing to name.
func programmesDuDossier(string) []ProgrammeBloquant { return nil }
