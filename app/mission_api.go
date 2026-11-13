package main

// The Missions API (ARCHITECTURE §Missions et orchestrateur): read, create (a mission and its orchestrator's
// session), change the card (boxes and policy without the agent; a change asked in words resumes the orchestrator),
// answer the orchestrator's question, cancel. A paired unlocked device reads, changes, answers and cancels; a
// mission is created on the PC (autorisesDistance).

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	maxPhraseMission   = 4000
	maxConsigneMission = 2000
	maxReponseMission  = 1000
)

// erreurMission: a refusal with its HTTP code (409: the mission's state does not allow it, 404: unknown question).
type erreurMission struct {
	code int
	msg  string
}

func (e erreurMission) Error() string { return e.msg }

var errMissionAnnulee = errors.New("the mission was cancelled by the owner")

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
	case errors.Is(err, errMentionInconnue):
		s.erreur(w, http.StatusBadRequest, "A mention no longer exists: remove it and send again")
	case errors.Is(err, errOrchestrateurOccupe):
		s.erreur(w, http.StatusConflict, err.Error())
	case errors.As(err, &avecArgs):
		s.erreurArgs(w, http.StatusBadRequest, avecArgs.cle, avecArgs.args...)
	case errors.As(err, &entree):
		s.erreur(w, http.StatusBadRequest, err.Error())
	default:
		s.erreur(w, http.StatusInternalServerError, err.Error())
	}
}

// orchestrateurTravaille: an orchestrator's session runs for mission id.
func orchestrateurTravaille(id string) bool {
	orchestrateursEnCours.Lock()
	defer orchestrateursEnCours.Unlock()
	return orchestrateursEnCours.m[id] != nil
}

func evenementMission(etat, detail string) EvenementMission {
	return EvenementMission{T: time.Now().UTC().Format(formatCree), Etat: etat, Detail: detail}
}

// relancerOuEchec: the orchestrator resumed with enTete; if it cannot start, the mission is in echec with the reason.
func (s *Serveur) relancerOuEchec(id, enTete string) Mission {
	err := s.lancerOrchestrateur(id, enTete)
	m, _ := modifierMission(id, func(m *Mission) error {
		if err != nil && (m.Etat == "preparation" || m.Etat == "proposee") {
			m.Etat = "echec"
			m.Historique = append(m.Historique, evenementMission("echec", couper(s.masquerSecrets(err.Error()), 300)))
		}
		return nil
	})
	return m
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
// on the Servers task (its state and server); a project or task gone is "inconnu".
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
		case "servers":
			if t, ok := tacheMission(v.Ref); ok && len(t.Serveurs) > 0 {
				v.Actuel, v.Serveur = t.Etat, t.Serveurs[0]
			} else if v.Etat != "refusee" {
				v.Actuel = "inconnu"
			}
		}
	}
	return m
}

// apiMissions: GET /api/missions, every mission, the newest first, its lanes as they are now.
func (s *Serveur) apiMissions(w http.ResponseWriter, _ *http.Request) {
	l := lireMissions()
	for i := range l {
		l[i] = s.voiesEnDirect(l[i])
	}
	s.ecrireJSON(w, map[string]any{"missions": l})
}

// apiMission: GET /api/missions/{id}; unknown, damaged or hostile id → 404. orchestrateur: its session runs now.
func (s *Serveur) apiMission(w http.ResponseWriter, r *http.Request) {
	m, err := lireMission(r.PathValue("id"))
	if err != nil {
		s.erreur(w, http.StatusNotFound, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"mission": s.voiesEnDirect(m), "orchestrateur": orchestrateurTravaille(m.ID)})
}

