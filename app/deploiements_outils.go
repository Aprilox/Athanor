package main

// The chef ops' deployment tools (M135, ARCHITECTURE.md §Déploiements, Qui déclenche): deployer and
// revenir_version act on the environment its task was launched for, only as a step of a plan the owner
// accepted (prod: never by a rule), and run the Go engine (deployer, revenir), never a shell of the agent.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// outilsDeploiement: tools of an environment, not of a server (no serveur argument).
var outilsDeploiement = []string{"deployer", "revenir_version"}

// argsDeploiementOutil: the arguments of deployer and revenir_version.
type argsDeploiementOutil struct {
	Env        string `json:"env"`
	Methode    string `json:"methode,omitempty"`
	Source     string `json:"source,omitempty"`
	Dossier    string `json:"dossier,omitempty"`
	Construire string `json:"construire,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

func init() {
	parmi := func(d string, l ...string) map[string]any {
		return map[string]any{"type": "string", "description": d, "enum": append([]string{}, l...)}
	}
	env := schemaTexte("Leave it empty: your task's site")
	classe := parmi("Your own class of this action; the code's class is kept when higher", "lecture", "changement", "destructrice")
	outilsHub["deployer"] = OutilHub{Description: "Deploy a commit of the project to your task's site with Athanor's engine (release, atomic switch, external check). " +
		"The method is then recorded so that later deployments need no agent. A step of an accepted plan only.",
		Schema: schemaObjet(map[string]any{"env": env, "methode": parmi("statique: Caddy serves a folder; compose: docker compose up of the release; k3s: kubectl apply of a manifests folder on a k3s server, only when the owner asked for Kubernetes", "statique", "compose", "k3s"),
			"source":     parmi("artefact: the commit archived on this PC and sent; git: each server fetches it with the deployment key", "artefact", "git"),
			"dossier":    map[string]any{"type": "string", "maxLength": maxDossierPublie, "description": "The published folder (statique), the compose file's folder or the manifests' folder (k3s), relative to the repository; empty: its root"},
			"construire": schemaTexte("A build command run on this PC in a checkout of the commit before the archive (artefact source only)"),
			"commit":     schemaTexte("The full commit hash"), "classe": classe}, "methode", "source", "commit"),
		Faire: faireOutilDeploiement("deployer")}
	outilsHub["revenir_version"] = OutilHub{Description: "Switch your task's site back to the version served before the last deployment, then check it. A step of an accepted plan only.",
		Schema: schemaObjet(map[string]any{"env": env, "classe": classe}), Faire: faireOutilDeploiement("revenir_version")}
}

// classerDeploiement: a deployment tool's arguments checked and canonical, and its class: a change; a build
// command is classed like an agent's command (C41), the higher class kept.
func classerDeploiement(outil string, brut json.RawMessage) (Classe, string, argsDeploiementOutil, error) {
	var a argsDeploiementOutil
	if err := lireArguments(brut, &a); err != nil {
		return 0, "", a, err
	}
	if a.Env != "" && !nomEnvValide(a.Env) { // the project's site is checked when the deployment starts
		return 0, "", a, errors.New("env: leave it empty (your task's site)")
	}
	if outil == "revenir_version" {
		return ClasseChangement, "switches the site back to its previous version", a, nil
	}
	a.Commit = strings.ToLower(strings.TrimSpace(a.Commit))
	if !reCommitComplet.MatchString(a.Commit) {
		return 0, "", a, errors.New("commit must be the full commit hash (40 hexadecimal characters, git rev-parse HEAD)")
	}
	if err := validerConfigDeploiement(ConfigDeploiement{a.Methode, a.Source, a.Dossier, a.Construire}); err != nil {
		return 0, "", a, err
	}
	c, raison := ClasseChangement, "deploys commit "+a.Commit[:12]+" to the site"
	if a.Construire != "" {
		if cc, r := classerCommande(a.Construire); cc > c {
			c, raison = cc, raison+"; its build command: "+r
		}
	}
	return c, raison, a, nil
}

// envTache: env is the site x's task was launched for, or empty (M305b: a task has one site, nothing to name):
// its project and its name, else the unknown site's answer (the refusal audited).
func (s *Serveur) envTache(x SessionHub, outil, env string) (string, string, error) {
	projet, nom, _ := strings.Cut(x.Env, "/")
	if projet != "" && (env == "" || nom == env) {
		return projet, nom, nil
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
		Action: "serveurs." + outil, Resultat: "refusé : environnement hors de la tâche (" + couper(env, 10) + ")"})
	return "", "", errEnvInconnu
}

// faireOutilDeploiement: the MCP side of deployer and revenir_version: the task's environment, a step of an
// accepted plan, then the engine; its result is the history's entry.
func faireOutilDeploiement(nom string) func(*Serveur, SessionHub, json.RawMessage) (string, error) {
	return func(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
		var a map[string]json.RawMessage
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %v", err)
		}
		if err := argumentsDe(nom, a); err != nil {
			return "", err
		}
		if err := s.accesServeursAgent(x); err != nil {
			return "", err
		}
		e, err := classerEtape(nom, a)
		var c argsDeploiementOutil
		var projet string
		if err == nil {
			_ = json.Unmarshal(e.Arguments, &c)
			projet, c.Env, err = s.envTache(x, nom, c.Env)
		}
		if err == nil {
			err = s.etapeCloudflare(x, &e) // a step of an accepted plan, whatever the environment
		}
		if err != nil {
			if !errors.Is(err, errEnvInconnu) {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
					Action: "serveurs." + nom, Resultat: "refusé : " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)})
			}
			return "", err
		}
		ctx := s.contexteSession(x.Hache)
		var d Deploiement
		if nom == "deployer" {
			d, err = s.deployer(ctx, projet, c.Env, c.Commit, OptionsDeploiement{Config: &ConfigDeploiement{c.Methode, c.Source, c.Dossier, c.Construire}, Auteur: "agent", Accorde: true})
		} else {
			d, err = s.revenir(ctx, projet, c.Env, "agent", "")
		}
		s.noterEtape(e.Plan, e.Num, err, nil)
		res := "ok : " + d.ID
		if err != nil {
			res = "échec : " + err.Error()
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "hub", Projet: x.Projet, Mission: x.Mission, Env: c.Env, Agent: x.Agent,
			Action: "serveurs." + nom, Resultat: couper(s.masquerSecrets(res), auditMaxOutil)})
		if err != nil && d.ID == "" { // refused before it started: nothing in the history
			return "", errors.New(strings.ReplaceAll(s.masquerSecrets(err.Error()), "<", "&lt;"))
		}
		return enDonnees("deployment", fmt.Sprintf(` tool="%s"`, nom), s.masquerSecrets(enJSON(d))), nil
	}
}
