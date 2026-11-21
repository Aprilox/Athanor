package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRaccourciAncienRenomme: an older Athanor.lnk becomes "Athanor Hub.lnk" (the same file, never a second
// shortcut); with both there, the old one goes and the new one stays.
func TestRaccourciAncienRenomme(t *testing.T) {
	d := t.TempDir()
	ancien, neuf := filepath.Join(d, "Athanor.lnk"), filepath.Join(d, "Athanor Hub.lnk")
	if err := os.WriteFile(ancien, []byte("ancien"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := renommerAncienLnk(d); got != neuf {
		t.Fatalf("path %q", got)
	}
	if b, err := os.ReadFile(neuf); err != nil || string(b) != "ancien" {
		t.Fatalf("not renamed: %q %v", b, err)
	}
	if _, err := os.Stat(ancien); err == nil {
		t.Fatal("old shortcut still there")
	}
	_ = os.WriteFile(ancien, []byte("ancien"), 0o644)
	renommerAncienLnk(d)
	if _, err := os.Stat(ancien); err == nil {
		t.Fatal("old shortcut doubled")
	}
	if b, _ := os.ReadFile(neuf); string(b) != "ancien" {
		t.Fatal("new shortcut replaced")
	}
}
