package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// RelaisJalon (M136): the dev deployment of a project's HEAD after a milestone delivered, one per milestone.
// Recorded before it starts, so that a restart neither repeats nor loses it; its state once started is the
// deployment history's (Etat here only says where the relay itself is).
type RelaisJalon struct {
	Jalon  string `json:"jalon"`
	Commit string `json:"commit"`
	Date   string `json:"date"` // when the milestone was seen done (RFC 3339)
	// attente (not pushed yet) | lance | echec (not started); M284, a project without a deployment method yet:
	// preparation (its first deployment's task works) | a_preparer (to prepare: an information, never a failure)
	Etat   string `json:"etat"`
	ID     string `json:"id,omitempty"`     // the deployment, once started
	Erreur string `json:"erreur,omitempty"` // why it did not start
	Accord string `json:"accord,omitempty"` // the owner's accord asked for its changed build files (C68)
	// M282: the last delivery noted (<project>/.athanor/done/.last-delivery) and whether the project's deliveries
	// are watched yet (the first look only learns); both survive the relays that follow
	Livraison    string `json:"livraison,omitempty"`
	LivraisonVue bool   `json:"livraison_vue,omitempty"`
	// M283: the server preparation launched for the project's deployment (deploiement_projet.go); survives the relays too
	Preparation *PreparationRelais `json:"preparation,omitempty"`
	// M284: the Servers task of the first deployment (premierDeploiementProjet); survives the relays too
	Premier string `json:"premier,omitempty"`
	// M298: the owner paused or stopped the project: no preparation, no deployment, no plan of its tasks until
	// Start; only pauserProjet changes it
	Pause bool `json:"pause,omitempty"`
}

// attentePushRelais: how long a milestone's commit may wait to be on GitHub (git source) before the relay tries anyway.
const attentePushRelais = 30 * time.Minute

var relaisMu sync.Mutex

func fichierRelais() string { return filepath.Join(dossierModule("hub"), "relais_jalons.json") }

func lireRelais() map[string]RelaisJalon {
	m := map[string]RelaisJalon{}
	lireJSONFichier(fichierRelais(), &m)
	return m
}

func ecrireRelais(projet string, r RelaisJalon) {
	relaisMu.Lock()
	defer relaisMu.Unlock()
	m := lireRelais()
	r.Pause = m[projet].Pause // the owner's, whatever the caller read before they paused
	m[projet] = r
	_ = ecrireJSONFichier(fichierRelais(), m, 0o600)
}

// pauserProjet (M298): the project's deployment held (pause, stop now) or let go again (Start, Prepare now).
func pauserProjet(projet string, pause bool) {
	relaisMu.Lock()
	defer relaisMu.Unlock()
	m := lireRelais()
	if r := m[projet]; r.Pause != pause {
		r.Pause = pause
		m[projet] = r
		_ = ecrireJSONFichier(fichierRelais(), m, 0o600)
	}
}

// deplacerRelais: a renamed project keeps its record; nouveau "": the project is removed, it goes.
func deplacerRelais(ancien, nouveau string) {
	relaisMu.Lock()
	defer relaisMu.Unlock()
	m := lireRelais()
	r, ok := m[ancien]
	if !ok {
		return
	}
	delete(m, ancien)
	if nouveau != "" {
		m[nouveau] = r
	}
	_ = ecrireJSONFichier(fichierRelais(), m, 0o600)
}

// momentRelais (M282): when the project's deployment is made by itself: jalon | livraison | "" (on request, or
// M284: no server, nothing is tried, noted nor said).
func (s *Serveur) momentRelais(projet string) string {
	if s.acces == nil {
		return ""
	}
	e := s.acces.Environnements(projet)[s.envJalon(projet)]
	if len(e.Serveurs) == 0 {
		return ""
	}
	return e.Moment
}

// sansMethodeRelais (M284): a relay of an older version that failed for want of a deployment method.
func sansMethodeRelais(r RelaisJalon) bool {
	return r.Etat == "echec" && r.ID == "" && strings.Contains(r.Erreur, "no deployment method")
}

// relaisActif: the project's deployment is made by itself, after each milestone or at delivery.
func (s *Serveur) relaisActif(projet string) bool { return s.momentRelais(projet) != "" }

// envJalon (M279): the environment deployed after each milestone is the project's one deployment (never a
// Production one, validerEnvironnement refuses it).
func (s *Serveur) envJalon(projet string) string { return s.deploiementProjet(projet) }

