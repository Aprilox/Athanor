package main

// Application configuration: a JSON file next to the executable.
// It never holds a secret in clear text (the password is hashed and salted).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// native Windows mode: the pilot is Athanor.exe and Claude Code the Windows program, without
	// WSL nor sandbox (natif_windows.go)
	ModeNatif bool `json:"mode_natif"`
	// the programs on Claude's desktop are heard while a pilot runs (bureau_calme_windows.go; muted by default)
	EntendreBureauClaude bool `json:"entendre_bureau_claude,omitempty"`
	// no look for a new version of Athanor when it opens (maj.go; on by default)
	PasVerifierMaj bool `json:"pas_verifier_maj,omitempty"`
	// end of a pilot already announced to the user (project folder → STATE.json "maj" of that end)
	FinsVues map[string]string `json:"fins_vues,omitempty"`
	// remote access: on or off, its local port, the trusted devices (distance.go; hashes of keys only)
	Distance DistanceConfig `json:"distance"`
	// the shortcuts to the installed Athanor made from the window (raccourci.go)
	Raccourcis []string `json:"raccourcis,omitempty"`
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

// configNeuve: no configuration file yet (a first launch): native Windows is then the default mode, since
// it needs nothing installed, unless this kit already has its WSL installation (nouveauServeur).
var configNeuve bool

func lireConfig() Config {
	c := configDefaut()
	if b, err := os.ReadFile(cheminConfig()); err == nil {
		_ = json.Unmarshal(b, &c)
	} else if os.IsNotExist(err) {
		configNeuve = true
	}
	// folders saved by older versions with a final dot or space ("montre3."): Windows drops it, Linux keeps
	// it, so the pilot would have worked in another folder than the one shown in Explorer
	for i, p := range c.Projets {
		c.Projets[i].Dossier = cheminWindowsPropre(p.Dossier)
	}
	return c
}

// cheminWindowsPropre removes the final dots and spaces of each folder name of a Windows path, as
// Windows itself does ("C:\Dev\site." is "C:\Dev\site"). Linux and WSL keep them, and would see
// another folder. Other paths are returned as they are.
func cheminWindowsPropre(d string) string {
	if len(d) < 3 || d[1] != ':' || (d[2] != '\\' && d[2] != '/') {
		return d
	}
	parties := strings.FieldsFunc(d[3:], func(r rune) bool { return r == '\\' || r == '/' })
	for i, x := range parties {
		if y := strings.TrimRight(x, ". "); y != "" {
			parties[i] = y
		}
	}
	return d[:2] + `\` + strings.Join(parties, `\`)
}

func ecrireConfig(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// written aside then renamed: a crash never leaves half a file (the project list would be lost)
	f := cheminConfig()
	t, err := os.CreateTemp(filepath.Dir(f), ".athanor-config-*.tmp")
	if err != nil {
		return err
	}
	_ = t.Chmod(0o600)
	_, err = t.Write(b)
	if e := t.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(t.Name(), f)
	}
	if err != nil {
		os.Remove(t.Name())
	}
	return err
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
