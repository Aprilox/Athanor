package main

// Missions (3.1, note "simplicité d'abord"): a mission links a Studio project, at most one server and one API key
// of the vault, with one permission choice ("needs me" = manuel, "autonomous" = auto). Launching it is the home's
// project card launch (create the project, its deployment, its settings, start its pilot), or for an existing
// project its deployment and a new request. Its state is read from the project and its deployment, never kept.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MissionDemande: what the New mission form (or its edit) sends.
type MissionDemande struct {
	Texte   string `json:"texte"`   // what the mission must do
	Plan    string `json:"plan"`    // optional: added to the project's request
	Projet  string `json:"projet"`  // a new project's name, or an existing one's
	Dossier string `json:"dossier"` // a new project's folder ("": next to the other projects)
	Serveur string `json:"serveur"` // inventory id or name ("": none)
	API     string `json:"api"`     // vault id or name ("": none)
	URL     string `json:"url"`
	// how to put it online, in the owner's words, and when (jalon | livraison, "": livraison)
	PlanDeploiement string `json:"plan_deploiement"`
	Moment          string `json:"moment"`
	Autonome        bool   `json:"autonome"`
	// a new project only, as on Manage: working modes, GitHub repository, own settings
	Modes      *ModesCarte       `json:"modes,omitempty"`
	GitHub     string            `json:"github,omitempty"`
	Depot      string            `json:"depot,omitempty"`
	SansPousse bool              `json:"sans_pousse,omitempty"`
	Atelier    bool              `json:"atelier,omitempty"`
	Reglages   map[string]string `json:"reglages,omitempty"`
	Index      *int              `json:"index,omitempty"` // the home conversation's message whose proposal this launches
}

// VueMission: a mission as the Missions page shows it, its project and server read now.
type VueMission struct {
	Mission
	Suivie bool `json:"suivie"` // a mission of this version (an old one is read only)
	// en_cours | arret_jalon | pause | arretee | terminee | perdue (its project is gone)
	Statut       string         `json:"statut,omitempty"`
	Avance       VoieMission    `json:"avance"` // milestones done, the one in progress, what the lead does
	ServeurNom   string         `json:"serveur_nom,omitempty"`
	ServeurEtat  string         `json:"serveur_etat,omitempty"` // aucun | attente | prepare | deploie | en_ligne | echec | a_preparer | arrete
	ServeurErr   string         `json:"serveur_erreur,omitempty"`
	ServeurMsg   *MessageMoteur `json:"serveur_message,omitempty"`
	ServeurTache bool           `json:"serveur_tache,omitempty"` // a Servers task of the mission runs now
	URL          string         `json:"url,omitempty"`
	APINom       string         `json:"api_nom,omitempty"`
}

var missionMu sync.Mutex // one change of a mission file at a time

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

// deploiementMission: the project's deployment as the form asks it (nil: neither server nor API).
func deploiementMission(d MissionDemande) *DeploiementDemande {
	if d.Serveur == "" && d.API == "" {
		return nil
	}
	x := &DeploiementDemande{URL: strings.TrimSpace(d.URL), Plan: d.PlanDeploiement, Moment: d.Moment, Serveurs: []string{}, APIs: []string{}}
	if d.Serveur != "" {
		x.Serveurs = []string{d.Serveur}
	}
	if d.API != "" {
		x.APIs = []string{d.API}
	}
	if x.Moment == "" {
		x.Moment = "livraison"
	}
	return x
}

func modeMission(autonome bool) string {
	if autonome {
		return "auto"
	}
	return "manuel"
}

