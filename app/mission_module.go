package main

// The Missions module (M300): missions are no longer created; the ones already on disk are read at #missions
// ("Old missions"). No mission file: no menu entry and no home tile. The API is in mission_api.go.

func init() { enregistrerModule(moduleMissions{}) }

type moduleMissions struct{}

// nbMissions: the missions the page lists (a damaged file is not one: it would keep an empty module).
func nbMissions() int { return len(lireMissions()) }

func (moduleMissions) ID() string  { return "missions" }
func (moduleMissions) Nom() string { return "Old missions" }
func (moduleMissions) Pages() []PageModule {
	if nbMissions() == 0 {
		return nil
	}
	return []PageModule{{Hash: "#missions", Ordre: 10, Icone: "missions", Libelle: "Old missions", Fils: []string{"#mission"}}}
}

// Elements: a mission whose orchestrator asks the owner a question is in the bell until it is answered, as are a
// mission that waits for the vault's infra key and a launch that stopped.
func (moduleMissions) Elements(s *Serveur) []ElementHub {
	var out []ElementHub
	for _, m := range lireMissions() {
		if q, ok := questionOuverte(m); ok && preparable(Mission{Etat: m.Etat}) == nil {
			choix := []ActionAlerte{} // M158: answered from the bell (a paired device too), the session resumed with it
			for _, c := range q.Choix {
				choix = append(choix, ActionAlerte{ID: "repondre", Libelle: c, Route: "missions/" + m.ID + "/repondre",
					Corps: map[string]any{"question": q.ID, "choix": c}, Distance: true})
			}
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "actif", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The orchestrator asks: {0}", Args: []any{q.Texte}, Page: "#missions", Cle: q.ID, Actions: choix}})
		}
		if missionSuivie(m.Etat) && s.attendCoffre(m) { // I3: after a restart, until the infra key opens
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "actif", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The mission waits for the vault to be unlocked", Page: "#missions"}})
		}
		// a launch stopped by a failed step (mission_lancer.go): its reason, until the owner dismisses it
		if e := raisonEchec(m); echecLancement(m) {
			var raison any = e.Detail
			if e.Message != nil { // B87: said in the page's language
				raison = e.Message
			}
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "termine", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The launch of the mission stopped: {0}", Args: []any{raison}, Page: "#missions"}})
		}
	}
	return out
}

func (moduleMissions) Tutoriel() []EtapeTutoriel { return nil }

// Resume: how many old missions there are; none = no tile.
func (moduleMissions) Resume(*Serveur) ResumeModule {
	n := nbMissions()
	if n == 0 {
		return ResumeModule{Etat: "inactif"}
	}
	return ResumeModule{Stats: []StatModule{{Cle: "Old missions", Valeur: n, Page: "#missions"}}, Etat: "inactif"}
}
