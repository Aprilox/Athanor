package main

// Adding a server (P4): the address gives the host fingerprint the owner confirms (never accepted
// silently), then the server is reached with an SSH key of the vault, or with an ed25519 key generated
// here (infra entry of the vault, its public part and an authorized_keys command to copy, then Verify), or
// with the server's password given once (a generated key is put in authorized_keys, then the server is
// reached again with it; the password is never kept nor logged). Cutting password logins comes after.
// The private key never leaves the vault and this process: no response, audit line or file of
// modules/servers holds it.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

var (
	errServeurAdresse     = erreurEntree("Give the server's address (a host name or an IP address)")
	errServeurPort        = erreurEntree("The port must be between 1 and 65535")
	errServeurUtilisateur = erreurEntree("Give a valid user name to connect with")
	errServeurEmpreinte   = erreurEntree("Confirm the server's host fingerprint first")
	errServeurMode        = erreurEntree("Unknown way to add a server")
	errServeurPasCle      = erreurEntree("This vault entry is not a usable SSH private key")
	errServeurCleCode     = erreurEntree("A server's key must be in the infra scope: change its scope in the Vault first")
	errServeurPhrase      = erreurEntree("This SSH key is protected by a passphrase: Athanor needs a key without one")
	errServeurInjoignable = errors.New("The server did not answer")
	errServeurCleRefusee  = errors.New("The server refused the key: check that its public part is in ~/.ssh/authorized_keys")
	errServeurMdpVide     = erreurEntree("Give the server's password")
	errServeurMdpRefuse   = errors.New("The server refused the password")
	errServeurClePosee    = errors.New("The key could not be put in ~/.ssh/authorized_keys on the server")

	reHoteServeur        = regexp.MustCompile(`^[A-Za-z0-9._:%-]{1,253}$`)
	reUtilisateurServeur = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,31}$`)
)

// corpsAjoutServeur: POST /api/serveurs/ajouter. With id: Verify the server waiting for its generated key.
type corpsAjoutServeur struct {
	ID          string   `json:"id"`
	Nom         string   `json:"nom"`
	Hote        string   `json:"hote"`
	Port        int      `json:"port"`
	Utilisateur string   `json:"utilisateur"`
	Empreinte   string   `json:"empreinte"` // the fingerprint the owner confirmed
	Mode        string   `json:"mode"`      // coffre | generee | mot_de_passe
	Cle         string   `json:"cle"`       // mode coffre: the vault entry
	Roles       []string `json:"roles"`
	MotDePasse  string   `json:"mot_de_passe"` // Athanor's password: native mode + an existing infra key (hub_preuve.go)
	// mode mot_de_passe: the server's password, used for this request only
	MotDePasseServeur string `json:"mot_de_passe_serveur"`
}

func verifierAdresseServeur(hote string, port int) error {
	switch {
	case !reHoteServeur.MatchString(hote) || strings.HasPrefix(hote, "-"):
		return errServeurAdresse
	case port < 1 || port > 65535:
		return errServeurPort
	}
	return nil
}

// statutAjout: the HTTP status of an addition error.
func statutAjout(err error) int {
	switch {
	case errors.Is(err, errCoffreSansMdp), errors.Is(err, errEmpreinteChangee), errors.Is(err, errServeurCleRefusee),
		errors.Is(err, errServeurMdpRefuse), errors.As(err, new(erreurCoupure)):
		return http.StatusConflict // no password: the message names where to choose one
	case errors.Is(err, errCoffreVerrouille):
		return http.StatusLocked
	case errors.Is(err, errCoffreInconnu), errors.Is(err, errServeurInconnu):
		return http.StatusNotFound
	case errors.Is(err, errServeurInjoignable), errors.Is(err, errServeurClePosee), errors.Is(err, errFicheIllisible):
		return http.StatusBadGateway
	case errors.As(err, new(erreurEntree)):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// apiServeursEmpreinte: POST /api/serveurs/empreinte {hote, port}: the host fingerprint to confirm.
func (s *Serveur) apiServeursEmpreinte(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Hote string `json:"hote"`
		Port int    `json:"port"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if err := verifierAdresseServeur(c.Hote, c.Port); err != nil {
		s.erreur(w, statutAjout(err), err.Error())
		return
	}
	e, typ, err := empreinteHoteSSH(r.Context(), c.Hote, c.Port)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, errServeurInjoignable.Error())
		return
	}
	nom, commande := cleHote(typ)
	s.ecrireJSON(w, map[string]string{"empreinte": e, "type": nom, "commande": commande})
}