// noterJalonRelais: a milestone just done → its HEAD waits to be deployed (never twice the same milestone), when
// the project is deployed after each milestone (M282: not when it is deployed at delivery).
func (s *Serveur) noterJalonRelais(p Projet, jalon string, quand time.Time) {
	r := lireRelais()[p.Nom]
	if s.momentRelais(p.Nom) != "jalon" || r.Jalon == jalon {
		return
	}
	s.attendreRelais(p, r, jalon, "relais après jalon", quand)
}

// noterLivraison (M282): a delivery of the project (the pilot's .last-delivery changed) is noted before anything
// else, so that a restart never deploys it twice; then, when the project has a moment (a delivery also closes
// "after each milestone") and its HEAD is not what was last deployed with success, that HEAD waits to be
// deployed. The first look at a project only learns: a delivery older than this version deploys nothing.
func (s *Serveur) noterLivraison(p Projet, quand time.Time) {
	b, _ := os.ReadFile(filepath.Join(p.DemandesFaites(), ".last-delivery"))
	v, r := strings.TrimSpace(string(b)), lireRelais()[p.Nom]
	if r.LivraisonVue && r.Livraison == v || s.acces == nil || len(s.acces.Environnements(p.Nom)[s.envJalon(p.Nom)].Serveurs) == 0 {
		return // M284: without a server nothing is noted (a server linked later first learns)
	}
	premier := !r.LivraisonVue
	r.LivraisonVue, r.Livraison = true, v
	ecrireRelais(p.Nom, r)
	if premier || v == "" || !s.relaisActif(p.Nom) {
		return
	}
	sha, env, servi := sortieGit(p.Dossier, "rev-parse", "HEAD"), s.envJalon(p.Nom), ""
	for _, d := range historiqueDeploiements(p.Nom, env) {
		if d.Etat == "ok" {
			servi = d.Commit
		}
		if d.Etat == "en_cours" && d.Commit == sha {
			return // the last milestone's relay is deploying this HEAD
		}
	}
	if servi == sha || r.Etat == "attente" && r.Commit == sha {
		return
	}
	s.attendreRelais(p, r, s.textePush("livre"), "relais à la livraison", quand)
}

// attendreRelais: the project's HEAD waits to be deployed, for the milestone or the delivery named by jalon; what
// avant noted of the deliveries and of the preparation stays.
func (s *Serveur) attendreRelais(p Projet, avant RelaisJalon, jalon, action string, quand time.Time) {
	r := RelaisJalon{Jalon: jalon, Date: quand.UTC().Format(time.RFC3339), Etat: "attente", Livraison: avant.Livraison, LivraisonVue: avant.LivraisonVue, Preparation: avant.Preparation, Premier: avant.Premier}
	if sha, err := gitSortie(p.Dossier, "rev-parse", "HEAD"); err == nil && reCommitComplet.MatchString(sha) {
		r.Commit = sha
	} else {
		r.Etat, r.Erreur = "echec", "the project has no commit to deploy"
	}
	ecrireRelais(p.Nom, r)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Projet: p.Nom, Env: s.envJalon(p.Nom), Action: action, Resultat: jalon + " → " + r.Commit})
}

