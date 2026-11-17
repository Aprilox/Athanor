package main

// The Servers module: its menu page and home tile (the inventory and its API are in serveurs_inventaire.go).
// It declares no bus action yet: a deployment asked by a pilot gets a clear refusal until stage 6.

import "strings"

func init() { enregistrerModule(moduleServeurs{}) }

type moduleServeurs struct{}

func (moduleServeurs) ID() string  { return "servers" }
func (moduleServeurs) Nom() string { return "Servers" }
func (moduleServeurs) Pages() []PageModule {
	return []PageModule{{Hash: "#serveurs", Ordre: 30, Icone: "serveurs", Libelle: "Servers", Fils: []string{"#serveur", "#terminal", "#approbations"}}}
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
		// the last task's external verification failed (M98): the bell until the owner ignores it or a task passes
		if t := tachesServeur(x.ID); len(t) > 0 && t[0].Etat == "echec" && t[0].Verification != nil && t[0].Verification.Verdict != "ok" {
			out = append(out, ElementHub{ID: x.ID + ":verification", Titre: x.Nom, Etat: "erreur", Lien: "#serveur/" + x.ID,
				Attente: &AttenteElement{Raison: "The external check of a task on {0} failed: {1}", Args: []any{x.Nom, strings.Join(t[0].Verification.Lignes, " · ")}, Page: "#serveur/" + x.ID, Cle: t[0].ID}})
		}
		for _, r := range retoursNonVerifiesDe(x.ID) { // C49: a rollback the server could not be seen doing
			out = append(out, ElementHub{ID: x.ID + ":retour:" + r.ID, Titre: x.Nom, Etat: "erreur", Lien: "#serveur/" + x.ID,
				Attente: &AttenteElement{Raison: "The automatic rollback of {0} on {1} is not verified ({2}): check it with {3}", Args: []any{r.Quoi, x.Nom, r.Pourquoi, r.Verifier}, Page: "#serveur/" + x.ID, Cle: r.ID}})
		}
	}
	return out
}

// Tutoriel: the Servers page's 4 steps, then the Vault for the Cloudflare API (a missing card: the bubble in the middle).
func (moduleServeurs) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "Your servers", Texte: "Add your VPS or your home server: Athanor checks its fingerprint first, then connects with a key kept in the vault. That key is never shown, not even to the agents.", Cible: "[data-srv-ajouter]"},
		{Titre: "One card per server", Texte: "Each card says whether the server answers and what runs on it. Click its name to open its sheet: its environments, Harden this server and its tasks.", Cible: ".carte-serveur"},
		{Titre: "Tasks for the Servers agent", Texte: "On a server's sheet, Tasks: say what should be done (“Install nginx and serve my site over HTTPS”). The agent shows a plan first: nothing changes before you accept it, and prod always waits for you."},
		{Titre: "Health of the deployed sites", Texte: "Every 15 minutes, this PC checks your deployed sites and their servers. A problem rings the bell and your phone, with Retry, Go back to the previous version and I take over.", Cible: ".srv-reglage-sante"},
		{Titre: "Cloudflare, for DNS and HTTPS", Texte: "Your Cloudflare token goes into the Vault, as any API: Test shows its zones and what it may do there. The Servers agent then sets the DNS names and HTTPS of an environment, and of that name only. This tutorial stays in the “?”, with the guides “Add a server” and “Connect Cloudflare (DNS and HTTPS)”.", Cible: "[data-cof-ajouter]", Page: "#coffre"},
	}
}

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
