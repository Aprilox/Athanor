package main

// Controlling a launched mission (ARCHITECTURE §étape 10 Contrôle): pause (the project's pilot stopped, the
// mission's lanes locked), resume (the owner's presence), stop (nothing undone: the journal says what stays) and
// policy (stricter at once, looser with the owner's presence). The emergency stop pauses every launched mission.

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"
)

// arreterPiloteMission: replaced by the tests (no kit script there).
var arreterPiloteMission = func(s *Serveur, p Projet) error { _, err := s.arreterPiloteProjet(p); return err }

var errMissionArretee = errors.New("the mission was stopped by the owner")

// rigueurPolitique: the missions' policies, the loosest first.
var rigueurPolitique = []string{"autonome", "prod", "tout"}

// missionBloquee: the paused mission one of whose lanes is (module, ref) — a project name or a Servers task id —,
// "" none. Read by the milestone relay, the Servers plans, demarrerPilote (the prod relay reads the state itself).
func missionBloquee(module, ref string) string {
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

// projetMission: m's Dev lane's project (false: none, or gone).
func (s *Serveur) projetMission(m Mission) (Projet, bool) {
	for _, v := range m.Voies {
		if v.Module == "dev" {
			return s.projetParNom(v.Ref)
		}
	}
	return Projet{}, false
}

// projetTermine: p's pilot finished its request (as voiesEnDirect says "termine").
func projetTermine(p Projet) bool {
	e, lu := p.LireEtat()
	return lu && e.Phase == "termine"
}

// mettreEnPause: launched mission id paused by qui (raison in its history and journal); its pilot is the caller's.
func (s *Serveur) mettreEnPause(id, qui, raison string) (Mission, error) {
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Etat != "lancee" {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": only a launched mission can be paused"}
		}
		m.Etat = "en_pause"
		m.Historique = append(m.Historique, evenementMission("en_pause", raison))
		ajouterJournal(m, []EntreeJournal{s.entreeJournal("pause:"+time.Now().UTC().Format(formatCree), time.Now(), "attention", "Mission paused: {0}", raison)})
		return nil
	})
	if err == nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission en pause", Resultat: raison})
	}
	return m, err
}

// pauserMissionsUrgence: every launched mission paused by the emergency stop (its pilots are stopped there).
func (s *Serveur) pauserMissionsUrgence(qui string) {
	for _, m := range lireMissions() {
		if m.Etat == "lancee" {
			_, _ = s.mettreEnPause(m.ID, qui, "emergency stop")
		}
	}
}

// apiControleMission: POST /api/missions/{id}/pause|reprendre|arreter|politique {mot_de_passe?, politique?}.
// Resume and a looser policy need the owner's presence (password, or a paired device unlocked with it).
func (s *Serveur) apiControleMission(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c struct {
			MotDePasse string `json:"mot_de_passe"`
			Politique  string `json:"politique"`
		}
		if r.ContentLength != 0 && lireCorps(r, &c) != nil {
			s.erreur(w, http.StatusBadRequest, "Invalid request")
			return
		}
		id, qui := r.PathValue("id"), deQui(r)
		m, err := lireMission(id)
		if err != nil {
			s.erreurMissionHTTP(w, err)
			return
		}
		if action == "politique" && !slices.Contains(politiquesMission, c.Politique) {
			s.erreur(w, http.StatusBadRequest, "Unknown policy")
			return
		}
		if action == "reprendre" || action == "politique" && slices.Index(rigueurPolitique, c.Politique) < slices.Index(rigueurPolitique, m.Politique) {
			if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Mission: id, Action: "mission " + action + " refusé", Resultat: err.Error()})
				s.erreur(w, code, err.Error())
				return
			}
		}
		switch action {
		case "pause":
			if m, err = s.mettreEnPause(id, qui, qui); err == nil {
				s.ordreVoiesDistantes(m, "pause_fin_cycle")
				if p, ok := s.projetMission(m); ok && p.PiloteActif() {
					marqueAuto(p, false)
					if e := arreterPiloteMission(s, p); e != nil {
						s.noterJournalMission(id, s.entreeJournal("pause:pilote:"+m.Historique[len(m.Historique)-1].T, time.Now(), "erreur", "The pilot of {0} did not stop: {1}", p.Nom, e.Error()))
					}
				}
			}
		case "reprendre":
			m, err = s.reprendreMission(id, qui)
		case "arreter":
			m, err = s.arreterMission(id, qui)
		case "politique":
			m, err = s.changerPolitiqueMission(id, qui, c.Politique)
		}
		if err != nil {
			s.erreurMissionHTTP(w, err)
			return
		}
		s.ecrireJSON(w, map[string]any{"mission": s.voiesEnDirect(m)})
	}
}

