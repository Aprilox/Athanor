package main

// A project's pilot seen from the hub: the start and stop the tests replace, and what holds a project or a
// Servers task back.

import "slices"

// demarrerPiloteProjet: replaced by the tests (a fake pilot).
var demarrerPiloteProjet = (*Serveur).demarrerPilote

// arreterPiloteProjet: replaced by the tests (no kit script there).
var arreterPiloteProjet = func(s *Serveur, p Projet) error { _, err := s.arreterPiloteProjet(p); return err }

// projetBloque: the paused mission one of whose lanes is (module, ref) — a project name or a Servers task id —,
// or (M298) the project the owner paused or stopped, ref being its name or a task its deployment launched; ""
// none. Read by the milestone relay, the preparation, the Servers plans, demarrerPilote (the prod relay reads the
// state itself).
func projetBloque(module, ref string) string {
	if ref == "" {
		return ""
	}
	for _, m := range lireMissions() {
		if m.Etat == "en_pause" && slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == module && v.Ref == ref }) {
			return m.ID
		}
	}
	for nom, r := range lireRelais() {
		if r.Pause && (module == "dev" && nom == ref || module == "servers" && (r.Premier == ref || r.Preparation != nil && r.Preparation.Tache == ref)) {
			return nom
		}
	}
	return ""
}

// arreterTachesProjet (M298): "Stop immediately" also cancels the Servers tasks the project's deployment launched
// and that still run (preparation, first deployment), their tokens revoked (arreterTache). A cancelled preparation
// is made again, once, when the project starts again, and so is a cancelled first deployment (its commit waits).
func (s *Serveur) arreterTachesProjet(projet string) {
	r := lireRelais()[projet]
	ids := []string{r.Premier}
	if r.Preparation != nil {
		ids = append(ids, r.Preparation.Tache)
	}
	for _, id := range ids {
		if !s.arreterTache(id) {
			continue
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "owner", Vers: "servers", Projet: projet, Mission: id, Action: "tâche serveurs arrêtée", Resultat: "projet arrêté"})
		if id == r.Premier {
			r.Etat = "attente" // its commit waits again, as before that task
		} else {
			r.Preparation = &PreparationRelais{} // no fingerprint: prepared again
		}
		ecrireRelais(projet, r)
	}
}
