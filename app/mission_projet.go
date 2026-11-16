package main

// A new project's settings on its mission card (M254): its folder, its working modes and time limits, its
// GitHub repository; set by the owner before Launch, applied by the launch right after the project's creation.

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// apiProjetMission: POST /api/missions/{id}/projet {dossier, modes, github, depot}: the new project's settings
// of a proposed card, all replaced; the folder is checked at once (full path, not a system folder nor another
// project's, empty or creatable) and chosen on the PC only.
func (s *Serveur) apiProjetMission(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Dossier string      `json:"dossier"`
		Modes   *ModesCarte `json:"modes"`
		GitHub  string      `json:"github"`
		Depot   string      `json:"depot"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id := r.PathValue("id")
	m, err := lireMission(id)
	var avant CarteMission
	if err == nil && json.Unmarshal(m.Carte, &avant) != nil {
		err = erreurMission{http.StatusConflict, "This mission has no card yet"}
	}
	if err == nil && !avant.Projet.Nouveau {
		err = erreurEntree("The project's settings are chosen for a new project only")
	}
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	dossier := strings.TrimSpace(c.Dossier)
	if appareilDe(r) != "" && dossier != avant.Projet.Dossier {
		s.erreur(w, http.StatusForbidden, "The environments and the project of a card are changed on the PC")
		return
	}
	if dossier != "" {
		if err := s.controlerDossierMission(avant.Projet.Nom, dossier); err != nil {
			s.erreurMissionHTTP(w, err)
			return
		}
		dossier = cheminWindowsPropre(filepath.Clean(dossier))
	}
	depot, err := depotMission(c.GitHub, c.Depot, avant.Projet.Nom)
	if err == nil {
		err = validerModesCarte(c.Modes)
	}
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	m, _, err = s.changerCarte(id, "project settings changed", func(_ *Mission, carte *CarteMission, _ contexteCarte) error {
		if !carte.Projet.Nouveau || !strings.EqualFold(carte.Projet.Nom, avant.Projet.Nom) { // changed by the agent meanwhile
			return errCarteChangee
		}
		carte.Projet.Dossier, carte.Projet.GitHub, carte.Projet.Depot, carte.Projet.Proprietaire = dossier, c.GitHub, depot, true
		carte.Dev.Modes = c.Modes
		return nil
	})
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + id, Mission: id,
		Action: "carte de mission modifiée", Resultat: "project settings: " + cmp.Or(dossier, "default folder") + ", GitHub " + cmp.Or(c.GitHub, "none")})
	s.ecrireJSON(w, map[string]any{"mission": m})
}

// depotMission: the card's repository for github ("" none, lier OWNER/NAME or its address, creer a name, the
// project's by default).
func depotMission(mode, depot, nom string) (string, error) {
	depot = strings.TrimSpace(depot)
	switch mode {
	case "":
		return "", nil
	case "lier":
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

// githubMission: the new project linked to its repository at the launch, with this computer's connection as
// Manage › GitHub does (auto push on): an existing OWNER/NAME, or a private repository created with the access
// kept here. A project already linked is left as it is: a launch resumed never creates a second repository.
// Replaced by the tests (a fake GitHub).
var githubMission = func(s *Serveur, p Projet, mode, depot string) error {
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
		out, _ := s.executerGitHub(p, 15*time.Minute, "lier-compte", depot, "1", "", "0", "0", "0")
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
	out, err := s.executerGitHub(p, 15*time.Minute, "lier", "creer", depot, "1", "", "0")
	if gh, _ := s.etatGitHub(p); err != nil || !gh.Lie {
		return errors.New(firstNonEmpty(derniereLigneTexte(out), "GitHub: unreadable answer from WSL"))
	}
	return nil
}
