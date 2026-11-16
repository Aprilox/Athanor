package main

// The mission's relay to prod (ARCHITECTURE §étape 10 Relais): once the Studio lane finished, the latest dev
// deployment checked on the project's HEAD and the prod Servers tasks done, the owner's accord for prod is asked,
// bound to the mission, its card's print, the full SHA and the servers, used once; accepted → the engine deploys
// prod and its check ends the mission. Never without an accord, whatever the mission's policy (no rule lets it by).

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// commencerDeploiementMission: the engine's start (the tests replace it; the engine has its own tests).
var commencerDeploiementMission = (*Serveur).commencerDeploiement

// relaisProdMu: one relay step at a time (the follower and Ask again), so that one accord starts one deployment.
var relaisProdMu sync.Mutex

// accordProd: the accord the prod deployment of sha for mission m waits for (Reprendre compares all of it).
func (s *Serveur) accordProd(m Mission, projet, sha string) Approbation {
	env := envFinalMission(m)
	e := s.acces.Environnements(projet)[env]
	hote := cmp.Or(hoteEnv(&e), projet)
	avant, n, changements := s.changementsProd(projet, env, sha)
	ap := Approbation{Projet: projet, Env: env, Mission: m.ID, Agent: "relais", Action: actionDeploiement,
		Raison: "mission " + m.ID + ": commit " + sha + " → " + hote,
		Details: map[string]string{"mission": m.ID, "empreinte": empreinteCarte(m), "sha": sha, "commit": shaCourt(sha), "env": env, "url": e.URL,
			"hote": hote, "serveurs": listeServeursAccord(e.Serveurs), "avant": shaCourt(avant), "commits": n, "changements": changements}}
	// R1 (C68, C69): the deployment runs with Accorde, so the owner reads the build files it would run first
	if p, ok := s.projetParNom(projet); ok {
		if f := fichiersBuildChanges(p, env, sha, s.configProdMission(projet, env)); len(f) > 0 {
			ap.Details["fichiers_build"] = couper(strings.Join(f, ", "), 300)
			ap.Raison += "; build files changed: " + ap.Details["fichiers_build"]
		}
	}
	return ap
}

// envFinal (M247): the card's environment of the final deployment, its first Production one ("" without; a
// card written before M247 has no production field, its "prod" environment is it).
func envFinal(c CarteMission) string {
	for _, e := range c.Envs {
		if e.Production || e.Nom == "prod" {
			return e.Nom
		}
	}
	return ""
}

// envJalonCarte (M247): the card's linked environment, deployed after each milestone: dev, or else its first
// environment that is not Production ("" without one).
func envJalonCarte(c CarteMission) string {
	nom := ""
	for _, e := range c.Envs {
		if e.Nom == "dev" && !e.Production {
			return "dev"
		}
		if nom == "" && !e.Production && e.Nom != "prod" {
			nom = e.Nom
		}
	}
	return nom
}

// envFinalMission: envFinal of m's card, "prod" without one (the relay never asks then).
func envFinalMission(m Mission) string {
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	return cmp.Or(envFinal(c), "prod")
}

// configProdMission: the method env (Production) is deployed with: its own, or the milestone environment's
// checked one for its first deployment.
func (s *Serveur) configProdMission(projet, env string) *ConfigDeploiement {
	envs := s.acces.Environnements(projet)
	return cmp.Or(envs[env].Deploiement, envs[s.envJalon(projet)].Deploiement)
}

// changementsProd (P7): what the prod deployment of sha changes, for the owner deciding from a phone: the version
// prod serves now ("" before the first one), the number of commits since it and the newest three's subjects, the vault's values masked;
// cut under bornerApprobation's length so that the same accord compares equal.
func (s *Serveur) changementsProd(projet, env, sha string) (avant, n, changements string) {
	h := historiqueDeploiements(projet, env)
	for _, d := range slices.Backward(h) {
		if d.Etat == "ok" {
			avant = d.Commit
			break
		}
	}
	p, ok := s.projetParNom(projet)
	if !ok {
		return avant, "", ""
	}
	plage := []string{"log", "--format=%s", "-n", "20", sha}
	if avant != "" {
		plage = append(plage, "^"+avant)
	}
	out, err := gitSortie(p.Dossier, plage...)
	if err != nil || out == "" {
		return avant, "0", ""
	}
	l := strings.Split(out, "\n")
	n = strconv.Itoa(len(l))
	if len(l) == 20 {
		n = "20+"
	}
	return avant, n, couper(s.masquerSecrets(strings.Join(l[:min(len(l), 3)], "; ")), 190)
}

