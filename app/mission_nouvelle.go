package main

// Missions (3.1, note "simplicité d'abord"): a mission links a Studio project, servers and API keys of the vault,
// with one permission choice ("needs me" = manuel, "autonomous" = auto, "full autonomy" = ignorer, confirmed with
// Athanor's password once). With a project: its servers the project is deployed on, its other servers (a database,
// a worker…) set up as the servers' plan says; launching it is the home's project card launch (create the project,
// its deployment, its settings, start its pilot), or for an existing project its deployment and a new request.
// Without a project: one Servers task on its servers and keys (install, configure, link them). Its state is read
// from the project, its deployment or its task, never kept.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MissionDemande: what the New mission form (or its edit) sends.
type MissionDemande struct {
	Texte   string `json:"texte"`   // what the mission must do
	Plan    string `json:"plan"`    // optional: the steps, added to the project's request (or to the task's text)
	Projet  string `json:"projet"`  // a new project's name, or an existing one's
	Dossier string `json:"dossier"` // a new project's folder ("": next to the other projects)
	// no Studio project: one Servers task on Serveurs and APIs
	SansProjet bool     `json:"sans_projet,omitempty"`
	Serveurs   []string `json:"serveurs"`         // inventory ids or names: where the project is deployed (without a project: every server)
	Autres     []string `json:"autres,omitempty"` // a project's other servers (a database…), never deployed to
	// what a server does, in the owner's words (id or name → text): required for each of Autres, optional otherwise
	Roles   map[string]string `json:"roles,omitempty"`
	APIs    []string          `json:"apis"`              // vault ids or names
	Serveur string            `json:"serveur,omitempty"` // read: one server, as the first 3.1 forms and proposals sent it
	API     string            `json:"api,omitempty"`     // read: one key, likewise
	URL     string            `json:"url"`
	// the servers' plan, in the owner's words (how to put it online, what each server does), and when (jalon |
	// livraison, "": livraison)
	PlanDeploiement string `json:"plan_deploiement"`
	Moment          string `json:"moment"`
	Mode            string `json:"mode"` // manuel ("": needs me) | auto | ignorer (no human action on the servers)
	// a new project only, as on Manage: working modes, GitHub repository, own settings
	Modes      *ModesCarte       `json:"modes,omitempty"`
	GitHub     string            `json:"github,omitempty"`
	Depot      string            `json:"depot,omitempty"`
	SansPousse bool              `json:"sans_pousse,omitempty"`
	Atelier    bool              `json:"atelier,omitempty"`
	Reglages   map[string]string `json:"reglages,omitempty"`
	Index      *int              `json:"index,omitempty"` // the home conversation's message whose proposal this launches
}

// MachineVue: a server of a mission as its frame names it; Site: the project is deployed there (else another role).
type MachineVue struct {
	ID   string `json:"id"`
	Nom  string `json:"nom"`
	Site bool   `json:"site"`
	Role string `json:"role,omitempty"` // what it does, in the owner's words
}

// VueMission: a mission as the Missions page shows it, its project, servers and task read now.
type VueMission struct {
	Mission
	Suivie bool `json:"suivie"` // a mission of this version (an old one is read only)
	// en_cours | arret_jalon | pause | arretee | terminee | perdue (its project is gone); a servers mission:
	// en_cours | attente (waits for the owner's accord) | terminee | echec | arretee
	Statut       string         `json:"statut,omitempty"`
	Avance       VoieMission    `json:"avance"` // milestones done, the one in progress, what the lead does
	Machines     []MachineVue   `json:"machines"`
	ServeurEtat  string         `json:"serveur_etat,omitempty"` // aucun | attente | prepare | deploie | en_ligne | echec | a_preparer | arrete
	ServeurErr   string         `json:"serveur_erreur,omitempty"`
	ServeurMsg   *MessageMoteur `json:"serveur_message,omitempty"`
	ServeurTache bool           `json:"serveur_tache,omitempty"` // a Servers task of the mission runs now
	URL          string         `json:"url,omitempty"`
	APINoms      []string       `json:"api_noms"`
	Bilan        string         `json:"bilan,omitempty"` // a servers mission: what its task answered once over
}

var missionMu sync.Mutex // one change of a mission file at a time

const maxRoleServeur = 200 // a server's role, in the owner's words

func ecrireMission(m Mission) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return ecrireAtomique(filepath.Join(dossierMissions(), m.ID+".json"), b, 0o600)
}

