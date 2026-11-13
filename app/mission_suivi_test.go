package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMissionSuivi (M167): a launched mission followed on a fake pilot and fake sources: each transition (milestone
// delivered, dev deployed then checked, Servers plan waiting then done, accord asked then given, Studio lane
// finished) gives one journal entry and moves the dev relay; reading again adds nothing; no witness value in the
// mission's file; GET /api/missions/{id} gives the relay and the journal; the follower stops with the mission.
func TestMissionSuivi(t *testing.T) {
	s := serveurCoffreTest(t)
	temoin := "TEMOIN-" + aleatoire(10)
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé", Famille: "secret", Type: "stripe"}, temoin); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"dev"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	p := Projet{Nom: "site", Dossier: filepath.Join(t.TempDir(), "site")}
	s.config.Projets = append(s.config.Projets, p)
	tache := TacheServeurs{ID: "tache01", Serveurs: []string{"a1"}, Etat: "preparation", Cree: time.Now().UTC().Format(formatCree)}
	for _, f := range []string{dossierMissions(), fichierJalonsFaits(), cheminHistorique("site", "dev"), filepath.Join(dossierTaches("a1"), "tache01.json")} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	ecrireFichier(t, filepath.Join(p.Atelier(), "pilot.pid"), "1", 0o644) // the fake pilot works
	lance := time.Now().Add(-time.Minute)
	carte, _ := json.Marshal(CarteMission{Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.site.test"}}})
	m := Mission{ID: "suivi01", Phrase: "un site", Etat: "lancee", Carte: carte, Politique: "prod", Cree: lance.UTC().Format(formatCree),
		Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}, {Module: "servers", Ref: "tache01", Etat: "lancee"}},
		Historique: []EvenementMission{{T: lance.UTC().Format(formatCree), Etat: "lancee", Detail: "proprietaire"}}}
	if err := enregistrerMission(m); err != nil {
		t.Fatal(err)
	}
	if err := noterJalonFait(fichierJalonsFaits(), p.Dossier, "M0 : avant la mission", lance.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	etape := func(nom string, attendu int, relais string) Mission {
		t.Helper()
		for range 2 { // the second reading must add nothing
			if !s.observerMission(m.ID) {
				t.Fatalf("%s: the mission is no longer followed", nom)
			}
		}
		lu, _ := lireMission(m.ID)
		if len(lu.Journal) != attendu || lu.Relais == nil || lu.Relais.Etape != relais {
			t.Fatalf("%s: %d entries (want %d), relay %+v (want %s): %+v", nom, len(lu.Journal), attendu, lu.Relais, relais, lu.Journal)
		}
		return lu
	}
	etape("no source yet (a milestone before the launch)", 0, "construction")

	_ = noterJalonFait(fichierJalonsFaits(), p.Dossier, "M1 : accueil "+temoin, time.Now())
	lu := etape("milestone", 1, "construction")
	if e := lu.Journal[0]; !strings.HasPrefix(e.ID, "jalon:M1 : accueil ") || strings.Contains(e.ID, temoin) || e.Cle != "Milestone delivered: {0}" || strings.Contains(e.Params[0], temoin) {
		t.Fatalf("milestone entry: %+v", e)
	}

	sha := strings.Repeat("ab12", 10)
	d := Deploiement{ID: "dep1", Projet: "site", Env: "dev", Commit: sha, Etat: "ok", Debut: time.Now().UTC().Format(time.RFC3339), Serveurs: []string{"a1"}}
	_ = noterHistorique(d)
	lu = etape("dev deployed", 2, "dev_deploye")
	if lu.Relais.Commit != sha || lu.Relais.DeploiementDev != "dep1" || entree(lu, "deploi:dep1:ok").Params[1] != "ab12ab1" {
		t.Fatalf("dev deployment: %+v %+v", lu.Relais, lu.Journal)
	}
	d.Verification = &VerifDeploiement{OK: true, T: time.Now().UTC().Format(time.RFC3339)}
	_ = noterHistorique(d)
	etape("dev checked", 3, "dev_verifie")

	tache.Etat, tache.Plans = "attente_accord", []string{"p1"}
	_ = enregistrerTache(tache)
	lu = etape("plan waiting", 4, "dev_verifie")
	if e := entree(lu, "plan:tache01:1"); e.Niveau != "attention" || e.Params[0] != "web-1" {
		t.Fatalf("plan entry: %+v", e)
	}
	tache.Etat, tache.Fin = "finie", time.Now().UTC().Format(formatCree)
	_ = enregistrerTache(tache)
	etape("plan done", 5, "dev_verifie")

	x, err := s.approbations.Demander(Approbation{Projet: "site", Env: "prod", Mission: m.ID, Agent: "athanor", Action: "deployer", Raison: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	etape("accord asked", 6, "dev_verifie")
	if _, err := s.approbations.Decider(x.ID, "accepter", "proprietaire"); err != nil {
		t.Fatal(err)
	}
	etape("accord given", 7, "dev_verifie")

	_ = os.Remove(filepath.Join(p.Atelier(), "pilot.pid"))
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644)
	lu = etape("studio lane finished", 8, "dev_verifie")
	if b, _ := os.ReadFile(filepath.Join(dossierMissions(), m.ID+".json")); strings.Contains(string(b), temoin) {
		t.Fatal("the witness value is in the mission's file")
	}

	mux := http.NewServeMux()
	s.routes(mux)
	r := httptest.NewRequest("GET", "/api/missions/"+m.ID, nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var rep struct{ Mission Mission }
	if json.Unmarshal(w.Body.Bytes(), &rep) != nil || len(rep.Mission.Journal) != 8 || rep.Mission.Relais == nil || rep.Mission.Relais.Etape != "dev_verifie" {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}

	// the follower: started once, it reads by itself, and stops with the mission
	ancien := pasSuiviMission
	pasSuiviMission = 10 * time.Millisecond
	t.Cleanup(func() { pasSuiviMission = ancien })
	_ = noterJalonFait(fichierJalonsFaits(), p.Dossier, "M2 : contact", time.Now())
	s.suivreMission(m.ID)
	s.suivreMission(m.ID)
	attendreQue(t, func() bool { lu, _ = lireMission(m.ID); return len(lu.Journal) == 9 })
	_, _ = modifierMission(m.ID, func(m *Mission) error { m.Etat = "terminee"; return nil })
	attendreQue(t, func() bool { _, suivi := suivisMission.Load(m.ID); return !suivi })
	if s.observerMission(m.ID) {
		t.Fatal("an ended mission is still followed")
	}
}

func attendreQue(t *testing.T, f func() bool) {
	t.Helper()
	for fin := time.Now().Add(5 * time.Second); !f(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(fin) {
			t.Fatal("timed out")
		}
	}
}

func entree(m Mission, id string) EntreeJournal {
	for _, e := range m.Journal {
		if e.ID == id {
			return e
		}
	}
	return EntreeJournal{Params: []string{"", ""}}
}
