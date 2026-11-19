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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	// serveurAPI (M286): what stands for the server of a task on the owner's API alone: the folder of its file, the
	// {id} of its routes and the "serveur" of its plans. Never an inventory id (16 hexadecimal characters).
	serveurAPI = "api"
)

// TacheServeurs: one task, as stored and shown.
type TacheServeurs struct {
	ID           string             `json:"id"`
	Texte        string             `json:"texte"`
	Serveurs     []string           `json:"serveurs"`
	Etat         string             `json:"etat"`
	Forcee       bool               `json:"forcee,omitempty"` // native Windows mode, forced by the owner with the password
	Env          string             `json:"env,omitempty"`    // "projet/env": a task for an environment (M116)
	APIs         []string           `json:"apis,omitempty"`   // vault ids it was given (M286); with no server, an API-only task
	Mode         string             `json:"mode,omitempty"`   // the permission mode its launcher gave it (M286), "" for the older rules
	Cree         string             `json:"cree"`
	Fin          string             `json:"fin,omitempty"`
	Plans        []string           `json:"plans,omitempty"`
	Resultat     string             `json:"resultat,omitempty"`
	Verification *VerificationTache `json:"verification,omitempty"` // the external verifier's report (M98)
	Historique   []EvenementTache   `json:"historique"`
	Actions      []ActionTache      `json:"actions,omitempty"` // every tool call of its agents, live (M231e)
	Direct       bool               `json:"direct,omitempty"`  // "live in my terminal" (M231f): shown in the owner's terminal, its typing locked
	Terminal     string             `json:"-"`                 // that terminal's session (never sent: it opens a shell)
	Panneau      bool               `json:"panneau,omitempty"` // launched from the terminal's assistant panel: the server's permission mode applies (M244f)
	// Assistant: repos | travail | attente_decision | arretee | erreur, the one state the page's banner, chat and
	// terminal lock read; Verrou: one of its commands runs now in the owner's terminal, whose typing and files are
	// locked then, and only then (M242a).
	Assistant string `json:"assistant,omitempty"`
	Verrou    bool   `json:"verrou,omitempty"`
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
	ctx      context.Context
	annonce  bool      // a call locked the owner's terminal and its unlocking is not said yet
	finAppel time.Time // when its last call ended
}

const (
	delaiClavierLibre = 3 * time.Second         // no command of the assistant for that long: the owner's terminal says it is free
	maxDureeVerrou    = delaiSFTP + time.Minute // a call still "running" after that never locks (its end was lost)
)

// etatAssistant: a task's state as the assistant's one state (M242a).
func etatAssistant(etat string) string {
	switch etat {
	case "attente_accord":
		return "attente_decision"
	case "arretee":
		return "arretee"
	case "echec":
		return "erreur"
	case "finie":
		return "repos"
	}
	return "travail"
}

// arretee: the task was stopped (the owner, a revocation, the emergency stop), even while its session still dies.
func (e *tacheEnCours) arretee() bool { return e.ctx != nil && e.ctx.Err() != nil }

// verrouille: one of the task's calls runs now in the owner's terminal (e.mu held). Never while the assistant
// thinks, waits for a decision, is stopped or failed, nor once the owner took over.
func (e *tacheEnCours) verrouille(maintenant time.Time) bool {
	if !e.t.Direct || e.t.Terminal == "" || e.arretee() || etatAssistant(e.t.Etat) != "travail" {
		return false
	}
	for _, a := range e.t.Actions {
		if d, err := time.Parse(time.RFC3339, a.T); a.Etat == "en_cours" && err == nil && maintenant.Sub(d) < maxDureeVerrou {
			return true
		}
	}
	return false
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
	return executerSessionKit(ctx, commandeSessionKit(s, "session-serveurs", dossier), delaiSessionTache+time.Minute)
}

