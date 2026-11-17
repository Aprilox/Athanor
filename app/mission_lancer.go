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
		if m.Etat != "proposee" && m.Etat != "acceptee" && !echecLancement(*m) {
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
	// M254: a launch resumed after a failed step: the project it created and the request it added stay as they are
	repris := m.Etat == "echec"
	projetCree := repris && faitMission(m, "project created: ") // M268: its request is already the card's Dev brief
	if projetCree {
		c.Projet.Nouveau, c.Dev.Modes = false, nil
	}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil { // what changed since the card (a project removed, a host taken)
		return m, err
	}
	c = calculerCarte(c, m.Politique, x)
	if !x.infraOuvert && slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Portee == "infra" && !a.Decoche }) {
		return m, errMissionInfraVerrouille
	}
	if c.Projet.Nouveau && c.Projet.GitHub == "lier" && c.Projet.Depot == "" { // M264: chosen, never typed
		return m, erreurEntree("Write the repository to link as OWNER/NAME, then launch.")
	}
	if c.Projet.Nouveau {
		dossier = cmp.Or(dossier, c.Projet.Dossier, dossierProjetNeuf(c.Projet.Nom, x.projets))
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
		voies, faits, err = s.etapesMission(c, m.Politique, qui, dossier, projetCree || repris && faitMission(m, "request added: "))
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

// echecLancement: m's launch stopped at a failed step (in echec after a launch, changed since or not); it can be resumed.
func echecLancement(m Mission) bool { return m.Etat == "echec" && lanceeUneFois(m) }

// faitMission: a launch event of m starts with debut (what a failed launch already did).
func faitMission(m Mission, debut string) bool {
	return slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == "lancee" && strings.HasPrefix(e.Detail, debut) })
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
func (s *Serveur) etapesMission(c CarteMission, politique, qui, dossier string, demandeFaite bool) ([]VoieMission, []EvenementMission, error) {
	voies, faits := []VoieMission{}, []EvenementMission{}
	audit := func(v VoieMission) {
		voies = append(voies, v)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: v.Module, Projet: c.Projet.Nom, Action: "voie de mission lancée", Resultat: v.Ref + " → " + v.Etat})
	}
	// 1. the project: created (the Dev brief as its request) or given the Dev brief as a new request
	var p Projet
	if c.Projet.Nouveau {
		var err error
		if p, _, err = s.ajouterProjet(c.Projet.Nom, dossier, demandeStudio(c), false); err != nil {
			return voies, faits, errMoteur("creating the project: {0}", err)
		}
		faits = append(faits, evenementMission("lancee", "project created: "+p.Dossier))
		if md := c.Dev.Modes; md != nil { // before its pilot starts: its first cycle reads them
			if err := p.ecrireMode(ModeVue{Parallele: md.Parallele, ParalleleMax: md.ParalleleMax, Eco: md.Eco, Rapide: md.Rapide, CadrageMax: md.CadrageMax, DureeMax: md.DureeMax}); err != nil {
				return voies, faits, errMoteur("the working modes: {0}", err)
			}
			faits = append(faits, evenementMission("lancee", "working modes set: "+nomsModes(*md)))
		}
	} else {
		var ok bool
		if p, ok = s.projetNomme(c.Projet.Nom); !ok {
			return voies, faits, errMoteur("the project {0} no longer exists", c.Projet.Nom)
		}
		if c.Dev.Brief != "" && !demandeFaite {
			if _, _, err := s.ajouterDemande(p, c.Titre, demandeStudio(c)); err != nil {
				return voies, faits, errMoteur("adding the request: {0}", err)
			}
			faits = append(faits, evenementMission("lancee", "request added: "+c.Titre))
		}
	}
	// M254: the GitHub repository the owner chose, linked before the pilot pushes anything (again on a resumed launch)
	if c.Projet.GitHub != "" {
		if err := githubMission(s, p, c.Projet); err != nil {
			return voies, faits, errMoteur("the project {0} is created in {1}, but its GitHub repository is not linked: {2}", p.Nom, p.Dossier, err)
		}
		faits = append(faits, evenementMission("lancee", "GitHub repository linked: "+c.Projet.Depot))
	}
	if s.acces == nil {
		return voies, faits, errMoteur("the access file cannot be read")
	}
	// native Windows mode: launching a mission with a Servers lane or an infra entry is the owner's Force for its project (logged)
	if force, err := s.forcerNatifMission(p, c, qui); err != nil {
		return voies, faits, err
	} else if force {
		faits = append(faits, evenementMission("lancee", "native Windows mode: the project is forced by this launch (logged)"))
	}
	// 2. the environments (M265: before the grants, which name them): the card's URL, protection and servers, the mission's policy (prod: always accord)
	inventaire, _ := lireServeurs()
	actuels := s.acces.Environnements(p.Nom)
	for _, ec := range c.Envs {
		if err := s.definirEnvCarte(p, c, ec, politique, actuels, inventaire); err != nil {
			return voies, faits, err
		}
		faits = append(faits, evenementMission("lancee", "environment set: "+ec.Nom+" "+ec.URL+", "+ec.Protection+", "+ec.Politique))
	}
	// 3. the ticked vault entries (an entry already given stays as it is; one to fill is the owner's)
	for _, a := range c.Acces {
		if a.Decoche || a.ARemplir {
			continue
		}
		f, err := s.donnerAccesCarte(p, c, a, qui)
		if err != nil {
			return voies, faits, err
		}
		if f.Detail != "" {
			faits = append(faits, f)
		}
	}
	// C86: in the project's secrets.env now, a pilot already running included (demarrerPilote stops before it)
	if len(faits) > 0 {
		if _, err := s.injecterAcces(p, nil); err != nil {
			return voies, faits, errMoteur("writing the access given: {0}", err)
		}
	}
	// 4. the pilot (it injects the grants given above)
	if c.Dev.Brief != "" {
		if _, err := demarrerPiloteMission(s, p); err != nil {
			return voies, faits, errMoteur("starting the pilot: {0}", err)
		}
		audit(VoieMission{Module: "dev", Ref: p.Nom, Etat: "lancee"})
	}
	// 5. one Servers preparation per environment with servers (M249): now if the project's plan exists, otherwise
	// the follower launches it as soon as the plan appears (preparerServeursMission)
	if c.Serveurs.Brief == "" {
		return voies, faits, nil
	}
	for _, ec := range c.Envs {
		if len(ec.Serveurs) == 0 {
			continue
		}
		env := p.Nom + "/" + ec.Nom
		if !planPret(p) {
			audit(VoieMission{Module: "servers", Etat: "attente_plan", Env: env})
			continue
		}
		v, err := s.lancerPreparation(c, ec, p, qui)
		if err != nil {
			return voies, faits, errMoteur("the Servers task for {0}: {1}", env, err)
		}
		audit(v)
	}
	return voies, faits, nil
}

