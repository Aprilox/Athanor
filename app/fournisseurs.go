package main

// The Servers module's providers (Cloudflare): fournisseurs.json in its data folder (0600, written
// atomically, never a token: the token is named by its infra vault entry id) and its API: list, add from a
// vault entry, test (fournisseurs_cloudflare.go), remove.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ZoneFournisseur: a zone the token sees and what it may do there (deduced by Test).
type ZoneFournisseur struct {
	ID     string   `json:"id"`
	Nom    string   `json:"nom"`
	Droits []string `json:"droits"`
}

type Fournisseur struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"` // "cloudflare"
	Nom    string            `json:"nom"`
	Cle    string            `json:"cle"` // id of its infra vault entry (the token stays in the vault)
	Compte string            `json:"compte"`
	Zones  []ZoneFournisseur `json:"zones"`
	Droits []string          `json:"droits"` // every right seen on at least one zone
	Teste  time.Time         `json:"teste,omitzero"`
	Etat   string            `json:"etat"` // "" (never tested) | "ok" | "refuse" | "expire" | "desactive" | "sans_zone" | "injoignable"
	Bilan  *MessageTest      `json:"bilan,omitempty"`
}

type fichierFournisseurs struct {
	Version      int           `json:"version"`
	Fournisseurs []Fournisseur `json:"fournisseurs"`
}

var (
	fournisseursMu         sync.Mutex
	errFournisseursFichier = errors.New("The providers file cannot be read")
	errFournisseurCle      = erreurEntree("Choose a Cloudflare entry of the vault")
	errFournisseurPortee   = erreurEntree("A provider's token must be in the infra scope: change its scope in the Vault first")
	errFournisseurNom      = erreurEntree("Give the provider a name (100 characters at most)")
	errFournisseurUtilise  = "Take this provider out of these environments first: {0}"
)

func fichierFournisseursChemin() string {
	return filepath.Join(dossierModule("servers"), "fournisseurs.json")
}

// lireFournisseurs: the providers (empty when there is no file yet).
func lireFournisseurs() ([]Fournisseur, error) {
	fournisseursMu.Lock()
	defer fournisseursMu.Unlock()
	return lireFichierFournisseurs()
}

func lireFichierFournisseurs() ([]Fournisseur, error) {
	b, err := os.ReadFile(fichierFournisseursChemin())
	if errors.Is(err, os.ErrNotExist) {
		return []Fournisseur{}, nil
	}
	var f fichierFournisseurs
	if err != nil || json.Unmarshal(b, &f) != nil || f.Version != 1 {
		return nil, errFournisseursFichier
	}
	if f.Fournisseurs == nil {
		f.Fournisseurs = []Fournisseur{}
	}
	return f.Fournisseurs, nil
}

// modifierFournisseurs: reads, changes and writes the providers under one lock; nothing is written on error.
func modifierFournisseurs(f func([]Fournisseur) ([]Fournisseur, error)) error {
	fournisseursMu.Lock()
	defer fournisseursMu.Unlock()
	l, err := lireFichierFournisseurs()
	if err != nil {
		return err
	}
	if l, err = f(l); err != nil {
		return err
	}
	b, err := json.MarshalIndent(fichierFournisseurs{Version: 1, Fournisseurs: l}, "", "  ")
	if err != nil {
		return err
	}
	return ecrireAtomique(fichierFournisseursChemin(), b, 0o600)
}

// cleFournisseur: the vault entry of a provider's token, refused unless it is a Cloudflare entry in infra.
func (s *Serveur) cleFournisseur(id string) (EntreeCoffre, error) {
	i := slices.IndexFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == id })
	if i < 0 {
		return EntreeCoffre{}, errFournisseurCle
	}
	e := s.coffre.Liste()[i]
	switch {
	case e.Famille != "secret" || e.Type != "cloudflare":
		return e, errFournisseurCle
	case e.Portee != "infra":
		return e, errFournisseurPortee
	}
	return e, nil
}

func (s *Serveur) erreurFournisseurs(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errServeurInconnu):
		s.erreur(w, http.StatusNotFound, errEnvFourn.Error())
	case errors.Is(err, errCoffreVerrouille), errors.Is(err, errCoffreSansMdp), errors.Is(err, errCoffreInconnu):
		s.erreurCoffre(w, err)
	case errors.As(err, new(erreurEntree)):
		s.erreur(w, http.StatusBadRequest, err.Error())
	default:
		s.erreur(w, http.StatusInternalServerError, err.Error())
	}
}

// apiFournisseurs: GET /api/fournisseurs, the providers and the environments that use each.
func (s *Serveur) apiFournisseurs(w http.ResponseWriter, _ *http.Request) {
	l, err := lireFournisseurs()
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"fournisseurs": l, "environnements": s.acces.UtilisationsFournisseurs()})
}

// apiFournisseurAjouter: POST /api/fournisseurs {nom, cle}: a Cloudflare provider from an infra vault entry.
func (s *Serveur) apiFournisseurAjouter(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom string `json:"nom"`
		Cle string `json:"cle"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.coffrePret(w) {
		return
	}
	c.Nom = strings.TrimSpace(c.Nom)
	e, err := s.cleFournisseur(c.Cle)
	if err == nil && c.Nom == "" {
		c.Nom = e.Nom
	}
	if err == nil && (c.Nom == "" || utf8.RuneCountInString(c.Nom) > 100) {
		err = errFournisseurNom
	}
	f := Fournisseur{ID: aleatoire(8), Type: "cloudflare", Nom: c.Nom, Cle: c.Cle, Zones: []ZoneFournisseur{}, Droits: []string{}}
	if err == nil {
		err = modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return append(l, f), nil })
	}
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "fournisseur ajouté",
		Resultat: f.Nom + " [" + f.ID + "], clé " + e.Nom + " [" + e.ID + "]"})
	s.ecrireJSON(w, f)
}

// apiFournisseurSupprimer: DELETE /api/fournisseurs/{id}, refused while an environment uses it (named).
func (s *Serveur) apiFournisseurSupprimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if envs := s.acces.UtilisationsFournisseurs()[id]; len(envs) > 0 {
		s.erreurArgs(w, http.StatusConflict, errFournisseurUtilise, strings.Join(envs, ", "))
		return
	}
	var nom string
	err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		i := slices.IndexFunc(l, func(f Fournisseur) bool { return f.ID == id })
		if i < 0 {
			return nil, errServeurInconnu
		}
		nom = l[i].Nom
		return slices.Delete(l, i, i+1), nil
	})
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "fournisseur retiré", Resultat: nom + " [" + id + "]"})
	s.apiFournisseurs(w, r)
}
