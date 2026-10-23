//go:build !windows

package main

// athanor plan | solutions | reglages-ouvrier against the Python written in parallel.sh and pilot.sh (read
// from the scripts themselves): the same outputs, the same files.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// pythonDuScript: the Python program written in a script, between debut and fin.
func pythonDuScript(t *testing.T, script, apres, debut, fin string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "system", script))
	if err != nil {
		t.Skip("no " + script)
	}
	s := string(b)
	i := strings.Index(s, apres)
	if i < 0 {
		t.Fatalf("%s: %q not found", script, apres)
	}
	s = s[i:]
	j := strings.Index(s, debut)
	k := strings.Index(s[j+len(debut):], fin)
	if j < 0 || k < 0 {
		t.Fatalf("%s: program after %q not found", script, apres)
	}
	f := filepath.Join(t.TempDir(), "programme.py")
	_ = os.WriteFile(f, []byte(s[j+len(debut):j+len(debut)+k]), 0o644)
	return f
}

var planParallele = "# Plan\n\nIntro M9 : pas un jalon\n\n## Wave 1\n" +
	"- [x] M1 : skeleton — Files: index.html, css/base.css, js/app.js (shared), package.json. Depends on: none. Acceptance: x. Read: none.\n" +
	"  - [ ] M1a : a sub-item, never a milestone\n" +
	"##   WAVE 2\n" +
	"- [ ] M2 : two — Files: `src/a.ts`, src/b/, ./docs/A.md. Depends on: M1. Acceptance: x.\n" +
	"- [ ] M3 : three — Files: src/a.ts; package.json; src/c/*.ts. Depends on: M1, M2. Read: A.\n" +
	"- [X] M4.1 : four point one — Files: package.json, yarn.lock\n" +
	"- [ ] M4-b : four b\twith a tab — Depends on: nothing\n" +
	"## Wave 3\n" +
	"- [ ] M٣ : arabic digit — Files: docs/guide.md, README.md\n" +
	"- [ ] M2 : duplicate id — Files: x.txt. depends ON: M1 M3,, -\n" +
	"- [ ] Intro sans identifiant — Files: notes.rst\n" +
	"- [ ]   M10 : spaces\r\n" +
	"- [ ] J7 : looks like a generated id\n" +
	"- [ ] M8 : long id here — Files: a b c (spaces), very/long/" + strings.Repeat("x", 210) + ", lib/ (folder).\u0085- [ ] M11 : after NEL\n" +
	"- [x]M12 : no space\n"

func TestPariteParalleleListe(t *testing.T) {
	py := pythonDuScript(t, "parallel.sh", "parallele_plan() {", "<<'PY'\n", "\nPY\n")
	d := t.TempDir()
	for i, plan := range []string{planParallele, "", "- [ ] A1 : x\n- [ ] A2 : y\n- [x] A3 : z — Depends on: A1\n", strings.ReplaceAll(planParallele, "\n", "\r\n")} {
		f := filepath.Join(d, fmt.Sprintf("plan%d.md", i))
		_ = os.WriteFile(f, []byte(plan), 0o644)
		so, _, code := pythonEntree(t, nil, nil, py, f)
		gso, _, gcode := capturer(t, func() int { return lancerPlan([]string{"lister", f}) })
		if so != gso || code != gcode {
			t.Errorf("plan %d:\n go %d %q\n py %d %q", i, gcode, gso, code, so)
		}
	}
	so, _, code := pythonEntree(t, nil, nil, py, filepath.Join(d, "absent.md"))
	gso, _, gcode := capturer(t, func() int { return lancerPlan([]string{"lister", filepath.Join(d, "absent.md")}) })
	if so != gso || (code == 0) != (gcode == 0) {
		t.Errorf("no plan: go %d %q, py %d %q", gcode, gso, code, so)
	}
}