// forcerNatifMission: in native Windows mode, a card with a Servers lane or an infra entry forces p (the owner's
// Force, logged), once; whether it did.
func (s *Serveur) forcerNatifMission(p Projet, c CarteMission, qui string) (bool, error) {
	if !modeNatif.Load() || c.AvertissementServeurs == "" && c.AvertissementInfra == "" || s.acces.Projet(p.Nom).ForcerInfraNatif {
		return false, nil
	}
	if err := s.acces.ForcerInfra(p.Nom, true); err != nil {
		return false, errMoteur("forcing the project in native Windows mode: {0}", err)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: p.Nom, Action: "forcer_infra_natif", Resultat: "true (mission lancée en mode Windows natif)"})
	return true, nil
}

// definirEnvCarte: card environment ec written to p (actuels: p's environments as they are): the card's URL,
// protection and servers, the mission's policy (prod: always accord).
func (s *Serveur) definirEnvCarte(p Projet, c CarteMission, ec EnvCarte, politique string, actuels map[string]Environnement, inventaire []ServeurInventaire) error {
	e := actuels[ec.Nom]
	e.URL, e.Protection, e.Serveurs = ec.URL, ec.Protection, ec.Serveurs // C81: never weakened (envSansAffaiblir)
	// M247: Production from the card (never lowered); the milestone relay on the first other environment only
	e.Production = e.Production || ec.Production
	e.Politique, e.DeployerAuJalon = ec.Politique, relaisAuJalon(c, ec, e, inventaire)
	if e.Fournisseur == "" { // the Servers lane's DNS: the provider holding the host's zone, its token ticked on the card
		e.Fournisseur = fournisseurCoche(hoteURL(ec.URL), c.Acces)
	}
	v, err := validerEnvironnement(ec.Nom, e, inventaire)
	if err == nil {
		err = s.acces.definirAncien(p.Nom, ec.Nom, v)
	}
	if err != nil {
		return errMoteur("the environment {0}: {1}", ec.Nom, err)
	}
	// C82: a tout mission asks every deployment of its environments: the owner's "never ask again" ends (as B71)
	if politique == "tout" && s.approbations != nil {
		if _, err := s.approbations.RetirerRegles(func(r RegleAccord) bool {
			return r.Projet == p.Nom && r.Env == ec.Nom && r.Action == actionDeploiement
		}); err != nil {
			return errMoteur("the environment {0}: {1}", ec.Nom, err)
		}
	}
	return nil
}