// commandeSessionKit: the kit's command of a hub session (COMMAND FOLDER), without any secret in its environment.
func commandeSessionKit(s *Serveur, commande, dossier string) *exec.Cmd {
	c := commandeKit(s.kitCourant(), commande, dossier)
	if instanceSessionsServeurs != "" {
		c.Args = append(c.Args, instanceSessionsServeurs)
	}
	c.Env = envSansSecrets(s, c.Env)
	cacherFenetre(c)
	return c
}

// executerSessionKit: c run until it ends, ctx stops or delai passes (the kit's own limit failed).
func executerSessionKit(ctx context.Context, c *exec.Cmd, delai time.Duration) error {
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
	case <-time.After(delai):
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
	if e.arretee() && etat != "arretee" { // a stopped task's late steps never bring it back to life
		e.mu.Unlock()
		return
	}
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
	t.Serveurs, t.Plans, t.Historique, t.Actions = slices.Clone(t.Serveurs), slices.Clone(t.Plans), slices.Clone(t.Historique), slices.Clone(t.Actions)
	t.Assistant, t.Verrou = etatAssistant(t.Etat), e.verrouille(time.Now())
	if e.arretee() {
		t.Assistant = "arretee"
	}
	return t
}

func enregistrerTache(t TacheServeurs) error {
	d := dossierTaches(slices.Concat(t.Serveurs, []string{serveurAPI})[0])
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return ecrireAtomique(filepath.Join(d, t.ID+".json"), b, 0o600)
}

// lancerTacheServeurs: a new task on serveurs (inventory ids, the first one holds the task's file), run in the
// background; forcee: the owner forced it in native Windows mode (the API asks the password first).
func (s *Serveur) lancerTacheServeurs(texte string, serveurs []string, forcee bool) (TacheServeurs, error) {
	return s.lancerTache(texte, serveurs, forcee, "")
}

// lancerTache: env "projet/env" (M116) gives the task that environment's servers and the host name of its URL, the
// only name its Cloudflare tools act on; serveurs must then be among the environment's.
func (s *Serveur) lancerTache(texte string, serveurs []string, forcee bool, env string) (TacheServeurs, error) {
	return s.lancerTacheDans(texte, serveurs, forcee, env, "", false, "", nil, "")
}

