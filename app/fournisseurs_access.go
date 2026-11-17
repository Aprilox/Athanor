package main

// Cloudflare Access on an environment's host name (M118, ARCHITECTURE.md §Protection): access_proteger makes
// (or updates) a self-hosted Access application on the name, with one "allow" policy for the e-mail addresses
// the owner chose; access_retirer removes that application. Both are Cloudflare changes of the agent's plan.

import (
	"errors"
	"net/url"
	"slices"
	"strings"
)

// maxEmailsAccess: the e-mail addresses one Access policy allows.
const maxEmailsAccess = 50

// classerAccess (pure): access_proteger's e-mail addresses checked and made canonical (lower case, sorted,
// once each), so that the step matches the accepted plan whatever their order.
func classerAccess(a argsCloudflare) (Classe, string, argsCloudflare, error) {
	emails := []string{}
	for _, e := range a.Emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if !reEmailAuteur.MatchString(e) {
			return 0, "", a, errors.New("emails: e-mail addresses only (name@domain)")
		}
		emails = append(emails, e)
	}
	slices.Sort(emails)
	emails = slices.Compact(emails)
	if len(emails) == 0 || len(emails) > maxEmailsAccess {
		return 0, "", a, errors.New("emails: 1 to 50 e-mail addresses")
	}
	return ClasseChangement, "only " + strings.Join(emails, ", ") + " can open " + a.Nom + " (Cloudflare Access)", argsCloudflare{Nom: a.Nom, Emails: emails}, nil
}

// appAccess: an Access application as Cloudflare lists it.
type appAccess struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}

// access: access_proteger or access_retirer (a.Emails empty) for a.Nom with provider f's token.
func (s *Serveur) access(f Fournisseur, jeton string, a argsCloudflare) (any, error) {
	if f.Compte == "" {
		return nil, errors.New("the Cloudflare account is not known: the owner clicks Test on the Cloudflare API in the Vault")
	}
	apps := "/accounts/" + url.PathEscape(f.Compte) + "/access/apps"
	var l []appAccess
	// shortcut: the first page of the account's applications only, page through if accounts hold more than Cloudflare's default page
	if err := s.cloudflare(jeton, "GET", apps, nil, &l); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(l, func(x appAccess) bool { return strings.EqualFold(strings.TrimSuffix(x.Domain, "/"), a.Nom) })
	if len(a.Emails) == 0 { // access_retirer
		if i < 0 {
			return nil, errors.New("no Cloudflare Access application protects this name: nothing removed")
		}
		if err := s.cloudflare(jeton, "DELETE", apps+"/"+url.PathEscape(l[i].ID), nil, nil); err != nil {
			return nil, err
		}
		return map[string]any{"resultat": "removed", "nom": a.Nom, "suite": "the site is open to everyone again"}, nil
	}
	var app appAccess
	if i >= 0 {
		app = l[i]
	} else if err := s.cloudflare(jeton, "POST", apps, map[string]any{"name": "Athanor " + a.Nom, "domain": a.Nom, "type": "self_hosted",
		"session_duration": "24h"}, &app); err != nil {
		return nil, err
	} else if app.ID == "" {
		return nil, errors.New("Cloudflare made no application")
	}
	include := []map[string]any{}
	for _, e := range a.Emails {
		include = append(include, map[string]any{"email": map[string]string{"email": e}})
	}
	politique := map[string]any{"name": "Athanor: allowed e-mails", "decision": "allow", "include": include, "precedence": 1}
	pols := apps + "/" + url.PathEscape(app.ID) + "/policies"
	var existantes []appAccess
	if err := s.cloudflare(jeton, "GET", pols, nil, &existantes); err != nil {
		return nil, err
	}
	if len(existantes) > 1 { // another policy could let others in: never left beside the owner's list
		return nil, errors.New("the Access application of this name has several policies: remove it (access_retirer) and protect it again")
	}
	var err error
	if len(existantes) == 1 { // an application already there: its policy becomes the owner's list
		err = s.cloudflare(jeton, "PUT", pols+"/"+url.PathEscape(existantes[0].ID), politique, nil)
	} else {
		err = s.cloudflare(jeton, "POST", pols, politique, nil)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"resultat": "protected", "nom": a.Nom, "emails": a.Emails}, nil
}
