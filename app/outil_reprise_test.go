package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func depotReprise(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	d := t.TempDir()
	projet, prive := filepath.Join(d, "p"), filepath.Join(d, "prive")
	if err := os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(prive, 0o755)
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "T"}, {"config", "user.email", "t@t"}} {
		if out, err := exec.Command("git", append([]string{"-C", projet}, a...)...).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	_ = os.WriteFile(filepath.Join(projet, "a"), []byte("a\n"), 0o644)
	if out, err := exec.Command("sh", "-c", "cd '"+projet+"' && git add -A && git commit -qm M1").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	return projet, prive
}

func TestRepriseProjetPropre(t *testing.T) {
	projet, prive := depotReprise(t)
	// the pilot's own .gitignore block is not work left by a session
	_ = os.WriteFile(filepath.Join(projet, ".gitignore"), []byte("# Local secrets (added by Athanor)\n.env\n.env.*\n!.env.example\n!.env.sample\n!.env.template\n!.env.dist\n"), 0o644)
	_ = os.WriteFile(filepath.Join(prive, "cycle"), []byte("3"), 0o644)
	lignes, notes := reprendreProjet(projet, prive)
	if len(lignes) != 0 || len(notes) != 0 {
		t.Fatalf("clean project: %q %q", lignes, notes)
	}
	if _, err := os.Stat(filepath.Join(projet, ".athanor", "RESUME.md")); err == nil {
		t.Fatal("RESUME.md written for a clean project")
	}
}

func TestRepriseProjetInterrompu(t *testing.T) {
	projet, prive := depotReprise(t)
	vieux := filepath.Join(projet, ".git", "index.lock")
	_ = os.WriteFile(vieux, nil, 0o644)
	_ = os.Chtimes(vieux, time.Now().Add(-10*time.Minute), time.Now().Add(-10*time.Minute))
	frais := filepath.Join(projet, ".git", "HEAD.lock")
	_ = os.WriteFile(frais, nil, 0o644)
	_ = os.WriteFile(filepath.Join(projet, "b"), []byte("half\n"), 0o644)
	_ = os.WriteFile(filepath.Join(projet, ".gitignore"), []byte("node_modules\n"), 0o644)
	_ = os.WriteFile(filepath.Join(projet, ".athanor", "check.sh"), []byte("cd /home/olduser/site && npm test\nnode "+projet+"/x.js\n"), 0o755)
	_ = os.WriteFile(filepath.Join(projet, ".athanor", "PLAN.md"), []byte("- [x] M1 : a\n- [ ] M2 : b\n"), 0o644)
	lignes, notes := reprendreProjet(projet, prive)
	_ = os.Remove(frais)
	if _, err := os.Stat(vieux); err == nil {
		t.Fatal("old git lock kept")
	}
	tout := strings.Join(lignes, "\n")
	for _, x := range []string{"(.git/index.lock)", "1 of 2 milestones ticked", "RESUME.md"} {
		if !strings.Contains(tout, x) {
			t.Fatalf("missing %q in %q", x, tout)
		}
	}
	if strings.Contains(tout, "HEAD.lock") {
		t.Fatal("a fresh lock was removed")
	}
	n := strings.Join(notes, "\n")
	if !strings.Contains(n, "`?? b`") || !strings.Contains(n, "`?? .gitignore`") || !strings.Contains(n, "/home/olduser/site") || strings.Contains(n, projet+"/x.js") {
		t.Fatalf("notes: %q", n)
	}
	for _, f := range []string{"RESUME.md", "REVERIFY"} {
		if _, err := os.Stat(filepath.Join(projet, ".athanor", f)); err != nil {
			t.Fatalf("%s missing", f)
		}
	}
}
