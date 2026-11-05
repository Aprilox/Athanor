package main

// The window guard (Windows): Claude Code starts some programs without a console of their own (its MCP
// servers, some of its commands); each console program then opens a window of its own, a console or a
// Windows Terminal, on the user's screen. The guard watches the windows that appear and hides at once those
// of the programs Athanor started (directly or not), which go on running without them; what it hid is
// written in the pilot's log (logs/fenetres-masquees.log for the application's own). The decisions are
// here, on every system (tested anywhere); garde_fenetres_windows.go watches the windows.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The classes of the windows the guard looks at: a console of Windows' own (conhost), a Windows Terminal.
const (
	classeConsoleGarde  = "ConsoleWindowClass"
	classeTerminalGarde = "CASCADIA_HOSTING_WINDOW_CLASS"
)

// processusGarde: one process of the machine.
type processusGarde struct {
	pid, ppid uint32
	exe       string // its program (full path when known)
	cree      int64  // when it started (Windows' 100 ns units; 0: unknown)
}

func nomExeGarde(exe string) string {
	return strings.ToLower(filepath.Base(strings.ReplaceAll(exe, `\`, "/")))
}

// descendantsGarde: the processes started, directly or not, by one of racines. A parent counts only when
// it started before its child: a process number given again to another program is nobody's parent.
func descendantsGarde(procs []processusGarde, racines ...uint32) map[uint32]processusGarde {
	par := map[uint32]processusGarde{}
	enfants := map[uint32][]uint32{}
	for _, p := range procs {
		par[p.pid] = p
		if p.pid != p.ppid {
			enfants[p.ppid] = append(enfants[p.ppid], p.pid)
		}
	}
	out := map[uint32]processusGarde{}
	var file []uint32
	for _, r := range racines {
		if _, ok := par[r]; ok {
			file = append(file, r)
		}
	}
	for len(file) > 0 {
		x := file[0]
		file = file[1:]
		for _, e := range enfants[x] {
			if _, vu := out[e]; vu {
				continue
			}
			pe, px := par[e], par[x]
			if pe.cree != 0 && px.cree != 0 && pe.cree < px.cree {
				continue
			}
			out[e] = pe
			file = append(file, e)
		}
	}
	for _, r := range racines {
		delete(out, r)
	}
	return out
}

// reCheminTitre: a window title that starts with a program's path (a console's title before the program
// changes it: "C:\Windows\System32\cmd.exe - npx …").
var reCheminTitre = regexp.MustCompile(`(?i)^(?:[^:\\]{0,40}: )?([a-z]:\\[^"<>|?*]*?\.exe)\b`)

// fenetreAMasquerGarde: whether a window that just appeared is one of ours to hide, and the program to name.
// A console: the process Windows names for it (the program, or its console host) is one of ours. A Windows
// Terminal (a console handed over to it): its title is the path of a program of ours that started in the
// last `recent` units. Never Athanor's own nor a program racine started itself: the windows it opens on
// purpose (installation, sign-in: Athanor.exe or wsl.exe in a console of their own); everything else it starts
// has no window.
func fenetreAMasquerGarde(racine uint32, classe string, pid uint32, titre string, nous map[uint32]processusGarde, maintenant, recent int64) (processusGarde, bool) {
	voulue := func(p processusGarde) bool { return p.ppid == racine || nomExeGarde(p.exe) == "athanor.exe" }
	switch classe {
	case classeConsoleGarde:
		p, ok := nous[pid]
		if !ok {
			return p, false
		}
		if n := nomExeGarde(p.exe); n == "conhost.exe" || n == "openconsole.exe" {
			if parent, ok := nous[p.ppid]; ok {
				p = parent
			} else {
				return p, false // the console of a program racine started itself
			}
		}
		return p, !voulue(p)
	case classeTerminalGarde:
		m := reCheminTitre.FindStringSubmatch(strings.TrimSpace(titre))
		if m == nil {
			return processusGarde{}, false
		}
		for _, p := range nous {
			if strings.EqualFold(filepath.Clean(p.exe), filepath.Clean(m[1])) && !voulue(p) &&
				(p.cree == 0 || maintenant-p.cree <= recent) {
				return p, true
			}
		}
	}
	return processusGarde{}, false
}

// texteFenetreGarde: the line written for a hidden window.
func texteFenetreGarde(p processusGarde, nous map[uint32]processusGarde) string {
	t := "Windows: a window of " + filepath.Base(strings.ReplaceAll(p.exe, `\`, "/"))
	if parent, ok := nous[p.ppid]; ok {
		t += " (started by " + filepath.Base(strings.ReplaceAll(parent.exe, `\`, "/")) + ")"
	}
	return t + " opened on your screen: hidden, it goes on without it."
}

// noterFenetreKit: the application's own hidden windows, in logs/fenetres-masquees.log (the last 500 lines).
func noterFenetreKit(kit, texte string) {
	if kit == "" {
		return
	}
	f := filepath.Join(kit, "logs", "fenetres-masquees.log")
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	b, _ := os.ReadFile(f)
	lignes := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lignes) >= 500 {
		lignes = lignes[len(lignes)-499:]
	}
	if len(b) == 0 {
		lignes = nil
	}
	lignes = append(lignes, time.Now().Format("2006-01-02 15:04:05")+" "+texte)
	_ = os.WriteFile(f, []byte(strings.Join(lignes, "\n")+"\n"), 0o644)
}
