package main

// A mission toward a remote Dev (ARCHITECTURE §Ouverture, Plusieurs Dev): the card names a project nom@instance of a
// paired remote that carries dev; the Go resolves it (ProjetCarte.Distant, never the agent's) into a remote Dev lane
// (module dev_distant, ref "remote id/project"). Launch = the Dev brief added to its queue then its pilot started,
// through the bus and the remote's MCP; the follower reads its milestones and activity from the remote's projets
// list; pause and stop reach its pilot. Its environments, deployments and their accords stay the remote's own: the
// card gives none and the hub's prod relay never runs for it (no prod without an accord, even in autonome).
// A remote offline: the lane waits, the journal says so, and the follower sends what is left once it answers.

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const moduleDevDistant = "dev_distant"

// projetDistantCarte: the remote project the card's name designates (nom@instance, or nom@remote id as a mention's ref).
func (x contexteCarte) projetDistantCarte(nom string) (devDistant, ProjetDistant, bool) {
	for _, d := range x.distants {
		for _, p := range d.projets {
			if strings.EqualFold(nom, p.Nom+"@"+d.d.Nom) || strings.EqualFold(nom, p.Nom+"@"+d.d.ID) {
				return d, p, true
			}
		}
	}
	return devDistant{}, ProjetDistant{}, false
}

// validerCarteDistante: c's project as a remote one (distant false: no remote project has this name). A remote
// lane is a Dev brief only: no new project, no working modes, no environment, Servers lane or vault entry (those
// would be this hub's, for a code it does not hold), and the remote must have granted the tools the launch uses.
func validerCarteDistante(c *CarteMission, x contexteCarte) (err error, distant bool) {
	d, p, ok := x.projetDistantCarte(c.Projet.Nom)
	if !ok {
		return nil, false
	}
	c.Projet.Nom, c.Projet.Distant = p.Nom+"@"+d.d.Nom, d.d.ID+"/"+p.Nom
	outils := []string{"pilote"}
	if c.Dev.Brief != "" {
		outils = append(outils, "ajouter_demande")
	}
	switch {
	case c.Dev.Modes != nil || c.Serveurs.Modes != nil:
		return erreurEntree("Working modes are chosen for a new project only"), true
	case len(c.Envs) > 0 || c.Serveurs.Brief != "" || len(c.Acces) > 0:
		return erreurEntreeArgs{"{0} is on a remote Athanor: its environments, servers and accesses are set there, the card gives none", []string{c.Projet.Nom}}, true
	}
	for _, o := range outils {
		if !slices.Contains(d.d.Outils, o) {
			return erreurEntreeArgs{"The remote Athanor {0} did not grant this hub the tool {1}", []string{d.d.Nom, o}}, true
		}
	}
	return nil, true
}

// projetDistantRef: the remote and its project of a lane's ref "remote id/project", as last read.
func (s *Serveur) projetDistantRef(ref string) (devDistant, ProjetDistant, bool) {
	id, nom, _ := strings.Cut(ref, "/")
	for _, x := range s.devDistants() {
		if x.d.ID != id {
			continue
		}
		if i := slices.IndexFunc(x.projets, func(p ProjetDistant) bool { return p.Nom == nom }); i >= 0 {
			return x, x.projets[i], true
		}
	}
	return devDistant{}, ProjetDistant{}, false
}

// envoyerVoieDistante: what v still owes the remote sent: the Dev brief (a_envoyer), then the pilot's start
// (a_demarrer); v.Etat is lancee once both went through. Stops at the first refusal (the next try resumes there).
func (s *Serveur) envoyerVoieDistante(v *VoieMission, c CarteMission, mission string) error {
	id, projet, _ := strings.Cut(v.Ref, "/")
	envoyer := func(action string, donnees map[string]any) error {
		rep := s.envoyerBus(MessageHub{De: "hub", Vers: "distant-" + id, Action: action, Projet: projet, Mission: mission, Donnees: donnees})
		if rep.Etat != "ok" {
			return errors.New(firstNonEmpty(rep.Raison, rep.Etat))
		}
		return nil
	}
	if v.Etat == "a_envoyer" {
		if err := envoyer("ajouter_demande", map[string]any{"texte": c.Dev.Brief, "titre": c.Titre}); err != nil {
			return err
		}
		v.Etat = "a_demarrer"
	}
	if v.Etat == "a_demarrer" {
		if err := envoyer("pilote", map[string]any{"action": "demarrer"}); err != nil {
			return err
		}
		v.Etat = "lancee"
		s.rafraichirDistant(id) // its pilot shows working at once
	}
	return nil
}

