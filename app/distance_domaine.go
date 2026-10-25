package main

// Remote access on your own domain (distance.go, distance_tunnel.go): instead of a quick tunnel, whose
// address changes at each restart of the computer, a "named" Cloudflare tunnel on a host name of yours
// (athanor.example.com). It needs a free Cloudflare account and a domain whose DNS is at Cloudflare.
// Athanor runs cloudflared's own steps once:
//   1. tunnel login: Cloudflare's page opens in the browser, you sign in and choose the domain; the
//      account's certificate is written in Athanor's folder (system/cloudflared/compte), never elsewhere;
//   2. tunnel create: a tunnel of this computer, its credentials in Athanor's folder too;
//   3. tunnel route dns: the host name points to that tunnel (an existing record is never replaced);
// then the tunnel runs with a configuration file towards the local server of the remote access. Its
// address never changes, and Cloudflare Access can be put in front of it (the Remote page's guide), so that
// Cloudflare asks for your e-mail before anything reaches the computer.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// etatDomaine: the setup of a domain under way (or its last error).
type etatDomaine struct {
	etape   string // connexion (sign-in in the browser), creation, pret, erreur
	message string
	lien    string // the sign-in page, when the browser did not open by itself
	gen     int
}

var (
	reHoteDomaine = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	reUUID        = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reLienLogin   = regexp.MustCompile(`https://dash\.cloudflare\.com/argotunnel\?\S+`)
	reCNAME       = regexp.MustCompile(`Added CNAME (\S+)`)
)

func (s *Serveur) dossierCloudflared() string { return filepath.Dir(s.cheminCloudflared()) }

// compteCloudflared: the "home" cloudflared is given, so that its account certificate stays in Athanor's folder.
func (s *Serveur) compteCloudflared() string { return filepath.Join(s.dossierCloudflared(), "compte") }

func (s *Serveur) certCloudflared() string {
	return filepath.Join(s.compteCloudflared(), ".cloudflared", "cert.pem")
}

func (s *Serveur) fichiersTunnelNomme(id string) (cred, cfg string) {
	return filepath.Join(s.dossierCloudflared(), "tunnel-"+id+".json"), filepath.Join(s.dossierCloudflared(), "tunnel-"+id+".yml")
}

// tunnelNomme: the domain in use and its tunnel ("" when it is the quick tunnel).
func (s *Serveur) tunnelNomme() (hote, id string) {
	s.mu.Lock()
	hote, id = s.config.Distance.Domaine, s.config.Distance.TunnelID
	s.mu.Unlock()
	if hote == "" || id == "" {
		return "", ""
	}
	if cred, _ := s.fichiersTunnelNomme(id); !fichierExiste(cred) {
		return "", ""
	}
	return hote, id
}

// commandeCloudflared: cloudflared ARGS with Athanor's folder as its home (its files stay there).
func (s *Serveur) commandeCloudflared(ctx context.Context, args ...string) *exec.Cmd {
	c := commandeContexte(ctx, s.cheminCloudflared(), args...)
	c.Dir = s.dossierCloudflared()
	c.Env = append(os.Environ(), "HOME="+s.compteCloudflared(), "USERPROFILE="+s.compteCloudflared(), "TUNNEL_ORIGIN_CERT="+s.certCloudflared())
	return c
}

// executerCloudflared: one step, its output (both streams), at most limite.
func (s *Serveur) executerCloudflared(limite time.Duration, args ...string) (string, error) {
	ctx, fin := context.WithTimeout(context.Background(), limite)
	defer fin()
	var sortie bytes.Buffer
	c := s.commandeCloudflared(ctx, args...)
	c.Stdout, c.Stderr = &sortie, &sortie
	err := c.Run()
	return sortie.String(), err
}

func (s *Serveur) etatDomaine(gen int, etape, message, lien string) bool {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.domaine.gen {
		return false
	}
	d.domaine.etape, d.domaine.message, d.domaine.lien = etape, message, lien
	return true
}

