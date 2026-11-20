package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// PreparationRelais (M283): the server preparation of a project's deployment, kept in its relay record.
type PreparationRelais struct {
	Tache     string `json:"tache,omitempty"`     // its Servers task ("": the servers already served the project)
	Empreinte string `json:"empreinte,omitempty"` // the servers, the address and the deployment plan it was made for
	Erreur    string `json:"erreur,omitempty"`    // why it did not start (tried again at the next look)
}

// planPret (M249): p's framing is done, its plan holds at least one milestone.
func planPret(p Projet) bool { return len(p.LirePlan()) > 0 }

// donneesPreparation: what the preparation brief is made of: the project's deployment, or a mission's card
// (briefPreparationMission, until M302).
type donneesPreparation struct {
	Entete          string   // who asks, before the address
	URL, Protection string   // the site's address and its protection
	DNS             string   // the provider holding the host's zone, its token given ("" none)
	APIs, ARemplir  []string // names of the vault entries given, and of those the owner has still to fill
	Site, Autres    []string // names of the servers the project is deployed on, and of the project's other servers
	Plan            string   // the deployment's plan, in the owner's words
	NotesChef       string   // what the project's lead told the server manager (informer_serveurs)
	Demande         string   // the project's request
	Accord          bool     // a production is submitted to the owner before it first goes online
}

// briefPreparation (M249): what the Servers task prepares before the first deployment: the standard list adapted to
// the plan, the DNS provider, the vault entries given or still to fill (names only), the plans.
func briefPreparation(d donneesPreparation, p Projet) string {
	var b strings.Builder
	b.WriteString(d.Entete + ": " + d.URL + ", protection " + d.Protection + ".\n" +
		"Prepare these servers now, while the Studio builds the project, so that each delivery then only deploys it. " +
		"Standard list, adapted to the project's plan below (skip what it does not need): the system packages and runtime it needs; " +
		"the web server the deployment plan names (nginx, Apache…), else Caddy, which Athanor's engine writes the sites of (never both on the same ports); a dedicated system user; the site's folder; the TLS certificate of " +
		"the host; the firewall (SSH, HTTP and HTTPS only). Do not deploy the application itself.\n")
	if d.Accord {
		b.WriteString(phraseAccordProduction)
	}
	if len(d.Autres) > 0 {
		b.WriteString("Servers: the project is deployed on " + strings.Join(d.Site, ", ") + "; the project's other servers (" + strings.Join(d.Autres, ", ") +
			") are never deployed to: give each the role written next to it and the plan below says (a database, a worker…), and link them to the servers the project is deployed on.\n")
	}
	if d.DNS != "" {
		b.WriteString("DNS: the zone of " + hoteURL(d.URL) + " is at " + d.DNS + ", its token is given. " + phraseDNS(hoteURL(d.URL)))
	}
	if len(d.APIs) > 0 {
		b.WriteString("Vault entries given (names only, never ask for a value): " + strings.Join(d.APIs, ", ") + ".\n")
	}
	if len(d.ARemplir) > 0 {
		b.WriteString("Vault entries the owner has still to fill (leave what needs them for later): " + strings.Join(d.ARemplir, ", ") + ".\n")
	}
	// both cut: a task's text holds maxTexteTache characters, the project's plan comes last
	if t := strings.TrimSpace(d.Plan); t != "" {
		b.WriteString("Deployment plan (the owner's words):\n" + couper(t, 1500) + "\n")
	}
	if t := strings.TrimSpace(d.NotesChef); t != "" {
		b.WriteString("The project lead's notes (how it is built and run):\n" + couper(t, 1200) + "\n")
	}
	if t := strings.TrimSpace(d.Demande); t != "" {
		b.WriteString("Project's request (its start):\n" + couper(t, 600) + "\n")
	}
	b.WriteString("Project plan:\n")
	for _, j := range p.LirePlan() {
		b.WriteString("- " + couper(j.Titre, 160) + "\n")
	}
	return b.String()
}

// phraseAccordProduction (M283, M310d): how a project's task asks the owner before a production first goes online:
// the plan's own accord, which no mode but ignorer accepts alone (outilProposerPlan).
const phraseAccordProduction = "If the deployment plan or the request below describes a production (real users or data, a public launch), " +
	"propose your plan with accord set to true and wait for the owner's accord before anything goes online for the first time.\n"

// tientHote: h is in one of f's zones.
func tientHote(f Fournisseur, h string) bool {
	return h != "" && slices.ContainsFunc(f.Zones, func(z ZoneFournisseur) bool { return h == z.Nom || strings.HasSuffix(h, "."+z.Nom) })
}

