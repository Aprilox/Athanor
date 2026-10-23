package main

// The Go settings against settings.py and chat.py: the same JSON, but for the hook commands, which call
// this executable (athanor hook …) instead of the Python guards; their arguments are the same.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// shlexSplit: enough of shlex.split for the commands the settings write (plain words, '…' quoting).
func shlexSplit(s string) []string {
	var out []string
	var cur strings.Builder
	dans, mot := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case dans && c == '\'':
			dans = false
		case dans:
			cur.WriteByte(c)
		case c == '\'':
			dans, mot = true, true
		case c == '"' && strings.HasPrefix(s[i:], `"'"`):
			cur.WriteByte('\'')
			i += 2
		case c == ' ':
			if mot {
				out = append(out, cur.String())
				cur.Reset()
				mot = false
			}
		default:
			cur.WriteByte(c)
			mot = true
		}
	}
	if mot {
		out = append(out, cur.String())
	}
	return out
}

// sansProgramme: the settings with each hook command reduced to its arguments (after the program).
func sansProgramme(t *testing.T, v any) any {
	t.Helper()
	pre := v.(map[string]any)["hooks"].(map[string]any)["PreToolUse"].([]any)
	for _, c := range pre {
		h := c.(map[string]any)["hooks"].([]any)[0].(map[string]any)
		mots := shlexSplit(h["command"].(string))
		switch {
		case len(mots) > 1 && (strings.HasSuffix(mots[1], "guard.py") || strings.HasSuffix(mots[1], "bash_guard.py")):
			nom := map[bool]string{true: "bash", false: "fichiers"}[strings.HasSuffix(mots[1], "bash_guard.py")]
			h["command"] = append([]string{nom}, mots[2:]...)
		case len(mots) > 2 && mots[1] == "hook":
			h["command"] = mots[2:]
		}
		b, _ := json.Marshal(h["command"])
		var x any
		_ = json.Unmarshal(b, &x)
		h["command"] = x
	}
	return v
}

func TestPariteReglages(t *testing.T) {
	_ = scriptPython(t, "settings.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit := filepath.Join(d, "kit")
	_ = os.MkdirAll(filepath.Join(kit, "system", "bin"), 0o755)
	for _, f := range []string{"settings.py", "extensions.py"} {
		b, _ := os.ReadFile(filepath.Join("..", "system", f))
		_ = os.WriteFile(filepath.Join(kit, "system", f), b, 0o755)
	}
	_ = os.MkdirAll(filepath.Join(kit, "extensions"), 0o755)
	projet := filepath.Join(d, "mon projet")
	_ = os.MkdirAll(projet, 0o755)
	_ = os.Symlink(projet, filepath.Join(d, "lien-projet"))
	cas := []struct {
		nom     string
		projets string
		pcTools string
		rtk     bool
		env     map[string]string
	}{
		{"simple", `{}`, "", false, map[string]string{"PROJECT": projet}},
		{"unreal, rtk, outils", `{"` + projet + `/": {"mcp": ["unreal", "Bad ID", "roblox-studio"], "skills": ["x"]}}`, "UE_DIR='/opt/UE 5'\nBLENDER_EXE='/opt/blender/blender'\nANDROID_SDK_DIR=''\n", true,
			map[string]string{"PROJECT": filepath.Join(d, "lien-projet"), "ATHANOR_PLUGIN": "/p/plugin", "ATHANOR_PLUGINS": "/p/plugins", "RTK": "1", "ATHANOR_SANS_COAUTEUR": "1", "CLAUDE_CONFIG_DIR": "/conf"}},
		{"ouvrier", `{"` + strings.ToUpper(projet) + `": {"mcp": ["unreal"]}}`, "", true,
			map[string]string{"PROJECT": filepath.Join(projet, ".athanor", "work", "M2"), "ATHANOR_PROJET_PRINCIPAL": projet, "RTK": "0"}},
		{"sans PROJECT", `pas du json`, "", false, map[string]string{}},
	}
	for _, c := range cas {
		_ = os.WriteFile(filepath.Join(kit, "extensions", "projets.json"), []byte(c.projets), 0o644)
		_ = os.WriteFile(filepath.Join(kit, "system", "pc_tools.env"), []byte(c.pcTools), 0o644)
		os.Remove(filepath.Join(kit, "system", "bin", "rtk"))
		if c.rtk {
			_ = os.WriteFile(filepath.Join(kit, "system", "bin", "rtk"), []byte("#!/bin/sh\n"), 0o755)
		}
		env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + d}
		for k, v := range c.env {
			env = append(env, k+"="+v)
		}
		outPy, outGo := filepath.Join(d, "py", "s.json"), filepath.Join(d, "go", "s.json")
		cmd := exec.Command("python3", "-I", filepath.Join(kit, "system", "settings.py"), kit, outPy)
		cmd.Env = env
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: settings.py: %v %s", c.nom, err, b)
		}
		// the Go one, in this process: the same environment
		ancien := os.Environ()
		os.Clearenv()
		for _, e := range env {
			k, v, _ := strings.Cut(e, "=")
			os.Setenv(k, v)
		}
		err := construireReglages(kit, outGo)
		os.Clearenv()
		for _, e := range ancien {
			k, v, _ := strings.Cut(e, "=")
			os.Setenv(k, v)
		}
		if err != nil {
			t.Fatal(err)
		}
		var p, g any
		bp, _ := os.ReadFile(outPy)
		bg, _ := os.ReadFile(outGo)
		_ = json.Unmarshal(bp, &p)
		_ = json.Unmarshal(bg, &g)
		if !reflect.DeepEqual(sansProgramme(t, p), sansProgramme(t, g)) {
			t.Errorf("%s:\n--- go\n%s\n--- python\n%s", c.nom, bg, bp)
		}
		// the same key order (Claude Code does not care, people reading the file do)
		if cles(bp) != cles(bg) {
			t.Errorf("%s: key order\n go %s\n py %s", c.nom, cles(bg), cles(bp))
		}
	}
}

