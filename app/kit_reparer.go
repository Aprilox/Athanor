package main

// The downloaded file opened again, at the version Athanor's folder already has: the folder is checked
// against the files the executable carries, and what differs is put back (a damaged or deleted file, an
// executable rebuilt or broken), never more. A newer folder is never touched by an older file (embarque.go
// runs the folder's own copy then), and a newer file updates it as always.
//
// What the downloaded file did is said to the application it opens (ATHANOR_LANCEMENT…): the window then
// offers a shortcut to the installed Athanor, says that an older file changed nothing, or what it repaired.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// reparerKit: the folder d, at this executable's version, made identical to what this executable carries:
// the files that differ (template/, the licence, the settings model, app/ when it is there) and the
// executables. Their names; nothing when everything matched. Never in a copy of the source code.
func reparerKit(d string) ([]string, error) {
	if !kitEmbarque() || sourceDuCode(d) || versionKit(d) != versionAthanor {
		return nil, nil
	}
	fichiers, modes, err := fichiersArchive(archiveKitGz)
	if err != nil {
		return nil, err
	}
	var repares []string
	var premiere error
	noter := func(err error) {
		if err != nil && premiere == nil {
			premiere = err
		}
	}
	identique := func(chemin string, b []byte) bool {
		x, err := os.ReadFile(chemin)
		return err == nil && bytes.Equal(x, b)
	}
	parDossier := map[string]map[string][]byte{}
	differe := map[string]bool{}
	for nom, b := range fichiers {
		haut, reste, ok := strings.Cut(nom, "/")
		switch {
		case ok && (haut == "template" || haut == "app"):
			if haut == "app" {
				if _, err := os.Stat(filepath.Join(d, "app", "go.mod")); err != nil {
					continue // the sources: only where they already are
				}
			}
			if parDossier[haut] == nil {
				parDossier[haut] = map[string][]byte{}
			}
			parDossier[haut][reste] = b
			if !identique(filepath.Join(d, haut, filepath.FromSlash(reste)), b) {
				differe[haut] = true
				repares = append(repares, nom)
			}
		case !ok && nom == "env.example":
			if !identique(filepath.Join(d, ".env.example"), b) {
				noter(ecrireAtomique(filepath.Join(d, ".env.example"), b, 0o644))
				repares = append(repares, ".env.example")
			}
		case !ok && nom == "LICENSE":
			if !identique(filepath.Join(d, "LICENSE"), b) {
				noter(ecrireAtomique(filepath.Join(d, "LICENSE"), b, 0o644))
				repares = append(repares, "LICENSE")
			}
		}
	}
	for haut := range differe { // the folder written aside then put in place (never half replaced)
		m := map[string]os.FileMode{}
		for rel := range parDossier[haut] {
			m[rel] = modes[haut+"/"+rel]
		}
		noter(remplacerDossier(filepath.Join(d, haut), parDossier[haut], m))
	}
	// the executable: this one, when the folder's differs (a broken one, or another build)
	if exe, err := os.Executable(); err == nil {
		cible := exeDuKit(d)
		if !memeFichier(exe, cible) && !memeContenu(exe, cible) {
			b, err := os.ReadFile(exe)
			if err == nil {
				err = ecrireAtomique(cible, b, 0o755)
			}
			noter(err)
			if err == nil {
				repares = append(repares, filepath.Base(cible))
			}
		}
	}
	// Windows: the Linux executable of the WSL mode (never under a running pilot)
	if len(athanorLinuxGz) > 0 && len(pilotesEnCours(d)) == 0 {
		if z, err := gzip.NewReader(bytes.NewReader(athanorLinuxGz)); err == nil {
			if b, err := io.ReadAll(z); err == nil && len(b) > 0 && !identique(filepath.Join(d, "athanor"), b) {
				err = ecrireAtomique(filepath.Join(d, "athanor"), b, 0o755)
				noter(err)
				if err == nil {
					repares = append(repares, "athanor (WSL)")
				}
			}
		}
	}
	sort.Strings(repares)
	return repares, premiere
}

// memeContenu: two files with the same bytes (compared by their sha256: executables are large).
func memeContenu(a, b string) bool {
	h := func(f string) []byte {
		x, err := os.Open(f)
		if err != nil {
			return nil
		}
		defer x.Close()
		s := sha256.New()
		if _, err := io.Copy(s, x); err != nil {
			return nil
		}
		return s.Sum(nil)
	}
	ha, hb := h(a), h(b)
	return ha != nil && hb != nil && bytes.Equal(ha, hb)
}

// InfoLancement: what the downloaded file did before opening Athanor (the window says it).
type InfoLancement struct {
	Telechargement bool     `json:"telechargement"`       // opened from the downloaded file, not from Athanor's folder
	Fichier        string   `json:"fichier,omitempty"`    // that file
	Ancien         string   `json:"ancien,omitempty"`     // its version, older than the folder's: nothing replaced
	Repares        []string `json:"repares,omitempty"`    // same version: what it put back
	MisAJour       string   `json:"mis_a_jour,omitempty"` // the version it updated the folder from
}

// infoLancement: read once, from what lancerDepuisTelechargement left in the environment.
func infoLancement() *InfoLancement {
	if os.Getenv("ATHANOR_LANCEMENT") != "telechargement" {
		return nil
	}
	i := &InfoLancement{Telechargement: true, Fichier: os.Getenv("ATHANOR_LANCEMENT_FICHIER"), Ancien: os.Getenv("ATHANOR_LANCEMENT_ANCIEN"),
		MisAJour: os.Getenv("ATHANOR_LANCEMENT_MAJ")}
	if r := os.Getenv("ATHANOR_LANCEMENT_REPARES"); r != "" {
		i.Repares = strings.Split(r, "\n")
	}
	return i
}

// noterLancement: what the downloaded file did, for the application it opens.
func noterLancement(fichier, ancien, majDepuis string, repares []string) {
	_ = os.Setenv("ATHANOR_LANCEMENT", "telechargement")
	_ = os.Setenv("ATHANOR_LANCEMENT_FICHIER", fichier)
	_ = os.Setenv("ATHANOR_LANCEMENT_ANCIEN", ancien)
	_ = os.Setenv("ATHANOR_LANCEMENT_MAJ", majDepuis)
	_ = os.Setenv("ATHANOR_LANCEMENT_REPARES", strings.Join(repares, "\n"))
}
