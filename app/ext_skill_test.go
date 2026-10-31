package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func kitSkillEssai(t *testing.T) (kit, d string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash and python3 as on Linux")
	}
	for _, p := range []string{"bash", "python3", "git"} {
		if trouverProgramme(p) == "" {
			t.Skip(p)
		}
	}
	kit = t.TempDir()
	d = dossierSkill(kit, "outils-pdf")
	ecrireFichier(t, filepath.Join(d, "SKILL.md"), "---\nname: outils-pdf\n---\n\nRun `python scripts/lire.py FILE`.\n", 0o644)
	ecrireFichier(t, filepath.Join(d, "scripts/prep.sh"), "#!/usr/bin/env bash\r\necho ok\r\nsed -i 's/a/b/' x\r\n", 0o755)
	ecrireFichier(t, filepath.Join(d, "scripts/lire.py"), "import sys\nprint(sys.argv[1]\n", 0o644)
	ecrireFichier(t, filepath.Join(d, "scripts/ok.js"), "console.log(1)\n", 0o644)
	ecrireFichier(t, filepath.Join(d, "LISEZMOI.txt"), "pas un script\n", 0o644)
	b, _ := os.ReadFile(filepath.Join(d, "SKILL.md"))
	_ = b
	ecrireFichier(t, filepath.Join(kit, "extensions/bibliotheque.json"), `{"skills": {"outils-pdf": {"nom": "outils-pdf", "source": "github.com/x/y", "catalogue": ""}}, "mcp": {}}`, 0o644)
	return kit, d
}

// What the test of a skill finds: its header, its programs, scripts that do not parse, other systems' habits.
func TestTesterSkill(t *testing.T) {
	kit, _ := kitSkillEssai(t)
	r := testerSkill(kit, "outils-pdf")
	tout := ""
	for _, p := range r.Problemes {
		tout += p.Fichier + ": " + p.Message + map[bool]string{true: " [grave]", false: ""}[p.Grave] + "\n"
	}
	attendus := []string{"SKILL.md: its header", "scripts/prep.sh: Windows line endings (CRLF): bash and the #! line fail on macOS and Linux [grave]",
		"scripts/lire.py: Python cannot read it", "scripts/prep.sh: “sed -i” without a suffix"}
	if trouverProgramme("python") == "" {
		attendus = append(attendus, "SKILL.md: it runs “python …”: this computer has python3, not python [grave]")
	}
	for _, x := range attendus {
		if !strings.Contains(tout, x) {
			t.Errorf("missing %q in\n%s", x, tout)
		}
	}
	if r.OK || r.Scripts != 3 || strings.Contains(tout, "ok.js") || strings.Contains(tout, "LISEZMOI") {
		t.Fatalf("%+v\n%s", r, tout)
	}
	if r := testerSkill(kit, "absent"); r.Erreur != "unknown skill" {
		t.Fatal(r)
	}
}

// Claude fixes a copy; it is tested and shown as a diff; applied, the skill before it is kept for Undo.
func TestCorrigerSkill(t *testing.T) {
	kit, d := kitSkillEssai(t)
	ancien := demanderClaudeDossier
	var dossierVu string
	demanderClaudeDossier = func(dir, consigne string, _ time.Duration) (string, error) {
		dossierVu = dir
		if !strings.Contains(consigne, "Windows line endings") || !strings.Contains(consigne, "Python cannot read it") {
			t.Errorf("the question: %s", consigne)
		}
		_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: outils-pdf\ndescription: Reads PDFs.\n---\n\nRun `python3 scripts/lire.py FILE` (python on Windows).\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "scripts/prep.sh"), []byte("#!/usr/bin/env bash\necho ok\nsed -i.bak 's/a/b/' x && rm -f x.bak\n"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "scripts/lire.py"), []byte("import sys\nprint(sys.argv[1])\n"), 0o644)
		return `Voilà. {"explication": "Fins de ligne et parenthèse corrigées.", "manque": "", "athanor": ""}`, nil
	}
	t.Cleanup(func() { demanderClaudeDossier = ancien })
	var etapes []string
	res := corrigerSkill(kit, "outils-pdf", func(q string, n int) { etapes = append(etapes, q) })
	if !res.OK || len(res.Fichiers) != 3 || res.Explication != "Fins de ligne et parenthèse corrigées." || len(res.Apres) != 0 || len(res.Avant) == 0 {
		t.Fatalf("%+v", res)
	}
	if dossierVu != propositionSkill(kit, "outils-pdf") || strings.Join(etapes, ",") != "test,claude,test" {
		t.Fatalf("%s %v", dossierVu, etapes)
	}
	if !strings.Contains(res.Diff, "--- a/scripts/lire.py\n+++ b/scripts/lire.py") || !strings.Contains(res.Diff, "-print(sys.argv[1]") || !strings.Contains(res.Diff, "+print(sys.argv[1])") || strings.Contains(res.Diff, kit) {
		t.Fatalf("diff:\n%s", res.Diff)
	}
	// nothing replaced before Apply
	if b, _ := os.ReadFile(filepath.Join(d, "scripts/lire.py")); !strings.Contains(string(b), "print(sys.argv[1]\n") {
		t.Fatal("replaced before Apply")
	}
	if err := appliquerSkill(kit, "outils-pdf"); err != nil {
		t.Fatal(err)
	}
	if r := testerSkill(kit, "outils-pdf"); !r.OK || !skillCorrige(kit, "outils-pdf") {
		t.Fatalf("after Apply: %+v", r)
	}
	if l := lireRapports(kit); len(l) != 1 || l[0].Genre != "skill" || len(l[0].Fichiers) != 3 || l[0].Source != "github.com/x/y" {
		t.Fatalf("report: %+v", l)
	}
	if err := appliquerSkill(kit, "outils-pdf"); err == nil {
		t.Fatal("applied twice")
	}
	if err := annulerSkill(kit, "outils-pdf"); err != nil {
		t.Fatal(err)
	}
	if r := testerSkill(kit, "outils-pdf"); r.OK || skillCorrige(kit, "outils-pdf") {
		t.Fatalf("after Undo: %+v", r)
	}
}