// connecterServeur: a client reached with the vault entry's key and the confirmed fingerprint, its error
// told in the owner's words.
func (s *Serveur) connecterServeur(ctx context.Context, srv ServeurInventaire) (*ssh.Client, error) {
	cle, err := s.coffre.Valeur(srv.Cle)
	if err != nil {
		return nil, err
	}
	c, err := connecterSSH(ctx, AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: srv.Utilisateur, Cle: []byte(cle), Empreinte: srv.EmpreinteHote})
	err = erreurConnexion(err, errServeurCleRefusee)
	noterJoignable(srv.ID, err, ctx.Err() == nil)
	return c, err
}

// noterJoignable: the list shows what the last connection learnt (a terminal, a tool, a deployment), not
// only the last sheet: unreachable after a failure (not one the owner cancelled), reachable again after a
// success. A changed fingerprint keeps its own state (rafraichirFiche, C43).
func noterJoignable(id string, err error, abouti bool) {
	injoignable := errors.Is(err, errServeurInjoignable) || errors.Is(err, errServeurCleRefusee)
	if id == "" || (err != nil && (!injoignable || !abouti)) {
		return
	}
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		switch {
		case i < 0 || l[i].Vu.IsZero() || l[i].Etat == "empreinte_changee":
			return nil, errServeurInconnu // not added yet, or a state this connection cannot settle: unchanged
		case err != nil && l[i].Etat != "injoignable":
			l[i].Etat = "injoignable"
		case err == nil && l[i].Etat == "injoignable":
			l[i].Etat, l[i].Vu = "ok", time.Now().UTC()
		default:
			return nil, errServeurInconnu // nothing to write
		}
		return l, nil
	})
}

// erreurConnexion: a connection error as the owner reads it (refuse: what an authentication refusal means).
func erreurConnexion(err, refuse error) error {
	var phrase *ssh.PassphraseMissingError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errEmpreinteChangee):
		return errEmpreinteChangee
	case errors.As(err, &phrase):
		return errServeurPhrase
	case strings.HasPrefix(err.Error(), "unreadable SSH key"):
		return errServeurPasCle
	case strings.Contains(err.Error(), "unable to authenticate"):
		return refuse
	}
	return errServeurInjoignable
}

// essayerCle: connects with the vault entry's key and the confirmed fingerprint, then hangs up.
func (s *Serveur) essayerCle(r *http.Request, srv ServeurInventaire) error {
	c, err := s.connecterServeur(r.Context(), srv)
	if err != nil {
		return err
	}
	return c.Close()
}

// ajouterCleGeneree: a new ed25519 key for srv, kept as an infra entry of the vault; its public line.
func (s *Serveur) ajouterCleGeneree(srv ServeurInventaire) (EntreeCoffre, string, error) {
	prive, public, err := cleGeneree("athanor-" + srv.ID)
	if err != nil {
		return EntreeCoffre{}, "", err
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH " + srv.Nom, Famille: "secret", Type: "ssh", Portee: "infra",
		Source: "serveurs:" + srv.ID, Note: srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port)}, prive)
	return e, public, err
}

// poserCle: puts the public line in ~/.ssh/authorized_keys once (.ssh 700, the file 600), whatever was there.
func poserCle(ctx context.Context, c *ssh.Client, public string) error {
	// public is "ssh-ed25519 <base64> athanor-<hex>": nothing in it can leave the single quotes
	r, err := executerSSH(ctx, c, "umask 077; mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && "+
		"{ grep -qxF '"+public+"' ~/.ssh/authorized_keys || echo '"+public+"' >> ~/.ssh/authorized_keys; }", nil, 20*time.Second)
	if err != nil || r.Code != 0 {
		return errServeurClePosee
	}
	return nil
}

