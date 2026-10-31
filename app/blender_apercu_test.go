//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// a kit whose library has Blender's server, switched on for the project (mode: as given)
func kitBlenderEssai(t *testing.T, mode string, actif bool) (kit, projet string) {
	t.Helper()
	// the preview's logic, tested on every system (a Mac runs the MCP's window instead: TestModeBlenderMac)
	ancien := apercuBlenderPossible
	apercuBlenderPossible = func() bool { return true }
	t.Cleanup(func() { apercuBlenderPossible = ancien })
	d, _ := filepath.EvalSymlinks(t.TempDir())
	kit, projet = filepath.Join(d, "kit"), filepath.Join(d, "projet")
	_ = os.MkdirAll(filepath.Join(projet, ".git"), 0o755)
	e := map[string]any{"nom": "Blender", "catalogue": "blender", "config": map[string]any{"type": "stdio", "command": "uvx", "args": []any{"blender-mcp"}}}
	if mode != "" {
		e["mode_blender"] = mode
	}
	b, _ := json.Marshal(map[string]any{"mcp": map[string]any{"blender": e, "autre": map[string]any{"config": map[string]any{"url": "https://x.org/mcp"}}}})
	ecrireFichier(t, filepath.Join(kit, "extensions/bibliotheque.json"), string(b), 0o644)
	ids := []string{"autre"}
	if actif {
		ids = append(ids, "blender")
	}
	c, _ := json.Marshal(map[string]any{projet: map[string]any{"skills": []string{}, "mcp": ids}})
	ecrireFichier(t, filepath.Join(kit, "extensions/projets.json"), string(c), 0o644)
	return kit, projet
}

func allerDans(t *testing.T, d string) {
	t.Helper()
	ancien, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(ancien) })
}

