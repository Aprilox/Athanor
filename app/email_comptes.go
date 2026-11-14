package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// CompteEmail: an SMTP account; its password is the vault entry Entree, never in this file.
type CompteEmail struct {
	ID          string `json:"id"`
	Nom         string `json:"nom"`
	Hote        string `json:"hote"`
	Port        int    `json:"port"`
	Securite    string `json:"securite"` // "starttls" | "tls"
	Utilisateur string `json:"utilisateur"`
	Expediteur  string `json:"expediteur"`
	Entree      string `json:"entree"`
	Cree        string `json:"cree"`
	Modifie     string `json:"modifie"`
}

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
