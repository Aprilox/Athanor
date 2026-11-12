package main

// The hub's approvals (ARCHITECTURE.md, stage 4): what an agent asks and only the owner decides, in
// hub/approbations.json; the "do not ask again" rules in hub/regles.json, set only by an owner's decision
// (never by an MCP tool). exigerAccord is the hub's own check of I6: prod or destructive → wait for an
// accord, unless a rule says otherwise. Who decides: Athanor's window or a paired device; in native
// Windows mode an agent can read instance.json and get a window cookie, so the window must retype
// Athanor's password there (no password: a paired device only).

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// dureeApprobation: an approval not decided within this delay expires (refused, for the agent).
const dureeApprobation = 24 * time.Hour

// C29: bounds against a flood of requests (phone, approbations.json).
const (
	maxAttenteProjet  = 20                  // pending approvals per project
	delaiNotification = 5 * time.Minute     // one phone notification per project within this delay
	gardeApprobations = 30 * 24 * time.Hour // a decided or expired approval is purged after this delay
)

var (
	errApprobationInconnue = erreurEntree("Unknown approval")
	errApprobationDecidee  = erreurEntree("This approval was already decided or has expired")
	errApprobationDecision = erreurEntree("Unknown decision (accepter, refuser or toujours)")
	errApprobationEntree   = erreurEntree("Choose a project key (code) of the vault for this access")
	errRegleInconnue       = erreurEntree("Unknown rule")
	errTropDemandes        = errors.New("too many requests are waiting for the owner on this project: wait for their decisions")
)

// Approbation: one request of an agent. Etat: attente | acceptee | refusee | expiree; Par: fenetre |
// appareil:<id>; T: the time of the decision (or of the expiry).
type Approbation struct {
	ID       string            `json:"id"`
	Projet   string            `json:"projet"`
	Env      string            `json:"env"`
	Mission  string            `json:"mission,omitempty"`
	Agent    string            `json:"agent"`
	Action   string            `json:"action"`
	Details  map[string]string `json:"details,omitempty"`
	Raison   string            `json:"raison"`
	Cree     string            `json:"cree"`
	Expire   string            `json:"expire"`
	Etat     string            `json:"etat"`
	Par      string            `json:"par,omitempty"`
	T        string            `json:"t,omitempty"`
	Utilisee string            `json:"utilisee,omitempty"` // an accepted action done (I6: one accord, one action)
}

// RegleAccord: "do not ask again" for this action of this project in this environment; for an access,
// only the vault entry the owner granted (Entree), under the variable name (Nom).
type RegleAccord struct {
	Projet string `json:"projet"`
	Env    string `json:"env"`
	Action string `json:"action"`
	Entree string `json:"entree,omitempty"`
	Nom    string `json:"nom,omitempty"`
}

// Approbations: the store. Maintenant (nil: time.Now) is for the tests.
type Approbations struct {
	mu         sync.Mutex
	dossier    string
	liste      []Approbation
	regles     []RegleAccord
	notifie    map[string]time.Time // project -> last phone notification
	Maintenant func() time.Time
}

func dossierApprobations() string { return filepath.Join(dossierAthanor(), "hub") }

// ouvrirApprobations reads both files (missing: empty; unreadable: empty, and the error).
func ouvrirApprobations(dossier string) (*Approbations, error) {
	a := &Approbations{dossier: dossier}
	var errs []error
	for nom, v := range map[string]any{"approbations.json": &a.liste, "regles.json": &a.regles} {
		b, err := os.ReadFile(filepath.Join(dossier, nom))
		if err == nil {
			err = json.Unmarshal(b, v)
		}
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("%s: %w", nom, err))
		}
	}
	if len(errs) > 0 {
		a.liste, a.regles = nil, nil // shortcut: an unreadable file loses the pending approvals, the agents ask again
	}
	return a, errors.Join(errs...)
}

func (a *Approbations) maintenant() time.Time {
	if a.Maintenant != nil {
		return a.Maintenant().UTC()
	}
	return time.Now().UTC()
}

func (a *Approbations) enregistrer() error {
	if err := os.MkdirAll(a.dossier, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a.liste, "", "  ")
	if err := ecrireAtomique(filepath.Join(a.dossier, "approbations.json"), b, 0o600); err != nil {
		return err
	}
	b, _ = json.MarshalIndent(a.regles, "", "  ")
	return ecrireAtomique(filepath.Join(a.dossier, "regles.json"), b, 0o600)
}

