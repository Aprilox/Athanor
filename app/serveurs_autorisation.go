package main

// The permission mode of the assistant beside a server's terminal (M244f, note 19h03 §5), kept per server and
// decided here, never in the agent's instructions. The one list of dangerous commands is classerCommande's
// destructive class (deleting, formatting, stopping, permissions, firewall…, serveurs_risque.go).
//   - manuel: every call waits for the owner's accept (a plan), reads included;
//   - auto: reads and changes run at once, a dangerous call or a command of no list (M244k1) waits for the owner;
//   - modifications: the assistant makes a plan and applies it; a plan without a dangerous step nor an unknown
//     command is accepted at once;
//   - ignorer: everything runs, nothing is asked (chosen with a warning and a confirmation).
// It applies only to the tasks launched from the terminal's assistant panel (TacheServeurs.Panneau, set in Go when
// the request names an open terminal of this server opened in the window, M244k2); missions and the sheet's tasks
// keep the plan rules of serveurs_plans.go. "ignorer" is chosen only with a password and the window unlocked.

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

var modesAutorisation = []string{"manuel", "auto", "modifications", "ignorer"}

const (
	decisionLibre        = "libre"        // runs at once
	decisionPlan         = "plan"         // runs as a step of a plan, accepted at once by the mode
	decisionProprietaire = "proprietaire" // waits for the owner's accept
)

// decisionMode: what mode m does with a call of class c, inconnue when it runs a command of no list (an unknown or
// empty mode is manuel).
func decisionMode(m string, c Classe, inconnue bool) string {
	switch {
	case m == "ignorer":
		return decisionLibre
	case c == ClasseDestructrice || inconnue || m != "auto" && m != "modifications":
		return decisionProprietaire
	case m == "modifications" && c == ClasseChangement:
		return decisionPlan
	}
	return decisionLibre
}

// modeDemandeInconnue (M244k7): the modes where an unknown command waits for the owner, a "do not ask again" rule included.
func modeDemandeInconnue(m string) bool { return m == "auto" || m == "modifications" }

// etapeInconnue: e is a free command whose class is only a guess (a command of no list, commandeInconnue).
func etapeInconnue(e Etape) bool {
	if e.Outil != "ssh_commande" || e.Classe != ClasseChangement {
		return false
	}
	var a argsOutilServeur
	return json.Unmarshal(e.Arguments, &a) != nil || commandeInconnue(a.Commande)
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
	propre, env := t.hache == x.Hache, t.t.Env
	t.mu.Unlock()
	if !panneau {
		if !propre {
			return ""
		}
		// M280: a task for a project follows the project's mode. Its own choice comes first; without one, a mission's
		// Servers task still follows its mission (M271, until the missions leave in M302)
		projet, _, _ := strings.Cut(env, "/")
		mission := cmp.Or(missionDeTache(x.Mission), x.Mission)
		if _, err := lireMission(mission); err == nil && mission != "" && (projet == "" || reglagePropre(lireConfig(), projet, "permission") == "") {
			return modeMission(mission)
		}
		if m := modeProjet(projet); projet != "" && m != "manuel" {
			return m
		}
		return "" // Manual: the plan rules of serveurs_plans.go, as for a mission
	}
	srv, _ := serveurProd(id)
	return modeServeur(srv)
}

// missionDeTache (M275b): the mission whose Servers lane is task id, "" for none (a task's session carries the task's
// own id, not its mission's).
func missionDeTache(id string) string {
	for _, m := range lireMissions() {
		if slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Ref == id }) {
			return m.ID
		}
	}
	return ""
}

// modeServeur: srv's mode, manuel when never chosen.
func modeServeur(srv ServeurInventaire) string {
	if slices.Contains(modesAutorisation, srv.Autorisation) {
		return srv.Autorisation
	}
	if d := lireConfig().PermissionsUsage["assistant"]; slices.Contains(modesAutorisation, d) { // M271: Settings › Permissions
		return d
	}
	return "manuel"
}

// modeProjet (M280): the mode of a project's tasks: its own setting, else Settings › Permissions (the "missions" use,
// shown as Projects), else manuel.
func modeProjet(projet string) string { return reglageProjet(projet, "permission") }

