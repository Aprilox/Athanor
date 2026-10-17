package main

// Application configuration: a JSON file next to the executable.
// It never holds a secret in clear text (the password is hashed and salted).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Kit                       string   `json:"kit"`
	Projets                   []Projet `json:"projets"`
	ProjetActif               string   `json:"projet_actif"`
	LancerPiloteAuDemarrage   bool     `json:"lancer_pilote_au_demarrage"`
	BureauVirtuel             bool     `json:"bureau_virtuel"`
	ProgrammesSupplementaires string   `json:"programmes_supplementaires"`
	MotDePasseHash            string   `json:"mot_de_passe_hash,omitempty"`
	Sel                       string   `json:"sel,omitempty"`
	Port                      int      `json:"port"`
	Langue                    string   `json:"langue"` // "" = never chosen (welcome wizard)
	Theme                     string   `json:"theme"`  // interface palette (see themesConnus)
	Accent                    string   `json:"accent"` // interface accent color (see accentsConnus)
	BienvenueVue              bool     `json:"bienvenue_vue"`
	// end of a pilot already announced to the user (project folder → STATE.json "maj" of that end)
	FinsVues map[string]string `json:"fins_vues,omitempty"`
}

func configDefaut() Config {
	return Config{
		BureauVirtuel:             true,
		ProgrammesSupplementaires: "UnrealEditor.exe\nUnrealEditor-Cmd.exe\nblender.exe\nchrome-headless-shell.exe\nadb.exe",
	}
}

// cheminConfig: the config file next to the executable. Athanor.exe and the Linux binary can sit in the
// same folder; each keeps its own file, since their paths (C:\... versus /home/...) are not interchangeable.
func cheminConfig() string {
	nom := "athanor-config.json"
	if runtime.GOOS != "windows" {
		nom = "athanor-config-" + runtime.GOOS + ".json"
	}
	exe, err := os.Executable()
	if err != nil {
		return nom
	}
	return filepath.Join(filepath.Dir(exe), nom)
}

func lireConfig() Config {
	c := configDefaut()
	if b, err := os.ReadFile(cheminConfig()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func ecrireConfig(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cheminConfig(), b, 0o600)
}

// devinerKit looks for the tool folder: the executable's folder, its parents, or the current folder.
func devinerKit() string {
	var candidats []string
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		candidats = append(candidats, d, filepath.Dir(d), filepath.Dir(filepath.Dir(d)))
	}
	if wd, err := os.Getwd(); err == nil {
		candidats = append(candidats, wd)
	}
	for _, c := range candidats {
		if (Kit{c}).Valide() {
			return c
		}
	}
	return ""
}
