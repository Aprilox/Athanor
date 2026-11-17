package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMissionRelaisProd (M168): the prod relay of a launched mission. Never a prod deployment without the owner's
// accord, in every policy (prod, tout, autonome); an accepted accord of another mission or of another SHA lets
// nothing through; the accord is used once; a card changed since → the accord is refused; refused → Ask again;
// a failed prod deployment → the incident in the journal, then Retry checked → the mission ends; no prod on the
// card → the mission ends once dev is checked.
func TestMissionRelaisProd(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s) // the infra key open: a mission on servers waits for it (R7)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"dev", "prod"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	depot, _, head := depotSiteExemple(t)
	p := Projet{Nom: "site", Dossier: depot}
	s.config.Projets = append(s.config.Projets, p)
	for env, u := range map[string]string{"dev": "https://dev.site.test", "prod": "https://site.test"} {
		if err := s.acces.definirAncien("site", env, Environnement{URL: u, Serveurs: []string{"a1"}, Protection: "aucune", Politique: "accord"}); err != nil {
			t.Fatal(err)
		}
	}
	tache := TacheServeurs{ID: "tacheprod", Serveurs: []string{"a1"}, Env: "site/prod", Etat: "attente_accord", Cree: time.Now().UTC().Format(formatCree)}
	for _, f := range []string{dossierMissions(), fichierJalonsFaits(), cheminHistorique("site", "dev"), cheminHistorique("site", "prod"), filepath.Join(dossierTaches("a1"), tache.ID+".json")} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	if err := enregistrerTache(tache); err != nil {
		t.Fatal(err)
	}
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644) // the Studio lane finished
	lance := time.Now().Add(-time.Minute)
	_ = noterHistorique(Deploiement{ID: "dev1", Projet: "site", Env: "dev", Commit: head, Etat: "ok", Debut: time.Now().UTC().Format(time.RFC3339), Serveurs: []string{"a1"},
		Verification: &VerifDeploiement{OK: true, T: time.Now().UTC().Format(time.RFC3339)}})

	var lances []string // the prod deployments the engine was asked for
	ancien := commencerDeploiementMission
	t.Cleanup(func() { commencerDeploiementMission = ancien })
	commencerDeploiementMission = func(_ *Serveur, projet, env, commit string, o OptionsDeploiement) (Deploiement, func(context.Context) (Deploiement, error), error) {
		d := Deploiement{ID: "prod" + string(rune('0'+len(lances))), Projet: projet, Env: env, Commit: commit, Etat: "en_cours", Auteur: o.Auteur, Accorde: o.Accorde,
			Serveurs: []string{"a1"}, Debut: time.Now().UTC().Format(time.RFC3339)}
		lances = append(lances, env+" "+commit)
		_ = noterHistorique(d)
		return d, func(context.Context) (Deploiement, error) { return d, nil }, nil
	}

	mission := func(id, politique string, envs ...string) Mission {
		t.Helper()
		var ec []EnvCarte
		for _, e := range envs {
			ec = append(ec, EnvCarte{Nom: e})
		}
		carte, _ := json.Marshal(CarteMission{Projet: ProjetCarte{Nom: "site"}, Envs: ec})
		m := Mission{ID: id, Phrase: "un site", Etat: "lancee", Carte: carte, Politique: politique, Cree: lance.UTC().Format(formatCree),
			Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}, {Module: "servers", Ref: tache.ID, Etat: "lancee"}},
			Historique: []EvenementMission{{T: lance.UTC().Format(formatCree), Etat: "lancee", Detail: "proprietaire"}}}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	observer := func(id string) Mission {
		t.Helper()
		for range 2 {
			s.observerMission(id)
		}
		m, _ := lireMission(id)
		return m
	}
	attente := func(m Mission) Approbation {
		t.Helper()
		x, ok := s.approbations.Lire(m.Relais.Accord)
		if !ok || x.Etat != "attente" || x.Env != "prod" || x.Mission != m.ID || x.Details["sha"] != head || x.Details["empreinte"] != empreinteCarte(m) ||
			x.Details["mission"] != m.ID || x.Details["url"] != "https://site.test" || x.Details["env"] != "prod" {
			t.Fatalf("pending prod accord: %+v %+v", m.Relais, x)
		}
		return x
	}

	// 1. the prod Servers task not done yet: nothing asked; done: the accord asked, in every policy, nothing deployed
	m := mission("relaisautonome", "autonome", "dev", "prod")
	if m = observer(m.ID); m.Relais.Etape != "dev_verifie" || m.Relais.Accord != "" {
		t.Fatalf("prod task not done: %+v", m.Relais)
	}
	tache.Etat = "finie"
	_ = enregistrerTache(tache)
	for _, pol := range []string{"prod", "tout"} {
		if a := observer(mission("relais"+pol, pol, "dev", "prod").ID); a.Relais.Etape != "accord_prod" {
			t.Fatalf("%s: %+v", pol, a.Relais)
		} else {
			attente(a)
		}
	}
	m = observer(m.ID)
	x := attente(m)
	if len(lances) != 0 {
		t.Fatalf("prod deployed without an accord: %v", lances)
	}

	// 2. an accepted accord of another mission, or of another SHA, lets nothing through
	for _, autre := range []func(*Approbation){
		func(a *Approbation) { a.Mission, a.Details["mission"] = "relaisprod", "relaisprod" },
		func(a *Approbation) { a.Details["sha"] = strings.Repeat("cd34", 10) },
	} {
		a := s.accordProd(m, "site", head)
		autre(&a)
		y, err := s.approbations.Demander(a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.approbations.Decider(y.ID, "accepter", "proprietaire"); err != nil {
			t.Fatal(err)
		}
	}
	if m = observer(m.ID); m.Relais.Etape != "accord_prod" || m.Relais.Accord != x.ID || len(lances) != 0 {
		t.Fatalf("another accord let prod through: %+v %v", m.Relais, lances)
	}

	// 3. accepted → deployed once (used), and never again with the same accord
	if _, _, err := s.deciderApprobation("proprietaire", x.ID, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	m = observer(m.ID)
	if y, _ := s.approbations.Lire(x.ID); m.Relais.Etape != "prod_deploye" || m.Relais.DeploiementProd != "prod0" || len(lances) != 1 || lances[0] != "prod "+head || y.Utilisee == "" {
		t.Fatalf("accepted: %+v %v %+v", m.Relais, lances, y)
	}
	_, _ = modifierMission(m.ID, func(m *Mission) error { m.Relais.Etape, m.Relais.Accord = "dev_verifie", ""; return nil })
	if m = observer(m.ID); len(lances) != 1 || m.Relais.Etape != "accord_prod" || m.Relais.Accord == x.ID {
		t.Fatalf("an accord used twice: %+v %v", m.Relais, lances)
	}
	x = attente(m)

	// 4. the card changed since the accord was asked: accepting it is refused, a new one is asked
	_, _ = modifierMission(m.ID, func(m *Mission) error { m.Politique = "prod"; return nil })
	if _, _, err := s.deciderApprobation("proprietaire", x.ID, "accepter", "", "", ""); err != errCarteChangee {
		t.Fatalf("changed card accepted: %v", err)
	}
	if y, _ := s.approbations.Lire(x.ID); y.Etat != "refusee" {
		t.Fatalf("changed card: %+v", y)
	}
	m = observer(m.ID)
	if y := attente(m); y.ID == x.ID || len(lances) != 1 {
		t.Fatalf("not asked again: %+v %v", m.Relais, lances)
	}

	// 5. refused → the relay waits; Ask again asks a new one (409 while it waits)
	x = attente(m)
	if _, _, err := s.deciderApprobation("proprietaire", x.ID, "refuser", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if m = observer(m.ID); m.Relais.Etape != "accord_prod" || m.Relais.Accord != x.ID || len(lances) != 1 {
		t.Fatalf("refused: %+v %v", m.Relais, lances)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	redemander := func() int {
		r := httptest.NewRequest("POST", "/api/missions/"+m.ID+"/redemander", nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if c := redemander(); c != http.StatusOK {
		t.Fatalf("ask again: %d", c)
	}
	m, _ = lireMission(m.ID)
	if y := attente(m); y.ID == x.ID {
		t.Fatal("ask again: the same accord")
	}
	if c := redemander(); c != http.StatusConflict {
		t.Fatalf("ask again while waiting: %d", c)
	}

	// 6. accepted → prod fails: the incident in the journal; Retry checked → the mission ends
	if _, _, err := s.deciderApprobation("proprietaire", m.Relais.Accord, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if m = observer(m.ID); m.Relais.Etape != "prod_deploye" || len(lances) != 2 {
		t.Fatalf("second prod: %+v %v", m.Relais, lances)
	}
	h := historiqueDeploiements("site", "prod")
	d := h[len(h)-1]
	d.Etat, d.Fin = "echec", time.Now().UTC().Format(time.RFC3339)
	_ = noterHistorique(d)
	if m = observer(m.ID); m.Etat != "lancee" || entree(m, "incident:"+d.ID).Niveau != "erreur" {
		t.Fatalf("incident: %s %+v", m.Etat, m.Journal)
	}
	d = Deploiement{ID: "retry1", Projet: "site", Env: "prod", Commit: head, Etat: "ok", Auteur: "proprietaire", Debut: time.Now().UTC().Format(time.RFC3339),
		Serveurs: []string{"a1"}, Verification: &VerifDeploiement{OK: true, T: time.Now().UTC().Format(time.RFC3339)}}
	_ = noterHistorique(d)
	s.relaisProd(m.ID) // checked between two readings of the follower: its events still written as the mission ends
	if m, _ = lireMission(m.ID); m.Etat != "terminee" || m.Relais.Etape != "termine" || entree(m, "mission:terminee").Cle == "" ||
		entree(m, "deploi:retry1:ok").Cle == "" || entree(m, "verif:retry1:ok").Cle == "" {
		t.Fatalf("retry checked: %s %+v", m.Etat, m.Relais)
	}

	// 7. no prod on the card: ended once dev is checked, nothing asked
	sans := observer(mission("relaisdev", "autonome", "dev").ID)
	if sans.Etat != "terminee" || sans.Relais.Etape != "termine" || sans.Relais.Accord != "" || len(lances) != 2 {
		t.Fatalf("no prod: %s %+v %v", sans.Etat, sans.Relais, lances)
	}

	// 8. M271: the mission's Ignore permissions mode: the prod accord accepted at once (no ping, no password), deployed, audited
	ig := mission("relaisignorer", "prod", "dev", "prod")
	_, _ = modifierMission(ig.ID, func(m *Mission) error {
		var c CarteMission
		_ = json.Unmarshal(m.Carte, &c)
		c.Permissions = "ignorer"
		m.Carte, _ = json.Marshal(c)
		return nil
	})
	ig = observer(ig.ID)
	if y, _ := s.approbations.Lire(ig.Relais.Accord); ig.Relais.Etape != "prod_deploye" || len(lances) != 3 || y.Etat != "acceptee" || y.Utilisee == "" {
		t.Fatalf("ignorer: %+v %v %+v", ig.Relais, lances, y)
	}
	if b := journalAuditTexte(t); !strings.Contains(b, "déploiement accepté sans confirmation") {
		t.Error("ignorer: the deployment without confirmation is not audited")
	}
}
