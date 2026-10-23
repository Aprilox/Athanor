//go:build !windows

package main

// athanor pont and athanor roblox-sync against bridge.py and roblox-sync.py: the same files, outputs and
// answers.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	binaireUneFois sync.Once
	binaireTest    string
	binaireErreur  error
)

// binaireAthanor: this package built once (the services run as their own process).
func binaireAthanor(t *testing.T) string {
	t.Helper()
	binaireUneFois.Do(func() {
		d, err := os.MkdirTemp("", "athanor-bin")
		if err != nil {
			binaireErreur = err
			return
		}
		binaireTest = filepath.Join(d, "athanor")
		out, err := exec.Command("go", "build", "-o", binaireTest, ".").CombinedOutput()
		if err != nil {
			binaireErreur = fmt.Errorf("%v %s", err, out)
		}
	})
	if binaireErreur != nil {
		t.Skip("cannot build: ", binaireErreur)
	}
	return binaireTest
}

func attendre(t *testing.T, cond func() bool, delai time.Duration) bool {
	t.Helper()
	fin := time.Now().Add(delai)
	for time.Now().Before(fin) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func TestParitePont(t *testing.T) {
	s := scriptPython(t, "bridge.py")
	bin := binaireAthanor(t)
	d, _ := filepath.EvalSymlinks(t.TempDir())
	resultats := map[string]string{}
	for _, moteur := range []string{"py", "go"} {
		kit, projet := filepath.Join(d, moteur, "kit"), filepath.Join(d, moteur, "projet")
		ecrireFichier(t, filepath.Join(kit, "system/bin/tool"), "#!/bin/bash\necho \"args: $*\"; echo \"cwd: ${PWD##*/}\"; echo \"direct=$PONT_DIRECT project=${PROJECT##*/} mcp=$UNREAL_MCP_URL tz=$TZ autre=${AUTRE:-}\"; echo erreur >&2; exit 3\n", 0o755)
		ecrireFichier(t, filepath.Join(kit, "system/bin/adb"), "#!/bin/bash\necho start; sleep 30; echo never\n", 0o755)
		ecrireFichier(t, filepath.Join(kit, "system/bin/blender"), "#!/bin/bash\nkill -9 $$\n", 0o755)
		_ = os.MkdirAll(filepath.Join(kit, "tools/x"), 0o755)
		_ = os.MkdirAll(filepath.Join(projet, "src"), 0o755)
		pontDir := filepath.Join(projet, ".athanor/pont")
		ecrireFichier(t, filepath.Join(pontDir, "vieux-reste"), "x", 0o644)
		var c *exec.Cmd
		if moteur == "py" {
			c = exec.Command("python3", "-I", s, kit, projet)
		} else {
			c = exec.Command(bin, "pont", kit, projet)
		}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		vivant := filepath.Join(pontDir, "vivant")
		if !attendre(t, func() bool { return estFichier(vivant) }, 10*time.Second) {
			t.Fatalf("%s: no heartbeat", moteur)
		}
		if estFichier(filepath.Join(pontDir, "vieux-reste")) {
			t.Errorf("%s: the old files stay", moteur)
		}
		demande := func(id string, v any) {
			b, _ := json.Marshal(v)
			_ = os.WriteFile(filepath.Join(pontDir, "demande-"+id+".json.tmp"), b, 0o644)
			_ = os.Rename(filepath.Join(pontDir, "demande-"+id+".json.tmp"), filepath.Join(pontDir, "demande-"+id+".json"))
		}
		demande("1-a", map[string]any{"outil": "tool", "cwd": filepath.Join(projet, "src"), "args": []any{"a b", 3, true, nil}, "env": map[string]any{"UNREAL_MCP_URL": "http://x", "TZ": "UTC", "AUTRE": "non"}})
		demande("2-b", map[string]any{"outil": "rm", "cwd": projet, "args": []any{"-rf", "/"}})
		demande("3-c", map[string]any{"outil": "tool", "cwd": "/etc"})
		demande("4-d", map[string]any{"outil": "tool", "cwd": filepath.Join(kit, "tools/x")})
		demande("5-e", map[string]any{"outil": "tool"})
		demande("6_f", map[string]any{"outil": "adb"})
		demande("7-g", map[string]any{"outil": "blender"})
		demande("8-h", []any{"pas un objet"})
		demande("pas bon!", map[string]any{"outil": "tool"})
		_ = os.WriteFile(filepath.Join(pontDir, "demande-9-i.json"), []byte(`{"outil": "tool", `), 0o644) // being written
		_ = os.Symlink(filepath.Join(d, "ailleurs.json"), filepath.Join(pontDir, "demande-10-j.json"))
		fini := func(id string) bool { return estFichier(filepath.Join(pontDir, "code-"+id)) }
		if !attendre(t, func() bool { return estFichier(filepath.Join(pontDir, "sortie-6_f.log")) }, 10*time.Second) {
			t.Errorf("%s: adb not started", moteur)
		}
		time.Sleep(500 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(pontDir, "annuler-6_f"), nil, 0o644)
		ids := []string{"1-a", "2-b", "3-c", "4-d", "5-e", "6_f", "7-g", "8-h"}
		if !attendre(t, func() bool {
			for _, id := range ids {
				if !fini(id) {
					return false
				}
			}
			return true
		}, 20*time.Second) {
			t.Errorf("%s: requests not finished", moteur)
		}
		var r []string
		for _, id := range ids {
			code, _ := os.ReadFile(filepath.Join(pontDir, "code-"+id))
			sortie, _ := os.ReadFile(filepath.Join(pontDir, "sortie-"+id+".log"))
			o := string(sortie)
			if id == "8-h" {
				o = regexp.MustCompile(`error .*`).ReplaceAllString(o, "error …")
			}
			r = append(r, fmt.Sprintf("%s: %s %q", id, code, o))
		}
		for _, n := range []string{"demande-pas bon!.json", "demande-9-i.json", "demande-10-j.json"} {
			_, err := os.Lstat(filepath.Join(pontDir, n))
			r = append(r, fmt.Sprintf("%s present %v", n, err == nil))
		}
		resultats[moteur] = strings.Join(r, "\n")
		_ = c.Process.Signal(syscall.SIGTERM)
		_ = c.Wait()
		if code := c.ProcessState.ExitCode(); code != 0 {
			t.Errorf("%s: exit %d after TERM", moteur, code)
		}
	}
	if resultats["py"] != resultats["go"] {
		t.Errorf("bridge:\n go %s\n py %s", resultats["go"], resultats["py"])
	}
}

// kitRojo: a Rojo project with every kind of script and folder.
func kitRojo(t *testing.T, p string) {
	f := func(r, c string) { ecrireFichier(t, filepath.Join(p, r), c, 0o644) }
	f("default.project.json", "\ufeff"+`{"name": "Jeu", "globIgnorePaths": ["**/*.spec.luau", "src/ignore/*", 3], "emitLegacyScripts": true,
 "tree": {"$className": "DataModel",
  "ReplicatedStorage": {"$className": "ReplicatedStorage", "Partage": {"$path": "src/shared"}, "Seul": {"$path": "src/seul.luau"}, "Modele": {"$path": "assets/m.rbxm"}},
  "ServerScriptService": {"$className": "ServerScriptService", "Serveur": {"$path": "src/server"}},
  "StarterPlayer": {"$className": "StarterPlayer", "StarterPlayerScripts": {"$className": "StarterPlayerScripts", "Client": {"$path": "src/client"}}},
  "Workspace": {"$className": "Workspace", "Part": {"$className": "Part", "$properties": {"Anchored": true}}},
  "Absent": {"$path": "src/absent"}, "Liste": [1, 2]}}`)
	f("src/shared/Util.luau", "return {}\r\n-- é\n")
	f("src/shared/Config.module.lua", "return 1\n")
	f("src/shared/donnees.json", "{}")
	f("src/shared/Etrange.foo.luau", "x")
	f("src/shared/test.spec.luau", "ignored")
	f("src/shared/Sous/init.luau", "return 'init'\n")
	f("src/shared/Sous/Enfant.server.luau", "print(1)\n")
	f("src/shared/.cache.luau", "hidden")
	f("src/shared/Vide/.gitkeep", "")
	f("src/seul.luau", "return 'seul'\n")
	f("src/server/init.server.luau", "print('main')\n")
	f("src/server/Aide.luau", "return 2\n")
	f("src/server/Gros.luau", strings.Repeat("-", 100))
	f("src/client/Main.client.luau", "print('client')\xff\n")
	f("src/client/INIT.CLIENT.LUA", "print('upper')\n")
	f("src/ignore/x.luau", "x")
	f("assets/m.rbxm", "bin")
}

func TestPariteRobloxSync(t *testing.T) {
	s := scriptPython(t, "roblox-sync.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(d, "projet")
	kitRojo(t, p)
	for _, cas := range []func(){
		func() {},
		func() {
			b, _ := os.ReadFile(filepath.Join(p, "default.project.json"))
			_ = os.WriteFile(filepath.Join(p, "default.project.json"), []byte(strings.Replace(string(b), `"emitLegacyScripts": true`, `"emitLegacyScripts": false`, 1)), 0o644)
		},
		func() { _ = os.WriteFile(filepath.Join(p, "default.project.json"), []byte("{pas du json"), 0o644) },
		func() {
			os.Remove(filepath.Join(p, "default.project.json"))
			ecrireFichier(t, filepath.Join(p, "a/b/jeu.project.json"), `{"tree": {"$className": "DataModel", "RS": {"$path": "../../src/shared"}}}`, 0o644)
		},
		func() { os.RemoveAll(filepath.Join(p, "a")) },
	} {
		cas()
		so, _, code := pythonEntree(t, nil, nil, s, "payload", p)
		gso, _, gcode := capturer(t, func() int { return lancerRobloxSync([]string{"payload", p}) })
		n := regexp.MustCompile(`cannot be read: [^"]*`)
		if code != gcode || n.ReplaceAllString(so, "cannot be read: …") != n.ReplaceAllString(gso, "cannot be read: …") {
			t.Errorf("payload:\n go %d %s\n py %d %s", gcode, gso, code, so)
		}
	}
}

func TestPariteRobloxServeur(t *testing.T) {
	s := scriptPython(t, "roblox-sync.py")
	bin := binaireAthanor(t)
	d, _ := filepath.EvalSymlinks(t.TempDir())
	type serveur struct {
		url, luau string
		c         *exec.Cmd
	}
	reAdresse := regexp.MustCompile(`http://127\.0\.0\.1:(\d+)/([A-Za-z0-9_-]+)`)
	demarrer := func(moteur string) (serveur, string) {
		p := filepath.Join(d, moteur)
		kitRojo(t, p)
		pid := filepath.Join(d, moteur+".pid")
		_ = os.WriteFile(pid, []byte("1234\n"), 0o644)
		var c *exec.Cmd
		if moteur == "py" {
			c = exec.Command("python3", "-I", s, "serve", p, pid)
		} else {
			c = exec.Command(bin, "roblox-sync", "serve", p, pid)
		}
		out, _ := c.StdoutPipe()
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { _, _ = io.Copy(io.Discard, out) }()
		luau := filepath.Join(p, ".athanor/studio/SYNC.luau")
		if !attendre(t, func() bool { return estFichier(luau) }, 10*time.Second) {
			t.Fatalf("%s: no SYNC.luau", moteur)
		}
		b, _ := os.ReadFile(luau)
		m := reAdresse.FindStringSubmatch(string(b))
		if m == nil {
			t.Fatalf("%s: no address in SYNC.luau", moteur)
		}
		return serveur{m[0], reAdresse.ReplaceAllString(string(b), "ADRESSE"), c}, p
	}
	lire := func(url string) (int, any) {
		r, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var v any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &v)
		return r.StatusCode, v
	}
	py, ppy := demarrer("py")
	gs, pgo := demarrer("go")
	if py.luau != gs.luau {
		t.Error("SYNC.luau differs")
	}
	for etape := 0; etape < 3; etape++ {
		switch etape {
		case 1: // a model and a script change: the model is announced, the script sent
			for _, p := range []string{ppy, pgo} {
				time.Sleep(20 * time.Millisecond)
				_ = os.WriteFile(filepath.Join(p, "assets/m.rbxm"), []byte("bin2"), 0o644)
				_ = os.WriteFile(filepath.Join(p, "src/shared/Util.luau"), []byte("return 'neuf'\n"), 0o644)
				_ = os.WriteFile(filepath.Join(p, "src/shared/autre.txt"), []byte("t"), 0o644)
			}
		case 2:
		}
		ca, a := lire(py.url)
		cb, b := lire(gs.url)
		if ca != cb || !reflect.DeepEqual(a, b) {
			t.Errorf("step %d:\n go %d %v\n py %d %v", etape, cb, b, ca, a)
		}
		if etape == 1 && !strings.Contains(fmt.Sprint(b), "assets/m.rbxm") {
			t.Errorf("the changed model is not announced: %v", b)
		}
	}
	ca, _ := lire(py.url + "x")
	cb, _ := lire(gs.url + "x")
	if ca != 404 || cb != 404 {
		t.Errorf("a wrong key: go %d py %d", cb, ca)
	}
	// the pilot stops: both stop and take their SYNC.luau away
	os.Remove(filepath.Join(d, "py.pid"))
	_ = os.WriteFile(filepath.Join(d, "go.pid"), []byte("999\n"), 0o644)
	for _, x := range []struct {
		s serveur
		p string
	}{{py, ppy}, {gs, pgo}} {
		fini := make(chan error, 1)
		go func() { fini <- x.s.c.Wait() }()
		select {
		case <-fini:
		case <-time.After(15 * time.Second):
			_ = x.s.c.Process.Kill()
			t.Errorf("%s: still running", x.p)
		}
		if estFichier(filepath.Join(x.p, ".athanor/studio/SYNC.luau")) {
			t.Errorf("%s: SYNC.luau stays", x.p)
		}
	}
}
