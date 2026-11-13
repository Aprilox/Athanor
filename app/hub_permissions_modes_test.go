package main

import (
	"strings"
	"testing"
)

// TestPermissionsModesHub (M174b, Athanor 2.52.0): the working modes (parallel, eco, fast) keep the rights of the
// mode route. A paired device: TestDistanceToutesRoutes. An agent: no hub tool reads or writes them (the owner's
// switches, in Manage). A mission: its card carries them for a new project only, written at the launch by
// Projet.ecrireMode (TestMissionLancer), never for an existing project nor on the Servers lane.
func TestPermissionsModesHub(t *testing.T) {
	for nom, o := range outilsHub {
		if strings.Contains(nom, "mode") || strings.Contains(o.Description, "FAST") || strings.Contains(o.Description, "working mode") {
			t.Errorf("the agents' tool %q touches the working modes", nom)
		}
	}
	x := contexteCarte{projets: []Projet{{Nom: "boutique", Dossier: t.TempDir()}}}
	modes := &ModesCarte{Rapide: true}
	cas := []struct {
		nom   string
		carte CarteMission
		err   string
	}{
		{"new project", CarteMission{Titre: "Site", Projet: ProjetCarte{Nom: "neuf", Nouveau: true}, Dev: BriefCarte{Brief: "un site", Modes: modes}, JalonsEstimes: 3}, ""},
		{"existing project", CarteMission{Titre: "Site", Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "un site", Modes: modes}, JalonsEstimes: 3}, "new project only"},
		{"Servers lane", CarteMission{Titre: "Site", Projet: ProjetCarte{Nom: "neuf", Nouveau: true}, Dev: BriefCarte{Brief: "un site"}, Serveurs: BriefCarte{Brief: "dns", Modes: modes}, JalonsEstimes: 3}, "Dev lane"},
	}
	for _, c := range cas {
		err := validerCarte(&c.carte, x)
		if (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: %v, want %q", c.nom, err, c.err)
		}
	}
	if n := nomsModes(ModesCarte{}); n != "usual" {
		t.Errorf("no mode: %q", n)
	}
	if n := nomsModes(ModesCarte{Parallele: true, Eco: true, Rapide: true}); n != "parallel, eco, fast" {
		t.Errorf("the three modes: %q", n)
	}
}
