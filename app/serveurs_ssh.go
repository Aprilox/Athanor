package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Connexion SSH du module Serveurs (étape 5) : la clé privée vient du coffre infra et ne vit qu'en mémoire,
// l'empreinte d'hôte apprise à l'ajout est toujours exigée.

const sortieMaxSSH = 1 << 20 // sortie d'une commande coupée à 1 Mo

var delaiSSH = 10 * time.Second // connexion et poignée de main ; les tests le raccourcissent

var errEmpreinteChangee = errors.New("The server's host fingerprint changed: connection refused (check the server, then remove and add it again)")

// AccesSSH : de quoi joindre un serveur. Cle (PEM OpenSSH) ou MotDePasse (ajout seulement, jamais gardé).
type AccesSSH struct {
	Hote        string
	Port        int
	Utilisateur string
	Cle         []byte
	MotDePasse  string
	Empreinte   string // "SHA256:…", obligatoire
}

func adresseSSH(hote string, port int) string {
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(hote, strconv.Itoa(port))
}

// dialSSH : TCP puis poignée de main SSH, le tout borné par delaiSSH (un serveur muet ne bloque pas) ;
// variable pour que le monde des captures dirige un serveur de démonstration vers son serveur en mémoire.
var dialSSH = dialSSHReseau

func dialSSHReseau(ctx context.Context, adresse string, conf *ssh.ClientConfig) (*ssh.Client, error) {
	ctx, annuler := context.WithTimeout(ctx, delaiSSH)
	defer annuler()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", adresse)
	if err != nil {
		return nil, err
	}
	fin, _ := ctx.Deadline()
	conn.SetDeadline(fin)
	stop := context.AfterFunc(ctx, func() { conn.Close() }) // annulation de l'appelant
	c, canaux, requetes, err := ssh.NewClientConn(conn, adresse, conf)
	if !stop() || err != nil {
		conn.Close()
		var ne net.Error
		if err == nil || ctx.Err() != nil || errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("ssh %s: no answer within %s", adresse, delaiSSH)
		}
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, canaux, requetes), nil
}

// empreinteHoteSSH : l'empreinte que présente le serveur, à faire confirmer par le propriétaire (aucune
// authentification tentée), et le type de sa clé (celle que le client négocie : ECDSA avant ed25519).
func empreinteHoteSSH(ctx context.Context, hote string, port int) (string, string, error) {
	var vue, typ string
	conf := &ssh.ClientConfig{User: "athanor", HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		vue, typ = ssh.FingerprintSHA256(k), k.Type()
		return errEmpreinteChangee // on s'arrête là
	}}
	_, err := dialSSH(ctx, adresseSSH(hote, port), conf)
	if vue != "" {
		return vue, typ, nil
	}
	return "", "", err
}

// cleHote : le type d'une clé d'hôte comme ssh-keygen -l l'écrit (ED25519, ECDSA, RSA) et la commande qui
// donne son empreinte sur le serveur.
func cleHote(typ string) (string, string) {
	nom := map[string]string{"ssh-ed25519": "ed25519", "ssh-rsa": "rsa", "ssh-dss": "dsa"}[typ]
	if strings.HasPrefix(typ, "ecdsa-") {
		nom = "ecdsa"
	}
	if nom == "" { // un type rare : toutes les clés du serveur, le propriétaire y retrouve la sienne
		return strings.ToUpper(typ), "for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done"
	}
	return strings.ToUpper(nom), "ssh-keygen -lf /etc/ssh/ssh_host_" + nom + "_key.pub"
}

// connecterSSH : client authentifié par clé ou mot de passe, refusé si l'empreinte d'hôte manque ou diffère.
func connecterSSH(ctx context.Context, a AccesSSH) (*ssh.Client, error) {
	if a.Empreinte == "" {
		return nil, errors.New("no host fingerprint confirmed for this server")
	}
	conf := &ssh.ClientConfig{User: a.Utilisateur, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if ssh.FingerprintSHA256(k) != a.Empreinte {
			return errEmpreinteChangee
		}
		return nil
	}}
	switch {
	case len(a.Cle) > 0:
		signer, err := ssh.ParsePrivateKey(a.Cle)
		if err != nil {
			return nil, fmt.Errorf("unreadable SSH key: %w", err)
		}
		conf.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case a.MotDePasse != "":
		conf.Auth = []ssh.AuthMethod{ssh.Password(a.MotDePasse)}
	default:
		return nil, errors.New("no key and no password")
	}
	c, err := dialSSH(ctx, adresseSSH(a.Hote, a.Port), conf)
	if errors.Is(err, errEmpreinteChangee) {
		return nil, errEmpreinteChangee
	}
	return c, err
}

// ResultatSSH : sortie (stdout puis stderr, mêlés dans l'ordre d'arrivée), code de sortie, coupure.
type ResultatSSH struct {
	Sortie []byte
	Code   int
	Coupee bool
}

// executerSSH : une commande, bornée par delai (la session est fermée au-delà) ; entree peut être nil.
func executerSSH(ctx context.Context, c *ssh.Client, commande string, entree io.Reader, delai time.Duration) (ResultatSSH, error) {
	s, err := c.NewSession()
	if err != nil {
		return ResultatSSH{}, err
	}
	defer s.Close()
	var tampon bytes.Buffer
	sortie := &limiteur{b: &tampon, max: sortieMaxSSH + 1} // un octet de plus : de quoi savoir si c'est coupé
	s.Stdout, s.Stderr, s.Stdin = sortie, sortie, entree
	ctx, annuler := context.WithTimeout(ctx, delai)
	defer annuler()
	fini := make(chan error, 1)
	go func() { fini <- s.Run(commande) }()
	select {
	case err = <-fini:
	case <-ctx.Done():
		// C34 : le processus distant est arrêté, pas seulement le canal ; SIGTERM et non SIGKILL : `timeout`
		// (commandeBornee) le relaie à tout son groupe puis tue 5 s plus tard, un SIGKILL ne tuerait que lui
		_ = s.Signal(ssh.SIGTERM)
		s.Close() // la sortie partielle est encore écrite par la session : on ne la lit pas
		if errors.Is(ctx.Err(), context.Canceled) {
			return ResultatSSH{Code: -1}, errors.New("command stopped: the operation was cancelled (task stopped, session revoked or emergency stop)")
		}
		return ResultatSSH{Code: -1}, fmt.Errorf("command stopped after %s", delai)
	}
	r := ResultatSSH{Sortie: tampon.Bytes()}
	if len(r.Sortie) > sortieMaxSSH {
		r.Sortie, r.Coupee = r.Sortie[:sortieMaxSSH], true
	}
	var sortieErr *ssh.ExitError
	if errors.As(err, &sortieErr) {
		r.Code = sortieErr.ExitStatus()
		err = nil
	}
	return r, err
}
