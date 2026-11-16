package main

// A mission always changeable (M266, note of 2026-11-16 13h03 §B): failed, paused, stopped or running, its card
// changes and its resources (keys, servers, providers) come and go, except the fields an action in progress
// freezes (champsFiges, said with the mission). Once launched, a change goes to the project at once
// (appliquerCarte): the entries given or taken back with the launch's audits, the environments defined again, a
// new Servers lane prepared by the follower at its next reading; the pilot reads its secrets at its next cycle.

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

// etatsModifiables: the states whose card the owner changes (proposee before M266 only).
var etatsModifiables = []string{"proposee", "echec", "lancee", "arret_demande", "en_pause", "arretee"}

// lanceeUneFois: m was launched at least once (a "lancee" event in its history).
func lanceeUneFois(m Mission) bool {
	return slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == "lancee" })
}

// raisonEchec: the event that put m in echec (its last "echec" event; the changes after it are "modifiee").
func raisonEchec(m Mission) EvenementMission {
	for i := len(m.Historique) - 1; i >= 0; i-- {
		if m.Historique[i].Etat == "echec" {
			return m.Historique[i]
		}
	}
	return EvenementMission{}
}

// projetFige: the card's project exists by a launch (an existing project, or the new one created): it is changed in
// Manage my project from then on, never on the card.
func projetFige(m Mission, c CarteMission) bool {
	return lanceeUneFois(m) && (!c.Projet.Nouveau || faitMission(m, "project created: "))
}

// envsEnPreparation: the card environments of m whose Servers task runs now (name → task id).
func envsEnPreparation(m Mission) map[string]string {
	out := map[string]string{}
	for _, v := range m.Voies {
		_, nom, _ := strings.Cut(v.Env, "/")
		if v.Module != "servers" || v.Ref == "" || nom == "" {
			continue
		}
		tachesEnCours.Lock()
		tourne := tachesEnCours.m[v.Ref] != nil
		tachesEnCours.Unlock()
		if t, ok := tacheMission(v.Ref); tourne || ok && !slices.Contains([]string{"finie", "echec", "arretee"}, t.Etat) {
			out[nom] = v.Ref
		}
	}
	return out
}

// Reasons of a frozen field (English keys, translated by the page).
const (
	figeOrchestrateur = "The orchestrator is working on this card: wait for it to finish"
	figeAccord        = "A launch request for this card waits for your decision: refuse it first to change the card"
	figeProjet        = "The project is created: change its settings with Manage this project"
	figeConsigne      = "The mission is launched: the orchestrator no longer rewrites its card; add a request to its project instead"
	figePolitique     = "The policy of a launched mission is changed with its controls"
	figeDistant       = "The project is on another PC: its environments and accesses are changed there"
	figeEnv           = "The Servers lane is preparing this environment: wait for it to end, or pause the mission"
)

// champsFiges: what of m's card cannot change now and why ("carte": nothing; "projet", "consigne", "politique",
// "envs", "env:NAME": that field), empty when everything can.
func (s *Serveur) champsFiges(m Mission) map[string]string {
	out := map[string]string{}
	var c CarteMission
	switch {
	case !slices.Contains(etatsModifiables, m.Etat) || json.Unmarshal(m.Carte, &c) != nil:
		out["carte"] = "This mission is " + m.Etat + ": its card can no longer be changed"
	case orchestrateurTravaille(m.ID):
		out["carte"] = figeOrchestrateur
	}
	if _, ok := s.accordMissionAttente(m.ID); ok {
		out["carte"] = figeAccord
	}
	if out["carte"] != "" || !lanceeUneFois(m) {
		return out
	}
	out["consigne"], out["politique"] = figeConsigne, figePolitique
	if projetFige(m, c) {
		out["projet"] = figeProjet
	}
	if c.Projet.Distant != "" {
		out["envs"] = figeDistant
	}
	for nom := range envsEnPreparation(m) {
		out["env:"+nom] = figeEnv
	}
	return out
}

// donnesCarte: c's ticked lines with a vault entry, by (entry id, environment), in the card's order.
func donnesCarte(c CarteMission) (cles [][2]string, lignes map[[2]string]AccesCarte) {
	lignes = map[[2]string]AccesCarte{}
	for _, a := range c.Acces {
		k := [2]string{a.ID, envAcces(a.Pour, c)}
		if _, deja := lignes[k]; a.Decoche || a.ARemplir || a.ID == "" || deja {
			continue
		}
		cles, lignes[k] = append(cles, k), a
	}
	return cles, lignes
}