// fournisseurDeploiement (M283): among l, the provider of e: the one whose token is among e's API (the one holding
// its host's zone first), else the older e.Fournisseur.
func fournisseurDeploiement(e Environnement, l []Fournisseur) (Fournisseur, bool) {
	h := hoteURL(e.URL)
	for _, choix := range []func(Fournisseur) bool{
		func(f Fournisseur) bool { return slices.Contains(e.APIs, f.Cle) && tientHote(f, h) },
		func(f Fournisseur) bool { return slices.Contains(e.APIs, f.Cle) },
		func(f Fournisseur) bool { return e.Fournisseur != "" && f.ID == e.Fournisseur },
	} {
		if i := slices.IndexFunc(l, choix); i >= 0 {
			return l[i], true
		}
	}
	return Fournisseur{}, false
}

// briefPreparationProjet: the preparation brief of p's deployment e.
func (s *Serveur) briefPreparationProjet(p Projet, e Environnement) string {
	d := donneesPreparation{Entete: "Project \"" + p.Nom + "\"", URL: e.URL, Protection: e.Protection, Plan: e.Plan,
		Accord: modeProjet(p.Nom) != "ignorer", Site: nomsRoles(e.Serveurs, e.Roles), Autres: nomsRoles(e.Autres, e.Roles), NotesChef: e.NotesChef}
	if s.coffre != nil {
		for _, x := range s.coffre.Liste() {
			if slices.Contains(e.APIs, x.ID) {
				d.APIs = append(d.APIs, x.Nom)
			}
		}
	}
	l, _ := lireFournisseurs()
	if f, ok := fournisseurDeploiement(e, l); ok && tientHote(f, hoteURL(e.URL)) {
		d.DNS = f.ID
	}
	b, _ := os.ReadFile(p.Demande())
	d.Demande = string(b)
	return briefPreparation(d, p)
}

// preparerProjet (M283): the servers of p's deployment are prepared by a Servers task as soon as its plan holds a
// milestone, once per fingerprint (servers, address, deployment plan) noted in the relay's record: a restart
// launches nothing again, a change of one of them prepares again. Nothing without a server or a moment. The first look at a project
// already deployed only learns: its servers serve it.
func (s *Serveur) preparerProjet(p Projet) {
	if s.acces == nil {
		return
	}
	e := s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)]
	if len(e.Serveurs) == 0 || e.Moment == "" || !planPret(p) || projetBloque("dev", p.Nom) != "" {
		return
	}
	serveurs := slices.Sorted(slices.Values(e.tousServeurs())) // a server added to the others is prepared too
	h := sha256.Sum256([]byte(strings.Join(serveurs, ",") + "\x00" + e.URL + "\x00" + e.Plan))
	r, prep := lireRelais()[p.Nom], PreparationRelais{Empreinte: hex.EncodeToString(h[:12])}
	if r.Preparation != nil && r.Preparation.Empreinte == prep.Empreinte {
		return
	}
	if r.Preparation != nil || e.Deploiement == nil {
		t, err := s.lancerTache(couper(s.briefPreparationProjet(p, e), maxTexteTache-1), e.Serveurs, s.sansRappelsNatif(), p.Nom) // native Windows mode: only once the owner turned its reminders off, as for the first deployment
		if err != nil {
			prep = PreparationRelais{Erreur: s.masquerSecrets(err.Error())}
			if r.Preparation != nil && *r.Preparation == prep {
				return // said once, tried again at each look (the vault closed, native Windows mode)
			}
		} else {
			prep.Tache = t.ID
			s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: p.Nom, Env: s.envJalon(p.Nom),
				Action: "préparation du serveur", Resultat: t.ID + " (" + strings.Join(serveurs, ", ") + ")"})
		}
	}
	r.Preparation = &prep
	ecrireRelais(p.Nom, r)
}

// prefixePremierDeploiement: how the brief of a first deployment task starts.
const prefixePremierDeploiement = "First deployment"

// briefPremierDeploiement (M275b, M284): what the first deployment task does: commit head of p to its site
// with one deployer step; pour names who asks after the address (a mission's title, until M302).
func briefPremierDeploiement(url, pour string, p Projet, head string) string {
	return prefixePremierDeploiement + " of project " + p.Nom + " (" + url + ")" + pour + ": " +
		"its work is ready to go online and its site has no deployment method yet. Propose one plan whose step is deployer " +
		"(commit " + head + ", source artefact; the method and the published folder from the project's files: " +
		"statique for a static site, compose when it has a compose file), then apply it once accepted. Athanor then deploys " +
		"each later milestone or delivery itself with that method. Web server: Athanor's engine writes a Caddy site by default; when " +
		"the deployment plan names another web server (nginx, Apache…), set it up on the servers yourself to serve the site's folder " +
		"current release (statique, its published folder inside) or to proxy to the published port (compose), and give deployer proxy aucun: never install Caddy " +
		"against the owner's plan.\n"
}

