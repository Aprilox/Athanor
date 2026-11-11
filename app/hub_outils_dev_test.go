package main

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestOutilsDev: the five tools of the Dev pilot through the real MCP route: names only (never a
// value), an approval for demander_acces (once), the bus refusal of demander_deploiement, a blocker in
// the bell, and every tool held to the token's project.
func TestOutilsDev(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	for _, nom := range []string{"demo", "autre"} {
		p := Projet{Nom: nom, Dossier: filepath.Join(d, nom)}
		if err := os.MkdirAll(filepath.Join(p.Atelier(), "logs"), 0o755); err != nil {
			t.Fatal(err)
		}
		s.config.Projets = append(s.config.Projets, p)
	}
	temoin := "témoin-outils-" + aleatoire(6)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct{ projet, env, v string }{{"demo", "dev", "STRIPE_KEY"}, {"demo", "staging", "STRIPE_STAGING"}, {"autre", "dev", "AUTRE_KEY"}} {
		if _, err := s.acces.Ajouter(g.projet, e, g.env, g.v, ""); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.routes(mux)
	jeton, err := s.jetons.Creer("demo", outilsPilote)
	if err != nil {
		t.Fatal(err)
	}
	b := "Bearer " + jeton
	reponses := ""
	appel := func(nom, args string) (string, bool) {
		t.Helper()
		rep := appelMCP(t, mux, b, appelOutil(nom, args))
		if rep.Error != nil || len(rep.Result.Content) != 1 {
			t.Fatalf("%s %s: %+v", nom, args, rep)
		}
		reponses += rep.Result.Content[0].Text + "\n"
		return rep.Result.Content[0].Text, rep.Result.IsError
	}

	// tools/list: the five, with short English descriptions
	rep := appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var noms []string
	for _, o := range rep.Result.Tools {
		noms = append(noms, o.Name)
	}
	if !slices.Equal(noms, outilsPilote) {
		t.Fatalf("tools/list: %v", noms)
	}
	for _, n := range outilsPilote {
		if dsc := outilsHub[n].Description; len(dsc) > 200 || strings.ContainsAny(dsc, "éèàç") {
			t.Errorf("%s: description %q", n, dsc)
		}
	}

	// etat and environnements: names of the variables received, never a value, never another project
	txt, er := appel("etat", "{}")
	if er || !strings.Contains(txt, `"STRIPE_KEY"`) || strings.Contains(txt, "STRIPE_STAGING") || strings.Contains(txt, "AUTRE_KEY") || !strings.Contains(txt, `"projet": "demo"`) {
		t.Errorf("etat: %s", txt)
	}
	txt, er = appel("environnements", "{}")
	if er || !strings.Contains(txt, `"environnement": "dev"`) || !strings.Contains(txt, "STRIPE_STAGING") || strings.Contains(txt, "AUTRE_KEY") {
		t.Errorf("environnements: %s", txt)
	}

	// another project in the arguments: refused by every tool, nothing done
	for _, n := range outilsPilote {
		if txt, er = appel(n, `{"projet":"autre"}`); !er || !strings.Contains(txt, "your session's project") {
			t.Errorf("%s with another project: %v %s", n, er, txt)
		}
	}
	if l, _ := s.approbations.Liste(); len(l) != 0 {
		t.Fatalf("an approval from a refused call: %+v", l)
	}

	// demander_acces: one pending approval for demo's current environment, the same one when asked again
	if _, er = appel("demander_acces", `{"nom":"OPENAI_API_KEY"}`); !er {
		t.Error("demander_acces without raison accepted")
	}
	txt, er = appel("demander_acces", `{"nom":"OPENAI_API_KEY","raison":"résumés de test"}`)
	l, _ := s.approbations.Liste()
	if er || len(l) != 1 || !strings.HasPrefix(txt, "waiting for the owner, id "+l[0].ID) {
		t.Fatalf("demander_acces: %v %s %+v", er, txt, l)
	}
	if a := l[0]; a.Projet != "demo" || a.Env != "dev" || a.Action != "demander_acces" || a.Details["nom"] != "OPENAI_API_KEY" || a.Raison != "résumés de test" || a.Etat != "attente" {
		t.Errorf("approval: %+v", a)
	}
	if txt, _ = appel("demander_acces", `{"nom":"OPENAI_API_KEY","raison":"encore"}`); !strings.Contains(txt, l[0].ID) {
		t.Errorf("asked again: %s", txt)
	}
	if l, _ = s.approbations.Liste(); len(l) != 1 {
		t.Errorf("asked again: %d approvals", len(l))
	}
	if txt, _ = appel("etat", "{}"); !strings.Contains(txt, l[0].ID) || !strings.Contains(txt, `"etat": "attente"`) {
		t.Errorf("etat without the request: %s", txt)
	}

	// demander_deploiement: through the bus to servers, which does not exist yet
	if txt, er = appel("demander_deploiement", `{"environnement":"prod","commit":"abc123"}`); !er || !strings.Contains(txt, "Servers module does not exist yet") {
		t.Errorf("demander_deploiement: %v %s", er, txt)
	}
	if _, er = appel("demander_deploiement", `{"environnement":"ailleurs","commit":"abc123"}`); !er {
		t.Error("unknown environment accepted")
	}

	// signaler: the bell of the Dev module, demo's element only, and the audit log
	if txt, er = appel("signaler", `{"blocage":"La base de test refuse la connexion"}`); er {
		t.Fatalf("signaler: %s", txt)
	}
	var vu bool
	for _, el := range (moduleDev{}).Elements(s) {
		a := el.Attente
		switch {
		case el.ID == "demo":
			vu = a != nil && a.Raison == "The pilot reports a blocker: {0}" && len(a.Args) == 1 && a.Args[0] == "La base de test refuse la connexion"
		case a != nil:
			t.Errorf("%s waits: %+v", el.ID, a)
		}
	}
	if !vu {
		t.Error("blocker not in the bell")
	}

	// the other project's token sees its own project only
	j2, _ := s.jetons.Creer("autre", outilsPilote)
	b = "Bearer " + j2
	if txt, _ = appel("etat", "{}"); !strings.Contains(txt, "AUTRE_KEY") || strings.Contains(txt, "STRIPE") || strings.Contains(txt, l[0].ID) {
		t.Errorf("etat of autre: %s", txt)
	}

	journal := auditTexte(t)
	for _, m := range []string{"approbation demandée: demander_acces", "blocage signalé", "La base de test refuse la connexion", "module absent", "mcp signaler"} {
		if !strings.Contains(journal, m) {
			t.Errorf("%q not in the audit log", m)
		}
	}
	if strings.Contains(journal, temoin) || strings.Contains(reponses, temoin) {
		t.Error("a vault value in an answer or the audit log")
	}
}
