package main

// The Dev pilot's tools on the hub's MCP route (outilsPilote): what it may ask the hub about its own
// project, and nothing else. A tool has no project argument: the session's token decides the project,
// and any unknown argument (a "project" pointing elsewhere) is refused. No answer ever carries a value
// of the vault, only the names of the variables the pilot receives.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// dureeSignal: a blocker reported by a pilot stays in the bell this long (a new one replaces it).
const dureeSignal = 24 * time.Hour

// SignalPilote: the last blocker a project's pilot reported (signaler).
type SignalPilote struct {
	ID    string
	Texte string
	T     time.Time
}

func init() {
	objet, texte := schemaObjet, schemaTexte
	outilsHub["etat"] = OutilHub{Description: "Your project's state in Athanor: plan progress, phase, what waits for the owner, the names of the variables you receive, your access requests.",
		Schema: objet(map[string]any{}), Faire: outilEtat}
	outilsHub["environnements"] = OutilHub{Description: "Your project's environments: the current one and, for each, its URL, protection, deployment policy, the names of its servers and of the variables you receive (never their values).",
		Schema: objet(map[string]any{}), Faire: outilEnvironnements}
	outilsHub["demander_acces"] = OutilHub{Description: "Ask the owner for a key or secret you need (e.g. STRIPE_SECRET_KEY). Once accepted, it appears in .athanor/secrets.env, no restart needed.",
		Schema: objet(map[string]any{"nom": texte("Variable name, e.g. STRIPE_SECRET_KEY"), "raison": texte("Why you need it, in one sentence")}, "nom", "raison"), Faire: outilDemanderAcces}
	outilsHub["demander_deploiement"] = OutilHub{Description: "Ask for a deployment of a commit to dev, staging or prod. Prod always waits for the owner. Once accepted it runs with the recorded method: follow it with deploiement_etat.",
		Schema: objet(map[string]any{"environnement": texte("dev, staging or prod"), "commit": texte("Full commit hash to deploy (git rev-parse HEAD)")}, "environnement", "commit"), Faire: outilDemanderDeploiement}
	outilsHub["deploiement_etat"] = OutilHub{Description: "The state of a deployment of your project started by demander_deploiement: en_cours, ok, echec or revenu, with its external check.",
		Schema: objet(map[string]any{"id": texte("The deployment's id, from demander_deploiement")}, "id"), Faire: outilDeploiementEtat}
	outilsHub["signaler"] = OutilHub{Description: "Report a blocker to the owner: it shows in Athanor's bell. One short sentence.",
		Schema: objet(map[string]any{"blocage": texte("What blocks you, in one sentence")}, "blocage"), Faire: outilSignaler}
}

// schemaObjet: a tool's arguments schema; required is never null (Claude Code drops a server whose schema is invalid).
func schemaObjet(props map[string]any, requis ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": append([]string{}, requis...), "additionalProperties": false}
}

func schemaTexte(d string) map[string]any { return map[string]any{"type": "string", "description": d} }

// lireArguments decodes a tool's arguments; an unknown one (another project…) is refused.
func lireArguments(args json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments (%v): a tool only works on your session's project and takes only its own arguments", err)
	}
	return nil
}

// texteRequis: a required text argument, trimmed and cut to n characters.
func texteRequis(nom, v string, n int) (string, error) {
	if v = strings.TrimSpace(v); v == "" {
		return "", fmt.Errorf("%s is required", nom)
	}
	return couper(v, n), nil
}

func enJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// recusParEnv: the project's current environment and, per environment, the variables the pilot receives
// (code entries) and the number of infra entries Athanor keeps for itself.
func (s *Serveur) recusParEnv(projet string) (string, map[string][]string, map[string]int, error) {
	if s.acces == nil || s.accesErr != nil || s.coffre == nil {
		return "", nil, nil, errors.New("Athanor's access store is unavailable right now")
	}
	pa := s.acces.Projet(projet)
	portee := map[string]string{}
	for _, e := range s.coffre.Liste() {
		portee[e.ID] = e.Portee
	}
	recus, infra := map[string][]string{}, map[string]int{}
	for _, a := range pa.Acces {
		switch portee[a.Entree] {
		case "code":
			recus[a.Env] = append(recus[a.Env], a.Variable)
		case "":
		default:
			infra[a.Env]++
		}
	}
	for _, l := range recus {
		slices.Sort(l)
	}
	return pa.Env, recus, infra, nil
}

