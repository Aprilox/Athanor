package main

// A new project's settings on its mission card (M254): its folder, its working modes and time limits, its
// GitHub repository; set by the owner before Launch, applied by the launch right after the project's creation.

import (
	"cmp"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
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
		Auto    *bool       `json:"auto"` // nil: on, as in Manage
		Atelier bool        `json:"atelier"`
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
	depot, err := depotProjet(c.GitHub, c.Depot, avant.Projet.Nom)
	if err == nil {
		err = validerModesCarte(c.Modes)
	}
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	m, _, err = s.changerCarte(id, deQui(r), "project settings changed", func(_ *Mission, carte *CarteMission, _ contexteCarte) error {
		if !carte.Projet.Nouveau || !strings.EqualFold(carte.Projet.Nom, avant.Projet.Nom) { // changed by the agent meanwhile
			return errCarteChangee
		}
		carte.Projet.Dossier, carte.Projet.GitHub, carte.Projet.Depot, carte.Projet.Proprietaire = dossier, c.GitHub, depot, true
		carte.Projet.SansPousse, carte.Projet.Atelier = c.Auto != nil && !*c.Auto, c.Atelier && c.GitHub != ""
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
