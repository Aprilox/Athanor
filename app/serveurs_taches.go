package main

// Servers tasks (ARCHITECTURE.md §Boucle légère, M97): the owner's text and the servers it may touch. A task gets a
// "servers" token whose perimeter is its servers (revoked at its end), an empty session folder outside every project
// with the MCP config of the hub's relay, and a Claude session (the ops lead) that reads, then proposes a plan. The
// session ends there: Athanor waits for the owner's decision, then resumes the same session to apply the accepted
// plan. States: preparation → critique (the security critic reads the plan) → attente_accord → application → verification → finie | echec | arretee, in
// modules/servers/<server>/taches/<id>.json. Stop (the owner, a revocation, the emergency stop) cuts everything.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	prefixeTache      = "servers:tache:"   // the project of a task's session: never a real project
	prefixeServeur    = "servers:serveur:" // the project of a "do not ask again" rule a task's plan gives (C54)
	maxCommande       = 2000               // characters of a command (C51)
	delaiSessionTache = 20 * time.Minute   // one Claude session of a task
	maxPlansTache     = 5                  // plans proposed, decided and applied in turn by one task
	maxTexteTache     = 4000
)

// TacheServeurs: one task, as stored and shown.
type TacheServeurs struct {
	ID           string             `json:"id"`
	Texte        string             `json:"texte"`
	Serveurs     []string           `json:"serveurs"`
	Etat         string             `json:"etat"`
	Forcee       bool               `json:"forcee,omitempty"` // native Windows mode, forced by the owner with the password
	Cree         string             `json:"cree"`
	Fin          string             `json:"fin,omitempty"`
	Plans        []string           `json:"plans,omitempty"`
	Resultat     string             `json:"resultat,omitempty"`
	Verification *VerificationTache `json:"verification,omitempty"` // the external verifier's report (M98)
	Historique   []EvenementTache   `json:"historique"`
}

// EvenementTache: a state reached, and what brought it (a plan's id, an error).
type EvenementTache struct {
	T      string `json:"t"`
	Etat   string `json:"etat"`
	Detail string `json:"detail,omitempty"`
}

// tacheEnCours: a running task, its token's hash and how to stop it.
type tacheEnCours struct {
	mu       sync.Mutex
	t        TacheServeurs
	hache    string
	annuler  context.CancelCauseFunc
	reprise  string // the Claude session to resume
	applique string // the plan being applied
	annexe   string // the token's hash of the critic's or the verifier's session while it runs
}

var tachesEnCours = struct {
	sync.Mutex
	m map[string]*tacheEnCours // by task id
}{m: map[string]*tacheEnCours{}}

// instanceSessionsServeurs: the Athanor the sessions' relay reaches, when not this one (the real test's hub, M102).
var instanceSessionsServeurs string

var (
	errClaudeNonConnecte = errors.New("Claude Code is not logged in where the pilot runs (/login, or Settings › Claude connection)")
	errTacheArretee      = errors.New("stopped by the owner")
	errTacheRevoquee     = errors.New("session token revoked")
)

// prefixeVerifEchouee: translated by the page (a PREFIXES key of i18n.js), the verifier's lines kept as they are.
const prefixeVerifEchouee = "the external verification failed: "

// sessionTacheServeurs runs one Claude session of a task in dossier (consigne.txt, reprise, hub-jeton and mcp.json
// are there; the session works in dossier/travail and writes its stream to session.jsonl). Tests replace it.
var sessionTacheServeurs = func(s *Serveur, ctx context.Context, dossier string) error {
	c := commandeKit(s.kitCourant(), "session-serveurs", dossier)
	if instanceSessionsServeurs != "" {
		c.Args = append(c.Args, instanceSessionsServeurs)
	}
	c.Env = envSansSecrets(s, c.Env)
	cacherFenetre(c)
	if err := c.Start(); err != nil {
		return err
	}
	fin := make(chan error, 1)
	go func() { fin <- c.Wait() }()
	select {
	case err := <-fin:
		return err
	case <-ctx.Done():
		arreterArbre(c)
		<-fin
		return context.Cause(ctx)
	case <-time.After(delaiSessionTache + time.Minute):
		arreterArbre(c)
		<-fin
		return errors.New("the session exceeded its time limit")
	}
}