func outilEtat(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	vues := s.vuesProjets("")
	i := slices.IndexFunc(vues, func(pv ProjetVue) bool { return pv.Nom == x.Projet })
	if i < 0 {
		return "", errors.New("your project is no longer in Athanor")
	}
	pv := vues[i]
	env, recus, _, err := s.recusParEnv(x.Projet)
	if err != nil {
		return "", err
	}
	demandes := []map[string]string{}
	if s.approbations != nil {
		l, _ := s.approbations.Liste()
		for _, a := range l {
			if a.Projet == x.Projet {
				demandes = append(demandes, map[string]string{"id": a.ID, "action": a.Action, "nom": a.Details["nom"], "etat": s.approbations.PourAgent(a.ID)})
			}
		}
		if len(demandes) > 10 {
			demandes = demandes[len(demandes)-10:]
		}
	}
	return enJSON(map[string]any{"projet": pv.Nom, "actif": pv.Actif, "phase": pv.Phase,
		"plan":   map[string]any{"faits": pv.Faits, "jalons": pv.Jalons, "jalon": firstNonEmpty(pv.JalonTitre, pv.Jalon)},
		"besoin": pv.Besoin, "pour_vous": pv.PourVous, "environnement": env, "variables_recues": append([]string{}, recus[env]...),
		"demandes": demandes}), nil
}

func outilEnvironnements(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	env, recus, infra, err := s.recusParEnv(x.Projet)
	if err != nil {
		return "", err
	}
	noms := map[string]string{}
	if inv, err := lireServeurs(); err == nil {
		for _, v := range inv {
			noms[v.ID] = v.Nom
		}
	}
	envs := s.acces.Environnements(x.Projet)
	l := []map[string]any{}
	for _, e := range s.acces.EnvsDe(x.Projet) {
		srv := []map[string]any{}
		for _, id := range envs[e].Serveurs {
			if noms[id] != "" {
				srv = append(srv, map[string]any{"nom": noms[id]})
			}
		}
		v := vueEnvironnement(envs[e], srv)
		v["nom"], v["en_cours"], v["variables_recues"], v["infra_gardee_par_athanor"] = e, e == env, append([]string{}, recus[e]...), infra[e]
		l = append(l, v)
	}
	return enJSON(map[string]any{"environnement": env, "environnements": l}), nil
}

// outilDemanderAcces: a pending approval for the project's current environment (the same one when it
// is already waiting, so that a pilot asking at every cycle does not pile them up).
func outilDemanderAcces(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ Nom, Raison string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	nom, err := texteRequis("nom", a.Nom, 100)
	if err != nil {
		return "", err
	}
	if !nomSecretValide(nom) { // C31: the name goes to the phone and approbations.json, never a value
		return "", errors.New("nom must be a variable name such as STRIPE_KEY (letters, digits, _; not a name the shell uses)")
	}
	raison, err := texteRequis("raison", a.Raison, 500)
	if err != nil {
		return "", err
	}
	env, recus, _, err := s.recusParEnv(x.Projet)
	if err != nil {
		return "", err
	}
	if slices.Contains(recus[env], nom) {
		return nom + " is already in .athanor/secrets.env (" + env + "): read it there, no request needed", nil
	}
	l, _ := s.approbations.Liste()
	for _, d := range l {
		if d.Projet == x.Projet && d.Action == "demander_acces" && d.Env == env && d.Details["nom"] == nom && d.Etat == "attente" {
			return "waiting for the owner, id " + d.ID + " (already asked)", nil
		}
	}
	attente := func(id string) string {
		return "waiting for the owner, id " + id + ": once accepted, " + nom + " is in .athanor/secrets.env (read it again, no restart needed); go on with work that does not need it"
	}
	ap := Approbation{Projet: x.Projet, Env: env, Mission: x.Mission, Agent: x.Agent,
		Action: "demander_acces", Details: map[string]string{"nom": nom}, Raison: s.masquerSecrets(raison)}
	// the owner's "never ask again" for this project and environment: only the vault entry it granted for that
	// variable (its own name may differ), while it is still a code secret of the vault
	if id := s.approbations.EntreeRegle(x.Projet, env, ap.Action, nom); id != "" && s.cleCode(id) {
		if d, err := s.approbations.Demander(ap); err == nil {
			if _, _, err := s.deciderApprobation("regle", d.ID, "accepter", id, "", ""); err == nil {
				return nom + " granted by the owner's rule for this project and environment: it is in .athanor/secrets.env (read it again, no restart needed)", nil
			}
			s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: env,
				Agent: x.Agent, Action: "approbation demandée: " + ap.Action, Resultat: "attente " + d.ID + " (the rule could not grant it)"})
			s.notifierApprobation(d)
			return attente(d.ID), nil
		}
	}
	d, err := s.demanderApprobation(ap)
	if err != nil {
		return "", errors.New("the request could not be recorded, try again later")
	}
	return attente(d.ID), nil
}

