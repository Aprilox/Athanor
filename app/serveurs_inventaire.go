package main

// The Servers module's inventory: serveurs.json in its data folder (0600, written atomically, never a
// secret value: a key is named by its vault entry id) and its API: list, one server with its sheet,
// rename or change roles, remove.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type ServeurInventaire struct {
	ID              string    `json:"id"`
	Nom             string    `json:"nom"`
	Hote            string    `json:"hote"`
	Port            int       `json:"port"`
	Utilisateur     string    `json:"utilisateur"`
	Cle             string    `json:"cle"`            // id of its vault entry (the key stays in the vault)
	EmpreinteHote   string    `json:"empreinte_hote"` // "SHA256:…", learnt at the addition, then required
	Roles           []string  `json:"roles"`          // "dev" | "prod"
	Cree            time.Time `json:"cree"`
	Vu              time.Time `json:"vu,omitzero"`
	Etat            string    `json:"etat"` // "ok" | "injoignable" | "inconnu"
	MotDePasseCoupe bool      `json:"mot_de_passe_coupe"`
}

type fichierServeurs struct {
	Version  int                 `json:"version"`
	Serveurs []ServeurInventaire `json:"serveurs"`
}

var (
	serveursMu         sync.Mutex // one writer of serveurs.json at a time (read, change, write)
	errServeursFichier = errors.New("The servers file cannot be read")
	errServeurInconnu  = errors.New("Unknown server")
	idServeurValide    = regexp.MustCompile(`^[0-9a-f]{1,32}$`) // ids are aleatoire()'s hex: never a path
	rolesServeur       = []string{"dev", "prod"}
)

func fichierInventaire() string { return filepath.Join(dossierModule("servers"), "serveurs.json") }

// lireServeurs: the inventory (empty when there is no file yet).
func lireServeurs() ([]ServeurInventaire, error) {
	b, err := os.ReadFile(fichierInventaire())
	if errors.Is(err, os.ErrNotExist) {
		return []ServeurInventaire{}, nil
	}
	var f fichierServeurs
	if err != nil || json.Unmarshal(b, &f) != nil || f.Version != 1 {
		return nil, errServeursFichier
	}
	if f.Serveurs == nil {
		f.Serveurs = []ServeurInventaire{}
	}
	return f.Serveurs, nil
}

// modifierServeurs: reads, changes and writes the inventory under one lock; nothing is written on error.
func modifierServeurs(f func([]ServeurInventaire) ([]ServeurInventaire, error)) error {
	serveursMu.Lock()
	defer serveursMu.Unlock()
	liste, err := lireServeurs()
	if err != nil {
		return err
	}
	if liste, err = f(liste); err != nil {
		return err
	}
	b, err := json.MarshalIndent(fichierServeurs{Version: 1, Serveurs: liste}, "", "  ")
	if err != nil {
		return err
	}
	return ecrireAtomique(fichierInventaire(), b, 0o600)
}

// serveursUtilisantCle: the names of the servers that reach their host with that vault entry.
func serveursUtilisantCle(id string) ([]string, error) {
	liste, err := lireServeurs()
	var noms []string
	for _, x := range liste {
		if x.Cle == id {
			noms = append(noms, x.Nom)
		}
	}
	return noms, err
}

// rolesValides: the roles sorted without duplicates, or the refusal of an unknown one.
func rolesValides(l []string) ([]string, error) {
	for _, x := range l {
		if !slices.Contains(rolesServeur, x) {
			return nil, erreurEntree("Unknown server role")
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(l))), nil
}

func (s *Serveur) erreurServeurs(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errServeurInconnu):
		s.erreur(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errServeursFichier):
		s.erreur(w, http.StatusInternalServerError, err.Error())
	default:
		s.erreur(w, http.StatusBadRequest, err.Error())
	}
}

// apiServeurs: GET /api/serveurs, the inventory.
func (s *Serveur) apiServeurs(w http.ResponseWriter, _ *http.Request) {
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"serveurs": liste})
}

// apiServeur: GET /api/serveurs/{id}, one server and its sheet (null until the sheet is made).
func (s *Serveur) apiServeur(w http.ResponseWriter, r *http.Request) {
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == r.PathValue("id") })
	if i < 0 || !idServeurValide.MatchString(liste[i].ID) {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	var fiche json.RawMessage
	if b, err := os.ReadFile(filepath.Join(dossierModule("servers"), liste[i].ID, "fiche.json")); err == nil && json.Valid(b) {
		fiche = b
	}
	s.ecrireJSON(w, map[string]any{"serveur": liste[i], "fiche": fiche})
}

// apiServeurModifier: POST /api/serveurs/{id} {nom?, roles?}: renames it or sets its roles.
func (s *Serveur) apiServeurModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom   *string   `json:"nom"`
		Roles *[]string `json:"roles"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if c.Nom != nil {
		n := strings.TrimSpace(*c.Nom)
		switch {
		case n == "":
			s.erreur(w, http.StatusBadRequest, "Give the server a name")
			return
		case utf8.RuneCountInString(n) > 100:
			s.erreur(w, http.StatusBadRequest, "The name is too long (100 characters at most)")
			return
		}
		c.Nom = &n
	}
	if c.Roles != nil {
		roles, err := rolesValides(*c.Roles)
		if err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		c.Roles = &roles
	}
	var rendu ServeurInventaire
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == r.PathValue("id") })
		if i < 0 {
			return nil, errServeurInconnu
		}
		if c.Nom != nil {
			l[i].Nom = *c.Nom
		}
		if c.Roles != nil {
			l[i].Roles = *c.Roles
		}
		rendu = l[i]
		return l, nil
	})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireJSON(w, map[string]any{"serveur": rendu})
}

// apiServeurSupprimer: DELETE /api/serveurs/{id}: out of the inventory, its sheet and files deleted; its
// vault key stays (the owner removes it from the vault, now that no server uses it).
func (s *Serveur) apiServeurSupprimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		n := len(l)
		if l = slices.DeleteFunc(l, func(x ServeurInventaire) bool { return x.ID == id }); len(l) == n || !idServeurValide.MatchString(id) {
			return nil, errServeurInconnu
		}
		return l, nil
	})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	_ = os.RemoveAll(filepath.Join(dossierModule("servers"), id))
	s.ecrireJSON(w, map[string]bool{"ok": true})
}
