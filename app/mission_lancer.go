package main

// Launching a mission (ARCHITECTURE §Missions et orchestrateur, Lancer): once, the state moving from proposee or
// acceptee to lancee under verrouMissions; then, in order, the Dev lane's project (a new one through the usual
// project creation, or a request added to an existing one), the ticked vault entries, the environments, the
// pilot, and one Servers task per environment to prepare. A failed step stops the next ones, goes to the history
// and to the bell; nothing already done is undone.

import (
	"cmp"
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

// fournisseurCoche: the provider whose zone holds host h and whose vault token is ticked among acces ("" without one).
func fournisseurCoche(h string, acces []AccesCarte) string {
	l, _ := lireFournisseurs()
	for _, f := range l {
		coche := slices.ContainsFunc(acces, func(a AccesCarte) bool { return a.ID == f.Cle && !a.Decoche && !a.ARemplir })
		if h != "" && coche && slices.ContainsFunc(f.Zones, func(z ZoneFournisseur) bool { return h == z.Nom || strings.HasSuffix(h, "."+z.Nom) }) {
			return f.ID
		}
	}
	return ""
}

// politiqueEnvMission: a Production environment always waits for the owner; the others deploy alone unless
// the mission's policy is tout.
func politiqueEnvMission(production bool, politique string) string {
	if production || politique == "tout" {
		return "accord"
	}
	return "auto"
}

// lancerMission: mission id launched by qui; dossier: the new project's folder chosen on the PC ("" = next to
// the other projects); empreinte: the card the accord was asked for ("" = the card as it is, the owner's Launch).
// Refused (nothing done) while the orchestrator works, without a card, when the card is not that of empreinte or
// changed while it was checked, when an infra entry is ticked and the infra keys are locked, or when the new
// project's folder cannot be created (B88). In native Windows mode a Servers lane or an infra entry forces the project (logged).
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
	var voies []VoieMission
	var faits []EvenementMission
	if c.Projet.Distant != "" {
		voies, faits, err = s.etapesDistantes(c, qui, id)
	} else {
		voies, faits, err = s.etapesMission(c, m.Politique, qui, dossier)
	}
	defer s.suivreMission(id) // stops by itself at once if the launch ended in echec
	return modifierMission(id, func(m *Mission) error {
		m.Voies = voies
		m.Historique = append(m.Historique, faits...) // C86: what was given and set, even when a later step failed
		if err != nil {
			m.Etat = "echec"
			e := evenementMission("echec", couper(s.masquerSecrets(err.Error()), 300))
			if s.masquerSecrets(err.Error()) == err.Error() { // B87: translated where shown; nothing masked to leak
				e.Message = messageDe(err)
			}
			m.Historique = append(m.Historique, e)
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

// etapesMission: the launch's steps in order; the lanes started so far, one history line per entry given and
// environment set (C86), and the first step's error.
func (s *Serveur) etapesMission(c CarteMission, politique, qui, dossier string) ([]VoieMission, []EvenementMission, error) {
	voies, faits := []VoieMission{}, []EvenementMission{}
	audit := func(v VoieMission) {
		voies = append(voies, v)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: v.Module, Projet: c.Projet.Nom, Action: "voie de mission lancée", Resultat: v.Ref + " → " + v.Etat})
	}
	// 1. the project: created (the Dev brief as its request) or given the Dev brief as a new request
	var p Projet
	if c.Projet.Nouveau {
		var err error
		if p, _, err = s.ajouterProjet(c.Projet.Nom, dossier, c.Dev.Brief, false); err != nil {
			return voies, faits, errMoteur("creating the project: {0}", err)
		}
		if md := c.Dev.Modes; md != nil { // before its pilot starts: its first cycle reads them
			if err := p.ecrireMode(ModeVue{Parallele: md.Parallele, Eco: md.Eco, Rapide: md.Rapide}); err != nil {
				return voies, faits, errMoteur("the working modes: {0}", err)
			}
			faits = append(faits, evenementMission("lancee", "working modes set: "+nomsModes(*md)))
		}
	} else {
		var ok bool
		if p, ok = s.projetNomme(c.Projet.Nom); !ok {
			return voies, faits, errMoteur("the project {0} no longer exists", c.Projet.Nom)
		}
		if c.Dev.Brief != "" {
			if _, _, err := s.ajouterDemande(p, c.Titre, c.Dev.Brief); err != nil {
				return voies, faits, errMoteur("adding the request: {0}", err)
			}
		}
	}
	if s.acces == nil {
		return voies, faits, errMoteur("the access file cannot be read")
	}
	// native Windows mode: launching a mission with a Servers lane or an infra entry is the owner's Force for its project (logged)
	if modeNatif.Load() && (c.AvertissementServeurs != "" || c.AvertissementInfra != "") && !s.acces.Projet(p.Nom).ForcerInfraNatif {
		if err := s.acces.ForcerInfra(p.Nom, true); err != nil {
			return voies, faits, errMoteur("forcing the project in native Windows mode: {0}", err)
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: p.Nom, Action: "forcer_infra_natif", Resultat: "true (mission lancée en mode Windows natif)"})
		faits = append(faits, evenementMission("lancee", "native Windows mode: the project is forced by this launch (logged)"))
	}
	// 2. the ticked vault entries (an entry already given stays as it is; one to fill is the owner's)
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
			return voies, faits, errMoteur("giving {0}: {1}", a.Entree, err)
		}
		action := "acces_accorde"
		if e.Portee == "infra" && modeNatif.Load() {
			action = "acces_accorde_force" // as the Access page: an infra entry given in native mode, the project forced
		}
		s.auditAcces(qui, p.Nom, action, e, g.Variable, g.Env)
		faits = append(faits, evenementMission("lancee", "access given: "+a.Entree+" → "+g.Variable+" ("+g.Env+")"))
	}
	// C86: in the project's secrets.env now, a pilot already running included (demarrerPilote stops before it)
	if len(faits) > 0 {
		if _, err := s.injecterAcces(p, nil); err != nil {
			return voies, faits, errMoteur("writing the access given: {0}", err)
		}
	}
	// 3. the environments: the card's URL, protection and servers, the mission's policy (prod: always accord)
	inventaire, _ := lireServeurs()
	actuels := s.acces.Environnements(p.Nom)
	for _, ec := range c.Envs {
		e := actuels[ec.Nom]
		e.URL, e.Protection, e.Serveurs = ec.URL, ec.Protection, ec.Serveurs // C81: never weakened (envSansAffaiblir)
		// M247: Production from the card (never lowered); the milestone relay on the first other environment only
		e.Production = e.Production || ec.Production
		e.Politique, e.DeployerAuJalon = ec.Politique, ec.Nom == envJalonCarte(c) && !e.Production && !serveurRoleProd(ec.Serveurs, inventaire)
		if e.Fournisseur == "" { // the Servers lane's DNS: the provider holding the host's zone, its token ticked on the card
			e.Fournisseur = fournisseurCoche(hoteURL(ec.URL), c.Acces)
		}
		v, err := validerEnvironnement(ec.Nom, e, inventaire)
		if err == nil {
			err = s.acces.DefinirEnvironnement(p.Nom, ec.Nom, v)
		}
		if err != nil {
			return voies, faits, errMoteur("the environment {0}: {1}", ec.Nom, err)
		}
		faits = append(faits, evenementMission("lancee", "environment set: "+ec.Nom+" "+ec.URL+", "+ec.Protection+", "+ec.Politique))
		// C82: a tout mission asks every deployment of its environments: the owner's "never ask again" ends (as B71)
		if politique == "tout" && s.approbations != nil {
			if _, err := s.approbations.RetirerRegles(func(r RegleAccord) bool {
				return r.Projet == p.Nom && r.Env == ec.Nom && r.Action == actionDeploiement
			}); err != nil {
				return voies, faits, errMoteur("the environment {0}: {1}", ec.Nom, err)
			}
		}
	}
	// 4. the pilot (it injects the grants given above)
	if c.Dev.Brief != "" {
		if _, err := demarrerPiloteMission(s, p); err != nil {
			return voies, faits, errMoteur("starting the pilot: {0}", err)
		}
		audit(VoieMission{Module: "dev", Ref: p.Nom, Etat: "lancee"})
	}
	// 5. one Servers task per environment with servers; native Windows mode: launched without a barrier, logged
	if c.Serveurs.Brief == "" {
		return voies, faits, nil
	}
	for _, ec := range c.Envs {
		if len(ec.Serveurs) == 0 {
			continue
		}
		env := p.Nom + "/" + ec.Nom
		natif := modeNatif.Load()
		texte := c.Serveurs.Brief + "\n\nMission \"" + c.Titre + "\", environment " + ec.Nom + ": " + ec.URL + ", protection " + ec.Protection + "."
		t, err := s.lancerTache(couper(texte, maxTexteTache-1), ec.Serveurs, natif, env)
		if err != nil {
			return voies, faits, errMoteur("the Servers task for {0}: {1}", env, err)
		}
		audit(VoieMission{Module: "servers", Ref: t.ID, Etat: t.Etat})
		if natif {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "servers", Projet: p.Nom, Action: "voie Serveurs sans barrière",
				Resultat: t.ID + " (" + env + ", mission lancée en mode Windows natif)"})
		}
	}
	return voies, faits, nil
}

