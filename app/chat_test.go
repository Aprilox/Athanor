package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLireTourChat(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.jsonl")
	_ = os.WriteFile(f, []byte(`{"type":"system","subtype":"init","session_id":"s-1","model":"sonnet"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Je regarde."},{"type":"tool_use","id":"a","name":"Read","input":{"file_path":"/p/src/x.luau"}}]},"session_id":"s-1"}
{"type":"assistant","parent_tool_use_id":"z","message":{"content":[{"type":"text","text":"sous-agent"}]}}
{"type":"result","is_error":false,"result":"Réponse.\n<note>Fais X.</note>","total_cost_usd":0.01,"session_id":"s-1"}
`), 0o644)
	textes, outils, final, session, cout, erreur := lireTour(f)
	if len(textes) != 1 || len(outils) != 1 || !strings.HasPrefix(outils[0], "Reads ") || session != "s-1" || cout != 0.01 || erreur != "" || !strings.Contains(final, "<note>") {
		t.Fatalf("%q %q %q %q %v %q", textes, outils, final, session, cout, erreur)
	}
	if m := reNote.FindStringSubmatch(final); m == nil || m[1] != "Fais X." {
		t.Fatalf("note: %v", m)
	}
}

// One chat turn: read-only or editing tools, the model and the session given to Claude Code, the Go settings
// (the Go file guard as its hook), the stream into ID.jsonl, an edit committed.
func TestSessionChat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a shell script")
	}
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit, projet := filepath.Join(d, "kit"), filepath.Join(d, "projet")
	_ = os.MkdirAll(filepath.Join(kit, "logs", "chat"), 0o755)
	_ = os.MkdirAll(projet, 0o755)
	faux := filepath.Join(d, "claude")
	_ = os.WriteFile(faux, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > "+filepath.Join(d, "args")+"\necho '{\"type\":\"result\"}'\necho changed > x.txt\n"), 0o755)
	t.Setenv("CLAUDE_BIN", faux)
	for _, f := range []string{"msg", "prompt", "subject"} {
		_ = os.WriteFile(filepath.Join(kit, "logs", "chat", "c1."+f), []byte("x-"+f), 0o644)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if code := sessionChat(kit, projet, "c1", "question", "sonnet", ""); code != 0 {
		t.Fatalf("code %d", code)
	}
	a, _ := os.ReadFile(filepath.Join(d, "args"))
	if s := string(a); !strings.Contains(s, "--disallowedTools\nBash\nEdit\nWrite") || strings.Contains(s, "acceptEdits") || !strings.Contains(s, "--model\nsonnet") || !strings.Contains(s, "-p\nx-msg") {
		t.Fatalf("question mode: %s", s)
	}
	if s := string(a); !strings.Contains(s, "x-prompt\n\n## Ponytail: lean work") || strings.Contains(s, "smallest complete change") {
		t.Fatalf("question mode: Ponytail's lean work: %s", s)
	}
	if b, _ := os.ReadFile(filepath.Join(kit, "logs", "chat", "c1.jsonl")); !strings.Contains(string(b), "result") {
		t.Fatalf("stream: %s", b)
	}
	if s, _ := os.ReadFile(filepath.Join(kit, "logs", "chat", "c1.settings.json")); !strings.Contains(string(s), " hook fichiers ") {
		t.Errorf("settings: %s", s)
	}
	// editing: the change is committed with the subject
	g := func(args ...string) { c := exec.Command("git", args...); c.Dir = projet; _ = c.Run() }
	g("init", "-q")
	g("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	if code := sessionChat(kit, projet, "c1", "modif", "", "abc"); code != 0 {
		t.Fatalf("code %d", code)
	}
	a, _ = os.ReadFile(filepath.Join(d, "args"))
	if s := string(a); !strings.Contains(s, "acceptEdits") || !strings.Contains(s, "--resume\nabc") {
		t.Fatalf("modif mode: %s", s)
	}
	if s := string(a); !strings.Contains(s, "## Ponytail: the smallest complete change") {
		t.Fatalf("modif mode: Ponytail's rules: %s", s)
	}
	if h, _ := os.ReadFile(filepath.Join(kit, "logs", "chat", "c1.commit")); len(strings.TrimSpace(string(h))) < 7 {
		t.Fatalf("no commit")
	}
}
