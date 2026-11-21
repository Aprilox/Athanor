package main

import (
	"strings"
	"testing"
)

// TestRedTeamProjets (M302): the attacks of the former missions' red team that still apply, replayed on a
// project whose one deployment is an older Production site: its pilot deploys nothing outside the owner's
// accord (R6), a relay facing locked infra keys waits without using the accord (R7), and an accord is good
// for its full commit and its project only.
func TestRedTeamProjets(t *testing.T) {
	banc := func(t *testing.T) (*Serveur, func(nom string) (Projet, string)) {
		s, _, _ := serveursAjoutTest(t)
		return s, func(nom string) (Projet, string) {
			t.Helper()
			p, head := projetRelaisTest(t, s, nom, Environnement{URL: "http://" + nom + ".test", Serveurs: []string{serveurRelaisTest(t)},
				Protection: "aucune", Politique: "accord", Production: true, Moment: "livraison"})
			if err := s.acces.NoterDeploiement(nom, "site", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site"}); err != nil {
				t.Fatal(err)
			}
			livrerTest(t, p, "20261101-100000") // the first look only notes it
			passageRelaisTest(t, s, p)
			return p, head
		}
	}
	// livrer: one more delivery of p, seen twice by the relay; its pending accord.
	livrer := func(t *testing.T, s *Serveur, p Projet, v string) Approbation {
		t.Helper()
		livrerTest(t, p, v)
		passageRelaisTest(t, s, p)
		passageRelaisTest(t, s, p)
		a := attenteRelaisTest(s, p.Nom)
		if len(a) != 1 || len(historiqueDeploiements(p.Nom, "site")) != 0 {
			t.Fatalf("delivery %s of %s: accords %s, %d deployment(s)", v, p.Nom, enJSON(a), len(historiqueDeploiements(p.Nom, "site")))
		}
		return a[0]
	}
	accepter := func(t *testing.T, s *Serveur, id string) {
		t.Helper()
		if _, _, err := s.deciderApprobation("fenetre", id, "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("R6_pilote_deploie_hors_relais", func(t *testing.T) {
		s, projet := banc(t)
		p, head := projet("rtp6")
		txt, err := outilDemanderDeploiement(s, SessionHub{Projet: p.Nom, Agent: "pilote"}, []byte(`{"environnement":"site","commit":"`+head+`"}`))
		l, _ := s.approbations.Liste()
		if err != nil || !strings.Contains(txt, "waiting for the owner") || len(l) != 1 || l[0].Etat != "attente" || l[0].Agent != "pilote" || len(historiqueDeploiements(p.Nom, "site")) != 0 {
			t.Fatalf("the pilot deployed an older Production site without the owner: %v %q, accords %s, %d deployment(s)", err, txt, enJSON(l), len(historiqueDeploiements(p.Nom, "site")))
		}
		if _, _, err := s.deciderApprobation("fenetre", l[0].ID, "toujours", "", "", ""); err == nil {
			t.Error("a \"never ask again\" rule accepted for an older Production site")
		}
	})

	t.Run("R7_coffre_verrouille_accord_garde", func(t *testing.T) {
		s, projet := banc(t)
		motDePasseCoffreTest(t, s)
		p, head := projet("rtp7")
		x := livrer(t, s, p, "20261102-100000")
		accepter(t, s, x.ID)
		s.coffre.Verrouiller() // the infra keys locked before the relay's next look
		passageRelaisTest(t, s, p)
		if y, _ := s.approbations.Lire(x.ID); len(historiqueDeploiements(p.Nom, "site")) != 0 || y.Utilisee != "" || lireRelais()[p.Nom].Etat == "echec" {
			t.Fatalf("the relay used the accord with the infra keys locked: %d started, used %q, %+v", len(historiqueDeploiements(p.Nom, "site")), y.Utilisee, lireRelais()[p.Nom])
		}
		if err := s.coffre.Deverrouiller("mdp-test"); err != nil { // the owner opens the vault again
			t.Fatal(err)
		}
		passageRelaisTest(t, s, p)
		h := historiqueDeploiements(p.Nom, "site")
		if y, _ := s.approbations.Lire(x.ID); len(h) != 1 || h[0].Commit != head || y.Utilisee == "" {
			t.Fatalf("vault open again: %s, used %q", enJSON(h), y.Utilisee)
		}
	})

	t.Run("Tient_accord_lie_sha_et_projet", func(t *testing.T) {
		s, projet := banc(t)
		pa, ha := projet("rtpa")
		pb, _ := projet("rtpb")
		x := livrer(t, s, pa, "20261102-100000")
		if x.Details["commit"] != ha || len(ha) != 40 {
			t.Fatalf("the accord does not carry the full commit: %+v", x)
		}
		accepter(t, s, x.ID)
		// the same accord for another commit, a short commit or another project: never used
		for quoi, change := range map[string]func(*Approbation){
			"another SHA":     func(a *Approbation) { a.Details["commit"] = strings.Repeat("0", 40) },
			"a short SHA":     func(a *Approbation) { a.Details["commit"] = ha[:7] },
			"another project": func(a *Approbation) { a.Projet = pb.Nom },
		} {
			ap := Approbation{Projet: x.Projet, Env: x.Env, Agent: x.Agent, Action: x.Action, Raison: x.Raison, Details: map[string]string{}}
			for k, v := range x.Details {
				ap.Details[k] = v
			}
			change(&ap)
			if _, etat, _ := s.approbations.Reprendre(ap); etat == "utilisee" {
				t.Fatalf("the accord accepted for %s at %s used for %s", pa.Nom, ha, quoi)
			}
		}
		// project b delivered: its relay asks its own accord, a's stays unused and nothing is deployed
		y := livrer(t, s, pb, "20261102-100000")
		if z, _ := s.approbations.Lire(x.ID); y.ID == x.ID || z.Utilisee != "" {
			t.Fatalf("project b took a's accord: %+v", z)
		}
		passageRelaisTest(t, s, pa) // the project it was given for: deployed once
		passageRelaisTest(t, s, pa)
		h := historiqueDeploiements(pa.Nom, "site")
		if z, _ := s.approbations.Lire(x.ID); len(h) != 1 || h[0].Commit != ha || z.Utilisee == "" || len(historiqueDeploiements(pb.Nom, "site")) != 0 {
			t.Fatalf("the accord's own project: %s, used %q", enJSON(h), z.Utilisee)
		}
	})
}
