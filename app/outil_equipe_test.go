package main

// The Go team against equipe.py: the same proposals imported or refused (same lines, same agents.json,
// same .refused.txt), the same --agents JSON, the same TEAM.md.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func copierArbre(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
}

func preparerEquipe(t *testing.T, d string) (kit, projet, agents string) {
	kit, projet, agents = filepath.Join(d, "kit"), filepath.Join(d, "projet"), filepath.Join(d, "agents")
	_ = os.MkdirAll(filepath.Join(kit, "extensions"), 0o755)
	_ = os.MkdirAll(filepath.Join(projet, ".athanor", "agents"), 0o755)
	copierArbre(t, filepath.Join("..", "template", ".claude", "agents"), agents)
	// one of Athanor's agents without a model line and with CRLF, one with tools
	_ = os.WriteFile(filepath.Join(agents, "zz-test.md"), []byte("---\r\nname: zz-test\r\ndescription: test agent\r\ntools: Read, Grep ,Glob\r\n---\r\nDo it.\r\n"), 0o644)
	existant := `{"agents": [
  {"nom": "traducteur", "description": "Translates.", "consignes": "Translate well.", "modele": "eco", "lecture_seule": true, "projet": "", "origine": "utilisateur", "extra": 1},
  {"nom": "fort-un", "description": "Strong.", "consignes": "Be strong.", "modele": "fort", "projet": "` + projet + `", "origine": "chef"},
  {"nom": "INVALIDE", "description": "x", "consignes": "y"},
  {"nom": "autre-projet", "description": "x", "consignes": "y", "projet": "/ailleurs"},
  {"nom": "mauvais-modele", "description": "x", "consignes": "y", "modele": "gpt"}
], "refuses": {"` + strings.ToUpper(projet) + `/": ["jamais", 3], "x": "pas une liste"}, "autre": {"garde": true}}`
	_ = os.WriteFile(filepath.Join(kit, "extensions", "agents.json"), []byte("\ufeff"+existant), 0o644)
	p := filepath.Join(projet, ".athanor", "agents")
	ecrire := func(n, c string) { _ = os.WriteFile(filepath.Join(p, n), []byte(c), 0o644) }
	ecrire("bon.md", "---\nname: Relecteur\ndescription: \"Relit les textes.\"\nmodel: cheap   (or default)\nread_only: yes\n---\n\nRelis tout.\n")
	ecrire("crlf.md", "\ufeff---\r\nname: crlf-agent\r\ndescription: 'CRLF.'\r\nmodel: strong\r\n---\r\nCorps.\r\n")
	ecrire("doublon.md", "---\nname: traducteur\ndescription: d\n---\nc\n")
	ecrire("refuse.md", "---\nname: jamais\ndescription: d\n---\nc\n")
	ecrire("reserve.md", "---\nname: tester\ndescription: d\n---\nc\n")
	ecrire("modele.md", "---\nname: modele-x\ndescription: d\nmodel: gpt-5\n---\nc\n")
	ecrire("sans.md", "pas de front matter")
	ecrire("vide.md", "---\nname: vide\ndescription: \n---\nc\n")
	ecrire("long.md", "---\nname: long\ndescription: d\n---\n"+strings.Repeat("x", 40001))
	ecrire(".cache.md", "---\nname: cache\ndescription: d\n---\nc\n")
	ecrire("notes.txt", "pas un .md")
	_ = os.Symlink("/etc/hostname", filepath.Join(p, "lien.md"))
	for i := 0; i < 4; i++ {
		ecrire("chef"+string(rune('a'+i))+".md", "---\nname: chef-"+string(rune('a'+i))+"\ndescription: chef "+string(rune('a'+i))+"\n---\nc\n")
	}
	_ = os.WriteFile(filepath.Join(p, ".refused.txt"), []byte("ancien: une raison\n"), 0o644)
	return
}

