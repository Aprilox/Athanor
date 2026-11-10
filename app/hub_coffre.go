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
func (moduleCoffre) Tutoriel() []EtapeTutoriel { return nil }
