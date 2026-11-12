package main

// Stage 7: a project's environments (dev, staging, prod): its URL, the inventory servers it runs on, its
// protection, its deployment policy, its DNS provider and its deployment key. They live in hub/acces.json
// next to the grants, so renaming or deleting a project carries them along. Never a secret value: the
// provider and the key are ids.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

type Environnement struct {
	URL            string   `json:"url"`
	Serveurs       []string `json:"serveurs"`                  // inventory ids
	Protection     string   `json:"protection"`                // aucune | access | auth_proxy
	Politique      string   `json:"politique"`                 // accord | auto (prod: accord, locked)
	Fournisseur    string   `json:"fournisseur,omitempty"`     // provider id (fournisseurs.json)
	CleDeploiement string   `json:"cle_deploiement,omitempty"` // vault entry id, set by Athanor, never by a request
}

var (
	protectionsEnv  = []string{"aucune", "access", "auth_proxy"}
	politiquesEnv   = []string{"accord", "auto"}
	nomHoteValide   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	errEnvURL       = erreurEntree("Invalid URL: https:// or http://, a host name, and no user name or password in it")
	errEnvProtect   = erreurEntree("Unknown protection (aucune, access or auth_proxy)")
	errEnvPolitique = erreurEntree("Unknown policy (accord or auto)")
	errEnvProdAuto  = erreurEntree("Production always asks for your approval: its policy cannot be automatic")
	errEnvFourn     = erreurEntree("Unknown provider")
)

// erreurEntreeArgs: an input error whose message key takes arguments ({0}…), translated by the page.
type erreurEntreeArgs struct {
	cle  string
	args []string
}

func (e erreurEntreeArgs) Error() string {
	m := e.cle
	for i, a := range e.args {
		m = strings.ReplaceAll(m, fmt.Sprintf("{%d}", i), a)
	}
	return m
}

const (
	errEnvPasProd = "{0} is not a production server: production only runs on servers with the prod role"
	errEnvProd    = "{0} is a production server: it can only be used by production"
)

// envDefaut: an environment not configured yet.
func envDefaut() Environnement {
	return Environnement{Serveurs: []string{}, Protection: "aucune", Politique: "accord"}
}

// validerURLEnv: "" (not deployed yet) or http(s)://host[:port][/path], no credentials.
func validerURLEnv(brut string) (string, error) {
	brut = strings.TrimSpace(brut)
	if brut == "" {
		return "", nil
	}
	u, err := url.Parse(brut)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" ||
		!nomHoteValide.MatchString(strings.ToLower(u.Hostname())) || len(u.Hostname()) > 253 {
		return "", errEnvURL
	}
	return brut, nil
}

