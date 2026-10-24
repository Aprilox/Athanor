//go:build !darwin

package main

// boucleAppMac: Athanor.app exists on the Mac only.
func boucleAppMac(a actionsAppMac) bool { return false }