// apiCreerMission: POST /api/missions {phrase, mentions[]{type, ref}} → a mission in preparation and its session.
func (s *Serveur) apiCreerMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Phrase   string            `json:"phrase"`
		Mentions []MentionDemandee `json:"mentions"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	phrase := strings.TrimSpace(c.Phrase)
	if phrase == "" || len([]rune(phrase)) > maxPhraseMission {
		s.erreur(w, http.StatusBadRequest, "Say what you want in one sentence, 4000 characters at most")
		return
	}
	mentions, err := s.resoudreMentions(c.Mentions)
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	cree := time.Now().UTC().Format(formatCree)
	m := Mission{ID: aleatoire(8), Phrase: s.masquerSecrets(phrase), Mentions: mentions, Etat: "preparation", Politique: "prod",
		Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{{T: cree, Etat: "preparation"}}, Cree: cree}
	if err := enregistrerMission(m); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + m.ID, Mission: m.ID, Action: "mission créée"})
	s.ecrireJSON(w, map[string]any{"mission": s.relancerOuEchec(m.ID, "")})
}

// apiModifierMission: POST /api/missions/{id}/modifier {acces_decoches[]{entree, pour}, politique, envs, projet,
// consigne}: the boxes, the policy, the environments and the project change the card, checked and computed again
// by the Go; a consigne then resumes the orchestrator with the card and the change asked. Absent = unchanged.
func (s *Serveur) apiModifierMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		AccesDecoches *[]AccesCarte `json:"acces_decoches"`
		Politique     string        `json:"politique"`
		Envs          *[]EnvCarte   `json:"envs"`
		Projet        *ProjetCarte  `json:"projet"`
		Consigne      string        `json:"consigne"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	consigne := strings.TrimSpace(c.Consigne)
	switch {
	case len([]rune(consigne)) > maxConsigneMission:
		s.erreur(w, http.StatusBadRequest, "The change asked is 2000 characters at most")
		return
	case c.Politique != "" && !slices.Contains(politiquesMission, c.Politique):
		s.erreur(w, http.StatusBadRequest, "Unknown policy (prod, tout or autonome)")
		return
	}
	id := r.PathValue("id")
	if _, ok := s.accordMissionAttente(id); ok { // C77: the card asked to launch stays as it is (its print is checked too)
		s.erreur(w, http.StatusConflict, "A launch request for this card waits for your decision: refuse it first to change the card")
		return
	}
	ctx := s.contexteCarte()
	var carte CarteMission
	m, err := modifierMission(id, func(m *Mission) error {
		switch _, ouverte := questionOuverte(*m); {
		case m.Etat != "proposee":
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": its card can no longer be changed"}
		case orchestrateurTravaille(id):
			return erreurMission{http.StatusConflict, "The orchestrator is working on this card: wait for it to finish"}
		case ouverte && consigne != "":
			return erreurMission{http.StatusConflict, "Answer the orchestrator's question first"}
		}
		if err := json.Unmarshal(m.Carte, &carte); err != nil {
			return errors.New("the mission's card cannot be read")
		}
		if c.AccesDecoches != nil {
			decoche := make([]bool, len(carte.Acces))
			for _, d := range *c.AccesDecoches {
				d.Entree, d.Pour = strings.TrimSpace(d.Entree), strings.TrimSpace(d.Pour)
				trouve := false
				for i, a := range carte.Acces { // B83: every copy of the same entry for the same target
					if memeAcces(a, d) {
						decoche[i], trouve = true, true
					}
				}
				if !trouve {
					return erreurEntreeArgs{"The card has no access {0} for {1}", []string{couper(d.Entree, 100), couper(d.Pour, 100)}}
				}
			}
			for i := range carte.Acces {
				carte.Acces[i].Decoche = decoche[i]
			}
		}
		if c.Envs != nil {
			carte.Envs = *c.Envs
		}
		if c.Projet != nil {
			carte.Projet = *c.Projet
		}
		if c.Politique != "" {
			m.Politique = c.Politique
		}
		if err := validerCarte(&carte, ctx); err != nil {
			return err
		}
		carte = calculerCarte(carte, m.Politique, ctx)
		b, err := json.Marshal(carte)
		if err != nil {
			return err
		}
		m.Carte = b
		detail := "changed by the owner"
		if consigne != "" {
			detail = "change asked: " + couper(s.masquerSecrets(consigne), 280)
		}
		m.Historique = append(m.Historique, evenementMission(m.Etat, detail))
		return nil
	})
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + id, Mission: id,
		Action: "carte de mission modifiée", Resultat: "politique " + m.Politique})
	if consigne != "" {
		b, _ := json.MarshalIndent(carte, "", " ")
		m = s.relancerOuEchec(id, "The owner asks this change to the card: "+consigne+"\n\nThe card as it is now, with the owner's changes:\n"+string(b))
	}
	s.ecrireJSON(w, map[string]any{"mission": m})
}

// apiRepondreMission: POST /api/missions/{id}/repondre {question, choix|texte}: the answer recorded, the
// orchestrator's session resumed with it.
func (s *Serveur) apiRepondreMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Question string `json:"question"`
		Choix    string `json:"choix"`
		Texte    string `json:"texte"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	texte := strings.TrimSpace(c.Texte)
	if c.Choix == "" && (texte == "" || len([]rune(texte)) > maxReponseMission) {
		s.erreur(w, http.StatusBadRequest, "Pick a choice or write an answer, 1000 characters at most")
		return
	}
	id := r.PathValue("id")
	var q QuestionMission
	_, err := modifierMission(id, func(m *Mission) error {
		i := slices.IndexFunc(m.Questions, func(x QuestionMission) bool { return x.ID == c.Question })
		switch {
		case m.Etat != "preparation" && m.Etat != "proposee":
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": its questions are closed"}
		case i < 0:
			return erreurMission{http.StatusNotFound, "Unknown question"}
		case m.Questions[i].Reponse != "":
			return erreurMission{http.StatusConflict, "This question is already answered"}
		case orchestrateurTravaille(id):
			return erreurMission{http.StatusConflict, "The orchestrator is still working: try again in a moment"}
		case c.Choix != "" && !slices.Contains(m.Questions[i].Choix, c.Choix):
			return erreurEntree("Unknown choice")
		}
		rep := c.Choix
		if rep == "" {
			rep = s.masquerSecrets(texte)
		}
		m.Questions[i].Reponse = rep
		q = m.Questions[i]
		m.Historique = append(m.Historique, evenementMission(m.Etat, "answered: "+couper(rep, 280)))
		return nil
	})
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "question de mission répondue"})
	s.ecrireJSON(w, map[string]any{"mission": s.relancerOuEchec(id, "The owner answers your question \""+q.Texte+"\": "+q.Reponse)})
}

// apiAnnulerMission: POST /api/missions/{id}/annuler: a mission not launched yet is cancelled, its orchestrator
// stopped, its token revoked and its launch request refused.
func (s *Serveur) apiAnnulerMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := modifierMission(id, func(m *Mission) error {
		if !slices.Contains([]string{"preparation", "proposee", "acceptee"}, m.Etat) {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": it can no longer be cancelled"}
		}
		m.Etat = "annulee"
		m.Historique = append(m.Historique, evenementMission("annulee", "cancelled by the owner"))
		return nil
	})
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	orchestrateursEnCours.Lock()
	if e := orchestrateursEnCours.m[id]; e != nil {
		e.annuler(errMissionAnnulee)
	}
	orchestrateursEnCours.Unlock()
	s.revoquerJetons("mission annulée", func(x SessionHub) bool { return x.Projet == prefixeMission+id })
	s.fermerLancementMission(id, deQui(r), "refuser", "mission annulée")
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission annulée"})
	s.ecrireJSON(w, map[string]any{"mission": m})
}