// controlerFiges: the change from avant to apres of launched mission m touches no frozen field (409 with its reason).
func controlerFiges(m Mission, avant, apres CarteMission) error {
	if projetFige(m, avant) && (apres.Projet != avant.Projet || !memeJSON(apres.Dev.Modes, avant.Dev.Modes)) {
		return erreurMission{http.StatusConflict, figeProjet}
	}
	_, av := donnesCarte(avant)
	_, ap := donnesCarte(apres)
	if avant.Projet.Distant != "" && (!memeJSON(avant.Envs, apres.Envs) || len(av) != len(ap) ||
		slices.ContainsFunc(mapsCles(av), func(k [2]string) bool { _, ok := ap[k]; return !ok })) {
		return erreurMission{http.StatusConflict, figeDistant}
	}
	for nom := range envsEnPreparation(m) {
		if apres.Serveurs.Brief != avant.Serveurs.Brief { // M270: a running Servers task keeps the brief it was given
			return erreurMission{http.StatusConflict, figeEnv + " (" + nom + ")"}
		}
		serveurs := func(c CarteMission) string {
			i := slices.IndexFunc(c.Envs, func(e EnvCarte) bool { return e.Nom == nom })
			if i < 0 {
				return ""
			}
			return strings.Join(c.Envs[i].Serveurs, ",")
		}
		retire := slices.ContainsFunc(mapsCles(av), func(k [2]string) bool { _, ok := ap[k]; return !ok && k[1] == nom })
		if serveurs(avant) != serveurs(apres) || retire {
			return erreurMission{http.StatusConflict, figeEnv + " (" + nom + ")"}
		}
	}
	return nil
}

// memeJSON: a and b say the same thing (cards' parts, compared as written).
func memeJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func mapsCles[K comparable, V any](m map[K]V) []K {
	l := make([]K, 0, len(m))
	for k := range m {
		l = append(l, k)
	}
	return l
}

