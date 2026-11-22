package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGardeCheckComplet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	projet := t.TempDir()
	git := func(a ...string) {
		c := exec.Command("git", append([]string{"-C", projet, "-c", "user.name=essai", "-c", "user.email=essai@example.invalid"}, a...)...)
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, b)
		}
	}
	git("init", "-q")
	_ = os.WriteFile(filepath.Join(projet, "a.txt"), []byte("1"), 0o644)
	git("add", "-A")
	git("commit", "-q", "-m", "M1")
	_ = os.MkdirAll(filepath.Join(projet, ".athanor", ".verif"), 0o755)
	_ = os.WriteFile(filepath.Join(projet, ".athanor", "check.sh"), []byte("#!/bin/bash\n"), 0o755)
	t.Setenv("PROJECT", projet)
	journal := filepath.Join(projet, ".athanor", ".verif", "full.log")
	essai := func(commande string) string {
		b, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"command": commande}})
		return gardeBash(b)
	}
	complet := "cd \"$PROJECT\"; timeout 900 bash .athanor/check.sh > .athanor/.verif/full.log 2>&1; echo \"code $?\"; tail -n 5 .athanor/.verif/full.log"
	refuse := func(attendu bool, quoi string) {
		t.Helper()
		if r := essai(complet); strings.Contains(r, "already passed") != attendu {
			t.Errorf("%s: refused %v, want %v (%q)", quoi, r != "", attendu, r)
		}
	}
	refuse(false, "first run")
	_ = os.WriteFile(journal, []byte("...\nCHECK: PASSED\n"), 0o644)
	refuse(true, "again with nothing changed")
	if r := essai("bash .athanor/check.sh quick n3 > .athanor/.verif/q.log 2>&1"); r != "" {
		t.Errorf("the quick mode refused: %q", r)
	}
	// a change of the project: the full check goes through again (the earlier output is older than this run)
	_ = os.WriteFile(filepath.Join(projet, "a.txt"), []byte("2"), 0o644)
	vieux := time.Now().Add(-time.Hour)
	_ = os.Chtimes(journal, vieux, vieux)
	refuse(false, "after a change")
	refuse(false, "its output not written yet")
	// a failed run can always be run again
	_ = os.WriteFile(journal, []byte("CHECK: FAILED\n"), 0o644)
	refuse(false, "after a failure")
	// a change of check.sh itself, after a pass
	_ = os.WriteFile(journal, []byte("CHECK: PASSED\n"), 0o644)
	refuse(true, "passed")
	_ = os.WriteFile(filepath.Join(projet, ".athanor", "check.sh"), []byte("#!/bin/bash\necho\n"), 0o755)
	refuse(false, "check.sh changed")
	for _, c := range []string{"bash -n .athanor/check.sh", "grep -n CHECK .athanor/check.sh", "cat .athanor/.verif/full.log"} {
		if r := essai(c); r != "" {
			t.Errorf("%s refused: %q", c, r)
		}
	}
}
