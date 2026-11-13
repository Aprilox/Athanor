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
)

// The states of a mission, in their usual order.
var etatsMission = []string{"preparation", "proposee", "acceptee", "lancee", "terminee", "annulee", "echec"}

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