// avancerRelais: the waiting relay of p started once its commit can be fetched (git source: on a remote
// branch, or after attentePushRelais); returns a channel closed when that deployment ends (nil: none started).
// M284: without a deployment method yet, the waiting relay (or an older version's failure for want of one) starts
// the project's first deployment instead of failing, and the record follows that task.
func (s *Serveur) avancerRelais(p Projet, maintenant time.Time) <-chan struct{} {
	r, ok := lireRelais()[p.Nom]
	if !ok || s.acces == nil {
		return nil
	}
	env := s.envJalon(p.Nom)
	e := s.acces.Environnements(p.Nom)[env]
	if r.Etat == "preparation" {
		s.suivrePremierDeploiement(p, e, r)
		return nil
	}
	if r.Etat != "attente" && !(sansMethodeRelais(r) && e.Deploiement == nil) {
		return nil
	}
	if e.Moment == "" || len(e.Serveurs) == 0 || projetBloque("dev", p.Nom) != "" { // a paused project: the relay waits for Start
		return nil
	}
	if e.Deploiement == nil {
		_, _ = s.premierDeploiementProjet(p, modeNatif.Load() && s.sansRappelsNatif()) // its result is in the record
		return nil
	}
	if e.Deploiement != nil && e.Deploiement.Source == "git" {
		depuis, _ := time.Parse(time.RFC3339, r.Date)
		if sortieGit(p.Dossier, "branch", "-r", "--contains", r.Commit) == "" && maintenant.Sub(depuis) < attentePushRelais {
			return nil // the pilot pushes after the milestone's commit
		}
	}
	accorde, attendre := s.accordBuildRelais(p, e, &r)
	if attendre {
		return nil
	}
	d, suite, err := s.commencerDeploiement(p.Nom, env, r.Commit, OptionsDeploiement{Auteur: "relais", Accorde: accorde})
	if err != nil {
		r.Etat, r.Erreur = "echec", s.masquerSecrets(err.Error())
		ecrireRelais(p.Nom, r)
		s.notifierRelais(p.Nom, false, r.Jalon)
		return nil
	}
	r.Etat, r.ID = "lance", d.ID
	ecrireRelais(p.Nom, r)
	fini := make(chan struct{})
	go func() {
		defer close(fini)
		d, _ := suite(context.Background()) // its result is in the history
		if d.Etat == "ok" {
			s.notifierRelais(p.Nom, true, r.Jalon)
		} else {
			s.notifierIncidents() // the incident tells it, with its three actions
		}
	}()
	return fini
}

func (s *Serveur) notifierRelais(projet string, ok bool, jalon string) {
	cle := "relais_echec"
	if ok {
		cle = "relais_ok"
	}
	s.notifierAppareils(MessagePush{Titre: s.textePush(cle) + " · " + projet, Texte: jalon, URL: "/#suivi", Tag: "relais-" + projet})
}

// etatRelais: the project's last relay with the state of its deployment (nil: relay off or nothing relayed yet).
// M284: a project with servers and no deployment method yet is never a failure: "preparation" while its first
// deployment's task works, else "a_preparer" when nothing makes it by itself (on request, or that task did not
// make it), which the owner's "Prepare now" launches.
func (s *Serveur) etatRelais(projet string) *RelaisJalon {
	if s.acces == nil {
		return nil
	}
	e := s.acces.Environnements(projet)[s.envJalon(projet)]
	r, ok := lireRelais()[projet]
	switch {
	case r.Etat == "preparation":
		return &r
	case len(e.Serveurs) == 0 && r.ID == "":
		return nil // no server: nothing to say (a deployment made before its server was unlinked stays readable)
	case len(e.Serveurs) > 0 && e.Deploiement == nil && (e.Moment == "" || r.Etat == "a_preparer" || sansMethodeRelais(r)):
		return &RelaisJalon{Etat: "a_preparer", Jalon: r.Jalon, Commit: r.Commit, Date: r.Date}
	case e.Moment == "" || !ok || r.Etat == "" || r.Etat == "a_preparer":
		return nil // nothing relayed yet (a record may only hold what was learnt of the deliveries)
	}
	if r.Etat == "lance" {
		h := historiqueDeploiements(projet, s.envJalon(projet))
		if i := slices.IndexFunc(h, func(d Deploiement) bool { return d.ID == r.ID }); i >= 0 {
			r.Etat, r.Erreur = h[i].Etat, h[i].Erreur // en_cours | ok | echec | revenu
			if h[i].Verification != nil && !h[i].Verification.OK && r.Erreur == "" {
				r.Erreur = "the external check failed"
			}
		}
	}
	return &r
}

// alertesRelais: the bell rings for a relay that failed (not started, or its deployment failed).
func (s *Serveur) alertesRelais() []AlerteHub {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	out := []AlerteHub{}
	for _, p := range projets {
		if p.Archive {
			continue
		}
		v := s.etatRelais(p.Nom)
		if v == nil || v.Etat != "echec" || v.ID != "" && s.incidentEnv(lireSante(), p.Nom, s.envJalon(p.Nom)) != nil { // a failed deployment: the incident rings
			continue
		}
		h := sha256.Sum256([]byte("relais\x00" + p.Nom + "\x00" + v.Jalon + "\x00" + v.Commit))
		empreinte := hex.EncodeToString(h[:12])
		out = append(out, AlerteHub{Empreinte: empreinte, Module: "dev", Element: p.Nom, Titre: p.Nom,
			Raison: "Dev deployment after the milestone failed ({0}): {1}", Args: []any{v.Jalon, v.Erreur}, Actions: []ActionAlerte{
				{ID: "voir", Libelle: "View", Page: "acces", Projet: p.Nom}, ignorerOuRouvrir(empreinte, true)}})
	}
	return out
}

