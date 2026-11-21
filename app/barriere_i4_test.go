package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestBarriereI4: the pilot's sandbox (bubblewrap in WSL and Linux, Seatbelt on macOS: both from the
// settings' sandbox.filesystem) and the Seatbelt cage of the MCP servers keep Athanor's settings, hub/
// (vault, approvals, audit journal) and modules/ out of reach, even under a writable folder.
func TestBarriereI4(t *testing.T) {
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit := filepath.Join(d, "Athanor")
	tools := filepath.Join(kit, "tools")
	for _, x := range []string{"hub", "modules", "tools"} {
		_ = os.MkdirAll(filepath.Join(kit, x), 0o755)
	}
	_ = os.WriteFile(filepath.Join(kit, "athanor-config.json"), []byte("{}"), 0o600)
	proteges := []string{filepath.Join(kit, "hub"), filepath.Join(kit, "modules"), filepath.Join(kit, "athanor-config.json")}

	// 1. the pilot's rules, as construireReglages writes them
	var fs struct{ AllowWrite, DenyWrite, DenyRead, AllowRead []string }
	b, _ := json.Marshal(filesystemBarriere(kit, tools, []string{d, tools}))
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fs.AllowWrite, []string{tools}) {
		t.Errorf("allowWrite %v", fs.AllowWrite)
	}
	for _, p := range proteges {
		if !slices.Contains(fs.DenyWrite, p) || !slices.Contains(fs.DenyRead, p) {
			t.Errorf("%s not denied: %s", p, b)
		}
	}
	for _, p := range append(fs.DenyWrite, fs.DenyRead...) {
		if strings.Contains(p, "athanor-config-") { // missing files: Claude's bubblewrap refuses them
			t.Errorf("a missing file is denied: %s", p)
		}
	}
	if !slices.Contains(fs.DenyRead, "~/") || !slices.Contains(fs.DenyRead, "/mnt") {
		t.Errorf("denyRead lost the home or /mnt: %v", fs.DenyRead)
	}

	// 2. the MCP servers' Seatbelt profile: the denial comes last, so a writable folder containing Athanor's
	// folder (d) does not give it back
	p := profilSeatbelt(filepath.Join(d, "maison"), tools, []string{d}, nil, func(string) bool { return true })
	lignes := strings.Split(strings.TrimSpace(p), "\n")
	derniere := lignes[len(lignes)-1]
	if !strings.HasPrefix(derniere, "(deny file-read* file-write* ") {
		t.Fatalf("the last rule is not a denial:\n%s", p)
	}
	for _, x := range proteges {
		if !strings.Contains(derniere, "(subpath "+chaineSBPL(x)+")") {
			t.Errorf("%s missing from the last rule: %s", x, derniere)
		}
	}
	if !strings.Contains(p, "(allow file-read* file-write* (subpath "+chaineSBPL(d)+")") {
		t.Errorf("the writable folder is gone:\n%s", p)
	}

	// 3. the settings file itself (not in native Windows: no sandbox there, the exception is said on the panel)
	if runtime.GOOS == "windows" {
		return
	}
	t.Setenv("PROJECT", filepath.Join(d, "projet"))
	out := filepath.Join(d, "s.json")
	if err := construireReglages(kit, out); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Sandbox struct {
			Filesystem struct{ DenyWrite, DenyRead []string }
		}
	}
	b, _ = os.ReadFile(out)
	_ = json.Unmarshal(b, &r)
	for _, x := range proteges {
		if !slices.Contains(r.Sandbox.Filesystem.DenyWrite, x) || !slices.Contains(r.Sandbox.Filesystem.DenyRead, x) {
			t.Errorf("%s not denied in the settings:\n%s", x, b)
		}
	}
}
