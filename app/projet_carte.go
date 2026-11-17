package main

// A new project's settings as a card carries them (the home's project card, a mission's card): its folder, its
// working modes and time limits, its GitHub repository; checked when the owner sets them, applied right after the
// project's creation.

import (
	"cmp"
	"errors"
	"strings"
	"time"
)

type ProjetCarte struct {
	Nom     string `json:"nom"`
	Nouveau bool   `json:"nouveau"`
	Distant string `json:"distant,omitempty"` // worked out by the validation: "remote id/project" of a nom@instance
	// M254, a new project only, set by the owner (POST /projet, never the agent): its folder ("" = next to the
	// other projects), its GitHub repository (github: "" none, lier an existing OWNER/NAME, creer a private name)
	Dossier string `json:"dossier,omitempty"`
	GitHub  string `json:"github,omitempty"`
	Depot   string `json:"depot,omitempty"`
	// M264: the two boxes of Manage › GitHub (choixGitHub): automatic push off, workshop shared
	SansPousse   bool `json:"sans_pousse,omitempty"`
	Atelier      bool `json:"atelier,omitempty"`
	Proprietaire bool `json:"proprietaire,omitempty"` // the owner set them: a new card from the agent keeps them (and the modes)
}

// ModesCarte: the three independent switches of a new project, written at the launch by Projet.ecrireMode.
type ModesCarte struct {
	Parallele bool `json:"parallele"`
	Eco       bool `json:"eco"`
	Rapide    bool `json:"rapide"`
	// as POST /api/mode: workers at most (0: 3), framing time box in minutes, work time limit in hours (nil: Settings)
	ParalleleMax int  `json:"parallele_max,omitempty"`
	CadrageMax   *int `json:"cadrage_max,omitempty"`
	DureeMax     *int `json:"duree_max,omitempty"`
}

// depotProjet: the card's repository for github ("" none, lier OWNER/NAME or its address, creer a name, the
// project's by default).
func depotProjet(mode, depot, nom string) (string, error) {
	depot = strings.TrimSpace(depot)
	switch mode {
	case "":
		return "", nil
	case "lier":
		if depot == "" { // M264: the choice is kept while the repository is still to type; the launch refuses it
			return "", nil
		}
		depot = strings.TrimSuffix(strings.TrimPrefix(depot, "https://github.com/"), ".git")
		if !reDepotGitHub.MatchString(depot) {
			return "", erreurEntree("Invalid repository: write it as OWNER/NAME (e.g. octocat/my-site).")
		}
	case "creer":
		depot = cmp.Or(depot, nomDepotPropose(nom))
		if !reNomGitHub.MatchString(depot) || depot == "." || depot == ".." {
			return "", erreurEntree("Invalid repository name: letters, digits, dots, dashes and underscores only.")
		}
	default:
		return "", erreurEntree("GitHub: choose none, link or create")
	}
	return depot, nil
}

// validerModesCarte: the bounds Projet.ecrireMode applies, checked when the owner sets them (not at the launch).
func validerModesCarte(m *ModesCarte) error {
	switch {
	case m == nil:
		return nil
	case m.ParalleleMax != 0 && (m.ParalleleMax < 2 || m.ParalleleMax > 10):
		return erreurEntree("Workers at most: between 2 and 10")
	case m.CadrageMax != nil && *m.CadrageMax != 0 && (*m.CadrageMax < 10 || *m.CadrageMax > 240):
		return erreurEntree("The framing time box is between 10 and 240 minutes.")
	case m.DureeMax != nil && (*m.DureeMax < -1 || *m.DureeMax > 87600):
		return erreurEntree("The work time limit is between 1 and 87600 hours, or unlimited.")
	}
	return nil
}

// githubProjet: the new project linked to its repository at the launch, with this computer's connection as
// Manage › GitHub does (auto push on): an existing OWNER/NAME, or a private repository created with the access
// kept here. A project already linked is left as it is: a launch resumed never creates a second repository.
// Replaced by the tests (a fake GitHub).
var githubProjet = func(s *Serveur, p Projet, pc ProjetCarte) error {
	mode, depot, auto, atelier := pc.GitHub, pc.Depot, map[bool]string{true: "0", false: "1"}[pc.SansPousse], map[bool]string{true: "1", false: "0"}[pc.Atelier]
	v, err := s.etatGitHub(p)
	switch {
	case err != nil:
		return err
	case v.Lie:
		return nil
	case !v.ConnexionPC.Connecte:
		return errors.New("This computer is not connected to GitHub: Settings › Options › GitHub.")
	}
	if mode == "lier" {
		out, _ := s.executerGitHub(p, 15*time.Minute, "lier-compte", depot, auto, "", atelier, "0", "0")
		var res ResultatLierCompte
		if lireJSONFinal(out, &res) != nil {
			return errors.New(raisonPoussee(out))
		}
		if res.Resultat != "lie" {
			return errors.New(cmp.Or(res.Detail, res.Resultat))
		}
		return nil
	}
	if !v.ConnexionPC.AccesGarde { // no sign-in window in a launch
		return errors.New("Creating a repository from a mission needs the access kept on this computer: Settings › Options › GitHub.")
	}
	out, err := s.executerGitHub(p, 15*time.Minute, "lier", "creer", depot, auto, "", atelier)
	if gh, _ := s.etatGitHub(p); err != nil || !gh.Lie {
		return errors.New(firstNonEmpty(derniereLigneTexte(out), "GitHub: unreadable answer from WSL"))
	}
	return nil
}
