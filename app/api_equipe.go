package main

// The project's own sub-agents: the ones the user creates on the Team page and the ones the project
// lead proposes (the pilot imports them, system/equipe.py). They live in extensions/agents.json,
// shared with the pilot, which passes them to every Claude session (lead and parallel workers).

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// AgentPerso: a sub-agent of the user's or of the lead's (same fields as system/equipe.py).
type AgentPerso struct {
	Nom          string `json:"nom"`
	Description  string `json:"description"`
	Consignes    string `json:"consignes"`
	Modele       string `json:"modele"` // "" as the other sub-agents, "eco" cheap model, "fort" opus
	LectureSeule bool   `json:"lecture_seule"`
	Projet       string `json:"projet"`  // "" = every project, else the project's folder
	Origine      string `json:"origine"` // utilisateur | chef
	Cree         string `json:"cree,omitempty"`
}

type fichierAgents struct {
	Agents  []AgentPerso        `json:"agents"`
	Refuses map[string][]string `json:"refuses"`
}

var (
	verrouAgents   sync.Mutex
	reNomAgent     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
	agentsReserves = map[string]bool{"architect": true, "creator": true, "developer": true, "tester": true, "critic": true, "executor": true, "scout": true,
		"general-purpose": true, "explore": true, "plan": true, "statusline-setup": true, "output-style-setup": true, "claude-code-guide": true, "fork": true}
)

const agentsParProjetMax = 12

func cheminAgents(k Kit) string { return filepath.Join(dossierExt(k), "agents.json") }

func lireAgents(k Kit) fichierAgents {
	var f fichierAgents
	lireJSONFichier(cheminAgents(k), &f)
	if f.Refuses == nil {
		f.Refuses = map[string][]string{}
	}
	return f
}

// agentsLisibles: the file is absent or valid JSON (an unreadable one is never overwritten).
func agentsLisibles(k Kit) bool {
	b, err := os.ReadFile(cheminAgents(k))
	if os.IsNotExist(err) {
		return true
	}
	var f fichierAgents
	return err == nil && json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &f) == nil
}