// validerEnvironnement checks e for environment nom against the inventory (pure: tested without HTTP).
// A prod server only goes into prod, and prod only takes prod servers; prod's policy is always accord.
func validerEnvironnement(nom string, e Environnement, inventaire []ServeurInventaire) (Environnement, error) {
	if !slices.Contains(envsProjet, nom) {
		return e, errAccesEnv
	}
	var err error
	if e.URL, err = validerURLEnv(e.URL); err != nil {
		return e, err
	}
	if e.Protection == "" {
		e.Protection = "aucune"
	}
	if !slices.Contains(protectionsEnv, e.Protection) {
		return e, errEnvProtect
	}
	if e.Politique == "" {
		e.Politique = "accord"
	}
	if !slices.Contains(politiquesEnv, e.Politique) {
		return e, errEnvPolitique
	}
	if nom == "prod" && e.Politique != "accord" {
		return e, errEnvProdAuto
	}
	if e.Fournisseur != "" && !idServeurValide.MatchString(e.Fournisseur) {
		return e, errEnvFourn // shortcut: format only until the providers exist (M114), then checked against fournisseurs.json
	}
	ids := []string{}
	for _, id := range e.Serveurs {
		i := slices.IndexFunc(inventaire, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return e, erreurEntree(errServeurInconnu.Error())
		}
		prod := slices.Contains(inventaire[i].Roles, "prod")
		switch {
		case nom == "prod" && !prod:
			return e, erreurEntreeArgs{errEnvPasProd, []string{inventaire[i].Nom}}
		case nom != "prod" && prod:
			return e, erreurEntreeArgs{errEnvProd, []string{inventaire[i].Nom}}
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	e.Serveurs = ids
	return e, nil
}

// Environnements: every environment of the project, the ones not configured with their defaults (a copy).
func (a *Acces) Environnements(projet string) map[string]Environnement {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := map[string]Environnement{}
	for _, nom := range envsProjet {
		e := envDefaut()
		if p := a.f.Projets[projet]; p != nil && p.Environnements[nom] != nil {
			e = *p.Environnements[nom]
			e.Serveurs = append([]string{}, e.Serveurs...)
		}
		r[nom] = e
	}
	return r
}

// DefinirEnvironnement replaces environment nom of the project (already validated); its deployment key
// stays, it is Athanor's.
func (a *Acces) DefinirEnvironnement(projet, nom string, e Environnement) error {
	return a.modifier(func(f *fichierAcces) error {
		p := projetDe(f, projet)
		if p.Environnements == nil {
			p.Environnements = map[string]*Environnement{}
		}
		if avant := p.Environnements[nom]; avant != nil {
			e.CleDeploiement = avant.CleDeploiement
		} else {
			e.CleDeploiement = ""
		}
		p.Environnements[nom] = &e
		return nil
	})
}

// RetirerServeur takes a server out of every environment; the answer lists the "project/env" it left.
func (a *Acces) RetirerServeur(id string) ([]string, error) {
	var touches []string
	a.mu.Lock()
	for nomP, p := range a.f.Projets {
		for nomE, e := range p.Environnements {
			if slices.Contains(e.Serveurs, id) {
				touches = append(touches, nomP+"/"+nomE)
			}
		}
	}
	a.mu.Unlock()
	if len(touches) == 0 {
		return nil, nil
	}
	slices.Sort(touches)
	return touches, a.modifier(func(f *fichierAcces) error {
		for _, p := range f.Projets {
			for _, e := range p.Environnements {
				e.Serveurs = slices.DeleteFunc(e.Serveurs, func(x string) bool { return x == id })
			}
		}
		return nil
	})
}

// envsIncompatibles: the "project/env" a server with these roles can no longer be in (a role change).
func (a *Acces) envsIncompatibles(id string, roles []string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var l []string
	for nomP, p := range a.f.Projets {
		for nomE, e := range p.Environnements {
			if slices.Contains(e.Serveurs, id) && (nomE == "prod") != slices.Contains(roles, "prod") {
				l = append(l, nomP+"/"+nomE)
			}
		}
	}
	slices.Sort(l)
	return l
}

// apiEnvironnements: GET /api/projets/{nom}/environnements.
func (s *Serveur) apiEnvironnements(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	if !s.accesPret(w, projet) {
		return
	}
	s.ecrireJSON(w, map[string]any{"projet": projet, "envs": envsProjet, "environnements": s.acces.Environnements(projet)})
}

// apiEnvironnementModifier: POST /api/projets/{nom}/environnements {env, url, serveurs, protection,
// politique, fournisseur}: the environment is replaced as given.
func (s *Serveur) apiEnvironnementModifier(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	var c struct {
		Env string `json:"env"`
		Environnement
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	inventaire, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	e, err := validerEnvironnement(c.Env, c.Environnement, inventaire)
	if err == nil {
		err = s.acces.DefinirEnvironnement(projet, c.Env, e)
	}
	var entree erreurEntree
	var avecArgs erreurEntreeArgs
	switch {
	case errors.As(err, &avecArgs):
		s.erreurArgs(w, http.StatusBadRequest, avecArgs.cle, avecArgs.args...)
		return
	case errors.As(err, &entree):
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Action: "environnement modifié",
		Resultat: fmt.Sprintf("%s: %s, %d serveur(s), %s, %s", c.Env, e.URL, len(e.Serveurs), e.Protection, e.Politique)})
	s.apiEnvironnements(w, r)
}

// UtilisationsServeurs: server id → the "project/env" that use it, sorted (the Servers page).
func (a *Acces) UtilisationsServeurs() map[string][]string {
	m := map[string][]string{}
	if a == nil {
		return m
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for nomP, p := range a.f.Projets {
		for nomE, e := range p.Environnements {
			for _, id := range e.Serveurs {
				m[id] = append(m[id], nomP+"/"+nomE)
			}
		}
	}
	for _, l := range m {
		slices.Sort(l)
	}
	return m
}

// vueEnvironnement: what an agent may read of an environment: never the provider's or the key's id.
func vueEnvironnement(e Environnement, serveurs []map[string]any) map[string]any {
	return map[string]any{"url": e.URL, "protection": e.Protection, "politique": e.Politique, "serveurs": serveurs,
		"dns_cloudflare": e.Fournisseur != "", "cle_deploiement_prete": e.CleDeploiement != ""}
}
