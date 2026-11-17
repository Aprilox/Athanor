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

// Elements: an old mission is no longer followed, nothing of it rings.
func (moduleMissions) Elements(*Serveur) []ElementHub { return nil }

func (moduleMissions) Tutoriel() []EtapeTutoriel { return nil }

// Resume: how many old missions there are; none = no tile.
func (moduleMissions) Resume(*Serveur) ResumeModule {
	n := nbMissions()
	if n == 0 {
		return ResumeModule{Etat: "inactif"}
	}
	return ResumeModule{Stats: []StatModule{{Cle: "Old missions", Valeur: n, Page: "#missions"}}, Etat: "inactif"}
}