// envSansSecrets: env (nil: Athanor's own) without a variable named like a secret or holding a known vault value.
func envSansSecrets(s *Serveur, env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	var out []string
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		if EstSecret(k) || (len(v) >= 6 && s.masquerSecrets(v) != v) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func dossierTaches(serveur string) string {
	return filepath.Join(dossierModule("servers"), serveur, "taches")
}
func dossierSessionTache(id string) string {
	return filepath.Join(dossierModule("servers"), "sessions", id)
}

// tacheActive: the session of hache belongs to a running task (its token stays valid while it runs).
func tacheActive(hache string) bool {
	tachesEnCours.Lock()
	defer tachesEnCours.Unlock()
	for _, e := range tachesEnCours.m {
		e.mu.Lock()
		a := e.annexe
		e.mu.Unlock()
		if e.hache == hache || a == hache {
			return true
		}
	}
	return false
}

// tacheForcee: the session of hache belongs to a running task the owner forced in native Windows mode.
func tacheForcee(hache string) bool {
	tachesEnCours.Lock()
	defer tachesEnCours.Unlock()
	for _, e := range tachesEnCours.m {
		e.mu.Lock()
		if e.hache == hache || e.annexe == hache {
			defer e.mu.Unlock()
			return e.t.Forcee
		}
		e.mu.Unlock()
	}
	return false
}

// etat: a new state; the same state for the same reason again (the watch and the task's end both reach
// verification) is written once.
func (e *tacheEnCours) etat(etat, detail string) {
	e.mu.Lock()
	if h := e.t.Historique; len(h) > 0 && h[len(h)-1].Etat == etat && h[len(h)-1].Detail == couper(detail, 300) {
		e.mu.Unlock()
		return
	}
	e.t.Etat = etat
	e.t.Historique = append(e.t.Historique, EvenementTache{T: time.Now().UTC().Format(time.RFC3339), Etat: etat, Detail: couper(detail, 300)})
	t := e.t
	e.mu.Unlock()
	_ = enregistrerTache(t)
}

func (e *tacheEnCours) copie() TacheServeurs {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.t
	t.Serveurs, t.Plans, t.Historique = slices.Clone(t.Serveurs), slices.Clone(t.Plans), slices.Clone(t.Historique)
	return t
}

func enregistrerTache(t TacheServeurs) error {
	d := dossierTaches(t.Serveurs[0])
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return ecrireAtomique(filepath.Join(d, t.ID+".json"), b, 0o600)
}

// lancerTacheServeurs: a new task on serveurs (inventory ids, the first one holds the task's file), run in the
// background; forcee: the owner forced it in native Windows mode (the API asks the password first).
func (s *Serveur) lancerTacheServeurs(texte string, serveurs []string, forcee bool) (TacheServeurs, error) {
	texte = strings.TrimSpace(s.masquerSecrets(texte))
	switch {
	case texte == "" || len([]rune(texte)) > maxTexteTache:
		return TacheServeurs{}, erreurEntree(fmt.Sprintf("Say what to do, in %d characters at most", maxTexteTache))
	case len(serveurs) == 0:
		return TacheServeurs{}, erreurEntree("A task needs at least one server")
	case modeNatif.Load() && !forcee:
		return TacheServeurs{}, errServeursNatif
	case s.jetons == nil || s.approbations == nil:
		return TacheServeurs{}, errors.New("the hub's sessions or approvals cannot be read")
	}
	liste, err := lireServeurs()
	if err != nil {
		return TacheServeurs{}, err
	}
	for _, id := range serveurs {
		if !slices.ContainsFunc(liste, func(x ServeurInventaire) bool { return x.ID == id }) {
			return TacheServeurs{}, errServeurInconnu
		}
	}
	t := TacheServeurs{ID: aleatoire(6), Texte: texte, Serveurs: slices.Clone(serveurs), Forcee: forcee, Cree: time.Now().UTC().Format(time.RFC3339)}
	dossier := dossierSessionTache(t.ID)
	if err := os.MkdirAll(filepath.Join(dossier, "travail"), 0o700); err != nil {
		return TacheServeurs{}, err
	}
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: prefixeTache + t.ID, Mission: t.ID, Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: t.Serveurs})
	if err == nil {
		err = ecrireAtomique(filepath.Join(dossier, "hub-jeton"), []byte(jeton+"\n"), 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dossier, "consigne.txt"), []byte(consigneChefOps(texte, t.Serveurs)), 0o600)
	}
	if err != nil {
		_, _ = s.jetons.Revoquer(func(x SessionHub) bool { return x.Projet == prefixeTache+t.ID })
		_ = os.RemoveAll(dossier)
		return TacheServeurs{}, err
	}
	ctx, annuler := context.WithCancelCause(context.Background())
	e := &tacheEnCours{t: t, hache: hacherJeton(jeton), annuler: annuler}
	tachesEnCours.Lock()
	tachesEnCours.m[t.ID] = e
	tachesEnCours.Unlock()
	e.etat("preparation", "")
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "owner", Vers: "servers", Projet: prefixeTache + t.ID, Mission: t.ID, Agent: agentServeurs,
		Action: "tâche serveurs lancée", Resultat: strings.Join(t.Serveurs, ", ") + map[bool]string{true: " (forcée en mode natif)"}[forcee]})
	go s.executerTache(ctx, e, dossier)
	return e.copie(), nil
}

