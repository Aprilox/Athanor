package main

// The Missions module: its menu page (between Projects and Studio) and home tile; the API is in mission_api.go.

func init() { enregistrerModule(moduleMissions{}) }

type moduleMissions struct{}

func (moduleMissions) ID() string  { return "missions" }
func (moduleMissions) Nom() string { return "Missions" }
func (moduleMissions) Pages() []PageModule {
	return []PageModule{{Hash: "#missions", Ordre: 10, Icone: "missions", Libelle: "Missions"}}
}
func (moduleMissions) Elements(*Serveur) []ElementHub { return nil } // the cards and questions join the bell later (M154, M155)
func (moduleMissions) Tutoriel() []EtapeTutoriel      { return nil }

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
