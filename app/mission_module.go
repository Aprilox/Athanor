package main

// The Missions module: a mission links a Studio project, a server and an API key (mission_nouvelle.go); the page
// #missions shows one frame per mission, #nouvelle-mission the form that launches or changes one. The API is in
// mission_api.go and mission_nouvelle.go.

func init() { enregistrerModule(moduleMissions{}) }

type moduleMissions struct{}

func (moduleMissions) ID() string  { return "missions" }
func (moduleMissions) Nom() string { return "Missions" }
func (moduleMissions) Pages() []PageModule {
	return []PageModule{{Hash: "#missions", Ordre: 2, Icone: "missions", Libelle: "Missions", Fils: []string{"#mission", "#nouvelle-mission"}}}
}

// Elements: a mission's project already shows in the hub's elements (Studio); nothing twice.
func (moduleMissions) Elements(*Serveur) []ElementHub { return nil }

func (moduleMissions) Tutoriel() []EtapeTutoriel { return nil }

// Resume: the missions whose pilot works now; none at all = no tile.
func (moduleMissions) Resume(s *Serveur) ResumeModule {
	l := lireMissions()
	if len(l) == 0 {
		return ResumeModule{Etat: "inactif"}
	}
	n := 0
	for _, m := range l {
		if p, ok := s.projetParNom(m.Projet); ok && m.Projet != "" && p.PiloteActif() {
			n++
		}
	}
	etat := "inactif"
	if n > 0 {
		etat = "ok"
	}
	return ResumeModule{Stats: []StatModule{{Cle: "Missions in progress", Valeur: n, Page: "#missions"}}, Etat: etat}
}