// executerTache: the task's sessions in turn, then its end (token revoked, session folder removed).
func (s *Serveur) executerTache(ctx context.Context, e *tacheEnCours, dossier string) {
	projet := prefixeTache + e.t.ID
	surveillance := make(chan struct{})
	go s.surveillerTache(ctx, e, surveillance)
	err := s.sessionTache(ctx, e, dossier)
	fin, detail := "finie", ""
	var appliques []string
	for n := 0; err == nil && n < maxPlansTache; n++ {
		plan := s.planNeufTache(projet, e)
		if plan == "" {
			break
		}
		if s.critiqueEnCours(plan) { // a plan accepted by the owner's rule is not presented: no critique
			e.etat("critique", plan)
			s.critiquerPlan(ctx, e, dossier, plan)
			if ctx.Err() != nil {
				break
			}
		}
		e.etat("attente_accord", plan)
		ap, errA := s.attendreDecision(ctx, plan)
		if errA != nil {
			err = errA
			break
		}
		if ap.Etat != "acceptee" {
			fin, detail = "finie", "plan "+plan+" "+ap.Etat
			e.mu.Lock()
			e.t.Resultat = "Plan " + plan + " " + ap.Etat + ": nothing was changed."
			e.mu.Unlock()
			break
		}
		e.mu.Lock()
		e.applique = plan
		e.mu.Unlock()
		e.etat("application", plan)
		appliques = append(appliques, plan)
		if err = os.WriteFile(filepath.Join(dossier, "consigne.txt"), []byte(consigneApplication(plan)), 0o600); err == nil {
			err = s.sessionTache(ctx, e, dossier)
		}
	}
	if err == nil && ctx.Err() == nil && len(appliques) > 0 {
		e.etat("verification", appliques[len(appliques)-1])
		if v := s.verifierTache(ctx, e, dossier, appliques); v.Verdict != "ok" && ctx.Err() == nil {
			err = errors.New(prefixeVerifEchouee + strings.Join(v.Lignes, " · "))
		}
	}
	close(surveillance)
	switch cause := context.Cause(ctx); {
	case cause != nil:
		fin, detail = "arretee", cause.Error()
	case err != nil:
		fin, detail = "echec", s.masquerSecrets(err.Error())
	}
	s.finirTache(e, fin, detail)
	_ = os.RemoveAll(dossier)
}