// evenement: what happened to the mission, said by the page in its language (etat: lancee, modifiee, pause,
// arretee, reprise, serveur_arrete, serveur_relance).
func evenement(m *Mission, etat, detail string) {
	m.Historique = append(m.Historique, EvenementMission{T: time.Now().UTC().Format(time.RFC3339), Etat: etat, Detail: detail})
	if n := len(m.Historique); n > 200 {
		m.Historique = m.Historique[n-200:]
	}
}

// texteMission: the request the project's pilot reads: the mission, then its plan when there is one.
func texteMission(texte, plan string) string {
	texte, plan = strings.TrimSpace(texte), strings.TrimSpace(plan)
	if plan == "" {
		return texte
	}
	return texte + "\n\n## Plan\n\n" + plan
}

// texteTacheMission: what a servers mission's task does: the mission, its plan, each server's role, its servers' plan.
func texteTacheMission(texte, plan, roles, planServeurs string) string {
	t := texteMission(texte, plan)
	if roles != "" {
		t += "\n\n## Servers\n\n" + roles
	}
	if p := strings.TrimSpace(planServeurs); p != "" {
		t += "\n\n## Servers plan\n\n" + p
	}
	return t
}

// lignesRoles: one line per server of ids that has a role ("- name: role"), "" when none has.
func lignesRoles(ids []string, roles map[string]string) string {
	var l []string
	for i, n := range nomsServeurs(ids) {
		if r := roles[ids[i]]; r != "" {
			l = append(l, "- "+n+": "+r)
		}
	}
	return strings.Join(l, "\n")
}

// serveursMission, apisDe: m's servers and keys, an early 3.1 file's single ones read too.
func serveursMission(m Mission) []string {
	if len(m.Serveurs) == 0 && m.Serveur != "" {
		return []string{m.Serveur}
	}
	return m.Serveurs
}

func apisDe(m Mission) []string {
	if len(m.APIs) == 0 && m.API != "" {
		return []string{m.API}
	}
	return m.APIs
}

// deploiementMission: the project's deployment as the form asks it (nil: neither server nor API).
func deploiementMission(d MissionDemande) *DeploiementDemande {
	if len(d.Serveurs)+len(d.Autres)+len(d.APIs) == 0 {
		return nil
	}
	x := &DeploiementDemande{URL: strings.TrimSpace(d.URL), Plan: d.PlanDeploiement, Moment: cmp.Or(d.Moment, "livraison"),
		Serveurs: slices.Clone(d.Serveurs), Autres: append([]string{}, d.Autres...), APIs: slices.Clone(d.APIs), Roles: map[string]string{}}
	maps.Copy(x.Roles, d.Roles)
	return x
}

// modesMission: the three choices of a mission for its servers; the SSH assistant's "modifications" is not one.
var modesMission = []string{"manuel", "auto", "ignorer"}

// modeDe: m's permission choice, an early 3.1 file's Autonome read as auto.
func modeDe(m Mission) string {
	switch {
	case m.Mode != "":
		return m.Mode
	case m.Autonome:
		return "auto"
	}
	return "manuel"
}

// choixMission: Ignore permissions (full autonomy) as everywhere: Athanor's window, confirmed, with the password
// (choixIgnorer), asked when it is chosen, never again while it stays; a refusal goes to the audit log.
func (s *Serveur) choixMission(w http.ResponseWriter, r *http.Request, mode, avant string, confirmer bool, mdp, projet string) bool {
	if mode != "ignorer" || avant == "ignorer" {
		return true
	}
	code, err := s.choixIgnorer(r, mode, confirmer, mdp)
	if err == nil {
		return true
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: projet, Action: "mission en autonomie totale", Resultat: "refusée : " + err.Error()})
	s.erreur(w, code, err.Error())
	return false
}

// sansDoublon: l trimmed, each once, in its order, without "".
func sansDoublon(l []string) []string {
	r := []string{}
	for _, x := range l {
		if x = strings.TrimPrefix(strings.TrimSpace(x), "@"); x != "" && !slices.Contains(r, x) {
			r = append(r, x)
		}
	}
	return r
}