// A headless run with a script: Claude's script run by Athanor's, which saves the preview; anything else
// (another mode, the server off, no script, no -b) left as it is.
func TestPreparerApercuBlender(t *testing.T) {
	kit, projet := kitBlenderEssai(t, "", true)
	travail := filepath.Join(projet, "ouvrier")
	_ = os.MkdirAll(filepath.Join(travail, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(travail, "outils"), 0o755)
	allerDans(t, filepath.Join(travail, "outils"))
	var so, se bytes.Buffer
	p := &passerelle{kit: kit, projet: projet, sortie: &so, erreur: &se}
	args := []string{"-b", "scene.blend", "-P", "grue.py", "--", "-P", "x.py"}
	a := p.preparerApercuBlender(args, func(c string) string { return "W:" + c })
	attendu := filepath.Join(travail, ".athanor", "blender", "athanor-preview.blend")
	if a.fichier != attendu || a.lancer == "" || a.args[3] != a.lancer || strings.Join(a.args[4:], " ") != "-- -P x.py" || strings.Join(a.args[:3], " ") != "-b scene.blend -P" {
		t.Fatalf("%+v", a)
	}
	s := lire(t, a.lancer)
	if !strings.Contains(s, `_script = "W:`+filepath.Join(travail, "outils", "grue.py")+`"`) || !strings.Contains(s, `_apercu = "W:`+attendu+`"`) || !strings.Contains(s, "runpy.run_path(_script") {
		t.Fatalf("the script:\n%s", s)
	}
	for _, x := range [][]string{{"-P", "grue.py"}, {"-b", "scene.blend", "--render-frame", "1"}, {"-b", "-P", "notes.txt"}, {"-b", "--", "-P", "x.py"}} {
		if r := p.preparerApercuBlender(x, func(c string) string { return c }); r.fichier != "" || strings.Join(r.args, " ") != strings.Join(x, " ") {
			t.Errorf("%v changed: %+v", x, r)
		}
	}
	for _, c := range []struct {
		mode  string
		actif bool
	}{{"arriere-plan", true}, {"direct", true}, {"", false}} {
		k, pr := kitBlenderEssai(t, c.mode, c.actif)
		q := &passerelle{kit: k, projet: pr, sortie: &so, erreur: &se}
		if r := q.preparerApercuBlender(args, func(c string) string { return c }); r.fichier != "" {
			t.Errorf("%+v: %+v", c, r)
		}
	}
}

// After the run: the window opened once (it reloads by itself afterwards); nothing when nothing was saved.
func TestMontrerApercuBlender(t *testing.T) {
	d := t.TempDir()
	var se bytes.Buffer
	p := &passerelle{kit: d, projet: d, sortie: &se, erreur: &se}
	fichier := filepath.Join(d, ".athanor", "blender", "athanor-preview.blend")
	lancer := filepath.Join(d, ".athanor", "blender", ".lancer-1.py")
	ecrireFichier(t, lancer, "x", 0o644)
	var ouverts []string
	ouvrir := func(f, r string) error { ouverts = append(ouverts, f+"|"+r); return nil }
	p.montrerApercuBlender(apercuBlender{fichier: fichier, lancer: lancer}, ouvrir)
	if len(ouverts) != 0 || existeP(lancer) {
		t.Fatalf("nothing saved: %v", ouverts)
	}
	ecrireFichier(t, fichier, "blend", 0o644)
	p.montrerApercuBlender(apercuBlender{fichier: fichier}, ouvrir)
	r := filepath.Join(d, ".athanor", "blender", "athanor-reload.py")
	if len(ouverts) != 1 || ouverts[0] != fichier+"|"+r || !strings.Contains(lire(t, r), "bpy.ops.wm.open_mainfile") || !strings.Contains(se.String(), "shown to the user") {
		t.Fatalf("%v %q", ouverts, se.String())
	}
	ecrireFichier(t, fichier+".vu", "1", 0o644) // the window answers
	p.montrerApercuBlender(apercuBlender{fichier: fichier}, ouvrir)
	if len(ouverts) != 1 {
		t.Fatal("opened twice")
	}
	vieux := time.Now().Add(-time.Minute)
	_ = os.Chtimes(fichier+".vu", vieux, vieux) // closed by the user: opened again
	p.montrerApercuBlender(apercuBlender{fichier: fichier}, ouvrir)
	if len(ouverts) != 2 {
		t.Fatal("not opened again")
	}
}

// In the background, the session has no Blender MCP server, and Claude is told how to work.
func TestConstruireMCPBlender(t *testing.T) {
	for _, mode := range []string{"", "arriere-plan", "direct"} {
		kit, projet := kitBlenderEssai(t, mode, true)
		f := filepath.Join(t.TempDir(), "mcp.json")
		ids, notes := construireMCP(kit, choixDuProjetKit(kit, projet), f, projet, "")
		aide, _ := os.ReadFile(f + ".sauvegarde.md")
		switch mode {
		case "direct":
			if strings.Contains(strings.Join(notes, ";"), "in the background") || strings.Contains(string(aide), "blender -b -P") {
				t.Errorf("direct: %v %v %s", ids, notes, aide)
			}
		default:
			if strings.Join(ids, ",") != "autre" || !strings.Contains(string(aide), "blender -b -P script.py") || !strings.Contains(strings.Join(notes, ";"), "blender in the background") {
				t.Errorf("%q: %v %v %s", mode, ids, notes, aide)
			}
			if (mode == "") != strings.Contains(string(aide), "never open Blender's window yourself") {
				t.Errorf("%q: %s", mode, aide)
			}
		}
	}
	kit, _ := kitBlenderEssai(t, "", true)
	k := Kit{Racine: kit}
	if err := ecrireModeBlender(k, "blender", "arriere-plan"); err != nil || lireBiblio(k).MCP["blender"].ModeBlender != "arriere-plan" {
		t.Fatal(err)
	}
	if err := ecrireModeBlender(k, "blender", "apercu"); err != nil || lireBiblio(k).MCP["blender"].ModeBlender != "" {
		t.Fatal(err)
	}
	if err := ecrireModeBlender(k, "autre", "apercu"); err == nil {
		t.Fatal("another server")
	}
}

// With a real Blender (when there is one): the run keeps its arguments, the preview is saved.
func TestApercuBlenderReel(t *testing.T) {
	blender, err := exec.LookPath("blender")
	if err != nil {
		t.Skip("no Blender")
	}
	d := t.TempDir()
	ecrireFichier(t, filepath.Join(d, "caisse.py"), "import bpy, sys\nbpy.ops.wm.read_factory_settings(use_empty=True)\nbpy.ops.mesh.primitive_cube_add()\nbpy.context.active_object.name = 'Caisse'\nprint('ARGS', sys.argv[sys.argv.index('--')+1:])\n", 0o644)
	apercu := filepath.Join(d, ".athanor", "blender", "athanor-preview.blend")
	lancer := filepath.Join(d, "lancer.py")
	ecrireFichier(t, lancer, scriptApercuBlender(filepath.Join(d, "caisse.py"), apercu), 0o644)
	sonde := filepath.Join(d, "sonde.py")
	ecrireFichier(t, sonde, "import bpy\nprint('OBJETS', sorted(o.name for o in bpy.data.objects))\n", 0o644)
	out, err := exec.Command(blender, "-b", "-P", lancer, "--", "un", "deux").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ARGS ['un', 'deux']") || !existeP(apercu) {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _ = exec.Command(blender, "-b", apercu, "-P", sonde).CombinedOutput()
	if !strings.Contains(string(out), "OBJETS ['Caisse']") {
		t.Fatalf("the preview:\n%s", out)
	}
	// an error in Claude's script: what was built is still shown, and the error is still Blender's
	ecrireFichier(t, filepath.Join(d, "caisse.py"), "import bpy\nbpy.ops.wm.read_factory_settings(use_empty=True)\nbpy.ops.mesh.primitive_uv_sphere_add()\nbpy.context.active_object.name = 'Boule'\nraise RuntimeError('pas fini')\n", 0o644)
	out, _ = exec.Command(blender, "-b", "--python-exit-code", "5", "-P", lancer).CombinedOutput()
	if !strings.Contains(string(out), "pas fini") {
		t.Fatalf("%s", out)
	}
	out, _ = exec.Command(blender, "-b", apercu, "-P", sonde).CombinedOutput()
	if !strings.Contains(string(out), "OBJETS ['Boule']") {
		t.Fatalf("after an error:\n%s", out)
	}
}

// On a Mac (no gateway: Claude runs Blender itself), the preview is the MCP's window, as before.
func TestModeBlenderMac(t *testing.T) {
	kit, projet := kitBlenderEssai(t, "", true)
	apercuBlenderPossible = func() bool { return false } // put back by kitBlenderEssai's cleanup
	if modeBlenderEffectif("") != modeBlenderDirect || modeBlenderEffectif("arriere-plan") != modeBlenderArrierePlan {
		t.Fatal("mac")
	}
	_, notes := construireMCP(kit, choixDuProjetKit(kit, projet), filepath.Join(t.TempDir(), "mcp.json"), projet, "")
	if strings.Contains(strings.Join(notes, ";"), "in the background") {
		t.Fatalf("%v", notes)
	}
}