// surveillerTache: a revoked token (emergency stop, the owner's revocation, expiry) stops the task; during the
// application, the plan's last step run turns it to verification.
func (s *Serveur) surveillerTache(ctx context.Context, e *tacheEnCours, fin chan struct{}) {
	for {
		select {
		case <-fin:
			return
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		if !slices.ContainsFunc(s.jetons.Liste(), func(x SessionHub) bool { return x.Hache == e.hache }) {
			e.annuler(errTacheRevoquee)
			return
		}
		e.mu.Lock()
		etat, plan := e.t.Etat, e.applique
		e.mu.Unlock()
		if etat == "application" {
			if p, err := s.lirePlan(plan); err == nil && !slices.ContainsFunc(p.Etapes, func(x EtapePlan) bool { return x.Executee == "" }) {
				e.etat("verification", plan)
			}
		}
	}
}

// sessionTache: one Claude session; its session id kept to resume it, its last answer as the task's result.
func (s *Serveur) sessionTache(ctx context.Context, e *tacheEnCours, dossier string) error {
	e.mu.Lock()
	reprise := e.reprise
	e.mu.Unlock()
	if err := os.WriteFile(filepath.Join(dossier, "reprise"), []byte(reprise), 0o600); err != nil {
		return err
	}
	journal := filepath.Join(dossier, "session.jsonl")
	_ = os.Remove(journal)
	err := sessionTacheServeurs(s, ctx, dossier)
	id, resultat, enErreur := lireSessionClaude(journal)
	if b, _ := os.ReadFile(journal); bytes.Contains(b, []byte(`"error":"authentication_failed"`)) {
		return errClaudeNonConnecte
	}
	e.mu.Lock()
	if id != "" {
		e.reprise = id
	}
	if resultat != "" {
		e.t.Resultat = couper(s.masquerSecrets(resultat), maxTexteTache)
	}
	e.mu.Unlock()
	if err == nil && enErreur {
		err = errors.New("the Claude session ended with an error: " + couper(resultat, 200))
	}
	if err == nil && id == "" {
		err = errors.New("the Claude session did not start (no session in its journal)")
	}
	return err
}

// lireSessionClaude: the session id (init event), the last result's text and whether it is an error.
func lireSessionClaude(journal string) (string, string, bool) {
	f, err := os.Open(journal)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	var id, resultat string
	enErreur := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type, Subtype, Result string
			SessionID             string `json:"session_id"`
			IsError               bool   `json:"is_error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type == "system" && ev.Subtype == "init" && ev.SessionID != "" {
			id = ev.SessionID
		}
		if ev.Type == "result" {
			resultat, enErreur = ev.Result, ev.IsError
		}
	}
	return id, resultat, enErreur
}

// planNeufTache: the last plan the task's session proposed that the task has not handled yet ("" when none).
func (s *Serveur) planNeufTache(projet string, e *tacheEnCours) string {
	l, _ := s.approbations.Liste()
	e.mu.Lock()
	defer e.mu.Unlock()
	plan := ""
	for _, ap := range l {
		if ap.Projet == projet && ap.Action == actionPlanServeur && !slices.Contains(e.t.Plans, ap.ID) && ap.Cree >= e.t.Cree {
			plan = ap.ID
		}
	}
	if plan != "" {
		e.t.Plans = append(e.t.Plans, plan)
	}
	return plan
}

// attendreDecision: the plan's approval once decided (accepted, refused or expired), or the task's stop.
func (s *Serveur) attendreDecision(ctx context.Context, plan string) (Approbation, error) {
	for {
		ap, ok := s.approbations.Lire(plan)
		if !ok {
			return Approbation{}, errors.New("plan " + plan + " is gone")
		}
		if ap.Etat != "attente" {
			return ap, nil
		}
		select {
		case <-ctx.Done():
			return ap, context.Cause(ctx)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// finirTache: the final state, the token revoked (C33 cuts what it still runs), pending plans refused.
func (s *Serveur) finirTache(e *tacheEnCours, etat, detail string) {
	projet := prefixeTache + e.t.ID
	tachesEnCours.Lock()
	delete(tachesEnCours.m, e.t.ID)
	tachesEnCours.Unlock()
	s.revoquerJetons("tâche "+etat, func(x SessionHub) bool { return x.Projet == projet || strings.HasPrefix(x.Projet, projet+":") })
	l, _ := s.approbations.Liste()
	for _, ap := range l {
		if ap.Projet == projet && ap.Etat == "attente" {
			_, _, _ = s.deciderApprobation("hub", ap.ID, "refuser", "", "", "")
		}
	}
	e.mu.Lock()
	e.t.Fin = time.Now().UTC().Format(time.RFC3339)
	e.mu.Unlock()
	e.etat(etat, detail)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: projet, Mission: e.t.ID, Agent: agentServeurs,
		Action: "tâche serveurs " + etat, Resultat: couper(detail, 300)})
}

// arreterTache: the owner stops a running task (false: not running): its session is killed and its token revoked
// at once, which cuts what it still runs on a server (C33).
func (s *Serveur) arreterTache(id string) bool {
	tachesEnCours.Lock()
	e := tachesEnCours.m[id]
	tachesEnCours.Unlock()
	if e != nil {
		e.annuler(errTacheArretee)
		e.mu.Lock()
		a := e.annexe
		e.mu.Unlock()
		s.revoquerJetons("tâche arrêtée", func(x SessionHub) bool { return x.Hache == e.hache || x.Hache == a && a != "" })
	}
	return e != nil
}

// tachesServeur: the tasks of a server, newest first (50 at most); a running one as it is now.
func tachesServeur(id string) []TacheServeurs {
	fichiers, _ := filepath.Glob(filepath.Join(dossierTaches(id), "*.json"))
	l := []TacheServeurs{}
	for _, f := range fichiers {
		var t TacheServeurs
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &t) == nil {
			l = append(l, t)
		}
	}
	tachesEnCours.Lock()
	for i := range l {
		if e := tachesEnCours.m[l[i].ID]; e != nil {
			l[i] = e.copie()
		}
	}
	tachesEnCours.Unlock()
	slices.SortFunc(l, func(a, b TacheServeurs) int { return strings.Compare(b.Cree, a.Cree) })
	return l[:min(len(l), 50)]
}

// ----------------------------------------------------------------------------- API

// apiTachesServeur: GET /api/serveurs/{id}/taches (a paired device reads it too).
func (s *Serveur) apiTachesServeur(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if liste, err := lireServeurs(); err != nil || !slices.ContainsFunc(liste, func(x ServeurInventaire) bool { return x.ID == id }) {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	taches, plans := tachesServeur(id), map[string]PlanServeur{} // the plans as the page shows them (masked)
	for _, t := range taches {
		for _, p := range t.Plans {
			if s.approbations == nil {
				break
			}
			if x, err := s.planAffiche(p); err == nil {
				plans[p] = x
			}
		}
	}
	s.ecrireJSON(w, map[string]any{"taches": taches, "plans": plans, "natif": modeNatif.Load()})
}

// apiLancerTacheServeur: POST /api/serveurs/{id}/taches {texte, forcer, mot_de_passe}. Native Windows mode: refused
// with the WSL proposal, unless the owner forces it, with Athanor's password retyped.
func (s *Serveur) apiLancerTacheServeur(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte      string `json:"texte"`
		Forcer     bool   `json:"forcer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id := r.PathValue("id")
	forcee := modeNatif.Load() && c.Forcer
	if modeNatif.Load() && !c.Forcer {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": errServeursNatif.Error(), "proposer_wsl": true})
		return
	}
	if forcee && !s.preuveServeur(w, r, c.MotDePasse, "tâche serveurs forcée refusée", id) {
		return
	}
	t, err := s.lancerTacheServeurs(c.Texte, []string{id}, forcee)
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"tache": t})
}

