package main

// The vault's HTTP API (GET/POST /api/coffre, POST/DELETE /api/coffre/{id}) and its hub module. No
// answer ever holds a secret's value: entries go out through Liste/masquer, errors are fixed messages.

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxNomCoffre    = 100
	maxNoteCoffre   = 2000
	maxValeurCoffre = 64 << 10
)

const (
	errCoffreRequete = erreurEntree("Invalid request")
	errCoffreValeur  = erreurEntree("Give the entry a value")
	errCoffreNomLong = erreurEntree("The name is too long (100 characters at most)")
	errCoffreNote    = erreurEntree("The note is too long (2000 characters at most)")
	errCoffreTaille  = erreurEntree("The value is too large (64 KiB at most)")
)

// erreurCoffre answers a vault error with its status: a closed key is 423, a bad entry 400.
func (s *Serveur) erreurCoffre(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, errCoffreVerrouille), errors.Is(err, errCoffreSansMdp):
		code = http.StatusLocked
	case errors.Is(err, errCoffreInconnu):
		code = http.StatusNotFound
	case errors.As(err, new(erreurEntree)):
		code = http.StatusBadRequest
	}
	s.erreur(w, code, err.Error())
}

// coffrePret: the loaded vault, or false once the refusal is written.
func (s *Serveur) coffrePret(w http.ResponseWriter) bool {
	if s.coffreErr != nil || s.coffre == nil {
		s.erreur(w, http.StatusInternalServerError, "The vault file cannot be read")
		return false
	}
	return true
}

// corpsCoffre: what the page sends; a nil field is left unchanged by POST /api/coffre/{id}.
type corpsCoffre struct {
	Nom     *string `json:"nom"`
	Famille string  `json:"famille"`
	Type    *string `json:"type"`
	Note    *string `json:"note"`
	Portee  *string `json:"portee"`
	Valeur  *string `json:"valeur"`
	// M225: the production value when it differs ("" goes back to one value for every environment)
	Prod *string `json:"prod"`
	// Athanor's password, retyped to move an infra entry to code (hub_preuve.go)
	MotDePasse string `json:"mot_de_passe"`
}

// lireCorpsCoffre decodes and bounds the body; a decoding error never echoes the input.
func lireCorpsCoffre(w http.ResponseWriter, r *http.Request) (corpsCoffre, error) {
	var c corpsCoffre
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxValeurCoffre)
	if err := lireCorps(r, &c); err != nil {
		return c, errCoffreRequete
	}
	switch {
	case c.Nom != nil && utf8.RuneCountInString(*c.Nom) > maxNomCoffre:
		return c, errCoffreNomLong
	case c.Note != nil && utf8.RuneCountInString(*c.Note) > maxNoteCoffre:
		return c, errCoffreNote
	case c.Valeur != nil && len(*c.Valeur) > maxValeurCoffre, c.Prod != nil && len(*c.Prod) > maxValeurCoffre:
		return c, errCoffreTaille
	case c.Valeur != nil && *c.Valeur == "":
		return c, errCoffreValeur
	}
	return c, nil
}

func val(p *string, defaut string) string {
	if p == nil {
		return defaut
	}
	return *p
}

// apiCoffre: GET /api/coffre, the entries (secrets without their value) and the state of the keys.
func (s *Serveur) apiCoffre(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	s.ecrireJSON(w, map[string]any{"entrees": s.coffre.Liste(), "etat": s.etatCoffre(), "migration": s.resumeMigration()})
}

// apiCoffreAjouter: POST /api/coffre, a new entry.
func (s *Serveur) apiCoffreAjouter(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	c, err := lireCorpsCoffre(w, r)
	if err == nil && c.Valeur == nil {
		err = errCoffreValeur
	}
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: val(c.Nom, ""), Famille: c.Famille, Type: val(c.Type, ""),
		Note: val(c.Note, ""), Portee: val(c.Portee, "")}, *c.Valeur)
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	if c.Prod != nil && *c.Prod != "" {
		if err := s.coffre.PoserValeurProd(e.ID, *c.Prod); err != nil {
			_ = s.coffre.Supprimer(e.ID) // all or nothing
			s.erreurCoffre(w, err)
			return
		}
		e.DeuxValeurs = true
	}
	s.injecterTous(nil) // a value now in the vault and granted nowhere leaves the old files
	s.ecrireJSON(w, e)
}

