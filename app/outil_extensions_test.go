package main

// athanor extensions against extensions.py: the same plugin folders, MCP file, notes, Test results.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// capturer: what f writes on the standard output and error, and its exit code.
func capturer(t *testing.T, f func() int) (string, string, int) {
	t.Helper()
	d := t.TempDir()
	so, _ := os.Create(filepath.Join(d, "out"))
	se, _ := os.Create(filepath.Join(d, "err"))
	vo, ve := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = so, se
	code := f()
	os.Stdout, os.Stderr = vo, ve
	so.Close()
	se.Close()
	a, _ := os.ReadFile(filepath.Join(d, "out"))
	b, _ := os.ReadFile(filepath.Join(d, "err"))
	return string(a), string(b), code
}

func pythonComplet(t *testing.T, script string, args ...string) (string, string, int) {
	t.Helper()
	c := execPython(script, args...)
	var so, se strings.Builder
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	code := 0
	if e, ok := err.(interface{ ExitCode() int }); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func execPython(script string, args ...string) *exec.Cmd {
	return exec.Command("python3", append([]string{"-I", script}, args...)...)
}

// arbre: every file under d (relative path, mode, contents) and every link (where it leads).
func arbre(t *testing.T, d string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
		if err != nil || p == d {
			return nil
		}
		r, _ := filepath.Rel(d, p)
		st, _ := os.Lstat(p)
		switch {
		case st.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out = append(out, "lien "+r+" -> "+l)
		case st.IsDir():
			out = append(out, "dossier "+r)
		default:
			b, _ := os.ReadFile(p)
			out = append(out, fmt.Sprintf("%s %v %q", r, st.Mode().Perm(), b))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func ecrireFichier(t *testing.T, p, texte string, mode os.FileMode) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(texte), mode); err != nil {
		t.Fatal(err)
	}
}

// kitExtensions: a kit whose library holds every kind of server, skills and plugins with links.
func kitExtensions(t *testing.T, d string) (kit, projet string) {
	kit, projet = filepath.Join(d, "kit"), filepath.Join(d, "projet")
	_ = os.MkdirAll(filepath.Join(projet, ".git"), 0o755)
	ext := filepath.Join(kit, "extensions")
	// skills: one with sub-folders, a followed link, a broken link, .git and __pycache__ left out
	sk := filepath.Join(ext, "skills")
	ecrireFichier(t, filepath.Join(sk, "lua/SKILL.md"), "---\nname: lua\n---\nLua é\n", 0o644)
	ecrireFichier(t, filepath.Join(sk, "lua/scripts/run.sh"), "#!/bin/sh\necho ok\n", 0o755)
	ecrireFichier(t, filepath.Join(sk, "lua/.git/HEAD"), "x", 0o644)
	ecrireFichier(t, filepath.Join(sk, "lua/__pycache__/a.pyc"), "x", 0o644)
	ecrireFichier(t, filepath.Join(d, "dehors/ref.md"), "reference\n", 0o600)
	ecrireFichier(t, filepath.Join(d, "dehors/sous/b.md"), "b\n", 0o644)
	_ = os.Symlink(filepath.Join(d, "dehors/ref.md"), filepath.Join(sk, "lua/ref.md"))
	_ = os.Symlink(filepath.Join(d, "dehors"), filepath.Join(sk, "lua/dossier-lie"))
	_ = os.Symlink(filepath.Join(d, "nulle-part"), filepath.Join(sk, "lua/casse"))
	ecrireFichier(t, filepath.Join(sk, "vide/README.md"), "no SKILL.md", 0o644)
	_ = os.Symlink(filepath.Join(sk, "lua"), filepath.Join(sk, "lien"))
	ecrireFichier(t, filepath.Join(sk, "web/SKILL.md"), "web\n", 0o644)
	// plugins: hooks, servers and links taken out
	pl := filepath.Join(ext, "plugins")
	ecrireFichier(t, filepath.Join(pl, "outils/.claude-plugin/plugin.json"), `{"name":"outils"}`, 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/commands/a.md"), "a\n", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/hooks/hooks.json"), "{}", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/hooks/autre.json"), "{}", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/.mcp.json"), "{}", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/settings.json"), "{}", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/monitors/m.json"), "{}", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/workflows/w.yml"), "x", 0o644)
	ecrireFichier(t, filepath.Join(pl, "outils/__pycache__/x.pyc"), "x", 0o644)
	_ = os.Symlink(filepath.Join(d, "dehors"), filepath.Join(pl, "outils/lien"))
	_ = os.Symlink(filepath.Join(d, "dehors/ref.md"), filepath.Join(pl, "outils/commands/lien.md"))
	ecrireFichier(t, filepath.Join(pl, "absent-biblio/x"), "x", 0o644)
	biblio := `{
 "skills": {"lua": {}, "web": {}},
 "plugins": {"outils": {"nom": "Outils"}, "fantome": {}},
 "mcp": {
  "distant": {"config": {"type": "http", "url": "https://api.exemple.org/${REGION}/mcp", "headers": {"Authorization": "Bearer ${CLE}", "X-Option": "${FACULTATIVE}", "X-Fixe": "1"}}, "reglages": {"REGION": "eu", "PORT": 8080, "VRAI": true, "RIEN": null}, "optionnels": ["FACULTATIVE"], "sauvegarde": "  Save with Ctrl+S.  "},
  "sse": {"config": {"type": "sse", "url": "http://localhost:${PORT}/sse"}, "reglages": {"PORT": 9000}},
  "sans-cle": {"config": {"url": "https://x.org/mcp", "headers": {"K": "${CLE_ABSENTE}", "L": "${AUTRE}"}}},
  "url-fausse": {"config": {"type": "http", "url": "ftp://x"}},
  "local": {"config": {"command": "node", "args": ["{PROJECT}/srv.js", "--port", "${PORT}", 3, true], "env": {"TOKEN": "${CLE}", "OPT": "${FACULTATIVE}", "B_FIXE": "z", "A_PROJET": "{PROJECT}"}}, "reglages": {"PORT": "7000"}, "optionnels": ["FACULTATIVE"], "catalogue": "godot"},
  "mauvaise-var": {"config": {"command": "x", "env": {"1BAD": "v"}}},
  "sans-commande": {"config": {"command": "  "}},
  "inconnu": {"config": {"type": "ftp"}},
  "type-nombre": {"config": {"type": 5, "command": "x"}},
  "rien": {"nom": "pas de config"},
  "fenetre": {"config": {"type": "windows", "command": "C:\\outil.exe", "args": ["{PROJECT}"]}},
  "du-plugin": {"config": {"command": "${CLAUDE_PLUGIN_ROOT}/bin/srv", "args": ["${CLAUDE_PLUGIN_DATA}", "${CLAUDE_PROJECT_DIR}"]}, "plugin": "outils"},
  "plugin-eteint": {"config": {"command": "y"}, "plugin": "autre"},
  "unreal": {"config": {"type": "http", "url": "${UNREAL_MCP_URL}"}, "catalogue": "unreal"},
  "eteint": {"config": {"url": "https://off.org"}}
 }
}`
	ecrireFichier(t, filepath.Join(ext, "bibliotheque.json"), biblio, 0o644)
	ecrireFichier(t, filepath.Join(ext, "secrets.json"), "\ufeff"+`{"distant": {"CLE": "s3cr\u00e8t \"x\"", "REGION": ""}, "local": {"CLE": "abc", "PORT": "7100"}}`, 0o600)
	choix := map[string]any{strings.ToUpper(projet) + "/": map[string]any{
		"skills":  []any{"lua", "vide", "lien", "web", "absent", "../x", 5},
		"plugins": []any{"outils", "absent-biblio", "fantome"},
		"mcp": []any{"distant", "sse", "sans-cle", "url-fausse", "local", "mauvaise-var", "sans-commande", "inconnu", "type-nombre",
			"rien", "fenetre", "du-plugin", "plugin-eteint", "unreal", "absent", "Majuscule"},
	}}
	b, _ := json.Marshal(choix)
	ecrireFichier(t, filepath.Join(ext, "projets.json"), string(b), 0o644)
	ecrireFichier(t, filepath.Join(kit, "app/catalogue.json"), `{"mcp": [{"id": "godot", "sauvegarde": "first"}, {"id": "godot", "sauvegarde": "Save the scene (Ctrl+S)."}, {"id": "unreal", "sauvegarde": ""}, {"nom": "sans id"}]}`, 0o644)
	ecrireFichier(t, filepath.Join(kit, "system/mcp-jail.sh"), "#!/bin/sh\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n", 0o755)
	return kit, projet
}

