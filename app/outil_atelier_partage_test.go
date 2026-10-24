package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestAtelierPartageable(t *testing.T) {
	for f, attendu := range map[string]bool{
		".athanor/PLAN.md": true, ".athanor/docs/DESIGN.md": true, ".athanor/queue/01.md": true, ".athanor/attachments/request/a.pdf": true,
		".athanor/secrets.env": false, ".athanor/logs/cycle-1.jsonl": false, ".athanor/STATE.json": false, ".athanor/work/M2/x": false,
		".athanor/attachments/for-you-T1/x": false, ".athanor/.env": false, ".athanor/for-you/replies.json": false, "PLAN.md": false,
		".athanor/docs/../secrets.env": false,
	} {
		if atelierPartageable(f) != attendu {
			t.Errorf("%s: %v", f, !attendu)
		}
	}
}

// git itself: with the block of a shared workshop, only the shareable files are seen; switched back, none
func TestExclusionAtelierGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	p := t.TempDir()
	g := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = p
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	g("init", "-q")
	for _, f := range []string{"index.html", ".athanor/PLAN.md", ".athanor/LOG.md", ".athanor/docs/DESIGN.md", ".athanor/secrets.env", ".athanor/logs/c.jsonl",
		".athanor/STATE.json", ".athanor/attachments/request/brief.txt", ".athanor/attachments/for-you-T1/code.txt", ".athanor/.env", ".athanor/work/M1/a"} {
		_ = os.MkdirAll(filepath.Join(p, filepath.Dir(f)), 0o755)
		_ = os.WriteFile(filepath.Join(p, f), []byte("x"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(p, ".git", "info", "exclude"), []byte("# git's own comment\n.athanor/\n*.log\n"), 0o644)
	vus := func() string {
		l := strings.Fields(g("ls-files", "--others", "--exclude-standard"))
		sort.Strings(l)
		return strings.Join(l, " ")
	}
	if err := ecrireExclusionAtelier(p, true); err != nil {
		t.Fatal(err)
	}
	if v := vus(); v != ".athanor/LOG.md .athanor/PLAN.md .athanor/attachments/request/brief.txt .athanor/docs/DESIGN.md index.html" {
		t.Fatalf("shared: %s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(p, ".git", "info", "exclude")); !strings.HasPrefix(string(b), "# git's own comment\n*.log\n# Athanor") {
		t.Fatalf("the user's lines kept: %q", b)
	}
	if err := ecrireExclusionAtelier(p, false); err != nil {
		t.Fatal(err)
	}
	if v := vus(); v != "index.html" {
		t.Fatalf("not shared: %s", v)
	}
	b1, _ := os.ReadFile(filepath.Join(p, ".git", "info", "exclude"))
	_ = ecrireExclusionAtelier(p, false)
	if b2, _ := os.ReadFile(filepath.Join(p, ".git", "info", "exclude")); string(b1) != string(b2) {
		t.Fatal("written again the same: no change")
	}
}

// the scanner: a shared workshop's files are no finding, the others still are
func TestScannerAtelierPartage(t *testing.T) {
	diff := "\x01abc\n+++ b/.athanor/PLAN.md\n@@ -0,0 +1 @@\n+- [ ] M1\n+++ b/.athanor/secrets.env\n@@ -0,0 +1 @@\n+X=1\n"
	var a, b strings.Builder
	scannerSecretsAtelier("diff", strings.NewReader(diff), &a, false)
	scannerSecretsAtelier("diff", strings.NewReader(diff), &b, true)
	if !strings.Contains(a.String(), ".athanor/PLAN.md\tAthanor workshop") || strings.Contains(b.String(), ".athanor/PLAN.md") ||
		!strings.Contains(b.String(), ".athanor/secrets.env\tAthanor workshop") {
		t.Fatalf("not shared:\n%s\nshared:\n%s", a.String(), b.String())
	}
}