// nettoyerDemande: d's fields trimmed, its servers and API keys by id (nommerDeploiement, an unknown one
// refused), checked; an error is an i18n key.
func (s *Serveur) nettoyerDemande(d *MissionDemande) error {
	d.Texte, d.Projet, d.Dossier = strings.TrimSpace(d.Texte), strings.TrimSpace(d.Projet), strings.TrimSpace(d.Dossier)
	d.Serveurs, d.APIs = sansDoublon(append([]string{d.Serveur}, d.Serveurs...)), sansDoublon(append([]string{d.API}, d.APIs...))
	d.Autres, d.Serveur, d.API = sansDoublon(d.Autres), "", ""
	if d.SansProjet { // no project: every server is one of the task's
		d.Serveurs, d.Autres, d.Projet, d.Dossier = sansDoublon(append(d.Serveurs, d.Autres...)), nil, "", ""
	}
	switch {
	case d.Texte == "":
		return erreurEntree("Write what it must do.")
	case utf8.RuneCountInString(texteMission(d.Texte, d.Plan)) > maxTexteAction:
		return erreurEntree("This text is too long (20,000 characters at most).")
	case d.SansProjet && len(d.Serveurs)+len(d.APIs) == 0:
		return erreurEntree("A task needs at least one server or one API")
	case len(d.Autres) > 0 && len(d.Serveurs) == 0:
		return erreurEntree("Choose at least one server where the project is deployed.")
	case d.Moment != "" && d.Moment != "jalon" && d.Moment != "livraison":
		return erreurEntree("Invalid request")
	case d.Mode != "" && !slices.Contains(modesMission, d.Mode):
		return erreurEntree("Unknown permission mode (manuel, auto, modifications or ignorer)")
	case s.coffre != nil && s.coffre.Urgence() != "":
		return erreurEntree("The emergency stop is on: resume Athanor first.")
	}
	x := DeploiementDemande{Serveurs: slices.Concat(d.Serveurs, d.Autres), APIs: d.APIs, Roles: d.Roles}
	noms := s.nommerDeploiement(&x)
	for _, id := range x.Serveurs {
		if _, ok := noms[id]; !ok {
			return errServeurInconnu
		}
	}
	for _, id := range x.APIs {
		if _, ok := noms[id]; !ok {
			return errCoffreInconnu
		}
	}
	d.Serveurs, d.Autres, d.APIs = x.Serveurs[:len(d.Serveurs)], slices.DeleteFunc(slices.Clone(x.Serveurs[len(d.Serveurs):]), func(id string) bool { return slices.Contains(x.Serveurs[:len(d.Serveurs)], id) }), x.APIs
	d.Mode = cmp.Or(d.Mode, "manuel")
	// each server's role: trimmed, secrets masked, only the mission's servers'; one of another role says what it does
	d.Roles = map[string]string{}
	for _, id := range slices.Concat(d.Serveurs, d.Autres) {
		if r := s.masquerSecrets(strings.TrimSpace(x.Roles[id])); r != "" {
			d.Roles[id] = r
		}
	}
	switch {
	case slices.ContainsFunc(d.Autres, func(id string) bool { return d.Roles[id] == "" }):
		return erreurEntree("Say what each server with another role does.")
	case slices.ContainsFunc(slices.Collect(maps.Values(d.Roles)), func(r string) bool { return utf8.RuneCountInString(r) > maxRoleServeur }):
		return erreurEntree("A server's role holds 200 characters at most.")
	case d.SansProjet && utf8.RuneCountInString(texteTacheMission(d.Texte, d.Plan, lignesRoles(d.Serveurs, d.Roles), d.PlanDeploiement)) > maxTexteTache:
		return erreurEntree("A servers mission holds 4,000 characters at most (its text and its plans).")
	}
	return nil
}

// lancerTacheMission: a servers mission's task (native Windows mode: only once its reminders are off, as a card).
func (s *Serveur) lancerTacheMission(m Mission) (TacheServeurs, error) {
	return s.lancerTacheDans(texteTacheMission(m.Phrase, m.Plan, lignesRoles(serveursMission(m), m.Roles), m.PlanServeurs), serveursMission(m), modeNatif.Load() && s.sansRappelsNatif(), "", "", false, "", apisDe(m), modeDe(m))
}