// anciensSitesProduction (M302): the sites of projet that are not its deployment, marked Production and deployed
// before: a mission put them online, nothing does any more until the owner chooses one as the project's deployment.
func (s *Serveur) anciensSitesProduction(projet string) []string {
	var out []string
	courant := s.deploiementProjet(projet)
	for nom, e := range s.acces.Environnements(projet) {
		if nom != courant && e.Production && len(historiqueDeploiements(projet, nom)) > 0 {
			out = append(out, nom)
		}
	}
	slices.Sort(out)
	return out
}

// texteAncienSite: what the Deployment tab and the bell say of such a site ({0}: its address).
const texteAncienSite = "{0} was put online by a mission: make it the project's deployment to go on deploying it."

// alertesAnciensSites: the bell's information (Info: never shown as a failure) for each of them.
func (s *Serveur) alertesAnciensSites() []AlerteHub {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	out := []AlerteHub{}
	for _, p := range projets {
		if p.Archive || s.acces == nil {
			continue
		}
		envs := s.acces.Environnements(p.Nom)
		for _, nom := range s.anciensSitesProduction(p.Nom) {
			e := envs[nom]
			empreinte := "ancien-site-" + p.Nom + "-" + nom
			out = append(out, AlerteHub{Empreinte: empreinte, Module: "dev", Element: p.Nom, Titre: p.Nom, Info: true,
				Raison: texteAncienSite, Args: []any{cmp.Or(hoteEnv(&e), nom)}, Actions: []ActionAlerte{
					{ID: "voir", Libelle: "View", Page: "acces", Projet: p.Nom}, ignorerOuRouvrir(empreinte, true)}})
		}
	}
	return out
}

// accordBuildRelais (C68): ticking "Deploy to dev at each milestone" accepts the deployments, not a build whose
// files changed since the last deployment the owner accepted: for those, the relay asks the owner once and
// waits (attendre); accorde says the owner accepted this one. Refused or expired: the relay fails, said.
// M282: an older Production site, which a delivery can now deploy, always asks the same way.
func (s *Serveur) accordBuildRelais(p Projet, e Environnement, r *RelaisJalon) (accorde, attendre bool) {
	env := s.envJalon(p.Nom)
	f := fichiersBuildChanges(p, env, r.Commit, e.Deploiement)
	if len(f) == 0 && !s.prudenceRenforcee(p.Nom, env) {
		return false, false
	}
	liste, raison := couper(strings.Join(f, ", "), 300), "commit "+r.Commit+" → "+hoteEnv(&e)
	if liste != "" {
		raison += "; build files changed: " + liste
	}
	if s.sautIgnorer(p.Nom, env, "relais", "accord de déploiement non demandé: "+raison) { // M285
		return true, false
	}
	ap := Approbation{Projet: p.Nom, Env: env, Agent: "relais", Action: actionDeploiement, Raison: raison,
		Details: map[string]string{"commit": r.Commit, "hote": hoteEnv(&e), "serveurs": listeServeursAccord(e.Serveurs), "fichiers_build": liste}}
	if c := s.etatCoffre(); len(e.Serveurs) > 0 && c.MotDePasse && !c.Infra { // R7: the infra keys locked, the accord would be used for a deployment that cannot run
		return false, true
	}
	y, etat, err := s.approbations.Reprendre(ap)
	switch {
	case err != nil || etat == "attente":
		return false, true
	case etat == "utilisee":
		return true, false
	case r.Accord != "": // asked already, and no longer waiting
		if y, ok := s.approbations.Lire(r.Accord); ok && y.Etat == "acceptee" {
			return true, false
		}
		r.Etat, r.Erreur = "echec", "the owner did not accept this deployment (changed build files: "+cmp.Or(liste, "none")+")"
		ecrireRelais(p.Nom, *r)
		s.notifierRelais(p.Nom, false, r.Jalon)
		return false, true
	}
	if y, err = s.demanderApprobation(ap); err == nil {
		r.Accord = y.ID
		ecrireRelais(p.Nom, *r)
	}
	return false, true
}