// etapesDistantes: the launch of a card on a remote project; the remote offline or refusing: the lane waits (the
// follower sends the rest), the history says why.
func (s *Serveur) etapesDistantes(c CarteMission, qui, mission string) ([]VoieMission, []EvenementMission, error) {
	v, faits := VoieMission{Module: moduleDevDistant, Ref: c.Projet.Distant, Etat: "a_envoyer"}, []EvenementMission{}
	if c.Dev.Brief == "" {
		v.Etat = "a_demarrer"
	}
	if err := s.envoyerVoieDistante(&v, c, mission); err != nil {
		faits = append(faits, evenementMission("lancee", couper(s.masquerSecrets("the remote lane waits for "+c.Projet.Nom+": "+err.Error()), 300)))
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: moduleDevDistant, Projet: c.Projet.Nom, Mission: mission, Action: "voie de mission lancée", Resultat: v.Ref + " → " + v.Etat})
	return []VoieMission{v}, faits, nil
}

// relancerVoiesDistantes: a launched mission's remote lanes that still owe the remote something, sent again; a
// refusal of a remote that answers goes to the journal once per reason (offline is said by evenementsMission).
func (s *Serveur) relancerVoiesDistantes(m Mission) {
	if m.Etat != "lancee" {
		return
	}
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	for i, v := range m.Voies {
		if x, _, ok := s.projetDistantRef(v.Ref); v.Module != moduleDevDistant || v.Etat == "lancee" || !ok || !x.enLigne {
			continue
		}
		err := s.envoyerVoieDistante(&v, c, m.ID)
		_, _ = modifierMission(m.ID, func(y *Mission) error {
			if i < len(y.Voies) && y.Voies[i].Ref == v.Ref {
				y.Voies[i].Etat = v.Etat
			}
			if err != nil {
				ajouterJournal(y, []EntreeJournal{s.entreeJournal("distant:refus:"+v.Ref+":"+err.Error(), time.Now(), "erreur",
					"The remote Athanor did not take the mission for {0}: {1}", c.Projet.Nom, err.Error())})
			}
			return nil
		})
	}
}

// voieDistanteEnDirect: a remote lane as the remote's projets list says now (offline: its last state, hors_ligne).
func (s *Serveur) voieDistanteEnDirect(v *VoieMission) {
	x, p, ok := s.projetDistantRef(v.Ref)
	if !ok {
		v.Actuel = "inconnu"
		return
	}
	v.Nom = p.Nom + "@" + x.d.Nom
	if n, _ := fmt.Sscanf(p.Plan, "%d/%d", &v.Fait, &v.Total); n != 2 || v.Fait < 0 || v.Fait > v.Total {
		v.Fait, v.Total = 0, 0
	}
	switch {
	case !x.enLigne:
		v.Actuel = "hors_ligne"
	case v.Etat != "lancee":
		v.Actuel = "attente"
	case p.PiloteActif:
		v.Actuel, v.Detail = "travaille", p.Jalon
	case p.Phase == "termine":
		v.Actuel = "termine"
	default:
		v.Actuel = "arrete"
	}
}

// evenementsDistants: a remote lane's journal: offline (once per absence), each new count of milestones done, the end.
func (s *Serveur) evenementsDistants(v VoieMission, noter func(id string, t time.Time, niveau, cle string, params ...string)) {
	x, _, ok := s.projetDistantRef(v.Ref)
	if !ok {
		return
	}
	switch v.Actuel {
	case "hors_ligne":
		noter("distant:hors_ligne:"+v.Ref+":"+x.d.Vu, time.Now(), "attention", "The remote Athanor {0} is offline: the mission waits for it", x.d.Nom)
	case "termine":
		noter("dev:termine:"+v.Ref, time.Now(), "info", "The Studio lane finished the request of {0}", v.Nom)
	}
	if v.Total > 0 && v.Etat == "lancee" {
		noter(fmt.Sprintf("distant:jalons:%s:%d", v.Ref, v.Fait), time.Now(), "info", "{0}: {1} / {2} milestones done", v.Nom, fmt.Sprint(v.Fait), fmt.Sprint(v.Total))
	}
}

