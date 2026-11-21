package main

// The Settings module: the pages about the whole application, declared like a module's (last in the
// menu, no home tile, nothing at work).

func init() { enregistrerModule(moduleReglages{}) }

type moduleReglages struct{}

func (moduleReglages) ID() string  { return "reglages" }
func (moduleReglages) Nom() string { return "Settings" }
func (moduleReglages) Pages() []PageModule {
	return []PageModule{
		{Hash: "#reglages", Ordre: 90, Icone: "reglages", Libelle: "Settings"}, {Hash: "#extensions", Ordre: 91, Icone: "extensions", Libelle: "Skills & MCP"},
		{Hash: "#sante", Ordre: 92, Icone: "sante", Libelle: "Setup & health"}, {Hash: "#distance", Ordre: 93, Icone: "distance", Libelle: "Remote"},
	}
}
func (moduleReglages) Resume(*Serveur) ResumeModule {
	return ResumeModule{Stats: []StatModule{}, Etat: "inactif"}
}
func (moduleReglages) Elements(*Serveur) []ElementHub { return nil }
func (moduleReglages) Tutoriel() []EtapeTutoriel      { return nil }
