package main

// The library of sub-agents: the ones the user creates (Team page, Chat) and the ones a project's lead
// proposes (the pilot imports them, outil_equipe.go), each switched on in the projects that use it. They
// live in extensions/agents.json, shared with the pilot, which passes a project's ones to every Claude
// session of it (lead and parallel workers).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// AgentPerso: a sub-agent of the library (same fields as the pilot reads, outil_equipe.go).
type AgentPerso struct {
	Nom          string   `json:"nom"`
	Description  string   `json:"description"`
	Consignes    string   `json:"consignes"`
	Modele       string   `json:"modele"` // "" as the other sub-agents, "eco" cheap model, "fort" opus
	LectureSeule bool     `json:"lecture_seule"`
	Projets      []string `json:"projets"`          // the folders of the projects that use it (never null once written)
	Projet       string   `json:"projet,omitempty"` // before 2.49.9, and a lead's proposal as the pilot imports it: its project ("" = every project)
	Origine      string   `json:"origine"`          // utilisateur | chef
	Cree         string   `json:"cree,omitempty"`
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

func cheminAgents(k Kit) string { return filepath.Join(dossierExt(k), "agents.json") }

// lireAgents: the library; a lead's proposal (its "projet") is switched on in its project. A sub-agent of
// the former form "for every project" keeps Projets nil until MigrerAgents switches it on everywhere.
func lireAgents(k Kit) fichierAgents {
	var f fichierAgents
	lireJSONFichier(cheminAgents(k), &f)
	if f.Refuses == nil {
		f.Refuses = map[string][]string{}
	}
	for i, a := range f.Agents {
		if a.Projets == nil && a.Projet != "" {
			f.Agents[i].Projets = []string{a.Projet}
		}
		f.Agents[i].Projet = ""
	}
	return f
}

func ecrireAgents(k Kit, f fichierAgents) error {
	for i := range f.Agents {
		if f.Agents[i].Projets == nil { // null would read as the former "every project" for the pilot
			f.Agents[i].Projets = []string{}
		}
	}
	return ecrireJSONFichier(cheminAgents(k), f, 0o644)
}

// MigrerAgents (2.49.9): extensions/agents.json becomes a library of sub-agents switched on per project.
// One "for every project" is switched on in each project of the list; a name several projects had becomes
// one sub-agent (same content) or several (name-2, name-3…). Nothing is written when nothing changes.
func (s *Serveur) MigrerAgents() {
	k := s.kitCourant()
	verrouAgents.Lock()
	defer verrouAgents.Unlock()
	if !agentsLisibles(k) {
		return
	}
	f := lireAgents(k)
	s.mu.Lock()
	var tous []string
	for _, p := range s.config.Projets {
		tous = append(tous, p.Dossier)
	}
	s.mu.Unlock()
	pris := map[string]bool{}
	for _, a := range f.Agents {
		pris[a.Nom] = true
	}
	garde, place, change := []AgentPerso{}, map[string]int{}, false
	for _, a := range f.Agents {
		if a.Projets == nil {
			a.Projets, change = append([]string{}, tous...), true
		}
		if j, ok := place[a.Nom]; ok {
			change = true
			b := &garde[j]
			if b.Description == a.Description && b.Consignes == a.Consignes && b.Modele == a.Modele && b.LectureSeule == a.LectureSeule {
				for _, d := range a.Projets {
					if !b.duDossier(d) {
						b.Projets = append(b.Projets, d)
					}
				}
				continue
			}
			a.Nom = nomAgentLibre(a.Nom, pris)
		}
		place[a.Nom] = len(garde)
		garde = append(garde, a)
	}
	if change {
		f.Agents = garde
		_ = ecrireAgents(k, f)
	}
}

// nomAgentLibre: nom-2, nom-3… the first one no sub-agent has (31 characters at most).
func nomAgentLibre(nom string, pris map[string]bool) string {
	for i := 2; ; i++ {
		suffixe := fmt.Sprintf("-%d", i)
		n := tronquerRunes(nom, 31-len(suffixe)) + suffixe
		if !pris[n] {
			pris[n] = true
			return n
		}
	}
}

// dossierAgents: a project moved to the folder cible, or left Athanor (cible ""): the sub-agents it uses
// and the proposals refused in it follow (the library keeps the sub-agents).
func dossierAgents(k Kit, ancien, cible string) {
	verrouAgents.Lock()
	defer verrouAgents.Unlock()
	if !agentsLisibles(k) {
		return
	}
	f := lireAgents(k)
	change := false
	for i := range f.Agents {
		a := &f.Agents[i]
		if a.duDossier(ancien) {
			a.retirerDossier(ancien)
			if cible != "" {
				a.Projets = append(a.Projets, cible)
			}
			change = true
		}
	}
	for cle, v := range f.Refuses {
		if normaliserDossier(cle) == normaliserDossier(ancien) {
			delete(f.Refuses, cle)
			if cible != "" {
				f.Refuses[cible] = v
			}
			change = true
		}
	}
	if change {
		_ = ecrireAgents(k, f)
	}
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

// duProjet: the sub-agent is switched on in this project.
func (a AgentPerso) duProjet(p Projet) bool { return a.duDossier(p.Dossier) }

func (a AgentPerso) duDossier(d string) bool {
	for _, x := range a.Projets {
		if normaliserDossier(x) == normaliserDossier(d) {
			return true
		}
	}
	return false
}

func (a *AgentPerso) retirerDossier(d string) {
	garde := []string{}
	for _, x := range a.Projets {
		if normaliserDossier(x) != normaliserDossier(d) {
			garde = append(garde, x)
		}
	}
	a.Projets = garde
}

// refuserAgent: a proposal of the lead the user removed from a project: never proposed again there.
func (f *fichierAgents) refuserAgent(dossier, nom string) {
	for cle := range f.Refuses {
		if normaliserDossier(cle) == normaliserDossier(dossier) {
			dossier = cle
		}
	}
	if !contient(f.Refuses[dossier], nom) {
		f.Refuses[dossier] = append(f.Refuses[dossier], nom)
	}
}

// accepterAgent: the user switched this name on, or created it, in the project: no longer refused there.
func (f *fichierAgents) accepterAgent(dossier, nom string) {
	for cle, v := range f.Refuses {
		if normaliserDossier(cle) != normaliserDossier(dossier) {
			continue
		}
		garde := []string{}
		for _, n := range v {
			if n != nom {
				garde = append(garde, n)
			}
		}
		f.Refuses[cle] = garde
	}
}

// AgentBiblio: a sub-agent of the library as the Team page shows it, switched on or not in its project.
type AgentBiblio struct {
	AgentPerso
	Actif bool `json:"actif"`
}

func (s *Serveur) bibliothequeAgents(p Projet) []AgentBiblio {
	l := []AgentBiblio{}
	for _, a := range lireAgents(s.kitCourant()).Agents {
		if validerAgent(a) == nil {
			l = append(l, AgentBiblio{a, a.duProjet(p)})
		}
	}
	return l
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
		if a.duProjet(p) && validerAgent(a) == nil {
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
		Action string     `json:"action"` // enregistrer | supprimer (from the library) | activer (in this project)
		Ancien string     `json:"ancien"` // its name (before an edit)
		Agent  AgentPerso `json:"agent"`
		Actif  bool       `json:"actif"` // activer: on or off
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
	i := f.trouver(c.Ancien)
	if c.Action != "enregistrer" && i < 0 {
		s.erreur(w, http.StatusNotFound, "This sub-agent no longer exists.")
		return
	}
	cle := ""
	switch c.Action {
	case "supprimer":
		a := f.Agents[i]
		f.Agents = append(f.Agents[:i], f.Agents[i+1:]...)
		if a.Origine == "chef" { // the lead never proposes it again in the projects that used it
			for _, d := range a.Projets {
				f.refuserAgent(d, a.Nom)
			}
		}
		cle = "Sub-agent deleted from the library: it leaves its projects' teams at the next cycle."
	case "activer":
		a := &f.Agents[i]
		if !c.Actif {
			a.retirerDossier(p.Dossier)
			if a.Origine == "chef" {
				f.refuserAgent(p.Dossier, a.Nom)
			}
			cle = "Switched off in this project: it leaves the team at the next cycle."
			break
		}
		if !a.duProjet(p) {
			a.Projets = append(a.Projets, p.Dossier)
		}
		f.accepterAgent(p.Dossier, a.Nom)
		cle = "Sub-agent saved: it joins the team at the next cycle."
	case "enregistrer":
		cle, code, err := enregistrerAgentPerso(k, &f, p, c.Ancien, c.Agent)
		if err != nil {
			s.erreur(w, code, err.Error())
			return
		}
		s.msg(w, cle)
		return
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	if err := ecrireAgents(k, f); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, cle)
}

// trouver: the index of the library's sub-agent of this name, -1 when none.
func (f fichierAgents) trouver(nom string) int {
	for i, a := range f.Agents {
		if nom != "" && a.Nom == nom {
			return i
		}
	}
	return -1
}

// enregistrerAgentPerso: a sub-agent of the library created (switched on in the project p) or changed
// (ancien: its name before; its projects stay), written to extensions/agents.json (the caller holds
// verrouAgents and read f); the message for the user, or the HTTP code and the error.
func enregistrerAgentPerso(k Kit, f *fichierAgents, p Projet, ancien string, a AgentPerso) (string, int, error) {
	a.Nom = strings.ToLower(strings.TrimSpace(a.Nom))
	a.Description, a.Consignes = strings.TrimSpace(a.Description), strings.TrimSpace(a.Consignes)
	if err := validerAgent(a); err != nil {
		return "", http.StatusBadRequest, err
	}
	a.Projets, a.Projet, a.Origine, a.Cree = []string{p.Dossier}, "", "utilisateur", time.Now().Format("2006-01-02")
	i := -1
	if ancien != "" {
		if i = f.trouver(ancien); i < 0 {
			return "", http.StatusNotFound, errors.New("This sub-agent no longer exists.")
		}
		a.Origine, a.Cree, a.Projets = f.Agents[i].Origine, f.Agents[i].Cree, f.Agents[i].Projets // a proposal of the lead stays labelled as such
	}
	if j := f.trouver(a.Nom); j >= 0 && j != i {
		return "", http.StatusBadRequest, errors.New("A sub-agent already has this name.")
	}
	if i >= 0 {
		f.Agents[i] = a
	} else {
		f.Agents = append(f.Agents, a)
	}
	f.accepterAgent(p.Dossier, a.Nom) // a name removed earlier and created again by the user is no longer refused
	if err := ecrireAgents(k, *f); err != nil {
		return "", http.StatusInternalServerError, err
	}
	return "Sub-agent saved: it joins the team at the next cycle.", 0, nil
}