// connecterDomaine: sign-in (once), tunnel, host name; then the remote access uses it.
func (s *Serveur) connecterDomaine(gen int, hote string) {
	echec := func(err error) { s.etatDomaine(gen, "erreur", err.Error(), "") }
	if !fichierExiste(s.cheminCloudflared()) {
		s.etatDomaine(gen, "creation", "", "")
		if err := s.installerCloudflared(); err != nil {
			echec(err)
			return
		}
	}
	_ = os.MkdirAll(filepath.Dir(s.certCloudflared()), 0o700)
	if !fichierExiste(s.certCloudflared()) {
		if !s.etatDomaine(gen, "connexion", "", "") {
			return
		}
		if err := s.connexionCloudflare(gen); err != nil {
			echec(err)
			return
		}
	}
	if !s.etatDomaine(gen, "creation", "", "") {
		return
	}
	cred := filepath.Join(s.dossierCloudflared(), "tunnel-nouveau.json")
	_ = os.Remove(cred)
	nom := "athanor-" + aleatoire(3)
	out, err := s.executerCloudflared(2*time.Minute, "tunnel", "--origincert", s.certCloudflared(), "create", "--credentials-file", cred, nom)
	id := ""
	if m := regexp.MustCompile(`with id (` + reUUID.String() + `)`).FindStringSubmatch(out); m != nil {
		id = m[1]
	}
	if err != nil || id == "" {
		echec(fmt.Errorf("Cloudflare did not create the tunnel: %s", derniereLigneSortie(out)))
		return
	}
	if !fichierExiste(cred) { // an older cloudflared writes it in its home
		cred = filepath.Join(s.compteCloudflared(), ".cloudflared", id+".json")
	}
	credFinal, _ := s.fichiersTunnelNomme(id)
	if _, enCours := s.tunnelNomme(); enCours == id || fichierExiste(credFinal) {
		echec(errors.New("Cloudflare gave the id of a tunnel already here"))
		return
	}
	if err := os.Rename(cred, credFinal); err != nil {
		echec(err)
		return
	}
	out, err = s.executerCloudflared(2*time.Minute, "tunnel", "--origincert", s.certCloudflared(), "route", "dns", id, hote)
	if err != nil {
		_, _ = s.executerCloudflared(time.Minute, "tunnel", "--origincert", s.certCloudflared(), "delete", "-f", id)
		_ = os.Remove(credFinal)
		echec(fmt.Errorf("Cloudflare did not point %s to the tunnel: %s", hote, derniereLigneSortie(out)))
		return
	}
	if m := reCNAME.FindStringSubmatch(out); m != nil && strings.TrimSuffix(strings.ToLower(m[1]), ".") != hote {
		hote = strings.TrimSuffix(strings.ToLower(m[1]), ".") // the name Cloudflare gave in the domain chosen at sign-in
	}
	s.mu.Lock()
	s.config.Distance.Domaine, s.config.Distance.TunnelID = hote, id
	err = ecrireConfig(s.config)
	actif := s.config.Distance.Actif
	s.mu.Unlock()
	if err != nil {
		echec(err)
		return
	}
	if !s.etatDomaine(gen, "pret", "", "") {
		return
	}
	if actif { // the quick tunnel gives way to the domain's
		s.arreterTunnel()
		go s.lancerTunnel()
	}
}

// connexionCloudflare: cloudflared's sign-in, which opens Cloudflare's page in the browser and waits for
// you to choose the domain (10 minutes at most); its link is shown too.
func (s *Serveur) connexionCloudflare(gen int) error {
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Minute)
	defer fin()
	var sortie bytes.Buffer
	c := s.commandeCloudflared(ctx, "tunnel", "login")
	t := &tamponSur{b: &sortie}
	c.Stdout, c.Stderr = t, t
	if err := c.Start(); err != nil {
		return err
	}
	fini := make(chan error, 1)
	go func() { fini <- c.Wait() }()
	for {
		select {
		case err := <-fini:
			if fichierExiste(s.certCloudflared()) {
				return nil
			}
			if err == nil {
				err = errors.New("no certificate")
			}
			return fmt.Errorf("The sign-in to Cloudflare did not finish: %s", derniereLigneSortie(t.lire()))
		case <-time.After(500 * time.Millisecond):
			if l := reLienLogin.FindString(t.lire()); l != "" {
				s.etatDomaine(gen, "connexion", "", l)
			}
			d := s.dist()
			d.mu.Lock()
			abandon := gen != d.domaine.gen
			d.mu.Unlock()
			if abandon {
				fin()
				return errors.New("cancelled")
			}
		}
	}
}