func TestPariteParalleleChevauchement(t *testing.T) {
	py := pythonDuScript(t, "parallel.sh", "parallele_chevauchement() {", "<<'PY'\n", "\nPY\n")
	d := t.TempDir()
	plan := filepath.Join(d, "PLAN.md")
	_ = os.WriteFile(plan, []byte(planParallele+"- [ ] M20 : more — Files: package.json, src/shared.ts\n- [ ] M21 : more — Files: src/shared.ts\n- [ ] M22 : x — Files: src/shared.ts, lib/x.go\n"), 0o644)
	lignes := []string{
		"M2 : two — Files: `src/a.ts`, src/b/, ./docs/A.md. Depends on: M1. Acceptance: x.",
		"M3 : three — Files: src/a.ts; package.json; src/c/*.ts. Depends on: M1, M2. Read: A.",
		"M30 : free — Files: other/file.c. Depends on: M1.",
		"M31 : shared only — Files: src/shared.ts, package-lock.json, sub/go.sum",
		"M32 : folder — Files: src/b/deep/x.ts",
		"M33 : no files",
		"M34 : FILE: Lib (Note). acceptance: y",
	}
	courants := [][]string{
		{},
		{"M3\t" + lignes[1]},
		{"M30\t" + lignes[2], "M32\t" + lignes[4]},
		{"M9\tM9 : x — Files: none.\tsrc/a.ts, ./other/"},
		{"M9\tM9 : x\t lib/ , src/shared.ts"},
		{"M8\tM8 : y — Files: src", "M3\t" + lignes[1]},
		{"sans-tab"},
	}
	for _, l := range lignes {
		for _, c := range courants {
			args := append([]string{plan, l}, c...)
			so, _, code := pythonEntree(t, nil, nil, py, args...)
			gso, _, gcode := capturer(t, func() int { return lancerPlan(append([]string{"chevauchement"}, args...)) })
			if so != gso || code != gcode {
				t.Errorf("%q %q:\n go %d %q\n py %d %q", l, c, gcode, gso, code, so)
			}
		}
	}
	so, _, _ := pythonEntree(t, nil, nil, py, filepath.Join(d, "absent"), lignes[0], "M3\t"+lignes[1])
	gso, _, _ := capturer(t, func() int {
		return lancerPlan([]string{"chevauchement", filepath.Join(d, "absent"), lignes[0], "M3\t" + lignes[1]})
	})
	if so != gso {
		t.Errorf("no plan: go %q py %q", gso, so)
	}
}

func TestPariteParalleleDocumentation(t *testing.T) {
	py := pythonDuScript(t, "parallel.sh", "parallele_documentation_seule() {", "python3 -I -c '\n", "\n'\n}")
	for _, e := range []string{
		"", "\n\n",
		"M2 : x — Files: docs/a.md, README.MD. Depends on: M1.\nM3 : y — Files: notes.txt (draft)\n",
		"M2 : x — Files: docs/a.md, src/a.ts.\n",
		"M2 : x — no files\n",
		"M2 : x — Files: docs/code.py\nM3 : y — Files: guide.adoc; spec.rst\n",
		"M2 : x — Files: ,\n",
		"M2 : x — Files: `./docs/a.md`\r\nM3 : Files: x.markdown\r\n",
	} {
		_, _, code := pythonEntree(t, nil, []byte(e), py)
		_, _, gcode := lancerAvecEntree(t, []byte(e), func() int { return lancerPlan([]string{"documentation"}) })
		if code != gcode {
			t.Errorf("%q: go %d py %d", e, gcode, code)
		}
	}
}

func TestPariteParalleleCocher(t *testing.T) {
	py := pythonDuScript(t, "parallel.sh", "issue=fusionne", "<<'PY'\n", "\nPY\n")
	d := t.TempDir()
	for _, id := range []string{"M2", "M4_1", "M4_b", "M٣", "M3", "M1", "J7", "M10", "M11", "absent"} {
		a, b := filepath.Join(d, "py.md"), filepath.Join(d, "go.md")
		_ = os.WriteFile(a, []byte(planParallele), 0o644)
		_ = os.WriteFile(b, []byte(planParallele), 0o644)
		_, _, code := pythonEntree(t, nil, nil, py, a, id)
		_, _, gcode := capturer(t, func() int { return lancerPlan([]string{"cocher", b, id}) })
		x, _ := os.ReadFile(a)
		y, _ := os.ReadFile(b)
		if code != gcode || string(x) != string(y) {
			t.Errorf("tick %s:\n go %d %q\n py %d %q", id, gcode, y, code, x)
		}
	}
}

