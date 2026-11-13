package main

// Stage 7: a project's environments (dev, staging, prod): its URL, the inventory servers it runs on, its
// protection, its deployment policy, its DNS provider and its deployment key. They live in hub/acces.json
// next to the grants, so renaming or deleting a project carries them along. Never a secret value: the
// provider and the key are ids.

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Environnement struct {
	URL            string   `json:"url"`
	Serveurs       []string `json:"serveurs"`                  // inventory ids
	Protection     string   `json:"protection"`                // aucune | access | auth_proxy
	Politique      string   `json:"politique"`                 // accord | auto (prod: accord, locked)
	Fournisseur    string   `json:"fournisseur,omitempty"`     // provider id (fournisseurs.json)
	CleDeploiement string   `json:"cle_deploiement,omitempty"` // vault entry id, set by Athanor, never by a request
	// the origin certificate (M117): its vault entry id and expiry (RFC 3339), set by Athanor, never by a request
	Certificat       string `json:"certificat,omitempty"`
	CertificatExpire string `json:"certificat_expire,omitempty"`
	// the servers where Caddy can read it (C60): only they get its tls line
	CertificatServeurs []string `json:"certificat_serveurs,omitempty"`
	// how it is deployed (M131), recorded by Athanor after a first deployment, never by a request
	Deploiement *ConfigDeploiement `json:"deploiement,omitempty"`
	// dev only (M136): the project's HEAD deployed to dev after each milestone delivered (relais_jalon.go)
	DeployerAuJalon bool `json:"deployer_au_jalon,omitempty"`
}

var (
	protectionsEnv  = []string{"aucune", "access", "auth_proxy"}
	politiquesEnv   = []string{"accord", "auto"}
	nomHoteValide   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	errEnvURL       = erreurEntree("Invalid URL: https:// or http://, a host name, and no user name or password in it")
	errEnvURLIP     = erreurEntree("Use the site's host name, not an IP address or localhost")
	errEnvURLIDN    = erreurEntree("Type an international domain name in its punycode form (xn--…)")
	errEnvURLPort   = erreurEntree("Invalid port in the URL: a number from 1 to 65535")
	errEnvProtect   = erreurEntree("Unknown protection (aucune, access or auth_proxy)")
	errEnvPolitique = erreurEntree("Unknown policy (accord or auto)")
	errEnvProdAuto  = erreurEntree("Production always asks for your approval: its policy cannot be automatic")
	errEnvRelais    = erreurEntree("Only dev can be deployed after each milestone: staging and production wait for a request")
	errEnvFourn     = erreurEntree("Unknown provider")
	errEnvAuthHTTP  = erreurEntree("The password protection needs an https:// address: over http:// the password would travel in clear")
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
	errEnvPasProd  = "{0} is not a production server: production only runs on servers with the prod role"
	errEnvProd     = "{0} is a production server: it can only be used by production"
	errEnvHotePris = "{0} is already the address of {1}: one host name per environment"
)

// envDefaut: an environment not configured yet.
func envDefaut() Environnement {
	return Environnement{Serveurs: []string{}, Protection: "aucune", Politique: "accord"}
}

// validerURLEnv: "" (not deployed yet) or http(s)://host[:port][/path], no credentials, no IP nor localhost (the
// Caddy site and the DNS record are made for a name); scheme and host come back in lower case (B73).
func validerURLEnv(brut string) (string, error) {
	brut = strings.TrimSpace(brut)
	if brut == "" {
		return "", nil
	}
	u, err := url.Parse(brut)
	if err != nil || len(brut) > 2048 || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" {
		return "", errEnvURL
	}
	hote := strings.ToLower(u.Hostname())
	switch {
	case strings.ContainsFunc(hote, func(r rune) bool { return r > 127 }):
		return "", errEnvURLIDN
	case net.ParseIP(hote) != nil || hote == "localhost" || strings.HasSuffix(hote, ".localhost"):
		return "", errEnvURLIP
	case !nomHoteValide.MatchString(hote) || len(hote) > 253:
		return "", errEnvURL
	}
	if port := u.Port(); port != "" || strings.HasSuffix(u.Host, ":") {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", errEnvURLPort
		}
		hote += ":" + port
	}
	u.Host = hote
	return u.String(), nil
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
	if e.Protection == "auth_proxy" && strings.HasPrefix(strings.ToLower(e.URL), "http:") {
		return e, errEnvAuthHTTP // basic_auth in clear (C62)
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
	if e.DeployerAuJalon && nom != "dev" {
		return e, errEnvRelais
	}
	if e.Fournisseur != "" && !idServeurValide.MatchString(e.Fournisseur) {
		return e, errEnvFourn // the id's form here; apiEnvironnementModifier checks it exists in fournisseurs.json
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
			if e.Deploiement != nil {
				c := *e.Deploiement
				e.Deploiement = &c
			}
		}
		if p := a.f.Projets[projet]; p != nil {
			e.CleDeploiement = p.CleDeploiement // one key per project
		}
		r[nom] = e
	}
	return r
}

// DefinirEnvironnement replaces environment nom of the project (already validated); its certificate stays,
// it is Athanor's (the deployment key is the project's: ProjetAcces.CleDeploiement).
func (a *Acces) DefinirEnvironnement(projet, nom string, e Environnement) error {
	return a.modifier(func(f *fichierAcces) error {
		p := projetDe(f, projet)
		if p.Environnements == nil {
			p.Environnements = map[string]*Environnement{}
		}
		// one environment per host name, across projects: a dev task must never hold prod's name (C57)
		if h := hoteEnv(&e); h != "" {
			for nomP, autre := range f.Projets {
				if autre == nil {
					continue
				}
				for nomE, x := range autre.Environnements {
					if (nomP != projet || nomE != nom) && hoteEnv(x) == h {
						return erreurEntreeArgs{errEnvHotePris, []string{h, nomP + "/" + nomE}}
					}
				}
			}
		}
		e.CleDeploiement, e.Certificat, e.CertificatExpire, e.CertificatServeurs, e.Deploiement = "", "", "", nil, nil
		if avant := p.Environnements[nom]; avant != nil {
			e.Certificat, e.CertificatExpire, e.CertificatServeurs = avant.Certificat, avant.CertificatExpire, avant.CertificatServeurs
			e.Deploiement = avant.Deploiement
		}
		p.Environnements[nom] = &e
		return nil
	})
}