// relaisProd: one step of mission id's prod relay, from what its sources say now.
func (s *Serveur) relaisProd(id string) {
	relaisProdMu.Lock()
	defer relaisProdMu.Unlock()
	m, err := lireMission(id)
	if err != nil || m.Etat != "lancee" || m.Relais == nil || s.approbations == nil || s.acces == nil {
		return
	}
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	vues := s.voiesEnDirect(m).Voies
	i := slices.IndexFunc(vues, func(v VoieMission) bool { return v.Module == "dev" })
	if i < 0 { // M268b: no Studio lane (servers or keys only): done once every Servers task is
		if c.Dev.Brief == "" && m.Relais.Etape == "construction" &&
			!slices.ContainsFunc(vues, func(v VoieMission) bool { return v.Module != "servers" || v.Actuel != "finie" }) {
			s.finirMission(id, "construction", cmp.Or(map[bool]string{true: "no Studio lane: the Servers tasks are done"}[len(vues) > 0], "no Studio lane: the vault entries are given"))
		}
		return
	}
	p, ok := s.projetParNom(vues[i].Ref)
	if !ok {
		return
	}
	head, _ := gitSortie(p.Dossier, "rev-parse", "HEAD")
	r, env := *m.Relais, envFinal(c)
	if r.Etape != "prod_deploye" && s.attendCoffre(m) { // R7: the infra keys locked, the accord would be used for a deployment that cannot run
		return
	}
	switch r.Etape {
	case "dev_verifie":
		if vues[i].Actuel != "termine" || r.Commit == "" || r.Commit != head || !prodPrete(vues, p.Nom, cmp.Or(env, "prod")) {
			return
		}
		if env == "" { // M247: no Production environment, the delivery is the linked environment's (deployed at each milestone)
			s.finirMission(id, "dev_verifie", "no prod on the card: dev checked on "+shaCourt(head))
			return
		}
		s.accordOuDeployer(m, p.Nom, head)
	case "accord_prod":
		ap := s.accordProd(m, p.Nom, r.Commit)
		bornerApprobation(&ap)
		x, lu := s.approbations.Lire(r.Accord)
		if r.Commit != head || lu && !maps.Equal(x.Details, ap.Details) {
			// the project, the card or what prod serves changed since: that accord is for something else, asked again on what is there now
			if lu && x.Etat == "attente" {
				s.refuserAccordMission(x.ID, "relais", "la mission a changé")
			}
			s.majRelais(id, "accord_prod", func(r *RelaisMission) { r.Etape, r.Accord = "dev_verifie", "" })
			return
		}
		if lu && x.Etat == "acceptee" && x.Utilisee == "" {
			s.accordOuDeployer(m, p.Nom, r.Commit)
		} // pending: wait; refused or expired: Ask again (POST /api/missions/{id}/redemander)
	case "prod_deploye":
		h := historiqueDeploiements(p.Nom, env)
		j := slices.IndexFunc(h, func(d Deploiement) bool { return d.ID == r.DeploiementProd })
		if j < 0 {
			return
		}
		d := h[len(h)-1] // the newest since ours: Retry makes a new one
		e := s.acces.Environnements(p.Nom)[env]
		switch {
		case d.Etat == "en_cours" || d.Etat == "ok" && d.Verification == nil:
		case d.Etat == "ok" && d.Verification.OK && !d.Retour:
			s.finirMission(id, "prod_deploye", "prod checked: "+e.URL+" (version "+shaCourt(d.Commit)+")")
		default: // P6: the environment's incident carries Retry, Go back and Take over
			s.noterJournalMission(id, s.entreeJournal("incident:"+d.ID, time.Now(), "erreur",
				"Production incident on {0}: retry, go back to the previous version or take over", cmp.Or(hoteEnv(&e), p.Nom)))
		}
	}
}

// prodPrete: every Servers task of the mission for the project's Production environment env is done.
func prodPrete(vues []VoieMission, projet, env string) bool {
	for _, v := range vues {
		if v.Module != "servers" {
			continue
		}
		if v.Etat == "attente_plan" && v.Env == projet+"/"+env { // M249: its preparation has not even started
			return false
		}
		if t, ok := tacheMission(v.Ref); ok && t.Env == projet+"/"+env && t.Etat != "finie" {
			return false
		}
	}
	return true
}

