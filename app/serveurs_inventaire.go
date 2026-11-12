package main

// The Servers module's inventory: serveurs.json in its data folder (0600, written atomically, never a
// secret value: a key is named by its vault entry id) and its API: list, one server with its sheet,
// rename or change roles, remove.

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Etat            string    `json:"etat"` // "ok" | "injoignable" | "empreinte_changee" | "inconnu"
	MotDePasseCoupe bool      `json:"mot_de_passe_coupe"`
}

type fichierServeurs struct {
	Version  int                 `json:"version"`
	Serveurs []ServeurInventaire `json:"serveurs"`
}

var (
	serveursMu         sync.Mutex // serveurs.json: one reader or writer at a time (read, change, write)
	errServeursFichier = errors.New("The servers file cannot be read")
	errServeurInconnu  = errors.New("Unknown server")
	idServeurValide    = regexp.MustCompile(`^[0-9a-f]{1,32}$`) // ids are aleatoire()'s hex: never a path
	rolesServeur       = []string{"dev", "prod"}
)

func fichierInventaire() string { return filepath.Join(dossierModule("servers"), "serveurs.json") }

// lireServeurs: the inventory (empty when there is no file yet).
func lireServeurs() ([]ServeurInventaire, error) {
	serveursMu.Lock() // on Windows, a write moves the file away for an instant: never read it then
	defer serveursMu.Unlock()
	return lireInventaire()
}

func lireInventaire() ([]ServeurInventaire, error) {
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
	for i := range f.Serveurs {
		if f.Serveurs[i].Roles == nil { // written null by an older version: always a list for the page
			f.Serveurs[i].Roles = []string{}
		}
	}
	return f.Serveurs, nil
}

// modifierServeurs: reads, changes and writes the inventory under one lock; nothing is written on error.
func modifierServeurs(f func([]ServeurInventaire) ([]ServeurInventaire, error)) error {
	serveursMu.Lock()
	defer serveursMu.Unlock()
	liste, err := lireInventaire()
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
	return append([]string{}, slices.Compact(slices.Sorted(slices.Values(l)))...), nil // never nil: [] for the page
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
	s.ecrireJSON(w, map[string]any{"serveurs": liste, "environnements": s.acces.UtilisationsServeurs()})
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
	runbook, _ := os.ReadFile(fichierRunbook(liste[i].ID)) // M96: read only here
	s.ecrireJSON(w, map[string]any{"serveur": liste[i], "fiche": fiche, "annulations": annulationsDe(liste[i].ID), "runbook": s.masquerSecrets(string(runbook)),
		"environnements": append([]string{}, s.acces.UtilisationsServeurs()[liste[i].ID]...)})
}

// apiServeurModifier: POST /api/serveurs/{id} {nom?, roles?}: renames it or sets its roles;
// {couper_mot_de_passe:true}: cuts password logins (serveurs_ajout.go); {rafraichir:true}: its sheet made
// again (serveurs_fiche.go).
func (s *Serveur) apiServeurModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom              *string   `json:"nom"`
		Roles            *[]string `json:"roles"`
		CouperMotDePasse bool      `json:"couper_mot_de_passe"`
		Rafraichir       bool      `json:"rafraichir"`
		MotDePasse       string    `json:"mot_de_passe"` // Athanor's password: taking the prod role away (C37)
		Garder           string    `json:"garder"`       // a firewall or SSH change kept (serveurs_annulation.go)
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if c.CouperMotDePasse {
		s.couperMotDePasse(w, r, r.PathValue("id"))
		return
	}
	if c.Rafraichir {
		s.apiServeurRafraichir(w, r, r.PathValue("id"))
		return
	}
	if c.Garder != "" {
		txt, err := s.confirmerChangement(r.Context(), "propriétaire", r.PathValue("id"), c.Garder)
		if err != nil {
			s.erreurServeurs(w, err)
			return
		}
		s.ecrireJSON(w, map[string]any{"ok": true, "message": txt})
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
		case strings.IndexFunc(n, caractereInvisible) >= 0:
			s.erreur(w, http.StatusBadRequest, errNomInvisible.Error())
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
		liste, err := lireServeurs()
		if err != nil {
			s.erreurServeurs(w, err)
			return
		}
		i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == r.PathValue("id") })
		if i >= 0 && slices.Contains(liste[i].Roles, "prod") && !slices.Contains(roles, "prod") && !s.preuveServeur(w, r, c.MotDePasse, "retrait du rôle prod refusé", liste[i].Nom) {
			return // C37: without prod an agent may act on it as on a test server
		}
		if i >= 0 && s.acces != nil {
			if l := s.acces.envsIncompatibles(liste[i].ID, roles); len(l) > 0 {
				s.erreurArgs(w, http.StatusConflict, "Take this server out of these environments first: {0}", strings.Join(l, ", "))
				return // a prod server in a dev environment would be deployed to without approval
			}
		}
	}
	var rendu, avant ServeurInventaire
	err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == r.PathValue("id") })
		if i < 0 {
			return nil, errServeurInconnu
		}
		avant = l[i]
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
	if !slices.Equal(avant.Roles, rendu.Roles) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "rôles du serveur", Resultat: fmt.Sprintf("%s: %v → %v", rendu.Nom, avant.Roles, rendu.Roles)})
	}
	s.ecrireJSON(w, map[string]any{"serveur": rendu})
}

// apiServeurSupprimer: DELETE /api/serveurs/{id}: out of the inventory, its sheet and files deleted; its
// vault key stays (the owner removes it from the vault, now that no server uses it).
func (s *Serveur) apiServeurSupprimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var c struct {
		MotDePasse string `json:"mot_de_passe"`
	}
	_ = lireCorps(r, &c) // no body: no password
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 || !idServeurValide.MatchString(id) {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	nom := liste[i].Nom
	if !s.preuveServeur(w, r, c.MotDePasse, "suppression du serveur refusée", nom) {
		return // C37: its sheet would go with no trace, prod servers included
	}
	err = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		n := len(l)
		if l = slices.DeleteFunc(l, func(x ServeurInventaire) bool { return x.ID == id }); len(l) == n {
			return nil, errServeurInconnu
		}
		return l, nil
	})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: "serveur supprimé", Resultat: nom})
	_ = os.RemoveAll(filepath.Join(dossierModule("servers"), id))
	envs := []string{}
	if s.acces != nil {
		l, err := s.acces.RetirerServeur(id)
		if err != nil {
			fmt.Fprintln(os.Stderr, "environments:", err)
		}
		if len(l) > 0 {
			envs = l
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "serveur retiré des environnements", Resultat: nom + ": " + strings.Join(l, ", ")})
		}
	}
	s.ecrireJSON(w, map[string]any{"ok": true, "environnements": envs})
}

// preuveServeur: the owner is there (hub_preuve.go, WSL mode too); otherwise the refusal is logged and written.
func (s *Serveur) preuveServeur(w http.ResponseWriter, r *http.Request, mdp, action, nom string) bool {
	code, err := s.preuvePresence(r, mdp)
	if err != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "servers", Action: action, Resultat: nom + ": " + err.Error()})
		s.erreur(w, code, err.Error())
	}
	return err == nil
}