// cleGeneree: a new ed25519 key, as the vault keeps it (OpenSSH PEM) and as authorized_keys wants it.
func cleGeneree(commentaire string) (prive, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	bloc, err := ssh.MarshalPrivateKey(priv, commentaire)
	if err != nil {
		return "", "", err
	}
	p, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(bloc)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(p))) + " " + commentaire, nil
}

// commandeAuthorizedKeys: what the owner runs on the server to let the generated key in.
func commandeAuthorizedKeys(public string) string {
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '" + public + "' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
}

// apiServeursAjouter: POST /api/serveurs/ajouter: a server reached with a vault key (mode coffre), a
// generated key (mode generee: the key and the command to copy, the server waits for Verify), or Verify
// ({id}: the server's own key is tried, then the server is ready).
func (s *Serveur) apiServeursAjouter(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	var c corpsAjoutServeur
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	echec := func(err error) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "ajout de serveur refusé", Resultat: err.Error()})
		s.erreur(w, statutAjout(err), err.Error())
	}
	var srv ServeurInventaire
	if c.ID != "" { // Verify
		liste, err := lireServeurs()
		if err != nil {
			s.erreurServeurs(w, err)
			return
		}
		i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == c.ID })
		if i < 0 {
			s.erreurServeurs(w, errServeurInconnu)
			return
		}
		srv = liste[i]
	} else {
		if c.Port == 0 {
			c.Port = 22
		}
		c.Nom = strings.TrimSpace(c.Nom)
		if c.Nom == "" {
			c.Nom = c.Hote
		}
		roles, err := rolesValides(c.Roles)
		switch {
		case err != nil:
		case verifierAdresseServeur(c.Hote, c.Port) != nil:
			err = verifierAdresseServeur(c.Hote, c.Port)
		case !reUtilisateurServeur.MatchString(c.Utilisateur):
			err = errServeurUtilisateur
		case utf8.RuneCountInString(c.Nom) > 100:
			err = erreurEntree("The name is too long (100 characters at most)")
		case strings.IndexFunc(c.Nom, caractereInvisible) >= 0:
			err = errNomInvisible
		case !strings.HasPrefix(c.Empreinte, "SHA256:"):
			err = errServeurEmpreinte
		case c.Mode != "coffre" && c.Mode != "generee" && c.Mode != "mot_de_passe":
			err = errServeurMode
		case c.Mode == "mot_de_passe" && c.MotDePasseServeur == "":
			err = errServeurMdpVide
		}
		if err != nil {
			s.erreur(w, statutAjout(err), err.Error())
			return
		}
		srv = ServeurInventaire{ID: aleatoire(8), Nom: c.Nom, Hote: c.Hote, Port: c.Port, Utilisateur: c.Utilisateur,
			Cle: c.Cle, EmpreinteHote: c.Empreinte, Roles: roles, Cree: time.Now().UTC(), Etat: "inconnu"}
	}

	if c.ID == "" && c.Mode == "mot_de_passe" {
		s.ajouterParMotDePasse(w, r, srv, c.MotDePasseServeur, echec)
		return
	}
	if c.ID == "" && c.Mode == "generee" {
		e, public, err := s.ajouterCleGeneree(srv)
		if err != nil {
			echec(err)
			return
		}
		srv.Cle = e.ID
		if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { return append(l, srv), nil }); err != nil {
			_ = s.coffre.Supprimer(e.ID)
			s.erreurServeurs(w, err)
			return
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "cle_serveur_generee", Resultat: srv.Nom + " " + srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port)})
		s.ecrireJSON(w, map[string]any{"serveur": srv, "cle_publique": public, "commande": commandeAuthorizedKeys(public)})
		return
	}

	// the key: an SSH secret of the vault; an existing infra key in native mode needs the owner (C25)
	i := slices.IndexFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == srv.Cle })
	if i < 0 {
		echec(errCoffreInconnu)
		return
	}
	e := s.coffre.Liste()[i]
	if e.Type != "ssh" || e.Famille != "secret" {
		echec(errServeurPasCle)
		return
	}
	if e.Portee != "infra" { // C35: a code entry reopens alone on this PC and can be granted to a project
		echec(errServeurCleCode)
		return
	}
	if modeNatif.Load() && e.Source != "serveurs:"+srv.ID {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "ajout de serveur refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	if err := s.essayerCle(r, srv); err != nil {
		echec(err)
		return
	}
	s.serveurPret(w, r, srv, c.ID == "", "coffre")
}