// apiArreterTacheServeur: POST /api/serveurs/{id}/taches/{tache}/arreter.
func (s *Serveur) apiArreterTacheServeur(w http.ResponseWriter, r *http.Request) {
	id, tache := r.PathValue("id"), r.PathValue("tache")
	if !slices.ContainsFunc(tachesServeur(id), func(t TacheServeurs) bool { return t.ID == tache }) || !s.arreterTache(tache) {
		s.erreur(w, http.StatusNotFound, "No running task of that id on this server")
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Projet: prefixeTache + tache, Mission: tache, Action: "tâche serveurs arrêtée"})
	s.ecrireJSON(w, map[string]any{"ok": true})
}

// ----------------------------------------------------------------------------- the kit's side

func init() {
	enregistrerCommande("session-serveurs", "session-serveurs FOLDER [INSTANCE_FILE]: one Claude session of a Servers task (launched by the application)", lancerSessionServeurs)
}

// argsSessionServeurs: the ops lead's Claude: the hub's tools only (no built-in tool, no other MCP server).
func argsSessionServeurs(consigne, mcp, reprise string) []string {
	a := []string{"-p", consigne, "--mcp-config", mcp, "--strict-mcp-config", "--tools", "", "--allowedTools", "mcp__" + nomMCPHub,
		"--output-format", "stream-json", "--verbose"}
	if reprise != "" {
		a = append(a, "--resume", reprise)
	}
	return a
}

