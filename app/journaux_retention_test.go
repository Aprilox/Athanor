package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJournauxRetention: a project keeps its newest cycle logs only, and the pilot's output turns over past 20 MB.
func TestJournauxRetention(t *testing.T) {
	d := t.TempDir()
	for n := range maxJournauxCycle + 7 {
		ecrireFichier(t, filepath.Join(d, fmt.Sprintf("cycle-20261120-%06d-%04d.jsonl", n, n)), "{}", 0o644)
	}
	ecrireFichier(t, filepath.Join(d, "autre.jsonl"), "{}", 0o644)
	purgerJournauxCycle(d)
	l, _ := filepath.Glob(filepath.Join(d, "cycle-*.jsonl"))
	if len(l) != maxJournauxCycle || !strings.Contains(l[0], fmt.Sprintf("%06d", 7)) {
		t.Errorf("%d cycle logs kept, the oldest %s", len(l), filepath.Base(l[0]))
	}
	if _, err := os.Stat(filepath.Join(d, "autre.jsonl")); err != nil {
		t.Error("another file was removed")
	}
	f := filepath.Join(d, "output.log")
	ecrireFichier(t, f, strings.Repeat("x", 21<<20), 0o644)
	tournerJournal(f)
	if _, err := os.Stat(f); err == nil {
		t.Error("a 21 MB output was not turned over")
	}
	if fi, err := os.Stat(f + ".1"); err != nil || fi.Size() != 21<<20 {
		t.Errorf("the previous output: %v", err)
	}
	ecrireFichier(t, f, "court", 0o644)
	tournerJournal(f)
	if b, _ := os.ReadFile(f); string(b) != "court" {
		t.Error("a short output was turned over")
	}
}
