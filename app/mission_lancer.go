package main

// Launching a mission (ARCHITECTURE §Missions et orchestrateur, Lancer): once, the state moving from proposee or
// acceptee to lancee under verrouMissions; then, in order, the Dev lane's project (a new one through the usual
// project creation, or a request added to an existing one), the ticked vault entries, the environments, the
// pilot, and one Servers task per environment to prepare. A failed step stops the next ones, goes to the history
// and to the bell; nothing already done is undone.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// demarrerPiloteMission: replaced by the tests (a fake pilot).
var demarrerPiloteMission = (*Serveur).demarrerPilote

// errCarteChangee (C77, C79): the card or the policy changed since the launch was asked or read.
var errCarteChangee = erreurMission{http.StatusConflict, "The card has changed: look at it again before launching it"}

// apresControleLancement: called between the checks and the launch (a test changes the card just then).
var apresControleLancement = func(string) {}

var errMissionInfraVerrouille = erreurMission{http.StatusConflict, "This mission uses an infra key: unlock the vault's infra keys first"}

// dossierProjetNeuf: where a mission's new project goes, next to the most recent project of this PC (in the
// home folder without one).
func dossierProjetNeuf(nom string, projets []Projet) string {
	parent, _ := os.UserHomeDir()
	for i := len(projets) - 1; i >= 0; i-- {
		if !projets[i].Archive && filepath.IsAbs(projets[i].Dossier) {
			parent = filepath.Dir(filepath.Clean(projets[i].Dossier))
			break
		}
	}
	return filepath.Join(parent, nom)
}

// dossierAvecFichiers: d holds something other than a workshop and the system's own files.
func dossierAvecFichiers(d string) bool {
	e, _ := os.ReadDir(d)
	return slices.ContainsFunc(e, func(x os.DirEntry) bool {
		n := x.Name()
		return n != ".athanor" && n != ".DS_Store" && !strings.EqualFold(n, "desktop.ini") && !strings.EqualFold(n, "Thumbs.db")
	})
}

// envAcces: the environment a ticked entry is given to: pour "project/env" names it, a server gives its card
// environment, the project itself is dev.
func envAcces(pour string, c CarteMission) string {
	if _, env, ok := strings.Cut(pour, "/"); ok {
		return env
	}
	for _, e := range c.Envs {
		if slices.Contains(e.Serveurs, pour) {
			return e.Nom
		}
	}
	return "dev"
}

// politiqueEnvMission: prod always waits for the owner; dev and staging deploy alone unless the mission's
// policy is tout.
func politiqueEnvMission(env, politique string) string {
	if env == "prod" || politique == "tout" {
		return "accord"
	}
	return "auto"
}