// expirer marks the pending approvals past their delay and purges the ones decided long ago (lock
// held); says whether one changed.
func (a *Approbations) expirer() bool {
	t, change := a.maintenant(), false
	garde := a.liste[:0]
	for _, x := range a.liste {
		if exp, err := time.Parse(time.RFC3339, x.Expire); x.Etat == "attente" && (err != nil || !t.Before(exp)) {
			x.Etat, x.T, change = "expiree", t.Format(time.RFC3339), true
		}
		if d, err := time.Parse(time.RFC3339, x.T); x.Etat != "attente" && err == nil && t.Sub(d) > gardeApprobations {
			change = true
			continue
		}
		garde = append(garde, x)
	}
	a.liste = garde
	return change
}

// bornerApprobation cuts an agent's text to a sane length.
func bornerApprobation(x *Approbation) {
	x.Raison = couper(x.Raison, 500)
	d := map[string]string{}
	for k, v := range x.Details {
		d[k] = couper(v, 200)
	}
	x.Details = d
}

// Demander records a new pending approval (at most maxAttenteProjet pending per project).
func (a *Approbations) Demander(x Approbation) (Approbation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.maintenant()
	x.ID, x.Etat, x.Par, x.T, x.Utilisee = aleatoire(8), "attente", "", "", ""
	d := dureeApprobation
	if x.Action == actionPlanServeur {
		d = dureePlan
	}
	x.Cree, x.Expire = t.Format(time.RFC3339), t.Add(d).Format(time.RFC3339)
	bornerApprobation(&x)
	a.expirer()
	n := 0
	for _, y := range a.liste {
		if y.Projet == x.Projet && y.Etat == "attente" {
			n++
		}
	}
	if n >= maxAttenteProjet {
		_ = a.enregistrer()
		return Approbation{}, errTropDemandes
	}
	a.liste = append(a.liste, x)
	if err := a.enregistrer(); err != nil {
		a.liste = a.liste[:len(a.liste)-1]
		return Approbation{}, err
	}
	return x, nil
}

