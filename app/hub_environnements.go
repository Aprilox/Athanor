package main

// Stage 7: a project's environments (M246: free internal names, no dev/staging/prod label): its URL, the inventory servers it runs on, its
// protection, its deployment policy, its DNS provider and its deployment key. They live in hub/acces.json
// next to the grants, so renaming or deleting a project carries them along. Never a secret value: the
// provider and the key are ids.

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
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
	// never on a Production environment (M136): the project's HEAD deployed after each milestone (relais_jalon.go)
	DeployerAuJalon bool `json:"deployer_au_jalon,omitempty"`
	// M246: stronger caution (an accord for every risky action); set by the mission when its request speaks of
	// production, and on reading for an older "prod" environment or one on a server with the old prod role
	Production bool `json:"production,omitempty"`
	// M281, the project's deployment: its plan in free text (secrets masked), when it is deployed (jalon |
	// livraison | "" = on request, an older project only; kept in step with DeployerAuJalon for an older
	// reader) and the vault entries (API) it uses
	Plan   string   `json:"plan,omitempty"`
	Moment string   `json:"moment,omitempty"`
	APIs   []string `json:"apis,omitempty"`
}

var (
	protectionsEnv  = []string{"aucune", "access", "auth_proxy"}
	politiquesEnv   = []string{"accord", "auto"}
	momentsEnv      = []string{"", "jalon", "livraison"}
	nomHoteValide   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	errEnvURL       = erreurEntree("Invalid URL: https:// or http://, a host name, and no user name or password in it")
	errEnvURLIP     = erreurEntree("Use the site's host name, not an IP address or localhost")
	errEnvURLIDN    = erreurEntree("Type an international domain name in its punycode form (xn--…)")
	errEnvURLPort   = erreurEntree("Invalid port in the URL: a number from 1 to 65535")
	errEnvProtect   = erreurEntree("Unknown protection (aucune, access or auth_proxy)")
	errEnvPolitique = erreurEntree("Unknown policy (accord or auto)")
	errEnvProdAuto  = erreurEntree("Production always asks for your approval: its policy cannot be automatic")
	errEnvRelais    = erreurEntree("Production is deployed on request only, never after each milestone")
	errEnvNom       = erreurEntree("Invalid environment name: lower-case letters, digits, dots and dashes")
	errEnvFourn     = erreurEntree("Unknown provider")
	errEnvAuthHTTP  = erreurEntree("The password protection needs an https:// address: over http:// the password would travel in clear")
	errEnvMoment    = erreurEntree("Choose when the project is deployed: after each milestone or at delivery")
	errEnvPlan      = erreurEntree("The deployment plan is too long: 4,000 characters at most")
	errEnvAPI       = erreurEntree("Unknown API: choose it among the vault's entries")
	errEnvUnSeul    = erreurEntree("This project already has its deployment: a project has only one")
	errEnvSite      = erreurEntree("Unknown site")
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

const errEnvHotePris = "{0} is already the address of {1}: one host name per environment"

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

// nomEnvValide (M246): an environment's internal name, also a file name (deployment history) and never "*".
func nomEnvValide(nom string) bool {
	return len(nom) <= 100 && nomHoteValide.MatchString(nom)
}

// validerEnvironnement checks e for environment nom against the inventory (pure: tested without HTTP).
// A Production environment's policy is always accord; any server may serve any environment (M246).
func validerEnvironnement(nom string, e Environnement, inventaire []ServeurInventaire) (Environnement, error) {
	if !nomEnvValide(nom) {
		return e, errEnvNom
	}
	e.Production = e.Production || ancienProd(nom) || serveurRoleProd(e.Serveurs, inventaire)
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
	if e.Production && e.Politique != "accord" {
		return e, errEnvProdAuto
	}
	if !slices.Contains(momentsEnv, e.Moment) {
		return e, errEnvMoment
	}
	if (e.DeployerAuJalon || e.Moment == "jalon") && e.Production {
		return e, errEnvRelais
	}
	if utf8.RuneCountInString(e.Plan) > 4000 {
		return e, errEnvPlan
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
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	e.Serveurs = ids
	return e, nil
}

// serveurRoleProd: whether one of ids has the old prod role without dev in the inventory (M246 migration; a
// VPS with both roles was shared by dev and prod, V§1).
func serveurRoleProd(ids []string, inventaire []ServeurInventaire) bool {
	return slices.ContainsFunc(inventaire, func(v ServeurInventaire) bool {
		return slices.Contains(ids, v.ID) && slices.Contains(v.Roles, "prod") && !slices.Contains(v.Roles, "dev")
	})
}

// prudenceRenforcee (M247): whether every risky action on env of projet waits for an accord no rule lifts: a
// Production environment, the old name "prod", or one served by a server with the old prod role or that the
// inventory does not know (in doubt, caution). The one predicate of the guards: never compare a name to "prod".
func (s *Serveur) prudenceRenforcee(projet, env string) bool {
	if ancienProd(env) {
		return true
	}
	if s.acces == nil {
		return false
	}
	e, ok := s.acces.Environnements(projet)[env]
	if !ok || e.Production {
		return e.Production
	}
	inventaire, err := lireServeurs()
	return len(e.Serveurs) > 0 && (err != nil || serveurRoleProd(e.Serveurs, inventaire) || slices.ContainsFunc(e.Serveurs, func(id string) bool {
		return !slices.ContainsFunc(inventaire, func(v ServeurInventaire) bool { return v.ID == id })
	}))
}

// serveurPrudent (M247): a server with the old prod role, unknown to the inventory, or serving a Production
// environment of any project: its plans always wait for the owner.
func (s *Serveur) serveurPrudent(id string) bool {
	if _, prod := serveurProd(id); prod || s.acces == nil {
		return prod
	}
	return s.acces.ServeurProduction(id)
}

// ServeurProduction: whether server id serves a Production environment of some project.
func (a *Acces) ServeurProduction(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.f.Projets {
		if p == nil {
			continue
		}
		for _, e := range p.Environnements {
			if e != nil && e.Production && slices.Contains(e.Serveurs, id) {
				return true
			}
		}
	}
	return false
}

// nomDeploiement (M279): the internal name of a project's one deployment among its environments: the only one;
// of several (written by an older version), the one deployed after each milestone, or else the first by name
// that is not Production, or else the first; "site" for none. Never the owner's to choose or to read.
func nomDeploiement(envs map[string]Environnement) string {
	noms := slices.Sorted(maps.Keys(envs))
	for _, choisi := range []func(Environnement) bool{
		func(e Environnement) bool { return e.DeployerAuJalon },
		func(e Environnement) bool { return !e.Production },
		func(Environnement) bool { return true },
	} {
		for _, n := range noms {
			if choisi(envs[n]) {
				return n
			}
		}
	}
	return "site"
}

// deploiementProjet: the site the owner chose for projet (M281, an older project with several) while it
// exists, else nomDeploiement of its environments.
func (s *Serveur) deploiementProjet(projet string) string {
	if s.acces == nil {
		return "site"
	}
	envs := s.acces.Environnements(projet)
	if c := s.acces.Projet(projet).DeploiementCourant; c != "" && envs[c].Serveurs != nil { // Environnements never leaves Serveurs nil
		return c
	}
	return nomDeploiement(envs)
}

// ChoisirDeploiement (M281): site nom, one of an older project's, becomes the project's deployment.
func (a *Acces) ChoisirDeploiement(projet, nom string) error {
	return a.modifier(func(f *fichierAcces) error {
		p := f.Projets[projet]
		if p == nil || p.Environnements[nom] == nil {
			return errEnvSite
		}
		p.DeploiementCourant = nom
		return nil
	})
}

// ancienProd (M279): the one reading of an old environment name: "prod", written before the Production field
// (M246), still reads as Production (files, cards and approvals of older versions; the caution marker of M247).
func ancienProd(nom string) bool { return nom == "prod" }

// marquerProduction (M246): an older "prod" environment, or one on a server with the old prod role, reads as
// Production; in memory only, the file is written again only when it is changed.
func marquerProduction(f *fichierAcces, inventaire []ServeurInventaire) {
	for _, p := range f.Projets {
		if p == nil {
			continue
		}
		for nom, e := range p.Environnements {
			if e != nil && (ancienProd(nom) || serveurRoleProd(e.Serveurs, inventaire)) {
				e.Production = true
			}
		}
	}
}

// Environnements: every environment the project has (none for a project without a site), a copy.
func (a *Acces) Environnements(projet string) map[string]Environnement {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := map[string]Environnement{}
	p := a.f.Projets[projet]
	if p == nil {
		return r
	}
	for nom, x := range p.Environnements {
		if x == nil {
			continue
		}
		e := *x
		e.Serveurs = append([]string{}, e.Serveurs...)
		e.APIs = slices.Clone(e.APIs)
		if e.Moment == "" && e.DeployerAuJalon { // written before M281
			e.Moment = "jalon"
		}
		if e.Deploiement != nil {
			c := *e.Deploiement
			e.Deploiement = &c
		}
		e.CleDeploiement = p.CleDeploiement // one key per project
		r[nom] = e
	}
	return r
}

// EnvsDe (M246): the names a project's environment may have: the base ones (its grants, the pilot's
// environment) then every environment it was given, sorted.
func (a *Acces) EnvsDe(projet string) []string {
	l := slices.Clone(envsProjet)
	if a == nil {
		return l
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var autres []string
	if p := a.f.Projets[projet]; p != nil {
		for nom := range p.Environnements {
			if !slices.Contains(l, nom) {
				autres = append(autres, nom)
			}
		}
	}
	slices.Sort(autres)
	return append(l, autres...)
}

// EnvConnu: whether env is one of the project's environment names (EnvsDe).
func (a *Acces) EnvConnu(projet, env string) bool {
	return slices.Contains(a.EnvsDe(projet), env)
}

// DefinirEnvironnement replaces environment nom of the project (already validated); its certificate stays,
// it is Athanor's (the deployment key is the project's: ProjetAcces.CleDeploiement). M281: a project has one
// deployment, so a second name is refused; an older project with several keeps them.
func (a *Acces) DefinirEnvironnement(projet, nom string, e Environnement) error {
	return a.modifier(func(f *fichierAcces) error {
		if p := f.Projets[projet]; p != nil && len(p.Environnements) == 1 && p.Environnements[nom] == nil {
			return errEnvUnSeul
		}
		return poserEnvironnement(f, projet, nom, e)
	})
}

// definirAncien: DefinirEnvironnement as before M281, a second name included: an older mission still writes
// the several environments of its card, until the missions go (M302), and the tests write older projects.
func (a *Acces) definirAncien(projet, nom string, e Environnement) error {
	return a.modifier(func(f *fichierAcces) error { return poserEnvironnement(f, projet, nom, e) })
}

// poserEnvironnement writes environment nom in f, whatever the others.
func poserEnvironnement(f *fichierAcces, projet, nom string, e Environnement) error {
	p := projetDe(f, projet)
	if p.Environnements == nil {
		p.Environnements = map[string]*Environnement{}
	}
	// M281: both fields say "after each milestone"; an older writer changes DeployerAuJalon only, so it wins
	if e.DeployerAuJalon {
		e.Moment = "jalon"
	} else if e.Moment == "jalon" {
		e.Moment = ""
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
		for nom, e := range p.Environnements {
			if h := hoteEnv(e); h != "" && (e.Production || ancienProd(nom)) { // the old name reads as Production, like prudenceRenforcee
				r[h] = nomP + "/" + nom
			}
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

// apiEnvironnements: GET /api/projets/{nom}/environnements.
func (s *Serveur) apiEnvironnements(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	if !s.accesPret(w, projet) {
		return
	}
	// M248: only the environments the project has, read only on the page; prudence: those whose going back asks the password
	envs := s.acces.Environnements(projet)
	l, prudence := append([]string{}, slices.Sorted(maps.Keys(envs))...), []string{} // [] for an empty project, never null
	for _, nom := range l {
		if s.prudenceRenforcee(projet, nom) {
			prudence = append(prudence, nom)
		}
	}
	s.ecrireJSON(w, map[string]any{"projet": projet, "envs": l, "environnements": envs, "prudence": prudence, "deploiements": s.deploiementsAffiches(projet), "sante": s.vueSante(projet)})
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
	// M281: the plan, the moment and the API are the deployment route's; this older one keeps them as they are
	avant := s.acces.Environnements(projet)[c.Env]
	c.Plan, c.APIs, c.Moment = avant.Plan, avant.APIs, ""
	if avant.Moment == "livraison" { // "jalon" is this route's DeployerAuJalon
		c.Moment = avant.Moment
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
	if err != nil {
		s.erreurEnv(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Action: "environnement modifié",
		Resultat: fmt.Sprintf("%s: %s, %d serveur(s), %s, %s", c.Env, e.URL, len(e.Serveurs), e.Protection, e.Politique)})
	s.apiEnvironnements(w, r)
}

// erreurEnv answers err: an input error is a 400 with its translated message.
func (s *Serveur) erreurEnv(w http.ResponseWriter, err error) {
	var entree erreurEntree
	var avecArgs erreurEntreeArgs
	switch {
	case errors.As(err, &avecArgs):
		s.erreurArgs(w, http.StatusBadRequest, avecArgs.cle, avecArgs.args...)
	case errors.As(err, &entree):
		s.erreur(w, http.StatusBadRequest, err.Error())
	default:
		s.erreur(w, http.StatusInternalServerError, err.Error())
	}
}

// apiDeploiementProjet (M281): GET /api/projets/{nom}/deploiement: the project's one deployment (courant: its
// internal name, only to send back) and, for an older project, every site it has with its history.
func (s *Serveur) apiDeploiementProjet(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	if !s.accesPret(w, projet) {
		return
	}
	envs, courant := s.acces.Environnements(projet), s.deploiementProjet(projet)
	d, existe := envs[courant]
	if !existe {
		d = envDefaut()
		d.Moment = "livraison"
	}
	s.ecrireJSON(w, map[string]any{"projet": projet, "courant": courant, "deploiement": d, "sites": envs,
		"deploiements": s.deploiementsAffiches(projet), "sante": s.vueSante(projet)})
}

// apiDeploiementProjetModifier (M281): POST /api/projets/{nom}/deploiement {serveurs, url, plan, moment, apis}
// writes the project's one deployment, whatever its internal name; its protection, policy and Production stay
// as they are. With {courant}, nothing but this: that site of an older project becomes its deployment.
func (s *Serveur) apiDeploiementProjetModifier(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	var c struct {
		Serveurs []string `json:"serveurs"`
		URL      string   `json:"url"`
		Plan     string   `json:"plan"`
		Moment   string   `json:"moment"`
		APIs     []string `json:"apis"`
		Courant  string   `json:"courant"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	if c.Courant != "" {
		if err := s.acces.ChoisirDeploiement(projet, c.Courant); err != nil {
			s.erreurEnv(w, err)
			return
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Action: "déploiement choisi", Resultat: c.Courant})
		s.apiDeploiementProjet(w, r)
		return
	}
	inventaire, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	nom := s.deploiementProjet(projet)
	e, existe := s.acces.Environnements(projet)[nom]
	if !existe {
		e = envDefaut()
		if c.Moment == "" {
			c.Moment = "livraison"
		}
	} else if c.Moment == "" && e.Moment != "" { // on request: only where an older project already has it
		s.erreurEnv(w, errEnvMoment)
		return
	}
	apis := slices.Compact(slices.Sorted(slices.Values(c.APIs)))
	e.Serveurs, e.URL, e.Plan, e.Moment, e.APIs = c.Serveurs, c.URL, s.masquerSecrets(strings.TrimSpace(c.Plan)), c.Moment, apis
	e.DeployerAuJalon = e.Moment == "jalon"
	e, err = validerEnvironnement(nom, e, inventaire)
	if coffre := s.coffre.Liste(); err == nil && slices.ContainsFunc(apis, func(id string) bool {
		return !slices.ContainsFunc(coffre, func(x EntreeCoffre) bool { return x.ID == id })
	}) {
		err = errEnvAPI
	}
	if err == nil {
		err = s.acces.DefinirEnvironnement(projet, nom, e)
	}
	if err != nil {
		s.erreurEnv(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Action: "déploiement modifié",
		Resultat: fmt.Sprintf("%s, %d serveur(s), %d API, %s", e.URL, len(e.Serveurs), len(e.APIs), cmp.Or(e.Moment, "on request"))})
	s.apiDeploiementProjet(w, r)
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
	case s.prudenceRenforcee(projet, env):
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
