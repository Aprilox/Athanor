package main

// The permission mode of the assistant beside a server's terminal (M244f, note 19h03 §5), kept per server and
// decided here, never in the agent's instructions. The one list of dangerous commands is classerCommande's
// destructive class (deleting, formatting, stopping, permissions, firewall…, serveurs_risque.go).
//   - manuel: every call waits for the owner's accept (a plan), reads included;
//   - auto: reads and changes run at once, a dangerous call waits for the owner;
//   - modifications: the assistant makes a plan and applies it; a plan without a dangerous step is accepted at once;
//   - ignorer: everything runs, nothing is asked (chosen with a warning and a confirmation).
// It applies only to the tasks launched from the terminal's assistant panel (TacheServeurs.Panneau); missions and
// the sheet's tasks keep the plan rules of serveurs_plans.go.

import (
	"net/http"
	"slices"
)

var modesAutorisation = []string{"manuel", "auto", "modifications", "ignorer"}

const (
	decisionLibre        = "libre"        // runs at once
	decisionPlan         = "plan"         // runs as a step of a plan, accepted at once by the mode
	decisionProprietaire = "proprietaire" // waits for the owner's accept
)

// decisionMode: what mode m does with a call of class c (an unknown or empty mode is manuel).
func decisionMode(m string, c Classe) string {
	switch {
	case m == "ignorer":
		return decisionLibre
	case c == ClasseDestructrice || m != "auto" && m != "modifications":
		return decisionProprietaire
	case m == "modifications" && c == ClasseChangement:
		return decisionPlan
	}
	return decisionLibre
}

// modeAutorisation: the mode of server id when x is the session of a task launched from its terminal's assistant
// panel, else "" (the plan rules as before).
func modeAutorisation(x SessionHub, id string) string {
	t := tacheDeSession(x.Hache)
	if t == nil {
		return ""
	}
	t.mu.Lock()
	panneau := t.t.Panneau && t.hache == x.Hache // the critic's or the verifier's session runs nothing
	t.mu.Unlock()
	if !panneau {
		return ""
	}
	srv, _ := serveurProd(id)
	return modeServeur(srv)
}

// modeServeur: srv's mode, manuel when never chosen.
func modeServeur(srv ServeurInventaire) string {
	if slices.Contains(modesAutorisation, srv.Autorisation) {
		return srv.Autorisation
	}
	return "manuel"
}

// apiAutorisationServeur: POST /api/serveurs/{id}/autorisation {mode, confirmer}: the assistant's mode for this
// server, from Athanor's window only; "ignorer" needs confirmer (the page's warning was read).
func (s *Serveur) apiAutorisationServeur(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Mode      string `json:"mode"`
		Confirmer bool   `json:"confirmer"`
	}
	if lireCorps(r, &c) != nil || !slices.Contains(modesAutorisation, c.Mode) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if appareilDe(r) != "" {
		s.erreur(w, http.StatusForbidden, "The assistant's mode is chosen in Athanor's window")
		return
	}
	if c.Mode == "ignorer" && !c.Confirmer {
		s.erreur(w, http.StatusConflict, "Confirm the Ignore permissions mode: the assistant will then do everything without asking")
		return
	}
	id := r.PathValue("id")
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return nil, errServeurInconnu
		}
		l[i].Autorisation = c.Mode
		return l, nil
	})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "serveur:" + id, Action: "mode de l'assistant", Resultat: c.Mode})
	s.ecrireJSON(w, map[string]any{"autorisation": c.Mode})
}
