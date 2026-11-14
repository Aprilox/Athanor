package main

// The Missions module: its menu page (between Projects and Studio) and home tile; the API is in mission_api.go.

func init() { enregistrerModule(moduleMissions{}) }

type moduleMissions struct{}

func (moduleMissions) ID() string  { return "missions" }
func (moduleMissions) Nom() string { return "Missions" }
func (moduleMissions) Pages() []PageModule {
	return []PageModule{{Hash: "#missions", Ordre: 10, Icone: "missions", Libelle: "Missions", Fils: []string{"#mission"}}}
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
		if h := m.Historique; m.Etat == "echec" && len(h) > 1 && h[len(h)-2].Etat == "lancee" {
			var raison any = h[len(h)-1].Detail
			if h[len(h)-1].Message != nil { // B87: said in the page's language
				raison = h[len(h)-1].Message
			}
			out = append(out, ElementHub{ID: m.ID, Titre: titreMission(m), Etat: "termine", Lien: "#missions",
				Attente: &AttenteElement{Raison: "The launch of the mission stopped: {0}", Args: []any{raison}, Page: "#missions"}})
		}
	}
	return out
}

// Tutoriel: the Missions page's 5 steps (no mission yet: the bubble in the middle for the lanes).
func (moduleMissions) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "One sentence is enough", Texte: "Say what you want in the bar of the home page, for example “Put @my-shop online at shop.example.com”. The orchestrator answers with a mission card.", Cible: ".missions .cof-tete"},
		{Titre: "The card first", Texte: "The card says what will be built and deployed, what will be touched and what will ask for your accord. Change it or refuse it: nothing starts before you press Launch.", Cible: ".mis-liste, .missions .vide"},
		{Titre: "Lanes", Texte: "A launched mission runs in lanes: the project's pilot builds, the Servers agent prepares the environments, then the DNS names and HTTPS. Click a mission to see each lane, its state and its links.", Cible: ".mis-liste"},
		{Titre: "Your accord", Texte: "A plan, a deployment to prod or the infra vault waits for you: the bell and your phone ask, and you answer from there. Pause and Stop are at the top of the mission's page.", Cible: "[data-notifs]"},
		{Titre: "Find it again", Texte: "This tutorial stays in the “?”, with the guide “Launch a mission”.", Cible: ".btn-guide"},
	}
}

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
