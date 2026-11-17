package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// livrerTest: the delivery mark of p, as the pilot's last cycle writes it.
func livrerTest(t *testing.T, p Projet, v string) {
	t.Helper()
	if err := os.MkdirAll(p.DemandesFaites(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.DemandesFaites(), ".last-delivery"), []byte(v+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projetRelaisTest: a project on its own example repository, with e as its one deployment ("site").
func projetRelaisTest(t *testing.T, s *Serveur, nom string, e Environnement) (Projet, string) {
	t.Helper()
	depot, _, c2 := depotSiteExemple(t)
	p := Projet{Nom: nom, Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	oublierRelais(nom)
	t.Cleanup(func() { oublierRelais(nom) })
	if err := s.acces.definirAncien(nom, "site", e); err != nil {
		t.Fatal(err)
	}
	return p, c2
}

// commitRelaisTest: one more commit in p's repository, of the file nom only (never the workshop's files).
func commitRelaisTest(t *testing.T, p Projet, nom string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.Dossier, nom), []byte(nom+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitEssai(t, p.Dossier, "add", "--", nom)
	gitEssai(t, p.Dossier, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", nom)
	return gitEssai(t, p.Dossier, "rev-parse", "HEAD")
}

// passageRelaisTest: what surveillerJalonsPush does for p at each look, waiting for the deployment it starts.
func passageRelaisTest(t *testing.T, s *Serveur, p Projet) {
	t.Helper()
	s.noterLivraison(p, time.Now())
	if ch := s.avancerRelais(p, time.Now()); ch != nil {
		select {
		case <-ch:
		case <-time.After(time.Minute):
			t.Fatal("the relay's deployment did not end")
		}
	}
}

// attenteRelaisTest: the pending accords the relay asked for projet.
func attenteRelaisTest(s *Serveur, projet string) []Approbation {
	var out []Approbation
	l, _ := s.approbations.Liste()
	for _, x := range l {
		if x.Projet == projet && x.Agent == "relais" && x.Etat == "attente" {
			out = append(out, x)
		}
	}
	return out
}

// TestRelaisLivraison (M282): a delivery found at the first look deploys nothing; each delivery after it
// deploys HEAD once, also across a restart and again after the project was reopened; a HEAD already served
// is not deployed again; changed build files and an older Production site wait for the owner's accord;
// without a moment nothing is deployed; the record follows the project's name and goes with it.
func TestRelaisLivraison(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	env := Environnement{URL: "http://m282.test", Serveurs: []string{serveurRelaisTest(t)}, Protection: "aucune", Politique: "accord", Moment: "livraison"}
	p, c2 := projetRelaisTest(t, s, "m282", env)
	methode := func(projet, construire string) {
		t.Helper()
		if err := s.acces.NoterDeploiement(projet, "site", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site", Construire: construire}); err != nil {
			t.Fatal(err)
		}
	}
	methode(p.Nom, "")
	historique := func() []Deploiement { return historiqueDeploiements(p.Nom, "site") }

	livrerTest(t, p, "20261101-100000") // delivered before this version
	passageRelaisTest(t, s, p)
	if r := lireRelais()[p.Nom]; !r.LivraisonVue || r.Livraison != "20261101-100000" || r.Etat != "" || len(historique()) != 0 {
		t.Fatalf("first look: %+v, %d deployment(s)", r, len(historique()))
	}
	s.noterJalonRelais(p, "M1 : Un", time.Now()) // deployed at delivery: a milestone deploys nothing
	passageRelaisTest(t, s, p)
	if r := lireRelais()[p.Nom]; r.Jalon != "" || len(historique()) != 0 {
		t.Fatalf("a milestone deployed a project deployed at delivery: %+v", r)
	}

	livrerTest(t, p, "20261102-100000")
	passageRelaisTest(t, s, p)
	h, r := historique(), lireRelais()[p.Nom]
	if len(h) != 1 || h[0].Commit != c2 || h[0].Auteur != "relais" || r.Etat != "lance" || r.Livraison != "20261102-100000" || r.Jalon != s.textePush("livre") {
		t.Fatalf("delivery: %+v, history %s", r, enJSON(h))
	}
	passageRelaisTest(t, s, p) // a restarted hub knows the file only, as this look does: no second deployment
	if len(historique()) != 1 {
		t.Fatalf("the same delivery deployed twice: %s", enJSON(historique()))
	}

	c3 := commitRelaisTest(t, p, "suite.txt") // reopened by a next request, delivered again
	livrerTest(t, p, "20261103-100000")
	passageRelaisTest(t, s, p)
	passageRelaisTest(t, s, p)
	if h = historique(); len(h) != 2 || h[1].Commit != c3 {
		t.Fatalf("delivered again: %s", enJSON(h))
	}
	h[1].Etat = "ok" // HEAD is what the site serves: a delivery without a new commit is noted only
	if err := noterHistorique(h[1]); err != nil {
		t.Fatal(err)
	}
	livrerTest(t, p, "20261104-100000")
	passageRelaisTest(t, s, p)
	if r = lireRelais()[p.Nom]; r.Livraison != "20261104-100000" || len(historique()) != 2 {
		t.Fatalf("HEAD already served: %+v, %d deployment(s)", r, len(historique()))
	}

	// build files changed: the owner's accord is asked, nothing deployed meanwhile
	methode(p.Nom, "sh build.sh")
	c4 := commitRelaisTest(t, p, "build.sh")
	livrerTest(t, p, "20261105-100000")
	passageRelaisTest(t, s, p)
	passageRelaisTest(t, s, p)
	a := attenteRelaisTest(s, p.Nom)
	if r = lireRelais()[p.Nom]; len(a) != 1 || !strings.Contains(a[0].Details["fichiers_build"], "build.sh") || a[0].Details["commit"] != c4 || r.Accord != a[0].ID || r.Etat != "attente" || len(historique()) != 2 {
		t.Fatalf("changed build files: %+v, accords %s, %d deployment(s)", r, enJSON(a), len(historique()))
	}

	env.Moment = "" // on request: a delivery is noted, nothing more
	if err := s.acces.definirAncien(p.Nom, "site", env); err != nil {
		t.Fatal(err)
	}
	commitRelaisTest(t, p, "fin.txt")
	livrerTest(t, p, "20261106-100000")
	passageRelaisTest(t, s, p)
	if r = lireRelais()[p.Nom]; r.Livraison != "20261106-100000" || r.Commit != c4 || len(historique()) != 2 || len(attenteRelaisTest(s, p.Nom)) != 1 {
		t.Fatalf("without a moment: %+v, %d deployment(s)", r, len(historique()))
	}

	deplacerRelais(p.Nom, "m282b") // renamed, then removed
	if m := lireRelais(); m["m282b"].Livraison != "20261106-100000" || m[p.Nom].LivraisonVue {
		t.Errorf("renamed: %+v", m)
	}
	deplacerRelais("m282b", "")
	if _, reste := lireRelais()["m282b"]; reste {
		t.Error("the record stayed after the project was removed")
	}

	// an older Production site deployed at delivery: the owner is asked, whatever its files
	env.URL, env.Moment, env.Production = "http://m282p.test", "livraison", true
	prod, _ := projetRelaisTest(t, s, "m282p", env)
	methode(prod.Nom, "")
	passageRelaisTest(t, s, prod)
	livrerTest(t, prod, "20261107-100000")
	passageRelaisTest(t, s, prod)
	if a = attenteRelaisTest(s, prod.Nom); len(a) != 1 || !a[0].Production || len(historiqueDeploiements(prod.Nom, "site")) != 0 {
		t.Fatalf("Production site: accords %s, %d deployment(s)", enJSON(a), len(historiqueDeploiements(prod.Nom, "site")))
	}
}

// TestRelaisMoment (M282): "after each milestone" deploys at each milestone and, when HEAD moved since, at the
// delivery; "at delivery" ignores the milestones; what was noted of the deliveries survives each relay.
func TestRelaisMoment(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	env := Environnement{URL: "http://m282m.test", Serveurs: []string{serveurRelaisTest(t)}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true}
	p, c2 := projetRelaisTest(t, s, "m282m", env)
	s.noterLivraison(p, time.Now()) // the first look: no delivery yet
	s.noterJalonRelais(p, "M1 : Un", time.Now())
	if r := lireRelais()[p.Nom]; r.Jalon != "M1 : Un" || r.Commit != c2 || r.Etat != "attente" || !r.LivraisonVue {
		t.Fatalf("milestone: %+v", r)
	}
	livrerTest(t, p, "20261101-100000") // delivered on the commit the milestone's relay already holds
	s.noterLivraison(p, time.Now())
	if r := lireRelais()[p.Nom]; r.Jalon != "M1 : Un" || r.Livraison != "20261101-100000" {
		t.Fatalf("delivery on the relayed commit: %+v", r)
	}
	c3 := commitRelaisTest(t, p, "rapport.txt")
	livrerTest(t, p, "20261102-100000")
	s.noterLivraison(p, time.Now())
	if r := lireRelais()[p.Nom]; r.Jalon != s.textePush("livre") || r.Commit != c3 || r.Etat != "attente" || r.Livraison != "20261102-100000" {
		t.Fatalf("delivery after a new commit: %+v", r)
	}

	env.DeployerAuJalon, env.Moment = false, "livraison"
	if err := s.acces.definirAncien(p.Nom, "site", env); err != nil {
		t.Fatal(err)
	}
	s.noterJalonRelais(p, "M2 : Deux", time.Now())
	if r := lireRelais()[p.Nom]; r.Jalon != s.textePush("livre") || !s.relaisActif(p.Nom) || s.etatRelais(p.Nom) == nil {
		t.Fatalf("at delivery, a milestone: %+v, relay %v", r, s.relaisActif(p.Nom))
	}
}
