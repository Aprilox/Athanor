package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJournalPiloteSuivi: the pilot's log turns over past maxJournalPilote, its published copy is the whole private
// file, and the page's follower reads every line once (no line twice, none cut) across the turn over.
func TestJournalPiloteSuivi(t *testing.T) {
	ancien := maxJournalPilote
	maxJournalPilote = 64 << 10 // the rule, at a size a test writes fast
	t.Cleanup(func() { maxJournalPilote = ancien })
	p := &pilote{prive: t.TempDir(), etat: t.TempDir()}
	s := &Suiveur{Chemin: filepath.Join(p.etat, "pilot.log")}
	vus := map[string]int{}
	lire := func() {
		for _, l := range s.NouvellesLignes() {
			i := strings.Index(l, "ligne ")
			if i < 0 || !strings.HasSuffix(l, strings.Repeat("x", 300)) {
				t.Fatalf("a line cut: %.80q", l)
			}
			vus[l[i:i+12]]++
		}
	}
	const n = 500 // ~165 KB: two turns over
	for i := range n {
		p.log(fmt.Sprintf("ligne %06d %s", i, strings.Repeat("x", 300)))
		if i%7 == 0 {
			lire()
		}
	}
	lire()
	if _, err := os.Stat(filepath.Join(p.prive, "pilot.log.1")); err != nil {
		t.Error("the private log did not turn over")
	}
	if fi, err := os.Stat(filepath.Join(p.prive, "pilot.log")); err != nil || fi.Size() > maxJournalPilote+1000 {
		t.Errorf("the private log: %v", err)
	}
	doubles := 0
	for _, c := range vus {
		if c > 1 {
			doubles++
		}
	}
	if doubles > 0 || len(vus) < n-20 { // at most the lines written between the follower's last read and the turn over
		t.Errorf("lines read twice: %d, lines read: %d of %d", doubles, len(vus), n)
	}
}