// donnerAccesCarte: ticked line a of card c given to p for its environment ("mission" grant), audited; its history
// line (empty when p already had it).
func (s *Serveur) donnerAccesCarte(p Projet, c CarteMission, a AccesCarte, qui string) (EvenementMission, error) {
	e := s.entreeCoffre(a.ID)
	g, err := s.acces.Ajouter(p.Nom, e, envAcces(a.Pour, c), "", "mission")
	if errors.Is(err, errAccesDeja) {
		return EvenementMission{}, nil
	}
	if err != nil {
		return EvenementMission{}, errMoteur("giving {0}: {1}", a.Entree, err)
	}
	action := "acces_accorde"
	if e.Portee == "infra" && modeNatif.Load() {
		action = "acces_accorde_force" // as the Access page: an infra entry given in native mode, the project forced
	}
	s.auditAcces(qui, p.Nom, action, e, g.Variable, g.Env)
	return evenementMission("lancee", "access given: "+a.Entree+" → "+g.Variable+" ("+g.Env+")"), nil
}

// demandeStudio (M273): the Dev brief as the project's request; with an environment on the card, one line says the
// mission deploys it, so that the Studio plans no deployment and asks for no server's access.
func demandeStudio(c CarteMission) string {
	if len(c.Envs) == 0 {
		return c.Dev.Brief
	}
	return c.Dev.Brief + "\n\nDeployment:this Athanor mission's Servers lane puts the project on its servers, with the vault's keys. " +
		"Build it ready to deploy (build, tests, a deployment file when useful): no deployment milestone, no request for a server's access."
}

// planPret (M249): p's framing is done, its plan holds at least one milestone.
func planPret(p Projet) bool { return len(p.LirePlan()) > 0 }

// relaisAuJalon: ec (e as it will be written) is deployed after each milestone: the card's linked environment, never
// a Production one nor one on a server of the old prod role. Read at the launch and at the resume of a stopped mission.
func relaisAuJalon(c CarteMission, ec EnvCarte, e Environnement, inventaire []ServeurInventaire) bool {
	return ec.Nom == envJalonCarte(c) && !e.Production && !serveurRoleProd(ec.Serveurs, inventaire)
}

// lancerPreparation (M249): ec's Servers task, its brief the standard preparation adapted to p's plan; native
// Windows mode: launched without a barrier, logged.
func (s *Serveur) lancerPreparation(c CarteMission, ec EnvCarte, p Projet, qui string) (VoieMission, error) {
	env := p.Nom + "/" + ec.Nom
	natif := modeNatif.Load()
	t, err := s.lancerTache(couper(briefPreparation(c, ec, p), maxTexteTache-1), ec.Serveurs, natif, env)
	if err != nil {
		return VoieMission{}, err
	}
	if natif {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "servers", Projet: p.Nom, Action: "voie Serveurs sans barrière",
			Resultat: t.ID + " (" + env + ", mission lancée en mode Windows natif)"})
	}
	return VoieMission{Module: "servers", Ref: t.ID, Etat: t.Etat, Env: env}, nil
}

