package main

// Native Windows mode, the parts shared by every system: its health checks, and the question asked when
// the application closes while native pilots run (natif_windows.go has the Windows side).

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// The answer given in the application's own dialog when its window was closed (fermeture_windows.go):
// 0 not asked there (Windows' dialog asks then), 1 the native pilots go on, 2 they stop.
var decisionFermeture atomic.Int32

// pilotesAvantFermeture: the names of the native pilots running (set by main, asked when the window closes).
var pilotesAvantFermeture = func() []string { return nil }

// VerificationsNatives: the checks of the native Windows mode (Git Bash, the Windows programs the pilot
// needs, Claude Code for Windows, the token, the kit's scripts).
func VerificationsNatives() []*Verif {
	v := func(id, titre, explication, script string, facultatif bool, rep Reparation) *Verif {
		return &Verif{ID: id, Titre: titre, Explication: explication, Script: script, Facultatif: facultatif, Reparation: rep, tache: NouvelleTache("natif-"+id, titre)}
	}
	installer := Reparation{Genre: "powershell", Script: `commands\install-natif.ps1`, Explication: "Opens a window that installs what is missing with winget (Git for Windows, jq, Python, Node.js, GitHub CLI) and Claude Code for Windows. Windows may ask for your consent."}
	return []*Verif{
		v("git_bash", "Git for Windows (Git Bash)", "In native Windows mode, the pilot and Claude's commands run in Git Bash, the shell Claude Code uses on Windows.",
			`echo "Git Bash OK: bash $BASH_VERSION ($(uname -s))"`, false, installer),
		v("outils", "Programs the pilot needs", "git, jq and curl for Windows; Python and Node.js for the projects that use them, GitHub CLI to link a project to GitHub, uv for the MCP servers written in Python.",
			`export PATH="$PWD/system/natif:$PATH"; m=0; for p in git jq curl python node gh uv; do if v=$($p --version 2>&1 | head -n 1) && [ -n "$v" ]; then echo "ok  $p: $v"; else case $p in python|node|gh|uv) echo "MISSING $p (optional)" ;; *) echo "MISSING $p"; m=1 ;; esac; fi; done; exit $m`, false, installer),
		v("claude", "Claude Code for Windows", "The claude.exe program (%USERPROFILE%\\.local\\bin).",
			`export PATH="$HOME/.local/bin:$PATH"; v=$(claude --version 2>&1 | head -n 1); echo "$v"
n=$(printf '%s' "$v" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n 1); [ -n "$n" ] || exit 1
# older than what the pilot needs (instructions and team through files): an update is due
[ "$(printf '%s\n2.1.281\n' "$n" | sort -V | head -n 1)" = 2.1.281 ] || { echo "Claude Code $n is too OLD for the native mode (2.1.281 at least, and the latest models): Install everything updates it"; exit 1; }`, false, installer),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.",
			scriptJeton, false, reparationJeton),
		v("outils_pc", "PC software detected", "Unreal, Blender, Android SDK, Docker… found on this PC and made available to Claude.",
			`export PROJECT="$PWD/logs/detection"; mkdir -p "$PROJECT" tools; `+outilKit("detecter")+`"${X[@]}" >/dev/null 2>&1; sed -n '3,25p' "$PROJECT/.athanor/PC_TOOLS.md"`, true, Reparation{Genre: "aucune"}),
		v("outils_kit", "Tool scripts", "The pilot scripts are consistent and keep Linux line ends (Git Bash fails on CRLF).",
			`for f in system/pilot.sh system/parallel.sh system/safety.sh commands/*.sh system/natif/*; do bash -n "$f" || { echo "Syntax ERROR: $f"; exit 1; }; done
./Athanor.exe autotest "$PWD" >/dev/null 2>&1 || { echo 'Athanor.exe ERROR: its autotest fails (Rebuild Athanor)'; exit 1; }
if grep -lq $'\r' system/*.sh system/natif/* commands/*.sh 2>/dev/null; then echo 'ERROR: Windows line endings (CRLF) in a script'; exit 1; fi
echo 'scripts OK'`, false, Reparation{Genre: "aucune"}),
	}
}

// fermetureNative: the window was closed while pilots run in native Windows mode. They can go on in the
// background (they were started outside the application's job) or stop now: the user says.
func (s *Serveur) fermetureNative() {
	actifs, noms := s.pilotesNatifs()
	if len(actifs) == 0 {
		return
	}
	switch decisionFermeture.Load() {
	case 1:
		return
	case 2:
		for _, p := range actifs {
			_, _ = s.scriptProjet(p, "stop.sh")
		}
		return
	}
	s.mu.Lock()
	langue := s.config.Langue
	s.mu.Unlock()
	if questionOuiNon(libelle(langue, "natif_titre"), fmt.Sprintf(libelle(langue, "natif_texte"), strings.Join(noms, ", "))) {
		return
	}
	for _, p := range actifs {
		_, _ = s.scriptProjet(p, "stop.sh")
	}
}

// pilotesNatifs: the projects whose pilot runs in native Windows mode, and their names.
func (s *Serveur) pilotesNatifs() ([]Projet, []string) {
	if !modeNatif.Load() {
		return nil, nil
	}
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	var actifs []Projet
	var noms []string
	for _, p := range projets {
		if p.PiloteActif() {
			actifs = append(actifs, p)
			noms = append(noms, p.Nom)
		}
	}
	return actifs, noms
}