// nettoyerDemande: d's fields trimmed, its server and API by id (nommerDeploiement), checked; an error is an
// i18n key.
func (s *Serveur) nettoyerDemande(d *MissionDemande) error {
	d.Texte, d.Projet, d.Dossier = strings.TrimSpace(d.Texte), strings.TrimSpace(d.Projet), strings.TrimSpace(d.Dossier)
	d.Serveur, d.API = strings.TrimSpace(d.Serveur), strings.TrimSpace(d.API)
	switch {
	case d.Texte == "":
		return erreurEntree("Write what it must do.")
	case utf8.RuneCountInString(texteMission(d.Texte, d.Plan)) > maxTexteAction:
		return erreurEntree("This text is too long (20,000 characters at most).")
	case d.Moment != "" && d.Moment != "jalon" && d.Moment != "livraison":
		return erreurEntree("Invalid request")
	case s.coffre != nil && s.coffre.Urgence() != "":
		return erreurEntree("The emergency stop is on: resume Athanor first.")
	}
	if x := deploiementMission(*d); x != nil {
		noms := s.nommerDeploiement(x)
		if len(x.Serveurs) > 0 {
			if _, ok := noms[x.Serveurs[0]]; !ok {
				return errServeurInconnu
			}
			d.Serveur = x.Serveurs[0]
		}
		if len(x.APIs) > 0 {
			if _, ok := noms[x.APIs[0]]; !ok {
				return errCoffreInconnu
			}
			d.API = x.APIs[0]
		}
	}
	return nil
}