// lancerTacheDans: terminal, an open terminal's session: the task works "live" in it (M231f); panneau, launched from
// the terminal's assistant panel (its server's permission mode applies, M244f); contexte, what the agent reads
// before the request (contexteSuite: the task the owner stopped to send it), never shown as the request.
// M286: apis, vault ids the task may use through the Cloudflare tools, on the zones of their providers' sheets; with
// no server at all the token has no SSH, SFTP nor file tool. mode, the permission mode the launcher gives the task
// ("" keeps the older rules; an API-only task then takes Settings › Permissions' assistant mode).
func (s *Serveur) lancerTacheDans(texte string, serveurs []string, forcee bool, env, terminal string, panneau bool, contexte string, apis []string, mode string) (TacheServeurs, error) {
	texte = strings.TrimSpace(s.masquerSecrets(texte))
	switch {
	case texte == "" || len([]rune(texte)) > maxTexteTache:
		return TacheServeurs{}, erreurEntree(fmt.Sprintf("Say what to do, in %d characters at most", maxTexteTache))
	case len(serveurs) == 0 && len(apis) == 0:
		return TacheServeurs{}, erreurEntree("A task needs at least one server or one API")
	case len(apis) > 0 && env != "" || mode != "" && !slices.Contains(modesAutorisation, mode) || len(serveurs) == 0 && terminal != "":
		return TacheServeurs{}, erreurEntree("Invalid request")
	case modeNatif.Load() && !forcee:
		return TacheServeurs{}, errServeursNatif
	case s.jetons == nil || s.approbations == nil:
		return TacheServeurs{}, errors.New("the hub's sessions or approvals cannot be read")
	}
	var hotes []string
	if env != "" {
		projet, nom, _ := strings.Cut(env, "/")
		var e Environnement
		if s.acces != nil && slices.Contains(s.nomsProjets(), projet) {
			if nom == "" { // M279: "project" alone is its one deployment
				nom = s.deploiementProjet(projet)
				env = projet + "/" + nom
			}
			e = s.acces.Environnements(projet)[nom]
		}
		if len(e.Serveurs) == 0 || slices.ContainsFunc(serveurs, func(id string) bool { return !slices.Contains(e.Serveurs, id) }) {
			return TacheServeurs{}, erreurEntree("Unknown site, or this server is not one of its servers")
		}
		// all its servers, the given one first (it holds the task's file)
		serveurs = append(serveurs[:1:1], slices.DeleteFunc(slices.Clone(e.Serveurs), func(id string) bool { return id == serveurs[0] })...)
		if u, err := url.Parse(e.URL); err == nil && u.Hostname() != "" {
			hotes = []string{strings.ToLower(u.Hostname())}
		}
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
	outils, consigne := outilsServeursMCP, consigneChefOps(contexte+texte, serveurs)+consigneEnv(env, hotes)
	if len(apis) > 0 {
		for _, id := range apis {
			if s.coffre == nil || !slices.ContainsFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == id }) {
				return TacheServeurs{}, erreurEntree(errCoffreInconnu.Error())
			}
		}
		fiches, err := lireFournisseurs()
		if err != nil {
			return TacheServeurs{}, err
		}
		for _, f := range fiches {
			for _, z := range f.Zones {
				if slices.Contains(apis, f.Cle) && !slices.Contains(hotes, z.Nom) {
					hotes = append(hotes, z.Nom)
				}
			}
		}
		if len(hotes) > 0 { // an API without a provider sheet brings no zone
			consigne += consigneZones(hotes)
		}
		if len(serveurs) == 0 {
			outils, consigne = outilsAPISeule, consigneAPI(contexte+texte, hotes)
			mode = cmp.Or(mode, modeServeur(ServeurInventaire{})) // Settings › Permissions (assistant), else manuel
		}
	}
	t := TacheServeurs{ID: aleatoire(6), Texte: texte, Serveurs: append([]string{}, serveurs...), Forcee: forcee, Env: env, Cree: time.Now().UTC().Format(formatCree),
		Direct: terminal != "", Terminal: terminal, Panneau: panneau, APIs: slices.Clone(apis), Mode: mode}
	dossier := dossierSessionTache(t.ID)
	if err := os.MkdirAll(filepath.Join(dossier, "travail"), 0o700); err != nil {
		return TacheServeurs{}, err
	}
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: prefixeTache + t.ID, Mission: t.ID, Agent: agentServeurs, Outils: outils, Serveurs: t.Serveurs, Env: env, Hotes: hotes, APIs: apis})
	if err == nil {
		err = ecrireAtomique(filepath.Join(dossier, "hub-jeton"), []byte(jeton+"\n"), 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dossier, "consigne.txt"), []byte(consigne), 0o600)
	}
	if err != nil {
		_, _ = s.jetons.Revoquer(func(x SessionHub) bool { return x.Projet == prefixeTache+t.ID })
		_ = os.RemoveAll(dossier)
		return TacheServeurs{}, err
	}
	ctx, annuler := context.WithCancelCause(context.Background())
	e := &tacheEnCours{t: t, hache: hacherJeton(jeton), annuler: annuler, ctx: ctx}
	tachesEnCours.Lock()
	tachesEnCours.m[t.ID] = e
	tachesEnCours.Unlock()
	e.etat("preparation", "")
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "owner", Vers: "servers", Projet: prefixeTache + t.ID, Mission: t.ID, Agent: agentServeurs,
		Action: "tâche serveurs lancée", Resultat: strings.Join(slices.Concat(t.Serveurs, t.APIs), ", ") + map[bool]string{true: " (API seule)"}[len(t.Serveurs) == 0] +
			map[bool]string{true: " (mode " + mode + ")"}[mode != ""] + map[bool]string{true: " (forcée en mode natif)"}[forcee]})
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
		if err = os.WriteFile(filepath.Join(dossier, "consigne.txt"), []byte(consigneApplication(plan, len(e.t.Serveurs) == 0)), 0o600); err == nil {
			err = s.sessionTache(ctx, e, dossier)
		}
	}
	// M286: the external verifier visits servers; a task on an API alone has none, its agent's own reads are its check
	if err == nil && ctx.Err() == nil && len(appliques) > 0 && len(e.t.Serveurs) > 0 {
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
	_ = os.RemoveAll(dossier) // before the end is announced: a finished task leaves no token file behind
	s.finirTache(e, fin, detail)
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
			e.etat("arretee", errTacheRevoquee.Error())
			return
		}
		s.gardeFouVerrou(e, time.Now())
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
	s.revoquerJetons("tâche "+etat, func(x SessionHub) bool { return x.Projet == projet || strings.HasPrefix(x.Projet, projet+":") })
	s.refuserPlansEnAttente(projet)
	// its end written before it leaves the running ones: the page never reads a task neither running nor ended
	e.etat(etat, detail)
	e.mu.Lock()
	e.t.Fin = time.Now().UTC().Format(time.RFC3339)
	e.mu.Unlock()
	_ = enregistrerTache(e.copie()) // a stop may have written this state already: Fin is saved in any case
	tachesEnCours.Lock()
	delete(tachesEnCours.m, e.t.ID)
	tachesEnCours.Unlock()
	if context.Cause(e.ctx) != errTacheArretee { // a stop said it already
		ecrireDansTerminal(e, "\x1b[2m"+s.texteTerminal("The assistant's task ended: the keyboard is yours.")+"\x1b[0m\n")
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: projet, Mission: e.t.ID, Agent: agentServeurs,
		Action: "tâche serveurs " + etat, Resultat: couper(detail, 300)})
}

