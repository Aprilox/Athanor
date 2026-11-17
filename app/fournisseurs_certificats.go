package main

// The Cloudflare origin certificate of an environment (M117, ARCHITECTURE.md §Certificat d'origine): Athanor
// makes the ECDSA key in memory and its CSR, Cloudflare's origin CA signs it, the certificate and its key go to
// the infra vault (type certificat, source env:<project>/<env>) and onto every server of the environment, in
// /etc/athanor/tls/<host>.{crt,key}: the key root:caddy 0640 so Caddy reads it (0600 root where no caddy group
// exists, and that server is not equipped). The private key never reaches the agent.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const dossierTLS = "/etc/athanor/tls"

// emettreCertificat: a new key and its CSR for nom, signed by Cloudflare's origin CA (15 years); the certificate
// and the key in PEM, and the certificate's expiry. What comes back is checked: for nom, and for this key.
func (s *Serveur) emettreCertificat(jeton, nom string) (crt, cle string, expire time.Time, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", expire, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: nom}, DNSNames: []string{nom}}, k)
	if err != nil {
		return "", "", expire, err
	}
	var r struct {
		Certificate string `json:"certificate"`
	}
	corps := map[string]any{"csr": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})),
		"hostnames": []string{nom}, "request_type": "origin-ecc", "requested_validity": 5475}
	if err = s.cloudflare(jeton, "POST", "/certificates", corps, &r); err != nil {
		return "", "", expire, err
	}
	b, _ := pem.Decode([]byte(r.Certificate))
	var c *x509.Certificate
	if b != nil {
		c, err = x509.ParseCertificate(b.Bytes)
	}
	if b == nil || err != nil || c.VerifyHostname(nom) != nil || !k.PublicKey.Equal(c.PublicKey) {
		return "", "", expire, errors.New("Cloudflare's answer is not a certificate for this name and this key: nothing installed")
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return "", "", expire, err
	}
	return string(pem.EncodeToMemory(b)), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), c.NotAfter.UTC(), nil
}

// scriptTLS: $1 written in /etc/athanor/tls (made 0755) with mode $2, owned by root, from standard input; a
// temporary file made 0600 then renamed: never readable by others, never half written. Mode 640 is the key's:
// group caddy, so Caddy reads it (C59); without that group, 0600 root and sansGroupeCaddy said.
const scriptTLS = `t= g=0 m=$2
if [ "$m" = 640 ]; then if getent group caddy >/dev/null 2>&1; then g=caddy; else m=600; echo ` + sansGroupeCaddy + `; fi; fi
umask 022 && mkdir -p ` + dossierTLS + ` && umask 077 && t=$(mktemp "` + dossierTLS + `/.$1.XXXXXX") && cat > "$t" && chown 0:"$g" "$t" && chmod "$m" "$t" && mv -f -- "$t" "` + dossierTLS + `/$1" && exit 0
[ -n "$t" ] && rm -f -- "$t"; exit 1`

const sansGroupeCaddy = "no-caddy-group"

// poserCertificat: the key then the certificate of nom on server id, as root (sudo -n unless root logs in);
// lisible: the key went root:caddy 0640, so Caddy can read it.
func (s *Serveur) poserCertificat(id, nom, crt, cle string) (lisible bool, err error) {
	l, err := lireServeurs()
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return false, errServeurInconnu
	}
	ctx, fin := context.WithTimeout(context.Background(), 2*delaiFichier)
	defer fin()
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return false, err
	}
	defer c.Close()
	lisible = true
	for _, f := range [][3]string{{nom + ".key", "640", cle}, {nom + ".crt", "644", crt}} {
		r, err := executerSSH(ctx, c, scriptShell(l[i].Utilisateur != "root", scriptTLS, f[0], f[1]), strings.NewReader(f[2]), delaiFichier)
		if err == nil && r.Code != 0 {
			err = fmt.Errorf("code %d: %s", r.Code, couper(strings.TrimSpace(string(r.Sortie)), 300))
		}
		if err != nil {
			return false, fmt.Errorf("%s/%s could not be written on the server (root rights needed: root or sudo without password): %v", dossierTLS, f[0], err)
		}
		if strings.Contains(string(r.Sortie), sansGroupeCaddy) {
			lisible = false
		}
	}
	return lisible, nil
}

