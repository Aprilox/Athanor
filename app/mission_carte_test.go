package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func contexteCarteTest() contexteCarte {
	return contexteCarte{
		projets: []Projet{{Nom: "site-existant", Dossier: `C:\Dev\site`}, {Nom: "vieux", Dossier: `C:\Dev\vieux`, Archive: true}},
		inventaire: []ServeurInventaire{
			{ID: "s1dev", Nom: "vps-dev", Roles: []string{"dev"}, MotDePasseCoupe: true},
			{ID: "s2prod", Nom: "vps-prod", Roles: []string{"prod"}},
		},
		hotes:   map[string]string{"pris.fr": "autre/prod", "site.fr": "site-existant/prod"},
		entrees: []EntreeCoffre{{ID: "e1", Nom: "CF token", Type: "api_token", Portee: "infra"}, {ID: "e2", Nom: "Stripe", Type: "api_key", Portee: "code"}},
		zones:   []string{"martin.fr"},
		masquer: func(t string) string { return strings.ReplaceAll(t, "SECRET123", "***") },
	}
}

// carteMartin: the card of V§1 (bakery showcase site, protected dev + prod on the VPS).
func carteMartin() CarteMission {
	return CarteMission{
		Titre:  "Boulangerie Martin showcase site",
		Projet: ProjetCarte{Nom: "boulangerie-martin", Nouveau: true},
		Dev:    BriefCarte{Brief: "A showcase site for the Martin bakery"},
		Envs: []EnvCarte{
			{Nom: "dev", URL: "https://Dev.Martin.fr", Protection: "auth_proxy", Serveurs: []string{"s1dev"}},
			{Nom: "prod", URL: "https://martin.fr", Protection: "aucune", Serveurs: []string{"s2prod"}},
		},
		Serveurs:      BriefCarte{Brief: "DNS, certificates, proxy, protection, firewall"},
		Acces:         []AccesCarte{{Entree: "cf token", Pour: "boulangerie-martin/prod"}, {Entree: "SMTP", Pour: "boulangerie-martin"}},
		JalonsEstimes: 6,
	}
}

