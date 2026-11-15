package main

// Decisions an agent of this PC must not take on its own (M70b, critic C24 C25 C27). An agent that runs
// code as the user (native mode, or pc-exec in WSL mode) can read instance.json, get a window link from
// /api/instance/terminal and drive the window's API: the window's cookie proves nothing. Two proofs it
// cannot make:
//   - presence: Athanor's password retyped in the request, or a paired device unlocked with it (a phone,
//     off this PC). A password must exist: without one, a device the agent paired itself would count.
//   - absence: no pilot, chat or test session Athanor started is running (an agent only lives in one).
// shortcut: absence misses a process an agent left detached after its pilot stopped, and an agent can edit
// Athanor's files (DESIGN.md §Limite de la barrière); upgrade with a confirmation off this PC.

import (
	"errors"
	"net/http"
)

var (
	errPreuveSansMdp = erreurEntree("An agent of this PC could decide in your place: choose a password for Athanor in Options first, then retype it here (or decide from a paired phone)")
	errPreuveAgents  = erreurEntree("An agent Athanor started is still running and could do this in your place: stop the pilots, chats and tests first")
)

// preuvePresence: the owner is there (password retyped, or a paired device unlocked with it); an error
// comes with its HTTP status.
func (s *Serveur) preuvePresence(r *http.Request, mdp string) (int, error) {
	s.mu.Lock()
	sel, hash := s.config.Sel, s.config.MotDePasseHash
	s.mu.Unlock()
	if hash == "" {
		return http.StatusForbidden, errPreuveSansMdp
	}
	if appareilDe(r) != "" {
		return 0, nil // garde let it in: unlocked with the password on that device
	}
	if ok, _, plein := s.verifierMdpLent(sel, mdp, hash, true); plein {
		return http.StatusTooManyRequests, errTropEssaisMdp
	} else if !ok {
		return http.StatusUnauthorized, errors.New("Wrong password")
	}
	return 0, nil
}

// agentEnCours: what Athanor started that runs an agent now ("" none): a pilot, a chat, Test the hub's MCP.
func (s *Serveur) agentEnCours() string {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	test := s.projetTestMCP.Nom
	s.mu.Unlock()
	if test != "" {
		return test
	}
	for _, p := range projets {
		if p.PiloteActif() {
			return p.Nom
		}
	}
	chatMu.Lock()
	defer chatMu.Unlock()
	for d := range chatsActifs {
		return "chat " + d
	}
	return ""
}

// preuveAbsence: no agent of Athanor runs, so the request comes from the owner (first password, Resume).
func (s *Serveur) preuveAbsence() (int, error) {
	if s.agentEnCours() != "" {
		return http.StatusConflict, errPreuveAgents
	}
	return 0, nil
}
