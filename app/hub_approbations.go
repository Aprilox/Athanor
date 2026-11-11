package main

// The hub's approvals (ARCHITECTURE.md, stage 4): what an agent asks and only the owner decides, in
// hub/approbations.json; the "do not ask again" rules in hub/regles.json, set only by an owner's decision
// (never by an MCP tool). exigerAccord is the hub's own check of I6: prod or destructive → wait for an
// accord, unless a rule says otherwise. Who decides: Athanor's window or a paired device; in native
// Windows mode an agent can read instance.json and get a window cookie, so the window must retype
// Athanor's password there (no password: a paired device only).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// dureeApprobation: an approval not decided within this delay expires (refused, for the agent).
const dureeApprobation = 24 * time.Hour

var (
	errApprobationInconnue = erreurEntree("Unknown approval")
	errApprobationDecidee  = erreurEntree("This approval was already decided or has expired")
	errApprobationDecision = erreurEntree("Unknown decision (accepter, refuser or toujours)")
	errApprobationEntree   = erreurEntree("Choose a project key (code) of the vault for this access")
	errApprobationNatif    = erreurEntree("Native Windows mode: any program of this PC can drive this window, so an approval is decided from a paired phone, or choose a password for Athanor first")
)

// Approbation: one request of an agent. Etat: attente | acceptee | refusee | expiree; Par: fenetre |
// appareil:<id>; T: the time of the decision (or of the expiry).
type Approbation struct {
	ID      string            `json:"id"`
	Projet  string            `json:"projet"`
	Env     string            `json:"env"`
	Mission string            `json:"mission,omitempty"`
	Agent   string            `json:"agent"`
	Action  string            `json:"action"`
	Details map[string]string `json:"details,omitempty"`
	Raison  string            `json:"raison"`
	Cree    string            `json:"cree"`
	Expire  string            `json:"expire"`
	Etat    string            `json:"etat"`
	Par     string            `json:"par,omitempty"`
	T       string            `json:"t,omitempty"`
}

// RegleAccord: "do not ask again" for this action of this project in this environment.
type RegleAccord struct {
	Projet string `json:"projet"`
	Env    string `json:"env"`
	Action string `json:"action"`
}

// Approbations: the store. Maintenant (nil: time.Now) is for the tests.
type Approbations struct {
	mu         sync.Mutex
	dossier    string
	liste      []Approbation
	regles     []RegleAccord
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

// expirer marks the pending approvals past their delay (lock held); says whether one changed.
func (a *Approbations) expirer() bool {
	t, change := a.maintenant(), false
	for i := range a.liste {
		x := &a.liste[i]
		if exp, err := time.Parse(time.RFC3339, x.Expire); x.Etat == "attente" && (err != nil || !t.Before(exp)) {
			x.Etat, x.T, change = "expiree", t.Format(time.RFC3339), true
		}
	}
	return change
}

// Demander records a new pending approval (an agent's text is cut to a sane length).
func (a *Approbations) Demander(x Approbation) (Approbation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.maintenant()
	x.ID, x.Etat, x.Par, x.T = aleatoire(8), "attente", "", ""
	x.Cree, x.Expire = t.Format(time.RFC3339), t.Add(dureeApprobation).Format(time.RFC3339)
	x.Raison = couper(x.Raison, 500)
	for k, v := range x.Details {
		x.Details[k] = couper(v, 200)
	}
	a.expirer()
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
	r := RegleAccord{x.Projet, x.Env, x.Action}
	if decision == "toujours" && !slices.Contains(a.regles, r) {
		a.regles = append(a.regles, r)
	}
	if err := a.enregistrer(); err != nil {
		*x, a.regles = avant, regles
		return avant, err
	}
	return *x, nil
}

// RefuserTout (emergency stop): every pending approval refused by par; the refused ones.
func (a *Approbations) RefuserTout(par string) ([]Approbation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expirer()
	var out []Approbation
	t := a.maintenant().Format(time.RFC3339)
	for i := range a.liste {
		if x := &a.liste[i]; x.Etat == "attente" {
			x.Etat, x.Par, x.T = "refusee", par, t
			out = append(out, *x)
		}
	}
	return out, a.enregistrer()
}

// Regle: the owner said "do not ask again" for this action of this project in this environment.
func (a *Approbations) Regle(projet, env, action string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.regles, RegleAccord{projet, env, action})
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
	if err == nil && x.Projet != projetTestMCP { // Test the hub's MCP decides by itself: no phone ping
		s.notifierApprobation(x)
	}
	return x, err
}

// notifierApprobation (P7): the paired devices are told once, at the creation (the tag replaces nothing
// else); a tap opens For you, where the approval is decided.
func (s *Serveur) notifierApprobation(x Approbation) {
	quoi := s.textePush("appr")
	if x.Action == "demander_acces" {
		quoi = s.textePush("appr_acces")
	}
	texte := x.Raison
	if n := x.Details["nom"]; n != "" {
		texte = n + " (" + x.Env + ") · " + texte
	}
	s.notifierAppareils(MessagePush{Titre: fmt.Sprintf(s.textePush("appr_titre"), moduleDev{}.Nom(), x.Projet, quoi),
		Texte: texte, URL: "/#pourvous", Tag: "appr-" + x.ID})
}

// exigerAccord (I6): an action on prod or that destroys data waits for the owner's accord, unless the
// owner set a rule for it; when it must wait, the pending approval is created and returned with true.
// shortcut: no "autonomous" mission yet (stage 6 adds it), so only a rule lifts the wait.
func (s *Serveur) exigerAccord(x Approbation, destructif bool) (Approbation, bool, error) {
	if x.Env != "prod" && !destructif || s.approbations.Regle(x.Projet, x.Env, x.Action) {
		return x, false, nil
	}
	x, err := s.demanderApprobation(x)
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
	fenetreNatif := appareilDe(r) == "" && modeNatif.Load()
	s.ecrireJSON(w, map[string]any{"approbations": l, "regles": regles,
		"mot_de_passe_requis": fenetreNatif && mdp, "appareil_requis": fenetreNatif && !mdp})
}

// apiApprobationDecider: POST /api/hub/approbations {id, decision: accepter|refuser|toujours, entree, env,
// variable, mot_de_passe}. Accepting a demander_acces grants the chosen code entry and rewrites the
// project's secrets.env: the pilot reads it at its next command, without a restart.
func (s *Serveur) apiApprobationDecider(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID, Decision, Entree, Env, Variable string
		MotDePasse                          string `json:"mot_de_passe"`
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
	if appareilDe(r) == "" && modeNatif.Load() {
		s.mu.Lock()
		sel, hash := s.config.Sel, s.config.MotDePasseHash
		s.mu.Unlock()
		if hash == "" {
			refus(http.StatusForbidden, errApprobationNatif)
			return
		}
		time.Sleep(400 * time.Millisecond) // slows down bursts of attempts, as apiDeverrouiller
		if ok, _ := verifierMotDePasse(sel, c.MotDePasse, hash); !ok {
			refus(http.StatusUnauthorized, errors.New("Wrong password"))
			return
		}
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
	}
	accorde := ""
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
	x, err := s.approbations.Decider(id, decision, qui)
	if err != nil {
		return x, http.StatusInternalServerError, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: x.Env, Agent: x.Agent,
		Action: "approbation " + decision, Resultat: x.Action + " " + x.ID + " → " + x.Etat + accorde})
	return x, 0, nil
}
