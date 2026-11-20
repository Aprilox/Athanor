package main

// A vault key the owner gave a Servers task, put on its server by Athanor (a token for git, an app's .env): the
// agent writes {{coffre:NAME}} in fichier_ecrire's contenu, Athanor puts the value there when the accepted step
// runs and leaves the file 0600; the agent never reads the value (its outputs and the audit log are masked).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var reCleCoffre = regexp.MustCompile(`\{\{coffre:([^{}\n]{1,100})\}\}`)

// clesNommees: the names of the {{coffre:NAME}} of contenu, each once.
func clesNommees(contenu string) []string {
	var l []string
	for _, m := range reCleCoffre.FindAllStringSubmatch(contenu, -1) {
		if n := strings.TrimSpace(m[1]); !slices.Contains(l, n) {
			l = append(l, n)
		}
	}
	return l
}

// clesProjetEnv: a deployment task's keys: those given to its project (its Access tab), with the variable each
// one is read as.
func (s *Serveur) clesProjetEnv(env string) map[string]string {
	m := map[string]string{}
	projet, _, _ := strings.Cut(env, "/")
	if projet == "" || s.acces == nil {
		return m
	}
	for _, a := range s.acces.Projet(projet).Acces {
		m[a.Entree] = a.Variable
	}
	return m
}

// cleDeTache: the vault entry name n designates among the keys x was given (its name, any case; a deployment task:
// its project's keys too), else an error the agent can act on.
func (s *Serveur) cleDeTache(x SessionHub, n string) (EntreeCoffre, error) {
	projet := s.clesProjetEnv(x.Env)
	if s.coffre != nil {
		for _, e := range s.coffre.Liste() {
			_, duProjet := projet[e.ID]
			if (slices.Contains(x.APIs, e.ID) || duProjet) && e.Famille == "secret" && strings.EqualFold(e.Nom, n) {
				return e, nil
			}
		}
	}
	return EntreeCoffre{}, fmt.Errorf("{{coffre:%s}}: not one of the keys the owner gave this task (your instructions list them); ask for it with OWNER NEEDED", couper(n, 60))
}

// classerCles: a fichier_ecrire step that puts keys on the server: each one must be the task's; an infra key (the
// keys of servers and accounts) leaving Athanor is a destructive step, so the owner's own accord (C-cles).
func (s *Serveur) classerCles(x SessionHub, outil string, et *Etape) error {
	if outil != "fichier_ecrire" {
		var a argsOutilServeur
		if json.Unmarshal(et.Arguments, &a) == nil && reCleCoffre.MatchString(a.Commande+a.Chemin) {
			return errors.New("{{coffre:NAME}} goes in fichier_ecrire's contenu only: a command line or a path would show the value")
		}
		return nil
	}
	var a argsOutilServeur
	if err := json.Unmarshal(et.Arguments, &a); err != nil || a.Contenu == nil {
		return nil
	}
	for _, n := range clesNommees(*a.Contenu) {
		e, err := s.cleDeTache(x, n)
		if err != nil {
			return err
		}
		if e.Portee == "infra" {
			if et.Classe < ClasseDestructrice {
				et.Classe = ClasseDestructrice
			}
			et.Raison += "; puts the infra key " + e.Nom + " on the server (it leaves Athanor)"
		} else {
			if et.Classe < ClasseChangement {
				et.Classe = ClasseChangement
			}
			et.Raison += "; puts the key " + e.Nom + " into the file"
		}
	}
	return nil
}

// avecCles: fichier_ecrire's arguments with each {{coffre:NAME}} replaced by its value, read now from the vault
// (an infra key needs Athanor unlocked), and whether there was one.
func (s *Serveur) avecCles(x SessionHub, outil string, args json.RawMessage) (json.RawMessage, bool, error) {
	if outil != "fichier_ecrire" {
		return args, false, nil
	}
	var a map[string]json.RawMessage
	var contenu string
	if json.Unmarshal(args, &a) != nil || json.Unmarshal(a["contenu"], &contenu) != nil || len(clesNommees(contenu)) == 0 {
		return args, false, nil
	}
	var err error
	contenu = reCleCoffre.ReplaceAllStringFunc(contenu, func(m string) string {
		e, e2 := s.cleDeTache(x, strings.TrimSpace(reCleCoffre.FindStringSubmatch(m)[1]))
		if e2 == nil {
			var v string
			if v, e2 = s.coffre.Valeur(e.ID); e2 == nil {
				return v
			}
			e2 = fmt.Errorf("the key %s cannot be read from the vault now (unlock Athanor): %w", e.Nom, e2)
		}
		if err == nil {
			err = e2
		}
		return m
	})
	if err != nil {
		return nil, true, err
	}
	a["contenu"], _ = json.Marshal(contenu)
	b, _ := json.Marshal(a)
	return b, true, nil
}

// fermerFichierCles: a file that received keys is left readable by its owner only (0600); the agent changes it in
// a step of its own when a service needs more.
func (s *Serveur) fermerFichierCles(ctx context.Context, de, id string, args json.RawMessage) {
	var a argsOutilServeur
	if json.Unmarshal(args, &a) != nil || a.Chemin == "" {
		return
	}
	b, _ := json.Marshal(map[string]any{"commande": "chmod 600 -- " + citerShell(a.Chemin), "sudo": a.Sudo})
	_, _ = s.appelerOutilServeur(ctx, de, id, "ssh_commande", b, Sauvegarde{})
}

// lignesClesTache: what the agent is told about the keys it was given: their names and kinds (and the variable the
// project reads each as), never a value.
func (s *Serveur) lignesClesTache(apis []string, variables map[string]string) string {
	if s.coffre == nil || len(apis)+len(variables) == 0 {
		return ""
	}
	var l []string
	for _, e := range s.coffre.Liste() {
		v, duProjet := variables[e.ID]
		if (slices.Contains(apis, e.ID) || duProjet) && e.Famille == "secret" {
			ligne := fmt.Sprintf("- %s (%s, %s)", e.Nom, e.Type, e.Portee)
			if v != "" {
				ligne += ", read by the app as " + v
			}
			l = append(l, ligne)
		}
	}
	if len(l) == 0 {
		return ""
	}
	return "\nThe vault keys the owner gave this task (names only):\n" + strings.Join(l, "\n") + "\n" +
		"To put one on a server (git credentials for a private repository, an app's .env, a service's config), write it with " +
		"fichier_ecrire as {{coffre:NAME}} inside contenu, in an accepted plan's step: Athanor puts the value there when the step " +
		"runs and leaves the file 0600 (change its mode in a step of yours when a service needs it). You never see a value: " +
		"never print, echo or copy a key in a command. For a private GitHub repository, for instance: a ~/.git-credentials " +
		"holding https://x-access-token:{{coffre:NAME}}@github.com, then git config credential.helper store.\n"
}