// lancerMission: mission id launched by qui; dossier: the new project's folder chosen on the PC ("" = next to
// the other projects); empreinte: the card the accord was asked for ("" = the card as it is, the owner's Launch).
// Refused (nothing done) while the orchestrator works, without a card, when the card is not that of empreinte or
// changed while it was checked, when an infra entry is ticked and the infra keys are locked or the PC is in native
// mode, or when the new project's folder cannot be created (B88).
func (s *Serveur) lancerMission(id, qui, dossier, empreinte string) (Mission, error) {
	m, err := lireMission(id)
	pasLancable := func(m *Mission) error {
		if m.Etat != "proposee" && m.Etat != "acceptee" {
			return erreurMission{http.StatusConflict, "This mission is " + m.Etat + ": it cannot be launched"}
		}
		return nil
	}
	switch {
	case err != nil:
		return m, err
	case pasLancable(&m) != nil:
		return m, pasLancable(&m)
	case orchestrateurTravaille(id):
		return m, erreurMission{http.StatusConflict, "The orchestrator is still working on this card: wait for it"}
	case len(m.Carte) == 0:
		return m, erreurMission{http.StatusConflict, "This mission has no card yet"}
	case empreinte != "" && empreinte != empreinteCarte(m):
		return m, errCarteChangee
	}
	lue := empreinteCarte(m)
	var c CarteMission
	if err := json.Unmarshal(m.Carte, &c); err != nil {
		return m, err
	}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil { // what changed since the card (a project removed, a host taken)
		return m, err
	}
	c = calculerCarte(c, m.Politique, x)
	if c.AvertissementInfra != "" { // C80: native mode, an infra entry ticked, the project not forced (a new one never is)
		return m, errAccesInfraNatif
	}
	if !x.infraOuvert && slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Portee == "infra" && !a.Decoche }) {
		return m, errMissionInfraVerrouille
	}
	if c.Projet.Nouveau {
		if dossier == "" {
			dossier = dossierProjetNeuf(c.Projet.Nom, x.projets)
		}
		if err := s.controlerDossierMission(c.Projet.Nom, dossier); err != nil { // B88: refused before lancee, the mission intact
			return m, err
		}
	}
	apresControleLancement(id)
	m, err = modifierMission(id, func(m *Mission) error {
		if err := pasLancable(m); err != nil { // checked again under the lock: two clicks, one launch
			return err
		}
		if empreinteCarte(*m) != lue { // C79: changed (Modify on another device) since it was checked above
			return errCarteChangee
		}
		m.Etat, m.Voies = "lancee", []VoieMission{}
		m.Historique = append(m.Historique, evenementMission("lancee", qui))
		return nil
	})
	if err != nil {
		return m, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission lancée", Resultat: c.Titre})
	voies, err := s.etapesMission(c, m.Politique, qui, dossier)
	return modifierMission(id, func(m *Mission) error {
		m.Voies = voies
		if err != nil {
			m.Etat = "echec"
			m.Historique = append(m.Historique, evenementMission("echec", couper(s.masquerSecrets(err.Error()), 300)))
		}
		for _, v := range voies {
			if v.Etat == "refusee" {
				m.Historique = append(m.Historique, evenementMission("lancee", "servers lane refused: "+errServeursNatif.Error()))
				break
			}
		}
		return nil
	})
}