// lancerMission: the mission launched as d says; the mission and what its launch said (an i18n key and its args).
func (s *Serveur) lancerMission(d MissionDemande, qui string) (Mission, string, []string, error) {
	if err := s.nettoyerDemande(&d); err != nil {
		return Mission{}, "", nil, err
	}
	m := Mission{ID: aleatoire(8), Phrase: s.masquerSecrets(d.Texte), Etat: "lancee", Cree: time.Now().UTC().Format(time.RFC3339),
		Serveurs: d.Serveurs, Autres: d.Autres, Roles: d.Roles, APIs: d.APIs, Mode: d.Mode, Plan: s.masquerSecrets(strings.TrimSpace(d.Plan)),
		PlanServeurs: s.masquerSecrets(strings.TrimSpace(d.PlanDeploiement)), Mentions: Mentions{}, Questions: []QuestionMission{}}
	var cle string
	var args []string
	if d.SansProjet {
		t, err := s.lancerTacheMission(m)
		if err != nil {
			return Mission{}, "", nil, err
		}
		m.Tache = t.ID
		cle, args = "Task started on {0}.", []string{strings.Join(nomsServeurs(m.Serveurs), ", "), t.ID}
		evenement(&m, "lancee", "servers task: "+t.ID)
	} else if p, ok := s.projetLocalChat(d.Projet); ok {
		// an existing project: its deployment and permission when the mission links a server or a key, the
		// mission as a new request, its pilot started
		if x := deploiementMission(d); x != nil {
			if err := s.deploiementCarteErr(p.Nom, *x); err != nil {
				return Mission{}, "", nil, err
			}
			if _, err := s.accesPretErr(p.Nom); err != nil {
				return Mission{}, "", nil, err
			}
			if err := s.definirDeploiementProjet(p.Nom, *x, qui); err != nil {
				return Mission{}, "", nil, err
			}
			if _, err := definirReglageProjet(p.Nom, "permission", m.Mode); err != nil {
				return Mission{}, "", nil, err
			}
		}
		titre, _, _ := strings.Cut(d.Texte, "\n")
		if _, _, err := s.ajouterDemande(p, couper(titre, 60), texteMission(d.Texte, d.Plan)); err != nil {
			return Mission{}, "", nil, err
		}
		cle, args = "Request added to the queue of {0}.", []string{p.Nom}
		if !p.PiloteActif() {
			pauserProjet(p.Nom, false)
			noterArret(p.Nom, "")
			msg, err := demarrerPiloteProjet(s, p)
			if err != nil {
				cle, args = "Project “{0}” added, but its pilot did not start: {1}", []string{p.Nom, err.Error()}
			} else {
				cle, args = "Project “{0}” added. {1}", []string{p.Nom, msg}
			}
		}
		m.Projet = p.Nom
		evenement(&m, "lancee", "existing project: "+p.Nom)
	} else {
		a := ActionChat{Type: "projet", Projet: d.Projet, Dossier: d.Dossier, Texte: texteMission(d.Texte, d.Plan), Deploiement: deploiementMission(d),
			Modes: d.Modes, GitHub: d.GitHub, Depot: d.Depot, SansPousse: d.SansPousse, Atelier: d.Atelier, Reglages: d.Reglages}
		if a.Deploiement != nil {
			a.Permission = m.Mode
		}
		if a.Projet == "" {
			return Mission{}, "", nil, erreurEntree("Give the project a name.")
		}
		c := s.carteActionChat(a)
		if c.Erreur != "" {
			return Mission{}, "", nil, erreurEntree(c.Erreur)
		}
		var err error
		if cle, args, err = s.executerActionChat(c, qui, ""); err != nil {
			return Mission{}, "", nil, err
		}
		p, ok := s.projetLocalChat(c.Projet)
		if !ok {
			return Mission{}, "", nil, errors.New("the project was not created")
		}
		m.Projet = p.Nom
		evenement(&m, "lancee", "project created: "+p.Dossier) // what faitMission reads: its deletion may take the folder
	}
	if m.Projet != "" {
		m.Voies = []VoieMission{{Module: "dev", Ref: m.Projet, Etat: "lancee"}}
	}
	missionMu.Lock()
	err := ecrireMission(m)
	missionMu.Unlock()
	if err != nil {
		return Mission{}, "", nil, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: m.Projet, Mission: m.ID, Action: "mission lancée",
		Resultat: s.masquerSecrets(strings.Join(append([]string{cle}, args...), " | "))})
	return m, cle, args, nil
}