// missionTout (C82): a launched mission of policy tout covers env ("project/env"): its Servers plans and changes
// always ask the owner, whatever rule ("do not ask again for this server") is in place.
func missionTout(env string) bool {
	projet, nom, ok := strings.Cut(env, "/")
	if !ok {
		return false
	}
	return slices.ContainsFunc(lireMissions(), func(m Mission) bool {
		var c CarteMission
		return m.Etat == "lancee" && m.Politique == "tout" && json.Unmarshal(m.Carte, &c) == nil && c.Projet.Nom == projet &&
			slices.ContainsFunc(c.Envs, func(e EnvCarte) bool { return e.Nom == nom })
	})
}

// apiLancerMission: POST /api/missions/{id}/lancer {dossier?, mot_de_passe, empreinte}: the mission launched (its state and lanes in the
// answer, echec with the reason if a step failed). The folder is chosen on the PC only; empreinte is the card the
// owner read (R3: absent or another one, nothing is launched).
func (s *Serveur) apiLancerMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Dossier    string `json:"dossier"`
		MotDePasse string `json:"mot_de_passe"`
		Empreinte  string `json:"empreinte"`
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
	m, err := s.lancerMission(r.PathValue("id"), deQui(r), strings.TrimSpace(c.Dossier), cmp.Or(c.Empreinte, "absente"))
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.fermerLancementMission(m.ID, deQui(r), "accepter", "mission lancée")
	s.ecrireJSON(w, map[string]any{"mission": m})
}
