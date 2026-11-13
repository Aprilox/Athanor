package main

// The Missions API (reading for now; creating, editing and answering come with M155). A paired device reads it too.

import "net/http"

// apiMissions: GET /api/missions, every mission, the newest first.
func (s *Serveur) apiMissions(w http.ResponseWriter, _ *http.Request) {
	s.ecrireJSON(w, map[string]any{"missions": lireMissions()})
}

// apiMission: GET /api/missions/{id}; unknown, damaged or hostile id → 404.
func (s *Serveur) apiMission(w http.ResponseWriter, r *http.Request) {
	m, err := lireMission(r.PathValue("id"))
	if err != nil {
		s.erreur(w, http.StatusNotFound, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"mission": m})
}
