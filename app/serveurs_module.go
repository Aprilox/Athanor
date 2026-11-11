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

// Elements: a server whose host fingerprint changed waits for the owner (the bell); the others need nothing.
func (moduleServeurs) Elements(*Serveur) []ElementHub {
	liste, _ := lireServeurs()
	var out []ElementHub
	for _, x := range liste {
		if x.Etat == "empreinte_changee" {
			out = append(out, ElementHub{ID: x.ID, Titre: x.Nom, Etat: "inactif", Lien: "#serveur/" + x.ID,
				Attente: &AttenteElement{Raison: "The host fingerprint of {0} changed: check the server before trusting it again", Args: []any{x.Nom}, Page: "#serveur/" + x.ID, Cle: x.EmpreinteHote}})
		}
	}
	return out
}
func (moduleServeurs) Tutoriel() []EtapeTutoriel { return nil }

// Resume: how many servers, how many unreachable (each one needs the owner); a small file read.
func (moduleServeurs) Resume(*Serveur) ResumeModule {
	liste, _ := lireServeurs()
	injoignables := 0
	for _, x := range liste {
		if x.Etat == "injoignable" || x.Etat == "empreinte_changee" {
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
