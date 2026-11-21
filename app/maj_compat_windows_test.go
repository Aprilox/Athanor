package main

import "testing"

// TestMajCompatDemarrageAuto: the autostart value keeps its name (renaming it would start Athanor twice).
func TestMajCompatDemarrageAuto(t *testing.T) {
	if nomRun != "Athanor" {
		t.Fatalf("autostart value renamed: %q", nomRun)
	}
}
