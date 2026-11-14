package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMissionDevDistant (M190, S11: a mission toward a remote Dev): the card designates jeu@Atelier, a project of a
// paired remote that carries dev; the Go makes it a remote Dev lane (no environment, server or access: those are the
// remote's own); Lancer sends the brief then starts its pilot through its MCP; the follower reads its milestones;
// pause, resume and stop reach its pilot; the remote offline: the mission waits and says so, then sends the rest;
// the remote's request done ends the mission without any prod accord or deployment, even in autonome.
func TestMissionDevDistant(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dossierMissions(), cheminDistants()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	un := &fauxDevDistant{jeton: "jeton-un", outils: []string{"etat", "projets", "pilote", "ajouter_demande"},
		projets: []map[string]any{{"nom": "jeu", "pilote_actif": false, "phase": "", "plan": "3/10 milestones", "jalon": "M4"}}}
	deux := &fauxDevDistant{jeton: "jeton-deux", outils: []string{"etat", "projets", "pilote"},
		projets: []map[string]any{{"nom": "site", "plan": "1/4 milestones"}}}
	h1, h2 := httptest.NewServer(un), httptest.NewServer(deux)
	defer h2.Close()
	m := s.dst()
	m.mu.Lock()
	m.liste = []Distant{{ID: "d1", Nom: "Atelier", URL: h1.URL}, {ID: "d2", Nom: "Portable", URL: h2.URL}}
	m.jetons["d1"], m.jetons["d2"] = un.jeton, deux.jeton
	m.mu.Unlock()
	s.rafraichirDistants()
	locaux := len(s.config.Projets)

	brief := "Ajouter un niveau de neige au jeu"
	carte := func(nom string) CarteMission {
		return CarteMission{Titre: "Niveau de neige", Projet: ProjetCarte{Nom: nom}, Dev: BriefCarte{Brief: brief}, JalonsEstimes: 3}
	}
	t.Run("carte", func(t *testing.T) {
		x := s.contexteCarte()
		for _, nom := range []string{"jeu@Atelier", "JEU@atelier", "jeu@d1"} {
			c := carte(nom)
			c.Projet.Distant = "d9/autre" // the agent's: replaced
			if err := validerCarte(&c, x); err != nil || c.Projet.Distant != "d1/jeu" || c.Projet.Nom != "jeu@Atelier" {
				t.Fatalf("%s: %v %+v", nom, err, c.Projet)
			}
		}
		refus := map[string]func(*CarteMission){
			"prod":     func(c *CarteMission) { c.Envs = []EnvCarte{{Nom: "prod", URL: "https://jeu.test"}} },
			"acces":    func(c *CarteMission) { c.Acces = []AccesCarte{{Entree: "Clé", Pour: c.Projet.Nom}} },
			"modes":    func(c *CarteMission) { c.Dev.Modes = &ModesCarte{Rapide: true} },
			"nouveau":  func(c *CarteMission) { c.Projet.Nouveau = true },
			"sans dev": func(c *CarteMission) { c.Projet.Nom = "site@Portable" }, // remote 2 did not grant ajouter_demande
			"inconnu":  func(c *CarteMission) { c.Projet.Nom = "jeu@Portable" },
		}
		for nom, f := range refus {
			c := carte("jeu@Atelier")
			f(&c)
			if err := validerCarte(&c, x); err == nil {
				t.Errorf("%s: card accepted %+v", nom, c.Projet)
			}
		}
		c := carte("jeu@Atelier")
		if c = calculerCarte(c, "autonome", x); len(c.Approbations) != 0 {
			t.Errorf("approvals planned for a remote lane: %+v", c.Approbations)
		}
	})

	lancer := func(id, politique string) Mission {
		t.Helper()
		b, _ := json.Marshal(carte("jeu@Atelier"))
		if err := enregistrerMission(Mission{ID: id, Phrase: "un niveau de neige @jeu@Atelier", Etat: "proposee", Carte: b, Politique: politique,
			Cree: time.Now().UTC().Format(formatCree), Historique: []EvenementMission{evenementMission("proposee", "")}}); err != nil {
			t.Fatal(err)
		}
		x, err := s.lancerMission(id, "proprietaire", "", "")
		if err != nil || x.Etat != "lancee" || len(x.Voies) != 1 || x.Voies[0].Module != moduleDevDistant || x.Voies[0].Ref != "d1/jeu" {
			t.Fatalf("launch: %v %+v", err, x)
		}
		return x
	}
	controle := func(id, action, corps string) Mission {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/missions/"+id+"/"+action, strings.NewReader(corps))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.apiControleMission(action)(w, r)
		var v struct{ Mission Mission }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
		return v.Mission
	}
	journal := func(id, cle string) []EntreeJournal {
		x, _ := lireMission(id)
		var l []EntreeJournal
		for _, e := range x.Journal {
			if e.Cle == cle {
				l = append(l, e)
			}
		}
		return l
	}

	a := lancer("missiondista", "autonome")
	t.Run("lancer", func(t *testing.T) {
		if a.Voies[0].Etat != "lancee" || !slices.Equal(un.appelsDe("ajouter_demande"), []string{"ajouter_demande jeu"}) ||
			!slices.Equal(un.appelsDe("pilote"), []string{"pilote jeu demarrer"}) || len(un.demandes) != 1 || un.demandes[0] != brief {
			t.Fatalf("remote calls: %v %v (lane %+v)", un.appels, un.demandes, a.Voies)
		}
		if len(s.config.Projets) != locaux {
			t.Error("a local project was created for a remote lane")
		}
		v := s.voiesEnDirect(a).Voies[0]
		if v.Actuel != "travaille" || v.Nom != "jeu@Atelier" || v.Fait != 3 || v.Total != 10 || v.Detail != "M4" {
			t.Errorf("remote lane live: %+v", v)
		}
	})

	t.Run("suivi", func(t *testing.T) {
		s.observerMission(a.ID)
		un.mu.Lock()
		un.projets[0]["plan"], un.projets[0]["jalon"] = "5/10 milestones", "M6"
		un.mu.Unlock()
		s.rafraichirDistants()
		s.observerMission(a.ID)
		l := journal(a.ID, "{0}: {1} / {2} milestones done")
		if len(l) != 2 || !slices.Equal(l[1].Params, []string{"jeu@Atelier", "5", "10"}) {
			t.Fatalf("milestones in the journal: %+v", l)
		}
	})

	t.Run("pause et reprise", func(t *testing.T) {
		if x := controle(a.ID, "pause", ""); x.Etat != "en_pause" || !slices.Contains(un.appelsDe("pilote"), "pilote jeu pause_fin_cycle") {
			t.Fatalf("pause: %s %v", x.Etat, un.appelsDe("pilote"))
		}
		n := len(un.appelsDe("pilote"))
		if x := controle(a.ID, "reprendre", `{"mot_de_passe":"`+mdpPreuveTest+`"}`); x.Etat != "lancee" || len(un.appelsDe("pilote")) != n+1 || un.appelsDe("pilote")[n] != "pilote jeu demarrer" {
			t.Fatalf("resume: %s %v", x.Etat, un.appelsDe("pilote"))
		}
	})

	t.Run("hors ligne", func(t *testing.T) {
		h1.Close()
		s.rafraichirDistants()
		b := lancer("missiondistb", "prod")
		if b.Voies[0].Etat != "a_envoyer" || !slices.ContainsFunc(b.Historique, func(e EvenementMission) bool { return strings.Contains(e.Detail, "waits for jeu@Atelier") }) {
			t.Fatalf("offline launch: %+v %+v", b.Voies, b.Historique)
		}
		for _, id := range []string{a.ID, b.ID} {
			if !s.observerMission(id) || len(journal(id, "The remote Athanor {0} is offline: the mission waits for it")) != 1 {
				t.Fatalf("%s: the offline remote is not said", id)
			}
			s.observerMission(id) // said once per absence
			if x, _ := lireMission(id); x.Etat != "lancee" || len(journal(id, "The remote Athanor {0} is offline: the mission waits for it")) != 1 {
				t.Fatalf("%s offline: %s %+v", id, x.Etat, x.Journal)
			}
		}
		if v := s.voiesEnDirect(b).Voies[0]; v.Actuel != "hors_ligne" {
			t.Errorf("offline lane: %+v", v)
		}
		// M190: a pause sent while offline is kept on the lane, then taken by the remote on its return
		if x := controle(a.ID, "pause", ""); x.Etat != "en_pause" {
			t.Fatalf("offline pause: %s", x.Etat)
		}
		if x, _ := lireMission(a.ID); x.Voies[0].Ordre != "pause_fin_cycle" || !s.renvoyerOrdresDistants(lireMissionT(t, a.ID)) {
			t.Fatalf("offline pause not kept: %+v", x.Voies)
		}
		nPause := len(un.appelsDe("pilote"))
		// back online (a new address): the follower sends what the lane still owes, once
		h1 = httptest.NewServer(un)
		m.mu.Lock()
		m.liste[0].URL = h1.URL
		m.mu.Unlock()
		s.rafraichirDistants()
		s.observerMission(b.ID)
		s.observerMission(b.ID)
		if x, _ := lireMission(b.ID); x.Voies[0].Etat != "lancee" || len(un.demandes) != 2 {
			t.Fatalf("not sent once back: %+v %v", x.Voies, un.demandes)
		}
		if s.renvoyerOrdresDistants(lireMissionT(t, a.ID)) || !slices.Contains(un.appelsDe("pilote")[nPause:], "pilote jeu pause_fin_cycle") {
			t.Fatalf("offline pause not sent on return: %v", un.appelsDe("pilote")[nPause:])
		}
		if x, _ := lireMission(a.ID); x.Voies[0].Ordre != "" || len(journal(a.ID, "The remote Athanor took on its return the order {0} for {1}")) != 1 {
			t.Fatalf("order still owed: %+v %+v", x.Voies, x.Journal)
		}
		if x := controle(a.ID, "reprendre", `{"mot_de_passe":"`+mdpPreuveTest+`"}`); x.Etat != "lancee" {
			t.Fatalf("resume after return: %s", x.Etat)
		}
		if x := controle(b.ID, "arreter", ""); x.Etat != "arretee" || !slices.Contains(un.appelsDe("pilote"), "pilote jeu arreter") {
			t.Fatalf("stop: %s %v", x.Etat, un.appelsDe("pilote"))
		}
	})
	defer h1.Close()

	t.Run("fin sans prod", func(t *testing.T) {
		un.mu.Lock()
		un.projets[0]["pilote_actif"], un.projets[0]["phase"], un.projets[0]["plan"] = false, "termine", "10/10 milestones"
		un.mu.Unlock()
		s.rafraichirDistants()
		s.observerMission(a.ID)
		x, _ := lireMission(a.ID)
		if x.Etat != "terminee" || x.Relais == nil || x.Relais.Etape != "termine" || len(journal(a.ID, "The Studio lane finished the request of {0}")) != 1 {
			t.Fatalf("remote request done: %s %+v %+v", x.Etat, x.Relais, x.Journal)
		}
		if l, _ := s.approbations.Liste(); len(l) != 0 {
			t.Errorf("an accord was asked for a remote lane: %+v", l)
		}
	})

	for _, f := range []*fauxDevDistant{un, deux} {
		f.mu.Lock()
		for _, c := range f.appels {
			if nom, _, _ := strings.Cut(c, " "); !slices.Contains(f.outils, nom) {
				t.Errorf("the hub asked a tool not granted: %s", c)
			}
		}
		f.mu.Unlock()
	}
	if j := lireJournalAuditTest(); !strings.Contains(j, "ajouter_demande") || !strings.Contains(j, moduleDevDistant) {
		t.Error("the remote mission's orders are not in the audit log")
	}
}

func lireMissionT(t *testing.T, id string) Mission {
	t.Helper()
	m, err := lireMission(id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
