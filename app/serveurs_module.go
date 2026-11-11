package main

// The Servers module: its menu page and home tile (the inventory and its API are in serveurs_inventaire.go).
// It declares no bus action yet: a deployment asked by a pilot gets a clear refusal until stage 6.

func init() { enregistrerModule(moduleServeurs{}) }

type moduleServeurs struct{}

func (moduleServeurs) ID() string  { return "servers" }
func (moduleServeurs) Nom() string { return "Servers" }
func (moduleServeurs) Pages() []PageModule {
	return []PageModule{{Hash: "#serveurs", Ordre: 30, Icone: "serveurs", Libelle: "Servers", Fils: []string{"#serveur"}}}
}
func (moduleServeurs) Elements(*Serveur) []ElementHub { return nil }
func (moduleServeurs) Tutoriel() []EtapeTutoriel      { return nil }

// Resume: how many servers, how many unreachable (each one needs the owner); a small file read.
func (moduleServeurs) Resume(*Serveur) ResumeModule {
	liste, _ := lireServeurs()
	injoignables := 0
	for _, x := range liste {
		if x.Etat == "injoignable" {
			injoignables++
		}
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "Servers", Valeur: len(liste), Page: "#serveurs"},
		{Cle: "Unreachable", Valeur: injoignables, Page: "#serveurs"},
	}, Besoin: injoignables, Etat: "inactif"}
	switch {
	case injoignables > 0:
		r.Etat = "attention"
	case len(liste) > 0:
		r.Etat = "ok"
	}
	return r
}
