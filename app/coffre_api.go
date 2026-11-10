package main

// The vault's HTTP API (GET/POST /api/coffre, POST/DELETE /api/coffre/{id}) and its hub module. No
// answer ever holds a secret's value: entries go out through Liste/masquer, errors are fixed messages.

import (
	"errors"
	"net/http"
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
	case c.Valeur != nil && len(*c.Valeur) > maxValeurCoffre:
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
	s.ecrireJSON(w, map[string]any{"entrees": s.coffre.Liste(), "etat": s.etatCoffre()})
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
	if c.Nom != nil || c.Type != nil || c.Note != nil || c.Portee != nil {
		if e, err = s.coffre.ModifierMeta(id, val(c.Nom, e.Nom), val(c.Type, e.Type), val(c.Note, e.Note), val(c.Portee, e.Portee)); err != nil {
			s.erreurCoffre(w, err)
			return
		}
	}
	if c.Valeur != nil {
		if err := s.coffre.RemplacerValeur(id, *c.Valeur); err != nil {
			s.erreurCoffre(w, err)
			return
		}
	}
	for _, x := range s.coffre.Liste() {
		if x.ID == id {
			e = x
		}
	}
	s.ecrireJSON(w, e)
}

// apiCoffreSupprimer: DELETE /api/coffre/{id}.
func (s *Serveur) apiCoffreSupprimer(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	if err := s.coffre.Supprimer(r.PathValue("id")); err != nil {
		s.erreurCoffre(w, err)
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