func TestPariteExtensions(t *testing.T) {
	s := scriptPython(t, "extensions.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit, projet := kitExtensions(t, d)
	for _, env := range [][2]string{{"", ""}, {"WSL_INTEROP", "/run/x"}} {
		if env[0] != "" {
			t.Setenv(env[0], env[1])
			if _, err := os.Stat("/usr/bin/wslpath"); err != nil {
				continue // wslpath needed for a "windows" server
			}
		}
		cas := [][]string{
			{"preparer", kit, projet, "PRIVE"},
			{"preparer", kit, projet, "PRIVE", "--mcp", "PRIVE/autre.json", "--dossier-mcp", filepath.Join(d, "ouvrier"), "--principal", projet},
			{"mcp", kit, projet, "PRIVE/m.json", "--dossier-mcp", filepath.Join(d, "ouvrier"), "--principal", projet},
			{"mcp", kit, projet, "PRIVE/m.json", "--principal", projet},
			{"preparer", kit, filepath.Join(d, "autre-projet"), "PRIVE"},
		}
		for i, c := range cas {
			py, gp := filepath.Join(d, fmt.Sprintf("py%d", i)), filepath.Join(d, fmt.Sprintf("go%d", i))
			argsPy, argsGo := append([]string(nil), c...), append([]string(nil), c...)
			for j := range c {
				argsPy[j] = strings.ReplaceAll(c[j], "PRIVE", py)
				argsGo[j] = strings.ReplaceAll(c[j], "PRIVE", gp)
			}
			_ = os.MkdirAll(py, 0o755)
			_ = os.MkdirAll(gp, 0o755)
			// what a former run left must go
			ecrireFichier(t, filepath.Join(py, "plugin/vieux"), "x", 0o644)
			ecrireFichier(t, filepath.Join(gp, "plugin/vieux"), "x", 0o644)
			so, se, code := pythonComplet(t, s, argsPy...)
			gso, gse, gcode := capturer(t, func() int { return lancerExtensions(argsGo) })
			if code != gcode || so != gso || (se == "") != (gse == "") {
				t.Errorf("%v:\n go %d %q %q\n py %d %q %q", c, gcode, gso, gse, code, so, se)
			}
			if a, b := strings.Join(arbre(t, py), "\n"), strings.ReplaceAll(strings.Join(arbre(t, gp), "\n"), gp, py); a != b {
				t.Errorf("%v: different folders\n go %s\n py %s", c, b, a)
			}
		}
	}
	if b, _ := os.ReadFile(filepath.Join(d, "go0", "mcp.json")); !strings.Contains(string(b), `"distant"`) || !strings.Contains(string(b), "mcp-jail.sh") {
		t.Errorf("the case tests too little: %s", b)
	}
	if st, err := os.Stat(filepath.Join(d, "go0", "mcp.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mcp.json must be private: %v", st)
	}
}

func TestPariteExtensionsTester(t *testing.T) {
	s := scriptPython(t, "extensions.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit, projet := kitExtensions(t, d)
	// three servers: one that answers (its last answer without a delay), one that dies, one that is missing
	ecrireFichier(t, filepath.Join(d, "srv.py"), `import json, sys
for l in sys.stdin:
    m = json.loads(l)
    if m.get("id") == 1:
        print(json.dumps({"jsonrpc": "2.0", "id": 1, "result": {"serverInfo": {"name": "essai", "version": "2"}}}), flush=True)
    elif m.get("id") == 2:
        print("pas du json", flush=True)
        print(json.dumps({"jsonrpc": "2.0", "id": 7}), flush=True)
        print(json.dumps({"jsonrpc": "2.0", "id": 2, "result": {"tools": [{"name": "lire"}, {"name": "ecrire"}, {}]}}), flush=True)
        sys.exit(0)
`, 0o644)
	ecrireFichier(t, filepath.Join(d, "mort.sh"), "#!/bin/sh\nfor i in 1 2 3 4 5 6 7 8; do echo \"ligne $i\" >&2; done\nexit 3\n", 0o755)
	biblio := map[string]any{"mcp": map[string]any{
		"bon":     map[string]any{"config": map[string]any{"command": "python3", "args": []any{"-I", filepath.Join(d, "srv.py")}, "env": map[string]any{"X": "1"}}},
		"mort":    map[string]any{"config": map[string]any{"command": filepath.Join(d, "mort.sh")}},
		"absent":  map[string]any{"config": map[string]any{"command": "programme-introuvable-xyz"}},
		"distant": map[string]any{"config": map[string]any{"url": "https://x.org/mcp"}},
		"cle":     map[string]any{"config": map[string]any{"url": "https://x.org/${K}"}},
	}}
	b, _ := json.Marshal(biblio)
	ecrireFichier(t, filepath.Join(kit, "extensions/bibliotheque.json"), string(b), 0o644)
	for _, sid := range []string{"bon", "mort", "absent", "distant", "cle", "inconnu", "Mauvais id"} {
		so, _, code := pythonComplet(t, s, "tester", kit, projet, sid)
		debut := time.Now()
		gso, _, gcode := capturer(t, func() int { return lancerExtensions([]string{"tester", kit, projet, sid}) })
		var a, g any
		_ = json.Unmarshal([]byte(strings.Replace(so, `"[Errno 32] Broken pipe"`, `"no answer to initialize"`, 1)), &a) // a race of the script
		_ = json.Unmarshal([]byte(gso), &g)
		if code != gcode || !reflect.DeepEqual(a, g) || a == nil {
			t.Errorf("tester %s:\n go %d %s\n py %d %s", sid, gcode, gso, code, so)
		}
		if time.Since(debut) > 20*time.Second {
			t.Errorf("tester %s: too slow (%v)", sid, time.Since(debut))
		}
	}
	// without its cage, the program is not found
	os.Remove(filepath.Join(kit, "system/mcp-jail.sh"))
	so, _, _ := pythonComplet(t, s, "tester", kit, projet, "bon")
	gso, _, _ := capturer(t, func() int { return lancerExtensions([]string{"tester", kit, projet, "bon"}) })
	var a, g any
	_ = json.Unmarshal([]byte(so), &a)
	_ = json.Unmarshal([]byte(gso), &g)
	if !reflect.DeepEqual(a, g) {
		t.Errorf("tester without mcp-jail.sh:\n go %s\n py %s", gso, so)
	}
	// connexion: a remote server alone, in a private file
	for _, sid := range []string{"distant", "cle", "bon", "inconnu", "../x"} {
		fp, fg := filepath.Join(d, "c-py-"+sid), filepath.Join(d, "c-go-"+sid)
		_, se, code := pythonComplet(t, s, "connexion", kit, sid, fp)
		_, gse, gcode := capturer(t, func() int { return lancerExtensions([]string{"connexion", kit, sid, fg}) })
		bp, _ := os.ReadFile(fp)
		bg, _ := os.ReadFile(fg)
		var a, g any
		_ = json.Unmarshal(bp, &a)
		_ = json.Unmarshal(bg, &g)
		if code != gcode || se != gse || !reflect.DeepEqual(a, g) {
			t.Errorf("connexion %s:\n go %d %q %s\n py %d %q %s", sid, gcode, gse, bg, code, se, bp)
		}
	}
}

// A loop of links in a skill: extensions.py stopped there (no skills, no MCP at all); the copy leaves it out.
func TestExtensionsBoucle(t *testing.T) {
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit, projet := kitExtensions(t, d)
	_ = os.Symlink("..", filepath.Join(kit, "extensions/skills/lua/scripts/boucle"))
	_ = os.Symlink("../../web", filepath.Join(kit, "extensions/skills/lua/scripts/voisin"))
	prive := filepath.Join(d, "prive")
	so, _, code := capturer(t, func() int { return lancerExtensions([]string{"preparer", kit, projet, prive}) })
	if code != 0 || !strings.HasPrefix(so, "skills: lua, web · plugins: outils · MCP: distant") {
		t.Fatalf("%d %q", code, so)
	}
	if _, err := os.Stat(filepath.Join(prive, "plugin/skills/lua/scripts/boucle")); err == nil {
		t.Error("the loop was copied")
	}
	if b, _ := os.ReadFile(filepath.Join(prive, "plugin/skills/lua/scripts/voisin/SKILL.md")); string(b) != "web\n" {
		t.Errorf("a link to a neighbour folder must be followed: %q", b)
	}
}
