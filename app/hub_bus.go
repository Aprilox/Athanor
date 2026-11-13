package main

// The bus between modules (I5): every action a module asks of another goes through s.envoyerBus, which
// checks the recipient exists and declares the action, checks the access (the project is linked for that
// environment), keeps any path into the hub's or another module's data out of the answer (I4), and writes
// the request and its answer to the audit log under the same ID. I6 is checked before the recipient acts:
// on prod, or for an action the recipient says is destructive, the answer is "attente" until the owner
// accepts that very request (accordBus).

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// MessageHub: one message of the bus. Etat and Raison are set on an answer only.
type MessageHub struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"` // demande | reponse | evenement
	De      string         `json:"de"`
	Vers    string         `json:"vers"`
	Action  string         `json:"action"`
	Projet  string         `json:"projet,omitempty"`
	Env     string         `json:"env,omitempty"`
	Mission string         `json:"mission,omitempty"`
	Agent   string         `json:"agent,omitempty"`
	Donnees map[string]any `json:"donnees,omitempty"`
	Etat    string         `json:"etat,omitempty"` // ok | refuse | erreur | attente
	Raison  string         `json:"raison,omitempty"`
}

// recepteurBus: a module that answers on the bus (optional; it enters Module once a second module serves it).
type recepteurBus interface {
	Actions() []string
	Recevoir(s *Serveur, m MessageHub) (MessageHub, error)
}

// risqueBus (optional): "lecture" for an action that changes nothing (no accord, even on prod),
// "destructif" for one that destroys data (an accord in every environment), "" otherwise.
type risqueBus interface{ Risque(action string) string }

// envoyerBus: sends a request to its module and returns the answer (Etat "ok", "refuse", "erreur" or
// "attente": Raison then names the pending approval).
func (s *Serveur) envoyerBus(m MessageHub) MessageHub {
	m.Type, m.Etat, m.Raison = "demande", "", ""
	if m.ID == "" {
		m.ID = aleatoire(8)
	}
	s.auditBus(m)
	rep := s.traiterBus(m)
	rep.ID, rep.Type, rep.De, rep.Vers = m.ID, "reponse", m.Vers, m.De
	rep.Action, rep.Projet, rep.Env, rep.Mission, rep.Agent = m.Action, m.Projet, m.Env, m.Mission, m.Agent
	if rep.Etat == "" {
		rep.Etat = "ok"
	}
	if rep.Etat == "ok" && cheminInterdit(rep.Donnees, m.De) {
		rep.Etat, rep.Raison, rep.Donnees = "refuse", "forbidden path", nil
	}
	if rep.Etat != "ok" {
		rep.Donnees = nil
	}
	s.auditBus(rep)
	return rep
}

// actionsOrchestrateur: what a mission's orchestrator may ask on the bus, by module (reading actions only).
var actionsOrchestrateur = map[string][]string{"dev": {"etat_projet"}}

func refusBus(raison string) MessageHub { return MessageHub{Etat: "refuse", Raison: raison} }

func (s *Serveur) traiterBus(m MessageHub) MessageHub {
	var de, vers Module
	for _, x := range s.modulesTous() {
		if x.ID() == m.De {
			de = x
		}
		if x.ID() == m.Vers {
			vers = x
		}
	}
	switch {
	case m.De == agentOrchestrateur: // a mission's orchestrator: the reading actions of its white list only
		if !slices.Contains(actionsOrchestrateur[m.Vers], m.Action) {
			return refusBus("action not allowed to the orchestrator")
		}
	case strings.HasPrefix(m.De, "client:"): // a paired hub (envoyer_bus): the Servers actions only, I6 below
		if m.Vers != "servers" {
			return refusBus("action not allowed to a client")
		}
	case de == nil:
		return refusBus("unknown sender")
	}
	if vers == nil {
		return refusBus("module absent")
	}
	r, ok := vers.(recepteurBus)
	if !ok || !slices.Contains(r.Actions(), m.Action) {
		return refusBus("action not declared")
	}
	if md, distant := vers.(moduleDistant); distant { // a remote Athanor: its own scope checks the project
		if md.etat != "en_ligne" {
			return refusBus("remote Athanor unreachable")
		}
	} else if strings.HasPrefix(m.De, "client:") {
		// a server is no project: the client's grant of envoyer_bus is its access
	} else if raison := s.accesBus(m); raison != "" {
		return refusBus(raison)
	}
	if rep, attend := s.accordBus(m, vers); attend {
		return rep
	}
	rep, err := r.Recevoir(s, m)
	if err != nil {
		return MessageHub{Etat: "erreur", Raison: err.Error()}
	}
	return rep
}

