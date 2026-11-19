package main

// The Dev module: Athanor's projects and their pilots (the code that existed before the hub). It
// wraps that code, it does not rewrite it.

import (
	"math"
	"strings"
	"time"
)

type moduleDev struct{}

func (moduleDev) ID() string          { return "dev" }
func (moduleDev) Nom() string         { return "Studio" }
func (moduleDev) ProjetCourant() bool { return true }

// Pages: the pages of the selected project (Projects, every project at once, is the hub's).
func (moduleDev) Pages() []PageModule {
	return []PageModule{
		{Hash: "#suivi", Ordre: 20, Icone: "suivi", Libelle: "Overview"}, {Hash: "#equipe", Ordre: 21, Icone: "equipe", Libelle: "Team"},
		{Hash: "#demandes", Ordre: 22, Icone: "demandes", Libelle: "Requests"}, {Hash: "#chat", Ordre: 23, Icone: "chat", Libelle: "Chat"},
		{Hash: "#pourvous", Ordre: 24, Icone: "pourvous", Libelle: "For you"}, {Hash: "#acces", Ordre: 25, Icone: "acces", Libelle: "Access", Tour: "coffre"},
		{Hash: "#deploiement", Ordre: 26, Icone: "deploiement", Libelle: "Deployment"}, {Hash: "#journal", Ordre: 27, Icone: "journal", Libelle: "Log"},
		{Hash: "#projet", Ordre: 28, Icone: "projet", Libelle: "Manage"},
	}
}

// Elements: one per project not archived, its progress, its milestone in progress, what its pilot
// does now (the team view of a log already open, never a file read for it) and why it needs the owner.
func (moduleDev) Elements(s *Serveur) []ElementHub {
	var out []ElementHub
	for _, pv := range s.vuesProjets("") {
		if pv.Archive {
			continue
		}
		e := ElementHub{ID: pv.Nom, Titre: pv.Nom, Etat: "inactif", Lien: "#suivi", Projet: pv.Nom, Arret: pv.Arret}
		e.Avancement = &Avancement{Texte: "No plan yet"}
		if pv.Jalons > 0 {
			e.Avancement = &Avancement{Fait: pv.Faits, Total: pv.Jalons, Texte: "{0} / {1} milestones done", Etape: "Milestone {0}/{1}", EtapePlan: pv.Etape, EtapesPlan: pv.Etapes}
		}
		switch {
		case pv.Actif:
			e.Etat, e.Activite = "actif", firstNonEmpty(pv.JalonTitre, pv.Jalon)
			s.mu.Lock()
			j := s.logs[pv.Nom]
			s.mu.Unlock()
			if j != nil {
				for _, a := range j.Agents() {
					if a.Genre == "chef" && a.Statut != "termine" {
						e.Action = a.Activite
					}
				}
			}
		case pv.Phase == "termine" || pv.Jalons > 0 && pv.Faits == pv.Jalons:
			e.Etat = "termine"
		}
		e.Attente = attenteDev(pv)
		if !pv.Existe { // its folder is gone (moved, deleted, a drive unplugged): find it again or remove it
			e.Avancement = &Avancement{Texte: "Folder not found"}
			e.Attente = &AttenteElement{Raison: "The folder {0} is no longer there: on Manage, point the project to where it is now (Use another folder) or remove it from the list.", Args: []any{pv.Dossier}, Page: "#projet", Cle: pv.Dossier}
		}
		if g, ok := s.signalProjet(pv.Nom); ok && e.Attente == nil {
			e.Attente = &AttenteElement{Raison: "The pilot reports a blocker: {0}", Args: []any{g.Texte}, Page: "#suivi", Cle: g.ID}
		}
		if r := s.etatRelais(pv.Nom); r != nil && r.Etat == "echec" && e.Attente == nil {
			e.Attente = &AttenteElement{Raison: "The project's deployment failed ({0}): {1}", Args: []any{r.Jalon, messageOuTexte(r.Message, r.Erreur)}, Page: "#deploiement", Cle: "relais:" + r.Commit}
		}
		out = append(out, e)
	}
	return append(out, s.elementsDevDistants()...)
}

