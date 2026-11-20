package main

// Missions: one JSON file per mission in dossierAthanor()/hub/missions. Those of 3.0.0 and earlier (no Projet)
// are only read and, when the owner asks, deleted; those of 3.1 are written by mission_nouvelle.go. A damaged
// file is skipped on reading, never fatal.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// VoieMission: one lane of a launched mission (module dev or servers, ref = project or Servers task id).
type VoieMission struct {
	Module string `json:"module"`
	Ref    string `json:"ref"`
	Etat   string `json:"etat"`
	// read now by the API, never stored (voiesEnDirect): the pilot's or the task's state, the milestone, the task's server
	Actuel  string `json:"actuel,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Serveur string `json:"serveur,omitempty"`
	Nom     string `json:"nom,omitempty"`   // a remote Dev lane: its project as nom@instance
	Ordre   string `json:"ordre,omitempty"` // a remote Dev lane: the pilot order it missed while offline, sent on its return
	// Studio lane: the milestones done of the plan, the title of the one in progress, what the project lead does
	Fait     int    `json:"fait,omitempty"`
	Total    int    `json:"total,omitempty"`
	Titre    string `json:"titre,omitempty"`
	Activite string `json:"activite,omitempty"`
	Env      string `json:"env,omitempty"` // a Servers lane (M249): its "project/env", prepared once the project's plan exists
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
	ID          string             `json:"id"`
	Phrase      string             `json:"phrase"` // masked by masquerSecrets, at most 4000 characters
	Mentions    Mentions           `json:"mentions"`
	Etat        string             `json:"etat"`
	Carte       json.RawMessage    `json:"carte,omitempty"` // the mission card (CarteMission, M152)
	Politique   string             `json:"politique"`       // prod | tout | autonome
	Voies       []VoieMission      `json:"voies"`
	Questions   []QuestionMission  `json:"questions"`
	RefusCarte  *MessageMoteur     `json:"refus_carte,omitempty"` // M258: why the last card was refused, said if none follows
	Historique  []EvenementMission `json:"historique"`
	Cree        string             `json:"cree"`
	Session     string             `json:"session,omitempty"`      // the orchestrator's Claude session, resumed with --resume
	Relais      *RelaisMission     `json:"relais,omitempty"`       // where its relay stood
	Journal     []EntreeJournal    `json:"journal,omitempty"`      // at most maxJournalMission, the oldest first
	InfraJusqua string             `json:"infra_jusqua,omitempty"` // former option (removed, note 19h03 §3): still read from older missions, never used
	// a mission of 3.1 (mission_nouvelle.go): its project, server (inventory id) and API key (vault id), and its
	// permission choice; Projet "" = an old mission, read only
	Projet   string `json:"projet,omitempty"`
	Serveur  string `json:"serveur,omitempty"`
	API      string `json:"api,omitempty"`
	Autonome bool   `json:"autonome,omitempty"`
}

var (
	idMissionValide    = regexp.MustCompile(`^[a-z0-9]{6,32}$`)
	errMissionInconnue = errors.New("unknown mission")
)

func dossierMissions() string { return filepath.Join(dossierAthanor(), "hub", "missions") }

// lireMission: a hostile id (path, dots, capitals) is unknown, as is a damaged file.
func lireMission(id string) (Mission, error) {
	var m Mission
	if !idMissionValide.MatchString(id) {
		return m, errMissionInconnue
	}
	f := filepath.Join(dossierMissions(), id+".json")
	b, err := os.ReadFile(f)
	// Windows: ecrireAtomique may hold the file in its rename, or have moved it to .ancien for a moment
	for i := 0; err != nil && runtime.GOOS == "windows" && i < 10; i++ {
		if _, e := os.Stat(f + ".ancien"); e != nil && errors.Is(err, os.ErrNotExist) {
			break
		}
		time.Sleep(20 * time.Millisecond)
		b, err = os.ReadFile(f)
	}
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

// prefixeMission: the audit's "project" of a mission (missions:<id>): never a real project.
const prefixeMission = "missions:"

// dossierSessionMission: where the former orchestrator's session of mission id worked (removed with the mission).
func dossierSessionMission(id string) string { return filepath.Join(dossierMissions(), id+"-session") }

// titreMission: the card's title, or the start of the owner's sentence before a card exists.
func titreMission(m Mission) string {
	var c struct{ Titre string }
	if json.Unmarshal(m.Carte, &c) == nil && c.Titre != "" {
		return c.Titre
	}
	return couper(m.Phrase, 80)
}

// RelaisMission: where the mission's relay stood (construction → dev_deploye → dev_verifie → accord_prod →
// prod_deploye → termine) and what it stood on.
type RelaisMission struct {
	Etape           string `json:"etape"`
	Commit          string `json:"commit,omitempty"`
	DeploiementDev  string `json:"deploiement_dev,omitempty"`
	DeploiementProd string `json:"deploiement_prod,omitempty"`
	Accord          string `json:"accord,omitempty"`
}

// EntreeJournal: a translation key (an English template of i18n.js) and its parameters, never an agent's free text.
type EntreeJournal struct {
	ID     string   `json:"id"`
	T      string   `json:"t"`
	Cle    string   `json:"cle"`
	Params []string `json:"params,omitempty"`
	Niveau string   `json:"niveau"` // info | attention | erreur
}
