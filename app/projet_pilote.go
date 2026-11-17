package main

// A project's pilot seen from the hub: the start and stop the tests replace, and what holds a project or a
// Servers task back.

import "slices"

// demarrerPiloteProjet: replaced by the tests (a fake pilot).
var demarrerPiloteProjet = (*Serveur).demarrerPilote

// arreterPiloteProjet: replaced by the tests (no kit script there).
var arreterPiloteProjet = func(s *Serveur, p Projet) error { _, err := s.arreterPiloteProjet(p); return err }

// projetBloque: the paused mission one of whose lanes is (module, ref) — a project name or a Servers task id —,
// "" none. Read by the milestone relay, the Servers plans, demarrerPilote (the prod relay reads the state itself).
func projetBloque(module, ref string) string {
	if ref == "" {
		return ""
	}
	for _, m := range lireMissions() {
		if m.Etat == "en_pause" && slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == module && v.Ref == ref }) {
			return m.ID
		}
	}
	return ""
}
