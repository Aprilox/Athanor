package main

// What a running pilot has really taken, against what the user set since.
//
// The pilot reads the tool's settings (.env) once, when it starts, and the project's working modes when a
// cycle starts. It writes what it read into STATE.json ("reglages", "modes"); the app compares that with the
// files as they are now. A change is therefore told "taken" only once the pilot itself says it read it: one
// made at the last moment, while a pilot starts or restarts, stays "waiting" and is applied at the next turn.
//   - working modes waiting: the pilot reads them at its next cycle, nothing to do;
//   - settings waiting: the pilot must start again, so the app asks it to restart at the end of its cycle
//     (relancerPourReglages), once per pilot and per state of the settings.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// empreinteReglages: the settings of a .env as a pilot takes them when it starts, in a few characters. The
// connection is left out: the pilot reads it again at every cycle (relireConnexion). Comments, empty lines
// and the order of the lines do not count.
func empreinteReglages(texteEnv string) string {
	relues := map[string]bool{}
	for _, k := range clesConnexionClaude {
		relues[k] = true
	}
	var lignes []string
	for _, l := range strings.Split(texteEnv, "\n") {
		l = strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		if m := reCleEnvPilote.FindStringSubmatch(l); m != nil && !relues[m[1]] {
			lignes = append(lignes, m[1]+"="+strings.TrimSpace(m[2]))
		}
	}
	sort.Strings(lignes)
	h := sha256.Sum256([]byte(strings.Join(lignes, "\n")))
	return hex.EncodeToString(h[:6])
}

// empreinteReglagesKit: empreinteReglages of the tool folder's .env as it is now.
func empreinteReglagesKit(kit string) string {
	b, _ := os.ReadFile(filepath.Join(kit, ".env"))
	return empreinteReglages(string(b))
}

// modesDemandes: the working modes a project's .athanor/.env asks for, as the pilot reads them when a cycle
// starts (lireModeParallele): what is asked, before the pilot's own reasons to work otherwise for a cycle.
func modesDemandes(texteEnv string) string {
	par, maxi, eco, rapide := "0", "3", "0", "0"
	un := func(v string) string {
		if v == "1" {
			return "1"
		}
		return "0"
	}
	for _, l := range strings.Split(texteEnv, "\n") {
		l = strings.TrimSuffix(l, "\r")
		switch {
		case strings.HasPrefix(l, "ECO="):
			eco = un(l[len("ECO="):])
		case strings.HasPrefix(l, "FAST="):
			rapide = un(l[len("FAST="):])
		case strings.HasPrefix(l, "PARALLEL="):
			par = un(l[len("PARALLEL="):])
		case strings.HasPrefix(l, "PARALLELE="):
			par = un(l[len("PARALLELE="):])
		case strings.HasPrefix(l, "PARALLEL_MAX="):
			maxi = l[len("PARALLEL_MAX="):]
		case strings.HasPrefix(l, "PARALLELE_MAX="):
			maxi = l[len("PARALLELE_MAX="):]
		}
	}
	if !reParalleleMaxPilote.MatchString(maxi) {
		maxi = "3"
	}
	if par == "0" {
		maxi = "-" // the number of workers only counts with the parallel mode
	}
	return fmt.Sprintf("parallel=%s max=%s eco=%s fast=%s", par, maxi, eco, rapide)
}

// changementsEnAttente: what the user changed that the project's running pilot has not taken yet: "modes"
// (taken at its next cycle) and "reglages" (taken when it restarts: asked here, for the end of its cycle).
// Nothing for a stopped pilot, for one that says nothing of what it read (older than 2.52.1), nor while the
// state file is still the one of the pilot before.
func (s *Serveur) changementsEnAttente(p Projet, actif bool) []string {
	if !actif {
		return nil
	}
	e, ok := p.LireEtat()
	if !ok || e.Pid == 0 {
		return nil
	}
	if b, err := os.ReadFile(filepath.Join(p.Atelier(), "pilot.pid")); err != nil || strings.TrimSpace(string(b)) != fmt.Sprint(e.Pid) {
		return nil
	}
	var r []string
	if e.Modes != "" {
		b, _ := os.ReadFile(p.envProjet())
		if modesDemandes(string(b)) != e.Modes {
			r = append(r, "modes")
		}
	}
	if e.Reglages != "" {
		if courante := empreinteReglagesKit(s.kitCourant().Racine); courante != e.Reglages {
			r = append(r, "reglages")
			s.relancerPourReglages(p, e.Pid, courante)
		}
	}
	return r
}

// relancerPourReglages: the pilot of that process runs with settings older than the current ones: it is
// asked, once, to restart at the end of its cycle. Once per pilot and per state of the settings: a restart
// the user cancels is not asked again, and a pilot that restarted without the latest change (saved while it
// started) is asked again. A stop already asked, by the user or by this, is left as it is.
func (s *Serveur) relancerPourReglages(p Projet, pid int64, courante string) {
	cle := fmt.Sprint(pid, "|", courante)
	s.mu.Lock()
	deja := s.relanceReglages[p.Nom] == cle
	s.relanceReglages[p.Nom] = cle
	demander := !deja && !fichierExiste(filepath.Join(p.Atelier(), "STOP"))
	if demander {
		s.relancer[p.Nom] = true
	}
	s.mu.Unlock()
	if demander {
		go func() {
			if _, err := s.demanderArretApresCycle(p); err != nil {
				// not asked again by itself (no loop on a failing command): the page shows the settings
				// waiting, with its "restart after cycle" button
				s.mu.Lock()
				delete(s.relancer, p.Nom)
				s.mu.Unlock()
			}
		}()
	}
}

// verifierReglagesPilotes: right after the settings are saved, every running pilot is looked at (the pages
// also do it each time they refresh).
func (s *Serveur) verifierReglagesPilotes() {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	for _, p := range projets {
		if !p.Archive && p.Existe() {
			s.changementsEnAttente(p, p.PiloteActif())
		}
	}
}