func couper(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Liste: every approval, newest first, the expired ones marked; and the rules.
func (a *Approbations) Liste() ([]Approbation, []RegleAccord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.expirer() {
		_ = a.enregistrer()
	}
	l := slices.Clone(a.liste)
	sort.SliceStable(l, func(i, j int) bool { return l[i].Cree > l[j].Cree })
	return l, slices.Clone(a.regles)
}

// Lire: the approval id (expiry applied).
func (a *Approbations) Lire(id string) (Approbation, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.expirer() {
		_ = a.enregistrer()
	}
	for _, x := range a.liste {
		if x.ID == id {
			return x, true
		}
	}
	return Approbation{}, false
}

// Reprendre (I6, C63) looks for the same request (project, environment, mission, agent, action, details): an accepted one not
// used yet is used now ("utilisee"), else a pending one is returned ("attente"); "" when none.
func (a *Approbations) Reprendre(x Approbation) (Approbation, string, error) {
	bornerApprobation(&x)
	a.mu.Lock()
	defer a.mu.Unlock()
	change := a.expirer()
	var attente *Approbation
	for i := range a.liste {
		y := &a.liste[i]
		if y.Projet != x.Projet || y.Env != x.Env || y.Mission != x.Mission || y.Agent != x.Agent || y.Action != x.Action || !maps.Equal(y.Details, x.Details) {
			continue
		}
		if y.Etat == "acceptee" && y.Utilisee == "" {
			y.Utilisee = a.maintenant().Format(time.RFC3339)
			if err := a.enregistrer(); err != nil {
				y.Utilisee = ""
				return Approbation{}, "", err
			}
			return *y, "utilisee", nil
		}
		if y.Etat == "attente" {
			attente = y
		}
	}
	if change {
		_ = a.enregistrer()
	}
	if attente != nil {
		return *attente, "attente", nil
	}
	return Approbation{}, "", nil
}

// ANotifier (C29): at most one phone notification per project within delaiNotification.
func (a *Approbations) ANotifier(projet string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.maintenant()
	if d, ok := a.notifie[projet]; ok && t.Sub(d) < delaiNotification {
		return false
	}
	if a.notifie == nil {
		a.notifie = map[string]time.Time{}
	}
	a.notifie[projet] = t
	return true
}

// PourAgent: the state an agent sees: attente, acceptee or refusee (an expired approval is refused).
func (a *Approbations) PourAgent(id string) string {
	x, ok := a.Lire(id)
	switch {
	case !ok, x.Etat == "expiree":
		return "refusee"
	}
	return x.Etat
}

// Decider: accepter | refuser | toujours (accepted, and a rule for its project, environment and action).
func (a *Approbations) Decider(id, decision, par string) (Approbation, error) {
	return a.DeciderAvec(id, decision, par, "")
}

// DeciderAvec: Decider, with the vault entry an accepted access granted (the rule is bound to it).
func (a *Approbations) DeciderAvec(id, decision, par, entree string) (Approbation, error) {
	if !slices.Contains([]string{"accepter", "refuser", "toujours"}, decision) {
		return Approbation{}, errApprobationDecision
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expirer()
	i := slices.IndexFunc(a.liste, func(x Approbation) bool { return x.ID == id })
	if i < 0 {
		_ = a.enregistrer()
		return Approbation{}, errApprobationInconnue
	}
	x := &a.liste[i]
	if x.Etat != "attente" {
		_ = a.enregistrer()
		return *x, errApprobationDecidee
	}
	avant, regles := *x, slices.Clone(a.regles)
	x.Etat, x.Par, x.T = "acceptee", par, a.maintenant().Format(time.RFC3339)
	if decision == "refuser" {
		x.Etat = "refusee"
	}
	if decision == "toujours" { // the rule lets the action through: this accord is not kept for one more
		x.Utilisee = x.T
	}
	r := RegleAccord{x.Projet, x.Env, x.Action, "", ""}
	if x.Action == actionPlanServeur { // "do not ask again for this server"
		r = reglePlan(x.Projet, x.Details["serveur"])
	}
	if entree != "" {
		r.Entree, r.Nom = entree, x.Details["nom"]
	}
	if decision == "toujours" && !slices.Contains(a.regles, r) {
		a.regles = append(a.regles, r)
	}
	if err := a.enregistrer(); err != nil {
		*x, a.regles = avant, regles
		return avant, err
	}
	return *x, nil
}

// RefuserTout (emergency stop): every pending approval, and every accepted server plan not over yet, refused
// by par (no step of it runs any more); the refused ones.
func (a *Approbations) RefuserTout(par string) ([]Approbation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expirer()
	var out []Approbation
	t := a.maintenant().Format(time.RFC3339)
	for i := range a.liste {
		x := &a.liste[i]
		if x.Etat == "attente" || x.Etat == "acceptee" && x.Action == actionPlanServeur && t < x.Expire { // same RFC 3339 UTC form
			x.Etat, x.Par, x.T = "refusee", par, t
			out = append(out, *x)
		}
	}
	return out, a.enregistrer()
}

// RetirerRegles removes the rules f selects (Ask again, emergency stop); the removed ones.
func (a *Approbations) RetirerRegles(f func(RegleAccord) bool) ([]RegleAccord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out, garde []RegleAccord
	for _, r := range a.regles {
		if f(r) {
			out = append(out, r)
		} else {
			garde = append(garde, r)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	avant := a.regles
	if a.regles = garde; a.enregistrer() != nil {
		a.regles = avant
		return nil, errors.New("hub/regles.json: not written")
	}
	return out, nil
}

// Regle: the owner said "do not ask again" for this action of this project in this environment.
func (a *Approbations) Regle(r RegleAccord) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.regles, r)
}

// ----------------------------------------------------------------------------- server side

func (s *Serveur) chargerApprobations() {
	var err error
	if s.approbations, err = ouvrirApprobations(dossierApprobations()); err != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Action: "approbations illisibles", Resultat: "reprises à vide"})
	}
}

