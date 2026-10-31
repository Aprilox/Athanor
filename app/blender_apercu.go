package main

// How Claude works with Blender, a choice on the Blender server's line (Skills & MCP), for every project
// that switches that server on:
//   - "apercu" (the default): Claude works in the background (blender -b -P script.py: no window, no MCP
//     tools in its session, the cheapest), and every run is shown to the user: the gateway saves the
//     resulting scene as .athanor/blender/athanor-preview.blend of the folder Claude works in, and a Blender
//     window opens on it (on Claude's desktop when that option is on), reloading it by itself after each
//     run. Each folder has its window: workers in parallel are each shown in their own.
//   - "arriere-plan": the background only, no window at all.
//   - "direct": as before: the pilot opens Blender before each cycle and Claude drives it through the MCP.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	modeBlenderApercu      = "apercu"
	modeBlenderArrierePlan = "arriere-plan"
	modeBlenderDirect      = "direct"
)

// estEntreeBlender: the library's entry is the verified catalog's Blender server.
func estEntreeBlender(sid string, def objetJSON) bool {
	c, _ := def.brut("catalogue").(string)
	return c == "blender" || (c == "" && sid == "blender")
}

// modeBlenderDe: the entry's mode (the default: the preview).
func modeBlenderDe(def objetJSON) string {
	m, _ := def.brut("mode_blender").(string)
	return modeBlenderEffectif(m)
}

// apercuBlenderPossible: the preview window needs the "blender" gateway (Windows, WSL, Linux); on a Mac,
// Claude runs Blender itself: the preview mode is the MCP's, as before.
var apercuBlenderPossible = func() bool { return runtime.GOOS != "darwin" }

// modeBlenderEffectif: the mode as this computer runs it.
func modeBlenderEffectif(m string) string {
	switch m {
	case modeBlenderArrierePlan, modeBlenderDirect:
		return m
	}
	if !apercuBlenderPossible() {
		return modeBlenderDirect
	}
	return modeBlenderApercu
}

// modeBlenderProjet: the mode of the Blender server switched on for this project, "" when it is not on.
func modeBlenderProjet(kit, projet string) string {
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
	for _, sid := range choixDuProjetKit(kit, projet)["mcp"] {
		if biblio.a(sid) && estEntreeBlender(sid, biblio.objet(sid)) {
			return modeBlenderDe(biblio.objet(sid))
		}
	}
	return ""
}

// consigneModeBlender: what Claude is told when it works in the background (the MCP server left out).
func consigneModeBlender(mode string) string {
	if mode == modeBlenderArrierePlan {
		return "Blender is used in the background only in this project: `blender -b -P script.py` (no window, and no Blender MCP tools this session). Keep the Python that builds each model as .py files of the project."
	}
	return "Blender is used in the background in this project: `blender -b -P script.py` (no Blender MCP tools this session). After each run, Athanor shows the resulting scene to the user in a Blender window it opens and reloads by itself: never open Blender's window yourself, and never wait for it. Keep the Python that builds each model as .py files of the project."
}

// racineTravail: the folder Claude works in (its git root or workshop, inside the project), else the project.
func racineTravail(projet string) string {
	d, err := os.Getwd()
	if err != nil {
		return projet
	}
	d, _ = filepath.Abs(d)
	p, _ := filepath.Abs(projet)
	for x := d; strings.HasPrefix(x+string(filepath.Separator), p+string(filepath.Separator)); x = filepath.Dir(x) {
		if existeP(filepath.Join(x, ".git")) || dossierP(filepath.Join(x, ".athanor")) {
			return x
		}
		if x == filepath.Dir(x) {
			break
		}
	}
	return p
}

// pyChaine: a Python string literal (JSON's escapes are Python's).
func pyChaine(s string) string { b, _ := json.Marshal(s); return string(b) }

// scriptApercuBlender: the script Blender runs in place of Claude's: Claude's script as it is, then the
// scene saved as the preview (even after an error: what was built is shown), then Claude's error again.
func scriptApercuBlender(script, apercu string) string {
	return "# Athanor: runs " + filepath.Base(script) + ", then saves the scene for the preview window\n" +
		"import os, runpy, sys\nimport bpy\n_script = " + pyChaine(script) + "\n_apercu = " + pyChaine(apercu) + "\n_erreur = None\n" +
		"try:\n    runpy.run_path(_script, run_name=\"__main__\")\nexcept BaseException as e:\n    _erreur = e\n" +
		"try:\n    os.makedirs(os.path.dirname(_apercu), exist_ok=True)\n    _tmp = _apercu[:-6] + \".tmp.blend\"\n" +
		"    bpy.ops.wm.save_as_mainfile(filepath=_tmp, copy=True, check_existing=False)\n    os.replace(_tmp, _apercu)\n" +
		"except Exception as e:\n    print(\"Athanor: preview not saved:\", e, file=sys.stderr)\n" +
		"if _erreur is not None:\n    raise _erreur\n"
}

