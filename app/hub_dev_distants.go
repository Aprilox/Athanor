package main

// Several Devs under one hub (ARCHITECTURE.md §Ouverture, Plusieurs Dev): a remote Athanor that carries dev
// brings its projects (its projets tool, read with its state every 15 s). They show in the Dev tile and the
// Projects overview as nom@instance (greyed, last state known, when the remote is offline), an @mention finds
// them, and the owner starts, pauses at the end of its cycle or stops their pilot from here: through the bus
// (audited here), then the remote's pilote tool, its own scope checked there. What it sends back is data.

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ProjetDistant: one project of a remote Athanor, as its projets tool lists it (data, never an order).
type ProjetDistant struct {
	Nom         string `json:"nom"`
	PiloteActif bool   `json:"pilote_actif"`
	Phase       string `json:"phase"`
	Plan        string `json:"plan"` // "3/10 milestones"
	Jalon       string `json:"jalon"`
	Besoin      string `json:"besoin"`
}

// maxProjetsDistant: what one remote may bring at most (a remote cannot flood the hub's pages).
const maxProjetsDistant = 200

// lireProjetsDistant: the projects of a remote that answered and carries dev with the projets tool granted.
// lu = false: keep the last list (the remote did not answer); a remote without dev or projets has none.
func (s *Serveur) lireProjetsDistant(d Distant, repond bool, outils, modules []string) (l []ProjetDistant, lu bool) {
	if !repond {
		return nil, false
	}
	if !slices.Contains(modules, "dev") || !slices.Contains(outils, "projets") {
		return nil, true
	}
	texte, err := s.outilDistant(d, "projets", nil)
	var v struct{ Projets []ProjetDistant }
	if err != nil || json.Unmarshal([]byte(texte), &v) != nil {
		return nil, false
	}
	for _, p := range v.Projets {
		if p.Nom = strings.TrimSpace(couper(p.Nom, 80)); p.Nom != "" && len(l) < maxProjetsDistant {
			p.Jalon, p.Plan, p.Phase, p.Besoin = couper(p.Jalon, 200), couper(p.Plan, 40), couper(p.Phase, 40), couper(p.Besoin, 40)
			l = append(l, p)
		}
	}
	return l, true
}

// devDistant: a remote with its projects, as last read.
type devDistant struct {
	d       Distant
	enLigne bool
	projets []ProjetDistant
}

func (s *Serveur) devDistants() []devDistant {
	m := s.dst()
	m.mu.Lock()
	defer m.mu.Unlock()
	var l []devDistant
	for _, d := range m.liste {
		if ps := m.projets[d.ID]; len(ps) > 0 {
			l = append(l, devDistant{d: d, enLigne: m.etats[d.ID] == "en_ligne", projets: ps})
		}
	}
	return l
}

// elementsDevDistants: the remotes' projects as Dev elements, nom@instance; an offline remote's are greyed.
func (s *Serveur) elementsDevDistants() []ElementHub {
	var out []ElementHub
	for _, x := range s.devDistants() {
		for _, p := range x.projets {
			e := ElementHub{ID: x.d.ID + "/" + p.Nom, Titre: p.Nom + "@" + x.d.Nom, Etat: "inactif", Lien: "#global",
				Distant: x.d.ID, NomDistant: p.Nom, HorsLigne: !x.enLigne, Avancement: &Avancement{Texte: "No plan yet"}}
			var fait, total int
			if n, _ := fmt.Sscanf(p.Plan, "%d/%d", &fait, &total); n == 2 && total > 0 && fait >= 0 && fait <= total {
				e.Avancement = &Avancement{Fait: fait, Total: total, Texte: "{0} / {1} milestones done", Etape: "Milestone {0}/{1}"}
			}
			switch {
			case e.HorsLigne:
			case p.PiloteActif:
				e.Etat, e.Activite = "actif", p.Jalon
			case p.Phase == "termine" || total > 0 && fait == total:
				e.Etat = "termine"
			}
			if p.Besoin != "" && !e.HorsLigne {
				e.Attente = &AttenteElement{Raison: "Waits for you on {0}", Args: []any{x.d.Nom}, Page: "#global", Cle: "distant:" + e.ID + ":" + p.Besoin}
			}
			out = append(out, e)
		}
	}
	return out
}

// ordresPilote: what the hub may ask a remote pilot (the remote's pilote tool).
var ordresPilote = []string{"demarrer", "pause_fin_cycle", "arreter"}

// piloteDistant: a remote project's pilot started, paused at the end of its cycle or stopped, through the bus
// (audited here; refused when the remote did not grant pilote or is offline), then that remote read again.
func (s *Serveur) piloteDistant(id, projet, ordre string) error {
	if !slices.Contains(ordresPilote, ordre) {
		return errors.New("Unknown order.")
	}
	connu := false
	for _, x := range s.devDistants() {
		connu = connu || x.d.ID == id && slices.ContainsFunc(x.projets, func(p ProjetDistant) bool { return p.Nom == projet })
	}
	if !connu {
		return errors.New("Unknown remote project.")
	}
	rep := s.envoyerBus(MessageHub{De: "hub", Vers: "distant-" + id, Action: "pilote", Projet: projet, Donnees: map[string]any{"action": ordre}})
	if rep.Etat != "ok" {
		return errors.New(firstNonEmpty(rep.Raison, rep.Etat))
	}
	s.rafraichirDistant(id)
	return nil
}
