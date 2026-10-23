package main

// The Go hooks against guard.py and bash_guard.py: the same calls, the same decision and message.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func lancerScriptEntree(t *testing.T, script string, entree []byte, args ...string) (int, string) {
	t.Helper()
	c := exec.Command("python3", append([]string{"-I", script}, args...)...)
	c.Stdin = strings.NewReader(string(entree))
	var errs strings.Builder
	c.Stderr = &errs
	err := c.Run()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, strings.TrimSpace(errs.String())
}

func TestPariteGardeFichiers(t *testing.T) {
	s := scriptPython(t, "guard.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	projet, tools, lecture := filepath.Join(d, "projet"), filepath.Join(d, "tools"), filepath.Join(d, "systeme")
	for _, x := range []string{projet + "/src", projet + "/.git", projet + "/.claude/hooks", tools, lecture, d + "/ailleurs"} {
		_ = os.MkdirAll(x, 0o755)
	}
	_ = os.Symlink(d+"/ailleurs", projet+"/lien-dehors")     // a link that leads out of the project
	_ = os.Symlink(projet+"/src", d+"/ailleurs/lien-dedans") // and one that leads back in
	_ = os.Symlink("../ailleurs", projet+"/relatif")         // relative target
	_ = os.Symlink(projet+"/boucle", projet+"/boucle")       // a loop
	args := []string{projet, tools, "/tmp", "--lecture=" + lecture}
	appels := []map[string]any{}
	for _, outil := range []string{"Read", "Write", "Edit", "Glob", "Grep", "NotebookEdit", "MultiEdit"} {
		for _, c := range []string{
			"src/a.lua", projet + "/src/a.lua", projet + "/../ailleurs/x", "../ailleurs/x", "lien-dehors/x", "relatif/x",
			d + "/ailleurs/lien-dedans/a", "boucle/x", ".git/config", ".git", "src/.git/x", ".claude/settings.json",
			".claude/settings.local.json", ".claude/hooks/x.sh", ".mcp.json", "sous/.mcp.json", lecture + "/guard.py",
			tools + "/bin/x", "/tmp/a", "/tmpx/a", "~/x", "~", "~root/x", "~inconnu-xyz/x", "/", "", ".", "./src/../src/b",
			projet, projet + "/", "//etc/passwd", "/etc/../tmp/z", "src//a", strings.Repeat("a/", 40) + "z",
		} {
			for _, cle := range []string{"file_path", "notebook_path", "path"} {
				appels = append(appels, map[string]any{"tool_name": outil, "cwd": projet, "tool_input": map[string]any{cle: c}})
			}
		}
	}
	for _, motif := range []string{"*.lua", "/etc/*", "~/.ssh/*", "../**", "src/../../x*", "C:/x/*", "**/*.go", "/*"} {
		appels = append(appels, map[string]any{"tool_name": "Glob", "cwd": projet, "tool_input": map[string]any{"pattern": motif}})
		appels = append(appels, map[string]any{"tool_name": "Glob", "cwd": projet, "tool_input": map[string]any{"pattern": motif, "path": "src"}})
	}
	appels = append(appels,
		map[string]any{"tool_name": "Grep", "cwd": projet, "tool_input": map[string]any{"pattern": "x"}},
		map[string]any{"tool_name": "Grep", "cwd": d + "/ailleurs", "tool_input": map[string]any{"pattern": "x"}},
		map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": "relatif-sans-cwd"}},
		map[string]any{"tool_name": "Read", "cwd": projet, "tool_input": map[string]any{"file_path": 12}},
		map[string]any{"tool_name": "Read", "cwd": projet, "tool_input": nil},
		map[string]any{"tool_name": "Read", "cwd": projet},
	)
	var entrees [][]byte
	for _, a := range appels {
		b, _ := json.Marshal(a)
		entrees = append(entrees, b)
	}
	entrees = append(entrees, []byte("pas du json"), []byte(""), []byte(`{"tool_name":"Read","tool_input":{"file_path":"/etc/x"},"cwd":"`+projet+`"}`))
	refus := 0
	for _, e := range entrees {
		code, msg := lancerScriptEntree(t, s, e, args...)
		g := gardeFichiers(args, e)
		gcode := 0
		if g != "" {
			gcode = 2
			refus++
		}
		if gcode != code || g != msg {
			t.Errorf("%s:\n go %d %q\n py %d %q", e, gcode, g, code, msg)
		}
	}
	if refus < 50 {
		t.Errorf("only %d refusals: the cases test too little", refus)
	}
}

func TestPariteGardeBash(t *testing.T) {
	s := scriptPython(t, "bash_guard.py")
	cmds := []string{
		"sleep 840; echo ok", "sleep 30", "sleep 2m", "sleep 1m 30s", "sleep 2m 1s", "sleep 0.5h", "sleep 1d", "nosleep 900",
		"x.sleep 900", "a-sleep 900", "echo; sleep 121", "sleep 120", "sleep\t300", "for i in 1 2; do sleep 60; done",
		`powershell -c "Start-Sleep -Seconds 600"`, "Start-Sleep 5", "Start-Sleep 500", "start-sleep -s 200", "Start-Sleep -Milliseconds 900000",
		"Start-Sleep -m 100000", "timeout //t 300", "timeout /t 30", "TIMEOUT.EXE /T 500", "timeout 300 npm test",
		"taskkill //IM RobloxStudioBeta.exe //F", "Stop-Process -Name RobloxStudioBeta", "pkill -f UnrealEditor", "kill $(pgrep UE4Editor)",
		"killall robloxstudio", "taskkill /IM notepad.exe; echo RobloxStudio", "git commit -m 'x'", "", "roblox-studio --rebuild",
		"timeout 120 bash -c 'until curl -s x; do sleep 5; done'",
	}
	var entrees [][]byte
	for _, c := range cmds {
		b, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": c}})
		entrees = append(entrees, b)
	}
	entrees = append(entrees, []byte("pas du json"), []byte(`{"tool_input":{"command":12}}`), []byte(`{"tool_input":null}`))
	for _, e := range entrees {
		code, msg := lancerScriptEntree(t, s, e)
		g := gardeBash(e)
		gcode := 0
		if g != "" {
			gcode = 2
		}
		if gcode != code || g != msg {
			t.Errorf("%s:\n go %d %q\n py %d %q", e, gcode, g, code, msg)
		}
	}
}