// controlerDossierMission (B88): what would make the new project's creation fail, checked before the launch:
// the project's checks, a folder already holding files, a parent that does not exist or cannot be written.
func (s *Serveur) controlerDossierMission(nom, dossier string) error {
	d, err := s.controlerProjetNeuf(nom, dossier)
	if err != nil {
		return erreurEntree(err.Error())
	}
	if strings.HasPrefix(d, `\\`) || strings.HasPrefix(d, "//") { // a network share: unreachable for the pilot's sandbox
		return erreurEntreeArgs{"The folder {0} is on a network share: choose a folder of this PC for the new project", []string{d}}
	}
	if strings.HasPrefix(d, `\`) || strings.HasPrefix(d, "//") { // a network share: unreachable for the pilot's sandbox
		return erreurEntreeArgs{"The folder {0} is on a network share: choose a folder of this PC for the new project", []string{d}}
	}
	if dossierAvecFichiers(d) {
		return erreurEntreeArgs{"The folder {0} already holds files: choose another folder for the new project", []string{d}}
	}
	if fi, err := os.Stat(d); err == nil && fi.IsDir() {
		return nil // an empty folder already there
	}
	parent := filepath.Dir(d)
	f, err := os.CreateTemp(parent, ".athanor-essai-*")
	if err != nil {
		return erreurEntreeArgs{"The folder {0} cannot be created there: choose another folder for the new project", []string{d}}
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

// etapesMission: the launch's steps in order; the lanes started so far, and the first step's error.
func (s *Serveur) etapesMission(c CarteMission, politique, qui, dossier string) ([]VoieMission, error) {
	voies := []VoieMission{}
	audit := func(v VoieMission) {
		voies = append(voies, v)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: v.Module, Projet: c.Projet.Nom, Action: "voie de mission lancée", Resultat: v.Ref + " → " + v.Etat})
	}
	// 1. the project: created (the Dev brief as its request) or given the Dev brief as a new request
	var p Projet
	if c.Projet.Nouveau {
		var err error
		if p, _, err = s.ajouterProjet(c.Projet.Nom, dossier, c.Dev.Brief, false); err != nil {
			return voies, errors.New("creating the project: " + err.Error())
		}
	} else {
		var ok bool
		if p, ok = s.projetNomme(c.Projet.Nom); !ok {
			return voies, errors.New("the project " + c.Projet.Nom + " no longer exists")
		}
		if c.Dev.Brief != "" {
			if _, _, err := s.ajouterDemande(p, c.Titre, c.Dev.Brief); err != nil {
				return voies, errors.New("adding the request: " + err.Error())
			}
		}
	}
	// 2. the ticked vault entries (an entry already given stays as it is; one to fill is the owner's)
	if s.acces == nil {
		return voies, errors.New("the access file cannot be read")
	}
	for _, a := range c.Acces {
		if a.Decoche || a.ARemplir {
			continue
		}
		e := s.entreeCoffre(a.ID)
		g, err := s.acces.Ajouter(p.Nom, e, envAcces(a.Pour, c), "", "mission")
		if errors.Is(err, errAccesDeja) {
			continue
		}
		if err != nil {
			return voies, errors.New("giving " + a.Entree + ": " + err.Error())
		}
		s.auditAcces(qui, p.Nom, "acces_accorde", e, g.Variable, g.Env)
	}
	// 3. the environments: the card's URL, protection and servers, the mission's policy (prod: always accord)
	inventaire, _ := lireServeurs()
	actuels := s.acces.Environnements(p.Nom)
	for _, ec := range c.Envs {
		e := actuels[ec.Nom]
		e.URL, e.Protection, e.Serveurs = ec.URL, ec.Protection, ec.Serveurs // C81: never weakened (envSansAffaiblir)
		e.Politique, e.DeployerAuJalon = ec.Politique, ec.Nom == "dev"
		v, err := validerEnvironnement(ec.Nom, e, inventaire)
		if err == nil {
			err = s.acces.DefinirEnvironnement(p.Nom, ec.Nom, v)
		}
		if err != nil {
			return voies, errors.New("the environment " + ec.Nom + ": " + err.Error())
		}
	}
	// 4. the pilot (it injects the grants given above)
	if c.Dev.Brief != "" {
		if _, err := demarrerPiloteMission(s, p); err != nil {
			return voies, errors.New("starting the pilot: " + err.Error())
		}
		audit(VoieMission{Module: "dev", Ref: p.Nom, Etat: "lancee"})
	}
	// 5. one Servers task per environment with servers; native Windows mode: the lane is refused, Dev goes on
	if c.Serveurs.Brief == "" {
		return voies, nil
	}
	for _, ec := range c.Envs {
		if len(ec.Serveurs) == 0 {
			continue
		}
		env := p.Nom + "/" + ec.Nom
		if modeNatif.Load() {
			audit(VoieMission{Module: "servers", Ref: env, Etat: "refusee"})
			continue
		}
		texte := c.Serveurs.Brief + "\n\nMission \"" + c.Titre + "\", environment " + ec.Nom + ": " + ec.URL + ", protection " + ec.Protection + "."
		t, err := s.lancerTache(couper(texte, maxTexteTache-1), ec.Serveurs, false, env)
		if err != nil {
			return voies, errors.New("the Servers task for " + env + ": " + err.Error())
		}
		audit(VoieMission{Module: "servers", Ref: t.ID, Etat: t.Etat})
	}
	return voies, nil
}

// apiLancerMission: POST /api/missions/{id}/lancer {dossier?, mot_de_passe}: the mission launched (its state and lanes in the
// answer, echec with the reason if a step failed). The folder is chosen on the PC only.
func (s *Serveur) apiLancerMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Dossier    string `json:"dossier"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if r.ContentLength != 0 && lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if code, err := s.preuvePresence(r, c.MotDePasse); err != nil { // C78: launching gives vault entries, as /api/acces (C25)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Mission: r.PathValue("id"), Action: "lancement refusé", Resultat: err.Error()})
		s.erreur(w, code, err.Error())
		return
	}
	if appareilDe(r) != "" {
		c.Dossier = ""
	}
	m, err := s.lancerMission(r.PathValue("id"), deQui(r), strings.TrimSpace(c.Dossier), "")
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.fermerLancementMission(m.ID, deQui(r), "accepter", "mission lancée")
	s.ecrireJSON(w, map[string]any{"mission": m})
}