// apiLancerMission: POST /api/missions, the New mission form's Launch (the computer only: refuseHubDistance).
func (s *Serveur) apiLancerMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		MissionDemande
		Confirmer  bool   `json:"confirmer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	d := c.MissionDemande
	if !s.choixMission(w, r, d.Mode, "", c.Confirmer, c.MotDePasse, d.Projet) {
		return
	}
	verrouActionsChat.Lock() // a double click, or the home's card launched at once: one mission
	defer verrouActionsChat.Unlock()
	k := s.kitCourant()
	h := projetAccueil(k)
	conv := lireConversation(k, h)
	if i := d.Index; i != nil {
		if *i < 0 || *i >= len(conv.Messages) || conv.Messages[*i].Role != "claude" {
			s.erreur(w, http.StatusBadRequest, "Unknown message")
			return
		}
		if a, ok := lireActionChat(conv.Messages[*i].Texte); !ok || a.Type != "projet" && a.Type != "serveur" {
			s.erreur(w, http.StatusBadRequest, "This message proposes no action")
			return
		}
		if conv.Messages[*i].Action != nil {
			s.erreur(w, http.StatusConflict, "This card was already decided.")
			return
		}
	}
	m, cle, args, err := s.lancerMission(d, deQui(r))
	if err != nil {
		if errors.Is(err, errServeursNatif) {
			s.erreur(w, http.StatusConflict, err.Error())
			return
		}
		s.erreurMissionHTTP(w, err)
		return
	}
	if i := d.Index; i != nil { // the proposal is decided: the conversation says the mission was launched
		conv = lireConversation(k, h)
		if *i < len(conv.Messages) {
			a, _ := lireActionChat(conv.Messages[*i].Texte)
			conv.Messages[*i].Action = &CarteActionChat{ActionChat: ActionChat{Type: a.Type, Projet: m.Projet}, Etat: "lancee", Resultat: "Mission launched: follow it in Missions.", Acces: []string{}}
			ecrireConversation(k, h, conv)
		}
	}
	s.ecrireJSON(w, map[string]any{"mission": s.vueMission(m), "message_key": cle, "args": args})
}

// missionSuivie: mission id of this version, its project (409 when it is gone); a servers mission has none.
func (s *Serveur) missionSuivie(id string) (Mission, Projet, error) {
	m, err := lireMission(id)
	if err != nil {
		return m, Projet{}, err
	}
	switch {
	case m.Projet == "" && m.Tache != "":
		return m, Projet{}, nil
	case m.Projet == "":
		return m, Projet{}, erreurMission{http.StatusConflict, "An old mission is read only: delete it, or launch a new one."}
	}
	p, ok := s.projetLocalChat(m.Projet)
	if !ok {
		return m, p, erreurMission{http.StatusConflict, "Its project no longer exists: delete this mission."}
	}
	return m, p, nil
}

// apiModifierMission: POST /api/missions/{id}, in any state: its servers, its API keys, its permission choice, its
// plans, how and when it goes online; a changed text becomes a new request of the project (its pilot takes it
// after the current one, or at Resume). A servers mission keeps the change for its next launch.
func (s *Serveur) apiModifierMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		MissionDemande
		Confirmer  bool   `json:"confirmer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	d := c.MissionDemande
	missionMu.Lock()
	defer missionMu.Unlock()
	m, p, err := s.missionSuivie(r.PathValue("id"))
	if err == nil {
		d.SansProjet = m.Projet == ""
		err = s.nettoyerDemande(&d)
	}
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	if !s.choixMission(w, r, d.Mode, modeDe(m), c.Confirmer, c.MotDePasse, m.Projet) {
		return
	}
	qui := deQui(r)
	changes := []string{}
	if !slices.Equal(d.Serveurs, serveursMission(m)) || !slices.Equal(d.Autres, m.Autres) || !slices.Equal(d.APIs, apisDe(m)) || d.Mode != modeDe(m) || strings.TrimSpace(d.PlanDeploiement) != m.PlanServeurs || !maps.Equal(d.Roles, m.Roles) {
		changes = append(changes, "servers, roles, keys, plan or permission")
	}
	if m.Projet != "" {
		if x := deploiementMission(d); x != nil || len(serveursMission(m))+len(m.Autres)+len(apisDe(m)) > 0 {
			if x == nil { // all unlinked: the project keeps its address and plan, without server nor key
				e := s.acces.Environnements(p.Nom)[s.deploiementProjet(p.Nom)]
				x = &DeploiementDemande{URL: e.URL, Plan: e.Plan, Moment: cmp.Or(e.Moment, "livraison"), Serveurs: []string{}, Autres: []string{}, APIs: []string{}, Roles: map[string]string{}}
			}
			if _, err := s.accesPretErr(p.Nom); err == nil {
				err = s.definirDeploiementProjet(p.Nom, *x, qui)
			}
			if err != nil {
				s.erreurMissionHTTP(w, err)
				return
			}
			if len(d.Serveurs)+len(d.APIs) > 0 {
				if _, err := definirReglageProjet(p.Nom, "permission", d.Mode); err != nil {
					s.erreurMissionHTTP(w, err)
					return
				}
			}
		}
		if t := s.masquerSecrets(d.Texte); t != m.Phrase || strings.TrimSpace(d.Plan) != m.Plan {
			titre, _, _ := strings.Cut(d.Texte, "\n")
			if _, _, err := s.ajouterDemande(p, couper(titre, 60), texteMission(d.Texte, d.Plan)); err != nil {
				s.erreurMissionHTTP(w, err)
				return
			}
			changes = append(changes, "request added")
		}
	} else if s.masquerSecrets(d.Texte) != m.Phrase || strings.TrimSpace(d.Plan) != m.Plan {
		changes = append(changes, "text")
	}
	m.Phrase, m.Plan, m.PlanServeurs = s.masquerSecrets(d.Texte), s.masquerSecrets(strings.TrimSpace(d.Plan)), s.masquerSecrets(strings.TrimSpace(d.PlanDeploiement))
	m.Serveurs, m.Autres, m.Roles, m.APIs, m.Mode, m.Serveur, m.API, m.Autonome = d.Serveurs, d.Autres, d.Roles, d.APIs, d.Mode, "", "", false
	evenement(&m, "modifiee", strings.Join(changes, ", "))
	if err := ecrireMission(m); err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: m.Projet, Mission: m.ID, Action: "mission modifiée", Resultat: strings.Join(changes, ", ")})
	s.ecrireJSON(w, map[string]any{"mission": s.vueMission(m), "message_key": "Mission saved."})
}

