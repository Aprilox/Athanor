package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestComposeCaddyConteneur (C70, C74): a static site then a compose one on the same host: the site file
// becomes Caddy's reverse_proxy to 127.0.0.1 (the sources are no longer published); a port published beyond
// 127.0.0.1 is refused without the owner's accord; going back serves the static version with its own site
// again; behind auth_proxy, Caddy answers 401 without the password and the service with it.
func TestComposeCaddyConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	ports := func(ip string) string {
		return `{"services":{"web":{"image":"x","ports":[{"mode":"ingress","host_ip":"` + ip + `","target":80,"published":"18080","protocol":"tcp"}]}}}`
	}
	depot, c := depotCompose(t,
		map[string]string{"index.html": "statique-v1", "app/compose.yaml": "services: {}\n"},
		map[string]string{"index.html": "x", "app/compose.yaml": "services: {}\n", "app/config.json": ports("127.0.0.1")},
		map[string]string{"index.html": "x", "app/compose.yaml": "services: {}\n", "app/config.json": ports("")})
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m145", Dossier: depot})
	s.mu.Unlock()
	const hote, hoteS = "m145.test", "m145s.test"
	resoudreTest(t, hote+":80", web)
	resoudreTest(t, hoteS+":443", web) // the container's 443 is not published: the check from this PC fails, curl inside answers
	if err := s.acces.definirAncien("m145", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.definirAncien("m145", "staging", Environnement{URL: "https://" + hoteS, Serveurs: []string{id}, Protection: "auth_proxy", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.NoterCertificat("m145", "staging", "essai", "2027-01-01"); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.NoterServeursCertificat("m145", "staging", []string{id}); err != nil {
		t.Fatal(err)
	}
	crt, cle := certificatEssai(t, hoteS)
	nettoyer := "sudo -n rm -rf /srv/athanor/m145-dev /srv/athanor/m145-staging /etc/caddy/sites/" + hote + ".caddy /etc/caddy/sites/" + hoteS + ".caddy " +
		dossierTLS + "/" + hoteS + ".*; sudo -n pkill -f /tmp/amont.caddy; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	sur(nettoyer)
	t.Cleanup(func() { sur(nettoyer) })
	// the service: a second Caddy on 127.0.0.1:18080, the certificate of hoteS in Athanor's TLS folder
	amont := base64.StdEncoding.EncodeToString([]byte("{\n\tadmin off\n}\n:18080 {\n\tbind 127.0.0.1\n\trespond \"amont-ok\"\n}\n"))
	sur("echo " + amont + " | base64 -d | sudo -n tee /tmp/amont.caddy >/dev/null; sudo -n setsid -f caddy run --config /tmp/amont.caddy --adapter caddyfile </dev/null >/dev/null 2>&1")
	sur("sudo -n install -d " + dossierTLS + " && echo " + crt + " | base64 -d | sudo -n tee " + dossierTLS + "/" + hoteS + ".crt >/dev/null && echo " + cle + " | base64 -d | sudo -n tee " +
		dossierTLS + "/" + hoteS + ".key >/dev/null && sudo -n chgrp caddy " + dossierTLS + "/" + hoteS + ".key && sudo -n chmod 640 " + dossierTLS + "/" + hoteS + ".key")
	lire := func(chemin string) string {
		req, _ := http.NewRequest("GET", "http://"+web+chemin, nil)
		req.Host = hote
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return err.Error()
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return fmt.Sprint(r.StatusCode, " ", strings.TrimSpace(string(b)))
	}
	site := func(h string) string { return sur("sudo -n cat /etc/caddy/sites/" + h + ".caddy") }
	ctx := context.Background()

	d0, err := s.deployer(ctx, "m145", "dev", c[0], OptionsDeploiement{Config: &ConfigDeploiement{Methode: "statique", Source: "artefact"}, Auteur: "proprietaire"})
	if err != nil || d0.Etat != "ok" || lire("/app/compose.yaml") != "200 services: {}" {
		t.Fatalf("static v1: %v %+v %q", err, d0, lire("/app/compose.yaml"))
	}
	// static → compose: the site file replaced by the proxy, the sources no longer published
	d1, err := s.deployer(ctx, "m145", "dev", c[1], OptionsDeploiement{Config: &ConfigDeploiement{Methode: "compose", Source: "artefact", Dossier: "app"}, Auteur: "proprietaire"})
	if err != nil || d1.Etat != "ok" || d1.Methode != "compose" || d1.Amont != "127.0.0.1:18080" {
		t.Fatalf("compose: %v %+v", err, d1)
	}
	if b := site(hote); !strings.Contains(b, "reverse_proxy 127.0.0.1:18080") || strings.Contains(b, "file_server") {
		t.Errorf("compose site file: %q", b)
	}
	if g := lire("/app/compose.yaml"); g != "200 amont-ok" {
		t.Errorf("behind Caddy: %q", g)
	}
	// a port published on every interface, without the owner's accord: refused, the proxy kept
	d2, err := s.deployer(ctx, "m145", "dev", c[2], OptionsDeploiement{Auteur: "relais"})
	if err == nil || d2.Etat != "echec" || !strings.Contains(err.Error(), "service web: port 80 published beyond 127.0.0.1") {
		t.Fatalf("published everywhere: %v %+v", err, d2)
	}
	if b, g := site(hote), lire("/"); !strings.Contains(b, "reverse_proxy 127.0.0.1:18080") || g != "200 amont-ok" {
		t.Errorf("after the refusal: %q %q", b, g)
	}
	// going back to the static version: its own method and site file
	rr, err := s.revenir(ctx, "m145", "dev", "proprietaire", "")
	if err != nil || rr.Etat != "ok" || rr.Version != d0.Version || rr.Methode != "statique" {
		t.Fatalf("back to static: %v %+v", err, rr)
	}
	if b, g := site(hote), lire("/"); !strings.Contains(b, "file_server") || strings.Contains(b, "reverse_proxy") || g != "200 statique-v1" {
		t.Errorf("after going back: %q %q", b, g)
	}

	// auth_proxy: 401 without the password, the service with it
	d3, err := s.deployer(ctx, "m145", "staging", c[1], OptionsDeploiement{Config: &ConfigDeploiement{Methode: "compose", Source: "artefact", Dossier: "app"}, Auteur: "proprietaire"})
	if err == nil || !strings.Contains(err.Error(), "the external verification failed") || d3.Amont != "127.0.0.1:18080" {
		t.Fatalf("staging: %v %+v", err, d3)
	}
	if b := site(hoteS); !strings.Contains(b, "basicauth") || !strings.Contains(b, "reverse_proxy 127.0.0.1:18080") {
		t.Errorf("staging site file: %q", b)
	}
	mdp := ""
	for _, e := range s.coffre.Liste() {
		if e.Source == "proxy:m145/staging" {
			mdp, _ = s.coffre.Valeur(e.ID)
		}
	}
	jeton := base64.StdEncoding.EncodeToString([]byte(utilisateurProxy + ":" + mdp))
	curl := "curl -sk --max-time 10 --resolve " + hoteS + ":443:127.0.0.1 https://" + hoteS + "/"
	if g := sur(curl + " -o /dev/null -w '%{http_code}'"); g != "401" {
		t.Errorf("without the password: %q", g)
	}
	if g := sur(curl + " -H 'Authorization: Basic " + jeton + "'"); g != "amont-ok" {
		t.Errorf("with the password: %q", g)
	}
}

// certificatEssai: a self-signed certificate and its key for host, PEM in base64.
func certificatEssai(t *testing.T, host string) (string, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	modele := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, modele, modele, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	b64 := func(typ string, b []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}))
	}
	return b64("CERTIFICATE", der), b64("EC PRIVATE KEY", kd)
}