// refuserPlansEnAttente: the plans of a task's project still waiting for the owner, refused by the hub.
func (s *Serveur) refuserPlansEnAttente(projet string) {
	l, _ := s.approbations.Liste()
	for _, ap := range l {
		if ap.Projet == projet && ap.Etat == "attente" {
			_, _, _ = s.deciderApprobation("hub", ap.ID, "refuser", "", "", "")
		}
	}
}

// arreterTache: the owner stops a running task (false: not running): its session is killed and its token revoked
// at once, which cuts what it still runs on a server (C33).
func (s *Serveur) arreterTache(id string) bool {
	tachesEnCours.Lock()
	e := tachesEnCours.m[id]
	tachesEnCours.Unlock()
	if e != nil {
		deja := e.arretee() // a second Stop while its session dies says nothing again
		e.annuler(errTacheArretee)
		e.mu.Lock()
		a, dit := e.annexe, e.t.Direct && e.t.Terminal != "" && !deja
		e.annonce = false
		e.mu.Unlock()
		e.etat("arretee", errTacheArretee.Error()) // at once: the page and the lock never wait for its session to die
		if dit {
			ecrireDansTerminal(e, "\x1b[2m"+s.texteTerminal("The assistant was stopped: the keyboard is yours.")+"\x1b[0m\n")
		}
		s.refuserPlansEnAttente(prefixeTache + id) // a stopped task's plan never waits for a decision
		s.revoquerJetons("tâche arrêtée", func(x SessionHub) bool { return x.Hache == e.hache || x.Hache == a && a != "" })
	}
	return e != nil
}

// tacheInterrompue: why a task found unfinished at Athanor's start is closed (translated by the interface).
const tacheInterrompue = "Athanor stopped while this task ran"