// lancerSessionServeurs: run where the pilot runs (Windows, WSL, macOS, Linux), in FOLDER/travail, an empty folder.
func lancerSessionServeurs(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "Launched by the application: a server's page › Tasks.")
		return 2
	}
	kit, _ := os.Getwd()
	dossier := cheminNatifPilote(args[0])
	consigne, err := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
	reprise, _ := os.ReadFile(filepath.Join(dossier, "reprise"))
	mcp := filepath.Join(dossier, "mcp.json")
	h := serveurMCPHub(kit, mcp)
	if err != nil || h == nil {
		fmt.Fprintln(os.Stderr, "The task's folder is incomplete (instructions or token missing).")
		return 1
	}
	if len(args) == 2 { // the real test names the Athanor that runs it (INSTANCE_FILE, like tester-mcp)
		var a []string
		_ = json.Unmarshal(h.valeur["args"], &a)
		h.mettre("args", append(a, args[1]))
	}
	var serveurs objetJSON
	serveurs.mettre(nomMCPHub, *h)
	if err := ecrireMCP(mcp, serveurs); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write the MCP file:", err)
		return 1
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Claude Code NOT FOUND: Setup & health › Install everything.")
		return 1
	}
	f, err := os.Create(filepath.Join(dossier, "session.jsonl"))
	if err != nil {
		return 1
	}
	defer f.Close()
	c := commande(claude, argsSessionServeurs(string(consigne), mcp, strings.TrimSpace(string(reprise)))...)
	c.Dir = filepath.Join(dossier, "travail")
	c.Env = append(envTesterCoffre(kit), "ENABLE_CLAUDEAI_MCP_SERVERS=false", "ENABLE_TOOL_SEARCH=false")
	c.Stdout, c.Stderr = f, f
	groupeSante(c)
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Claude Code does not start:", err)
		return 1
	}
	minuteur := time.AfterFunc(delaiSessionTache, func() { tuerGroupeSante(c) })
	defer minuteur.Stop()
	if c.Wait() != nil {
		return 1
	}
	return 0
}