// attenteDev: what a project waits for from the owner (besoinProjet), with the bell's texts.
func attenteDev(pv ProjetVue) *AttenteElement {
	switch pv.Besoin {
	case "pour_vous":
		raison := "{0} tasks on the For you page"
		if pv.PourVous == 1 {
			raison = "1 task on the For you page"
		}
		return &AttenteElement{Raison: raison, Args: []any{pv.PourVous}, Page: "#pourvous", Cle: strings.Join(pv.idsPourVous, ",")}
	case "attente_vous":
		return &AttenteElement{Raison: "Claude is waiting for you", Page: "#pourvous"}
	case "connexion":
		return &AttenteElement{Raison: "Connection problem", Page: "#suivi"}
	case "budget":
		return &AttenteElement{Raison: "API budget or credit used up, Claude is waiting", Page: "#suivi"}
	}
	return nil
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
	distants := 0
	for _, x := range s.devDistants() {
		distants += len(x.projets)
		for _, p := range x.projets {
			enCours += boolInt(x.enLigne && p.PiloteActif)
			besoins += boolInt(x.enLigne && p.Besoin != "")
		}
	}
	r := ResumeModule{Stats: []StatModule{
		{Cle: "Projects running", Valeur: enCours, Page: "#global"},
		{Cle: "Milestones done today", Valeur: jalonsDuJour(fichierJalonsFaits(), time.Now()), Page: "#global"},
		{Cle: "Projects that need you", Valeur: besoins, Page: "#global"},
	}, Besoin: besoins, Etat: "inactif"}
	if distants > 0 {
		r.Stats = append(r.Stats, StatModule{Cle: "Remote projects", Valeur: distants, Page: "#global"})
	}
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	q := quotaActuel(projets)
	if v := valeurQuota(q); v != nil {
		r.Stats = append(r.Stats, StatModule{Cle: "Quota used", Valeur: v, Page: "#suivi"})
	}
	switch {
	case besoins > 0, q != nil && q.Statut == "rejected":
		r.Etat = "attention"
	case enCours > 0:
		r.Etat = "ok"
	}
	return r
}

// valeurQuota: the quota figure of the tile, the most used window in percent, else its status
// (allowed, allowed_warning, rejected); nil when no log said anything about it.
func valeurQuota(q *EtatQuota) any {
	if q == nil {
		return nil
	}
	plus := -1.0
	for _, f := range q.Fenetres {
		if f.Utilisation != nil && *f.Utilisation > plus {
			plus = *f.Utilisation
		}
	}
	if plus < 0 {
		return q.Statut
	}
	return int(math.Round(plus * 100))
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
func (moduleDev) Tutoriel() []EtapeTutoriel {
	return []EtapeTutoriel{
		{Titre: "Your projects", Texte: "Each project has its folder, its plan and its pilot. Choose one here, or create a new one from this list.", Cible: "#sel-projet"},
		{Titre: "The plan", Texte: "Claude splits the work into milestones. Each one is built, checked, then ticked here: you see what is done and what is left.", Cible: ".bloc-plan", Page: "#suivi"},
		{Titre: "The pilot", Texte: "Start launches the pilot: Claude works cycle after cycle, one milestone each time. Stop halts it; Start carries on where it was.", Cible: `[data-act="demarrer"], [data-act="arreter"]`, Page: "#suivi"},
		{Titre: "Requests", Texte: "Ask for a change or a new feature at any time: it waits in the queue and joins the plan at the next cycle.", Cible: `#onglets-module a[data-page="demandes"]`},
		{Titre: "For you", Texte: "What only you can do (create an account, give a key, check something by hand) waits here, step by step. Claude goes on with the rest meanwhile.", Cible: `#onglets-module a[data-page="pourvous"]`},
		{Titre: "Chat", Texte: "Talk with Claude about the project: ask a question, explain an idea. This tutorial stays at the top of the Guide.", Cible: `#onglets-module a[data-page="chat"]`},
	}
}

// The Dev module on the bus: etat_projet gives a project's progress (no path, no value).
func (moduleDev) Actions() []string { return []string{"etat_projet"} }

// Risque: etat_projet only reads (risqueBus).
func (moduleDev) Risque(string) string { return "lecture" }

func (moduleDev) Recevoir(s *Serveur, m MessageHub) (MessageHub, error) {
	for _, pv := range s.vuesProjets("") {
		if pv.Nom == m.Projet {
			return MessageHub{Donnees: map[string]any{"nom": pv.Nom, "actif": pv.Actif, "phase": pv.Phase,
				"jalon": firstNonEmpty(pv.JalonTitre, pv.Jalon), "faits": pv.Faits, "jalons": pv.Jalons, "besoin": pv.Besoin}}, nil
		}
	}
	return refusBus("unknown project"), nil
}