func TestPariteSolutions(t *testing.T) {
	py := pythonDuScript(t, "pilot.sh", "SOLUTIONS_PY='", "SOLUTIONS_PY='\n", "\n'\n")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	var long strings.Builder
	long.WriteString("# Solutions found (x)\n\nIntro\n1\n2\n3\n4\n5\n6\n7\n")
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&long, "- problème %d: solution %d   \n", i%70, i)
	}
	long.WriteString("  * Indenté: oui\n- " + strings.Repeat("é", 450) + "\n-pas un point\n- PROBLÈME 3 : solution\n- !!!\n\xff\xfe- invalide\n")
	cas := map[string]string{
		"vide":   "",
		"court":  "# S\n\n- a: b\n- a: b\n- A:  B\n",
		"pareil": "# Solutions\n\n- a: b\n",
		"long":   long.String(),
		"crlf":   "# S\r\n\r\n- a: b\r\n- a: b\r\n\x0c- c\u2028- d\n",
	}
	for nom, texte := range cas {
		for _, action := range [][]string{{"ranger", "F", "Godot game"}, {"fusionner", "F", "C"}} {
			r := map[string]string{}
			for _, moteur := range []string{"py", "go"} {
				dir := filepath.Join(d, nom+"-"+action[0]+"-"+moteur)
				_ = os.MkdirAll(dir, 0o755)
				f, c := filepath.Join(dir, "SOLUTIONS.md"), filepath.Join(dir, "copie.md")
				_ = os.WriteFile(f, []byte(texte), 0o644)
				_ = os.WriteFile(c, []byte(cas["long"]+"- nouveau: oui\n- a: b\n"), 0o644)
				args := []string{action[0], f}
				if action[0] == "ranger" {
					args = append(args, action[2])
				} else {
					args = append(args, c)
				}
				var code int
				if moteur == "py" {
					_, _, code = pythonEntree(t, nil, nil, py, args...)
				} else {
					_, _, code = capturer(t, func() int { return lancerSolutions(args) })
				}
				b, _ := os.ReadFile(f)
				st, _ := os.Stat(f)
				r[moteur] = fmt.Sprintf("%d %v %q", code, st.Mode().Perm(), b)
			}
			if r["py"] != r["go"] {
				t.Errorf("%s %s:\n go %s\n py %s", nom, action[0], r["go"], r["py"])
			}
		}
	}
	// missing, a link, a pipe, a folder in the file's place
	for _, place := range []string{"absent", "lien", "tube", "dossier"} {
		r := map[string]string{}
		for _, moteur := range []string{"py", "go"} {
			dir := filepath.Join(d, place+"-"+moteur)
			_ = os.MkdirAll(dir, 0o755)
			f := filepath.Join(dir, "SOLUTIONS.md")
			_ = os.WriteFile(filepath.Join(dir, "cible"), []byte("- secret: x\n"), 0o644)
			switch place {
			case "lien":
				_ = os.Symlink(filepath.Join(dir, "cible"), f)
			case "tube":
				_ = syscall.Mkfifo(f, 0o644)
			case "dossier":
				_ = os.Mkdir(f, 0o755)
			}
			args := []string{"ranger", f, "a project"}
			if moteur == "py" {
				_, _, _ = pythonEntree(t, nil, nil, py, args...)
			} else {
				_, _, _ = capturer(t, func() int { return lancerSolutions(args) })
			}
			st, err := os.Lstat(f)
			b, _ := os.ReadFile(f)
			c, _ := os.ReadFile(filepath.Join(dir, "cible"))
			r[moteur] = fmt.Sprintf("%v %v %q %q", err == nil && st.Mode().IsRegular(), err == nil && st.IsDir(), b, c)
		}
		if r["py"] != r["go"] {
			t.Errorf("%s:\n go %s\n py %s", place, r["go"], r["py"])
		}
	}
}

func TestPariteReglagesOuvrier(t *testing.T) {
	py := pythonDuScript(t, "parallel.sh", `"${OUTIL_REGLAGES[@]}" "$KIT" "$PRIVE/reglages-$ident.json"`, "<<'PY' || return 1\n", "\nPY\n")
	d := t.TempDir()
	for i, r := range []string{
		`{"language": "english", "permissions": {"allow": ["WebFetch", "Bash(tool *)"], "defaultMode": "bypassPermissions"}, "env": {"TOOLS": "/k/tools", "É": "ü <&> \u2028"}, "sandbox": {"enabled": true, "failIfUnavailable": true, "filesystem": {"allowWrite": ["/p"], "denyRead": ["~/", "/mnt"], "allowRead": ["/p", "/k/system"]}, "network": {"n": 3, "x": null, "f": false}}, "hooks": {"PreToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "'/k/athanor' hook fichiers '/p'"}]}]}, "cleanupPeriodDays": 30, "vide": {}, "liste": []}`,
		`{"language": "english", "env": {"TOOLS": "C:/k/tools"}}`,
		`{"sandbox": {"filesystem": {}}}`,
		`{"sandbox": {"enabled": true}}`,
	} {
		a, b := filepath.Join(d, fmt.Sprintf("py%d.json", i)), filepath.Join(d, fmt.Sprintf("go%d.json", i))
		_ = os.WriteFile(a, []byte(r), 0o644)
		_ = os.WriteFile(b, []byte(r), 0o644)
		_, _, code := pythonEntree(t, nil, nil, py, a, "/projet/.git")
		_, _, gcode := capturer(t, func() int { return lancerReglagesOuvrier([]string{b, "/projet/.git"}) })
		x, _ := os.ReadFile(a)
		y, _ := os.ReadFile(b)
		if (code == 0) != (gcode == 0) || (code == 0 && string(x) != string(y)) {
			t.Errorf("settings %d:\n go %d %s\n py %d %s", i, gcode, y, code, x)
		}
	}
}
