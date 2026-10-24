package main

// Claude's desktop, quiet (bureau_calme_windows.go): the decisions, kept apart to be tested on every system.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sourdineVoulue: a program of Claude's desktop is muted while a pilot runs, unless the user chose to
// hear Claude's desktop or is looking at it.
func sourdineVoulue(entendre, regarde, surBureauClaude bool) bool {
	return surBureauClaude && !entendre && !regarde
}

// descendants: the processes started (directly or not) by those of depart, depart included; parents maps
// a process to its parent. A process number given again to another program can form a loop: each one is
// visited once.
func descendants(depart map[uint32]bool, parents map[uint32]uint32) map[uint32]bool {
	r := map[uint32]bool{}
	for p := range depart {
		r[p] = true
	}
	for ajoute := true; ajoute; {
		ajoute = false
		for enfant, parent := range parents {
			if !r[enfant] && r[parent] && enfant != parent && enfant != 0 {
				r[enfant] = true
				ajoute = true
			}
		}
	}
	return r
}

// fichierSourdine: the programs Athanor muted (logs/sourdine.txt, one executable per line), so that an
// Athanor stopped before giving the sound back (killed, computer turned off) is made good by the next one.
func fichierSourdine(kit string) string { return filepath.Join(kit, "logs", "sourdine.txt") }

func lireSourdine(kit string) map[string]bool {
	r := map[string]bool{}
	b, err := os.ReadFile(fichierSourdine(kit))
	if err != nil {
		return r
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			r[strings.ToLower(l)] = true
		}
	}
	return r
}

func ecrireSourdine(kit string, exes map[string]bool) {
	if kit == "" {
		return
	}
	if len(exes) == 0 {
		_ = os.Remove(fichierSourdine(kit))
		return
	}
	l := make([]string, 0, len(exes))
	for e := range exes {
		l = append(l, e)
	}
	sort.Strings(l)
	_ = os.WriteFile(fichierSourdine(kit), []byte(strings.Join(l, "\n")+"\n"), 0o644)
}
