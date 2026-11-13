package main

// Following a launched mission (ARCHITECTURE §étape 10 Suivi): one goroutine per mission lancee/en_pause reads the
// sources (the project's pilot through voiesEnDirect, the milestones done, the deployment history, the Servers tasks,
// the linked approvals) and writes the journal at each transition; it starts nothing by itself but the prod relay
// (mission_relais.go).

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"time"
)

// RelaisMission: where the mission's relay is (construction → dev_deploye → dev_verifie → accord_prod →
// prod_deploye → termine) and what it stands on.
type RelaisMission struct {
	Etape           string `json:"etape"`
	Commit          string `json:"commit,omitempty"`
	DeploiementDev  string `json:"deploiement_dev,omitempty"`
	DeploiementProd string `json:"deploiement_prod,omitempty"`
	Accord          string `json:"accord,omitempty"`
}

// EntreeJournal: a translation key (an English template of i18n.js) and its parameters, never an agent's free text;
// ID is the event (jalon:M3, deploi:<id>:ok, accord:<id>): the same event is never written twice.
type EntreeJournal struct {
	ID     string   `json:"id"`
	T      string   `json:"t"`
	Cle    string   `json:"cle"`
	Params []string `json:"params,omitempty"`
	Niveau string   `json:"niveau"` // info | attention | erreur
}

const maxJournalMission = 500

// pasSuiviMission: the time between two readings of the sources (the tests call observerMission themselves).
var pasSuiviMission = 5 * time.Second

var suivisMission sync.Map // mission id → its follower runs

// suivreMission: id's follower started, once; it stops by itself when the mission is no longer followed.
func (s *Serveur) suivreMission(id string) {
	if _, deja := suivisMission.LoadOrStore(id, true); deja {
		return
	}
	go func() {
		defer suivisMission.Delete(id)
		for s.observerMission(id) {
			time.Sleep(pasSuiviMission)
		}
	}()
}

func missionSuivie(etat string) bool { return etat == "lancee" || etat == "en_pause" }

func dateSource(t string) time.Time {
	d, _ := time.Parse(time.RFC3339, t)
	return d
}

// observerMission: one reading of id's sources, its new events written and its relay moved on; false when the
// mission is no longer followed (gone, ended, stopped).
func (s *Serveur) observerMission(id string) bool {
	m, err := lireMission(id)
	if err != nil || !missionSuivie(m.Etat) {
		return false
	}
	evts, relais := s.evenementsMission(m)
	m, err = modifierMission(id, func(m *Mission) error {
		if !missionSuivie(m.Etat) {
			return errMissionInconnue
		}
		ajouterJournal(m, evts)
		avancerRelais(m, relais)
		return nil
	})
	if err != nil {
		return false
	}
	s.relaisProd(id)
	m, err = lireMission(id)
	return err == nil && missionSuivie(m.Etat)
}

// ajouterJournal: evts not written yet added in time order; at maxJournalMission, the oldest removed, and an
// event older than every kept one is not written again.
func ajouterJournal(m *Mission, evts []EntreeJournal) {
	for _, e := range evts {
		if slices.ContainsFunc(m.Journal, func(x EntreeJournal) bool { return x.ID == e.ID }) ||
			len(m.Journal) >= maxJournalMission && e.T <= m.Journal[0].T {
			continue
		}
		m.Journal = append(m.Journal, e)
	}
	slices.SortStableFunc(m.Journal, func(a, b EntreeJournal) int { return dateSource(a.T).Compare(dateSource(b.T)) })
	if n := len(m.Journal); n > maxJournalMission {
		m.Journal = slices.Delete(m.Journal, 0, n-maxJournalMission)
	}
}

// avancerRelais: the dev part of the relay (construction, dev_deploye, dev_verifie) follows the latest dev
// deployment; the prod part (from accord_prod on) is the prod relay's.
func avancerRelais(m *Mission, r RelaisMission) {
	if m.Relais == nil {
		m.Relais = &RelaisMission{Etape: "construction"}
	}
	if !slices.Contains([]string{"construction", "dev_deploye", "dev_verifie"}, m.Relais.Etape) || r.Etape == "" {
		return
	}
	m.Relais.Etape, m.Relais.Commit, m.Relais.DeploiementDev = r.Etape, r.Commit, r.DeploiementDev
}