// sautIgnorer (M285): projet, one of the owner's projects, is in the Ignorer mode: what would wait for the owner
// (quoi) goes on without asking, and the audit log says so. False in any other mode: the caller asks as before.
func (s *Serveur) sautIgnorer(projet, env, agent, quoi string) bool {
	if projet == "" || modeProjet(projet) != "ignorer" || !slices.Contains(s.nomsProjets(), projet) {
		return false
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: agent, Vers: "hub", Projet: projet, Env: env, Agent: agent,
		Action: quoi, Resultat: "sans demander : mode Ignorer du projet"})
	return true
}

// modeMission (M271): the mode of mission id's Servers lane, its card's or else Settings › Permissions (missions);
// "" for Manual or none: the plan rules of serveurs_plans.go (reads run, each change in a plan the owner accepts).
func modeMission(id string) string {
	m, err := lireMission(id)
	var c CarteMission
	if err != nil || json.Unmarshal(m.Carte, &c) != nil {
		return ""
	}
	if mode := cmp.Or(c.Permissions, lireConfig().PermissionsUsage["missions"]); mode != "manuel" && slices.Contains(modesAutorisation, mode) {
		return mode
	}
	return ""
}

// choixIgnorer (M271): mode is a known mode; "ignorer" only from Athanor's window, confirmed, with the password
// (an agent of this PC could otherwise choose it in the owner's place).
func (s *Serveur) choixIgnorer(r *http.Request, mode string, confirmer bool, mdp string) (int, error) {
	switch {
	case !slices.Contains(modesAutorisation, mode):
		return http.StatusBadRequest, errors.New("Unknown permission mode (manuel, auto, modifications or ignorer)")
	case mode != "ignorer":
		return 0, nil
	case appareilDe(r) != "":
		return http.StatusForbidden, errors.New("Ignore permissions is chosen in Athanor's window")
	case !confirmer:
		return http.StatusConflict, errors.New("Confirm the Ignore permissions mode: nothing will then be asked before a risky action")
	}
	return s.preuvePresence(r, mdp)
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
	// M244k2: the rule of planDecidableFenetre: a password exists and this window is unlocked with it (never asked again)
	s.mu.Lock()
	mdp := s.config.MotDePasseHash != ""
	s.mu.Unlock()
	if c.Mode == "ignorer" && (!mdp || s.verrouillePour(r)) {
		s.erreur(w, http.StatusForbidden, "An agent of this PC could choose Ignore permissions in your place: choose a password for Athanor in Options first, then unlock Athanor with it")
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

// usagesPermissions (M271): the uses Settings › Permissions gives a default mode to.
var usagesPermissions = []string{"assistant", "missions"}

// apiPermissionsReglages: GET /api/reglages/permissions, the default mode of each use ("" = Manual).
func (s *Serveur) apiPermissionsReglages(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p := map[string]string{}
	for _, u := range usagesPermissions {
		p[u] = s.config.PermissionsUsage[u]
	}
	s.mu.Unlock()
	s.ecrireJSON(w, map[string]any{"permissions": p, "modes": modesAutorisation})
}

// apiChoisirPermissions: POST /api/reglages/permissions {usage, mode, confirmer, mot_de_passe}: the default mode of a
// use, applied to every server and mission that has none of its own; "ignorer" as for a mission (choixIgnorer).
func (s *Serveur) apiChoisirPermissions(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Usage      string `json:"usage"`
		Mode       string `json:"mode"`
		Confirmer  bool   `json:"confirmer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil || !slices.Contains(usagesPermissions, c.Usage) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if code, err := s.choixIgnorer(r, c.Mode, c.Confirmer, c.MotDePasse); err != nil {
		s.erreur(w, code, err.Error())
		return
	}
	s.mu.Lock()
	if s.config.PermissionsUsage == nil {
		s.config.PermissionsUsage = map[string]string{}
	}
	s.config.PermissionsUsage[c.Usage] = c.Mode
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "permissions par défaut: " + c.Usage, Resultat: c.Mode})
	s.ecrireJSON(w, map[string]any{"usage": c.Usage, "mode": c.Mode})
}
