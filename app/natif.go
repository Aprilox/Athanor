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

// VerificationsNatives: the checks of the native Windows mode (Git for Windows, the Windows programs the
// pilot needs, Claude Code for Windows, the token, Athanor's own files).
func VerificationsNatives() []*Verif {
	v := func(id, titre, explication string, facultatif bool, rep Reparation) *Verif {
		return nouvelleVerif("natif-", id, titre, explication, facultatif, rep)
	}
	installer := Reparation{Genre: "windows", Titre: "Installation", Args: []string{"installer-natif"}, Explication: "Opens a window that installs what is missing with winget (Git for Windows, jq, Python, Node.js, GitHub CLI) and Claude Code for Windows. Windows may ask for your consent."}
	return []*Verif{
		v("git_bash", "Git for Windows", "git, for the milestones' commits and the parallel workers; Claude Code also runs its commands in its Git Bash (without it, in PowerShell).", false, installer),
		v("outils", "Programs the pilot needs", "git and curl for Windows; jq, Python and Node.js for the projects that use them, GitHub CLI to link a project to GitHub, uv for the MCP servers written in Python.", false, installer),
		v("claude", "Claude Code for Windows", "The claude.exe program (%USERPROFILE%\\.local\\bin).", false, installer),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.", false, reparationJeton),
		v("outils_pc", "PC software detected", "Unreal, Blender, Android SDK, Docker… found on this PC and made available to Claude.", true, Reparation{Genre: "aucune"}),
		v("outils_kit", "Athanor's files", "Athanor's executable passes its self-test and its texts for Claude (template/) are in place.", false, Reparation{Genre: "aucune"}),
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
			_, _ = s.arreterPiloteProjet(p)
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
		_, _ = s.arreterPiloteProjet(p)
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
