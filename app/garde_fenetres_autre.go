//go:build !windows

package main

// demarrerGardeFenetres: the window guard is Windows' only (garde_fenetres_windows.go).
func demarrerGardeFenetres(racine uint32, noter func(string)) {}
