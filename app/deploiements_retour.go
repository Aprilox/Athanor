package main

// M133 (ARCHITECTURE.md §Déploiements): the external check of an environment after each deployment, and the
// one-click way back: current ↔ previous on every server, Caddy reloaded, the environment checked again.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// VerifDeploiement: what a visitor gets from the environment's URL, judged. Problemes: codes the page words
// (injoignable | tls | statut | ouvert); missing security headers are shown, not a failure.
type VerifDeploiement struct {
	OK        bool         `json:"ok"`
	Problemes []string     `json:"problemes,omitempty"`
	HTTP      ResultatHTTP `json:"http"`
	T         string       `json:"t"`
}

// resolutionsTest: "host:port" → "ip:port" for verifierHTTP, so that the tests reach the container's Caddy
// under the environment's name; empty outside the tests.
var resolutionsTest = map[string]string{}

// jugerVerification: answers (2xx/3xx), a valid certificate over https, and, for a protected environment, the
// door closed without identification (401 for auth_proxy, a redirect to Cloudflare Access for access).
func jugerVerification(e Environnement, r ResultatHTTP) VerifDeploiement {
	v := VerifDeploiement{HTTP: r, T: time.Now().UTC().Format(time.RFC3339)}
	if r.Erreur != "" {
		v.Problemes = append(v.Problemes, "injoignable")
	}
	if strings.HasPrefix(r.URL, "https:") && (r.TLS == nil || !r.TLS.Valide) && r.Erreur == "" {
		v.Problemes = append(v.Problemes, "tls")
	}
	if r.Erreur == "" {
		switch e.Protection {
		case "auth_proxy":
			if r.Statut != http.StatusUnauthorized {
				v.Problemes = append(v.Problemes, "ouvert")
			}
		case "access":
			if u, err := url.Parse(r.Redirection); r.Statut < 300 || r.Statut > 399 || err != nil ||
				!strings.HasSuffix(strings.ToLower(u.Hostname()), ".cloudflareaccess.com") {
				v.Problemes = append(v.Problemes, "ouvert")
			}
		default:
			if r.Statut < 200 || r.Statut > 399 {
				v.Problemes = append(v.Problemes, "statut")
			}
		}
	}
	v.OK = len(v.Problemes) == 0
	return v
}

// verifierEnvironnement: the environment's URL visited from this PC, connected only to its servers.
func (s *Serveur) verifierEnvironnement(ctx context.Context, e Environnement) *VerifDeploiement {
	u, err := url.Parse(e.URL)
	if err != nil {
		v := jugerVerification(e, ResultatHTTP{URL: e.URL, Erreur: err.Error()})
		return &v
	}
	l, _ := lireServeurs()
	var hotes []string
	for _, x := range l {
		if slices.Contains(e.Serveurs, x.ID) {
			hotes = append(hotes, x.Hote)
		}
	}
	ctx, fin := context.WithTimeout(ctx, 20*time.Second)
	defer fin()
	v := jugerVerification(e, verifierHTTP(ctx, u, hotes))
	return &v
}

// verifierApres: d checked once its version is served; a failed check makes it a failure (the version stays served).
func (s *Serveur) verifierApres(ctx context.Context, e Environnement, d *Deploiement) error {
	d.Verification = s.verifierEnvironnement(ctx, e)
	if d.Verification.OK {
		return nil
	}
	m := messageVerification(d.Verification)
	d.Etat, d.Message = "echec", m
	return m
}

// scriptLireCible ($1 the environment's folder, $2 the version aimed at, "" for the previous one): the release
// served now, then the one aimed at, checked to be on this server.
const scriptLireCible = `cd -- "$1" 2>/dev/null && c=$(readlink current) || { echo "nothing is deployed on this server"; exit 6; }
if [ -n "$2" ]; then t=releases/$2; else t=$(readlink previous) || { echo "no previous version on this server"; exit 6; }; fi
[ -d "$t" ] || { echo "the version to go back to ($t) is no longer on this server"; exit 6; }
echo "$c"; echo "$t"`

