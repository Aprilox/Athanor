//go:build !darwin

package main

// boucleAppMac, demanderFenetreMac: Athanor.app exists on the Mac only.
func boucleAppMac(a actionsAppMac) bool { return false }
func demanderFenetreMac() bool          { return false }
