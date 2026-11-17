package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// CompteEmail: an SMTP account; its password is the vault entry Entree, never in this file.
type CompteEmail struct {
	ID          string `json:"id"`
	Nom         string `json:"nom"`
	Hote        string `json:"hote"`
	Port        int    `json:"port"`
	Securite    string `json:"securite"` // "starttls" | "tls" | "none" (localhost only)
	Utilisateur string `json:"utilisateur"`
	Expediteur  string `json:"expediteur"`
	Entree      string `json:"entree"`
	Cree        string `json:"cree"`
	Modifie     string `json:"modifie"`
	// the last test send (M203): on the card, and in the bell while it failed
	DernierTest *ResultatEnvoiEmail `json:"dernier_test,omitempty"`
	// its sending settings (M204): SMTP_HOST… → vault variable, created at the first « Configure sending »
	Variables map[string]string `json:"variables,omitempty"`
}

// the refusals of an account form: fixed messages (never the input), translated by the page
const (
	errEmailRequete     = erreurEntree("Invalid request")
	errEmailNom         = erreurEntree("Give the account a name (100 characters at most)")
	errEmailHote        = erreurEntree("The SMTP server is not a valid host name")
	errEmailPort        = erreurEntree("The port must be a number between 1 and 65535")
	errEmailSecurite    = erreurEntree("Sending without encryption is only allowed to this PC (localhost)")
	errEmailUtilisateur = erreurEntree("The user name is not valid")
	errEmailExpediteur  = erreurEntree("The sender is not a valid e-mail address")
	errEmailMdp         = erreurEntree("Give the account's password (1024 characters at most)")
	errEmailInconnu     = erreurEntree("Unknown email account")
	errEmailMdpServeur  = erreurEntree("The SMTP server changed: retype the account's password, so it only goes to the server you chose")
	errEmailComptes     = "The email accounts cannot be read"
)

// errsEmail: every message of the module that reaches the page (TestEmailComptesTraduction).
var errsEmail = []string{errEmailRequete.Error(), errEmailNom.Error(), errEmailHote.Error(), errEmailPort.Error(),
	errEmailSecurite.Error(), errEmailUtilisateur.Error(), errEmailExpediteur.Error(), errEmailMdp.Error(),
	errEmailInconnu.Error(), errEmailComptes, errEmailConflit.Error(), errEmailPasConfigu.Error()}

var (
	muComptesEmail sync.Mutex // comptes.json: read, changed and written under it
	reHoteEmail    = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

func fichierComptesEmail() string { return filepath.Join(dossierModule("email"), "comptes.json") }

// lireComptesEmail: the accounts, an empty list when none was added yet.
func lireComptesEmail() ([]CompteEmail, error) {
	liste := []CompteEmail{}
	b, err := os.ReadFile(fichierComptesEmail())
	if errors.Is(err, fs.ErrNotExist) {
		return liste, nil
	}
	if err != nil {
		return liste, err
	}
	if err := json.Unmarshal(b, &liste); err != nil {
		return []CompteEmail{}, err
	}
	return liste, nil
}

func ecrireComptesEmail(l []CompteEmail) error {
	return ecrireJSONFichier(fichierComptesEmail(), l, 0o600)
}

// corpsCompteEmail: what the form sends; MotDePasse empty on a change keeps the saved one.
type corpsCompteEmail struct {
	Nom         string `json:"nom"`
	Hote        string `json:"hote"`
	Port        int    `json:"port"`
	Securite    string `json:"securite"`
	Utilisateur string `json:"utilisateur"`
	Expediteur  string `json:"expediteur"`
	MotDePasse  string `json:"mot_de_passe"`
}

func hoteLocal(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

// verifier normalises and checks the fields of an account (the password is checked by the caller).
func (c *corpsCompteEmail) verifier() error {
	c.Nom, c.Hote = strings.TrimSpace(c.Nom), strings.ToLower(strings.TrimSpace(c.Hote))
	c.Utilisateur, c.Expediteur = strings.TrimSpace(c.Utilisateur), strings.TrimSpace(c.Expediteur)
	switch {
	case c.Nom == "" || utf8.RuneCountInString(c.Nom) > 100:
		return errEmailNom
	case len(c.Hote) > 253 || (net.ParseIP(c.Hote) == nil && !reHoteEmail.MatchString(c.Hote)):
		return errEmailHote
	case c.Port < 1 || c.Port > 65535:
		return errEmailPort
	case c.Securite != "starttls" && c.Securite != "tls" && c.Securite != "none":
		return errEmailRequete
	case c.Securite == "none" && !hoteLocal(c.Hote):
		return errEmailSecurite
	case utf8.RuneCountInString(c.Utilisateur) > 254 || strings.IndexFunc(c.Utilisateur, unicode.IsControl) >= 0:
		return errEmailUtilisateur
	}
	if a, err := mail.ParseAddress(c.Expediteur); err != nil || len(c.Expediteur) > 320 || a.Address == "" {
		return errEmailExpediteur
	}
	return nil
}

func mdpEmailValide(m string) bool { return m != "" && utf8.RuneCountInString(m) <= 1024 }

// lireCorpsEmail decodes and checks the body; an error never echoes the input.
func lireCorpsEmail(w http.ResponseWriter, r *http.Request) (corpsCompteEmail, error) {
	var c corpsCompteEmail
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := lireCorps(r, &c); err != nil {
		return c, errEmailRequete
	}
	return c, c.verifier()
}

func nomEntreeEmail(nom string) string { return "SMTP — " + nom }

// apiEmailComptes: GET /api/email/comptes, the accounts (no password: it is only in the vault).
func (s *Serveur) apiEmailComptes(w http.ResponseWriter, r *http.Request) {
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	type compteVu struct {
		CompteEmail
		Projets []ProjetEmail `json:"projets"` // where it sends (M204): listed before a delete
	}
	vus := []compteVu{}
	for _, c := range liste {
		vus = append(vus, compteVu{c, s.acces.projetsEmail(c.Entree)})
	}
	s.ecrireJSON(w, map[string]any{"comptes": vus, "projets_hub": s.nomsProjets()})
}

// apiEmailAjouter: POST /api/email/comptes, a new account; its password goes into the vault (type smtp).
func (s *Serveur) apiEmailAjouter(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	c, err := lireCorpsEmail(w, r)
	if err == nil && !mdpEmailValide(c.MotDePasse) {
		err = errEmailMdp
	}
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	muComptesEmail.Lock()
	defer muComptesEmail.Unlock()
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nomEntreeEmail(c.Nom), Famille: "secret", Type: "smtp", Portee: "code",
		Note: "Password of the SMTP account " + c.Nom + " (" + c.Utilisateur + " @ " + c.Hote + ")"}, c.MotDePasse)
	if err != nil {
		s.erreurCoffre(w, err) // a closed code key: 423
		return
	}
	maintenant := time.Now().UTC().Format(time.RFC3339)
	compte := CompteEmail{ID: aleatoire(8), Nom: c.Nom, Hote: c.Hote, Port: c.Port, Securite: c.Securite,
		Utilisateur: c.Utilisateur, Expediteur: c.Expediteur, Entree: e.ID, Cree: maintenant, Modifie: maintenant}
	if err := ecrireComptesEmail(append(liste, compte)); err != nil {
		_ = s.coffre.Supprimer(e.ID) // no password left behind without its account
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "email", Action: "compte e-mail ajouté", Resultat: compte.Nom})
	s.ecrireJSON(w, compte)
}

