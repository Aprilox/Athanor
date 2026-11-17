package main

// The sheets of the vault's Cloudflare API: fournisseurs.json in the Servers module's data folder (0600, written
// atomically, never a token: the token is named by its infra vault entry id). M289: a sheet is made and
// updated by the Vault's Test (coffre_tester.go, fournisseurs_cloudflare.go) and goes with its vault entry.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
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
	errFournisseursFichier = errors.New("The Cloudflare APIs file cannot be read")
	errFournisseurCle      = erreurEntree("Choose a Cloudflare entry of the vault")
	errFournisseurPortee   = erreurEntree("A Cloudflare token must be in the infra scope: change its scope in the Vault first")
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

// testerFiche (M289): Test on a Cloudflare API of the infra vault keeps what it learns (zones, rights, state)
// as that API's sheet in fournisseurs.json, made on the first test and found again by its vault entry.
func (s *Serveur) testerFiche(e EntreeCoffre, jeton, de string) (Fournisseur, error) {
	l, err := lireFournisseurs()
	if err != nil {
		return Fournisseur{}, err
	}
	f := Fournisseur{ID: aleatoire(8), Type: "cloudflare", Cle: e.ID}
	if i := slices.IndexFunc(l, func(x Fournisseur) bool { return x.Cle == e.ID }); i >= 0 {
		f = l[i]
	}
	f = s.testerFournisseur(f, jeton)
	f.Nom = e.Nom
	err = modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		vu := false
		for j := range l {
			if l[j].Cle == e.ID { // an older file may hold several sheets of one token: each keeps its id
				x := f
				x.ID, vu = l[j].ID, true
				l[j] = x
			}
		}
		if !vu {
			l = append(l, f)
		}
		return l, nil
	})
	if err != nil {
		return Fournisseur{}, err
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "cloudflare", Action: "API testée",
		Resultat: e.Nom + " [" + e.ID + "]: " + f.Etat + ", " + strconv.Itoa(len(f.Zones)) + " zone(s)"})
	return f, nil
}

// fichesCoffre: vault entry id → its sheet, for the Vault's cards (never a token).
func fichesCoffre() map[string]Fournisseur {
	m := map[string]Fournisseur{}
	l, _ := lireFournisseurs()
	for _, f := range slices.Backward(l) { // the first sheet of an entry wins
		m[f.Cle] = f
	}
	return m
}

// oublierFiches: the sheets of a vault entry go with it.
func oublierFiches(cle string) {
	deCle := func(f Fournisseur) bool { return f.Cle == cle }
	if l, _ := lireFournisseurs(); slices.ContainsFunc(l, deCle) {
		_ = modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) { return slices.DeleteFunc(l, deCle), nil })
	}
}