func shaCourt(sha string) string { return sha[:min(len(sha), 7)] }

// evenementsMission: what m's sources say now, as journal events, and the dev relay as the latest dev deployment
// since the launch leaves it (Etape "" without one). Every parameter masked and bounded.
func (s *Serveur) evenementsMission(m Mission) ([]EntreeJournal, RelaisMission) {
	lance := time.Time{}
	if i := slices.IndexFunc(m.Historique, func(e EvenementMission) bool { return e.Etat == "lancee" }); i >= 0 {
		lance = dateSource(m.Historique[i].T).Truncate(time.Second) // deployments are dated to the second
	}
	var evts []EntreeJournal
	noter := func(id string, t time.Time, niveau, cle string, params ...string) {
		evts = append(evts, s.entreeJournal(id, t, niveau, cle, params...))
	}
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	noms := map[string]string{}
	if l, err := lireServeurs(); err == nil {
		for _, x := range l {
			noms[x.ID] = x.Nom
		}
	}
	relais := RelaisMission{}
	for _, v := range s.voiesEnDirect(m).Voies {
		switch v.Module {
		case "dev":
			p, ok := s.projetParNom(v.Ref)
			if !ok {
				continue
			}
			for _, j := range lireJalonsFaits(fichierJalonsFaits()) {
				if j.Projet == p.Dossier && !j.Date.Before(lance) {
					noter("jalon:"+j.Jalon, j.Date, "info", "Milestone delivered: {0}", j.Jalon)
				}
			}
			if v.Actuel == "termine" {
				noter("dev:termine", time.Now(), "info", "The Studio lane finished the request of {0}", p.Nom)
			}
			for _, e := range c.Envs {
				for _, d := range historiqueDeploiements(p.Nom, e.Nom) {
					debut := dateSource(d.Debut)
					if debut.Before(lance) || d.Etat == "en_cours" {
						continue
					}
					fin := cmpDate(d.Fin, debut)
					switch d.Etat {
					case "ok":
						noter("deploi:"+d.ID+":ok", fin, "info", "Deployment to {0} done: version {1}", e.Nom, shaCourt(d.Commit))
					case "echec":
						noter("deploi:"+d.ID+":echec", fin, "erreur", "Deployment to {0} failed: version {1}", e.Nom, shaCourt(d.Commit))
					case "revenu":
						noter("deploi:"+d.ID+":revenu", fin, "attention", "{0} went back from version {1}", e.Nom, shaCourt(d.Commit))
					}
					if d.Verification != nil {
						if d.Verification.OK {
							noter("verif:"+d.ID+":ok", dateSource(d.Verification.T), "info", "{0} checked: {1} answers", e.Nom, e.URL)
						} else {
							noter("verif:"+d.ID+":echec", dateSource(d.Verification.T), "erreur", "{0} check failed: {1}", e.Nom, e.URL)
						}
					}
					if e.Nom == "dev" && d.Etat == "ok" && !d.Retour {
						relais = RelaisMission{Etape: "dev_deploye", Commit: d.Commit, DeploiementDev: d.ID}
						if d.Verification != nil && d.Verification.OK {
							relais.Etape = "dev_verifie"
						}
					}
				}
			}
		case "servers":
			t, ok := tacheMission(v.Ref)
			if !ok || len(t.Serveurs) == 0 {
				continue
			}
			nom := cmp.Or(noms[t.Serveurs[0]], t.Serveurs[0])
			switch t.Etat {
			case "attente_accord":
				noter("plan:"+t.ID+":"+strconv.Itoa(len(t.Plans)), time.Now(), "attention", "A Servers plan on {0} waits for your accord", nom)
			case "finie":
				noter("tache:"+t.ID+":finie", cmpDate(t.Fin, time.Now()), "info", "The Servers plan on {0} is done", nom)
			case "echec", "arretee":
				noter("tache:"+t.ID+":"+t.Etat, cmpDate(t.Fin, time.Now()), "erreur", "The Servers task on {0} stopped: {1}", nom, t.Etat)
			}
		}
	}
	if s.approbations != nil {
		l, _ := s.approbations.Liste()
		for _, x := range l {
			if x.Mission != m.ID && x.Details["mission"] != m.ID {
				continue
			}
			noter("accord:"+x.ID, dateSource(x.Cree), "attention", "Your accord is asked: {0} ({1})", x.Action, x.Env)
			switch x.Etat {
			case "acceptee":
				noter("accord:"+x.ID+":acceptee", dateSource(x.T), "info", "Accord given: {0} ({1})", x.Action, x.Env)
			case "refusee", "expiree":
				noter("accord:"+x.ID+":"+x.Etat, dateSource(x.T), "attention", "Accord not given ({2}): {0} ({1})", x.Action, x.Env, x.Etat)
			}
		}
	}
	return evts, relais
}