// briefPreparation (M249): what the Servers task prepares before the first delivery: the card's brief, the standard
// list adapted to the plan, the DNS provider, the vault entries given or still to fill (names only), the plan.
func briefPreparation(c CarteMission, ec EnvCarte, p Projet) string {
	var b strings.Builder
	b.WriteString(c.Serveurs.Brief + "\n\nMission \"" + c.Titre + "\", environment " + ec.Nom + ": " + ec.URL + ", protection " + ec.Protection + ".\n" +
		"Prepare these servers now, while the Studio builds the project, so that each delivery then only deploys it. " +
		"Standard list, adapted to the project's plan below (skip what it does not need): the system packages and runtime it needs; " +
		"nginx or an equivalent web server as reverse proxy; a dedicated system user; the site's folder; the TLS certificate of " +
		"the host; the firewall (SSH, HTTP and HTTPS only). Do not deploy the application itself.\n")
	if f := fournisseurCoche(hoteURL(ec.URL), c.Acces); f != "" {
		b.WriteString("DNS: the zone of " + hoteURL(ec.URL) + " is at the provider " + f + ", its token is given.\n")
	}
	var donnees, aRemplir []string
	for _, a := range c.Acces {
		switch {
		case a.Decoche:
		case a.ARemplir:
			aRemplir = append(aRemplir, a.Entree)
		default:
			donnees = append(donnees, a.Entree)
		}
	}
	if len(donnees) > 0 {
		b.WriteString("Vault entries given (names only, never ask for a value): " + strings.Join(donnees, ", ") + ".\n")
	}
	if len(aRemplir) > 0 {
		b.WriteString("Vault entries the owner has still to fill (leave what needs them for later): " + strings.Join(aRemplir, ", ") + ".\n")
	}
	b.WriteString("Project plan:\n")
	for _, j := range p.LirePlan() {
		b.WriteString("- " + couper(j.Titre, 160) + "\n")
	}
	return b.String()
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
		Dossier    string            `json:"dossier"`
		MotDePasse string            `json:"mot_de_passe"`
		Empreinte  string            `json:"empreinte"`
		Secrets    []secretLancement `json:"secrets"` // M271: the values of the card's entries still to fill (SSH password, key)
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
	if err := s.rangerSecretsLancement(r, r.PathValue("id"), c.Secrets); err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	m, err := s.lancerMission(r.PathValue("id"), deQui(r), strings.TrimSpace(c.Dossier), cmp.Or(c.Empreinte, "absente"))
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.fermerLancementMission(m.ID, deQui(r), "accepter", "mission lancée")
	s.ecrireJSON(w, map[string]any{"mission": m})
}

// secretLancement (M271): a value typed at the launch for an entry of the card still to fill.
type secretLancement struct {
	Entree string `json:"entree"`
	Type   string `json:"type"` // ssh (a private key) or mot_de_passe
	Valeur string `json:"valeur"`
}

// rangerSecretsLancement (M271): each value goes into the vault under its entry's name, only for an entry the card
// waits for (ARemplir), so the launch then gives it; the audit names the entry, never the value.
func (s *Serveur) rangerSecretsLancement(r *http.Request, id string, secrets []secretLancement) error {
	if len(secrets) == 0 {
		return nil
	}
	m, err := lireMission(id)
	var c CarteMission
	if err != nil || json.Unmarshal(m.Carte, &c) != nil {
		return cmp.Or(err, errors.New("This mission has no card yet"))
	}
	if s.coffreErr != nil || s.coffre == nil {
		return erreurMission{http.StatusInternalServerError, "The vault file cannot be read"}
	}
	for _, x := range secrets {
		if !slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.ARemplir && !a.Decoche && strings.EqualFold(a.Entree, x.Entree) }) {
			return erreurEntreeArgs{"{0} is not an entry this card waits for", []string{x.Entree}}
		}
		if x.Type != "ssh" && x.Type != "mot_de_passe" || x.Valeur == "" {
			return erreurEntree("A value typed at the launch is an SSH key (ssh) or a password (mot_de_passe), not empty")
		}
		e := EntreeCoffre{Nom: strings.TrimSpace(x.Entree), Famille: "secret", Type: x.Type}
		if s.nomPris(e.Nom, porteeEntree(e), "") {
			return erreurMission{http.StatusConflict, "An entry of the vault is already named " + e.Nom}
		}
		if _, err := s.coffre.Ajouter(e, x.Valeur); err != nil {
			return erreurMission{http.StatusConflict, err.Error()}
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "coffre", Mission: id, Action: "secret rangé au lancement", Resultat: e.Nom})
	}
	return nil
}