// reprendreMission: paused mission id launched again, its pilot started and its follower running.
func (s *Serveur) reprendreMission(id, qui string) (Mission, error) {
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Etat != "en_pause" {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": only a paused mission can be resumed"}
		}
		m.Etat = "lancee"
		m.Historique = append(m.Historique, evenementMission("lancee", "resumed: "+qui))
		ajouterJournal(m, []EntreeJournal{s.entreeJournal("reprise:"+time.Now().UTC().Format(formatCree), time.Now(), "info", "Mission resumed: {0}", qui)})
		return nil
	})
	if err != nil {
		return m, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission reprise"})
	if p, ok := s.projetMission(m); ok && !p.PiloteActif() && !projetTermine(p) {
		if _, e := demarrerPiloteMission(s, p); e != nil {
			s.noterJournalMission(id, s.entreeJournal("reprise:pilote:"+m.Historique[len(m.Historique)-1].T, time.Now(), "erreur", "The pilot of {0} did not start: {1}", p.Nom, e.Error()))
		} else {
			marqueAuto(p, true)
		}
	}
	s.ordreVoiesDistantes(m, "demarrer")
	s.suivreMission(id)
	return lireMission(id)
}

// arreterMission: launched or paused mission id stopped for good: its pilot stopped, its Servers tasks cancelled,
// the tokens of its orchestrator, chefs ops and pilot revoked, its pending accords refused, the deployment after
// each milestone off on its environments. Nothing is undone: the journal says what stays.
func (s *Serveur) arreterMission(id, qui string) (Mission, error) {
	var c CarteMission
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Etat != "lancee" && m.Etat != "en_pause" {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": only a launched or paused mission can be stopped"}
		}
		_ = json.Unmarshal(m.Carte, &c)
		m.Etat = "arretee"
		m.Historique = append(m.Historique, evenementMission("arretee", "stopped: "+qui))
		now := time.Now()
		reste := []EntreeJournal{s.entreeJournal("arret", now, "attention", "Mission stopped: {0}. Nothing was undone", qui)}
		noms := map[string]string{}
		if l, err := lireServeurs(); err == nil {
			for _, x := range l {
				noms[x.ID] = x.Nom
			}
		}
		for _, e := range c.Envs {
			reste = append(reste, s.entreeJournal("arret:env:"+e.Nom, now, "info", "The environment {0} stays: {1}", e.Nom, e.URL))
			for _, srv := range e.Serveurs {
				reste = append(reste, s.entreeJournal("arret:serveur:"+srv, now, "info", "The files on the server {0} stay", cmp.Or(noms[srv], srv)))
			}
		}
		if slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return !a.Decoche && !a.ARemplir }) {
			reste = append(reste, s.entreeJournal("arret:acces", now, "info", "The access given to {0} stays: remove it in Access if needed", c.Projet.Nom))
		}
		ajouterJournal(m, reste)
		return nil
	})
	if err != nil {
		return m, err
	}
	taches := map[string]bool{}
	for _, v := range m.Voies {
		if v.Module == "servers" {
			taches[v.Ref] = true
			s.arreterTache(v.Ref)
		}
	}
	p, avecProjet := s.projetMission(m)
	if avecProjet && p.PiloteActif() {
		marqueAuto(p, false)
		if e := arreterPiloteMission(s, p); e != nil {
			s.noterJournalMission(id, s.entreeJournal("arret:pilote", time.Now(), "erreur", "The pilot of {0} did not stop: {1}", p.Nom, e.Error()))
		}
	}
	s.ordreVoiesDistantes(m, "arreter")
	s.arreterOrchestrateur(id, errMissionArretee)
	s.revoquerJetons("mission arrêtée", func(x SessionHub) bool {
		return x.Projet == prefixeMission+id || avecProjet && x.Projet == p.Nom || x.Mission != "" && taches[x.Mission]
	})
	if s.approbations != nil {
		l, _ := s.approbations.Liste()
		for _, x := range l {
			if x.Etat == "attente" && (x.Mission == id || x.Details["mission"] == id || x.Mission != "" && taches[x.Mission]) {
				s.refuserAccordMission(x.ID, qui, "mission arrêtée")
			}
		}
	}
	if avecProjet && s.acces != nil {
		envs := s.acces.Environnements(p.Nom)
		for _, ec := range c.Envs {
			if e, ok := envs[ec.Nom]; ok && e.DeployerAuJalon {
				e.DeployerAuJalon = false
				if err := s.acces.DefinirEnvironnement(p.Nom, ec.Nom, e); err != nil {
					s.noterJournalMission(id, s.entreeJournal("arret:jalon:"+ec.Nom, time.Now(), "erreur", "The deployment after each milestone of {0} is still on: {1}", ec.Nom, err.Error()))
				}
			}
		}
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission arrêtée"})
	return lireMission(id)
}

