package main

// Sending settings of the deployed apps (M204): « Configure sending » of an account for a project and an
// environment = grants of the existing mechanism (acces.json): SMTP_HOST, SMTP_PORT, SMTP_SECURE, SMTP_USER
// and SMTP_FROM (vault variables the module creates, source email:<id>) and SMTP_PASSWORD (the account's smtp
// entry). The existing path delivers them: the pilot's secrets.env in dev, the Compose .env on deployment.

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var variablesEmail = []string{"SMTP_HOST", "SMTP_PORT", "SMTP_SECURE", "SMTP_USER", "SMTP_FROM"}

const (
	errEmailConflit    = erreurEntree("This project already has another SMTP_ setting in this environment (another account or a vault entry): remove it first")
	errEmailPasConfigu = erreurEntree("This account does not send for this project in this environment")
)

// ProjetEmail: a project and environment an account sends for.
type ProjetEmail struct {
	Projet string `json:"projet"`
	Env    string `json:"env"`
}

// valeursVariablesEmail: SMTP_SECURE is true for implicit TLS (port 465), as most mail libraries read it.
func valeursVariablesEmail(c CompteEmail) map[string]string {
	sec := "false"
	if c.Securite == "tls" {
		sec = "true"
	}
	return map[string]string{"SMTP_HOST": c.Hote, "SMTP_PORT": strconv.Itoa(c.Port), "SMTP_SECURE": sec,
		"SMTP_USER": c.Utilisateur, "SMTP_FROM": c.Expediteur}
}

// grantsEmail: variable name → vault entry, for an account whose variables exist.
func grantsEmail(c CompteEmail) map[string]string {
	g := map[string]string{"SMTP_PASSWORD": c.Entree}
	for n, id := range c.Variables {
		g[n] = id
	}
	return g
}

func entreesEmail(c CompteEmail) []string {
	ids := []string{c.Entree}
	for _, id := range c.Variables {
		ids = append(ids, id)
	}
	return ids
}

// variablesCompteEmail creates the account's missing variables in the vault and gives them the account's
// current settings; true when c.Variables changed (comptes.json must be written).
func (s *Serveur) variablesCompteEmail(c *CompteEmail) (bool, error) {
	vals := valeursVariablesEmail(*c)
	existantes := map[string]EntreeCoffre{}
	for _, e := range s.coffre.Liste() {
		existantes[e.ID] = e
	}
	if c.Variables == nil {
		c.Variables = map[string]string{}
	}
	change := false
	for _, n := range variablesEmail {
		nom, note := n+" — "+c.Nom, n+" of the SMTP account "+c.Nom+" (kept by the E-mail page)"
		if e, ok := existantes[c.Variables[n]]; ok {
			if e.Valeur != vals[n] || e.Nom != nom {
				v := vals[n]
				if _, err := s.coffre.ModifierMeta(e.ID, nom, "autre", note, "code", &v); err != nil {
					return change, err
				}
			}
			continue
		}
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "variable", Type: "autre", Portee: "code",
			Source: "email:" + c.ID, Note: note}, vals[n])
		if err != nil {
			return change, err
		}
		c.Variables[n] = e.ID
		change = true
	}
	return change, nil
}

// accorderEmail gives the account's entries to the project for env, all or nothing; a grant already there
// stays, an SMTP_ name taken by another entry refuses everything.
func (a *Acces) accorderEmail(projet string, envs []string, g map[string]string) error {
	noms := append([]string{"SMTP_PASSWORD"}, variablesEmail...)
	return a.modifier(func(f *fichierAcces) error {
		p := projetDe(f, projet)
		for _, n := range noms {
			if slices.ContainsFunc(p.Acces, func(x AccesProjet) bool { return slices.Contains(envs, x.Env) && x.Variable == n && x.Entree != g[n] }) {
				return errEmailConflit // nothing is written: all the environments or none
			}
		}
		for _, env := range envs {
			for _, n := range noms {
				if !slices.ContainsFunc(p.Acces, func(x AccesProjet) bool { return x.Env == env && x.Entree == g[n] }) {
					p.Acces = append(p.Acces, AccesProjet{Entree: g[n], Env: env, Variable: n, Auto: "email", Cree: time.Now().UTC()})
				}
			}
		}
		return nil
	})
}