func TestValiderCarte(t *testing.T) {
	cas := []struct {
		nom    string
		change func(*CarteMission)
		erreur string // "" = valid
	}{
		{"martin", func(*CarteMission) {}, ""},
		{"projet existant, son propre hôte", func(c *CarteMission) {
			c.Projet = ProjetCarte{Nom: "Site-Existant"}
			c.Envs = []EnvCarte{{Nom: "prod", URL: "https://site.fr", Serveurs: []string{"s2prod"}}}
			c.Acces = []AccesCarte{{Entree: "Stripe", Pour: "s2prod"}}
		}, ""},
		{"titre vide", func(c *CarteMission) { c.Titre = "  " }, "title"},
		{"titre sur deux lignes", func(c *CarteMission) { c.Titre = "a\nb" }, "title"},
		{"jalons 0", func(c *CarteMission) { c.JalonsEstimes = 0 }, "jalons_estimes"},
		{"jalons 61", func(c *CarteMission) { c.JalonsEstimes = 61 }, "jalons_estimes"},
		{"aucune voie", func(c *CarteMission) { c.Dev.Brief, c.Serveurs.Brief, c.Acces = "", "", nil }, "at least one lane"},
		{"brief trop long", func(c *CarteMission) { c.Dev.Brief = strings.Repeat("é", maxBriefCarte+1) }, "brief"},
		{"nom de projet invalide", func(c *CarteMission) { c.Projet.Nom = "../x" }, "Invalid project name"},
		{"projet neuf déjà pris", func(c *CarteMission) { c.Projet.Nom = "SITE-EXISTANT" }, "already exists"},
		{"projet neuf sans brief Dev", func(c *CarteMission) { c.Dev.Brief = "" }, "needs a Dev brief"},
		{"projet inconnu", func(c *CarteMission) { c.Projet = ProjetCarte{Nom: "fantome"} }, "no project named"},
		{"projet archivé", func(c *CarteMission) { c.Projet = ProjetCarte{Nom: "vieux"} }, "no project named"},
		{"serveur inconnu", func(c *CarteMission) { c.Envs[0].Serveurs = []string{"zzz"} }, errServeurInconnu.Error()},
		{"URL sur une IP", func(c *CarteMission) { c.Envs[0].URL = "https://127.0.0.1" }, errEnvURLIP.Error()},
		{"auth_proxy en http", func(c *CarteMission) { c.Envs[0].URL = "http://dev.martin.fr" }, errEnvAuthHTTP.Error()},
		{"hôte pris ailleurs", func(c *CarteMission) { c.Envs[1].URL = "https://pris.fr" }, "already the address of autre/prod"},
		{"même hôte deux fois", func(c *CarteMission) { c.Envs[1].URL = "https://dev.martin.fr" }, "already the address of boulangerie-martin/dev"},
		{"environnement en double", func(c *CarteMission) { c.Envs[1].Nom = "dev" }, "given twice"},
		{"nom d'environnement invalide", func(c *CarteMission) { c.Envs[1].Nom = "Q A" }, errEnvNom.Error()},
		{"voie Serveurs sans serveur", func(c *CarteMission) { c.Envs[0].Serveurs, c.Envs[1].Serveurs = nil, nil }, "Servers lane needs"},
		{"pour hors de la carte", func(c *CarteMission) { c.Acces[0].Pour = "autre/prod" }, "pour of"},
		{"entrée sans nom", func(c *CarteMission) { c.Acces[0].Entree = "" }, "vault entry"},
		{"trop d'entrées", func(c *CarteMission) { c.Acces = make([]AccesCarte, maxAccesCarte+1) }, "vault entries at most"},
	}
	for _, tc := range cas {
		c := carteMartin()
		tc.change(&c)
		err := validerCarte(&c, contexteCarteTest())
		switch {
		case tc.erreur == "" && err != nil:
			t.Errorf("%s: %v", tc.nom, err)
		case tc.erreur != "" && (err == nil || !strings.Contains(err.Error(), tc.erreur)):
			t.Errorf("%s: error %v, want %q", tc.nom, err, tc.erreur)
		}
	}
	// what validation leaves: URL in lower case, entries resolved or flagged, texts masked, the project's real name
	c := carteMartin()
	c.Dev.Brief += " SECRET123"
	if err := validerCarte(&c, contexteCarteTest()); err != nil {
		t.Fatal(err)
	}
	if c.Envs[0].URL != "https://dev.martin.fr" || strings.Contains(c.Dev.Brief, "SECRET123") {
		t.Errorf("not normalised: %q %q", c.Envs[0].URL, c.Dev.Brief)
	}
	if a := c.Acces[0]; a.ID != "e1" || a.Entree != "CF token" || a.Portee != "infra" || a.ARemplir {
		t.Errorf("known entry: %+v", a)
	}
	if a := c.Acces[1]; a.ID != "" || !a.ARemplir {
		t.Errorf("missing entry must be to fill: %+v", a)
	}
	c = carteMartin()
	c.Projet = ProjetCarte{Nom: "SITE-existant"}
	c.Envs, c.Serveurs.Brief, c.Acces = nil, "", nil
	if err := validerCarte(&c, contexteCarteTest()); err != nil || c.Projet.Nom != "site-existant" {
		t.Errorf("existing project: %v %q", err, c.Projet.Nom)
	}
}