// phraseDNS: what the agent does about host's DNS record (the external check needs it to resolve).
func phraseDNS(hote string) string {
	return "If " + hote + " has no DNS record yet (dns_lister), put its dns_ecrire step in your plan, before deployer: an A record to the public " +
		"address the deployment plan names, else to the server's. Without the Cloudflare tools of its zone, say in your answer that the owner must create that record.\n"
}

// briefPremierProjet: the first deployment's brief of p's deployment e under env: the method, the site's folder, the
// address's DNS record, the owner's plan always (the web server it names is the one used, full autonomy included:
// M310d's production accord only without it), the request's start.
func briefPremierProjet(p Projet, e Environnement, env, head string, ignorer bool) string {
	brief := briefPremierDeploiement(e.URL, "", p, head) + "The site's folder on the servers: " + baseDeploiement(p.Nom, env) + " (its current release: " + baseDeploiement(p.Nom, env) + "/current).\n" +
		"The servers are yours to set up: install whatever the deployment plan needs that is missing (system packages, the web server it names, tools), then deploy; never stop at a missing package.\n"
	if !ignorer {
		brief += phraseAccordProduction
	}
	if h := hoteURL(e.URL); h != "" {
		brief += phraseDNS(h)
	}
	if t := strings.TrimSpace(e.Plan); t != "" {
		brief += "Deployment plan (the owner's words):\n" + couper(t, 1500) + "\n"
	}
	if t := strings.TrimSpace(e.NotesChef); t != "" {
		brief += "The project lead's notes (how it is built and run):\n" + couper(t, 1200) + "\n"
	}
	if b, _ := os.ReadFile(p.Demande()); strings.TrimSpace(string(b)) != "" {
		brief += "Project's request (its start):\n" + couper(strings.TrimSpace(string(b)), 600) + "\n"
	}
	return brief
}

// tacheEnCoursID: the Servers task id exists and has not ended.
func tacheEnCoursID(id string) bool {
	t, ok := tacheMission(id)
	return ok && !slices.Contains([]string{"finie", "echec", "arretee"}, t.Etat)
}

var (
	premierMu sync.Mutex // the relay and the owner's "Prepare now" at once: one task
	// errRienAPreparer, errPreparationEnCours: why "Prepare now" launches nothing (translated by the interface)
	errRienAPreparer      = erreurEntree("Nothing to prepare now: this project has no server or is already deployed")
	errPreparationEnCours = erreurEntree("The servers of this project are still being prepared: try again once that task has ended")
)

// premierDeploiementProjet (M284): the first deployment of p, which has servers and no deployment method yet: one
// Servers task in the project's mode (lancerTache with the project as environment), once its servers' preparation
// has ended; the relay's record says "preparation" while it works (avancerRelais follows it). A task that cannot
// start (native Windows mode, the vault closed) leaves "a_preparer": an information, never a failure, tried again
// at the next milestone or delivery, or by the owner's "Prepare now" (forcee: forced by the owner in native mode).
func (s *Serveur) premierDeploiementProjet(p Projet, forcee bool) (TacheServeurs, error) {
	premierMu.Lock()
	defer premierMu.Unlock()
	env := s.envJalon(p.Nom)
	e, r := s.acces.Environnements(p.Nom)[env], lireRelais()[p.Nom]
	switch {
	case len(e.Serveurs) == 0 || e.Deploiement != nil:
		return TacheServeurs{}, errRienAPreparer
	case tacheEnCoursID(r.Premier):
		t, _ := tacheMission(r.Premier)
		return t, nil // asked twice: the task at work
	case r.Preparation != nil && tacheEnCoursID(r.Preparation.Tache):
		return TacheServeurs{}, errPreparationEnCours
	}
	head, err := gitSortie(p.Dossier, "rev-parse", "HEAD")
	if err != nil || !reCommitComplet.MatchString(head) {
		return TacheServeurs{}, erreurEntree("the project has no commit to deploy")
	}
	t, err := s.lancerTache(couper(briefPremierProjet(p, e, env, head, modeProjet(p.Nom) == "ignorer"), maxTexteTache-1), e.Serveurs, forcee, p.Nom)
	r.Etat, r.Commit, r.ID, r.Erreur, r.Accord = "a_preparer", head, "", "", ""
	if r.Date == "" {
		r.Date = time.Now().UTC().Format(time.RFC3339)
	}
	if err == nil {
		r.Etat, r.Premier = "preparation", t.ID
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: p.Nom, Env: env,
			Action: "premier déploiement", Resultat: t.ID + " (commit " + head + ")"})
	}
	ecrireRelais(p.Nom, r)
	return t, err
}