// retirerEmail takes the account's entries back from the project for envs.
func (a *Acces) retirerEmail(projet string, envs, ids []string) error {
	return a.modifier(func(f *fichierAcces) error {
		p := f.Projets[projet]
		if p == nil {
			return errEmailPasConfigu
		}
		avant := len(p.Acces)
		p.Acces = slices.DeleteFunc(p.Acces, func(x AccesProjet) bool { return slices.Contains(envs, x.Env) && slices.Contains(ids, x.Entree) })
		if len(p.Acces) == avant {
			return errEmailPasConfigu
		}
		return nil
	})
}

// projetsEmail: where the account's password is granted (its sending settings go with it).
func (a *Acces) projetsEmail(entree string) []ProjetEmail {
	l := []ProjetEmail{}
	if a == nil {
		return l
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for nom, p := range a.f.Projets {
		for _, x := range p.Acces {
			if x.Entree == entree {
				l = append(l, ProjetEmail{nom, x.Env})
			}
		}
	}
	sort.Slice(l, func(i, j int) bool {
		return l[i].Projet < l[j].Projet || l[i].Projet == l[j].Projet && slices.Index(envsProjet, l[i].Env) < slices.Index(envsProjet, l[j].Env)
	})
	return l
}

// apiEmailConfigurer: POST /api/email/configurer {compte, projet, env, retirer, mot_de_passe}. Giving the
// settings retypes Athanor's password like any grant (C25); a deployment to prod still needs its accord.
func (s *Serveur) apiEmailConfigurer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Compte, Projet, Env string
		Retirer             bool
		MotDePasse          string `json:"mot_de_passe"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, errEmailRequete.Error())
		return
	}
	if !s.accesPret(w, c.Projet) {
		return
	}
	if c.Env == "" {
		c.Env = tousEnvs // the whole project, like its keys (M304); an older configuration names its environment
	}
	if c.Env != tousEnvs && !s.acces.EnvConnu(c.Projet, c.Env) {
		s.erreur(w, http.StatusBadRequest, errAccesEnv.Error())
		return
	}
	if !c.Retirer {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "email", Projet: c.Projet, Action: "envoi e-mail refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	muComptesEmail.Lock()
	defer muComptesEmail.Unlock()
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	i := slices.IndexFunc(liste, func(x CompteEmail) bool { return x.ID == c.Compte })
	if i < 0 {
		s.erreur(w, http.StatusNotFound, errEmailInconnu.Error())
		return
	}
	compte := &liste[i]
	action := "envoi e-mail configuré"
	if c.Retirer {
		action = "envoi e-mail retiré"
		err = s.acces.retirerEmail(c.Projet, envsVises(c.Env), entreesEmail(*compte))
	} else {
		var change bool
		change, err = s.variablesCompteEmail(compte)
		if change && ecrireComptesEmail(liste) != nil {
			s.erreur(w, http.StatusInternalServerError, errEmailComptes)
			return
		}
		if err == nil {
			err = s.acces.accorderEmail(c.Projet, envsVises(c.Env), grantsEmail(*compte))
		}
	}
	switch {
	case errors.Is(err, errEmailConflit):
		s.erreur(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, errEmailPasConfigu):
		s.erreur(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		s.erreurCoffre(w, err)
		return
	}
	if p, ok := s.projetNomme(c.Projet); ok {
		if _, err := s.injecterAcces(p, nil); err != nil {
			fmt.Fprintln(os.Stderr, "access injection:", p.Nom+":", err)
		}
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "email", Projet: c.Projet, Action: action, Resultat: compte.Nom + " (" + strings.Join(envsVises(c.Env), ", ") + ")"})
	s.ecrireJSON(w, map[string]any{"compte": compte.ID, "projets": s.acces.projetsEmail(compte.Entree)})
}