// cleCode: id is a code secret of the vault.
func (s *Serveur) cleCode(id string) bool {
	return slices.ContainsFunc(s.coffre.Liste(), func(e EntreeCoffre) bool {
		return e.ID == id && e.Portee == "code" && e.Famille == "secret"
	})
}

// outilDemanderDeploiement (M120): the environment's policy decides (politiqueDeploiement); prod and the
// "accord" policy wait for the owner (one accord, one deployment). An accepted one runs in the background with
// the environment's recorded method (M135); without one, nothing is asked: a Servers task deploys first.
func outilDemanderDeploiement(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ Environnement, Commit string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	env, commit := a.Environnement, strings.ToLower(strings.TrimSpace(a.Commit))
	if !s.acces.EnvConnu(x.Projet, env) {
		return "", errors.New("unknown environment: one of the project's environments (lire_environnements)")
	}
	if s.prudenceRenforcee(x.Projet, env) && slices.ContainsFunc(lireMissions(), func(m Mission) bool { // R6: the mission's relay alone asks for prod
		return missionSuivie(m.Etat) && slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == "dev" && v.Ref == x.Projet })
	}) {
		return "", errors.New("a mission covers this project: its prod is deployed by the mission once dev is checked, after the owner's accord; do not ask for it, go on with your work")
	}
	if len(commit) == 64 && strings.Trim(commit, "0123456789abcdef") == "" { // B81: the engine archives SHA-1 commits only
		return "", errors.New("this commit is a SHA-256 hash: Athanor's engine deploys only repositories with SHA-1 commits (40 hexadecimal characters); nothing was asked")
	}
	if !reCommitComplet.MatchString(commit) { // C61: a prefix could name another commit made later
		return "", errors.New("commit must be the full commit hash (40 hexadecimal characters, git rev-parse HEAD)")
	}
	e := s.acces.Environnements(x.Projet)[env]
	if len(e.Serveurs) == 0 {
		return "", errors.New(env + " has no server yet: the owner adds one in the project's Environments panel; go on with work that does not need it")
	}
	if e.Deploiement == nil {
		return "", errors.New(env + " has no deployment method yet: a first deployment must be made by a Servers task (the owner launches it from the environment's card); go on with work that does not need it")
	}
	hote := x.Projet
	if u, err := url.Parse(e.URL); err == nil && u.Hostname() != "" {
		hote = u.Hostname()
	}
	// C63: the accord is for these servers, this agent and this mission (Reprendre compares them)
	ap := Approbation{Projet: x.Projet, Env: env, Mission: x.Mission, Agent: x.Agent, Action: actionDeploiement,
		Details: map[string]string{"commit": commit, "hote": hote, "serveurs": listeServeursAccord(e.Serveurs)}, Raison: "commit " + commit + " → " + hote}
	p, ok := s.projetNomme(x.Projet)
	if !ok {
		return "", errors.New("unknown project")
	}
	if sha, err := gitSortie(p.Dossier, "rev-parse", "--verify", "--quiet", "--end-of-options", commit+"^{commit}"); err != nil || sha != commit {
		return "", fmt.Errorf("commit %s is not in the project's repository (commit your work first); nothing was asked", commit) // B81: never an accord that cannot be used
	}
	fichiers := fichiersBuildChanges(p, env, commit, e.Deploiement)
	if len(fichiers) > 0 { // C68, C69: the build or the containers would run changed files of the repository: the owner reads them
		ap.Details["fichiers_build"] = couper(strings.Join(fichiers, ", "), 300)
		ap.Raison += "; build files changed: " + ap.Details["fichiers_build"]
	}
	accepte := func(par, accord string) (string, error) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: env, Agent: x.Agent,
			Action: "déploiement accepté", Resultat: "commit " + commit + " → " + hote + " (" + par + ")"})
		d, suite, err := s.commencerDeploiement(x.Projet, env, commit, OptionsDeploiement{Auteur: "agent", Accorde: accord != ""})
		if err != nil {
			if accord != "" { // C75: nothing started (a deployment already running…): the owner's accord stays usable
				s.approbations.Rendre(accord)
				return "", fmt.Errorf("deployment accepted (%s) but not started: %s; the accord is kept: call demander_deploiement again later", par, s.masquerSecrets(err.Error()))
			}
			return "", fmt.Errorf("deployment accepted (%s) but not started: %s", par, s.masquerSecrets(err.Error()))
		}
		go func() { _, _ = suite(context.Background()) }() // its result goes to the history (deploiement_etat)
		return "deployment " + d.ID + " of " + commit + " to " + env + " accepted (" + par + ") and started: follow it with deploiement_etat; go on with your work", nil
	}
	attente := func(id string) (string, error) {
		return "waiting for the owner, id " + id + ": once accepted, call demander_deploiement again with the same environment and commit; go on with work that does not need it", nil
	}
	if accord, raison := s.politiqueDeploiement(x.Projet, env, len(fichiers) > 0); !accord { // C69: changed build or container files are destructive
		return accepte(raison, "")
	}
	y, etat, err := s.approbations.Reprendre(ap)
	switch {
	case err != nil:
	case etat == "utilisee":
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Mission: x.Mission, Projet: x.Projet, Env: env,
			Agent: x.Agent, Action: "approbation utilisée: " + actionDeploiement, Resultat: y.ID})
		return accepte("the owner's accord "+y.ID, y.ID)
	case etat == "attente":
		return attente(y.ID)
	default:
		if y, err = s.demanderApprobation(ap); err == nil {
			return attente(y.ID)
		}
	}
	if errors.Is(err, errTropDemandes) {
		return "", err
	}
	return "", errors.New("the request could not be recorded, try again later")
}

