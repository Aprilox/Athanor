package main

// M133 (ARCHITECTURE.md §Déploiements): the external check of an environment after each deployment, and the
// one-click way back: current ↔ previous on every server, Caddy reloaded, the environment checked again.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
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
	msg := strings.Join(d.Verification.Problemes, ", ")
	if d.Verification.HTTP.Erreur != "" {
		msg += ": " + couper(d.Verification.HTTP.Erreur, 200)
	}
	d.Etat, d.Erreur = "echec", "the external verification failed: "+msg
	return errors.New(d.Erreur)
}

// scriptLirePrecedente ($1 the environment's folder): the current release then the previous one, if it is still there.
const scriptLirePrecedente = `cd -- "$1" 2>/dev/null && c=$(readlink current) || { echo "nothing is deployed on this server"; exit 6; }
p=$(readlink previous) && [ -d "$p" ] || { echo "no previous version on this server"; exit 6; }
echo "$c"; echo "$p"`

// scriptRevenir ($1 folder): previous served again, the version left becomes previous (going back twice undoes it).
const scriptRevenir = `cd -- "$1" || exit 1
c=$(readlink current) && p=$(readlink previous) || exit 6
ln -sfn "$p" .current.new && mv -fT .current.new current && ln -sfn "$c" .previous.new && mv -fT .previous.new previous`

// revenir: the environment's previous version served again on all its servers, Caddy reloaded (compose: its
// containers started again), checked; noted in
// the history (Retour), the version left marked revenu. The API asks prod's accord before calling it.
func (s *Serveur) revenir(ctx context.Context, projet, env, auteur string) (Deploiement, error) {
	e, ok := s.acces.Environnements(projet)[env]
	switch {
	case !ok:
		return Deploiement{}, errors.New("unknown environment")
	case e.Deploiement == nil || len(e.Serveurs) == 0:
		return Deploiement{}, errors.New("nothing was deployed to this environment yet")
	case e.Deploiement.Methode == "k3s" && len(e.Serveurs) != 1:
		return Deploiement{}, errK3sUnServeur
	}
	v, _ := verrousDeploiement.LoadOrStore(projet+"/"+env, &sync.Mutex{})
	verrou := v.(*sync.Mutex)
	if !verrou.TryLock() {
		return Deploiement{}, errors.New("a deployment of this environment is already running")
	}
	defer verrou.Unlock()
	l, err := lireServeurs()
	if err != nil {
		return Deploiement{}, err
	}
	base := baseDeploiement(projet, env)
	var clients []*ssh.Client
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	precedente, actuelle := "", ""
	for _, id := range e.Serveurs {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return Deploiement{}, errServeurInconnu
		}
		c, err := s.connecterServeur(ctx, l[i])
		if err != nil {
			return Deploiement{}, fmt.Errorf("%s: %w", id, err)
		}
		clients = append(clients, c)
		r, err := executerSSH(ctx, c, commeRoot(scriptLirePrecedente, base), nil, delaiCommande)
		if err := echecSSH(r, err); err != nil {
			return Deploiement{}, fmt.Errorf("%s: %w; nothing was switched", id, err)
		}
		cp := strings.SplitN(strings.TrimSpace(string(r.Sortie)), "\n", 2)
		if len(cp) != 2 {
			return Deploiement{}, fmt.Errorf("%s: unexpected answer %q", id, couper(string(r.Sortie), 200))
		}
		p := cp[1]
		if precedente != "" && p != precedente {
			return Deploiement{}, errors.New("the servers do not have the same previous version; nothing was switched")
		}
		precedente, actuelle = p, strings.TrimPrefix(cp[0], "releases/")
	}
	version := strings.TrimPrefix(precedente, "releases/")
	h := historiqueDeploiements(projet, env)
	commit, quitte := version[strings.LastIndex(version, "-")+1:], -1 // quitte: the newest entry of the version left
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Version == version && len(commit) < 40 {
			commit = h[i].Commit
		}
		if h[i].Version == actuelle && quitte < 0 {
			quitte = i
		}
	}
	debut := time.Now().UTC()
	d := Deploiement{ID: aleatoire(8), Projet: projet, Env: env, Commit: commit, Methode: e.Deploiement.Methode, Version: version,
		Etat: "en_cours", Auteur: auteur, Retour: true, Serveurs: e.Serveurs, Debut: debut.Format(time.RFC3339)}
	if err := noterHistorique(d); err != nil {
		return d, fmt.Errorf("the deployment history cannot be written: %w", err)
	}
	err = nil
	for i, c := range clients {
		if err = echecSSH(executerSSH(ctx, c, commeRoot(scriptRevenir, base), nil, delaiCommande)); err != nil {
			err = fmt.Errorf("%s: switch back: %w", e.Serveurs[i], err)
			break
		}
	}
	for i, c := range clients {
		if err != nil {
			break
		}
		recharger := commeRoot(scriptRecharger, base, fmt.Sprint(versionsGardees))
		switch e.Deploiement.Methode {
		case "compose":
			recharger = commeRoot(scriptRelancerCompose, base, e.Deploiement.Dossier, nomCompose(projet, env))
		case "k3s":
			recharger = commeRoot(scriptRevenirK3s, base, e.Deploiement.Dossier)
		}
		if err = echecSSH(executerSSH(ctx, c, recharger, nil, delaiDeploiement)); err != nil {
			err = fmt.Errorf("%s: %w", e.Serveurs[i], err)
		}
	}
	d.Etat = "ok"
	if err != nil {
		d.Etat, d.Erreur = "echec", err.Error()
	} else {
		err = s.verifierApres(ctx, e, &d)
		if quitte >= 0 {
			h[quitte].Etat = "revenu"
			_ = noterHistorique(h[quitte])
		}
	}
	d.Fin = time.Now().UTC().Format(time.RFC3339)
	_ = noterHistorique(d)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: auteur, Vers: "hub", Projet: projet, Env: env, Action: "deploiement.retour",
		Resultat: d.Etat + " " + d.Version + " " + couper(d.Erreur, 160)})
	return d, err
}

// deploiementsAffiches: env → its last deployments, newest first (the environment cards).
func deploiementsAffiches(projet string) map[string][]Deploiement {
	m := map[string][]Deploiement{}
	for _, env := range envsProjet {
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
	if lireCorps(r, &c) != nil || !slices.Contains(envsProjet, c.Env) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	if c.Env == "prod" {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Env: c.Env, Action: "deploiement.retour refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Minute) // not cut halfway when the page goes away
	defer fin()
	d, err := s.revenir(ctx, projet, c.Env, "proprietaire")
	if err != nil && d.ID == "" {
		s.erreur(w, http.StatusConflict, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"deploiement": d, "deploiements": deploiementsAffiches(projet)[c.Env]})
}