// suivrePremierDeploiement (M284): the first deployment's task has ended: with a method now, the relay shows the
// deployment that task made of its commit, or its commit waits for the regular relay (the task deployed an older
// one); still without a method, the deployment is to prepare (said as an information).
func (s *Serveur) suivrePremierDeploiement(p Projet, e Environnement, r RelaisJalon) {
	if tacheEnCoursID(r.Premier) {
		return
	}
	r.Etat = "a_preparer"
	servi := false
	if e.Deploiement != nil {
		r.Etat = "attente"
		for _, d := range historiqueDeploiements(p.Nom, s.envJalon(p.Nom)) {
			if d.Commit == r.Commit {
				r.Etat, r.ID, servi = "lance", d.ID, d.Etat == "ok" // the last one made of this commit
			}
		}
	}
	ecrireRelais(p.Nom, r)
	if servi {
		s.notifierRelais(p.Nom, true, r.Jalon)
	}
}

// apiPreparerDeploiement (M284): POST /api/projets/{nom}/preparer-deploiement {forcer, mot_de_passe}: "Prepare
// now", the first deployment of a project that has servers and no method yet, launched by the owner. Native
// Windows mode: as for any Servers task, refused with the WSL proposal unless forced with Athanor's password.
func (s *Serveur) apiPreparerDeploiement(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Forcer     bool   `json:"forcer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	projet := r.PathValue("nom")
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	p, _ := s.projetNomme(projet)
	sansRappel := s.sansRappelsNatif()
	forcee := modeNatif.Load() && (c.Forcer || sansRappel)
	if modeNatif.Load() && !forcee {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": errServeursNatif.Error(), "proposer_wsl": true})
		return
	}
	if forcee && !sansRappel && !s.preuveServeur(w, r, c.MotDePasse, "premier déploiement forcé refusé", projet) {
		return
	}
	if lireRelais()[p.Nom].Pause { // M298: the owner's request lets a paused or stopped project's deployment go again, its preparation first
		pauserProjet(p.Nom, false)
		s.preparerProjet(p)
	}
	t, err := s.premierDeploiementProjet(p, forcee)
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"tache": t, "relais": s.etatRelais(projet)})
}

// nomsServeurs: the inventory names of ids (an id the inventory no longer knows stays as it is).
// nomsRoles: the names of ids, each with its role in the owner's words in brackets when it has one.
func nomsRoles(ids []string, roles map[string]string) []string {
	l := nomsServeurs(ids)
	for i, id := range ids {
		if r := roles[id]; r != "" {
			l[i] += " (" + r + ")"
		}
	}
	return l
}

func nomsServeurs(ids []string) []string {
	l, _ := lireServeurs()
	noms := []string{}
	for _, id := range ids {
		n := id
		if i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id }); i >= 0 {
			n = l[i].Nom
		}
		noms = append(noms, n)
	}
	return noms
}

// premierARelancer: a first deployment that did not go through (servers, no method recorded, no task of it at work,
// and a try made: its task or a version deployed by hand); never tried, the relay starts it at its moment as before.
func premierARelancer(e Environnement, r RelaisJalon, essais int) bool {
	return len(e.Serveurs) > 0 && e.Deploiement == nil && r.Etat != "preparation" && !tacheEnCoursID(r.Premier) && (r.Premier != "" || essais > 0)
}

// reprendreDeploiement: the owner's Start (Studio, a mission's Resume) takes the project's deployment up where it
// failed: its first deployment launched again with the plan as it is now (its result in the relay's record).
func (s *Serveur) reprendreDeploiement(p Projet) {
	if s.acces == nil {
		return
	}
	env := s.envJalon(p.Nom)
	if premierARelancer(s.acces.Environnements(p.Nom)[env], lireRelais()[p.Nom], len(historiqueDeploiements(p.Nom, env))) {
		_, _ = s.premierDeploiementProjet(p, modeNatif.Load() && s.sansRappelsNatif())
	}
}

// ajouterPlanServeurs: texte added at the end of the plan for the servers of p's deployment (Discuss: a remark for
// the servers); the changed plan prepares the servers again (preparerProjet), a stuck first deployment goes again.
func (s *Serveur) ajouterPlanServeurs(p Projet, texte, qui string) error {
	texte = strings.TrimSpace(texte)
	if s.acces == nil || texte == "" {
		return erreurEntree("This message has no addition for the servers")
	}
	e := s.acces.Environnements(p.Nom)[s.deploiementProjet(p.Nom)]
	if len(e.Serveurs) == 0 {
		return erreurEntree("This project has no server")
	}
	plan := strings.TrimSpace(e.Plan)
	if plan != "" {
		plan += "\n\n"
	}
	if err := s.definirDeploiementProjet(p.Nom, DeploiementDemande{Serveurs: e.Serveurs, URL: e.URL, Plan: plan + texte, Moment: e.Moment, APIs: e.APIs}, qui); err != nil {
		return err
	}
	s.preparerProjet(p)
	s.reprendreDeploiement(p)
	return nil
}