func TestPariteEquipe(t *testing.T) {
	s := scriptPython(t, "equipe.py")
	d := t.TempDir()
	dPy, dGo := filepath.Join(d, "py"), filepath.Join(d, "go")
	kp, pp, ap := preparerEquipe(t, dPy)
	kg, pg, ag := preparerEquipe(t, dGo)
	// the projects differ by their folder: the files are compared with it replaced
	norm := func(b []byte, dir string) string {
		s := strings.ReplaceAll(strings.ReplaceAll(string(b), strings.ToUpper(dir), "D"), dir, "D")
		return strings.ReplaceAll(s, strings.ReplaceAll(dir, "/", `\\`), "D")
	}
	py, _ := python(t, s, "importer", kp, pp)
	gl := strings.Join(equipeImporter(kg, pg), "\n")
	if gl != "" {
		gl += "\n"
	}
	if norm([]byte(gl), dGo) != norm([]byte(py), dPy) {
		t.Errorf("importer lines:\n--- go\n%s\n--- python\n%s", gl, py)
	}
	if !strings.Contains(py, "added to the team: relecteur") || !strings.Contains(py, "refused (reserve.md)") {
		t.Fatalf("the cases test too little:\n%s", py)
	}
	for _, f := range []string{"kit/extensions/agents.json", "projet/.athanor/agents/.refused.txt"} {
		bp, _ := os.ReadFile(filepath.Join(dPy, f))
		bg, _ := os.ReadFile(filepath.Join(dGo, f))
		var vp, vg any
		if strings.HasSuffix(f, ".json") {
			_ = json.Unmarshal([]byte(norm(bp, dPy)), &vp)
			_ = json.Unmarshal([]byte(norm(bg, dGo)), &vg)
			if cles(bp) != cles(bg) {
				t.Errorf("%s: key order go %s py %s", f, cles(bg), cles(bp))
			}
		} else {
			vp, vg = norm(bp, dPy), norm(bg, dGo)
		}
		if !reflect.DeepEqual(vp, vg) {
			t.Errorf("%s:\n--- go\n%s\n--- python\n%s", f, bg, bp)
		}
	}
	lp, _ := os.ReadDir(filepath.Join(pp, ".athanor", "agents"))
	lg, _ := os.ReadDir(filepath.Join(pg, ".athanor", "agents"))
	if len(lp) != len(lg) {
		t.Errorf("proposals left: go %d, python %d", len(lg), len(lp))
	}
	for _, env := range [][]string{{}, {"SUBAGENT_MODEL=sonnet"}, {"SUBAGENT_MODEL=auto", "CHEAP_MODEL="}, {"CHEAP_MODEL=haiku-x"}} {
		for _, ouvrier := range []bool{false, true} {
			args := []string{"-I", s, "json", kp, pp, ap}
			if ouvrier {
				args = append(args, "--ouvrier")
			}
			c := exec.Command("python3", args...)
			c.Env = append(append([]string{}, os.Environ()...), env...)
			if !strings.Contains(strings.Join(env, " "), "CHEAP_MODEL") {
				c.Env = sansVariable(c.Env, "CHEAP_MODEL")
			}
			c.Env = sansVariable(c.Env, "SUBAGENT_MODEL")
			c.Env = append(c.Env, env...)
			out, err := c.Output()
			if err != nil {
				t.Fatal(err)
			}
			restaurer := definirEnv(t, env)
			g := equipeJSON(kg, pg, ag, ouvrier)
			restaurer()
			var vp, vg any
			_ = json.Unmarshal([]byte(norm(out, dPy)), &vp)
			_ = json.Unmarshal([]byte(norm([]byte(g), dGo)), &vg)
			if !reflect.DeepEqual(vp, vg) {
				t.Errorf("json %v ouvrier=%v:\n--- go\n%s\n--- python\n%s", env, ouvrier, g, out)
			}
		}
	}
	// the template copies the json step completed with a model line
	for _, f := range []string{"executor.md", "scout.md", "zz-test.md"} {
		bp, _ := os.ReadFile(filepath.Join(ap, f))
		bg, _ := os.ReadFile(filepath.Join(ag, f))
		if string(bp) != string(bg) {
			t.Errorf("%s rewritten:\n--- go\n%s\n--- python\n%s", f, bg, bp)
		}
	}
	python(t, s, "fiche", kp, pp, ap)
	if err := equipeFiche(kg, pg, ag); err != nil {
		t.Fatal(err)
	}
	bp, _ := os.ReadFile(filepath.Join(pp, ".athanor", "TEAM.md"))
	bg, _ := os.ReadFile(filepath.Join(pg, ".athanor", "TEAM.md"))
	if norm(bp, dPy) != norm(bg, dGo) || len(bp) < 200 {
		t.Errorf("TEAM.md:\n--- go\n%s\n--- python\n%s", bg, bp)
	}
}

func sansVariable(env []string, nom string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, nom+"=") {
			out = append(out, e)
		}
	}
	return out
}

// definirEnv: these variables for the Go side (CHEAP_MODEL and SUBAGENT_MODEL unset unless given).
func definirEnv(t *testing.T, env []string) func() {
	anciens := map[string]*string{}
	for _, n := range []string{"CHEAP_MODEL", "SUBAGENT_MODEL"} {
		if v, ok := os.LookupEnv(n); ok {
			anciens[n] = &v
		} else {
			anciens[n] = nil
		}
		os.Unsetenv(n)
	}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		os.Setenv(k, v)
	}
	return func() {
		for n, v := range anciens {
			if v == nil {
				os.Unsetenv(n)
			} else {
				os.Setenv(n, *v)
			}
		}
	}
}