// entreeJournal: a journal entry, its event id and parameters masked and bounded.
func (s *Serveur) entreeJournal(id string, t time.Time, niveau, cle string, params ...string) EntreeJournal {
	for i, p := range params {
		params[i] = couper(s.masquerSecrets(p), 200)
	}
	return EntreeJournal{ID: couper(s.masquerSecrets(id), 200), T: t.UTC().Format(formatCree), Cle: cle, Params: params, Niveau: niveau}
}

// cmpDate: the date t, or d when t is empty or unreadable.
func cmpDate(t string, d time.Time) time.Time {
	if x := dateSource(t); !x.IsZero() {
		return x
	}
	return d
}

// reprendreMissions (ARCHITECTURE §étape 10 Reprise), at Athanor's start after the vault and the tokens: the
// launched missions followed again (never lancerMission, never an access made again), a pending accord whose card
// changed refused, a mission that needs the closed infra key waiting for the unlock (journal, bell), a preparation
// cut by the stop back to its card or failed.
func (s *Serveur) reprendreMissions() {
	t := time.Now()
	id := "redemarrage:" + t.UTC().Format(formatCree)
	for _, m := range lireMissions() {
		s.refuserAccordsPerimes(m)
		switch {
		case m.Etat == "preparation" && !orchestrateurTravaille(m.ID):
			s.reprendrePreparation(m.ID)
		case !missionSuivie(m.Etat):
		case s.attendCoffre(m):
			s.noterJournalMission(m.ID, s.entreeJournal(id, t, "attention", "Athanor restarted: the mission waits for the vault to be unlocked"))
		default:
			s.noterJournalMission(m.ID, s.entreeJournal(id, t, "info", "Athanor restarted: the mission resumes"))
			s.suivreMission(m.ID)
		}
	}
}

// missionsDeverrouillees: the infra key just opened: the missions that waited for it followed again.
func (s *Serveur) missionsDeverrouillees() {
	t := time.Now()
	for _, m := range lireMissions() {
		if _, suivie := suivisMission.Load(m.ID); missionSuivie(m.Etat) && !suivie {
			s.noterJournalMission(m.ID, s.entreeJournal("deverrouille:"+t.UTC().Format(formatCree), t, "info", "The vault is unlocked: the mission resumes"))
			s.suivreMission(m.ID)
		}
	}
}

// attendCoffre: m needs the infra key (an infra entry ticked, a Servers lane, an environment on servers) and it is
// closed (I3: no infrastructure action while locked).
func (s *Serveur) attendCoffre(m Mission) bool {
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	infra := slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Portee == "infra" && !a.Decoche }) ||
		slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Etat != "refusee" }) ||
		slices.ContainsFunc(c.Envs, func(e EnvCarte) bool { return len(e.Serveurs) > 0 })
	return infra && !s.etatCoffre().Infra
}

// refuserAccordsPerimes: m's pending accords (launch, prod) asked for a card or a policy that is no longer m's.
func (s *Serveur) refuserAccordsPerimes(m Mission) {
	if s.approbations == nil {
		return
	}
	l, _ := s.approbations.Liste()
	e := empreinteCarte(m)
	for _, x := range l {
		if x.Etat == "attente" && (x.Mission == m.ID || x.Details["mission"] == m.ID) && x.Details["empreinte"] != "" && x.Details["empreinte"] != e {
			s.refuserAccordMission(x.ID, "hub", "la mission a changé pendant l'arrêt d'Athanor")
		}
	}
}