// arreterProjet (M250, M298, M315): p's pilot paused or stopped now (sorte pause | arreter); its deployment waits
// for Start, and a stop also cancels its Servers tasks.
func (s *Serveur) arreterProjet(p Projet, sorte string) error {
	marqueAuto(p, false)
	pauserProjet(p.Nom, true)
	noterArret(p.Nom, map[string]string{"arreter": "arret", "pause": "pause"}[sorte])
	if sorte == "arreter" {
		s.arreterTachesProjet(p.Nom)
	}
	return arreterPiloteProjet(s, p)
}

// relancerServeurMission: the mission's server work again: its first deployment when it has no method yet (as
// Prepare now), else the project's last commit deployed again, in the background.
func (s *Serveur) relancerServeurMission(p Projet) (string, error) {
	env := s.envJalon(p.Nom)
	e := s.acces.Environnements(p.Nom)[env]
	switch {
	case len(e.Serveurs) == 0:
		return "", erreurEntree("This mission has no server.")
	case modeNatif.Load() && !s.sansRappelsNatif():
		return "", errServeursNatif
	case s.prudenceRenforcee(p.Nom, env):
		return "", erreurEntree("This site asks for your password: relaunch it from the project's Deployment tab.")
	}
	if lireRelais()[p.Nom].Pause {
		pauserProjet(p.Nom, false)
		s.preparerProjet(p)
	}
	if e.Deploiement == nil {
		if _, err := s.premierDeploiementProjet(p, modeNatif.Load()); err != nil {
			return "", err
		}
		return "The first deployment is launched.", nil
	}
	head, err := gitSortie(p.Dossier, "rev-parse", "HEAD")
	if err != nil || !reCommitComplet.MatchString(head) {
		return "", erreurEntree("the project has no commit to deploy")
	}
	_, suite, err := s.commencerDeploiement(p.Nom, env, head, OptionsDeploiement{Auteur: "proprietaire", Accorde: true})
	if err != nil {
		return "", err
	}
	go func() {
		ctx, fin := context.WithTimeout(context.Background(), 15*time.Minute)
		defer fin()
		_, _ = suite(ctx)
	}()
	return "Deployment launched again.", nil
}