func cles(b []byte) string {
	var o objetJSON
	_ = json.Unmarshal(b, &o)
	return strings.Join(o.cles, ",")
}

func TestPariteChat(t *testing.T) {
	s := scriptPython(t, "chat.py")
	d := t.TempDir()
	for _, projet := range []string{"/home/x/mon projet", "/home/x/p'q"} {
		outPy, outGo := filepath.Join(d, "py.json"), filepath.Join(d, "sous", "go.json")
		python(t, s, "settings", projet, outPy)
		if err := reglagesChat(projet, outGo); err != nil {
			t.Fatal(err)
		}
		var p, g any
		bp, _ := os.ReadFile(outPy)
		bg, _ := os.ReadFile(outGo)
		_ = json.Unmarshal(bp, &p)
		_ = json.Unmarshal(bg, &g)
		if !reflect.DeepEqual(sansProgramme(t, p), sansProgramme(t, g)) || cles(bp) != cles(bg) {
			t.Errorf("%s:\n--- go\n%s\n--- python\n%s", projet, bg, bp)
		}
	}
}

func TestReglagesContenu(t *testing.T) {
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit := filepath.Join(d, "kit")
	_ = os.MkdirAll(filepath.Join(kit, "extensions"), 0o755)
	_ = os.WriteFile(filepath.Join(kit, "extensions", "projets.json"), []byte(`{"`+d+`/p": {"mcp": ["unreal"]}}`), 0o644)
	t.Setenv("PROJECT", d+"/p")
	t.Setenv("ATHANOR_SANS_COAUTEUR", "1")
	out := filepath.Join(d, "s.json")
	if err := construireReglages(kit, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	for _, attendu := range []string{`"enableAllProjectMcpServers": true`, `"mcp__unreal"`, `hook fichiers ` + d + `/p`, `"includeCoAuthoredBy": false`, `"failIfUnavailable": true`} {
		if !strings.Contains(string(b), attendu) {
			t.Errorf("missing %s in\n%s", attendu, b)
		}
	}
}

// The chat's script picks the Go settings when the kit's executable knows them, chat.py otherwise.
func TestScriptChatReglages(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	bin := os.Getenv("ATHANOR_BIN_TEST") // a build of this package (the test binary itself is no athanor)
	if bin == "" {
		t.Skip("ATHANOR_BIN_TEST not set (an athanor build)")
	}
	for _, avecGo := range []bool{false, true} {
		d, _ := filepath.EvalSymlinks(t.TempDir())
		kit, projet := filepath.Join(d, "kit"), filepath.Join(d, "projet")
		_ = os.MkdirAll(filepath.Join(kit, "system"), 0o755)
		_ = os.MkdirAll(filepath.Join(kit, "logs", "chat"), 0o755)
		_ = os.MkdirAll(projet, 0o755)
		b, _ := os.ReadFile(filepath.Join("..", "system", "chat.py"))
		_ = os.WriteFile(filepath.Join(kit, "system", "chat.py"), b, 0o755)
		if avecGo {
			b, _ := os.ReadFile(bin)
			_ = os.WriteFile(filepath.Join(kit, "athanor"), b, 0o755)
		}
		for _, f := range []string{"msg", "prompt"} {
			_ = os.WriteFile(filepath.Join(kit, "logs", "chat", "c1."+f), []byte("x"), 0o644)
		}
		c := exec.Command("bash", "-c", scriptChat(Projet{Nom: "p", Dossier: projet}, "c1", "question", "", ""))
		c.Dir = kit
		c.Env = append(os.Environ(), "CLAUDE_BIN=true")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("script (go=%v): %v %s", avecGo, err, out)
		}
		s, _ := os.ReadFile(filepath.Join(kit, "logs", "chat", "c1.settings.json"))
		if avecGo != strings.Contains(string(s), " hook fichiers ") || avecGo == strings.Contains(string(s), "guard.py") {
			t.Errorf("go=%v: %s", avecGo, s)
		}
	}
}