// demanderApprobation: a new pending approval, in the audit log.
func (s *Serveur) demanderApprobation(x Approbation) (Approbation, error) {
	x, err := s.approbations.Demander(x)
	res := "attente " + x.ID
	if err != nil {
		res = "erreur"
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: x.Env,
		Agent: x.Agent, Action: "approbation demandée: " + x.Action, Resultat: res})
	// Test the hub's MCP decides by itself: no phone ping; a task's plan is told once its critique is on it (critiquerPlan)
	if err == nil && x.Projet != projetTestMCP && !(x.Action == actionPlanServeur && strings.HasPrefix(x.Projet, prefixeTache)) {
		s.notifierApprobation(x)
	}
	return x, err
}

// notifierApprobation (P7): the paired devices are told once, at the creation (the tag replaces nothing
// else); a tap opens For you, where the approval is decided.
func (s *Serveur) notifierApprobation(x Approbation) {
	if !s.approbations.ANotifier(x.Projet) { // C29: the next ones are in For you, without a ping each
		return
	}
	s.notifierAppareils(s.messageApprobation(x))
}

// messageApprobation: the notification of an approval, as the paired devices receive it.
func (s *Serveur) messageApprobation(x Approbation) MessagePush {
	quoi := s.textePush("appr")
	if x.Action == "demander_acces" {
		quoi = s.textePush("appr_acces")
	}
	texte := x.Raison
	if n := x.Details["nom"]; n != "" {
		texte = n + " (" + x.Env + ") · " + texte
	}
	titre := fmt.Sprintf(s.textePush("appr_titre"), moduleDev{}.Nom(), x.Projet, quoi)
	if x.Action == actionDeploiement { // "Deploy martin.fr to production?", the project and commit below
		titre, texte = fmt.Sprintf(s.textePush("appr_deploi"), x.Details["hote"], x.Env), x.Projet+" · commit "+x.Details["commit"]
		if x.Env == "prod" {
			titre = fmt.Sprintf(s.textePush("appr_prod"), x.Details["hote"])
		}
	}
	if x.Action == actionPlanServeur { // "Plan for web-1", the plan's summary below
		titre, texte = fmt.Sprintf(s.textePush("appr_plan"), cmp.Or(x.Details["nom"], x.Details["serveur"])), x.Projet+" · "+x.Raison
	}
	return MessagePush{Titre: titre, Texte: texte, URL: "/#pourvous", Tag: "appr-" + x.ID}
}

// exigerAccord (I6): an action on prod or that destroys data waits for the owner's accord, unless the
// owner set a rule for it or accepted this very request (used once); when it must wait, the pending
// approval (the same one when it is asked again) is returned with true.
// shortcut: no "autonomous" mission yet (stage 6 adds it), so only a rule or an accord lifts the wait.
func (s *Serveur) exigerAccord(x Approbation, destructif bool) (Approbation, bool, error) {
	if x.Env != "prod" && !destructif || s.approbations.Regle(RegleAccord{Projet: x.Projet, Env: x.Env, Action: x.Action}) {
		return x, false, nil
	}
	y, etat, err := s.approbations.Reprendre(x)
	switch {
	case err != nil:
		return x, false, err
	case etat == "utilisee":
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: x.Env,
			Agent: x.Agent, Action: "approbation utilisée: " + x.Action, Resultat: y.ID})
		return y, false, nil
	case etat == "attente":
		return y, true, nil
	}
	x, err = s.demanderApprobation(x)
	return x, true, err
}

// verrouDecision: one decision at a time (accepting an access grants it before the approval is marked).
var verrouDecision sync.Mutex

// apiApprobations: GET /api/hub/approbations, the approvals and rules, and what the page needs to decide.
func (s *Serveur) apiApprobations(w http.ResponseWriter, r *http.Request) {
	l, regles := s.approbations.Liste()
	s.mu.Lock()
	mdp := s.config.MotDePasseHash != ""
	s.mu.Unlock()
	// appareil_requis: no password yet, nothing can decide (preuvePresence)
	s.ecrireJSON(w, map[string]any{"approbations": l, "regles": regles, "plans": s.plansAffiches(l),
		"mot_de_passe_requis": appareilDe(r) == "" && mdp, "appareil_requis": !mdp})
}

