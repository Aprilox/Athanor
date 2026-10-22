//go:build !windows

package main

// Outside Windows, the pilot keeps the computer awake itself (systemd-inhibit).
func GarderEveille(args []string) int { return 0 }