// apiEmailModifier: POST /api/email/comptes/{id}; a password typed replaces the vault's, empty keeps it.
func (s *Serveur) apiEmailModifier(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	c, err := lireCorpsEmail(w, r)
	if err == nil && c.MotDePasse != "" && !mdpEmailValide(c.MotDePasse) {
		err = errEmailMdp
	}
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	muComptesEmail.Lock()
	defer muComptesEmail.Unlock()
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	i := slices.IndexFunc(liste, func(x CompteEmail) bool { return x.ID == r.PathValue("id") })
	if i < 0 {
		s.erreur(w, http.StatusNotFound, errEmailInconnu.Error())
		return
	}
	compte := liste[i]
	if c.MotDePasse == "" && (c.Hote != compte.Hote || c.Port != compte.Port || c.Securite != compte.Securite) {
		s.erreurCoffre(w, errEmailMdpServeur) // RT1: the kept password must not follow the account to another server
		return
	}
	if c.Nom != compte.Nom || c.MotDePasse != "" {
		var mdp *string
		if c.MotDePasse != "" {
			mdp = &c.MotDePasse
		}
		note := "Password of the SMTP account " + c.Nom + " (" + c.Utilisateur + " @ " + c.Hote + ")"
		if _, err := s.coffre.ModifierMeta(compte.Entree, nomEntreeEmail(c.Nom), "smtp", note, "code", mdp); err != nil {
			s.erreurCoffre(w, err)
			return
		}
		s.injecterTous(nil) // a new password reaches the projects it is granted to
	}
	compte.Nom, compte.Hote, compte.Port, compte.Securite = c.Nom, c.Hote, c.Port, c.Securite
	compte.Utilisateur, compte.Expediteur = c.Utilisateur, c.Expediteur
	compte.Modifie = time.Now().UTC().Format(time.RFC3339)
	compte.DernierTest = nil       // changed settings: the last test no longer tells about them
	if len(compte.Variables) > 0 { // the projects it sends for get the new settings
		if _, err := s.variablesCompteEmail(&compte); err != nil {
			s.erreurCoffre(w, err)
			return
		}
		s.injecterTous(nil)
	}
	liste[i] = compte
	if err := ecrireComptesEmail(liste); err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	s.ecrireJSON(w, compte)
}

// apiEmailSupprimer: DELETE /api/email/comptes/{id}; its password leaves the vault (and every grant of it).
func (s *Serveur) apiEmailSupprimer(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	muComptesEmail.Lock()
	defer muComptesEmail.Unlock()
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	i := slices.IndexFunc(liste, func(x CompteEmail) bool { return x.ID == r.PathValue("id") })
	if i < 0 {
		s.erreur(w, http.StatusNotFound, errEmailInconnu.Error())
		return
	}
	compte := liste[i]
	for _, id := range entreesEmail(compte) { // its password and its variables, with every grant of them
		if err := s.retirerDuCoffre(id); err != nil && !errors.Is(err, errCoffreInconnu) {
			s.erreurCoffre(w, err)
			return
		}
	}
	if err := ecrireComptesEmail(slices.Delete(liste, i, i+1)); err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "email", Action: "compte e-mail retiré", Resultat: compte.Nom})
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
