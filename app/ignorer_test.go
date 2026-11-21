package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestIgnorerSansInterruption (M285): in the Ignorer mode of a project nothing waits for the owner: changed build
// files and an older Production site are deployed by the relay, a message that needs an accord goes on, a task's
// plan on that site is accepted at once; each of them leaves one audit line naming the project and the mode. In
// Manual the same four ask.
func TestIgnorerSansInterruption(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	_ = os.Remove(fichierReglagesProjets())
	t.Cleanup(func() { _ = os.Remove(fichierReglagesProjets()) })
	r1 := serveurRelaisTest(t)
	attente := func(projets ...string) (n int) {
		l, _ := s.approbations.Liste()
		for _, x := range l {
			if x.Etat == "attente" && (strings.Contains(x.Projet, projets[0]) || strings.Contains(x.Projet, projets[1])) {
				n++
			}
		}
		return n
	}
	// sauts: the audit lines of projet that say a demand was skipped
	sauts := func(projet string) (out []string) {
		for _, l := range strings.Split(journalAuditTexte(t), "\n") {
			var a LigneAudit
			if json.Unmarshal([]byte(l), &a) == nil && a.Projet == projet && strings.Contains(a.Resultat, "mode Ignorer") {
				out = append(out, a.Action)
			}
		}
		return out
	}
	for _, mode := range []string{"manuel", "ignorer"} {
		build, prod := "m285b"+mode[:1], "m285p"+mode[:1]
		env := Environnement{URL: "http://" + build + ".test", Serveurs: []string{r1}, Protection: "aucune", Politique: "accord", Moment: "livraison"}
		methode := func(projet, construire string) {
			t.Helper()
			if err := s.acces.NoterDeploiement(projet, "site", ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: "site", Construire: construire}); err != nil {
				t.Fatal(err)
			}
		}
		pb, _ := projetRelaisTest(t, s, build, env)
		env.URL, env.Production = "http://"+prod+".test", true
		pp, _ := projetRelaisTest(t, s, prod, env)
		for _, p := range []string{build, prod} {
			if _, err := definirReglageProjet(p, "permission", mode); err != nil {
				t.Fatal(err)
			}
			methode(p, "")
		}

		// changed build files, after a first deployment the site serves
		livrerTest(t, pb, "20261101-100000")
		passageRelaisTest(t, s, pb)
		livrerTest(t, pb, "20261102-100000")
		passageRelaisTest(t, s, pb)
		h := historiqueDeploiements(build, "site")
		if len(h) != 1 {
			t.Fatalf("%s: first delivery: %d deployment(s)", mode, len(h))
		}
		h[0].Etat = "ok"
		if err := noterHistorique(h[0]); err != nil {
			t.Fatal(err)
		}
		methode(build, "sh build.sh")
		commitRelaisTest(t, pb, "build.sh")
		livrerTest(t, pb, "20261103-100000")
		passageRelaisTest(t, s, pb)
		passageRelaisTest(t, s, pb)
		nb := len(historiqueDeploiements(build, "site"))

		// an older Production site, delivered
		passageRelaisTest(t, s, pp)
		livrerTest(t, pp, "20261104-100000")
		passageRelaisTest(t, s, pp)
		passageRelaisTest(t, s, pp)
		np := len(historiqueDeploiements(prod, "site"))

		// a message that needs the owner's accord on that site, then a task's plan of one change on it
		_, attend, err := s.exigerAccord(Approbation{Projet: prod, Env: "site", Agent: "pilote", Action: "redemarrer", Raison: "pilote → servers: redemarrer"}, true)
		if err != nil {
			t.Fatal(err)
		}
		tache := "t285" + mode[:1]
		e := &tacheEnCours{t: TacheServeurs{ID: tache, Env: prod + "/site"}, hache: "h" + tache}
		tachesEnCours.Lock()
		tachesEnCours.m[tache] = e
		tachesEnCours.Unlock()
		t.Cleanup(func() { tachesEnCours.Lock(); delete(tachesEnCours.m, tache); tachesEnCours.Unlock() })
		x := SessionHub{Hache: "h" + tache, Projet: prefixeTache + tache, Mission: tache, Agent: agentServeurs, Serveurs: []string{r1}, Env: prod + "/site"}
		plan, err := outilProposerPlan(s, x, json.RawMessage(`{"serveur":"`+r1+`","resume":"Relancer nginx","etapes":[{"outil":"ssh_commande","arguments":{"commande":"systemctl restart nginx"},"pourquoi":"nouvelle configuration"}]}`))
		if err != nil {
			t.Fatalf("%s: proposer_plan: %v", mode, err)
		}

		n, lignes := attente("m285b"+mode[:1], "m285p"+mode[:1])+attente(tache, tache), append(sauts(build), sauts(prod)...)
		if mode == "manuel" {
			if n != 4 || nb != 1 || np != 0 || !attend || !strings.Contains(plan, `"etat": "attente"`) || len(lignes) != 0 {
				t.Fatalf("manuel: %d pending (want 4), deployments %d and %d (want 1 and 0), accord waits %v, skips %q, plan %s", n, nb, np, attend, lignes, plan)
			}
			continue
		}
		if n != 0 || nb != 2 || np != 1 || attend || !strings.Contains(plan, `"etat": "acceptee"`) {
			t.Fatalf("ignorer: %d pending (want 0), deployments %d and %d (want 2 and 1), accord waits %v, plan %s", n, nb, np, attend, plan)
		}
		if len(sauts(build)) != 1 || len(sauts(prod)) != 3 { // the relay, the message, the plan
			t.Fatalf("ignorer: one audit line for each skip wanted, got %q and %q", sauts(build), sauts(prod))
		}
		for _, l := range lignes {
			if !strings.Contains(l, "non demandé") {
				t.Errorf("audit line without its action: %q", l)
			}
		}
	}
}