// fermerTachesInterrompues (M312c): at Athanor's start no task runs, so one that never ended was cut by Athanor's
// own stop (killed, computer turned off): it is closed as stopped, its token revoked and its pending plans refused,
// instead of staying "at work" for ever with a relay waiting behind it; a project's first deployment or servers'
// preparation cut that way is made again, once, by the relay.
func (s *Serveur) fermerTachesInterrompues() {
	fichiers, _ := filepath.Glob(filepath.Join(dossierModule("servers"), "*", "taches", "*.json"))
	coupees := map[string]bool{}
	for _, f := range fichiers {
		var t TacheServeurs
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &t) != nil || t.ID == "" || t.Fin != "" {
			continue
		}
		tachesEnCours.Lock()
		vivante := tachesEnCours.m[t.ID] != nil
		tachesEnCours.Unlock()
		if vivante {
			continue
		}
		t.Etat, t.Fin = "arretee", time.Now().UTC().Format(time.RFC3339)
		t.Historique = append(t.Historique, EvenementTache{T: t.Fin, Etat: "arretee", Detail: tacheInterrompue})
		if enregistrerTache(t) != nil {
			continue
		}
		coupees[t.ID] = true
		projet := prefixeTache + t.ID
		s.revoquerJetons("tâche interrompue", func(x SessionHub) bool { return x.Projet == projet || strings.HasPrefix(x.Projet, projet+":") })
		s.refuserPlansEnAttente(projet)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "servers", Projet: projet, Mission: t.ID, Agent: agentServeurs,
			Action: "tâche serveurs arretee", Resultat: tacheInterrompue})
	}
	for nom, r := range lireRelais() {
		switch { // as after "Stop immediately" (arreterTachesProjet): each is made again, once
		case r.Etat == "preparation" && coupees[r.Premier]:
			r.Etat = "attente"
		case r.Preparation != nil && coupees[r.Preparation.Tache]:
			r.Preparation = &PreparationRelais{}
		default:
			continue
		}
		ecrireRelais(nom, r)
	}
}

// tachesServeur: the tasks of a server, newest first (50 at most); a running one as it is now.
func tachesServeur(id string) []TacheServeurs {
	fichiers, _ := filepath.Glob(filepath.Join(dossierTaches(id), "*.json"))
	l := []TacheServeurs{}
	for _, f := range fichiers {
		var t TacheServeurs
		b, err := os.ReadFile(f)
		for i := 0; err != nil && !os.IsNotExist(err) && i < 5; i++ { // Windows refuses the read while ecrireAtomique renames over it
			time.Sleep(20 * time.Millisecond)
			b, err = os.ReadFile(f)
		}
		if err == nil && json.Unmarshal(b, &t) == nil {
			l = append(l, t)
		}
	}
	tachesEnCours.Lock()
	for i := range l {
		if e := tachesEnCours.m[l[i].ID]; e != nil {
			l[i] = e.copie()
		} else {
			l[i].Verrou = false
			if l[i].Fin == "" { // not running and never ended: Athanor stopped while it ran
				l[i].Assistant = "erreur"
			}
		}
	}
	tachesEnCours.Unlock()
	slices.SortFunc(l, func(a, b TacheServeurs) int { return strings.Compare(b.Cree, a.Cree) })
	return l[:min(len(l), 50)]
}

// ----------------------------------------------------------------------------- API

// verrouPourSession: a terminal page reads the lock of its own session only: Go refuses the typing of the task's
// session alone, so a second tab or a phone on the same server is neither locked nor told "working".
func verrouPourSession(taches []TacheServeurs, session string) {
	for i := range taches {
		taches[i].Verrou = taches[i].Verrou && session != "" && taches[i].Terminal == session
	}
}

