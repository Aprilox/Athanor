package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// TestMissionStudioSeulTerminee (M275b): a mission of the Studio lane alone (no environment on its card) ends once
// its project's pilot finished the request; while the pilot works it stays launched.
func TestMissionStudioSeulTerminee(t *testing.T) {
	s := serveurCoffreTest(t)
	p := Projet{Nom: "carnet", Dossier: filepath.Join(t.TempDir(), "carnet")}
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [x] M1 : Le carnet\n", 0o644)
	ecrireFichier(t, p.EtatJSON(), `{"phase":"travail"}`, 0o644)
	s.config.Projets = append(s.config.Projets, p)
	b, _ := json.Marshal(CarteMission{Titre: "Carnet", Projet: ProjetCarte{Nom: p.Nom, Nouveau: true}, Dev: BriefCarte{Brief: "Un carnet"}, JalonsEstimes: 1})
	m := Mission{ID: "studioseul", Phrase: "un carnet", Etat: "lancee", Politique: "prod", Carte: b, Voies: []VoieMission{{Module: "dev", Ref: p.Nom, Etat: "lancee"}},
		Historique: []EvenementMission{evenementMission("lancee", "")}, Cree: time.Now().UTC().Format(formatCree)}
	if err := enregistrerMission(m); err != nil {
		t.Fatal(err)
	}
	s.observerMission(m.ID)
	if lu, _ := lireMission(m.ID); lu.Etat != "lancee" {
		t.Fatalf("ended while its pilot works: %s", lu.Etat)
	}
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644) // the pilot finished the request
	s.observerMission(m.ID)
	if lu, _ := lireMission(m.ID); lu.Etat != "terminee" || lu.Relais == nil || lu.Relais.Etape != "termine" {
		t.Fatalf("a Studio-only mission whose pilot finished: %s %+v", lu.Etat, lu.Relais)
	}
}
