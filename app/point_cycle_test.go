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

// journalEssai: a cycle's stream-json log where each Bash command of the lead took the given time.
func journalEssai(t *testing.T, d string, cmds map[string]time.Duration, ordre []string) string {
	t.Helper()
	debut := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	var b strings.Builder
	ligne := func(v any) { j, _ := json.Marshal(v); b.Write(j); b.WriteByte('\n') }
	for i, c := range ordre {
		id := "t" + string(rune('a'+i))
		ligne(map[string]any{"type": "assistant", "timestamp": debut.Format(time.RFC3339Nano), "parent_tool_use_id": nil,
			"message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": c}}}}})
		debut = debut.Add(cmds[c])
		ligne(map[string]any{"type": "user", "timestamp": debut.Format(time.RFC3339Nano), "parent_tool_use_id": nil,
			"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}})
	}
	// a sub-agent's command is not the lead's
	ligne(map[string]any{"type": "assistant", "timestamp": debut.Format(time.RFC3339Nano), "parent_tool_use_id": "x",
		"message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "sous", "name": "Bash", "input": map[string]any{"command": "sleep 600"}}}}})
	f := filepath.Join(d, "cycle.jsonl")
	_ = os.WriteFile(f, []byte(b.String()), 0o644)
	return f
}

func TestCommandesLentes(t *testing.T) {
	d := t.TempDir()
	q := "cd \"$PROJECT\"; bash .athanor/check.sh quick n3 > .athanor/.verif/q.log 2>&1"
	durees := map[string]time.Duration{q: 50 * time.Second, "./gradlew build": 3 * time.Minute, "ls": time.Second}
	f := journalEssai(t, d, durees, []string{q, "ls", "./gradlew build", q})
	cmds := commandesDuCycle(f)
	if len(cmds) != 4 {
		t.Fatalf("commands: %v", cmds)
	}
	l := commandesLentes(cmds)
	if len(l) != 2 || l[0].cmd != "./gradlew build" || l[1].cmd != "bash .athanor/check.sh quick n3" || l[1].n != 2 || l[1].total != 100*time.Second {
		t.Fatalf("slow commands: %+v", l)
	}
}

func TestPointDeDepart(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	projet := t.TempDir()
	etat := filepath.Join(projet, ".athanor")
	_ = os.MkdirAll(filepath.Join(etat, ".verif"), 0o755)
	_ = os.WriteFile(filepath.Join(etat, "PLAN.md"), []byte("- [x] M1 : Skeleton\n- [ ] M2 : Contact form — Files: contact.html\n"), 0o644)
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "M1: Add the skeleton"}} {
		if a[0] == "add" {
			_ = os.WriteFile(filepath.Join(projet, "index.html"), []byte("x"), 0o644)
		}
		c := exec.Command("git", append([]string{"-C", projet, "-c", "user.name=e", "-c", "user.email=e@example.invalid"}, a...)...)
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, b)
		}
	}
	// a full check that passed on this very state
	t.Setenv("PROJECT", projet)
	journal := filepath.Join(etat, ".verif", "full.log")
	b, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"command": "bash .athanor/check.sh > .athanor/.verif/full.log 2>&1"}})
	_ = gardeBash(b)
	_ = os.WriteFile(journal, []byte("CHECK: PASSED\n"), 0o644)
	p := pointDeDepart(projet, etat, []commandeLente{{cmd: "./gradlew build", n: 2, total: 6 * time.Minute}})
	for _, attendu := range []string{"M2 : Contact form — Files: contact.html", "M1: Add the skeleton", "**Not committed:** nothing",
		"passed on exactly this state", "6 min 00 s (2×): `./gradlew build`", "without testing less"} {
		if !strings.Contains(p, attendu) {
			t.Errorf("missing %q in\n%s", attendu, p)
		}
	}
	// a change since: the check is no longer said to hold, and short slow commands are not worth a line
	_ = os.WriteFile(filepath.Join(projet, "index.html"), []byte("y"), 0o644)
	p = pointDeDepart(projet, etat, []commandeLente{{cmd: "./gradlew build", n: 1, total: 2 * time.Minute}})
	if strings.Contains(p, "exactly this state") || !strings.Contains(p, "the project changed since") || !strings.Contains(p, "index.html") || strings.Contains(p, "cost the most time") {
		t.Errorf("after a change:\n%s", p)
	}
}