var reVersionDeploiement = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{7,64}$`)

// versionConnue: what the history knows of a version (C74: the version goes back with its own method, folder
// and upstream; before M145: the environment's); i: its newest entry, -1 if none.
type versionConnue struct {
	methode, dossier, amont, commit string
	i                               int
}

func connaitreVersion(h []Deploiement, cfg *ConfigDeploiement, version string) versionConnue {
	v := versionConnue{methode: cfg.Methode, dossier: cfg.Dossier, commit: version[strings.LastIndex(version, "-")+1:], i: -1}
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Version != version {
			continue
		}
		if v.i < 0 {
			v.i = i
		}
		if len(v.commit) < 40 {
			v.commit = h[i].Commit
			if h[i].Methode != "" {
				v.methode, v.amont = h[i].Methode, h[i].Amont
			}
			if h[i].Dossier != nil {
				v.dossier = *h[i].Dossier
			}
		}
	}
	return v
}

// scriptRevenirK3s ($1 folder, $2 manifests' folder), after the switch: the release now current applied again
// and waited for (C72: never rollout undo, whose revision is not the version the links name).
const scriptRevenirK3s = `b=$1 dd=$2
` + fonctionsK3s + `appliquer "$b/current"`

// revenir: the environment served again at version cible ("" = the previous one) on all its servers (M133, C71).
// The API asks prod's accord before calling it.
func (s *Serveur) revenir(ctx context.Context, projet, env, auteur, cible string) (Deploiement, error) {
	v, _ := verrousDeploiement.LoadOrStore(projet+"/"+env, &sync.Mutex{})
	verrou := v.(*sync.Mutex)
	if !verrou.TryLock() {
		return Deploiement{}, errors.New("a deployment of this environment is already running")
	}
	defer verrou.Unlock()
	return s.revenirVerrouille(ctx, projet, env, auteur, cible)
}

// revenirVerrouille (the environment's lock held): the version aimed at, read before anything changes, served
// on every server (Caddy reloaded, compose containers started again, k3s applied again), checked; on any
// failure, the links of every server put back and the version left served again. Noted in the history
// (Retour), the version left marked revenu once it went through. Going back to the version served is harmless.
func (s *Serveur) revenirVerrouille(ctx context.Context, projet, env, auteur, cible string) (Deploiement, error) {
	e, ok := s.acces.Environnements(projet)[env]
	switch {
	case !ok:
		return Deploiement{}, errors.New("unknown environment")
	case e.Deploiement == nil || len(e.Serveurs) == 0:
		return Deploiement{}, errors.New("nothing was deployed to this environment yet")
	case e.Deploiement.Methode == "k3s" && len(e.Serveurs) != 1:
		return Deploiement{}, errK3sUnServeur
	case cible != "" && !reVersionDeploiement.MatchString(cible):
		return Deploiement{}, errors.New("invalid version")
	}
	l, err := lireServeurs()
	if err != nil {
		return Deploiement{}, err
	}
	base := baseDeploiement(projet, env)
	var serveurs []serveurPret
	defer func() {
		for _, x := range serveurs {
			x.c.Close()
		}
	}()
	version, actuelle := cible, ""
	for _, id := range e.Serveurs {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return Deploiement{}, errServeurInconnu
		}
		c, err := s.connecterServeur(ctx, l[i])
		if err != nil {
			return Deploiement{}, errMoteur("{0}: {1}", id, err)
		}
		serveurs = append(serveurs, serveurPret{id, c})
		r, err := executerSSH(ctx, c, commeRoot(scriptLireCible, base, cible), nil, delaiCommande)
		if err := echecSSH(r, err); err != nil {
			return Deploiement{}, errMoteur("{0}: {1}; nothing was switched", id, err)
		}
		ct := strings.SplitN(strings.TrimSpace(string(r.Sortie)), "\n", 2)
		if len(ct) != 2 || !strings.HasPrefix(ct[1], "releases/") {
			return Deploiement{}, errMoteur("{0}: unexpected answer \"{1}\"", id, couper(string(r.Sortie), 200))
		}
		t := strings.TrimPrefix(ct[1], "releases/")
		if version != "" && t != version {
			return Deploiement{}, errors.New("the servers do not have the same previous version; nothing was switched")
		}
		version, actuelle = t, strings.TrimPrefix(ct[0], "releases/")
	}
	h := historiqueDeploiements(projet, env)
	vise, quitte := connaitreVersion(h, e.Deploiement, version), connaitreVersion(h, e.Deploiement, actuelle)
	debut := time.Now().UTC()
	d := Deploiement{ID: aleatoire(8), Projet: projet, Env: env, Commit: vise.commit, Methode: vise.methode, Version: version, Dossier: &vise.dossier,
		Amont: vise.amont, Etat: "en_cours", Auteur: auteur, Retour: true, Serveurs: e.Serveurs, Debut: debut.Format(time.RFC3339)}
	if err := noterHistorique(d); err != nil {
		return d, errMoteur("the deployment history cannot be written: {0}", err)
	}
	// servir: what serves the current release of one server, with the method of version v
	servir := func(ctx context.Context, x serveurPret, v versionConnue) error {
		if v.methode == "k3s" {
			return echecSSH(executerSSH(ctx, x.c, commeRoot(scriptRevenirK3s, base, v.dossier), nil, delaiDeploiement))
		}
		amont := v.amont
		if v.methode == "statique" {
			amont = base + "/current"
			if v.dossier != "" {
				amont += "/" + v.dossier
			}
		}
		if v.methode == "compose" {
			if err := echecSSH(executerSSH(ctx, x.c, commeRoot(scriptRelancerCompose, base, v.dossier, nomCompose(projet, env)), nil, delaiDeploiement)); err != nil {
				return err
			}
		}
		bloc := ""
		if amont != "" {
			var err error
			if bloc, err = s.blocCaddyEnv(projet, env, x.id, amont); err != nil {
				return err
			}
		}
		if err := echecSSH(executerSSH(ctx, x.c, commeRoot(scriptSite, base, hoteEnv(&e)), strings.NewReader(bloc), delaiCommande)); err != nil {
			return err
		}
		return echecSSH(executerSSH(ctx, x.c, commeRoot(scriptRecharger, base, fmt.Sprint(versionsGardees)), nil, delaiDeploiement))
	}
	touches, err := basculerTous(ctx, serveurs, base, version)
	for _, x := range touches {
		if err != nil {
			break
		}
		if err = servir(ctx, x.serveurPret, vise); err != nil {
			err = errMoteur("{0}: {1}", x.id, err)
		}
	}
	d.Etat = "ok"
	if err == nil {
		err = s.verifierApres(ctx, e, &d)
	} else {
		d.Etat, d.Message = "echec", messageDe(err)
	}
	if err != nil && len(touches) > 0 { // the version left served again everywhere
		remettreLiens(ctx, touches, base)
		for _, x := range touches {
			if e2 := servir(context.WithoutCancel(ctx), x.serveurPret, quitte); e2 != nil {
				d.Message = suite(d.Message, errMoteur("{0}: the version left could not be served again: {1}", x.id, e2))
			}
		}
		d.Message = suite(d.Message, errMoteur("the links were put back on {0}", actuelle))
	} else if err == nil {
		d.Bascule = true
		if quitte.i >= 0 && actuelle != version {
			h[quitte.i].Etat = "revenu"
			_ = noterHistorique(h[quitte.i])
		}
	}
	d.Fin = time.Now().UTC().Format(time.RFC3339)
	s.noterMessage(&d) // B77
	_ = noterHistorique(d)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: auteur, Vers: "hub", Projet: projet, Env: env, Action: "deploiement.retour",
		Resultat: d.Etat + " " + d.Version + " " + couper(d.Erreur, 160)})
	return d, err
}

// deploiementsAffiches: env → its last deployments, newest first (the environment cards).
func (s *Serveur) deploiementsAffiches(projet string) map[string][]Deploiement {
	m := map[string][]Deploiement{}
	for _, env := range s.acces.EnvsDe(projet) {
		h := historiqueDeploiements(projet, env)
		slices.Reverse(h)
		m[env] = h[:min(len(h), 5)]
	}
	return m
}

// apiRevenir: POST /api/projets/{nom}/revenir {env, mot_de_passe}: prod goes back only with the owner's accord
// (Athanor's password in this window, or a paired device unlocked with it).
func (s *Serveur) apiRevenir(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	var c struct {
		Env        string `json:"env"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil || !s.acces.EnvConnu(projet, c.Env) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	if s.prudenceRenforcee(projet, c.Env) {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Env: c.Env, Action: "deploiement.retour refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Minute) // not cut halfway when the page goes away
	defer fin()
	d, err := s.revenir(ctx, projet, c.Env, "proprietaire", "")
	if err != nil && d.ID == "" {
		s.erreur(w, http.StatusConflict, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"deploiement": d, "deploiements": s.deploiementsAffiches(projet)[c.Env]})
}