// ajouterParMotDePasse: password login, a generated key put in authorized_keys, then the server reached
// again with that key; on any failure the generated key leaves the vault. The password ends its life here.
func (s *Serveur) ajouterParMotDePasse(w http.ResponseWriter, r *http.Request, srv ServeurInventaire, mdp string, echec func(error)) {
	client, err := connecterSSH(r.Context(), AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: srv.Utilisateur, MotDePasse: mdp, Empreinte: srv.EmpreinteHote})
	if err != nil {
		echec(erreurConnexion(err, errServeurMdpRefuse))
		return
	}
	defer client.Close()
	e, public, err := s.ajouterCleGeneree(srv)
	if err != nil {
		echec(err)
		return
	}
	srv.Cle = e.ID
	if err = poserCle(r.Context(), client, public); err == nil {
		err = s.essayerCle(r, srv)
	}
	if err != nil {
		_ = s.coffre.Supprimer(e.ID)
		echec(err)
		return
	}
	s.serveurPret(w, r, srv, true, "mot_de_passe")
}

// serveurPret: srv answered with its key: saved as ok (nouveau: appended; else Verify updates it).
func (s *Serveur) serveurPret(w http.ResponseWriter, r *http.Request, srv ServeurInventaire, nouveau bool, mode string) {
	srv.Etat, srv.Vu = "ok", time.Now().UTC()
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		j := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == srv.ID })
		if nouveau {
			return append(l, srv), nil
		}
		if j < 0 {
			return nil, errServeurInconnu // removed meanwhile
		}
		l[j].Etat, l[j].Vu = srv.Etat, srv.Vu
		srv = l[j]
		return l, nil
	})
	if err != nil {
		if mode == "mot_de_passe" {
			_ = s.coffre.Supprimer(srv.Cle)
		}
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "serveur_ajoute", Resultat: srv.Nom + " " + srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port) + " (" + mode + ")"})
	// its sheet at once (serveurs_fiche.go); a sheet that fails leaves the server added, told by fiche_erreur
	rep := map[string]any{"serveur": srv, "fiche": nil}
	if f, err := s.rafraichirFiche(r.Context(), srv); err != nil {
		rep["fiche_erreur"] = err.Error()
	} else {
		rep["fiche"] = f
	}
	// its runbook (serveurs_atelier.go), with what the sheet saw when there is one
	verrouRunbook.Lock()
	if _, err := assurerRunbook(srv.ID); err != nil {
		rep["runbook_erreur"] = err.Error()
	}
	verrouRunbook.Unlock()
	l, _ := lireServeurs() // its state and last sight as the sheet left them
	if i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == srv.ID }); i >= 0 {
		rep["serveur"] = l[i]
	}
	s.ecrireJSON(w, rep)
}

// erreurCoupure: why password logins were not cut (409: the server stays as it was).
type erreurCoupure string

func (e erreurCoupure) Error() string { return string(e) }

const (
	errCoupureAvantCle = erreurCoupure("Connect to this server with its key first: password logins can only be cut after that")
	errCoupureSudo     = erreurCoupure("Cutting password logins needs root or sudo without a password on the server")
	errCoupureConfig   = erreurCoupure("The server's SSH configuration did not accept the change: nothing was changed")
	errCoupureIgnoree  = erreurCoupure("Another setting of the server's SSH configuration still allows passwords: nothing was changed")
	errCoupureRecharge = erreurCoupure("The server's SSH service could not be reloaded: nothing was changed")
	errCoupureAnnulee  = erreurCoupure("The key no longer opened the server after the change: it was undone, password logins are still allowed")
	errCoupureBloquee  = erreurCoupure("The key no longer opened the server and the change could not be undone: remove /etc/ssh/sshd_config.d/00-athanor.conf on the server, then reload its SSH service")
)

