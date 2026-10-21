package main

import (
	"os"
	"path/filepath"
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

func TestScriptChat(t *testing.T) {
	p := Projet{Nom: "x", Dossier: `C:\Users\A'b\Jeu`}
	q := scriptChat(p, "id1", "question", "sonnet", "")
	if !strings.Contains(q, `--disallowedTools Bash Edit Write`) || strings.Contains(q, "acceptEdits") || !strings.Contains(q, `D='C:\Users\A'\''b\Jeu'`) {
		t.Fatal("question mode: read-only, the folder quoted")
	}
	m := scriptChat(p, "id1", "modif", "", "abc")
	if !strings.Contains(m, "acceptEdits") || !strings.Contains(m, "--disallowedTools Bash") || !strings.Contains(m, "S='abc'") {
		t.Fatal("modif mode")
	}
}