// agirTacheMission: a servers mission's actions: stop its task (pause and stop are one), or launch it again (its
// text, plans, servers and keys as they are now; never while it runs).
func (s *Serveur) agirTacheMission(m *Mission, action string) (string, string, error) {
	switch action {
	case "pause", "arreter", "serveur-arreter":
		s.arreterTache(m.Tache)
		return "The server's work of this mission is stopped.", "arretee", nil
	case "reprendre", "serveur-relancer":
		if tacheEnCoursID(m.Tache) {
			return "", "", erreurEntree("Its task still works: stop it first.")
		}
		t, err := s.lancerTacheMission(*m)
		if err != nil {
			return "", "", err
		}
		m.Tache = t.ID
		return "Task started on {0}.", "reprise", nil
	}
	return "", "", erreurMission{http.StatusNotFound, "Unknown action"}
}

// apiAgirMission: POST /api/missions/{id}/{action}: pause, arreter, reprendre (its project's pilot, in any
// state), serveur-arreter (its Servers tasks cancelled), serveur-relancer (relancerServeurMission); a servers
// mission: its task stopped or launched again.
func (s *Serveur) apiAgirMission(w http.ResponseWriter, r *http.Request) {
	m, p, err := s.missionSuivie(r.PathValue("id"))
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	action, cle := r.PathValue("action"), ""
	switch {
	case m.Projet == "":
		cle, action, err = s.agirTacheMission(&m, action)
	case action == "pause" || action == "arreter":
		err = s.arreterProjet(p, action)
		cle = map[string]string{"pause": "The pilot is paused: Start picks it up where it stopped.", "arreter": "The pilot is stopped."}[action]
		action = map[string]string{"pause": "pause", "arreter": "arretee"}[action]
	case action == "reprendre":
		pauserProjet(p.Nom, false)
		noterArret(p.Nom, "")
		marqueAuto(p, true) // restarted with the application, as Start does (demarrerPiloteAuto)
		if cle, err = demarrerPiloteProjet(s, p); err != nil && err.Error() == texteLivreRienDeNeuf {
			marqueAuto(p, false)
		}
		action = "reprise"
	case action == "serveur-arreter":
		s.arreterTachesProjet(p.Nom)
		cle, action = "The server's work of this mission is stopped.", "serveur_arrete"
	case action == "serveur-relancer":
		cle, err = s.relancerServeurMission(p)
		action = "serveur_relance"
	default:
		s.erreur(w, http.StatusNotFound, "Unknown action")
		return
	}
	if err != nil {
		var em erreurEntree
		var mi erreurMission
		switch {
		case errors.As(err, &mi):
			s.erreur(w, mi.code, mi.msg)
		case errors.Is(err, errServeursNatif), errors.As(err, &em), err.Error() == texteSansClaude, err.Error() == texteLivreRienDeNeuf:
			s.erreur(w, http.StatusConflict, err.Error())
		default:
			s.erreur(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	missionMu.Lock() // the pilot's start or stop may take a while: the file is read again and written alone
	if lu, err := lireMission(m.ID); err == nil {
		lu.Tache = m.Tache // a servers mission launched again: its new task
		m = lu
	}
	evenement(&m, action, "")
	_ = ecrireMission(m)
	missionMu.Unlock()
	args := []string{}
	if cle == "Task started on {0}." {
		args = []string{strings.Join(nomsServeurs(serveursMission(m)), ", ")}
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: m.Projet, Mission: m.ID, Action: "mission: " + action, Resultat: cle})
	s.ecrireJSON(w, map[string]any{"mission": s.vueMission(m), "message_key": cle, "args": args})
}

// machines: the frame's servers: where the project is deployed, then the others, each with its role.
func machines(site, autres []string, roles map[string]string) []MachineVue {
	l := []MachineVue{}
	for i, n := range nomsServeurs(site) {
		l = append(l, MachineVue{ID: site[i], Nom: n, Site: true, Role: roles[site[i]]})
	}
	for i, n := range nomsServeurs(autres) {
		l = append(l, MachineVue{ID: autres[i], Nom: n, Role: roles[autres[i]]})
	}
	return l
}

// nomsAPIs: the vault names of ids (a removed entry is left out).
func (s *Serveur) nomsAPIs(ids []string) []string {
	noms := []string{}
	if s.coffre == nil {
		return noms
	}
	for _, x := range s.coffre.Liste() {
		if slices.Contains(ids, x.ID) {
			noms = append(noms, x.Nom)
		}
	}
	return noms
}

// vueMission: m as the page shows it, its project, servers and task read now.
func (s *Serveur) vueMission(m Mission) VueMission {
	v := VueMission{Mission: m, Suivie: m.Projet != "" || m.Tache != "", Machines: []MachineVue{}, APINoms: []string{}}
	if !v.Suivie {
		v.Mission = s.voiesEnDirect(m)
		return v
	}
	v.Mode, v.Autonome, v.Serveurs, v.APIs, v.Serveur, v.API = modeDe(m), false, serveursMission(m), apisDe(m), "", ""
	v.APINoms = s.nomsAPIs(v.APIs)
	if m.Projet == "" { // a servers mission: its task
		v.Machines = machines(nil, v.Serveurs, v.Roles)
		t, ok := tacheMission(m.Tache)
		switch {
		case !ok:
			v.Statut = "arretee"
		case t.Etat == "finie":
			v.Statut = "terminee"
		case t.Etat == "echec" || t.Etat == "arretee":
			v.Statut = t.Etat
		case t.Etat == "attente_accord":
			v.Statut, v.ServeurTache = "attente", true
		default:
			v.Statut, v.ServeurTache = "en_cours", true
		}
		if ok && tacheFinieEtat(t.Etat) {
			v.Bilan = couper(s.masquerSecrets(t.Resultat), 1200)
		}
		return v
	}
	p, ok := s.projetParNom(m.Projet)
	if !ok {
		v.Statut = "perdue"
		return v
	}
	e, lu := p.LireEtat()
	r := lireRelais()[p.Nom]
	v.Avance = VoieMission{Module: "dev", Ref: p.Nom}
	switch {
	case p.PiloteActif() && r.Arret == "jalon":
		v.Statut, v.Avance.Actuel = "arret_jalon", "travaille"
	case p.PiloteActif():
		v.Statut, v.Avance.Actuel = "en_cours", "travaille"
	case lu && e.Phase == "termine":
		v.Statut = "terminee"
	case r.Arret == "pause":
		v.Statut = "pause"
	default:
		v.Statut = "arretee"
	}
	s.avancementVoie(&v.Avance, p, e.JalonEnCours)
	if s.acces == nil {
		return v
	}
	// the project's deployment is the truth: changed on its tab, it shows here too
	dep := s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)]
	v.URL, v.Serveurs, v.Autres, v.Roles, v.Machines, v.PlanServeurs = dep.URL, dep.Serveurs, dep.Autres, dep.Roles, machines(dep.Serveurs, dep.Autres, dep.Roles), dep.Plan
	if len(dep.APIs) > 0 {
		v.APIs, v.APINoms = dep.APIs, s.nomsAPIs(dep.APIs)
	}
	if len(dep.Serveurs) == 0 {
		v.ServeurEtat = "aucun"
		return v
	}
	v.ServeurTache = r.Preparation != nil && tacheEnCoursID(r.Preparation.Tache) || tacheEnCoursID(r.Premier)
	rel := s.etatRelais(p.Nom)
	switch {
	case r.Preparation != nil && tacheEnCoursID(r.Preparation.Tache):
		v.ServeurEtat = "prepare"
	case rel == nil && r.Pause:
		v.ServeurEtat = "arrete"
	case rel == nil:
		v.ServeurEtat = "attente"
	default:
		v.ServeurEtat = map[string]string{"preparation": "deploie", "a_preparer": "a_preparer", "en_cours": "deploie", "lance": "deploie",
			"ok": "en_ligne", "revenu": "en_ligne", "echec": "echec", "attente": "attente", "ailleurs": "attente"}[rel.Etat]
		if v.ServeurEtat == "" {
			v.ServeurEtat = "attente"
		}
		if v.ServeurEtat == "echec" {
			v.ServeurErr, v.ServeurMsg = s.masquerSecrets(rel.Erreur), rel.Message
		}
	}
	if v.ServeurEtat == "attente" && r.Preparation != nil {
		if t, ok := tacheMission(r.Preparation.Tache); ok && t.Etat == "echec" {
			v.ServeurEtat, v.ServeurErr = "echec", couper(s.masquerSecrets(t.Resultat), 400)
		} else if r.Preparation.Erreur != "" {
			v.ServeurErr = r.Preparation.Erreur
		}
	}
	return v
}

// tacheFinieEtat: a Servers task's state that no longer changes.
func tacheFinieEtat(etat string) bool {
	return slices.Contains([]string{"finie", "echec", "arretee"}, etat)
}