// apiApprobationDecider: POST /api/hub/approbations {id, decision: accepter|refuser|toujours, entree, env,
// variable, mot_de_passe}. Accepting a demander_acces grants the chosen code entry and rewrites the
// project's secrets.env: the pilot reads it at its next command, without a restart.
func (s *Serveur) apiApprobationDecider(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID, Decision, Entree, Env, Variable, Projet, Action, Nom string
		MotDePasse                                               string `json:"mot_de_passe"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, string(errCoffreRequete))
		return
	}
	qui := deQui(r)
	refus := func(code int, err error) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Action: "décision refusée: " + c.Decision, Resultat: c.ID + ", " + err.Error()})
		s.erreur(w, code, err.Error())
	}
	if c.Decision == "redemander" { // B52: Ask again only lowers what agents get, so no password for it
		l, err := s.approbations.RetirerRegles(func(x RegleAccord) bool { return x == RegleAccord{c.Projet, c.Env, c.Action, c.Entree, c.Nom} })
		switch {
		case err != nil:
			refus(http.StatusInternalServerError, err)
		case len(l) == 0:
			refus(http.StatusNotFound, errRegleInconnue)
		default:
			s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: c.Projet, Env: c.Env, Action: "règle retirée: " + c.Action, Resultat: "redemander"})
			s.ecrireJSON(w, map[string]any{"ok": true})
		}
		return
	}
	if code, err := s.preuvePresence(r, c.MotDePasse); err != nil { // C24 C25: WSL mode too
		refus(code, err)
		return
	}
	x, code, err := s.deciderApprobation(qui, c.ID, c.Decision, c.Entree, c.Env, c.Variable)
	if err != nil {
		refus(code, err)
		return
	}
	s.ecrireJSON(w, x)
}

// deciderApprobation: the decision once the decider is known (the window, a paired device, or Test the
// hub's MCP); an error comes with its HTTP status.
func (s *Serveur) deciderApprobation(qui, id, decision, entree, envChoisi, variable string) (Approbation, int, error) {
	verrouDecision.Lock()
	defer verrouDecision.Unlock()
	x, ok := s.approbations.Lire(id)
	switch {
	case !ok:
		return x, http.StatusNotFound, errApprobationInconnue
	case x.Etat != "attente":
		return x, http.StatusConflict, errApprobationDecidee
	case !slices.Contains([]string{"accepter", "refuser", "toujours"}, decision):
		return x, http.StatusBadRequest, errApprobationDecision
	case decision == "toujours" && x.Action == actionPlanServeur && x.Details["regle"] != "oui":
		return x, http.StatusConflict, errPlanSansRegle
	case decision == "toujours" && x.Action == actionDeploiement && x.Env == "prod": // I6: no rule ever lets prod through
		return x, http.StatusConflict, errEnvProdAuto
	case decision != "refuser" && x.Action == actionPlanServeur && s.critiqueEnCours(x.ID):
		return x, http.StatusConflict, errCritiqueEnCours
	}
	accorde, idEntree := "", ""
	if x.Action == "demander_acces" && decision != "refuser" {
		if code, err := s.accesPretErr(x.Projet); err != nil {
			return x, code, err
		}
		e := s.entreeCoffre(entree)
		if e.Portee != "code" || e.Famille != "secret" {
			return x, http.StatusConflict, errApprobationEntree
		}
		env := firstNonEmpty(envChoisi, x.Env)
		if variable == "" && nomSecretValide(x.Details["nom"]) {
			variable = x.Details["nom"]
		}
		idEntree = e.ID
		a, err := s.acces.Ajouter(x.Projet, e, env, variable, "")
		if err != nil {
			return x, http.StatusConflict, err
		}
		s.auditAcces(qui, x.Projet, "acces_accorde", e, a.Variable, a.Env)
		accorde = ", " + a.Variable + " (" + a.Env + ")"
		if p, ok := s.projetNomme(x.Projet); ok {
			if _, err := s.injecterAcces(p, nil); err != nil {
				fmt.Fprintln(os.Stderr, "access injection:", p.Nom+":", err)
			}
		}
	}
	x, err := s.approbations.DeciderAvec(id, decision, qui, idEntree)
	if err != nil {
		return x, http.StatusInternalServerError, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: x.Env, Agent: x.Agent,
		Action: "approbation " + decision, Resultat: x.Action + " " + x.ID + " → " + x.Etat + accorde})
	if s.planDurcissementAccepte(x) { // M138: Athanor's own plan, run by Athanor
		go s.appliquerDurcissement(x.ID)
	}
	return x, 0, nil
}
