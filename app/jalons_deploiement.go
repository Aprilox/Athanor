package main

import (
	"cmp"
	"slices"
)

// JalonDeploiement: one step of a project's work on its servers, shown in Studio beside its code milestones.
// Titre is an i18n key (Args its values); Etat: fait | en_cours | echec | a_faire | revenu (a way back).
type JalonDeploiement struct {
	Titre   string         `json:"titre"`
	Args    []string       `json:"args,omitempty"`
	Etat    string         `json:"etat"`
	Detail  string         `json:"detail,omitempty"`  // why it failed, in the engine's or the task's words
	Message *MessageMoteur `json:"message,omitempty"` // that failure for the page to translate (the Deployment tab's)
	Date    string         `json:"date,omitempty"`
}

// etatTacheServeurs: a Servers task's state as a milestone's (an unknown id: done long ago, its file gone).
func etatTacheServeurs(id string) string {
	x, ok := tacheMission(id)
	switch {
	case !ok || x.Etat == "finie":
		return "fait"
	case x.Etat == "echec" || x.Etat == "arretee":
		return "echec"
	}
	return "en_cours"
}

// jalonsDeploiement: the steps of projet's deployment, in order: its servers prepared, its first deployment,
// its last versions deployed (newest last, 5 at most), then the next one; nil without a server.
func (s *Serveur) jalonsDeploiement(projet string) []JalonDeploiement {
	if s.acces == nil {
		return nil
	}
	env := s.envJalon(projet)
	e := s.acces.Environnements(projet)[env]
	if len(e.Serveurs) == 0 {
		return nil
	}
	r := lireRelais()[projet]
	h := historiqueDeploiements(projet, env)
	l := []JalonDeploiement{}
	prep := JalonDeploiement{Titre: "Prepare the servers", Etat: "a_faire"}
	switch p := r.Preparation; {
	case p != nil && p.Erreur != "":
		prep.Etat, prep.Detail = "echec", p.Erreur
	case p != nil && p.Tache != "":
		prep.Etat = etatTacheServeurs(p.Tache)
	case p != nil || e.Deploiement != nil || len(h) > 0:
		prep.Etat = "fait" // the servers already served the project
	}
	l = append(l, prep)
	premier := JalonDeploiement{Titre: "First deployment", Etat: "a_faire"}
	switch {
	case e.Deploiement != nil:
		premier.Etat = "fait"
	case r.Premier != "" && etatTacheServeurs(r.Premier) == "en_cours":
		premier.Etat = "en_cours"
	case r.Premier != "" || len(h) > 0: // tried, and no method recorded: it did not go through (the Deployment tab relaunches it)
		premier.Etat = "echec"
	}
	l = append(l, premier)
	for _, d := range h[max(0, len(h)-5):] {
		j := JalonDeploiement{Titre: "Version {0} online", Args: []string{commitCourt(d.Commit)}, Date: cmp.Or(d.Fin, d.Debut)}
		switch d.Etat {
		case "ok":
			j.Etat = "fait"
		case "echec":
			j.Titre, j.Etat, j.Detail, j.Message = "Version {0} failed", "echec", d.Erreur, d.Message
		case "revenu":
			j.Titre, j.Etat = "Back to version {0}", "revenu"
		default:
			j.Titre, j.Etat = "Version {0} going online", "en_cours"
		}
		if d.Retour {
			j.Titre = "Back to version {0}"
		}
		l = append(l, j)
	}
	if !slices.ContainsFunc(l, func(j JalonDeploiement) bool { return j.Etat == "en_cours" }) {
		suivant := map[string]string{"jalon": "Next deployment: after the next milestone", "livraison": "Next deployment: at delivery"}[e.Moment]
		if suivant != "" {
			l = append(l, JalonDeploiement{Titre: suivant, Etat: "a_faire"})
		}
	}
	return l
}

// commitCourt: a commit's first 7 characters.
func commitCourt(commit string) string { return commit[:min(len(commit), 7)] }
