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

// TestMissionSuppression (note 10h04): a mission deleted with its tasks and files; its project and folder only when
// the mission created them and the path is safe; a running mission stopped first (now, or after its milestone by the
// follower); nothing back after a restart; the finished missions emptied at once.
func TestMissionSuppression(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.session = "session-test"
	s.mu.Unlock()
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a4", Nom: "dev-4", Hote: "203.0.113.8"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dossierMissions(), fichierRelais()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	base := t.TempDir()
	atelier := Projet{Nom: "atelier", Dossier: filepath.Join(base, "atelier")}
	ecrireFichier(t, atelier.Demande(), "# atelier\n\nUn site.\n", 0o644)
	ecrireFichier(t, atelier.Plan(), "# Plan\n\n- [ ] M1 : Le site\n", 0o644)
	neuf := Projet{Nom: "neuf", Dossier: filepath.Join(base, "neuf")}
	ecrireFichier(t, filepath.Join(neuf.Dossier, "index.html"), "<p>neuf</p>", 0o644)
	s.config.Projets = append(s.config.Projets, atelier, neuf)
	pid := filepath.Join(atelier.Atelier(), "pilot.pid")

	var mu sync.Mutex
	var appels, corbeille []string
	noter := func(a string) { mu.Lock(); appels = append(appels, a); mu.Unlock() }
	sessions := 0
	ancienPilote, ancienArret, ancienneSession, ancienPas, ancienneCorbeille := demarrerPiloteMission, arreterPiloteMission, sessionTacheServeurs, pasSuiviMission, corbeilleMission
	t.Cleanup(func() {
		demarrerPiloteMission, arreterPiloteMission, sessionTacheServeurs, pasSuiviMission, corbeilleMission = ancienPilote, ancienArret, ancienneSession, ancienPas, ancienneCorbeille
		tachesEnCours.Lock()
		for _, e := range tachesEnCours.m {
			e.annuler(errTacheArretee)
		}
		tachesEnCours.Unlock()
		attendreFinTaches()
	})
	pasSuiviMission = time.Hour
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		noter("demarrer " + p.Nom)
		ecrireFichier(t, pid, "1", 0o644)
		return "Pilot started", nil
	}
	arreterPiloteMission = func(_ *Serveur, p Projet) error { noter("arreter " + p.Nom); return os.Remove(pid) }
	s.arretTest = func(p Projet) (string, error) { noter("fin_cycle " + p.Nom); return "", nil }
	sessionTacheServeurs = func(_ *Serveur, ctx context.Context, _ string) error {
		mu.Lock()
		sessions++
		mu.Unlock()
		<-ctx.Done()
		return context.Cause(ctx)
	}
	corbeilleMission = func(d string) error { mu.Lock(); corbeille = append(corbeille, d); mu.Unlock(); return os.RemoveAll(d) }
	nbSessions := func() int { mu.Lock(); defer mu.Unlock(); return sessions }

	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	existe := func(id string) bool { _, err := lireMission(id); return err == nil }
	stockee := func(t *testing.T, etat string, voies []VoieMission, h ...EvenementMission) string {
		t.Helper()
		id := aleatoire(8)
		if err := enregistrerMission(Mission{ID: id, Phrase: "une mission", Etat: etat, Politique: "prod", Voies: voies, Questions: []QuestionMission{},
			Historique: append([]EvenementMission{evenementMission("proposee", "")}, h...), Cree: time.Now().UTC().Format(formatCree)}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	attendre := func(t *testing.T, quoi string, ok func() bool) {
		t.Helper()
		for range 400 {
			if ok() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("still not: %s", quoi)
	}
	// a launched mission on atelier, its Servers preparation running: its task id
	lancee := func(t *testing.T) (string, string) {
		t.Helper()
		c := CarteMission{Titre: "Atelier", Projet: ProjetCarte{Nom: atelier.Nom}, JalonsEstimes: 1, Dev: BriefCarte{Brief: "Un site"},
			Serveurs: BriefCarte{Brief: "Préparer le serveur"}, Envs: []EnvCarte{{Nom: "site", URL: "https://atelier.example.fr", Serveurs: []string{"a4"}}}}
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		id := aleatoire(8)
		if err := enregistrerMission(Mission{ID: id, Phrase: "mets mon atelier en ligne", Etat: "proposee", Politique: "prod", Carte: mustJSON(calculerCarte(c, "prod", x)),
			Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
			t.Fatal(err)
		}
		avant := nbSessions()
		if m, err := s.lancerMission(id, "proprietaire", "", ""); err != nil || m.Etat != "lancee" {
			t.Fatalf("launch: %v %+v", err, m)
		}
		s.observerMission(id)
		attendre(t, "the preparation's session", func() bool { return nbSessions() == avant+1 })
		m, _ := lireMission(id)
		i := slices.IndexFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Ref != "" })
		if i < 0 {
			t.Fatalf("no Servers task: %+v", m.Voies)
		}
		return id, m.Voies[i].Ref
	}
	partie := func(t *testing.T, id, tache string) {
		t.Helper()
		if existe(id) {
			t.Fatalf("mission %s still there", id)
		}
		for _, f := range []string{filepath.Join(dossierMissions(), id+".json"), dossierSessionMission(id), filepath.Join(dossierTaches("a4"), tache+".json"), dossierSessionTache(tache)} {
			if _, err := os.Stat(f); tache != "" && err == nil || tache == "" && strings.Contains(f, id) && err == nil {
				t.Fatalf("%s left behind", f)
			}
		}
	}

	t.Run("simple", func(t *testing.T) {
		id := stockee(t, "proposee", []VoieMission{})
		ecrireFichier(t, filepath.Join(dossierSessionMission(id), "notes.md"), "suivi", 0o600)
		var a ApercuSuppression
		if c, b := appel("GET", "/api/missions/"+id+"/suppression", ""); c != 200 || json.Unmarshal([]byte(b), &a) != nil || a.EnCours || a.DossierOK || a.Historique != 1 {
			t.Fatalf("preview: %d %s", c, b)
		}
		if c, b := appel("POST", "/api/missions/"+id+"/supprimer", `{}`); c != 200 || !strings.Contains(b, `"supprimee":true`) {
			t.Fatalf("delete: %d %s", c, b)
		}
		partie(t, id, "")
		if c, _ := appel("POST", "/api/missions/"+id+"/supprimer", `{}`); c != http.StatusNotFound {
			t.Fatalf("deleted twice: %d", c)
		}
		if c, _ := appel("POST", "/api/missions/x/supprimer", `{"arret":"bientot"}`); c != http.StatusBadRequest {
			t.Fatalf("unknown stop: %d", c)
		}
		p, err := (Audit{Dossier: dossierJournalAudit()}).Lire(FiltreAudit{Q: "mission supprimée"})
		if err != nil || !slices.ContainsFunc(p.Lignes, func(l LigneAudit) bool { return l.Mission == id && strings.Contains(l.Resultat, "mission "+id) }) {
			t.Fatalf("not audited: %v %+v", err, p.Lignes)
		}
	})

	t.Run("projet_existant_garde", func(t *testing.T) {
		id := stockee(t, "terminee", []VoieMission{{Module: "dev", Ref: atelier.Nom, Etat: "termine"}})
		_, b := appel("GET", "/api/missions/"+id+"/suppression", "")
		if !strings.Contains(b, `"dossier_ok":false`) || !strings.Contains(b, "existed before the mission") {
			t.Fatalf("an existing project offered: %s", b)
		}
		if c, _ := appel("POST", "/api/missions/"+id+"/supprimer", `{"projet":true}`); c != http.StatusConflict || !existe(id) {
			t.Fatalf("an existing project removed: %d", c)
		}
		if c, _ := appel("POST", "/api/missions/"+id+"/supprimer", `{}`); c != 200 {
			t.Fatalf("delete: %d", c)
		}
		if _, err := os.Stat(atelier.Plan()); err != nil {
			t.Fatal("the project's files went with the mission")
		}
	})

	creee := func(t *testing.T) string {
		return stockee(t, "terminee", []VoieMission{{Module: "dev", Ref: neuf.Nom, Etat: "termine"}}, evenementMission("lancee", "project created: "+neuf.Dossier))
	}
	t.Run("sans_dossier", func(t *testing.T) {
		id := creee(t)
		if c, _ := appel("POST", "/api/missions/"+id+"/supprimer", `{}`); c != 200 || existe(id) {
			t.Fatalf("delete: %d", c)
		}
		if _, ok := s.projetParNom(neuf.Nom); !ok || len(corbeille) != 0 {
			t.Fatalf("the project went without being ticked: %v", corbeille)
		}
	})

	t.Run("avec_dossier", func(t *testing.T) {
		id, autre := creee(t), creee(t)
		if _, b := appel("GET", "/api/missions/"+id+"/suppression", ""); !strings.Contains(b, "Another mission uses this project") {
			t.Fatalf("a project shared by two missions offered: %s", b)
		}
		if c, _ := appel("POST", "/api/missions/"+autre+"/supprimer", `{}`); c != 200 {
			t.Fatal(c)
		}
		if _, b := appel("GET", "/api/missions/"+id+"/suppression", ""); !strings.Contains(b, `"dossier_ok":true`) || !strings.Contains(b, `"projet":"neuf"`) {
			t.Fatalf("preview: %s", b)
		}
		if c, b := appel("POST", "/api/missions/"+id+"/supprimer", `{"projet":true}`); c != 200 || !strings.Contains(b, "folder "+strings.ReplaceAll(neuf.Dossier, `\`, `\\`)) {
			t.Fatalf("delete with the folder: %d %s", c, b)
		}
		if _, ok := s.projetParNom(neuf.Nom); ok || !slices.Equal(corbeille, []string{neuf.Dossier}) {
			t.Fatalf("project still listed or folder not in the recycle bin: %v", corbeille)
		}
		if _, err := os.Stat(atelier.Dossier); err != nil {
			t.Fatal("another folder went")
		}
	})

	t.Run("chemin_dangereux", func(t *testing.T) {
		home, _ := os.UserHomeDir()
		racine := filepath.VolumeName(base) + string(filepath.Separator)
		for _, d := range []string{home, racine, filepath.Dir(home), "relatif", s.kitCourant().Racine} {
			if d != "" && controlerDossierSupprime(d, s.kitCourant().Racine) == "" {
				t.Errorf("%q accepted", d)
			}
		}
		lien, cible := filepath.Join(base, "lien"), filepath.Join(base, "dehors")
		ecrireFichier(t, filepath.Join(cible, "a.txt"), "a", 0o644)
		if os.Symlink(cible, lien) == nil {
			if controlerDossierSupprime(lien, "") == "" {
				t.Error("a link accepted")
			}
		}
		perso := Projet{Nom: "perso", Dossier: home}
		s.config.Projets = append(s.config.Projets, perso)
		id := stockee(t, "terminee", []VoieMission{{Module: "dev", Ref: perso.Nom}}, evenementMission("lancee", "project created: "+home))
		if c, _ := appel("POST", "/api/missions/"+id+"/supprimer", `{"projet":true}`); c != http.StatusConflict || !existe(id) || len(corbeille) != 1 {
			t.Fatalf("the personal folder accepted: %d %v", c, corbeille)
		}
		_, _ = appel("POST", "/api/missions/"+id+"/supprimer", `{}`)
		s.config.Projets = slices.DeleteFunc(s.config.Projets, func(p Projet) bool { return p.Nom == perso.Nom })
	})

	t.Run("en_cours_immediat", func(t *testing.T) {
		id, tache := lancee(t)
		if c, b := appel("POST", "/api/missions/"+id+"/supprimer", `{}`); c != http.StatusConflict || !existe(id) {
			t.Fatalf("a running mission deleted without a stop: %d %s", c, b)
		}
		if _, b := appel("GET", "/api/missions/"+id+"/suppression", ""); !strings.Contains(b, `"en_cours":true`) || !strings.Contains(b, `"taches":1`) {
			t.Fatalf("preview: %s", b)
		}
		if c, b := appel("POST", "/api/missions/"+id+"/supprimer", `{"arret":"immediat"}`); c != 200 {
			t.Fatalf("delete now: %d %s", c, b)
		}
		partie(t, id, tache)
		if _, err := os.Stat(pid); err == nil {
			t.Fatal("the pilot still runs")
		}
		if _, ok := tacheMission(tache); ok {
			t.Fatal("the Servers task is still there")
		}
	})

	t.Run("en_cours_jalon_et_relance", func(t *testing.T) {
		id, tache := lancee(t)
		if c, b := appel("POST", "/api/missions/"+id+"/supprimer", `{"arret":"jalon"}`); c != 200 || !strings.Contains(b, `"differee":true`) {
			t.Fatalf("delete after the milestone: %d %s", c, b)
		}
		if m, _ := lireMission(id); m.Etat != "arret_demande" || m.Suppression == nil {
			t.Fatalf("not waiting for the milestone: %+v", m)
		}
		s.observerMission(id) // the pilot still runs: nothing deleted
		if !existe(id) {
			t.Fatal("deleted before the end of the milestone")
		}
		_ = os.Remove(pid) // the milestone is done
		avant := nbSessions()
		suivisMission.Delete(id)
		s.reprendreMissions() // Athanor restarted meanwhile
		s.observerMission(id)
		attendre(t, "the deletion once the milestone is done", func() bool { return !existe(id) }) // the restarted follower may do it
		time.Sleep(100 * time.Millisecond)
		partie(t, id, tache)
		suivisMission.Delete(id)
		s.reprendreMissions()
		time.Sleep(100 * time.Millisecond)
		if existe(id) || nbSessions() != avant || slices.ContainsFunc(lireMissions(), func(m Mission) bool { return m.ID == id }) {
			t.Fatal("the deleted mission came back after a restart")
		}
	})

	t.Run("vider", func(t *testing.T) {
		for _, m := range lireMissions() {
			_, _ = s.supprimerMission(m.ID, "test", OptionsSuppression{})
		}
		fini, annulee, echec, ouverte := stockee(t, "terminee", nil), stockee(t, "annulee", nil), stockee(t, "echec", nil), stockee(t, "proposee", nil)
		if c, b := appel("POST", "/api/missions/vider", ""); c != 200 || !strings.Contains(b, `"supprimees":3`) {
			t.Fatalf("empty: %d %s", c, b)
		}
		if existe(fini) || existe(annulee) || existe(echec) || !existe(ouverte) {
			t.Fatal("wrong missions emptied")
		}
	})
}

// attendreFinTaches: every running Servers task's goroutine ended (they must not outlive a test).
func attendreFinTaches() {
	for range 400 {
		tachesEnCours.Lock()
		n := len(tachesEnCours.m)
		tachesEnCours.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}