// oublierDomaine: back to the quick tunnel; the tunnel is deleted at Cloudflare and its files here (the
// host name's record stays in Cloudflare's DNS: remove it there if you want).
func (s *Serveur) oublierDomaine() {
	_, id := s.tunnelNomme()
	s.mu.Lock()
	if id == "" {
		id = s.config.Distance.TunnelID
	}
	s.config.Distance.Domaine, s.config.Distance.TunnelID = "", ""
	_ = ecrireConfig(s.config)
	actif := s.config.Distance.Actif
	s.mu.Unlock()
	d := s.dist()
	d.mu.Lock()
	d.domaine = etatDomaine{gen: d.domaine.gen + 1}
	d.mu.Unlock()
	s.arreterTunnel()
	if id != "" {
		if fichierExiste(s.certCloudflared()) {
			_, _ = s.executerCloudflared(time.Minute, "tunnel", "--origincert", s.certCloudflared(), "delete", "-f", id)
		}
		cred, cfg := s.fichiersTunnelNomme(id)
		_ = os.Remove(cred)
		_ = os.Remove(cfg)
	}
	_ = os.RemoveAll(s.compteCloudflared()) // the account's certificate: signed in again next time
	if actif {
		go s.lancerTunnel()
	}
}

// apiDistanceDomaine: POST {action: "connecter", hote} or {action: "oublier"} (Athanor's window only).
func (s *Serveur) apiDistanceDomaine(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Action string `json:"action"`
		Hote   string `json:"hote"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	switch c.Action {
	case "connecter":
		hote := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.Hote)), ".")
		hote = strings.TrimPrefix(strings.TrimPrefix(hote, "https://"), "http://")
		hote = strings.TrimSuffix(hote, "/")
		if !reHoteDomaine.MatchString(hote) || len(hote) > 253 || strings.HasSuffix(hote, ".trycloudflare.com") {
			s.erreur(w, http.StatusBadRequest, "A host name of your domain, e.g. athanor.example.com")
			return
		}
		d := s.dist()
		d.mu.Lock()
		if d.domaine.etape == "connexion" || d.domaine.etape == "creation" {
			d.mu.Unlock()
			s.erreur(w, http.StatusConflict, "Already under way")
			return
		}
		d.domaine.gen++
		d.domaine.etape, d.domaine.message, d.domaine.lien = "creation", "", ""
		gen := d.domaine.gen
		d.mu.Unlock()
		go s.connecterDomaine(gen, hote)
	case "annuler":
		d := s.dist()
		d.mu.Lock()
		d.domaine = etatDomaine{gen: d.domaine.gen + 1}
		d.mu.Unlock()
	case "oublier":
		s.oublierDomaine()
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	s.ecrireJSON(w, s.vueDistanceDe(appareilDe(r)))
}

// derniereLigneSortie: cloudflared's last useful line (an error first).
func derniereLigneSortie(out string) string {
	lignes := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lignes) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lignes[i]); strings.Contains(l, "ERR") || strings.Contains(strings.ToLower(l), "error") || strings.Contains(strings.ToLower(l), "failed") {
			return coupe(l, 300)
		}
	}
	for i := len(lignes) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lignes[i]); l != "" {
			return coupe(l, 300)
		}
	}
	return "no answer"
}

func coupe(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// tamponSur: a program's output read while it writes.
type tamponSur struct {
	mu sync.Mutex
	b  *bytes.Buffer
}

func (t *tamponSur) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.Write(p)
}

func (t *tamponSur) lire() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.String()
}
