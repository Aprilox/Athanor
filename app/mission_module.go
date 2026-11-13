package main

// The Missions module: its menu page (between Projects and Studio) and home tile; the API is in mission_api.go.

func init() { enregistrerModule(moduleMissions{}) }

type moduleMissions struct{}

func (moduleMissions) ID() string  { return "missions" }
func (moduleMissions) Nom() string { return "Missions" }
func (moduleMissions) Pages() []PageModule {
	return []PageModule{{Hash: "#missions", Ordre: 10, Icone: "missions", Libelle: "Missions"}}
}

// Elements: a mission whose orchestrator asks the owner a question is in the bell until it is answered, as is a
// launch that stopped.
func (moduleMissions) Elements(*Serveur) []ElementHub {
	var out []ElementHub
	for _, m := range lireMissions() {
		if q, ok := questionOuverte(m); ok && preparable(Mission{Etat: m.Etat}) == nil {
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "actif", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The orchestrator asks: {0}", Args: []any{q.Texte}, Page: "#missions", Cle: q.ID}})
		}
		// a launch stopped by a failed step (mission_lancer.go): its reason, until the owner dismisses it
		if h := m.Historique; m.Etat == "echec" && len(h) > 1 && h[len(h)-2].Etat == "lancee" {
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "termine", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The launch of the mission stopped: {0}", Args: []any{h[len(h)-1].Detail}, Page: "#missions"}})
		}
	}
	return out
}
func (moduleMissions) Tutoriel() []EtapeTutoriel { return nil }

// Resume: missions in progress (being prepared or launched), to launch (proposed or accepted), done.
func (moduleMissions) Resume(*Serveur) ResumeModule {
	enCours, aLancer, finies := 0, 0, 0
	for _, m := range lireMissions() {
		switch m.Etat {
		case "preparation", "lancee":
			enCours++
		case "proposee", "acceptee":
			aLancer++
		case "terminee":
			finies++
		}
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "In progress", Valeur: enCours, Page: "#missions"},
		{Cle: "To launch", Valeur: aLancer, Page: "#missions"},
		{Cle: "Finished", Valeur: finies, Page: "#missions"},
	}, Etat: "inactif"}
	switch {
	case aLancer > 0:
		r.Etat = "attention"
	case enCours+finies > 0:
		r.Etat = "ok"
	}
	return r
}