func normaliserDossier(d string) string {
	d = strings.ToLower(strings.TrimRight(strings.ReplaceAll(d, `\`, "/"), "/"))
	if strings.HasPrefix(d, "//wsl$/") { // the same WSL folder under its two Windows names
		d = "//wsl.localhost/" + d[len("//wsl$/"):]
	}
	return d
}

// duProjet: the sub-agent applies to this project (its own, or one for every project).
func (a AgentPerso) duProjet(p Projet) bool {
	return a.Projet == "" || normaliserDossier(a.Projet) == normaliserDossier(p.Dossier)
}

func validerAgent(a AgentPerso) error {
	switch {
	case !reNomAgent.MatchString(a.Nom):
		return errors.New("Name: lowercase letters, digits and dashes, 2 to 31 characters (e.g. translator).")
	case agentsReserves[a.Nom]:
		return errors.New("This name belongs to one of Athanor's sub-agents: choose another one.")
	case strings.TrimSpace(a.Description) == "" || utf8.RuneCountInString(a.Description) > 400:
		return errors.New("Say when to use it, in 400 characters at most.")
	case strings.TrimSpace(a.Consignes) == "" || utf8.RuneCountInString(a.Consignes) > 6000:
		return errors.New("Write its instructions, in 6,000 characters at most.")
	case a.Modele != "" && a.Modele != "eco" && a.Modele != "fort":
		return errors.New("Unknown model.")
	}
	return nil
}

// agentsDuProjet: the project's own sub-agents and the ones the user removed (never proposed again).
func (s *Serveur) agentsDuProjet(p Projet) ([]AgentPerso, []string) {
	f := lireAgents(s.kitCourant())
	l := []AgentPerso{}
	for _, a := range f.Agents {
		if a.duProjet(p) && validerAgent(a) == nil && len(l) < agentsParProjetMax {
			l = append(l, a)
		}
	}
	refus := []string{}
	for cle, v := range f.Refuses {
		if normaliserDossier(cle) == normaliserDossier(p.Dossier) {
			refus = append(refus, v...)
		}
	}
	return l, refus
}

func (s *Serveur) apiAgentsPerso(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var c struct {
		Action string     `json:"action"` // enregistrer | supprimer
		Ancien string     `json:"ancien"` // name before an edit
		Agent  AgentPerso `json:"agent"`
		Tous   bool       `json:"tous"` // for every project
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	k := s.kitCourant()
	verrouAgents.Lock()
	defer verrouAgents.Unlock()
	if !agentsLisibles(k) {
		s.erreur(w, http.StatusConflict, "extensions/agents.json is unreadable: fix or remove it, nothing was changed.")
		return
	}
	f := lireAgents(k)
	trouve := func(nom string) int {
		for i, a := range f.Agents {
			if a.Nom == nom && a.duProjet(p) {
				return i
			}
		}
		return -1
	}
	switch c.Action {
	case "supprimer":
		i := trouve(c.Ancien)
		if i < 0 {
			s.erreur(w, http.StatusNotFound, "This sub-agent no longer exists.")
			return
		}
		a := f.Agents[i]
		f.Agents = append(f.Agents[:i], f.Agents[i+1:]...)
		if a.Origine == "chef" { // the lead never proposes it again in this project
			f.Refuses[p.Dossier] = append(f.Refuses[p.Dossier], a.Nom)
		}
		if err := ecrireJSONFichier(cheminAgents(k), f, 0o644); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.msg(w, "Sub-agent removed: it leaves the team at the next cycle.")
	case "enregistrer":
		cle, code, err := enregistrerAgentPerso(k, &f, p, c.Ancien, c.Agent, c.Tous)
		if err != nil {
			s.erreur(w, code, err.Error())
			return
		}
		s.msg(w, cle)
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
	}
}

// enregistrerAgentPerso: a sub-agent of the user's created or changed (ancien: its name before), written to
// extensions/agents.json (the caller holds verrouAgents and read f); the message for the user, or the HTTP
// code and the error.
func enregistrerAgentPerso(k Kit, f *fichierAgents, p Projet, ancien string, a AgentPerso, tous bool) (string, int, error) {
	trouve := func(nom string) int {
		for i, x := range f.Agents {
			if x.Nom == nom && x.duProjet(p) {
				return i
			}
		}
		return -1
	}
	a.Nom = strings.ToLower(strings.TrimSpace(a.Nom))
	a.Description, a.Consignes = strings.TrimSpace(a.Description), strings.TrimSpace(a.Consignes)
	if err := validerAgent(a); err != nil {
		return "", http.StatusBadRequest, err
	}
	a.Projet, a.Origine, a.Cree = p.Dossier, "utilisateur", time.Now().Format("2006-01-02")
	if tous {
		a.Projet = ""
	}
	i := -1
	if ancien != "" {
		if i = trouve(ancien); i < 0 {
			return "", http.StatusNotFound, errors.New("This sub-agent no longer exists.")
		}
		a.Origine, a.Cree = f.Agents[i].Origine, f.Agents[i].Cree // a proposal of the lead stays labelled as such
	}
	if j := trouve(a.Nom); j >= 0 && j != i {
		return "", http.StatusBadRequest, errors.New("A sub-agent already has this name.")
	}
	if i >= 0 {
		f.Agents[i] = a
	} else {
		n := 0
		for _, x := range f.Agents {
			if x.duProjet(p) {
				n++
			}
		}
		if n >= agentsParProjetMax {
			return "", http.StatusBadRequest, errors.New("The team is full: 12 sub-agents of your own at most for a project.")
		}
		f.Agents = append(f.Agents, a)
	}
	// a name removed earlier and created again by the user is no longer refused
	for cle, v := range f.Refuses {
		garde := v[:0]
		for _, n := range v {
			if n != a.Nom || normaliserDossier(cle) != normaliserDossier(p.Dossier) {
				garde = append(garde, n)
			}
		}
		f.Refuses[cle] = garde
	}
	if err := ecrireJSONFichier(cheminAgents(k), *f, 0o644); err != nil {
		return "", http.StatusInternalServerError, err
	}
	return "Sub-agent saved: it joins the team at the next cycle.", 0, nil
}