// outilDeploiementEtat (M135): a deployment of the session's project, found by its id in the histories.
func outilDeploiementEtat(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ ID string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	for _, env := range s.acces.EnvsDe(x.Projet) {
		for _, d := range historiqueDeploiements(x.Projet, env) {
			if a.ID == "" || d.ID != a.ID {
				continue
			}
			r := map[string]any{"id": d.ID, "environnement": d.Env, "commit": d.Commit, "etat": d.Etat, "debut": d.Debut, "fin": d.Fin, "erreur": d.Erreur, "verification": d.Verification}
			// the error may quote a server or the build's output: data, never instructions
			return enDonnees("deployment", "", s.masquerSecrets(enJSON(r))), nil
		}
	}
	return "", errors.New("no deployment of your project has this id")
}

// listeServeursAccord: the sorted server ids an accord is bound to; a list longer than an approval detail
// (200 characters) becomes its hash, so two lists cut alike never share an accord.
func listeServeursAccord(ids []string) string {
	l := slices.Sorted(slices.Values(ids))
	if v := strings.Join(l, ","); len(v) <= 200 {
		return v
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join(l, ","))))
}

// outilSignaler: the blocker goes to the bell (the Dev element of the project) and to the audit log.
func outilSignaler(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ Blocage string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	texte, err := texteRequis("blocage", a.Blocage, 300)
	if err != nil {
		return "", err
	}
	texte = s.masquerSecrets(texte)
	s.mu.Lock()
	if s.signaux == nil {
		s.signaux = map[string]SignalPilote{}
	}
	// shortcut: kept in memory (lost when Athanor restarts, the audit log keeps it); persist if owners miss them
	s.signaux[x.Projet] = SignalPilote{ID: aleatoire(6), Texte: texte, T: time.Now()}
	s.mu.Unlock()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
		Action: "blocage signalé", Resultat: texte})
	return "reported: the owner sees it in Athanor's bell; go on with what does not depend on it", nil
}

// signalProjet: the project's blocker still shown, if any.
func (s *Serveur) signalProjet(projet string) (SignalPilote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.signaux[projet]
	return g, ok && time.Since(g.T) < dureeSignal
}