// accordOuDeployer: the owner's accord for sha used now (the deployment starts), or asked (the relay waits for it).
func (s *Serveur) accordOuDeployer(m Mission, projet, sha string) {
	ap := s.accordProd(m, projet, sha)
	env := ap.Env
	y, etat, err := s.approbations.Reprendre(ap)
	switch {
	case err != nil:
		return
	case etat == "utilisee":
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "relais", Vers: "hub", Mission: m.ID, Projet: projet, Env: env,
			Agent: "relais", Action: "approbation utilisée: " + actionDeploiement, Resultat: y.ID})
		s.deployerProdMission(m.ID, projet, env, sha, y.ID)
		return
	case etat == "":
		if y, err = s.demanderApprobation(ap); err != nil {
			return
		}
	}
	s.majRelais(m.ID, m.Relais.Etape, func(r *RelaisMission) { r.Etape, r.Commit, r.Accord = "accord_prod", sha, y.ID })
}

// deployerProdMission: the engine started on prod with the owner's accord; not started → the accord given back
// (the next step tries again) and said in the journal.
func (s *Serveur) deployerProdMission(id, projet, env, sha, accord string) {
	o := OptionsDeploiement{Auteur: "relais", Accorde: true} // the accord showed the build files (accordProd)
	if s.acces.Environnements(projet)[env].Deploiement == nil {
		o.Config = s.configProdMission(projet, env) // prod's first deployment: the method dev was checked with (recorded on prod once it went through)
	}
	d, suite, err := commencerDeploiementMission(s, projet, env, sha, o)
	if err != nil {
		s.approbations.Rendre(accord)
		s.noterJournalMission(id, s.entreeJournal("prod:"+accord+":non_lance", time.Now(), "erreur",
			"The production deployment did not start: {0}", err.Error()))
		s.majRelais(id, "", func(r *RelaisMission) { r.Etape, r.Commit, r.Accord = "accord_prod", sha, accord })
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "relais", Vers: "hub", Mission: id, Projet: projet, Env: env,
		Agent: "relais", Action: "déploiement prod de la mission", Resultat: "commit " + sha + " → " + d.ID})
	s.majRelais(id, "", func(r *RelaisMission) {
		r.Etape, r.Commit, r.Accord, r.DeploiementProd = "prod_deploye", sha, accord, d.ID
	})
	go func() {
		if d, _ := suite(context.Background()); d.Etat != "ok" || d.Verification != nil && !d.Verification.OK {
			s.notifierIncidents()
		}
	}()
}

// majRelais: mission id's relay changed by f, only while it is still at step depuis ("": any) and launched.
func (s *Serveur) majRelais(id, depuis string, f func(*RelaisMission)) {
	_, _ = modifierMission(id, func(m *Mission) error {
		if m.Relais == nil || m.Etat != "lancee" || depuis != "" && m.Relais.Etape != depuis {
			return errMissionInconnue
		}
		f(m.Relais)
		return nil
	})
}

func (s *Serveur) noterJournalMission(id string, e EntreeJournal) {
	_, _ = modifierMission(id, func(m *Mission) error { ajouterJournal(m, []EntreeJournal{e}); return nil })
}

// finirMission: the relay's last step reached from step depuis, the mission ended.
func (s *Serveur) finirMission(id, depuis, detail string) {
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Relais == nil || m.Etat != "lancee" || m.Relais.Etape != depuis {
			return errMissionInconnue
		}
		evts, _ := s.evenementsMission(*m) // what ended it (the last deployment, its check): an ended mission is no longer read
		ajouterJournal(m, evts)
		m.Relais.Etape, m.Etat = "termine", "terminee"
		m.Historique = append(m.Historique, evenementMission("terminee", detail))
		ajouterJournal(m, []EntreeJournal{s.entreeJournal("mission:terminee", time.Now(), "info", "Mission done: {0}", detail)})
		return nil
	})
	if err == nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "relais", Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "mission terminée", Resultat: m.Relais.Etape})
	}
}

// apiRedemanderProd: POST /api/missions/{id}/redemander: the prod accord refused or expired is asked again.
func (s *Serveur) apiRedemanderProd(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	relaisProdMu.Lock()
	m, err := modifierMission(id, func(m *Mission) error {
		if m.Etat != "lancee" || m.Relais == nil || m.Relais.Etape != "accord_prod" {
			return erreurMission{http.StatusConflict, "This mission does not wait for a production accord"}
		}
		if x, ok := s.approbations.Lire(m.Relais.Accord); ok && (x.Etat == "attente" || x.Etat == "acceptee" && x.Utilisee == "") {
			return erreurMission{http.StatusConflict, "The production accord is already asked or given"}
		}
		m.Relais.Etape, m.Relais.Accord = "dev_verifie", ""
		return nil
	})
	relaisProdMu.Unlock()
	if err != nil {
		s.erreurMissionHTTP(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "missions", Projet: prefixeMission + id, Mission: id, Action: "accord de prod redemandé"})
	s.relaisProd(id)
	m, _ = lireMission(id)
	s.ecrireJSON(w, map[string]any{"mission": m})
}