// apiTachesServeur: GET /api/serveurs/{id}/taches (a paired device reads it too).
func (s *Serveur) apiTachesServeur(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if liste, err := lireServeurs(); id != serveurAPI && (err != nil || !slices.ContainsFunc(liste, func(x ServeurInventaire) bool { return x.ID == id })) {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	taches, plans := tachesServeur(id), map[string]PlanServeur{} // the plans as the page shows them (masked)
	if r.URL.Query().Has("terminal") {
		verrouPourSession(taches, r.URL.Query().Get("terminal"))
	}
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
	srv, _ := serveurProd(id)
	s.ecrireJSON(w, map[string]any{"taches": taches, "plans": plans, "natif": modeNatif.Load(), "sans_rappels": s.sansRappelsNatif(), "autorisation": modeServeur(srv)})
}

// panneauDe (M244k2): the task is sent from the assistant panel of depuis, an open terminal of server id the window
// opened: the server's permission mode applies. Never on a request's word (a sheet's task, a device, panneau: true).
func panneauDe(r *http.Request, depuis, id string) bool {
	if depuis == "" || appareilDe(r) != "" {
		return false
	}
	terminaux.Lock()
	defer terminaux.Unlock()
	x := terminaux.m[depuis]
	return x != nil && x.serveur == id && x.de == "fenetre"
}

// contexteSuite: the stopped task suite of server id (its request, then its last message: its answer, or else its
// last action and what it printed), for the owner's next message to read as a follow-up, an answer to its question
// included; secrets masked, bounded. "" when there is no such task.
func (s *Serveur) contexteSuite(id, suite string) string {
	if suite == "" {
		return ""
	}
	l := tachesServeur(id)
	i := slices.IndexFunc(l, func(x TacheServeurs) bool { return x.ID == suite })
	if i < 0 {
		return ""
	}
	x, dernier := l[i], l[i].Resultat
	if a := len(x.Actions); dernier == "" && a > 0 {
		dernier = strings.TrimSpace(x.Actions[a-1].Quoi)
		if o := strings.TrimSpace(x.Actions[a-1].Sortie); o != "" { // M244k7: what the server printed stays data
			dernier += "\n" + enDonnees("server-output", "", couper(o, 2000))
		}
	}
	if dernier == "" {
		dernier = "(nothing yet)"
	}
	return s.masquerSecrets("The owner stopped your previous task to send the message below; it may answer a question you asked. " +
		"Carry on from where that task was, with this message.\n\nPrevious request:\n" + couper(x.Texte, maxTexteTache) +
		"\n\nYour last message on it:\n" + couper(dernier, 2600) + "\n\nThe owner's message now:\n")
}

// apiLancerTacheServeur: POST /api/serveurs/{id}/taches {texte, forcer, mot_de_passe}. Native Windows mode: refused
// with the WSL proposal, unless the owner forces it, with Athanor's password retyped.
// M286: apis (vault ids) and mode (the task's permission mode; "ignorer" as everywhere, choixIgnorer); {id} "api"
// launches a task on these API alone, with no server.
func (s *Serveur) apiLancerTacheServeur(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte      string   `json:"texte"`
		Forcer     bool     `json:"forcer"`
		Env        string   `json:"env"` // "projet" (its one deployment, M279) or "projet/env", optional (M116)
		MotDePasse string   `json:"mot_de_passe"`
		Terminal   string   `json:"terminal"` // "live in my terminal": its open session on this server (M231f)
		Depuis     string   `json:"depuis"`   // the open terminal whose assistant panel sends it (M244f, M244k2)
		Suite      string   `json:"suite"`    // "Send now": the task stopped for it, whose request and last message go with it (M244k4)
		APIs       []string `json:"apis"`
		Mode       string   `json:"mode"`
		Confirmer  bool     `json:"confirmer"` // the Ignore mode's warning was read
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id := r.PathValue("id")
	if c.Mode != "" {
		if code, err := s.choixIgnorer(r, c.Mode, c.Confirmer, c.MotDePasse); err != nil {
			s.erreur(w, code, err.Error())
			return
		}
	}
	serveurs := []string{id}
	if id == serveurAPI {
		serveurs = nil
	}
	for _, sess := range []string{c.Terminal, c.Depuis} {
		if sess == "" {
			continue
		}
		terminaux.Lock()
		x := terminaux.m[sess]
		terminaux.Unlock()
		if x == nil || x.serveur != id {
			s.terminalFerme(w, "")
			return
		}
	}
	sansRappel := s.sansRappelsNatif() // the owner turned native mode's reminders off: no question, no password
	forcee := modeNatif.Load() && (c.Forcer || sansRappel)
	if modeNatif.Load() && !forcee {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": errServeursNatif.Error(), "proposer_wsl": true})
		return
	}
	if forcee && !sansRappel && !s.preuveServeur(w, r, c.MotDePasse, "tâche serveurs forcée refusée", id) {
		return
	}
	t, err := s.lancerTacheDans(c.Texte, serveurs, forcee, c.Env, c.Terminal, panneauDe(r, c.Depuis, id), s.contexteSuite(id, c.Suite), c.APIs, c.Mode)
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	if forcee && sansRappel {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Mission: t.ID, Action: "tâche serveurs forcée sans rappel", Resultat: id})
	}
	s.ecrireJSON(w, map[string]any{"tache": t})
}

