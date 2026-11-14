package main

// The vault module: its menu page and home tile (the API is in coffre_api.go).

func init() { enregistrerModule(moduleCoffre{}) }

type moduleCoffre struct{}

func (moduleCoffre) ID() string  { return "coffre" }
func (moduleCoffre) Nom() string { return "Vault" }
func (moduleCoffre) Pages() []PageModule {
	return []PageModule{{Hash: "#coffre", Ordre: 40, Icone: "coffre", Libelle: "Secrets and variables"}}
}
func (moduleCoffre) Elements(*Serveur) []ElementHub { return nil } // nothing at work: entries only
func (moduleCoffre) Resume(s *Serveur) ResumeModule {
	e := s.etatCoffre()
	secrets, variables := 0, 0
	if s.coffre != nil {
		for _, x := range s.coffre.Liste() {
			if x.Famille == "secret" {
				secrets++
			} else {
				variables++
			}
		}
	}
	statut := "Locked"
	switch {
	case e.Infra:
		statut = "Unlocked"
	case !e.MotDePasse:
		statut = "No password"
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "Secrets", Valeur: secrets, Page: "#coffre"},
		{Cle: "Variables", Valeur: variables, Page: "#coffre"},
		{Cle: "Vault status", Valeur: statut, Page: "#coffre"},
	}, Etat: "inactif"}
	if secrets+variables > 0 {
		r.Etat = "ok"
	}
	return r
}

// EtatCoffre: which keys are open, whether this computer has a keystore and Athanor a password.
type EtatCoffre struct {
	Code       bool `json:"code"`
	Infra      bool `json:"infra"`
	Systeme    bool `json:"systeme"`
	MotDePasse bool `json:"mot_de_passe"`
}

func (s *Serveur) etatCoffre() EtatCoffre {
	s.mu.Lock()
	e := EtatCoffre{MotDePasse: s.config.MotDePasseHash != ""}
	s.mu.Unlock()
	if s.coffre != nil {
		s.coffre.mu.Lock()
		e.Code, e.Infra, e.Systeme = s.coffre.cleCode != nil, s.coffre.cleInfra != nil, s.coffre.systeme != nil
		s.coffre.mu.Unlock()
	}
	return e
}

// Tutoriel: the vault and a project's Access panel (its first visit opens it too, see proposerTutoriel);
// exact about who sees what: infra values never reach the AI, granted code values do (as variables).
func (moduleCoffre) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "Your keys", Texte: "Add your keys and settings here. Secrets are stored encrypted: once saved, a secret's value is never shown again, you can only replace or delete it.", Cible: ".cof-tete"},
		{Titre: "Infra keys", Texte: "Infra keys (Cloudflare, SSH, your servers) open only with your password. Athanor keeps them and uses them itself when an action needs them: the AI never receives their value.", Cible: ".carte-coffre .badge-infra"},
		{Titre: "Code keys", Texte: "Code keys (a service's API key, for instance) reopen alone on this PC. A project's pilot gets one only if you grant it to that project: it then reads its value, as a variable. One key is enough, in testing as in production; turn on “Separate testing and production” only if the service gives you two keys.", Cible: ".acc-grille", Page: "#acces"},
		{Titre: "Before the start", Texte: "Next to Start, this badge says exactly what the pilot will receive, for the chosen environment.", Cible: ".acces-demarrer", Page: "#suivi"},
		{Titre: "Remove at any time", Texte: "Untick a box or press Remove: the value is taken out of the pilot's file at once; a pilot already working keeps what it has read until the end of its cycle. This tutorial stays in the “?”, at the top of the Guide.", Cible: ".acc-projet", Page: "#acces"},
	}
}
