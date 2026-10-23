package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemplacables(t *testing.T) {
	d := t.TempDir()
	defer definirKitTextes("")
	definirKitTextes(d)
	integre := []byte(`{"mcp":[]}`)
	if string(jsonRemplacable("app/catalogue.json", integre)) != string(integre) {
		t.Fatal("no local copy: the built-in one")
	}
	_ = os.MkdirAll(filepath.Join(d, "local", "app"), 0o755)
	f := filepath.Join(d, "local", "app", "catalogue.json")
	_ = os.WriteFile(f, []byte(`{"mcp":[{"id":"x"}]}`), 0o644)
	if string(jsonRemplacable("app/catalogue.json", integre)) != `{"mcp":[{"id":"x"}]}` {
		t.Fatal("a valid local copy wins")
	}
	_ = os.WriteFile(f, []byte(`{"mcp":[`), 0o644)
	if string(jsonRemplacable("app/catalogue.json", integre)) != string(integre) {
		t.Fatal("an invalid local copy is ignored")
	}
	if r := remplacements(d); len(r) != 1 || r[0] != "app/catalogue.json" {
		t.Fatalf("replacements: %v", r)
	}
	if !remplaceTexte("template/CLAUDE.md") || remplaceTexte("system/pilot.sh") {
		t.Fatal("only texts are replaced")
	}
}