// hoteEnv: the lower-case host name of e's URL ("" without URL).
func hoteEnv(e *Environnement) string {
	if e == nil {
		return ""
	}
	u, err := url.Parse(e.URL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// HotesProd: host name → "project/prod" for every prod environment with a URL, all projects.
func (a *Acces) HotesProd() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := map[string]string{}
	for nomP, p := range a.f.Projets {
		if p == nil {
			continue
		}
		if h := hoteEnv(p.Environnements["prod"]); h != "" {
			r[h] = nomP + "/prod"
		}
	}
	return r
}

// Hotes: host name → "project/env" for every environment with a URL, all projects.
func (a *Acces) Hotes() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := map[string]string{}
	for nomP, p := range a.f.Projets {
		if p == nil {
			continue
		}
		for nomE, e := range p.Environnements {
			if h := hoteEnv(e); h != "" {
				r[h] = nomP + "/" + nomE
			}
		}
	}
	return r
}

// NoterCertificat records environment nom's origin certificate (its vault entry and expiry).
func (a *Acces) NoterCertificat(projet, nom, id, expire string) error {
	return a.modifier(func(f *fichierAcces) error {
		p := projetDe(f, projet)
		if p.Environnements == nil {
			p.Environnements = map[string]*Environnement{}
		}
		if p.Environnements[nom] == nil {
			e := envDefaut()
			p.Environnements[nom] = &e
		}
		p.Environnements[nom].Certificat, p.Environnements[nom].CertificatExpire = id, expire
		return nil
	})
}

// NoterDeploiement records how environment nom is deployed (M131), after a deployment that went through.
func (a *Acces) NoterDeploiement(projet, nom string, c ConfigDeploiement) error {
	return a.modifier(func(f *fichierAcces) error {
		e := projetDe(f, projet).Environnements[nom]
		if e == nil {
			return errors.New("unknown environment")
		}
		e.Deploiement = &c
		return nil
	})
}

// NoterServeursCertificat adds ids to environment nom's servers equipped with its origin certificate.
func (a *Acces) NoterServeursCertificat(projet, nom string, ids []string) error {
	return a.modifier(func(f *fichierAcces) error {
		e := projetDe(f, projet).Environnements[nom]
		if e == nil {
			return errors.New("unknown environment")
		}
		for _, id := range ids {
			if !slices.Contains(e.CertificatServeurs, id) {
				e.CertificatServeurs = append(e.CertificatServeurs, id)
			}
		}
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
	s.ecrireJSON(w, map[string]any{"projet": projet, "envs": envsProjet, "environnements": s.acces.Environnements(projet), "deploiements": deploiementsAffiches(projet), "sante": s.vueSante(projet)})
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
	if err == nil && e.Fournisseur != "" {
		err = errEnvFourn
		if l, errF := lireFournisseurs(); errF != nil {
			err = errF
		} else if slices.ContainsFunc(l, func(f Fournisseur) bool { return f.ID == e.Fournisseur }) {
			err = nil
		}
	}
	if err == nil {
		err = s.acces.DefinirEnvironnement(projet, c.Env, e)
	}
	if err == nil && e.Politique == "accord" { // B71: "Ask for my accord" ends the "never ask again" rule
		_, err = s.approbations.RetirerRegles(func(r RegleAccord) bool {
			return r.Projet == projet && r.Env == c.Env && r.Action == actionDeploiement
		})
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
	return a.utilisations(func(e *Environnement) []string { return e.Serveurs })
}

// UtilisationsFournisseurs: provider id → the "project/env" that use it, sorted.
func (a *Acces) UtilisationsFournisseurs() map[string][]string {
	return a.utilisations(func(e *Environnement) []string { return []string{e.Fournisseur} })
}

func (a *Acces) utilisations(ids func(*Environnement) []string) map[string][]string {
	m := map[string][]string{}
	if a == nil {
		return m
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for nomP, p := range a.f.Projets {
		for nomE, e := range p.Environnements {
			for _, id := range ids(e) {
				if id != "" {
					m[id] = append(m[id], nomP+"/"+nomE)
				}
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
		"dns_cloudflare": e.Fournisseur != "", "cle_deploiement_prete": e.CleDeploiement != "", "certificat_expire": e.CertificatExpire}
}

// actionDeploiement: the approval a deployment waits for (demander_deploiement).
const actionDeploiement = "deploiement"

// politiqueDeploiement (I6): whether a deployment of the project to env waits for the owner's accord,
// and why. Prod always does, and so does a destructive one (C64): neither a policy nor a rule lifts it.
func (s *Serveur) politiqueDeploiement(projet, env string, destructif bool) (accordRequis bool, raison string) {
	switch {
	case env == "prod":
		return true, "production always asks the owner"
	case destructif:
		return true, "a destructive deployment always asks the owner"
	case s.acces.Environnements(projet)[env].Politique == "auto":
		return false, "policy auto"
	case s.approbations.Regle(RegleAccord{Projet: projet, Env: env, Action: actionDeploiement}):
		return false, "the owner's rule for this environment"
	}
	return true, "policy accord"
}
