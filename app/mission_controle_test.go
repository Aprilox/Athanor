package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionControle (M169): pause, resume, stop and policy of a launched mission, one subtest per effect, and
// the emergency stop that pauses every launched mission.
func TestMissionControle(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	// pairing writes the config next to the test binary, which moves dossierAthanor(): written now (as TestMissionLancer)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"dev", "prod"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	p := Projet{Nom: "site", Dossier: filepath.Join(t.TempDir(), "site")}
	ecrireFichier(t, p.Demande(), "# Site\n", 0o644)
	s.config.Projets = append(s.config.Projets, p)
	for env, u := range map[string]string{"dev": "https://dev.site.test", "prod": "https://site.test"} {
		pol := map[string]string{"dev": "auto", "prod": "accord"}[env]
		if err := s.acces.DefinirEnvironnement("site", env, Environnement{URL: u, Serveurs: []string{"a1"}, Protection: "aucune", Politique: pol, DeployerAuJalon: env == "dev"}); err != nil {
			t.Fatal(err)
		}
	}
	tache := TacheServeurs{ID: "tachectl", Serveurs: []string{"a1"}, Env: "site/dev", Etat: "attente_accord", Cree: time.Now().UTC().Format(formatCree)}
	for _, f := range []string{dossierMissions(), fichierRelais(), filepath.Join(dossierTaches("a1"), tache.ID+".json")} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	if err := enregistrerTache(tache); err != nil {
		t.Fatal(err)
	}
	pilote := filepath.Join(p.Atelier(), "pilot.pid")
	actif := func() { ecrireFichier(t, pilote, "1", 0o644) } // PiloteActif: a fresh pilot.pid
	var mu sync.Mutex
	var appels []string
	noter := func(a string) { mu.Lock(); appels = append(appels, a); mu.Unlock() }
	ancienArret, ancienDemarrer, ancienUrgence := arreterPiloteMission, demarrerPiloteMission, arreterPiloteUrgence
	t.Cleanup(func() {
		arreterPiloteMission, demarrerPiloteMission, arreterPiloteUrgence = ancienArret, ancienDemarrer, ancienUrgence
	})
	arreterPiloteMission = func(_ *Serveur, p Projet) error { noter("arret " + p.Nom); return os.Remove(pilote) }
	arreterPiloteUrgence = arreterPiloteMission
	demarrerPiloteMission = func(s *Serveur, p Projet) (string, error) {
		if id := missionBloquee("dev", p.Nom); id != "" { // as demarrerPilote
			return "", os.ErrPermission
		}
		noter("demarrer " + p.Nom)
		actif()
		return "Pilot started", nil
	}
	vider := func() []string { mu.Lock(); defer mu.Unlock(); a := appels; appels = nil; return a }

	carte, _ := json.Marshal(CarteMission{Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.site.test", Serveurs: []string{"a1"}},
		{Nom: "prod", URL: "https://site.test", Serveurs: []string{"a1"}}}, Acces: []AccesCarte{{Entree: "Clé Stripe", ID: "x1"}}})
	mission := func(id string) Mission {
		t.Helper()
		m := Mission{ID: id, Phrase: "un site", Etat: "lancee", Carte: carte, Politique: "prod", Cree: time.Now().UTC().Format(formatCree),
			Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}, {Module: "servers", Ref: tache.ID, Etat: "lancee"}},
			Historique: []EvenementMission{evenementMission("lancee", "proprietaire")}, Relais: &RelaisMission{Etape: "construction"}}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(id, action, corps string) (int, Mission, string) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/missions/"+id+"/"+action, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v struct{ Mission Mission }
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v.Mission, w.Body.String()
	}
	mdp := `{"mot_de_passe":"` + mdpPreuveTest + `"}`
	m := mission("missionctl")
	actif()

	t.Run("pause", func(t *testing.T) {
		if c, x, b := appel(m.ID, "pause", ""); c != 200 || x.Etat != "en_pause" || x.Journal[len(x.Journal)-1].Cle != "Mission paused: {0}" {
			t.Fatalf("pause: %d %s", c, b)
		}
		if a := vider(); !slices.Equal(a, []string{"arret site"}) || p.PiloteActif() {
			t.Fatalf("the pilot is not stopped: %v", a)
		}
		if c, _, _ := appel(m.ID, "pause", ""); c != http.StatusConflict {
			t.Fatalf("paused twice: %d", c)
		}
	})

	t.Run("verrou", func(t *testing.T) {
		if id := missionBloquee("dev", "site"); id != m.ID || missionBloquee("servers", tache.ID) != m.ID || missionBloquee("dev", tache.ID) != "" {
			t.Fatalf("missionBloquee: %q", id)
		}
		if _, err := s.demarrerPilote(p); err == nil || !strings.Contains(err.Error(), "paused") {
			t.Fatalf("demarrerPilote of a paused mission: %v", err)
		}
		ecrireRelais("site", RelaisJalon{Jalon: "M1", Commit: strings.Repeat("ab12", 10), Etat: "attente", Date: time.Now().UTC().Format(time.RFC3339)})
		if fini := s.avancerRelais(p, time.Now()); fini != nil || lireRelais()["site"].Etat != "attente" {
			t.Fatalf("the milestone relay deployed a paused mission: %+v", lireRelais()["site"])
		}
		x := SessionHub{Projet: prefixeTache + tache.ID, Mission: tache.ID, Agent: agentServeurs}
		if err := s.planOuRegle(x, "a1", &Etape{Outil: "commande", Classe: ClasseChangement}); err == nil || !strings.Contains(err.Error(), "paused") {
			t.Fatalf("a Servers plan step of a paused mission: %v", err)
		}
		s.relaisProd(m.ID) // the prod relay reads the state: nothing asked
		if l, _ := s.approbations.Liste(); len(l) != 0 {
			t.Fatalf("the prod relay ran in pause: %+v", l)
		}
	})

	t.Run("reprendre", func(t *testing.T) {
		for _, corps := range []string{"", `{"mot_de_passe":"faux"}`} {
			if c, _, _ := appel(m.ID, "reprendre", corps); c != http.StatusUnauthorized {
				t.Fatalf("resume without the password (%s): %d", corps, c)
			}
		}
		if x, _ := lireMission(m.ID); x.Etat != "en_pause" {
			t.Fatalf("resumed without the password: %s", x.Etat)
		}
		if c, x, b := appel(m.ID, "reprendre", mdp); c != 200 || x.Etat != "lancee" || missionBloquee("dev", "site") != "" {
			t.Fatalf("resume: %d %s", c, b)
		}
		if a := vider(); !slices.Equal(a, []string{"demarrer site"}) {
			t.Fatalf("the pilot is not started again: %v", a)
		}
	})

	t.Run("politique", func(t *testing.T) {
		d, err := s.approbations.Demander(Approbation{Projet: "site", Env: "dev", Agent: "pilote", Action: actionDeploiement, Raison: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.approbations.Decider(d.ID, "toujours", "proprietaire"); err != nil {
			t.Fatal(err)
		}
		regle := RegleAccord{Projet: "site", Env: "dev", Action: actionDeploiement}
		if !s.approbations.Regle(regle) {
			t.Fatal("no rule to remove")
		}
		// stricter: at once, without the password; dev waits for the owner and its rule ends, prod stays accord
		if c, x, b := appel(m.ID, "politique", `{"politique":"tout"}`); c != 200 || x.Politique != "tout" {
			t.Fatalf("stricter: %d %s", c, b)
		}
		envs := s.acces.Environnements("site")
		if envs["dev"].Politique != "accord" || envs["prod"].Politique != "accord" || s.approbations.Regle(regle) {
			t.Fatalf("tout: dev %s, prod %s, rule %v", envs["dev"].Politique, envs["prod"].Politique, s.approbations.Regle(regle))
		}
		// looser: the password; prod stays accord
		if c, _, _ := appel(m.ID, "politique", `{"politique":"autonome"}`); c != http.StatusUnauthorized {
			t.Fatalf("looser without the password: %d", c)
		}
		if c, x, b := appel(m.ID, "politique", `{"politique":"autonome","mot_de_passe":"`+mdpPreuveTest+`"}`); c != 200 || x.Politique != "autonome" {
			t.Fatalf("looser: %d %s", c, b)
		}
		if envs = s.acces.Environnements("site"); envs["dev"].Politique != "auto" || envs["prod"].Politique != "accord" {
			t.Fatalf("autonome: dev %s, prod %s", envs["dev"].Politique, envs["prod"].Politique)
		}
		if c, _, _ := appel(m.ID, "politique", `{"politique":"partout"}`); c != http.StatusBadRequest {
			t.Fatalf("unknown policy: %d", c)
		}
	})

	t.Run("arreter", func(t *testing.T) {
		// what the mission holds: its orchestrator's, chef ops' and pilot's tokens, a running task, pending accords
		jeton := func(x SessionHub) string {
			j, err := s.jetons.CreerSession(x)
			if err != nil {
				t.Fatal(err)
			}
			return hacherJeton(j)
		}
		orch := jeton(SessionHub{Projet: prefixeMission + m.ID, Mission: m.ID, Agent: "orchestrateur"})
		ops := jeton(SessionHub{Projet: prefixeTache + tache.ID, Mission: tache.ID, Agent: agentServeurs})
		pil := jeton(SessionHub{Projet: "site", Agent: "pilote"})
		autre := jeton(SessionHub{Projet: "autre", Agent: "pilote"})
		ctx, annuler := context.WithCancelCause(context.Background())
		tachesEnCours.Lock()
		tachesEnCours.m[tache.ID] = &tacheEnCours{t: tache, annuler: annuler}
		tachesEnCours.Unlock()
		t.Cleanup(func() { tachesEnCours.Lock(); delete(tachesEnCours.m, tache.ID); tachesEnCours.Unlock() })
		demander := func(a Approbation) string {
			x, err := s.approbations.Demander(a)
			if err != nil {
				t.Fatal(err)
			}
			return x.ID
		}
		aMission := demander(Approbation{Projet: "site", Env: "prod", Mission: m.ID, Agent: "relais", Action: actionDeploiement, Raison: "prod", Details: map[string]string{"mission": m.ID}})
		aPlan := demander(Approbation{Projet: prefixeTache + tache.ID, Mission: tache.ID, Agent: agentServeurs, Action: actionPlanServeur, Raison: "plan", Details: map[string]string{"serveur": "a1"}})
		aAutre := demander(Approbation{Projet: "autre", Env: "dev", Agent: "pilote", Action: actionDeploiement, Raison: "autre"})

		if c, x, b := appel(m.ID, "arreter", ""); c != 200 || x.Etat != "arretee" {
			t.Fatalf("stop: %d %s", c, b)
		}
		t.Run("pilote", func(t *testing.T) {
			if a := vider(); !slices.Equal(a, []string{"arret site"}) {
				t.Fatalf("the pilot is not stopped: %v", a)
			}
		})
		t.Run("taches", func(t *testing.T) {
			if context.Cause(ctx) != errTacheArretee {
				t.Fatalf("the Servers task is not cancelled: %v", context.Cause(ctx))
			}
		})
		t.Run("jetons", func(t *testing.T) {
			restent := map[string]bool{}
			for _, x := range s.jetons.Liste() {
				restent[x.Hache] = true
			}
			if restent[orch] || restent[ops] || restent[pil] || !restent[autre] {
				t.Fatalf("tokens: orchestrator %v, chef ops %v, pilot %v, another project %v", restent[orch], restent[ops], restent[pil], restent[autre])
			}
		})
		t.Run("accords", func(t *testing.T) {
			for id, etat := range map[string]string{aMission: "refusee", aPlan: "refusee", aAutre: "attente"} {
				if x, _ := s.approbations.Lire(id); x.Etat != etat {
					t.Fatalf("%s %s: %s, expected %s", x.Action, x.Projet, x.Etat, etat)
				}
			}
		})
		t.Run("deploiement_auto_jalon", func(t *testing.T) {
			if s.acces.Environnements("site")["dev"].DeployerAuJalon {
				t.Fatal("dev still deployed after each milestone")
			}
		})
		t.Run("journal", func(t *testing.T) {
			x, _ := lireMission(m.ID)
			if !slices.ContainsFunc(x.Journal, func(e EntreeJournal) bool { return strings.HasPrefix(e.ID, "arret:2") }) {
				t.Fatalf("the journal does not say the stop: %+v", x.Journal)
			}
			for _, id := range []string{"arret:env:dev", "arret:env:prod", "arret:serveur:a1", "arret:acces"} {
				if !slices.ContainsFunc(x.Journal, func(e EntreeJournal) bool { return e.ID == id }) {
					t.Fatalf("the journal does not say %s: %+v", id, x.Journal)
				}
			}
			if i := slices.IndexFunc(x.Journal, func(e EntreeJournal) bool { return e.ID == "arret:serveur:a1" }); x.Journal[i].Params[0] != "web-1" {
				t.Fatalf("the server's name: %+v", x.Journal[i])
			}
		})
		t.Run("fin", func(t *testing.T) {
			for _, a := range []string{"pause", "arreter"} { // resumed, it can (M250: TestMissionArretReprise)
				if c, _, _ := appel(m.ID, a, mdp); c != http.StatusConflict {
					t.Fatalf("%s a stopped mission: %d", a, c)
				}
			}
			if id := missionBloquee("dev", "site"); id != "" {
				t.Fatalf("a stopped mission still locks its project: %s", id)
			}
		})
	})

	t.Run("telephone", func(t *testing.T) {
		tel := mission("missiontel")
		s.config.Distance.Actif = true
		s.dist().tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
		h := s.gestionDistance(mux)
		cle := appairerTest(t, s, h)
		api := []string{"X-Athanor", "jeton"}
		if w := appelDistance(h, "POST", "/api/missions/"+tel.ID+"/pause", cle, "", api...); w.Code == 200 {
			t.Fatalf("a locked phone pauses: %d", w.Code)
		}
		if w := appelDistance(h, "POST", "/api/deverrouiller", cle, mdp, api...); w.Code != 200 {
			t.Fatalf("phone unlocks: %d %s", w.Code, w.Body)
		}
		for _, a := range []struct{ action, etat string }{{"pause", "en_pause"}, {"reprendre", "lancee"}, {"arreter", "arretee"}} {
			if w := appelDistance(h, "POST", "/api/missions/"+tel.ID+"/"+a.action, cle, "", api...); w.Code != 200 {
				t.Fatalf("phone %s: %d %s", a.action, w.Code, w.Body)
			}
			if x, _ := lireMission(tel.ID); x.Etat != a.etat {
				t.Fatalf("phone %s: %s", a.action, x.Etat)
			}
		}
		vider()
	})

	t.Run("urgence", func(t *testing.T) {
		u := mission("missionurg")
		s.arretUrgence("proprietaire")
		if x, _ := lireMission(u.ID); x.Etat != "en_pause" || x.Historique[len(x.Historique)-1].Detail != "emergency stop" {
			t.Fatalf("the emergency stop did not pause the mission: %s %+v", x.Etat, x.Historique)
		}
		if x, _ := lireMission(m.ID); x.Etat != "arretee" {
			t.Fatalf("a stopped mission changed: %s", x.Etat)
		}
	})
}
