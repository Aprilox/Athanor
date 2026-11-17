package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// inventaireTest: servers ids in the inventory, roles as given (M247: a server the inventory does not know
// reads as production, so a test's environments name known servers).
func inventaireTest(t *testing.T, roles []string, ids ...string) {
	t.Helper()
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		for _, id := range ids {
			l = append(l, ServeurInventaire{ID: id, Nom: id, Hote: "127.0.0.1", Port: 22, Roles: roles, Cree: time.Now()})
		}
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPrudenceRenforcee (M247): the guards follow Production, the old prod role and unknown servers, never the
// environment's name; under stronger caution no "never ask again", elsewhere the policy and the rules decide.
func TestPrudenceRenforcee(t *testing.T) {
	s := serveurBusTest(t)
	inventaireTest(t, nil, "v-dev")
	inventaireTest(t, []string{"prod"}, "v-prod")
	for env, e := range map[string]Environnement{
		"dev":     {URL: "https://dev.martin.fr", Serveurs: []string{"v-dev"}, Politique: "auto"},
		"site":    {URL: "https://martin.fr", Serveurs: []string{"v-dev"}, Production: true},
		"vitrine": {URL: "https://vitrine.martin.fr", Serveurs: []string{"v-prod"}},
		"essai":   {URL: "https://essai.martin.fr", Serveurs: []string{"v-dev"}, Politique: "accord"},
	} {
		if err := s.acces.definirAncien("demo", env, e); err != nil {
			t.Fatal(env, err)
		}
	}
	// a server removed from the inventory after the environment was set: in doubt, caution
	if err := s.acces.definirAncien("demo", "orphelin", Environnement{URL: "https://orphelin.martin.fr", Serveurs: []string{"v-dev"}, Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "v-tmp", Nom: "v-tmp", Hote: "127.0.0.1", Port: 22, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.definirAncien("demo", "orphelin", Environnement{URL: "https://orphelin.martin.fr", Serveurs: []string{"v-tmp"}, Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { return l[:len(l)-1], nil }); err != nil {
		t.Fatal(err)
	}
	for env, attendu := range map[string]bool{"dev": false, "essai": false, "site": true, "vitrine": true, "orphelin": true, "prod": true, "inconnu": false} {
		if got := s.prudenceRenforcee("demo", env); got != attendu {
			t.Errorf("prudenceRenforcee(%s) = %v", env, got)
		}
	}
	if !s.serveurPrudent("v-prod") || !s.serveurPrudent("absent") {
		t.Error("a server with the old prod role, or unknown, is prudent")
	}
	// v-dev serves the Production environment "site": its plans always ask
	if !s.serveurPrudent("v-dev") {
		t.Error("a server of a Production environment is prudent")
	}

	// a deployment: Production always asks and refuses "never ask again"; dev (auto) goes, essai (accord) asks
	// and accepts a rule
	if accord, _ := s.politiqueDeploiement("demo", "site", false); !accord {
		t.Error("Production deployed without accord")
	}
	if accord, _ := s.politiqueDeploiement("demo", "dev", false); accord {
		t.Error("dev in auto asks")
	}
	for env, toujours := range map[string]bool{"site": false, "essai": true} {
		ap, err := s.demanderApprobation(Approbation{Projet: "demo", Env: env, Agent: "pilote", Action: actionDeploiement,
			Details: map[string]string{"commit": "abc1234", "hote": env}})
		if err != nil {
			t.Fatal(err)
		}
		if ap.Production == toujours {
			t.Errorf("%s: Production %v", env, ap.Production)
		}
		_, _, err = s.deciderApprobation("test", ap.ID, "toujours", "", "", "")
		if (err == nil) != toujours {
			t.Errorf("%s: never ask again → %v", env, err)
		}
		if a := alertesApprobations([]Approbation{ap}); len(a) != 1 || len(a[0].Actions) != map[bool]int{true: 3, false: 2}[toujours] {
			t.Errorf("%s: the alert's actions: %+v", env, a)
		}
	}

	// a risky action on any server goes through the accord: destructive on dev waits and the owner's "never ask
	// again" lifts it; on the Production environment it waits always, even when not destructive, and no rule
	for env, regle := range map[string]bool{"dev": true, "site": false} {
		x := Approbation{Projet: "demo", Env: env, Agent: "pilote", Action: "supprimer_fichiers", Details: map[string]string{"n": env}}
		y, attend, err := s.exigerAccord(x, true)
		if err != nil || !attend {
			t.Fatalf("%s destructive: %v %v", env, attend, err)
		}
		if _, _, err := s.deciderApprobation("test", y.ID, "toujours", "", "", ""); (err == nil) != regle {
			t.Errorf("%s: never ask again → %v", env, err)
		}
		if _, attend, _ := s.exigerAccord(x, true); attend == regle {
			t.Errorf("%s: after never ask again, waits = %v", env, attend)
		}
		if _, attend, _ := s.exigerAccord(Approbation{Projet: "demo", Env: env, Agent: "pilote", Action: "lire_etat"}, false); attend == regle {
			t.Errorf("%s: a non-destructive action waits = %v", env, attend)
		}
	}
}

// TestMissionCarteProduction (M247): the orchestrator's production flag reaches the card and its policy; the
// card's final environment is its Production one, the milestone relay goes to the other.
func TestMissionCarteProduction(t *testing.T) {
	var c CarteMission
	if err := json.Unmarshal([]byte(`{"envs":[{"nom":"site","url":"https://martin.fr","production":true},{"nom":"apercu","url":"https://apercu.martin.fr"}]}`), &c); err != nil {
		t.Fatal(err)
	}
	if envFinal(c) != "site" || envJalonCarte(c) != "apercu" {
		t.Errorf("final %q, milestone %q", envFinal(c), envJalonCarte(c))
	}
	if politiqueEnvMission(true, "autonome") != "accord" || politiqueEnvMission(false, "prod") != "auto" || politiqueEnvMission(false, "tout") != "accord" {
		t.Error("policies")
	}
	x := contexteCarte{envs: map[string]map[string]Environnement{}}
	cc := calculerCarte(c, "autonome", x)
	if !strings.Contains(enJSON(cc.Approbations), "mise_en_prod") || cc.Envs[0].Politique != "accord" || cc.Envs[1].Politique != "auto" {
		t.Errorf("card: %s", enJSON(cc))
	}
	// a "site de dev": no production, no final deployment, deployed at each milestone under the policy
	var d CarteMission
	_ = json.Unmarshal([]byte(`{"envs":[{"nom":"site","url":"https://dev.martin.fr"}]}`), &d)
	if envFinal(d) != "" || envJalonCarte(d) != "site" || strings.Contains(enJSON(calculerCarte(d, "prod", x).Approbations), "mise_en_prod") {
		t.Errorf("dev site: %q %q", envFinal(d), envJalonCarte(d))
	}
	// an existing Production environment stays one, whatever the agent sends
	x.envs["demo"] = map[string]Environnement{"site": {URL: "https://martin.fr", Production: true, Politique: "accord"}}
	d.Projet.Nom = "demo"
	if e := calculerCarte(d, "autonome", x).Envs[0]; !e.Production || e.Politique != "accord" {
		t.Errorf("Production lowered: %+v", e)
	}
	// a card written before M247: its "prod" environment is the final one
	var v CarteMission
	_ = json.Unmarshal([]byte(`{"envs":[{"nom":"dev"},{"nom":"prod"}]}`), &v)
	if envFinal(v) != "prod" || envJalonCarte(v) != "dev" {
		t.Errorf("old card: %q %q", envFinal(v), envJalonCarte(v))
	}
}
