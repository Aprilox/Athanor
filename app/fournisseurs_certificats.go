package main

// The Cloudflare origin certificate of an environment (M117, ARCHITECTURE.md §Certificat d'origine): Athanor
// makes the ECDSA key in memory and its CSR, Cloudflare's origin CA signs it, the certificate and its key go to
// the infra vault (type certificat, source env:<project>/<env>) and onto the plan's server, in
// /etc/athanor/tls/<host>.{crt,key} (the key 0600 root). The private key never reaches the agent.

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
// temporary file made 0600 then renamed: never readable by others, never half written.
const scriptTLS = `umask 022 && mkdir -p ` + dossierTLS + ` && umask 077 && t=$(mktemp "` + dossierTLS + `/.$1.XXXXXX") && cat > "$t" && chown 0:0 "$t" && chmod "$2" "$t" && mv -f -- "$t" "` + dossierTLS + `/$1" && exit 0
[ -n "$t" ] && rm -f -- "$t"; exit 1`

// poserCertificat: the key then the certificate of nom on server id, as root (sudo -n unless root logs in).
func (s *Serveur) poserCertificat(id, nom, crt, cle string) error {
	l, err := lireServeurs()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return errServeurInconnu
	}
	ctx, fin := context.WithTimeout(context.Background(), 2*delaiFichier)
	defer fin()
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return err
	}
	defer c.Close()
	for _, f := range [][3]string{{nom + ".key", "600", cle}, {nom + ".crt", "644", crt}} {
		r, err := executerSSH(ctx, c, scriptShell(l[i].Utilisateur != "root", scriptTLS, f[0], f[1]), strings.NewReader(f[2]), delaiFichier)
		if err == nil && r.Code != 0 {
			err = fmt.Errorf("code %d: %s", r.Code, couper(strings.TrimSpace(string(r.Sortie)), 300))
		}
		if err != nil {
			return fmt.Errorf("%s/%s could not be written on the server (root rights needed: root or sudo without password): %v", dossierTLS, f[0], err)
		}
	}
	return nil
}

// certificatOrigine: the tool certificat_origine for x's environment, installed on the accepted plan's server.
func (s *Serveur) certificatOrigine(x SessionHub, jeton, nom, serveur string) (any, error) {
	projet, env, _ := strings.Cut(x.Env, "/")
	if serveur == "" || projet == "" {
		return nil, errors.New("certificat_origine runs only as a step of an accepted plan of a task launched for an environment")
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
	if err := s.poserCertificat(serveur, nom, crt, cle); err != nil {
		return nil, fmt.Errorf("the certificate is in the vault but not installed: %w", err)
	}
	return map[string]any{"resultat": "installed", "serveur": serveur, "certificat": dossierTLS + "/" + nom + ".crt", "cle": dossierTLS + "/" + nom + ".key",
		"expire": expire.Format(time.RFC3339), "suite": "point the web server at these two files, then set the zone's SSL mode to strict (ssl_regler)"}, nil
}
