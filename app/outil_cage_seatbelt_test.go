//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArgsCage(t *testing.T) {
	c, e := lireArgsCage([]string{"srv", "/k/tools", "--rw", "/p", "--ro", "/plug", "--env", "PORT", "8", "--env", "VIDE", "", "--", "npx", "-y", "x"})
	if e != "" || c.nom != "srv" || c.outils != "/k/tools" || strings.Join(c.ecriture, ",") != "/p" || strings.Join(c.lecture, ",") != "/plug" ||
		len(c.variables) != 2 || c.variables[0] != [2]string{"PORT", "8"} || c.variables[1] != [2]string{"VIDE", ""} || strings.Join(c.commande, " ") != "npx -y x" {
		t.Fatalf("%+v %q", c, e)
	}
	for _, mauvais := range [][]string{{"x"}, {"x", "/t", "--rw"}, {"x", "/t", "--env", "1A", "v", "--", "c"}, {"x", "/t", "--quoi", "--", "c"}, {"x", "/t", "--"}} {
		if _, e := lireArgsCage(mauvais); e == "" {
			t.Errorf("%v accepted", mauvais)
		}
	}
}

func TestProfilSeatbelt(t *testing.T) {
	existe := func(p string) bool { return !strings.Contains(p, "absent") }
	p := profilSeatbelt("/Users/moi", "/Users/moi/Athanor/tools", []string{"/Users/moi/Dev/jeu"}, []string{"/Users/moi/Athanor/logs/pilotes/x/plugin", "/absent"}, existe)
	lignes := strings.Split(strings.TrimSpace(p), "\n")
	attendu := []string{
		`(version 1)`,
		`(allow default)`,
		`(deny file-write*)`,
		`(allow file-write* (subpath "/private/tmp") (subpath "/private/var/folders") (subpath "/dev"))`,
		`(deny file-read* file-write* (subpath "/Users/moi") (literal "/Users/moi/Athanor/.env") (subpath "/Users/moi/Athanor/extensions") (subpath "/Users/moi/Athanor/logs") (regex "^/Users/moi/Athanor/athanor-config.*\\.json$"))`,
	}
	for i, a := range attendu {
		if i >= len(lignes) || lignes[i] != a {
			t.Fatalf("line %d:\n got %s\nwant %s\n%s", i, lignes[min(i, len(lignes)-1)], a, p)
		}
	}
	// the toolchains, the plugin (read-only), the project and tools/ (read-write) come back after the denials
	reste := strings.Join(lignes[len(attendu):], "\n")
	for _, a := range []string{`(subpath "/Users/moi/.nvm")`, `(literal "/Users/moi")`, `(allow file-read* (subpath "/Users/moi/Athanor/logs/pilotes/x/plugin"))`,
		`(allow file-read* file-write* (subpath "/Users/moi/Dev/jeu") (subpath "/Users/moi/Athanor/tools"))`} {
		if !strings.Contains(reste, a) {
			t.Errorf("missing %s in\n%s", a, reste)
		}
	}
	if strings.Contains(p, "/absent") {
		t.Errorf("a missing folder is in the profile:\n%s", p)
	}
	if got := chaineSBPL(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("quoting: %s", got)
	}
}

func TestEnvironnementCage(t *testing.T) {
	env := environnementCage([]string{"PATH=/bin", "HOME=/Users/moi", "CLAUDE_CODE_OAUTH_TOKEN=x", "ANTHROPIC_API_KEY=y", "PORT=1", "AUTRE=z"},
		[][2]string{{"PORT", "8"}, {"HOME", "/ailleurs"}, {"ANTHROPIC_API_KEY", "k"}}, "/k/tools/mcp/s", "/k/tools")
	j := strings.Join(env, "\n")
	for _, a := range []string{"PATH=/bin", "AUTRE=z", "PORT=8", "HOME=/k/tools/mcp/s", "npm_config_cache=/k/tools/cache/npm", "UV_CACHE_DIR=/k/tools/cache/uv"} {
		if !strings.Contains("\n"+j+"\n", "\n"+a+"\n") {
			t.Errorf("missing %s in\n%s", a, j)
		}
	}
	for _, non := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "PORT=1", "HOME=/Users/moi", "HOME=/ailleurs"} {
		if strings.Contains(j, non) {
			t.Errorf("%s left in\n%s", non, j)
		}
	}
}

// macOS only (GitHub Actions): a server in its cage writes in the project, never in the home nor in
// Athanor's folder, and reads no file of the home.
func TestCageSeatbeltMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	bin := os.Getenv("ATHANOR_BIN_TEST")
	if bin == "" {
		t.Skip("ATHANOR_BIN_TEST not set (an athanor build)")
	}
	maison, _ := filepath.EvalSymlinks(t.TempDir())
	kit := filepath.Join(maison, "Athanor")
	projet := filepath.Join(maison, "Dev", "jeu")
	for _, d := range []string{filepath.Join(kit, "tools"), projet} {
		_ = os.MkdirAll(d, 0o755)
	}
	_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("CLAUDE_CODE_OAUTH_TOKEN=secret\n"), 0o600)
	_ = os.WriteFile(filepath.Join(maison, "perso.txt"), []byte("mes notes\n"), 0o600)
	script := `echo ok > dedans.txt; echo x > "$1/dehors.txt" 2>/dev/null && echo ECRIT; cat "$1/perso.txt" 2>/dev/null; cat "$2/.env" 2>/dev/null; echo fin`
	c := exec.Command(bin, "mcp-cage", "srv", filepath.Join(kit, "tools"), "--rw", projet, "--", "/bin/sh", "-c", script, "sh", maison, kit)
	c.Env = append(os.Environ(), "HOME="+maison, "CLAUDE_CODE_OAUTH_TOKEN=x")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	s := string(out)
	if _, err := os.Stat(filepath.Join(projet, "dedans.txt")); err != nil || strings.Contains(s, "ECRIT") || strings.Contains(s, "mes notes") || strings.Contains(s, "secret") || !strings.Contains(s, "fin") {
		t.Fatalf("cage: %s", s)
	}
}