// lancerMission: the mission launched as d says; the mission and what its launch said (an i18n key and its args).
func (s *Serveur) lancerMission(d MissionDemande, qui string) (Mission, string, []string, error) {
	if err := s.nettoyerDemande(&d); err != nil {
		return Mission{}, "", nil, err
	}
	m := Mission{ID: aleatoire(8), Phrase: s.masquerSecrets(d.Texte), Etat: "lancee", Cree: time.Now().UTC().Format(time.RFC3339),
		Serveur: d.Serveur, API: d.API, Autonome: d.Autonome, Mentions: Mentions{}, Questions: []QuestionMission{}}
	var cle string
	var args []string
	if p, ok := s.projetLocalChat(d.Projet); ok {
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
			if _, err := definirReglageProjet(p.Nom, "permission", modeMission(d.Autonome)); err != nil {
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
			a.Permission = modeMission(d.Autonome)
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
	m.Voies = []VoieMission{{Module: "dev", Ref: m.Projet, Etat: "lancee"}}
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
	var d MissionDemande
	if lireCorps(r, &d) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
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
		if a, ok := lireActionChat(conv.Messages[*i].Texte); !ok || a.Type != "projet" {
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

// missionSuivie: mission id of this version, its project (409 when it is gone).
func (s *Serveur) missionSuivie(id string) (Mission, Projet, error) {
	m, err := lireMission(id)
	if err != nil {
		return m, Projet{}, err
	}
	if m.Projet == "" {
		return m, Projet{}, erreurMission{http.StatusConflict, "An old mission is read only: delete it, or launch a new one."}
	}
	p, ok := s.projetLocalChat(m.Projet)
	if !ok {
		return m, p, erreurMission{http.StatusConflict, "Its project no longer exists: delete this mission."}
	}
	return m, p, nil
}

// apiModifierMission: POST /api/missions/{id}, in any state: its server, its API key, its permission choice, how
// and when it goes online; a changed text becomes a new request of the project (its pilot takes it after the
// current one, or at Resume).
func (s *Serveur) apiModifierMission(w http.ResponseWriter, r *http.Request) {
	var d MissionDemande
	if lireCorps(r, &d) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	missionMu.Lock()
	defer missionMu.Unlock()
	m, p, err := s.missionSuivie(r.PathValue("id"))
	if err == nil {
		err = s.nettoyerDemande(&d)
	}
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	qui := deQui(r)
	if d.Serveur != m.Serveur || d.API != m.API || d.Serveur+d.API != "" {
		x := deploiementMission(d)
		if x == nil { // both unlinked: the project keeps its address and plan, without server nor key
			e := s.acces.Environnements(p.Nom)[s.deploiementProjet(p.Nom)]
			x = &DeploiementDemande{URL: e.URL, Plan: e.Plan, Moment: cmp.Or(e.Moment, "livraison"), Serveurs: []string{}, APIs: []string{}}
		}
		if _, err := s.accesPretErr(p.Nom); err == nil {
			err = s.definirDeploiementProjet(p.Nom, *x, qui)
		}
		if err != nil {
			s.erreurMissionHTTP(w, err)
			return
		}
		if d.Serveur+d.API != "" {
			if _, err := definirReglageProjet(p.Nom, "permission", modeMission(d.Autonome)); err != nil {
				s.erreurMissionHTTP(w, err)
				return
			}
		}
	}
	changes := []string{}
	if d.Serveur != m.Serveur || d.API != m.API || d.Autonome != m.Autonome {
		changes = append(changes, "server, key or permission")
	}
	if t := s.masquerSecrets(d.Texte); t != m.Phrase || strings.TrimSpace(d.Plan) != "" {
		titre, _, _ := strings.Cut(d.Texte, "\n")
		if _, _, err := s.ajouterDemande(p, couper(titre, 60), texteMission(d.Texte, d.Plan)); err != nil {
			s.erreurMissionHTTP(w, err)
			return
		}
		m.Phrase = t
		changes = append(changes, "request added")
	}
	m.Serveur, m.API, m.Autonome = d.Serveur, d.API, d.Autonome
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

// apiAgirMission: POST /api/missions/{id}/{action}: pause, arreter, reprendre (its project's pilot, in any
// state), serveur-arreter (its Servers tasks cancelled), serveur-relancer (relancerServeurMission).
func (s *Serveur) apiAgirMission(w http.ResponseWriter, r *http.Request) {
	m, p, err := s.missionSuivie(r.PathValue("id"))
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	action, cle := r.PathValue("action"), ""
	switch action {
	case "pause", "arreter":
		err = s.arreterProjet(p, action)
		cle = map[string]string{"pause": "The pilot is paused: Start picks it up where it stopped.", "arreter": "The pilot is stopped."}[action]
		action = map[string]string{"pause": "pause", "arreter": "arretee"}[action]
	case "reprendre":
		pauserProjet(p.Nom, false)
		noterArret(p.Nom, "")
		marqueAuto(p, true) // restarted with the application, as Start does (demarrerPiloteAuto)
		if cle, err = demarrerPiloteProjet(s, p); err != nil && err.Error() == texteLivreRienDeNeuf {
			marqueAuto(p, false)
		}
		action = "reprise"
	case "serveur-arreter":
		s.arreterTachesProjet(p.Nom)
		cle, action = "The server's work of this mission is stopped.", "serveur_arrete"
	case "serveur-relancer":
		cle, err = s.relancerServeurMission(p)
		action = "serveur_relance"
	default:
		s.erreur(w, http.StatusNotFound, "Unknown action")
		return
	}
	if err != nil {
		if errors.Is(err, errServeursNatif) {
			s.erreur(w, http.StatusConflict, err.Error())
			return
		}
		var em erreurEntree
		if errors.As(err, &em) || err.Error() == texteSansClaude || err.Error() == texteLivreRienDeNeuf {
			s.erreur(w, http.StatusConflict, err.Error())
			return
		}
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	missionMu.Lock() // the pilot's start or stop may take a while: the file is read again and written alone
	if lu, err := lireMission(m.ID); err == nil {
		m = lu
	}
	evenement(&m, action, "")
	_ = ecrireMission(m)
	missionMu.Unlock()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: m.Projet, Mission: m.ID, Action: "mission: " + action, Resultat: cle})
	s.ecrireJSON(w, map[string]any{"mission": s.vueMission(m), "message_key": cle, "args": []string{}})
}

// vueMission: m as the page shows it, its project and server read now.
func (s *Serveur) vueMission(m Mission) VueMission {
	v := VueMission{Mission: m, Suivie: m.Projet != ""}
	if !v.Suivie {
		v.Mission = s.voiesEnDirect(m)
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
	if s.coffre != nil && m.API != "" {
		for _, x := range s.coffre.Liste() {
			if x.ID == m.API {
				v.APINom = x.Nom
			}
		}
	}
	if s.acces == nil {
		return v
	}
	dep := s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)]
	v.URL = dep.URL
	if len(dep.Serveurs) == 0 {
		v.ServeurEtat = "aucun"
		return v
	}
	v.Serveur = dep.Serveurs[0] // the project's deployment is the truth: changed on its tab, it shows here too
	if l, err := lireServeurs(); err == nil {
		for _, x := range l {
			if x.ID == v.Serveur {
				v.ServeurNom = x.Nom
			}
		}
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