func TestCalculerCarte(t *testing.T) {
	x := contexteCarteTest()
	carte := carteMartin()
	if err := validerCarte(&carte, x); err != nil {
		t.Fatal(err)
	}
	types := func(c CarteMission) []string {
		l := []string{}
		for _, a := range c.Approbations {
			l = append(l, a.Type+":"+a.Cible)
		}
		return l
	}
	cas := []struct {
		nom       string
		politique string
		x         func(*contexteCarte)
		carte     func(*CarteMission)
		veut      []string
	}{
		// C84: the same rule as at the run: no rule in place, the dev plan asks too; prod's always does
		{"prod", "prod", nil, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"politique inconnue = prod", "n'importe", nil, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"tout", "tout", nil, nil, []string{"plan_serveurs:dev", "deploiement:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"autonome", "autonome", nil, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"coffre infra ouvert", "autonome", func(x *contexteCarte) { x.infraOuvert = true }, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod"}},
		{"entrée infra décochée", "autonome", nil, func(c *CarteMission) { c.Acces[0].Decoche = true }, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod"}},
		{"serveurs déjà durcis", "autonome", func(x *contexteCarte) { x.inventaire[1].MotDePasseCoupe = true }, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "deverrouiller_coffre:"}},
		{"sans prod", "tout", nil, func(c *CarteMission) { c.Envs = c.Envs[:1]; c.Acces = nil }, []string{"plan_serveurs:dev", "deploiement:dev"}},
		// rules in place: the dev server's lets its plan through, never a prod server's; tout ignores them all (C82)
		{"règles en place", "prod", func(x *contexteCarte) { x.regle = func(RegleAccord) bool { return true } }, nil, []string{"mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"règle d'un autre serveur", "prod", func(x *contexteCarte) {
			x.regle = func(r RegleAccord) bool { return r == reglePlan(prefixeTache, "s2prod") }
		}, nil, []string{"plan_serveurs:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
		{"tout et règles en place", "tout", func(x *contexteCarte) { x.regle = func(RegleAccord) bool { return true } }, nil, []string{"plan_serveurs:dev", "deploiement:dev", "mise_en_prod:https://martin.fr", "plan_serveurs:prod", "durcissement:vps-prod", "deverrouiller_coffre:"}},
	}
	for _, tc := range cas {
		xx, c := x, carte
		xx.inventaire = slices.Clone(x.inventaire)
		c.Acces = slices.Clone(carte.Acces)
		if tc.x != nil {
			tc.x(&xx)
		}
		if tc.carte != nil {
			tc.carte(&c)
		}
		if got := types(calculerCarte(c, tc.politique, xx)); !slices.Equal(got, tc.veut) {
			t.Errorf("%s: approvals %v, want %v", tc.nom, got, tc.veut)
		}
	}
	c := calculerCarte(carte, "prod", x)
	tc := c.Touche
	neuf, _ := os.UserHomeDir() // a new project goes next to the last project (a Windows path is relative elsewhere)
	if filepath.IsAbs(`C:\Dev\site`) {
		neuf = `C:\Dev`
	}
	if !slices.Equal(tc.Serveurs, []string{"vps-dev", "vps-prod"}) || !slices.Equal(tc.DNS, []string{"dev.martin.fr", "martin.fr"}) || !slices.Equal(tc.Zones, []string{"martin.fr"}) || tc.Dossier != filepath.Join(neuf, "boulangerie-martin") {
		t.Errorf("touched: %+v", tc)
	}
	for _, f := range []string{"/etc/caddy/sites/dev.martin.fr.caddy", "/etc/caddy/sites/martin.fr.caddy", "/srv/athanor/boulangerie-martin-dev", "/srv/athanor/boulangerie-martin-prod"} {
		if !slices.Contains(tc.Fichiers, f) {
			t.Errorf("touched files miss %s: %v", f, tc.Fichiers)
		}
	}
	if e := c.Estimation; e.Mesure || e.Jalons != 6 || e.MinutesMin != 113 || e.MinutesMax != 225 || e.CoutMax != 0 {
		t.Errorf("default estimate: %+v", e)
	}
	if c.AvertissementServeurs != "" {
		t.Errorf("warning outside native mode: %q", c.AvertissementServeurs)
	}
	xx := x
	xx.natif, xx.minutesCycle, xx.coutCycle = true, 40, 2
	c = calculerCarte(carte, "prod", xx)
	if e := c.Estimation; !e.Mesure || e.MinutesMin != 180 || e.MinutesMax != 360 || e.CoutMin != 9 || e.CoutMax != 18 {
		t.Errorf("measured estimate: %+v", e)
	}
	if c.AvertissementServeurs != infoServeursNatif {
		t.Errorf("native mode: no Servers information")
	}
	// an existing project: its folder is touched; zones without a provider: the last two labels
	e := carteMartin()
	e.Projet = ProjetCarte{Nom: "site-existant"}
	e.Envs = []EnvCarte{{Nom: "prod", URL: "https://www.site.fr", Serveurs: []string{"s2prod"}}}
	e.Acces = nil
	x.zones = nil
	if err := validerCarte(&e, x); err != nil {
		t.Fatal(err)
	}
	if c := calculerCarte(e, "prod", x); c.Touche.Dossier != `C:\Dev\site` || !slices.Equal(c.Touche.Zones, []string{"site.fr"}) {
		t.Errorf("existing project: %+v", c.Touche)
	}
	// C84: an existing dev environment that asks keeps asking (C81): its deployment is on the card, unless the
	// owner's rule lets it through (prod policy) or the tout launch ends that rule (C82)
	e.Envs = []EnvCarte{{Nom: "dev", URL: "https://dev.site.fr", Serveurs: []string{"s1dev"}}}
	e.Serveurs = BriefCarte{}
	if err := validerCarte(&e, x); err != nil {
		t.Fatal(err)
	}
	xx = x
	xx.envs = map[string]map[string]Environnement{"site-existant": {"dev": {URL: "https://dev.site.fr", Serveurs: []string{"s1dev"}, Politique: "accord"}}}
	if got := types(calculerCarte(e, "prod", xx)); !slices.Equal(got, []string{"deploiement:dev"}) {
		t.Errorf("existing dev that asks: %v", got)
	}
	xx.regle = func(r RegleAccord) bool {
		return r == RegleAccord{Projet: "site-existant", Env: "dev", Action: actionDeploiement}
	}
	if got := types(calculerCarte(e, "prod", xx)); len(got) != 0 {
		t.Errorf("deployment under the owner's rule still on the card: %v", got)
	}
	if got := types(calculerCarte(e, "tout", xx)); !slices.Equal(got, []string{"deploiement:dev"}) {
		t.Errorf("tout: the rule ends at the launch, the deployment asks: %v", got)
	}
}

// TestCarteChampsAgentIgnores: approvals, touched, estimate and warning sent by the agent never survive.
func TestCarteChampsAgentIgnores(t *testing.T) {
	brut := `{"titre":"Site","projet":{"nom":"site-existant"},"dev":{"brief":"Add a page"},"jalons_estimes":2,
		"approbations":[{"type":"rien"}],"touche":{"serveurs":["evil"],"dossier":"C:\\Windows","fichiers":["/etc/shadow"]},
		"estimation":{"jalons":999,"minutes_min":1,"mesure":true},"avertissement_serveurs":"none"}`
	var c CarteMission
	if err := json.Unmarshal([]byte(brut), &c); err != nil {
		t.Fatal(err)
	}
	x := contexteCarteTest()
	if err := validerCarte(&c, x); err != nil {
		t.Fatal(err)
	}
	c = calculerCarte(c, "tout", x)
	if len(c.Approbations) != 0 || len(c.Touche.Serveurs) != 0 || len(c.Touche.Fichiers) != 0 || c.Touche.Dossier != `C:\Dev\site` || c.AvertissementServeurs != "" {
		t.Errorf("agent's fields kept: %+v %+v", c.Approbations, c.Touche)
	}
	if e := c.Estimation; e.Jalons != 2 || e.Mesure || e.MinutesMin != 38 {
		t.Errorf("agent's estimate kept: %+v", e)
	}
}

func TestMoyennesCycles(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, ".athanor"), 0o700); err != nil {
		t.Fatal(err)
	}
	tsv := "entete\nt\t1\tm\tok\t30\t0\t0\t1.5\nt\t2\tm\tok\t50\t0\t0\t2.5\nt\t3\tm\techec\t0\t0\t0\t9\n"
	if err := os.WriteFile(filepath.Join(d, ".athanor", "stats.tsv"), []byte(tsv), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, c := moyennesCycles([]Projet{{Nom: "a", Dossier: d}, {Nom: "b", Dossier: filepath.Join(d, "absent")}}); m != 40 || c != 2 {
		t.Errorf("averages %v %v, want 40 2", m, c)
	}
	if m, c := moyennesCycles(nil); m != 0 || c != 0 {
		t.Errorf("no stats: %v %v", m, c)
	}
}
