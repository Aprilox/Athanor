package main

import (
	"context"
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

// TestMissionArretReprise (M250): the three ways to stop a launched mission (finish the milestone in progress,
// stop now, pause), the resume after each, Athanor restarted during a pause or a stop asked, without a Servers task
// or a deployment twice; and the same three choices on a project's pilot without a mission.
func TestMissionArretReprise(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.session = "session-test"
	s.mu.Unlock()
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a3", Nom: "dev-3", Hote: "203.0.113.7"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dossierMissions(), fichierRelais()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	p := Projet{Nom: "atelier", Dossier: filepath.Join(t.TempDir(), "atelier")}
	ecrireFichier(t, p.Demande(), "# atelier\n\nUn site.\n", 0o644)
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site\n- [ ] M2 : Le formulaire\n", 0o644)
	s.config.Projets = append(s.config.Projets, p)
	pid := filepath.Join(p.Atelier(), "pilot.pid")

	// doubles: the pilot (pilot.pid), its stop at the end of the cycle, the Servers sessions (they run until stopped)
	var mu sync.Mutex
	var appels []string
	noter := func(a string) { mu.Lock(); appels = append(appels, a); mu.Unlock() }
	vider := func() []string { mu.Lock(); defer mu.Unlock(); a := appels; appels = nil; return a }
	sessions := 0
	ancienPilote, ancienArret, ancienneSession, ancienPas := demarrerPiloteProjet, arreterPiloteProjet, sessionTacheServeurs, pasSuiviMission
	t.Cleanup(func() {
		demarrerPiloteProjet, arreterPiloteProjet, sessionTacheServeurs, pasSuiviMission = ancienPilote, ancienArret, ancienneSession, ancienPas
	})
	pasSuiviMission = time.Hour // the follower reads once, then the test reads itself
	demarrerPiloteProjet = func(_ *Serveur, p Projet) (string, error) {
		noter("demarrer " + p.Nom)
		ecrireFichier(t, pid, "1", 0o644)
		return "Pilot started", nil
	}
	arreterPiloteProjet = func(_ *Serveur, p Projet) error { noter("arreter " + p.Nom); return os.Remove(pid) }
	s.arretTest = func(p Projet) (string, error) { noter("fin_cycle " + p.Nom); return "", nil }
	sessionTacheServeurs = func(_ *Serveur, ctx context.Context, _ string) error {
		mu.Lock()
		sessions++
		mu.Unlock()
		<-ctx.Done()
		return context.Cause(ctx)
	}
	nbSessions := func() int { mu.Lock(); defer mu.Unlock(); return sessions }
	finTaches := func() { // the stopped tasks' goroutines end (they must not outlive the test)
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
	t.Cleanup(func() {
		tachesEnCours.Lock()
		for _, e := range tachesEnCours.m {
			e.annuler(errTacheArretee)
		}
		tachesEnCours.Unlock()
		for range 400 {
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	})

	c := CarteMission{Titre: "Atelier", Projet: ProjetCarte{Nom: p.Nom}, JalonsEstimes: 2, Dev: BriefCarte{Brief: "Un site"},
		Serveurs: BriefCarte{Brief: "Préparer le serveur"}, Envs: []EnvCarte{{Nom: "site", URL: "https://atelier.example.fr", Serveurs: []string{"a3"}}}}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil {
		t.Fatal(err)
	}
	id := aleatoire(8)
	if err := enregistrerMission(Mission{ID: id, Phrase: "mets mon atelier en ligne", Etat: "proposee", Politique: "prod", Carte: mustJSON(calculerCarte(c, "prod", x)),
		Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	lire := func() Mission { t.Helper(); m, _ := lireMission(id); return m }
	voieSrv := func() VoieMission {
		t.Helper()
		m := lire()
		i := slices.IndexFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" })
		if i < 0 {
			t.Fatalf("no Servers lane: %+v", m.Voies)
		}
		return m.Voies[i]
	}
	journal := func(cle string) bool {
		return slices.ContainsFunc(lire().Journal, func(e EntreeJournal) bool { return e.Cle == cle })
	}
	attendre := func(quoi string, ok func() bool) {
		t.Helper()
		for range 400 {
			if ok() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("still not: %s", quoi)
	}
	// a running preparation, its task id: one session more than before
	preparation := func(avant int) string {
		t.Helper()
		var wg sync.WaitGroup
		for range 2 { // two readings at once: one launch
			wg.Add(1)
			go func() { defer wg.Done(); s.observerMission(id) }()
		}
		wg.Wait()
		v := voieSrv()
		attendre("the preparation's session", func() bool { return nbSessions() == avant+1 })
		if v.Ref == "" || v.Etat == "attente_plan" || v.Env != p.Nom+"/site" {
			t.Fatalf("no preparation running: %+v", v)
		}
		time.Sleep(100 * time.Millisecond)
		if n := nbSessions(); n != avant+1 {
			t.Fatalf("%d sessions, expected %d", n, avant+1)
		}
		return v.Ref
	}
	// suspended: the lane back to attente_plan, its task stopped
	suspendue := func(ref string) {
		t.Helper()
		if v := voieSrv(); v.Etat != "attente_plan" || v.Ref != "" || v.Env != p.Nom+"/site" {
			t.Fatalf("the Servers lane is not suspended: %+v", v)
		}
		attendre("the task "+ref+" stopped", func() bool { x, _ := tacheMission(ref); return x.Etat == "arretee" })
		finTaches()
		if !slices.ContainsFunc(lire().Journal, func(e EntreeJournal) bool { return e.ID == "suspendue:"+ref }) {
			t.Fatalf("the journal does not say the suspension of %s", ref)
		}
	}
	redemarrer := func() { // Athanor restarted: the followers start from nothing
		suivisMission.Delete(id)
		s.reprendreMissions()
		s.observerMission(id)
	}

	if m, err := s.lancerMission(id, "proprietaire", "", ""); err != nil || m.Etat != "lancee" {
		t.Fatalf("launch: %v %+v", err, m)
	}
	vider()
	ref := preparation(0)
	if !s.relaisActif(p.Nom) {
		t.Fatal("the launch did not turn the milestone relay on")
	}

	t.Run("mode_inconnu", func(t *testing.T) {
		if c, _ := appel("/api/missions/"+id+"/arreter", `{"mode":"plus_tard"}`); c != http.StatusBadRequest || lire().Etat != "lancee" {
			t.Fatalf("unknown mode: %d %s", c, lire().Etat)
		}
	})

	t.Run("jalon", func(t *testing.T) {
		if c, b := appel("/api/missions/"+id+"/arreter", `{"mode":"jalon"}`); c != 200 || lire().Etat != "arret_demande" {
			t.Fatalf("stop after the milestone: %d %s", c, b)
		}
		if a := vider(); !slices.Equal(a, []string{"fin_cycle " + p.Nom}) {
			t.Fatalf("the pilot is not asked to stop at the end of its cycle: %v", a)
		}
		if _, err := os.Stat(filepath.Join(p.Atelier(), "auto")); err == nil {
			t.Fatal("the pilot would restart with the application")
		}
		if projetBloque("dev", p.Nom) != "" {
			t.Fatal("the milestone relay is blocked while the milestone ends")
		}
		ecrireRelais(p.Nom, RelaisJalon{Jalon: "M1", Commit: strings.Repeat("ab12", 10), Etat: "attente", Date: time.Now().UTC().Format(time.RFC3339)})
		s.observerMission(id)
		if m := lire(); m.Etat != "arret_demande" || voieSrv().Ref != ref {
			t.Fatalf("paused while the pilot ends its milestone: %s %+v", m.Etat, voieSrv())
		}
		// Athanor restarted meanwhile: still waiting for the milestone
		redemarrer()
		if m := lire(); m.Etat != "arret_demande" || !journal("Stop asked by {0}: the mission pauses once the milestone in progress is done") {
			t.Fatalf("after a restart: %s", m.Etat)
		}
		_ = os.Remove(pid) // the pilot ended its cycle; its milestone's relay still waits to deploy
		s.observerMission(id)
		if lire().Etat != "arret_demande" {
			t.Fatal("paused before the milestone's relay deployed")
		}
		r := lireRelais()[p.Nom]
		r.Etat = "lance"
		ecrireRelais(p.Nom, r)
		s.observerMission(id)
		if m := lire(); m.Etat != "en_pause" || !journal("The milestone in progress is done: the mission is paused") {
			t.Fatalf("not paused once the milestone is done: %s", m.Etat)
		}
		suspendue(ref)
		if projetBloque("dev", p.Nom) != id {
			t.Fatal("the paused mission does not lock its project")
		}
	})

	t.Run("redemarrage_en_pause", func(t *testing.T) {
		n := nbSessions()
		redemarrer()
		if m := lire(); m.Etat != "en_pause" || nbSessions() != n || voieSrv().Ref != "" {
			t.Fatalf("a restart during the pause launched something: %s, %d sessions", m.Etat, nbSessions())
		}
	})

	t.Run("reprise_apres_jalon", func(t *testing.T) {
		n := nbSessions()
		if m, err := s.reprendreMission(id, "proprietaire"); err != nil || m.Etat != "lancee" {
			t.Fatalf("resume: %v %s", err, m.Etat)
		}
		if a := vider(); !slices.Equal(a, []string{"demarrer " + p.Nom}) {
			t.Fatalf("the pilot is not started again: %v", a)
		}
		ref = preparation(n)
		if r := lireRelais()[p.Nom]; r.Etat != "lance" || r.Jalon != "M1" {
			t.Fatalf("the milestone relay changed: %+v", r)
		}
	})

	t.Run("annuler_arret_jalon", func(t *testing.T) {
		if c, b := appel("/api/missions/"+id+"/arreter", `{"mode":"jalon"}`); c != 200 {
			t.Fatalf("stop after the milestone: %d %s", c, b)
		}
		vider()
		ecrireFichier(t, filepath.Join(p.Atelier(), "STOP"), "after the cycle\n", 0o644)
		if m, err := s.reprendreMission(id, "proprietaire"); err != nil || m.Etat != "lancee" {
			t.Fatalf("resume a stop asked: %v %s", err, m.Etat)
		}
		if _, err := os.Stat(filepath.Join(p.Atelier(), "STOP")); err == nil {
			t.Fatal("the stop at the end of the cycle is still asked")
		}
		if _, err := os.Stat(filepath.Join(p.Atelier(), "auto")); err != nil {
			t.Fatal("the running pilot is not marked to restart with the application")
		}
		if a := vider(); len(a) != 0 || voieSrv().Ref != ref {
			t.Fatalf("a running pilot started again, or its preparation changed: %v %+v", a, voieSrv())
		}
	})

	t.Run("pause", func(t *testing.T) {
		if c, b := appel("/api/missions/"+id+"/arreter", `{"mode":"pause"}`); c != 200 || lire().Etat != "en_pause" {
			t.Fatalf("pause: %d %s", c, b)
		}
		if a := vider(); !slices.Equal(a, []string{"arreter " + p.Nom}) {
			t.Fatalf("the pilot is not stopped now: %v", a)
		}
		suspendue(ref)
		n := nbSessions()
		redemarrer()
		if lire().Etat != "en_pause" || nbSessions() != n {
			t.Fatal("a restart during the pause launched something")
		}
		if _, err := s.reprendreMission(id, "proprietaire"); err != nil {
			t.Fatal(err)
		}
		vider()
		ref = preparation(n)
	})

	t.Run("immediat", func(t *testing.T) {
		if _, err := modifierMission(id, func(m *Mission) error {
			m.Relais = &RelaisMission{Etape: "accord_prod", Commit: strings.Repeat("cd34", 10), Accord: "ancien"}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if c, b := appel("/api/missions/"+id+"/arreter", `{"mode":"immediat"}`); c != 200 || lire().Etat != "arretee" {
			t.Fatalf("stop now: %d %s", c, b)
		}
		if a := vider(); !slices.Equal(a, []string{"arreter " + p.Nom}) {
			t.Fatalf("the pilot is not stopped: %v", a)
		}
		if s.relaisActif(p.Nom) {
			t.Fatal("the milestone relay still on after the stop")
		}
		suspendue(ref)
		n := nbSessions()
		redemarrer()
		if lire().Etat != "arretee" || nbSessions() != n {
			t.Fatal("a restart after the stop launched something")
		}
		if m, err := s.reprendreMission(id, "proprietaire"); err != nil || m.Etat != "lancee" {
			t.Fatalf("resume a stopped mission: %v %s", err, m.Etat)
		}
		if a := vider(); !slices.Equal(a, []string{"demarrer " + p.Nom}) {
			t.Fatalf("the pilot is not started again: %v", a)
		}
		if !s.relaisActif(p.Nom) {
			t.Fatal("the milestone relay is not on again")
		}
		if r := lire().Relais; r == nil || r.Etape != "dev_verifie" || r.Accord != "" {
			t.Fatalf("the prod accord refused by the stop is not asked again: %+v", r)
		}
		ref = preparation(n)
		if h := historiqueDeploiements(p.Nom, "site"); len(h) != 0 {
			t.Fatalf("a deployment was replayed: %+v", h)
		}
		if c, _ := appel("/api/missions/"+id+"/reprendre", `{"mot_de_passe":"faux"}`); c == 200 {
			t.Fatal("resumed without the owner's presence")
		}
	})

	t.Run("projet_sans_mission", func(t *testing.T) {
		seul := Projet{Nom: "seul", Dossier: filepath.Join(t.TempDir(), "seul")}
		ecrireFichier(t, seul.Demande(), "# seul\n", 0o644)
		s.config.Projets = append(s.config.Projets, seul)
		for action, attendu := range map[string]string{"arreter-apres-cycle": "fin_cycle seul", "arreter": "arreter seul", "pause": "arreter seul"} {
			ecrireFichier(t, filepath.Join(seul.Atelier(), "pilot.pid"), "1", 0o644)
			marqueAuto(seul, true)
			pid = filepath.Join(seul.Atelier(), "pilot.pid")
			c, b := appel("/api/pilote/"+action+"?p=seul", "")
			if a := vider(); c != 200 || !slices.Equal(a, []string{attendu}) {
				t.Fatalf("%s: %d %s %v", action, c, b, a)
			}
			if _, err := os.Stat(filepath.Join(seul.Atelier(), "auto")); err == nil {
				t.Fatalf("%s: the pilot would restart with the application", action)
			}
		}
	})
}
