package main

import (
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestPauseProjet (M298): the three ways to stop a project cover its deployment. "Finish the milestone" holds
// nothing and lets its Servers tasks end. "Put on pause": no preparation, no deployment, no plan of its tasks
// executed, the tasks themselves stay (a restart of the application changes nothing: the pause is in the relay's
// file). "Stop immediately": the same, and its running tasks are cancelled, their tokens revoked. Start lets
// everything go again without a second task for the same work.
func TestPauseProjet(t *testing.T) {
	s, appel, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.RemoveAll(dossierMissions())
	r1 := serveurRelaisTest(t)
	arreter := arreterPiloteProjet
	t.Cleanup(func() { arreterPiloteProjet = arreter })
	arreterPiloteProjet = func(*Serveur, Projet) error { return nil }
	s.arretTest = func(Projet) (string, error) { return "", nil }
	var lances atomic.Int32
	arrive, lacher := make(chan struct{}, 8), make(chan struct{})
	finies := chefOpsTest(t, func(_, _ string) { // every task stays at work until the test lets it go
		lances.Add(1)
		arrive <- struct{}{}
		<-lacher
	})
	env := Environnement{URL: "https://m298.example.fr", Serveurs: []string{r1}, Protection: "aucune", Politique: "accord", Moment: "livraison", Plan: "nginx"}
	p, _ := projetRelaisTest(t, s, "m298", env)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	planRelaisTest(t, p, "- [ ] M1 : Le site\n")
	relais := func() RelaisJalon { return lireRelais()[p.Nom] }

	auTravail := func(n int32) {
		t.Helper()
		select {
		case <-arrive:
		case <-time.After(20 * time.Second):
			t.Fatal("no task at work")
		}
		if lances.Load() != n {
			t.Fatalf("%d tasks launched, wanted %d", lances.Load(), n)
		}
	}
	finir := func(id string) {
		t.Helper()
		lacher <- struct{}{}
		for range 400 {
			if !tacheEnCoursID(id) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("the task %s does not end", id)
	}
	jeton := func(id string) bool {
		return slices.ContainsFunc(s.jetons.Liste(), func(x SessionHub) bool { return x.Projet == prefixeTache+id })
	}
	plan := func(id string) error { // a change one of the project's tasks asks to run
		return s.planOuRegle(SessionHub{Projet: prefixeTache + id, Mission: id, Agent: agentServeurs, Serveurs: []string{r1}}, r1, &Etape{Outil: "commande", Classe: ClasseChangement})
	}
	action := func(a string) {
		t.Helper()
		if c, b := appel("/api/pilote/"+a+"?p=m298", nil); c != 200 {
			t.Fatalf("%s: %d %v", a, c, b)
		}
	}
	regarder := func() { // a look of the follower (no deployment can start here: the project has no method)
		t.Helper()
		s.preparerProjet(p)
		if ch := s.avancerRelais(p, time.Now()); ch != nil {
			t.Fatalf("a deployment started without a method: %+v", relais())
		}
	}
	rien := func(quand string, n int32) { // a look that launches no task
		t.Helper()
		regarder()
		if lances.Load() != n {
			t.Fatalf("%s: a task started (%d tasks, wanted %d): %+v", quand, lances.Load(), n, relais())
		}
	}

	s.noterLivraison(p, time.Now()) // the first look learns
	s.preparerProjet(p)
	auTravail(1)
	prep := relais().Preparation.Tache

	// finish the milestone: nothing held, the task goes on
	action("arreter-apres-cycle")
	if relais().Pause || relais().Arret != "jalon" || !tacheEnCoursID(prep) || projetBloque("dev", p.Nom) != "" {
		t.Fatalf("finish the milestone held the deployment: %+v", relais())
	}

	// pause: the task stays, none of its plans runs, a delivery waits
	action("pause")
	livrerTest(t, p, "1")
	s.noterLivraison(p, time.Now())
	if r := relais(); !r.Pause || r.Arret != "pause" || r.Etat != "attente" || !tacheEnCoursID(prep) || !jeton(prep) {
		t.Fatalf("pause: %+v, task running %v", r, tacheEnCoursID(prep))
	}
	if err := plan(prep); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("a paused project's task ran a change: %v", err)
	}
	if projetBloque("servers", "autre") != "" || projetBloque("dev", "autre") != "" {
		t.Fatal("the pause holds another project or task")
	}
	finir(prep)
	rien("pause", 1)
	rien("pause, after a restart", 1) // all a restart is: the record read from its file again

	// Start: the delivery's first deployment goes, the preparation is not made twice
	_, _ = s.demarrerPilote(p) // (no kit here: only what it does before launching)
	if relais().Pause || relais().Arret != "" {
		t.Fatal("Start did not lift the pause")
	}
	regarder()
	auTravail(2)
	premier := relais().Premier
	if r := relais(); r.Etat != "preparation" || premier == "" || r.Preparation.Tache != prep {
		t.Fatalf("after Start: %+v", r)
	}
	if err := plan(premier); err != nil && strings.Contains(err.Error(), "paused") {
		t.Fatalf("still paused after Start: %v", err)
	}

	// stop immediately: the running task cancelled, its token revoked, nothing launched until Start
	action("arreter")
	if tk, _ := tacheMission(premier); tk.Etat != "arretee" || jeton(premier) {
		t.Fatalf("stop now: task %s, token %v", tk.Etat, jeton(premier))
	}
	if r := relais(); !r.Pause || r.Arret != "arret" || r.Etat != "attente" || r.Preparation.Tache != prep {
		t.Fatalf("stop now: %+v", r)
	}
	lacher <- struct{}{} // (the fake session of the cancelled task ends)
	rien("stop now", 2)

	// Start again: one first deployment, not two; the preparation that had ended is not made again
	_, _ = s.demarrerPilote(p)
	regarder()
	auTravail(3)
	rien("Start again, second look", 3)
	if r := relais(); r.Pause || r.Etat != "preparation" || r.Premier == premier || r.Preparation.Tache != prep {
		t.Fatalf("after the second Start: %+v", r)
	}
	finir(relais().Premier)

	// stop immediately while the servers are prepared again (the plan changed): prepared again, once, at Start
	env.Plan = "nginx et un certificat"
	if err := s.acces.definirAncien(p.Nom, "site", env); err != nil {
		t.Fatal(err)
	}
	s.preparerProjet(p)
	auTravail(4)
	prep2 := relais().Preparation.Tache
	action("arreter")
	if tk, _ := tacheMission(prep2); prep2 == prep || tk.Etat != "arretee" || jeton(prep2) || relais().Preparation.Empreinte != "" {
		t.Fatalf("stop now during the preparation: task %s, %+v", tk.Etat, relais().Preparation)
	}
	lacher <- struct{}{}
	s.preparerProjet(p)
	if lances.Load() != 4 {
		t.Fatal("a stopped project's servers were prepared")
	}
	_, _ = s.demarrerPilote(p)
	s.preparerProjet(p)
	auTravail(5)
	s.preparerProjet(p)
	if r := relais(); lances.Load() != 5 || r.Preparation.Tache == prep2 || r.Preparation.Empreinte == "" {
		t.Fatalf("the preparation after Start: %d tasks, %+v", lances.Load(), r.Preparation)
	}
	finir(relais().Preparation.Tache)
	finies()
}