// apiReprendreMainTache: POST /api/serveurs/{id}/taches/{tache}/main: "Take over" (M231f): the owner's terminal
// is unlocked and no longer shows the task, which goes on in its own connection.
func (s *Serveur) apiReprendreMainTache(w http.ResponseWriter, r *http.Request) {
	id, tache := r.PathValue("id"), r.PathValue("tache")
	tachesEnCours.Lock()
	e := tachesEnCours.m[tache]
	tachesEnCours.Unlock()
	if e == nil && slices.ContainsFunc(tachesServeur(id), func(t TacheServeurs) bool { return t.ID == tache }) {
		s.ecrireJSON(w, map[string]any{"ok": true, "finie": true}) // ended meanwhile: the keyboard is the owner's already
		return
	}
	if e == nil || !slices.Contains(e.copie().Serveurs, id) {
		s.erreur(w, http.StatusNotFound, "No running task of that id on this server")
		return
	}
	ecrireDansTerminal(e, "\x1b[2m"+s.texteTerminal("You have the keyboard; the assistant goes on in its own connection.")+"\x1b[0m\n")
	e.mu.Lock()
	e.t.Direct, e.t.Terminal = false, ""
	e.mu.Unlock()
	_ = enregistrerTache(e.copie())
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Projet: prefixeTache + tache, Mission: tache, Action: "tâche serveurs : main reprise"})
	s.ecrireJSON(w, map[string]any{"ok": true})
}

// apiArreterTacheServeur: POST /api/serveurs/{id}/taches/{tache}/arreter.
func (s *Serveur) apiArreterTacheServeur(w http.ResponseWriter, r *http.Request) {
	id, tache := r.PathValue("id"), r.PathValue("tache")
	if !slices.ContainsFunc(tachesServeur(id), func(t TacheServeurs) bool { return t.ID == tache }) {
		s.erreur(w, http.StatusNotFound, "No running task of that id on this server")
		return
	}
	if !s.arreterTache(tache) { // ended meanwhile: nothing to stop, the page's view was late
		s.ecrireJSON(w, map[string]any{"ok": true, "finie": true})
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Projet: prefixeTache + tache, Mission: tache, Action: "tâche serveurs arrêtée"})
	s.ecrireJSON(w, map[string]any{"ok": true})
}

// ----------------------------------------------------------------------------- the kit's side

func init() {
	enregistrerCommande("session-serveurs", "session-serveurs FOLDER [INSTANCE_FILE]: one Claude session of a Servers task (launched by the application)",
		func(args []string) int { return lancerSessionHub(args, delaiSessionTache) })
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

// lancerSessionHub: a Servers task's or an orchestrator's session, run where the pilot runs (Windows, WSL, macOS,
// Linux), in FOLDER/travail, an empty folder, killed after delai.
func lancerSessionHub(args []string, delai time.Duration) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "Launched by the application (a server's tasks, a mission).")
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
	minuteur := time.AfterFunc(delai, func() { tuerGroupeSante(c) })
	defer minuteur.Stop()
	if c.Wait() != nil {
		return 1
	}
	return 0
}

// formatCree: RFC 3339 with a fixed-width fraction, so that two tasks created within a second still sort
// as strings (the newest first).
const formatCree = "2006-01-02T15:04:05.000Z07:00"