// shellCoupure: root or sudo -n, the drop-in file, and how to reload sshd (systemd, OpenRC, else its pid file).
const shellCoupure = `S=; [ "$(id -u)" = 0 ] || S="sudo -n"
[ -z "$S" ] || sudo -n true 2>/dev/null || exit 3
F=/etc/ssh/sshd_config.d/00-athanor.conf; SSHD=$(command -v sshd || echo /usr/sbin/sshd)
recharger() { $S systemctl reload ssh 2>/dev/null || $S systemctl reload sshd 2>/dev/null || $S rc-service sshd reload 2>/dev/null ||
  $S kill -HUP "$(cat /run/sshd.pid 2>/dev/null || cat /var/run/sshd.pid 2>/dev/null)" 2>/dev/null; }
`

// scriptCoupure: the drop-in sorts first (sshd keeps the first value it reads), sshd -t then sshd -T check
// it really applies, then reload; every failure removes the file again. Exit codes: see couperMotDePasse.
const scriptCoupure = shellCoupure + `$S mkdir -p /etc/ssh/sshd_config.d &&
printf '# Athanor: password logins cut. To allow them again, remove this file, then reload the SSH service.\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n' | $S tee "$F" >/dev/null || exit 4
$S "$SSHD" -t >/dev/null 2>&1 || { $S rm -f "$F"; exit 4; }
$S "$SSHD" -T 2>/dev/null | grep -qix 'passwordauthentication no' || { $S rm -f "$F"; exit 5; }
recharger || { $S rm -f "$F"; exit 6; }
`

const scriptRetourCoupure = shellCoupure + `$S rm -f "$F" && recharger`

var attenteRechargeSSH = time.Second // between reconnection tries while sshd reloads; tests shorten it

// couperMotDePasse: POST /api/serveurs/{id} {couper_mot_de_passe:true}: only for a server already reached
// with its key. The first session stays open through the change, so that it can be undone if the key no
// longer opens the server once sshd is reloaded.
func (s *Serveur) couperMotDePasse(w http.ResponseWriter, r *http.Request, id string) {
	if !s.coffrePret(w) {
		return
	}
	ctx := context.WithoutCancel(r.Context()) // C42: a closed browser must not stop the check or the undo
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	srv := liste[i]
	echec := func(err error) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "coupure du mot de passe refusée", Resultat: srv.Nom + ": " + err.Error()})
		s.erreur(w, statutAjout(err), err.Error())
	}
	if srv.Vu.IsZero() {
		echec(errCoupureAvantCle)
		return
	}
	client, err := s.connecterServeur(ctx, srv)
	if err != nil {
		echec(err)
		return
	}
	defer client.Close()
	res, err := executerSSH(ctx, client, scriptCoupure, nil, time.Minute)
	if err != nil {
		echec(errServeurInjoignable)
		return
	}
	if e, ok := map[int]error{3: errCoupureSudo, 4: errCoupureConfig, 5: errCoupureIgnoree, 6: errCoupureRecharge}[res.Code]; ok {
		echec(e)
		return
	} else if res.Code != 0 {
		echec(errCoupureConfig)
		return
	}
	var nouveau *ssh.Client
	for essai := 0; essai < 5; essai++ { // sshd may be restarting
		if nouveau, err = s.connecterServeur(ctx, srv); err == nil {
			break
		}
		time.Sleep(attenteRechargeSSH)
	}
	if err != nil {
		if res, err := executerSSH(ctx, client, scriptRetourCoupure, nil, time.Minute); err != nil || res.Code != 0 {
			echec(errCoupureBloquee)
		} else {
			echec(errCoupureAnnulee)
		}
		return
	}
	nouveau.Close()
	err = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		j := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if j < 0 {
			return nil, errServeurInconnu
		}
		l[j].MotDePasseCoupe, l[j].Etat, l[j].Vu = true, "ok", time.Now().UTC()
		srv = l[j]
		return l, nil
	})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "mot_de_passe_coupe", Resultat: srv.Nom + " " + srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port)})
	s.ecrireJSON(w, map[string]any{"serveur": srv})
}
