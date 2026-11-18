package main

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"
)

// Auto-lock (note 12h28 §3, note 19h03 §3): the computer's window locks itself after the owner's inactivity. Only
// its session goes: the vault and its infra keys, pilots, missions, tasks, tunnels and SSH sessions are never
// touched. Remote devices keep their own unlock.

// ordreVerrouAuto: the choices, shortest first (a longer one asks for the password); 0 = no timer.
var ordreVerrouAuto = []string{"15m", "1h", "8h", "fermeture", "jamais"}

var delaisVerrouAuto = map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "8h": 8 * time.Hour}

// delaiVerrouAuto: the delay of a choice, 0 without a timer. The test harness shortens it with ATHANOR_VERROU_AUTO
// (a duration), only under ATHANOR_TEST=1 and only below the chosen delay: it can never keep a window open longer.
func delaiVerrouAuto(v string) time.Duration {
	d := delaisVerrouAuto[verrouAutoDe(v)]
	if c, err := time.ParseDuration(os.Getenv("ATHANOR_VERROU_AUTO")); err == nil && os.Getenv("ATHANOR_TEST") == "1" && c > 0 && c < d {
		return c
	}
	return d
}

// horlogeVerrou: the auto-lock's clock (tests: a simulated one).
var horlogeVerrou = time.Now

// verrouAutoDe: the setting as chosen, 1 hour by default (or when unknown).
func verrouAutoDe(v string) string {
	if slices.Contains(ordreVerrouAuto, v) {
		return v
	}
	return "1h"
}

// resteVerrouAuto: what is left before the window locks itself, -1 without a timer (no password, locked,
// "until Athanor closes" or "never": Athanor still starts locked, I2/I3). Called with s.mu held.
func (s *Serveur) resteVerrouAuto(t time.Time) time.Duration {
	d := delaiVerrouAuto(s.config.VerrouAuto)
	if d == 0 || s.config.MotDePasseHash == "" || s.session == "" {
		return -1
	}
	return max(0, s.actifLe.Add(d).Sub(t))
}

// verifierVerrouAuto locks the window once its delay passed; true when it did. Only the interface locks (note
// 19h03 §3): the vault and its infra keys stay as they are, so AIs, missions, tasks and SSH sessions go on; the
// padlock and the emergency stop still close everything.
func (s *Serveur) verifierVerrouAuto(t time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resteVerrouAuto(t) != 0 {
		return false
	}
	s.session, s.verrouAuto = "", true
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Action: "verrouillage automatique", Resultat: verrouAutoDe(s.config.VerrouAuto)})
	return true
}

// surveillerVerrouAuto: the check every 15 seconds while Athanor runs.
func (s *Serveur) surveillerVerrouAuto() {
	go func() {
		for range time.Tick(15 * time.Second) {
			s.verifierVerrouAuto(horlogeVerrou())
		}
	}()
}

// apiActivite: POST /api/activite, sent by the window on the owner's own actions (never by its refresh loop):
// the delay starts again. A remote device's call changes nothing (its unlock is its own).
func (s *Serveur) apiActivite(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if appareilDe(r) == "" && s.session != "" {
		s.actifLe = horlogeVerrou()
	}
	s.ecrireJSON(w, map[string]int{"verrou_dans": secondesReste(s.resteVerrouAuto(horlogeVerrou()))})
}

func secondesReste(d time.Duration) int {
	if d < 0 {
		return -1
	}
	return int((d + time.Second - 1) / time.Second)
}

// apiVerrouAuto: POST /api/options/verrou-auto {valeur, mot_de_passe}: a longer delay (or none) asks for
// Athanor's password, a shorter one nothing. The computer only, logged.
func (s *Serveur) apiVerrouAuto(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Valeur     string `json:"valeur"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil || !slices.Contains(ordreVerrouAuto, c.Valeur) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	s.mu.Lock()
	avant := verrouAutoDe(s.config.VerrouAuto)
	s.mu.Unlock()
	if slices.Index(ordreVerrouAuto, c.Valeur) > slices.Index(ordreVerrouAuto, avant) {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "verrouillage automatique refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	s.mu.Lock()
	s.config.VerrouAuto = c.Valeur
	s.actifLe = horlogeVerrou() // the new delay counts from now
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "verrou_auto", Resultat: fmt.Sprint(avant, " → ", c.Valeur)})
	s.ecrireJSON(w, map[string]string{"verrou_auto": c.Valeur})
}