// apiCoffreModifier: POST /api/coffre/{id}, new metadata and/or a new value.
func (s *Serveur) apiCoffreModifier(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	c, err := lireCorpsCoffre(w, r)
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	id := r.PathValue("id")
	var e EntreeCoffre
	for _, x := range s.coffre.Liste() {
		if x.ID == id {
			e = x
		}
	}
	if e.ID == "" {
		s.erreurCoffre(w, errCoffreInconnu)
		return
	}
	versCode := e.Portee == "infra" && val(c.Portee, e.Portee) == "code"
	if versCode { // C35: code reopens alone on this PC (DPAPI) and can be granted: never a server's key, always the owner
		if s.cleDeServeur(w, id, "This key opens the server {0}: it stays in the infra scope") {
			return
		}
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "portée infra → code refusée", Resultat: e.Nom + ": " + err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	if c.Nom != nil || c.Type != nil || c.Note != nil || c.Portee != nil || c.Valeur != nil || c.Prod != nil {
		if e.Portee == "code" && val(c.Portee, e.Portee) == "infra" && s.acces != nil && s.accesErr == nil {
			// code to infra: its grants go and the files are rewritten while it is still code (its fingerprint
			// then recognises the value); an infra entry has none and would stay as an old-file line (M43 A1)
			reste := slices.DeleteFunc(s.coffre.Liste(), func(x EntreeCoffre) bool { return x.ID == id })
			if err := s.acces.Synchroniser(reste, s.nomsProjets()); err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.injecterTous(nil)
		}
		if e, err = s.coffre.ModifierMeta(id, val(c.Nom, e.Nom), val(c.Type, e.Type), val(c.Note, e.Note), val(c.Portee, e.Portee), c.Valeur); err != nil {
			s.erreurCoffre(w, err)
			return
		}
		if c.Prod != nil {
			if err := s.coffre.PoserValeurProd(id, *c.Prod); err != nil {
				s.erreurCoffre(w, err)
				return
			}
			e.DeuxValeurs = *c.Prod != ""
		}
		s.injecterTous(nil)
	}
	if versCode {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "portée infra → code", Resultat: e.Nom})
	}
	s.ecrireJSON(w, e)
}

// apiCoffreSupprimer: DELETE /api/coffre/{id}.
func (s *Serveur) apiCoffreSupprimer(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	id := r.PathValue("id")
	if s.cleDeServeur(w, id, "This key opens the server {0}: remove that server first") { // removed, it would cut it off
		return
	}
	if err := s.retirerDuCoffre(id); err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

// retirerDuCoffre deletes an entry with its grants (also used by the e-mail module for an account's password).
func (s *Serveur) retirerDuCoffre(id string) error {
	if s.acces != nil && s.accesErr == nil {
		// its grants go and the files are rewritten while it is still in the vault: its value is then
		// recognised and leaves every secrets.env instead of staying there as an old-file line
		reste := slices.DeleteFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == id })
		if err := s.acces.Synchroniser(reste, s.nomsProjets()); err != nil {
			return err
		}
		s.injecterTous(nil)
	}
	if err := s.coffre.Supprimer(id); err != nil {
		return err
	}
	_ = s.synchroniserAcces()
	return nil
}

// cleDeServeur: true, the refusal written (409 with the servers' names), when a server reaches its host with
// this vault entry or the inventory cannot be read.
func (s *Serveur) cleDeServeur(w http.ResponseWriter, id, message string) bool {
	noms, err := serveursUtilisantCle(id)
	if err != nil {
		s.erreurServeurs(w, err)
		return true
	}
	if len(noms) == 0 {
		return false
	}
	n := strings.Join(noms, ", ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": message, "args": []string{n}, "erreur": strings.Replace(message, "{0}", n, 1)})
	return true
}
