package main

// Adding a server (P4): the address gives the host fingerprint the owner confirms (never accepted
// silently), then the server is reached with an SSH key of the vault, or with an ed25519 key generated
// here (infra entry of the vault, its public part and an authorized_keys command to copy, then Verify).
// The private key never leaves the vault and this process: no response, audit line or file of
// modules/servers holds it.

import (
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
	errServeurPhrase      = erreurEntree("This SSH key is protected by a passphrase: Athanor needs a key without one")
	errServeurInjoignable = errors.New("The server did not answer")
	errServeurCleRefusee  = errors.New("The server refused the key: check that its public part is in ~/.ssh/authorized_keys")

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
	Mode        string   `json:"mode"`      // coffre | generee
	Cle         string   `json:"cle"`       // mode coffre: the vault entry
	Roles       []string `json:"roles"`
	MotDePasse  string   `json:"mot_de_passe"` // Athanor's password: native mode + an existing infra key (hub_preuve.go)
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
	case errors.Is(err, errCoffreSansMdp), errors.Is(err, errEmpreinteChangee), errors.Is(err, errServeurCleRefusee):
		return http.StatusConflict // no password: the message names where to choose one
	case errors.Is(err, errCoffreVerrouille):
		return http.StatusLocked
	case errors.Is(err, errCoffreInconnu), errors.Is(err, errServeurInconnu):
		return http.StatusNotFound
	case errors.Is(err, errServeurInjoignable):
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
	e, err := empreinteHoteSSH(r.Context(), c.Hote, c.Port)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, errServeurInjoignable.Error())
		return
	}
	s.ecrireJSON(w, map[string]string{"empreinte": e})
}

// essayerCle: connects with the vault entry's key and the confirmed fingerprint, then hangs up.
func (s *Serveur) essayerCle(r *http.Request, srv ServeurInventaire) error {
	cle, err := s.coffre.Valeur(srv.Cle)
	if err != nil {
		return err
	}
	c, err := connecterSSH(r.Context(), AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: srv.Utilisateur, Cle: []byte(cle), Empreinte: srv.EmpreinteHote})
	var phrase *ssh.PassphraseMissingError
	switch {
	case err == nil:
		return c.Close()
	case errors.Is(err, errEmpreinteChangee):
		return errEmpreinteChangee
	case errors.As(err, &phrase):
		return errServeurPhrase
	case strings.HasPrefix(err.Error(), "unreadable SSH key"):
		return errServeurPasCle
	case strings.Contains(err.Error(), "unable to authenticate"):
		return errServeurCleRefusee
	}
	return errServeurInjoignable
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
		case !strings.HasPrefix(c.Empreinte, "SHA256:"):
			err = errServeurEmpreinte
		case c.Mode != "coffre" && c.Mode != "generee":
			err = errServeurMode
		}
		if err != nil {
			s.erreur(w, statutAjout(err), err.Error())
			return
		}
		srv = ServeurInventaire{ID: aleatoire(8), Nom: c.Nom, Hote: c.Hote, Port: c.Port, Utilisateur: c.Utilisateur,
			Cle: c.Cle, EmpreinteHote: c.Empreinte, Roles: roles, Cree: time.Now().UTC(), Etat: "inconnu"}
	}

	if c.ID == "" && c.Mode == "generee" {
		prive, public, err := cleGeneree("athanor-" + srv.ID)
		if err != nil {
			echec(err)
			return
		}
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SSH " + srv.Nom, Famille: "secret", Type: "ssh", Portee: "infra",
			Source: "serveurs:" + srv.ID, Note: srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port)}, prive)
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
	if e.Portee == "infra" && modeNatif.Load() && e.Source != "serveurs:"+srv.ID {
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
	srv.Etat, srv.Vu = "ok", time.Now().UTC()
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		j := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == srv.ID })
		if c.ID == "" {
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
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "serveur_ajoute", Resultat: srv.Nom + " " + srv.Utilisateur + "@" + adresseSSH(srv.Hote, srv.Port)})
	s.ecrireJSON(w, map[string]any{"serveur": srv})
}