// accordBus (I6): whether the request must wait for the owner (and the answer to give then). Asked again
// with the same data, it is the same approval; once accepted, it lets that request through once.
func (s *Serveur) accordBus(m MessageHub, vers Module) (MessageHub, bool) {
	risque := ""
	if r, ok := vers.(risqueBus); ok {
		risque = r.Risque(m.Action)
	}
	if risque == "lecture" {
		return MessageHub{}, false
	}
	details := map[string]string{}
	for k, v := range m.Donnees {
		details[k] = s.masquerSecrets(fmt.Sprint(v))
	}
	x, attend, err := s.exigerAccord(Approbation{Projet: m.Projet, Env: m.Env, Mission: m.Mission, Agent: firstNonEmpty(m.Agent, m.De),
		Action: m.Action, Details: details, Raison: m.De + " → " + m.Vers + ": " + m.Action}, risque == "destructif")
	switch {
	case err != nil:
		return MessageHub{Etat: "erreur", Raison: err.Error()}, true
	case attend:
		return MessageHub{Etat: "attente", Raison: "waiting for the owner, id " + x.ID}, true
	}
	return MessageHub{}, false
}

// accesBus: why the project may not be asked about in that environment ("" = it may). A project is
// linked to an environment when it is its current one or when it has a grant there (acces.json).
func (s *Serveur) accesBus(m MessageHub) string {
	s.mu.Lock()
	connu := slices.ContainsFunc(s.config.Projets, func(p Projet) bool { return p.Nom == m.Projet })
	s.mu.Unlock()
	if m.Projet == "" || !connu {
		return "unknown project"
	}
	if s.acces == nil || s.accesErr != nil {
		return "no access"
	}
	pa := s.acces.Projet(m.Projet)
	if m.Env == pa.Env || slices.ContainsFunc(pa.Acces, func(a AccesProjet) bool { return a.Env == m.Env }) {
		return ""
	}
	return "no access"
}

// cheminInterdit: the answer names a path into the hub's data or into another module's than the asker's.
func cheminInterdit(d map[string]any, de string) bool {
	norm := func(p string) string {
		p = filepath.ToSlash(filepath.Clean(p))
		if runtime.GOOS == "windows" {
			p = strings.ToLower(p)
		}
		return p
	}
	base := dossierAthanor()
	hub, mods, sien := norm(filepath.Join(base, "hub")), norm(filepath.Join(base, "modules")), norm(filepath.Join(base, "modules", de))
	var voir func(v any) bool
	voir = func(v any) bool {
		switch x := v.(type) {
		case string:
			p := norm(x)
			return dans(p, hub) || dans(p, mods) && !dans(p, sien)
		case map[string]any:
			for _, y := range x {
				if voir(y) {
					return true
				}
			}
		case []any:
			for _, y := range x {
				if voir(y) {
					return true
				}
			}
		case []string:
			for _, y := range x {
				if voir(y) {
					return true
				}
			}
		}
		return false
	}
	return voir(d)
}

// dans: p holds the path racine or a path under it (".../hub" yes, ".../hubert" no).
func dans(p, racine string) bool {
	for i := strings.Index(p, racine); i >= 0; {
		if j := i + len(racine); j == len(p) || p[j] == '/' {
			return true
		}
		k := strings.Index(p[i+1:], racine)
		if k < 0 {
			return false
		}
		i += 1 + k
	}
	return false
}

// auditBus: a bus message in the audit log (the answer's result: ok, or its refusal and reason).
func (s *Serveur) auditBus(m MessageHub) {
	r := m.Etat
	if m.Raison != "" {
		r += ": " + m.Raison
	}
	s.ecrireAudit(LigneAudit{ID: m.ID, Type: m.Type, De: m.De, Vers: m.Vers, Projet: m.Projet, Env: m.Env,
		Mission: m.Mission, Agent: m.Agent, Action: m.Action, Resultat: r})
}
