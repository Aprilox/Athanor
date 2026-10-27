package main

import "testing"

// systemeEssai: the catalog's variants as on SYS ("windows", "macos", "linux") for one test.
func systemeEssai(t *testing.T, sys string) {
	t.Helper()
	ancien := systemeMCPForce
	systemeMCPForce = sys
	t.Cleanup(func() { systemeMCPForce = ancien })
}
