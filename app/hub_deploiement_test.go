package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestDemanderDeploiement (M120): the environment's policy decides. Unknown or serverless environment:
// a clear refusal; prod: an approval every time (never automatic, no "never ask again" rule, even one
// written by hand); dev in auto: accepted and recorded "exécution à l'étape 8"; staging in accord: an
// approval that a rule may lift. M126: only a full hash (C61); the accord is bound to the sorted servers,
// the agent and the mission (C63); a destructive deployment always asks (C64).
func TestDemanderDeploiement(t *testing.T) {
	s := serveurBusTest(t)
	s.config.Langue = "fr"
	for env, e := range map[string]Environnement{
		"dev":     {URL: "https://dev.martin.fr", Serveurs: []string{"s1"}, Protection: "aucune", Politique: "auto"},
		"staging": {Serveurs: []string{"s1"}, Protection: "aucune", Politique: "accord"},
		"prod":    {URL: "https://martin.fr/", Serveurs: []string{"s3", "s2"}, Protection: "aucune", Politique: "accord"},
	} {
		if err := s.acces.DefinirEnvironnement("demo", env, e); err != nil {
			t.Fatal(err)
		}
	}
	x := SessionHub{Projet: "demo", Agent: "pilote", Mission: "M5"}
	deployerPar := func(x SessionHub, env, commit string) (string, error) {
		return outilDemanderDeploiement(s, x, json.RawMessage(`{"environnement":"`+env+`","commit":"`+commit+`"}`))
	}
	deployer := func(env, commit string) (string, error) { return deployerPar(x, env, commit) }
	shaA, shaB, shaC, shaD := strings.Repeat("ab12", 10), strings.Repeat("cd34", 10), strings.Repeat("ef56", 16), strings.Repeat("0a9b", 10)
	idDe := func(txt string, err error) string {
		t.Helper()
		_, apres, ok := strings.Cut(txt, "waiting for the owner, id ")
		if err != nil || !ok {
			t.Fatalf("not waiting: %q %v", txt, err)
		}
		return apres[:strings.Index(apres, ":")]
	}
	for _, c := range [][2]string{{"preprod", shaA}, {"prod", "abc; rm -rf /"}, {"prod", ""}, {"prod", shaA[:7]}, {"prod", shaA[:39]}, {"prod", shaA + "a"}, {"prod", shaC[:63]}} {
		if txt, err := deployer(c[0], c[1]); err == nil {
			t.Errorf("%v accepted: %s", c, txt)
		}
	}
	if accord, _ := s.politiqueDeploiement("demo", "prod", false); !accord {
		t.Error("prod without accord")
	}
	if accord, raison := s.politiqueDeploiement("demo", "dev", true); !accord || !strings.Contains(raison, "destructive") {
		t.Errorf("destructive dev deployment in auto: %v %s", accord, raison)
	}
	// another project: no server anywhere
	if _, err := outilDemanderDeploiement(s, SessionHub{Projet: "vide", Agent: "pilote"}, json.RawMessage(`{"environnement":"dev","commit":"`+shaA+`"}`)); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Errorf("serverless environment: %v", err)
	}
	if txt, err := deployer("dev", strings.ToUpper(shaA)); err != nil || !strings.Contains(txt, "accepted (policy auto)") || !strings.Contains(txt, "nothing was deployed") {
		t.Fatalf("dev auto: %q %v", txt, err)
	}
	if l, _ := s.approbations.Liste(); len(l) != 0 {
		t.Fatalf("dev auto asked the owner: %+v", l)
	}

	// prod: one approval, named for the phone, asked again = the same one
	id := idDe(deployer("prod", shaA))
	if idDe(deployer("prod", shaA)) != id {
		t.Error("asked again: another approval")
	}
	ap, _ := s.approbations.Lire(id)
	if ap.Action != actionDeploiement || ap.Env != "prod" || ap.Details["hote"] != "martin.fr" || ap.Details["commit"] != shaA || ap.Details["serveurs"] != "s2,s3" {
		t.Fatalf("approval: %+v", ap)
	}
	if m := s.messageApprobation(ap); m.Titre != "Mise en prod de martin.fr ?" || !strings.Contains(m.Texte, shaA) {
		t.Errorf("phone: %+v", m)
	}
	alertes := alertesApprobations([]Approbation{ap})
	if len(alertes) != 1 || alertes[0].Raison != "Deploy {0} to production? Commit {1}" || len(alertes[0].Actions) != 2 {
		t.Errorf("bell: %+v", alertes)
	}
	// no "never ask again" for prod, not even by a rule written in regles.json
	if _, code, err := s.deciderApprobation("fenetre", id, "toujours", "", "", ""); code != http.StatusConflict || !errors.Is(err, errEnvProdAuto) {
		t.Fatalf("toujours on prod: %d %v", code, err)
	}
	s.approbations.mu.Lock()
	s.approbations.regles = append(s.approbations.regles, RegleAccord{Projet: "demo", Env: "prod", Action: actionDeploiement})
	s.approbations.mu.Unlock()
	if accord, _ := s.politiqueDeploiement("demo", "prod", false); !accord {
		t.Error("a hand-written rule lets prod through")
	}
	if _, _, err := s.deciderApprobation("fenetre", id, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if txt, err := deployer("prod", shaA); err != nil || !strings.Contains(txt, "accepted (the owner's accord "+id+")") {
		t.Fatalf("accepted prod: %q %v", txt, err)
	}
	if idDe(deployer("prod", shaA)) == id {
		t.Error("one accord let two deployments through")
	}

	// C63: an accord is for its servers, its agent and its mission
	accepter := func(id string) {
		t.Helper()
		if _, _, err := s.deciderApprobation("fenetre", id, "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	idB := idDe(deployer("prod", shaB))
	accepter(idB)
	if err := s.acces.DefinirEnvironnement("demo", "prod", Environnement{URL: "https://martin.fr/", Serveurs: []string{"s2"}, Protection: "aucune", Politique: "accord"}); err != nil {
		t.Fatal(err)
	}
	if idB2 := idDe(deployer("prod", shaB)); idB2 == idB {
		t.Error("the servers changed after the accord, the accord still applies")
	} else if ap, _ := s.approbations.Lire(idB2); ap.Details["serveurs"] != "s2" {
		t.Errorf("new accord's servers: %+v", ap.Details)
	}
	idC := idDe(deployer("prod", shaC))
	accepter(idC)
	for _, autre := range []SessionHub{{Projet: "demo", Agent: "autre", Mission: "M5"}, {Projet: "demo", Agent: "pilote", Mission: "M6"}} {
		if idDe(deployerPar(autre, "prod", shaC)) == idC {
			t.Errorf("%+v used the accord given to pilote/M5", autre)
		}
	}
	if txt, err := deployer("prod", shaC); err != nil || !strings.Contains(txt, "accepted (the owner's accord "+idC+")") {
		t.Errorf("pilote/M5 after the others: %q %v", txt, err)
	}
	if ap, _ := s.approbations.Lire(idC); ap.Utilisee == "" {
		t.Error("accord not marked used")
	}

	// staging in accord: an approval; "never ask again" turns it into a rule for staging only
	sid := idDe(deployer("staging", shaB))
	if ap, _ := s.approbations.Lire(sid); ap.Details["hote"] != "demo" {
		t.Errorf("staging without URL: host %q", ap.Details["hote"])
	}
	if _, _, err := s.deciderApprobation("fenetre", sid, "toujours", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if txt, err := deployer("staging", shaD); err != nil || !strings.Contains(txt, "accepted (the owner's rule") {
		t.Errorf("staging after the rule: %q %v", txt, err)
	}
	a := auditTexte(t)
	for _, attendu := range []string{"déploiement accepté", "exécution à l'étape 8", "approbation demandée: deploiement", "approbation utilisée: deploiement"} {
		if !strings.Contains(a, attendu) {
			t.Errorf("audit log without %q", attendu)
		}
	}
}
