package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCrochetCommitRetireCoauteur: the commit-msg hook (commits without Claude as co-author) really
// runs under git: the commit goes through (on macOS too, whose sed has no GNU -i) without the lines.
func TestCrochetCommitRetireCoauteur(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	p := t.TempDir()
	gitEssai(t, p, "init", "-q", "-b", "main")
	if err := crochetCommit(p, true); err != nil {
		t.Fatal(err)
	}
	ecrireEssai(t, filepath.Join(p, "a.txt"), "a\n")
	gitEssai(t, p, "add", "-A")
	msg := "M1: level one\n\nThe details.\n\nGenerated with [Claude Code](https://claude.com/claude-code)\n\nCo-Authored-By: Claude Opus <noreply@anthropic.com>\n"
	f := filepath.Join(t.TempDir(), "msg")
	_ = os.WriteFile(f, []byte(msg), 0o644)
	c := exec.Command("git", "-c", "user.name=A", "-c", "user.email=a@example.com", "commit", "-q", "-F", f)
	c.Dir = p
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("the hook blocked the commit: %v %s", err, out)
	}
	log := gitEssai(t, p, "log", "-1", "--format=%B")
	if strings.Contains(log, "Co-Authored-By") || strings.Contains(log, "Generated with") || !strings.Contains(log, "M1: level one") || !strings.Contains(log, "The details.") {
		t.Fatalf("message: %q", log)
	}
	if l, _ := filepath.Glob(filepath.Join(p, ".git", "COMMIT_EDITMSG.athanor-*")); len(l) != 0 {
		t.Fatalf("temporary file left: %v", l)
	}
}