// appliquerCarte: the changed card of mission m, launched once, given to its project now. The environments
// changed defined again (one left by the card loses its servers), the entries ticked since given and those
// removed taken back (only a grant the mission gave), each audited as at the launch and written to the project's
// secrets; an environment whose servers changed gets its Servers lane back to attente_plan (running mission:
// prepared by the follower at its next reading; failed: by the launch resumed). The project not created yet, or
// on another PC: nothing now, the launch takes the card as it is. A step that fails goes to the journal.
func (s *Serveur) appliquerCarte(m Mission, avant, apres CarteMission, qui string) Mission {
	p, ok := s.projetNomme(apres.Projet.Nom)
	if !ok || !projetFige(m, apres) || apres.Projet.Distant != "" || s.acces == nil {
		return m
	}
	var faits []EvenementMission
	var erreurs []EntreeJournal
	now := time.Now()
	echoue := func(err error) {
		erreurs = append(erreurs, s.entreeJournal("modif:"+now.UTC().Format(formatCree)+":"+aleatoire(4), now, "erreur", "The change of the card did not reach the project: {0}", err.Error()))
	}
	if force, err := s.forcerNatifMission(p, apres, qui); err != nil {
		echoue(err)
	} else if force {
		faits = append(faits, evenementMission("modifiee", "native Windows mode: the project is forced by this change (logged)"))
	}
	inventaire, _ := lireServeurs()
	actuels := s.acces.Environnements(p.Nom)
	var aPreparer []string
	for _, ec := range apres.Envs {
		i := slices.IndexFunc(avant.Envs, func(e EnvCarte) bool { return e.Nom == ec.Nom })
		if i >= 0 && memeJSON(avant.Envs[i], ec) {
			continue
		}
		if err := s.definirEnvCarte(p, apres, ec, m.Politique, actuels, inventaire); err != nil {
			echoue(err)
			continue
		}
		faits = append(faits, evenementMission("lancee", "environment set: "+ec.Nom+" "+ec.URL+", "+ec.Protection+", "+ec.Politique))
		if len(ec.Serveurs) > 0 && (i < 0 || strings.Join(avant.Envs[i].Serveurs, ",") != strings.Join(ec.Serveurs, ",")) {
			aPreparer = append(aPreparer, ec.Nom)
		}
	}
	for _, ea := range avant.Envs {
		e, ok := actuels[ea.Nom]
		if slices.ContainsFunc(apres.Envs, func(x EnvCarte) bool { return x.Nom == ea.Nom }) || !ok || len(e.Serveurs) == 0 {
			continue
		}
		e.Serveurs, e.DeployerAuJalon = nil, false
		if err := s.acces.DefinirEnvironnement(p.Nom, ea.Nom, e); err != nil {
			echoue(errMoteur("the environment {0}: {1}", ea.Nom, err))
			continue
		}
		faits = append(faits, evenementMission("modifiee", "environment left by the card: "+ea.Nom))
	}
	if apres.Serveurs.Brief != avant.Serveurs.Brief && apres.Serveurs.Brief != "" { // M270: its lanes prepared again with the new brief
		for _, ec := range apres.Envs {
			if len(ec.Serveurs) > 0 && !slices.Contains(aPreparer, ec.Nom) {
				aPreparer = append(aPreparer, ec.Nom)
			}
		}
		faits = append(faits, evenementMission("modifiee", "Servers brief changed: its environments prepared again with it"))
	}
	if apres.Dev.Brief != avant.Dev.Brief && apres.Dev.Brief != "" { // M270: the project's pilot reads it as a new request
		if _, _, err := s.ajouterDemande(p, apres.Titre, apres.Dev.Brief); err != nil {
			echoue(errMoteur("the new Studio brief: {0}", err))
		} else {
			faits = append(faits, evenementMission("modifiee", "Studio brief changed: added to the project as a request"))
		}
	}
	clesAv, av := donnesCarte(avant)
	clesAp, ap := donnesCarte(apres)
	acces := false
	for _, k := range clesAp {
		if _, ok := av[k]; ok {
			continue
		}
		f, err := s.donnerAccesCarte(p, apres, ap[k], qui)
		if err != nil {
			echoue(err)
		} else if f.Detail != "" {
			faits, acces = append(faits, f), true
		}
	}
	for _, k := range clesAv {
		if _, ok := ap[k]; ok {
			continue
		}
		i := slices.IndexFunc(s.acces.Projet(p.Nom).Acces, func(g AccesProjet) bool { return g.Entree == k[0] && g.Env == k[1] && g.Auto == "mission" })
		if i < 0 { // the owner's own grant, or already taken back: not the mission's to remove
			continue
		}
		g := s.acces.Projet(p.Nom).Acces[i]
		if err := s.acces.Retirer(p.Nom, k[0], k[1]); err != nil {
			echoue(errMoteur("taking back {0}: {1}", av[k].Entree, err))
			continue
		}
		s.auditAcces(qui, p.Nom, "acces_retire", s.entreeCoffre(k[0]), g.Variable, k[1])
		faits, acces = append(faits, evenementMission("modifiee", "access taken back: "+av[k].Entree+" → "+g.Variable+" ("+k[1]+")")), true
	}
	if acces { // C86: in the project's secrets.env now; a running pilot reads it at its next cycle
		if _, err := s.injecterAcces(p, nil); err != nil {
			echoue(errMoteur("writing the access given: {0}", err))
		}
	}
	m2, err := modifierMission(m.ID, func(x *Mission) error {
		x.Historique = append(x.Historique, faits...)
		ajouterJournal(x, erreurs)
		if x.Etat == "echec" {
			return nil // a failed launch makes its lanes again when resumed
		}
		for _, nom := range aPreparer {
			if apres.Serveurs.Brief == "" {
				break
			}
			env := p.Nom + "/" + nom
			v := VoieMission{Module: "servers", Etat: "attente_plan", Env: env}
			if i := slices.IndexFunc(x.Voies, func(y VoieMission) bool { return y.Module == "servers" && y.Env == env }); i >= 0 {
				x.Voies[i] = v // its former task is over (a running one freezes the environment)
			} else {
				x.Voies = append(x.Voies, v)
			}
		}
		x.Voies = slices.DeleteFunc(x.Voies, func(y VoieMission) bool { // waiting for an environment no longer prepared
			_, nom, _ := strings.Cut(y.Env, "/")
			return y.Module == "servers" && y.Etat == "attente_plan" &&
				!slices.ContainsFunc(apres.Envs, func(e EnvCarte) bool { return e.Nom == nom && len(e.Serveurs) > 0 })
		})
		return nil
	})
	if err != nil {
		return m
	}
	if missionSuivie(m2.Etat) {
		s.suivreMission(m2.ID)
	}
	return m2
}
