package main

// The Caddy site block of an environment (M118, ARCHITECTURE.md §Protection, §ACME): blocCaddy is pure and
// is what stage 8's deployments write on the server. HTTPS by Cloudflare's origin certificate when Athanor
// installed one on that server, otherwise Caddy's own (ACME, with the owner's technical e-mail when the vault holds one);
// auth_proxy adds basicauth (the name Caddy 2.6 of Debian's packages knows too; 2.8+ still accepts it), whose bcrypt hash Athanor computes from an infra mot_de_passe entry of the vault:
// the password itself never leaves this process.

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	utilisateurProxy = "athanor" // the user name of auth_proxy's basicauth
	varEmailACME     = "ACME_EMAIL"
)

var (
	reAmontCaddy  = regexp.MustCompile(`^[A-Za-z0-9.\-]+:[0-9]{1,5}$`)
	reRacineCaddy = regexp.MustCompile(`^(/[A-Za-z0-9_\-][A-Za-z0-9._\-]*)+$`) // a static site's folder: absolute, no "..", no space
)

// plagesCloudflare: the addresses Cloudflare's proxy reaches origins from, as https://www.cloudflare.com/ips-v4
// and https://www.cloudflare.com/ips-v6 list them (read 2026-11-12). An access site answers only them (C58).
// shortcut: frozen here, Cloudflare changes them rarely; read those two lists when deploying if they ever move.
var plagesCloudflare = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18", "108.162.192.0/18",
	"190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32", "2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

// blocCaddy (pure): the site block of e on server serveur (inventory id), proxied to amont (host:port), or
// serving the folder amont when it is an absolute path (a static deployment, M131); email: ACME's account e-mail (may be
// empty); hachage: the bcrypt hash of auth_proxy's password (required by auth_proxy, ignored otherwise).
func blocCaddy(e Environnement, serveur, amont, email, hachage string) (string, error) {
	u, err := url.Parse(e.URL)
	if e.URL == "" || err != nil || u.Hostname() == "" {
		return "", errors.New("the environment has no URL")
	}
	statique := strings.HasPrefix(amont, "/")
	if statique && !reRacineCaddy.MatchString(amont) || !statique && !reAmontCaddy.MatchString(amont) {
		return "", errors.New("upstream: host:port, or the absolute folder of a static site")
	}
	if e.Protection == "auth_proxy" && u.Scheme == "http" {
		return "", errors.New("auth_proxy needs an https:// URL: the password would travel in clear")
	}
	hote, adresse := strings.ToLower(u.Hostname()), strings.ToLower(u.Host)
	var b strings.Builder
	if u.Scheme == "http" {
		adresse = "http://" + adresse
	}
	fmt.Fprintf(&b, "%s {\n", adresse)
	switch {
	case u.Scheme == "http":
	case e.Certificat != "" && slices.Contains(e.CertificatServeurs, serveur): // only where Caddy can read it (C60)
		fmt.Fprintf(&b, "\ttls %s/%s.crt %s/%s.key\n", dossierTLS, hote, dossierTLS, hote)
	case email != "":
		if !reEmailAuteur.MatchString(email) {
			return "", errors.New("the ACME e-mail is not an e-mail address")
		}
		fmt.Fprintf(&b, "\ttls %s\n", email)
	}
	b.WriteString("\tencode zstd gzip\n\theader {\n")
	if u.Scheme != "http" {
		b.WriteString("\t\tStrict-Transport-Security \"max-age=31536000; includeSubDomains\"\n")
	}
	b.WriteString("\t\tX-Content-Type-Options nosniff\n\t\tX-Frame-Options DENY\n\t\tReferrer-Policy strict-origin-when-cross-origin\n\t\t-Server\n\t}\n")
	if e.Protection == "access" { // only Cloudflare's proxy, so Access, reaches the site: never by the origin's address
		fmt.Fprintf(&b, "\t@horsCloudflare not remote_ip %s\n\trespond @horsCloudflare 403\n", strings.Join(plagesCloudflare, " "))
	}
	if e.Protection == "auth_proxy" {
		if !strings.HasPrefix(hachage, "$2") || strings.ContainsAny(hachage, " \t\n{}") {
			return "", errors.New("auth_proxy needs the password's bcrypt hash")
		}
		fmt.Fprintf(&b, "\tbasicauth {\n\t\t%s %s\n\t}\n", utilisateurProxy, hachage)
	}
	if statique {
		fmt.Fprintf(&b, "\troot * %s\n\tfile_server\n}\n", amont)
	} else {
		fmt.Fprintf(&b, "\treverse_proxy %s\n}\n", amont)
	}
	return b.String(), nil
}

// hachageProxy: the bcrypt hash of environment projet/env's auth_proxy password, the infra mot_de_passe entry
// of the vault with source proxy:<projet>/<env>, made (random) the first time; the owner reads it in the vault.
// shortcut: found by its source, so renaming the project makes a new password; record its id in the environment if that matters.
func (s *Serveur) hachageProxy(projet, env string) (string, error) {
	source := "proxy:" + projet + "/" + env
	l := s.coffre.Liste()
	i := slices.IndexFunc(l, func(e EntreeCoffre) bool {
		return e.Source == source && e.Type == "mot_de_passe" && e.Portee == "infra"
	})
	id := ""
	if i >= 0 {
		id = l[i].ID
	} else {
		n, err := s.coffre.Ajouter(EntreeCoffre{Nom: "PROXY_PASSWORD " + projet + "/" + env, Famille: "secret", Type: "mot_de_passe", Portee: "infra",
			Note: "Password of the site's protection (user " + utilisateurProxy + "), made by Athanor", Source: source}, aleatoire(16))
		if err != nil {
			return "", fmt.Errorf("the proxy password could not be made in the vault (%v): the owner opens the vault", err)
		}
		id = n.ID
	}
	mdp, err := s.coffre.Valeur(id)
	if err != nil {
		return "", fmt.Errorf("the proxy password cannot be read (%v): the owner opens the vault", err)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(mdp), bcrypt.DefaultCost)
	return string(h), err
}

// blocCaddyEnv: blocCaddy for environment projet/env on server serveur, its e-mail and password hash read from the vault.
func (s *Serveur) blocCaddyEnv(projet, env, serveur, amont string) (string, error) {
	e, ok := s.acces.Environnements(projet)[env]
	if !ok {
		return "", errors.New("unknown environment")
	}
	email, hachage := "", ""
	for _, v := range s.coffre.Liste() {
		if v.Famille == "variable" && v.Nom == varEmailACME {
			email, _ = s.coffre.Valeur(v.ID)
			email = strings.TrimSpace(email)
		}
	}
	if e.Protection == "auth_proxy" {
		var err error
		if hachage, err = s.hachageProxy(projet, env); err != nil {
			return "", err
		}
	}
	return blocCaddy(e, serveur, amont, email, hachage)
}
