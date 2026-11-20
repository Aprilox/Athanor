package main

// The Missions API: read and delete (mission_supprimer.go); launch, change and act: mission_nouvelle.go.

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

// erreurMission: a refusal with its HTTP code (409: the mission's state does not allow it, 404: unknown question).
type erreurMission struct {
	code int
	msg  string
}

func (e erreurMission) Error() string { return e.msg }

// erreurMissionHTTP: err as the right answer (unknown mission 404, input 400, state 409, otherwise 500).
func (s *Serveur) erreurMissionHTTP(w http.ResponseWriter, err error) {
	var em erreurMission
	var avecArgs erreurEntreeArgs
	var entree erreurEntree
	switch {
	case errors.As(err, &em):
		s.erreur(w, em.code, em.msg)
	case errors.Is(err, errMissionInconnue):
		s.erreur(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errServeurInconnu), errors.Is(err, errCoffreInconnu):
		s.erreur(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errMentionInconnue):
		s.erreur(w, http.StatusBadRequest, "A mention no longer exists: remove it and send again")
	case errors.As(err, &avecArgs):
		s.erreurArgs(w, http.StatusBadRequest, avecArgs.cle, avecArgs.args...)
	case errors.As(err, &entree):
		s.erreur(w, http.StatusBadRequest, err.Error())
	default:
		s.erreur(w, http.StatusInternalServerError, err.Error())
	}
}

// tacheMission: the Servers task id wherever its server is (a running one as it is now).
func tacheMission(id string) (TacheServeurs, bool) {
	if !idMissionValide.MatchString(id) {
		return TacheServeurs{}, false
	}
	fichiers, _ := filepath.Glob(filepath.Join(dossierModule("servers"), "*", "taches", id+".json"))
	for _, f := range fichiers {
		if t := tacheParID(filepath.Base(filepath.Dir(filepath.Dir(f))), id); t.ID == id {
			return t, true
		}
	}
	return TacheServeurs{}, false
}

// voiesEnDirect: m's lanes with their state read now, on the project's pilot (dev: travaille, termine, arrete) or
// on the Servers task (its state and server); a project or task gone is "inconnu". A lane on a remote Athanor
// (dev_distant) is no longer asked: it keeps what the file says.
func (s *Serveur) voiesEnDirect(m Mission) Mission {
	m.Voies = slices.Clone(m.Voies)
	for i := range m.Voies {
		v := &m.Voies[i]
		switch v.Module {
		case "dev":
			p, ok := s.projetParNom(v.Ref)
			e, lu := p.LireEtat()
			switch {
			case !ok:
				v.Actuel = "inconnu"
			case p.PiloteActif():
				v.Actuel, v.Detail = "travaille", e.JalonEnCours
			case lu && e.Phase == "termine":
				v.Actuel = "termine"
			default:
				v.Actuel = "arrete"
			}
			if ok {
				s.avancementVoie(v, p, e.JalonEnCours)
			}
		case "servers":
			if t, ok := tacheMission(v.Ref); ok && len(t.Serveurs) > 0 {
				v.Actuel, v.Serveur = t.Etat, t.Serveurs[0]
			} else if v.Etat == "attente_plan" { // M249: prepared once the project's plan exists
				v.Actuel = v.Etat
			} else if v.Etat != "refusee" {
				v.Actuel = "inconnu"
			}
		}
	}
	return m
}

// avancementVoie: v's milestones done of p's plan, the title of milestone jalon in progress and, while its pilot
// works, what the project lead does (the team view of a log already open, as the home's Studio block).
func (s *Serveur) avancementVoie(v *VoieMission, p Projet, jalon string) {
	for _, j := range p.LirePlan() {
		v.Total++
		if j.Fait {
			v.Fait++
		}
		if v.Actuel == "travaille" && jalon != "" && v.Titre == "" && (jalon == j.ID || strings.HasPrefix(jalon, j.ID+" ")) {
			v.Titre = strings.TrimLeft(strings.TrimPrefix(j.Titre, j.ID), " :")
		}
	}
	if v.Actuel != "travaille" {
		return
	}
	s.mu.Lock()
	l := s.logs[p.Nom]
	s.mu.Unlock()
	if l != nil {
		for _, a := range l.Agents() {
			if a.Genre == "chef" && a.Statut != "termine" {
				v.Activite = a.Activite
			}
		}
	}
}

// apiMissions: GET /api/missions, every mission, the newest first, its project and server as they are now.
func (s *Serveur) apiMissions(w http.ResponseWriter, _ *http.Request) {
	l := []VueMission{}
	for _, m := range lireMissions() {
		l = append(l, s.vueMission(m))
	}
	s.ecrireJSON(w, map[string]any{"missions": l, "reglages": vueReglagesProjet("")}) // reglages: a new project's defaults, for the form
}

// apiMission: GET /api/missions/{id}, with what its deletion can also remove; unknown, damaged or hostile id → 404.
func (s *Serveur) apiMission(w http.ResponseWriter, r *http.Request) {
	m, err := lireMission(r.PathValue("id"))
	if err != nil {
		s.erreur(w, http.StatusNotFound, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"mission": s.vueMission(m), "suppression": s.apercuSuppression(m)})
}
