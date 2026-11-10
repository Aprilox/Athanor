package main

// The Dev module: Athanor's projects and their pilots (the code that existed before the hub). It
// wraps that code, it does not rewrite it.

func init() { enregistrerModule(moduleDev{}) }

type moduleDev struct{}

func (moduleDev) ID() string  { return "dev" }
func (moduleDev) Nom() string { return "Dev" }

func (moduleDev) Pages() []PageModule {
	return []PageModule{
		{"#global", "All projects"}, {"#suivi", "Overview"}, {"#equipe", "Team"}, {"#demandes", "Requests"},
		{"#chat", "Chat"}, {"#pourvous", "For you"}, {"#journal", "Log"},
	}
}

func (moduleDev) Resume(s *Serveur) ResumeModule {
	enCours, besoins := 0, 0
	for _, p := range s.vuesProjets("") {
		if p.Archive {
			continue
		}
		if p.Actif {
			enCours++
		}
		if p.Besoin != "" {
			besoins++
		}
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "Projects running", Valeur: enCours, Page: "#global"},
		{Cle: "Projects that need you", Valeur: besoins, Page: "#global"},
	}, Besoin: besoins, Etat: "inactif"}
	switch {
	case besoins > 0:
		r.Etat = "attention"
	case enCours > 0:
		r.Etat = "ok"
	}
	return r
}

// besoinProjet: why a project needs the user ("" = it does not), the only place of this rule (the web
// interface reads the besoin field): tasks on the For you page, or a pilot waiting for them.
func besoinProjet(pv ProjetVue) string {
	if pv.Archive {
		return ""
	}
	if pv.PourVous > 0 {
		return "pour_vous"
	}
	if pv.Actif && (pv.Phase == "attente_vous" || pv.Phase == "connexion" || pv.Phase == "budget") {
		return pv.Phase
	}
	return ""
}