// scriptRechargeur: run by the preview window: reloads its file when a run changed it, and says it is open
// (FILE.vu, touched every 2 s).
const scriptRechargeur = `# Athanor: the preview window reloads its file after each run of Claude's
import os, time
import bpy

def _athanor_recharger():
    f = bpy.data.filepath
    if not f:
        return 2.0
    try:
        with open(f + ".vu", "w") as h:
            h.write(str(time.time()))
    except OSError:
        pass
    try:
        m = os.path.getmtime(f)
    except OSError:
        return 2.0
    vu = bpy.app.driver_namespace.get("athanor_mtime")
    bpy.app.driver_namespace["athanor_mtime"] = m
    if vu is not None and m != vu and time.time() - m > 1.0:
        try:
            bpy.ops.wm.open_mainfile(filepath=f, load_ui=False)
        except Exception as e:
            print("Athanor: preview not reloaded:", e)
    elif vu is not None and m != vu:
        bpy.app.driver_namespace["athanor_mtime"] = vu  # still being written: next time
    return 2.0

if not bpy.app.driver_namespace.get("athanor_rechargeur"):
    bpy.app.driver_namespace["athanor_rechargeur"] = True
    bpy.app.timers.register(_athanor_recharger, first_interval=2.0, persistent=True)
`

// apercuBlender: a headless run of Claude's prepared for the preview: the arguments to run, the preview
// file and the script to remove afterwards ("" when the run is left as it is).
type apercuBlender struct {
	args            []string
	fichier, lancer string
}

func (p *passerelle) preparerApercuBlender(args []string, versBlender func(string) string) apercuBlender {
	r := apercuBlender{args: args}
	if modeBlenderProjet(p.kit, p.projet) != modeBlenderApercu {
		return r
	}
	fond, i := false, -1
	for j, a := range args {
		if a == "-b" || a == "--background" {
			fond = true
		}
		if (a == "-P" || a == "--python") && j+1 < len(args) {
			i = j + 1
		}
		if a == "--" {
			break
		}
	}
	if !fond || i < 0 || !strings.HasSuffix(minuscules(args[i]), ".py") {
		return r
	}
	script := args[i]
	if !filepath.IsAbs(script) {
		if d, err := os.Getwd(); err == nil {
			script = filepath.Join(d, script)
		}
	}
	dossier := filepath.Join(racineTravail(p.projet), ".athanor", "blender")
	if os.MkdirAll(dossier, 0o755) != nil {
		return r
	}
	fichier := filepath.Join(dossier, "athanor-preview.blend")
	lancer := filepath.Join(dossier, fmt.Sprintf(".lancer-%d.py", os.Getpid()))
	if os.WriteFile(lancer, []byte(scriptApercuBlender(versBlender(script), versBlender(fichier))), 0o644) != nil {
		return r
	}
	r.args = append(append(append([]string{}, args[:i]...), lancer), args[i+1:]...)
	r.fichier, r.lancer = fichier, lancer
	return r
}

// fenetreApercuOuverte: the preview window of that file answers (it touches FILE.vu every 2 s).
func fenetreApercuOuverte(fichier string) bool {
	st, err := os.Stat(fichier + ".vu")
	return err == nil && time.Since(st.ModTime()) < 8*time.Second
}

// montrerApercuBlender: after the run: the preview window opened on it, unless it is open already (it then
// reloads it by itself). ouvrir starts Blender on (file, reloading script).
func (p *passerelle) montrerApercuBlender(a apercuBlender, ouvrir func(fichier, rechargeur string) error) {
	if a.lancer != "" {
		_ = os.Remove(a.lancer)
	}
	if a.fichier == "" {
		return
	}
	if st, err := os.Stat(a.fichier); err != nil || time.Since(st.ModTime()) > 10*time.Minute {
		return // nothing saved by this run
	}
	if fenetreApercuOuverte(a.fichier) {
		fmt.Fprintln(p.erreur, "Athanor: the preview window shows this run (it reloads by itself).")
		return
	}
	rechargeur := filepath.Join(filepath.Dir(a.fichier), "athanor-reload.py")
	if os.WriteFile(rechargeur, []byte(scriptRechargeur), 0o644) != nil {
		return
	}
	if err := ouvrir(a.fichier, rechargeur); err != nil {
		fmt.Fprintln(p.erreur, "Athanor: the preview window did not open: "+err.Error())
		return
	}
	fmt.Fprintln(p.erreur, "Athanor: this run is shown to the user in a Blender window (it reloads after each run; nothing to do).")
}

// ecrireModeBlender: the mode written into the library's Blender entry.
func ecrireModeBlender(k Kit, id, mode string) error {
	b := lireBiblio(k)
	e, ok := b.MCP[id]
	if !ok || !(e.Catalogue == "blender" || (e.Catalogue == "" && id == "blender")) {
		return errors.New("not Blender's server")
	}
	e.ModeBlender = mode
	if mode == modeBlenderApercu {
		e.ModeBlender = ""
	}
	b.MCP[id] = e
	return ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
}