// arreterOrchestrateur: mission id's orchestrator session (if one runs) cancelled with err.
func (s *Serveur) arreterOrchestrateur(id string, err error) {
	orchestrateursEnCours.Lock()
	defer orchestrateursEnCours.Unlock()
	if e := orchestrateursEnCours.m[id]; e != nil {
		e.annuler(err)
	}
}

// changerPolitiqueMission: mission id's policy set to pol and its environments' policies recalculated (prod
// always accord); tout also ends the "never ask again" deployment rules of its environments (C82).
func (s *Serveur) changerPolitiqueMission(id, qui, pol string) (Mission, error) {
	var avant string
	var c CarteMission
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Etat != "lancee" && m.Etat != "en_pause" {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": its policy can no longer change"}
		}
		avant = m.Politique
		if pol == avant {
			return nil
		}
		_ = json.Unmarshal(m.Carte, &c)
		m.Politique = pol
		m.Historique = append(m.Historique, evenementMission(m.Etat, "policy "+avant+" → "+pol+": "+qui))
		ajouterJournal(m, []EntreeJournal{s.entreeJournal("politique:"+time.Now().UTC().Format(formatCree), time.Now(), "info", "Policy changed: {0} → {1}", avant, pol)})
		return nil
	})
	if err != nil || pol == avant {
		return m, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "politique de mission changée", Resultat: avant + " → " + pol})
	if s.acces == nil {
		return m, nil
	}
	envs := s.acces.Environnements(c.Projet.Nom)
	for _, ec := range c.Envs {
		e, ok := envs[ec.Nom]
		if !ok {
			continue
		}
		// R5: never under the existing environment's own policy, read when the card was worked out (C81)
		cible := politiqueEnvMission(ec.Nom, pol)
		if ec.Plancher == "accord" || ec.Avant != nil && ec.Avant.Politique == "accord" {
			cible = "accord"
		}
		if pol == "tout" && s.approbations != nil {
			_, _ = s.approbations.RetirerRegles(func(r RegleAccord) bool {
				return r.Projet == c.Projet.Nom && r.Env == ec.Nom && r.Action == actionDeploiement
			})
		}
		if e.Politique == cible {
			continue
		}
		e.Politique = cible
		if err := s.acces.DefinirEnvironnement(c.Projet.Nom, ec.Nom, e); err != nil {
			s.noterJournalMission(id, s.entreeJournal("politique:env:"+ec.Nom+":"+cible, time.Now(), "erreur", "The environment {0} kept its policy: {1}", ec.Nom, err.Error()))
			continue
		}
		s.noterJournalMission(id, s.entreeJournal("politique:env:"+ec.Nom+":"+time.Now().UTC().Format(formatCree), time.Now(), "info", "The environment {0} now deploys: {1}", ec.Nom, cible))
	}
	return lireMission(id)
}
