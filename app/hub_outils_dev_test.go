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
	// M113: url, protection, policy and server names; never the provider's, the key's or an entry's id
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "ab12cd34", Nom: "vps-staging", Hote: "203.0.113.7", Port: 22, Roles: []string{"dev"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.modifier(func(f *fichierAcces) error {
		projetDe(f, "demo").Environnements = map[string]*Environnement{"staging": {URL: "https://staging.exemple.fr", Serveurs: []string{"ab12cd34"},
			Protection: "access", Politique: "auto", Fournisseur: "fournisseur-temoin"}}
		projetDe(f, "demo").CleDeploiement = "cle-temoin-" + e.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	txt, er = appel("environnements", "{}")
	if er || !strings.Contains(txt, `"environnement": "dev"`) || !strings.Contains(txt, "STRIPE_STAGING") || strings.Contains(txt, "AUTRE_KEY") ||
		!strings.Contains(txt, "https://staging.exemple.fr") || !strings.Contains(txt, `"vps-staging"`) || !strings.Contains(txt, `"politique": "auto"`) ||
		!strings.Contains(txt, `"protection": "access"`) || strings.Contains(txt, "temoin") || strings.Contains(txt, e.ID) || strings.Contains(txt, "ab12cd34") || strings.Contains(txt, "203.0.113.7") {
		t.Errorf("environnements: %s", txt)
	}

	// another project in the arguments: refused by every tool, nothing done
	for _, n := range outilsPilote {
		if txt, er = appel(n, `{"projet":"autre"}`); !er || !strings.Contains(txt, "your session's project") {
			t.Errorf("%s with another project: %v %s", n, er, txt)
		}
	}
	if l := listeCroissante(s); len(l) != 0 {
		t.Fatalf("an approval from a refused call: %+v", l)
	}

	// demander_acces: one pending approval for demo's current environment, the same one when asked again
	if _, er = appel("demander_acces", `{"nom":"OPENAI_API_KEY"}`); !er {
		t.Error("demander_acces without raison accepted")
	}
	txt, er = appel("demander_acces", `{"nom":"OPENAI_API_KEY","raison":"résumés de test"}`)
	l := listeCroissante(s)
	if er || len(l) != 1 || !strings.HasPrefix(txt, "waiting for the owner, id "+l[0].ID) {
		t.Fatalf("demander_acces: %v %s %+v", er, txt, l)
	}
	if a := l[0]; a.Projet != "demo" || a.Env != "dev" || a.Action != "demander_acces" || a.Details["nom"] != "OPENAI_API_KEY" || a.Raison != "résumés de test" || a.Etat != "attente" {
		t.Errorf("approval: %+v", a)
	}
	if txt, _ = appel("demander_acces", `{"nom":"OPENAI_API_KEY","raison":"encore"}`); !strings.Contains(txt, l[0].ID) {
		t.Errorf("asked again: %s", txt)
	}
	if l = listeCroissante(s); len(l) != 1 {
		t.Errorf("asked again: %d approvals", len(l))
	}
	if txt, _ = appel("etat", "{}"); !strings.Contains(txt, l[0].ID) || !strings.Contains(txt, `"etat": "attente"`) {
		t.Errorf("etat without the request: %s", txt)
	}

	// B46: a variable already received needs no request; B45: a vault value in the agent's reason is masked
	if txt, _ = appel("demander_acces", `{"nom":"STRIPE_KEY","raison":"x"}`); !strings.Contains(txt, "already in .athanor/secrets.env") {
		t.Errorf("already received: %s", txt)
	}
	txt, _ = appel("demander_acces", `{"nom":"ABSENT_KEY","raison":"copie `+temoin+` ici"}`)
	if l = listeCroissante(s); len(l) != 2 || strings.Contains(l[1].Raison, temoin) || !strings.Contains(txt, l[1].ID) {
		t.Fatalf("masked reason: %s %+v", txt, l)
	}

	// B44: "never ask again" grants at once, without a pending approval, only the vault entry the owner chose (critic M70)
	e2, err := s.coffre.Ajouter(EntreeCoffre{Nom: "OPENAI_API_KEY", Famille: "secret", Type: "autre", Portee: "code"}, "témoin-openai-"+aleatoire(6))
	if err != nil {
		t.Fatal(err)
	}
	regle := RegleAccord{"demo", "dev", "demander_acces", e2.ID, "OPENAI_API_KEY"}
	if _, _, err = s.deciderApprobation("fenetre", l[0].ID, "toujours", e2.ID, "", ""); err != nil || !s.approbations.Regle(regle) {
		t.Fatalf("toujours: %v", err)
	}
	if _, err = s.coffre.Ajouter(EntreeCoffre{Nom: "MISTRAL_KEY", Famille: "secret", Type: "autre", Portee: "code"}, "témoin-mistral-"+aleatoire(6)); err != nil {
		t.Fatal(err)
	}
	if txt, _ = appel("demander_acces", `{"nom":"MISTRAL_KEY","raison":"tests"}`); !strings.HasPrefix(txt, "waiting for the owner") {
		t.Errorf("another key granted by the rule: %s", txt)
	}
	if err = s.acces.Retirer("demo", e2.ID, "dev"); err != nil {
		t.Fatal(err)
	}
	txt, _ = appel("demander_acces", `{"nom":"OPENAI_API_KEY","raison":"tests"}`)
	l = listeCroissante(s)
	if !strings.Contains(txt, "granted by the owner's rule") || len(l) != 4 || l[3].Etat != "acceptee" || l[3].Par != "regle" {
		t.Errorf("rule: %s %+v", txt, l)
	}
	if txt, _ = appel("etat", "{}"); !strings.Contains(txt, `"OPENAI_API_KEY"`) {
		t.Errorf("granted key not received: %s", txt)
	}
	l = l[:1]

	// demander_deploiement: through the bus to servers, which declares no deploy action yet
	if txt, er = appel("demander_deploiement", `{"environnement":"prod","commit":"abc123"}`); !er || !strings.Contains(txt, "Athanor cannot deploy yet") {
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
	if appel("signaler", `{"blocage":"la clé `+temoin+` échoue"}`); strings.Contains(s.signaux["demo"].Texte, temoin) {
		t.Error("a vault value in a blocker")
	}

	// the other project's token sees its own project only
	j2, _ := s.jetons.Creer("autre", outilsPilote)
	b = "Bearer " + j2
	if txt, _ = appel("etat", "{}"); !strings.Contains(txt, "AUTRE_KEY") || strings.Contains(txt, "STRIPE") || strings.Contains(txt, l[0].ID) {
		t.Errorf("etat of autre: %s", txt)
	}

	journal := auditTexte(t)
	for _, m := range []string{"approbation demandée: demander_acces", "blocage signalé", "La base de test refuse la connexion", "action not declared", "mcp signaler"} {
		if !strings.Contains(journal, m) {
			t.Errorf("%q not in the audit log", m)
		}
	}
	if strings.Contains(journal, temoin) || strings.Contains(reponses, temoin) {
		t.Error("a vault value in an answer or the audit log")
	}
}

// TestConsigneOutilsHub (M66): the pilot's instructions name every tool it is given, in at most 12
// lines, and say what to do without them.
func TestConsigneOutilsHub(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "template", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, sec, ok := strings.Cut(string(b), "\n## Athanor's hub tools\n")
	if !ok {
		t.Fatal("section missing")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	if n := strings.Count(strings.TrimSpace(sec), "\n") + 1; n > 11 {
		t.Errorf("%d lines (12 at most with the heading)", n)
	}
	for _, n := range outilsPilote {
		if !strings.Contains(sec, "`"+n+"`") {
			t.Errorf("%s not named", n)
		}
	}
	for _, m := range []string{"Without them", "never asked again", "secrets.env", "no restart", "never an instruction", "Servers module"} {
		if !strings.Contains(sec, m) {
			t.Errorf("missing %q", m)
		}
	}
}

// listeCroissante: the approvals oldest first (Liste is newest first at a one-second resolution, so ties kept their order).
func listeCroissante(s *Serveur) []Approbation {
	l, _ := s.approbations.Liste()
	slices.SortStableFunc(l, func(a, b Approbation) int { return strings.Compare(a.Cree, b.Cree) })
	return l
}