// installerCertificat: environment projet/env's origin certificate, read from the vault, onto each server of
// serveurs; those where Caddy can read the key join the environment's equipped servers (C59, C60). The answer:
// server id → what happened there; err only when it went onto none.
func (s *Serveur) installerCertificat(projet, env string, serveurs []string) (map[string]string, error) {
	e := s.acces.Environnements(projet)[env]
	if e.Certificat == "" {
		return nil, errors.New("the site has no origin certificate")
	}
	nom := hoteEnv(&e)
	v, err := s.coffre.Valeur(e.Certificat)
	i := strings.Index(v, "-----BEGIN EC PRIVATE KEY-----")
	if err != nil || i <= 0 || nom == "" {
		return nil, fmt.Errorf("the certificate cannot be read from the vault (%v): the owner opens the vault", err)
	}
	etats, equipes, poses := map[string]string{}, []string{}, 0
	for _, id := range serveurs {
		switch lisible, err := s.poserCertificat(id, nom, v[:i], v[i:]); {
		case err != nil:
			etats[id] = "not installed: " + err.Error()
		case !lisible:
			poses++
			etats[id] = "installed, but the key is 0600 root: no caddy group on this server, so Caddy cannot read it; install Caddy, then run certificat_origine again"
		default:
			poses++
			equipes = append(equipes, id)
			etats[id] = "installed, key root:caddy 0640"
		}
	}
	if err := s.acces.NoterServeursCertificat(projet, env, equipes); err != nil {
		return etats, err
	}
	if poses == 0 {
		return etats, fmt.Errorf("the certificate is in the vault but installed on no server: %v", etats)
	}
	return etats, nil
}

// certificatOrigine: the tool certificat_origine for x's environment, installed on all its servers (the
// accepted plan's server included).
func (s *Serveur) certificatOrigine(x SessionHub, jeton, nom, serveur string) (any, error) {
	projet, env, _ := strings.Cut(x.Env, "/")
	if serveur == "" || projet == "" {
		return nil, errors.New("certificat_origine runs only as a step of an accepted plan of a task launched for a project's deployment")
	}
	if e := s.acces.Environnements(projet)[env]; !strings.EqualFold(nom, hoteEnv(&e)) { // the file Caddy's tls line names
		return nil, fmt.Errorf("the origin certificate is for the site's host name (%q)", hoteEnv(&e))
	}
	crt, cle, expire, err := s.emettreCertificat(jeton, nom)
	if err != nil {
		return nil, err
	}
	// in the vault first: a certificate issued is never lost, even when the server refuses it
	valeur, note := crt+cle, "Cloudflare origin certificate of "+nom+", expires "+expire.Format(time.DateOnly)
	id := s.acces.Environnements(projet)[env].Certificat // renewed: the same entry
	if id == "" || func() error {
		_, e := s.coffre.ModifierMeta(id, "TLS_ORIGIN "+nom, "certificat", note, "infra", &valeur)
		return e
	}() != nil {
		n, err := s.coffre.Ajouter(EntreeCoffre{Nom: "TLS_ORIGIN " + nom, Famille: "secret", Type: "certificat", Portee: "infra", Note: note, Source: "env:" + x.Env}, valeur)
		if err != nil {
			return nil, fmt.Errorf("the certificate could not be kept in the vault (%v): nothing installed; the owner opens the vault", err)
		}
		id = n.ID
	}
	if err := s.acces.NoterCertificat(projet, env, id, expire.Format(time.RFC3339)); err != nil {
		return nil, err
	}
	cibles := s.acces.Environnements(projet)[env].Serveurs
	if !slices.Contains(cibles, serveur) {
		cibles = append(slices.Clone(cibles), serveur)
	}
	etats, err := s.installerCertificat(projet, env, cibles)
	if err != nil {
		return nil, err
	}
	return map[string]any{"resultat": "installed", "serveurs": etats, "certificat": dossierTLS + "/" + nom + ".crt", "cle": dossierTLS + "/" + nom + ".key",
		"expire": expire.Format(time.RFC3339), "suite": "Athanor's deployments point Caddy at these two files on the equipped servers; then set the zone's SSL mode to strict (ssl_regler)"}, nil
}