// finirMissionDistante: a launched mission whose lanes are all remote and finished is done (nothing to deploy here).
func (s *Serveur) finirMissionDistante(id string) {
	m, err := lireMission(id)
	if err != nil || m.Etat != "lancee" || m.Relais == nil || len(m.Voies) == 0 {
		return
	}
	noms := []string{}
	for _, v := range s.voiesEnDirect(m).Voies {
		if v.Module != moduleDevDistant || v.Actuel != "termine" {
			return
		}
		noms = append(noms, v.Nom)
	}
	s.finirMission(id, m.Relais.Etape, "the request of "+strings.Join(noms, ", ")+" is done on its Athanor")
}

// ordreVoiesDistantes: a pilot order (pause_fin_cycle, demarrer, arreter) to m's remote lanes already started; one
// that does not go through is said in the journal and, for a remote offline, kept on the lane (M190): the
// follower sends it on the remote's return (renvoyerOrdresDistants); a newer order replaces it.
func (s *Serveur) ordreVoiesDistantes(m Mission, ordre string) {
	du := false
	for i, v := range s.voiesEnDirect(m).Voies {
		if v.Module != moduleDevDistant || v.Etat != "lancee" || ordre == "demarrer" && v.Actuel == "termine" {
			continue
		}
		id, projet, _ := strings.Cut(v.Ref, "/")
		err := s.piloteDistant(id, projet, ordre)
		if err != nil {
			s.noterJournalMission(m.ID, s.entreeJournal("distant:"+ordre+":"+v.Ref+":"+time.Now().UTC().Format(formatCree), time.Now(), "erreur",
				"The remote Athanor did not take the order {0} for {1}: {2}", ordre, firstNonEmpty(v.Nom, v.Ref), err.Error()))
		}
		garde := ""
		if err != nil && v.Actuel == "hors_ligne" {
			garde, du = ordre, true
		}
		s.ordreVoie(m.ID, i, v.Ref, garde)
	}
	if du {
		s.suivreMission(m.ID) // a stopped mission is no longer followed: kept followed until the order is delivered
	}
}

// ordreVoie: lane i's owed order set (empty: none).
func (s *Serveur) ordreVoie(id string, i int, ref, ordre string) {
	_, _ = modifierMission(id, func(y *Mission) error {
		if i < len(y.Voies) && y.Voies[i].Ref == ref {
			y.Voies[i].Ordre = ordre
		}
		return nil
	})
}

// renvoyerOrdresDistants: the orders mission m's remote lanes missed, sent to those back online; true while one
// is still owed (its remote still offline). A remote back that refuses it, or a remote removed, drops it.
func (s *Serveur) renvoyerOrdresDistants(m Mission) bool {
	id, du := m.ID, false
	for i, v := range m.Voies {
		if v.Ordre == "" {
			continue
		}
		x, p, ok := s.projetDistantRef(v.Ref)
		if ok && !x.enLigne {
			du = true
			continue
		}
		if ok {
			d, projet, _ := strings.Cut(v.Ref, "/")
			cle, params := "The remote Athanor took on its return the order {0} for {1}", []string{v.Ordre, p.Nom + "@" + x.d.Nom}
			niveau := "info"
			if err := s.piloteDistant(d, projet, v.Ordre); err != nil {
				cle, niveau, params = "The remote Athanor did not take the order {0} for {1}: {2}", "erreur", append(params, err.Error())
			}
			s.noterJournalMission(id, s.entreeJournal("distant:retour:"+v.Ordre+":"+v.Ref+":"+time.Now().UTC().Format(formatCree), time.Now(), niveau, cle, params...))
		}
		s.ordreVoie(id, i, v.Ref, "")
	}
	return du
}
