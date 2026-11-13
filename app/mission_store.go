package main

// Missions: one JSON file per mission in dossierAthanor()/hub/missions (0600, atomic writes); a damaged
// file is skipped on reading, never fatal (ARCHITECTURE §Missions et orchestrateur).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// The states of a mission, in their usual order.
var etatsMission = []string{"preparation", "proposee", "acceptee", "lancee", "en_pause", "terminee", "arretee", "annulee", "echec"}

// MentionsMission: what the owner mentioned (@project, @server, @key), resolved by the Go (resoudreMentions):
// project names, server ids and vault entry ids (names are not unique); Nouveau: the "New project" chip.
type MentionsMission struct {
	Projets  []string `json:"projets"`
	Serveurs []string `json:"serveurs"`
	Cles     []string `json:"cles"`
	Nouveau  bool     `json:"nouveau,omitempty"`
}

// VoieMission: one lane of a launched mission (module dev or servers, ref = project or Servers task id).
type VoieMission struct {
	Module string `json:"module"`
	Ref    string `json:"ref"`
	Etat   string `json:"etat"`
	// read now by the API, never stored (voiesEnDirect): the pilot's or the task's state, the milestone, the task's server
	Actuel  string `json:"actuel,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Serveur string `json:"serveur,omitempty"`
	// Studio lane: the milestones done of the plan, the title of the one in progress, what the project lead does
	Fait     int    `json:"fait,omitempty"`
	Total    int    `json:"total,omitempty"`
	Titre    string `json:"titre,omitempty"`
	Activite string `json:"activite,omitempty"`
}

// QuestionMission: a question of the orchestrator to the owner (at most 4 choices), with the answer.
type QuestionMission struct {
	ID      string   `json:"id"`
	Texte   string   `json:"texte"`
	Choix   []string `json:"choix,omitempty"`
	Reponse string   `json:"reponse,omitempty"`
	Cree    string   `json:"cree"`
}

type EvenementMission struct {
	T      string `json:"t"`
	Etat   string `json:"etat"`
	Detail string `json:"detail,omitempty"`
	// B87: Detail to translate (a launch's failed step), an English template of i18n.js and its arguments
	Message *MessageMoteur `json:"message,omitempty"`
}

type Mission struct {
	ID         string             `json:"id"`
	Phrase     string             `json:"phrase"` // masked by masquerSecrets, at most 4000 characters
	Mentions   MentionsMission    `json:"mentions"`
	Etat       string             `json:"etat"`
	Carte      json.RawMessage    `json:"carte,omitempty"` // the mission card (CarteMission, M152)
	Politique  string             `json:"politique"`       // prod | tout | autonome
	Voies      []VoieMission      `json:"voies"`
	Questions  []QuestionMission  `json:"questions"`
	Historique []EvenementMission `json:"historique"`
	Cree       string             `json:"cree"`
	Session    string             `json:"session,omitempty"` // the orchestrator's Claude session, resumed with --resume
	Relais     *RelaisMission     `json:"relais,omitempty"`  // once launched (mission_suivi.go)
	Journal    []EntreeJournal    `json:"journal,omitempty"` // at most maxJournalMission, the oldest first
}

// MarshalJSON: the mission with its card's print (empreinteCarte), which the window sends back to launch it (R3).
func (m Mission) MarshalJSON() ([]byte, error) {
	type brute Mission
	e := ""
	if len(m.Carte) > 0 {
		e = empreinteCarte(m)
	}
	return json.Marshal(struct {
		brute
		Empreinte string `json:"empreinte,omitempty"`
	}{brute(m), e})
}

var (
	idMissionValide    = regexp.MustCompile(`^[a-z0-9]{6,32}$`)
	errMissionInconnue = errors.New("unknown mission")
)

func dossierMissions() string { return filepath.Join(dossierAthanor(), "hub", "missions") }

func enregistrerMission(m Mission) error {
	if !idMissionValide.MatchString(m.ID) || !slices.Contains(etatsMission, m.Etat) {
		return errors.New("invalid mission")
	}
	if err := os.MkdirAll(dossierMissions(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return ecrireAtomique(filepath.Join(dossierMissions(), m.ID+".json"), b, 0o600)
}

// verrouMissions: the orchestrator's end and its tools (M154) change the same files.
var verrouMissions sync.Mutex

// modifierMission: mission id read, changed by f and written under verrouMissions (f's error: nothing written).
func modifierMission(id string, f func(*Mission) error) (Mission, error) {
	verrouMissions.Lock()
	defer verrouMissions.Unlock()
	m, err := lireMission(id)
	if err == nil {
		err = f(&m)
	}
	if err == nil {
		err = enregistrerMission(m)
	}
	return m, err
}

// lireMission: a hostile id (path, dots, capitals) is unknown, as is a damaged file.
func lireMission(id string) (Mission, error) {
	var m Mission
	if !idMissionValide.MatchString(id) {
		return m, errMissionInconnue
	}
	b, err := os.ReadFile(filepath.Join(dossierMissions(), id+".json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.ID != id {
		return Mission{}, errMissionInconnue
	}
	return m, nil
}

// lireMissions: every readable mission, the newest first.
func lireMissions() []Mission {
	fichiers, _ := filepath.Glob(filepath.Join(dossierMissions(), "*.json"))
	l := []Mission{}
	for _, f := range fichiers {
		if m, err := lireMission(strings.TrimSuffix(filepath.Base(f), ".json")); err == nil {
			l = append(l, m)
		}
	}
	slices.SortFunc(l, func(a, b Mission) int { return strings.Compare(b.Cree, a.Cree) })
	return l
}
