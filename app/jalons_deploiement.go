package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
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
	Fois    int            `json:"-"`                 // the same failure in a row, counted
	Critere bool           `json:"critere,omitempty"` // an acceptance criterion of the servers' plan, as its last check found it
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
	// the versions: a failure fixed since (a later success, or the site found healthy after it) leaves the plan, the
	// same failure in a row is one line with its tries; the Deployment tab keeps the whole history
	sante, derniereOK := lireSante().Envs[projet+"/"+env], ""
	for _, d := range h {
		if d.Etat == "ok" {
			derniereOK = cmp.Or(d.Fin, d.Debut)
		}
	}
	var versions []JalonDeploiement
	for _, d := range h {
		quand := cmp.Or(d.Fin, d.Debut)
		if d.Etat == "echec" && (derniereOK > quand || sante.Etat == "ok" && sante.T > quand) {
			continue
		}
		j := JalonDeploiement{Titre: "Version {0} online", Args: []string{commitCourt(d.Commit)}, Date: quand}
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
		if n := len(versions); n > 0 && j.Etat == "echec" && versions[n-1].Etat == "echec" && versions[n-1].Args[0] == j.Args[0] && versions[n-1].Detail == j.Detail {
			versions[n-1].Fois++
			versions[n-1].Date = quand
			continue
		}
		j.Fois = 1
		versions = append(versions, j)
	}
	for i := range versions {
		if versions[i].Fois > 1 {
			versions[i].Titre, versions[i].Args = "Version {0} failed ({1} tries)", append(versions[i].Args, strconv.Itoa(versions[i].Fois))
		}
	}
	l = append(l, versions[max(0, len(versions)-5):]...)
	if v := derniereVerification(projet, e.tousServeurs()); v != nil { // its acceptance criteria, as a milestone's
		for _, c := range v.Criteres {
			l = append(l, JalonDeploiement{Titre: c.Texte, Etat: map[bool]string{true: "fait", false: "echec"}[c.OK], Critere: true, Date: v.T})
		}
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

// derniereVerification: the last check of projet's tasks on its servers that wrote acceptance criteria (nil: none).
func derniereVerification(projet string, serveurs []string) *VerificationTache {
	var v *VerificationTache
	for _, id := range serveurs {
		for _, t := range tachesServeur(id) {
			if p, _, _ := strings.Cut(t.Env, "/"); p == projet && t.Verification != nil && len(t.Verification.Criteres) > 0 && (v == nil || t.Verification.T > v.T) {
				x := *t.Verification
				v = &x
			}
		}
	}
	return v
}
