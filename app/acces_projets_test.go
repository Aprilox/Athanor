package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAccesVariablesProposees: every name derived from a type is a valid shell name for secrets.env.
func TestAccesVariablesProposees(t *testing.T) {
	for typ := range typesCoffre {
		if typ == "smtp" { // the email module writes its SMTP_* variables itself (email_config.go): no name proposed
			continue
		}
		v, ok := variableParType[typ]
		if !ok || !nomSecretValide(v) {
			t.Errorf("type %s: proposed %q", typ, v)
		}
	}
	for _, c := range []struct {
		e    EntreeCoffre
		want string
	}{
		{EntreeCoffre{Nom: "STRIPE_SECRET", Famille: "secret", Type: "stripe"}, "STRIPE_SECRET"},
		{EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, "STRIPE_KEY"},
		{EntreeCoffre{Nom: "Cloudflare – martin.fr", Famille: "secret", Type: "cloudflare"}, "CLOUDFLARE_API_TOKEN"},
		{EntreeCoffre{Nom: "PATH", Famille: "secret", Type: "jeton"}, "API_TOKEN"}, // a name that changes how commands run
		{EntreeCoffre{Nom: "url du site", Famille: "variable", Type: "autre"}, "APP_VALUE"},
	} {
		if g := variableProposee(c.e); g != c.want {
			t.Errorf("%q: %q, want %q", c.e.Nom, g, c.want)
		}
	}
}

// TestResumeAcces: the badge counts every grant of the current environment, the sentence only what the
// pilot receives (code scope), split into test keys and variables; other environments and gone entries
// count for nothing.
func TestResumeAcces(t *testing.T) {
	entrees := []EntreeCoffre{
		{ID: "s1", Famille: "secret", Portee: "code"}, {ID: "s2", Famille: "secret", Portee: "code"},
		{ID: "v1", Famille: "variable", Portee: "code"}, {ID: "i1", Famille: "secret", Portee: "infra"},
	}
	p := ProjetAcces{Env: "dev", Acces: []AccesProjet{
		{Entree: "s1", Env: "dev"}, {Entree: "s2", Env: "dev"}, {Entree: "v1", Env: "dev"}, {Entree: "i1", Env: "dev"},
		{Entree: "s1", Env: "prod"}, {Entree: "disparue", Env: "dev"},
	}}
	natif := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(natif) })
	modeNatif.Store(false)
	r := resumerAcces(p, entrees)
	if r.Env != "dev" || r.Total != 4 || r.Secrets != 2 || r.Variables != 1 || r.Infra != 1 || len(r.Envs) != 3 {
		t.Fatalf("dev: %+v", r)
	}
	// C-M218-9: native mode refuses the infra entry (not counted) until the project is forced
	modeNatif.Store(true)
	if r := resumerAcces(p, entrees); r.Total != 3 || r.Infra != 0 {
		t.Fatalf("native: %+v", r)
	}
	p.ForcerInfraNatif = true
	if r := resumerAcces(p, entrees); r.Total != 4 || r.Infra != 1 {
		t.Fatalf("native forced: %+v", r)
	}
	modeNatif.Store(false)
	p.Env = "prod"
	if r := resumerAcces(p, entrees); r.Total != 1 || r.Secrets != 1 || r.Variables+r.Infra != 0 {
		t.Fatalf("prod: %+v", r)
	}
	p.Env = "staging"
	if r := resumerAcces(p, entrees); r.Total != 0 {
		t.Fatalf("staging: %+v", r)
	}
}

// TestAccesProjets walks GET/POST /api/acces: environments, proposed and edited names, duplicates,
// the vault and the projects followed, automatic grants made once, and no value anywhere.
func TestAccesProjets(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "TEMOIN-" + aleatoire(12)
	var reponses strings.Builder
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	poster := func(m map[string]string) (int, map[string]any) {
		if _, ok := m["mot_de_passe"]; !ok {
			m["mot_de_passe"] = mdpPreuveTest
		}
		b, _ := json.Marshal(m)
		return appel("POST", "/api/acces", string(b))
	}
	acces := func(v map[string]any) map[string]map[string]any { // "entry|env" → grant
		r := map[string]map[string]any{}
		l, _ := v["acces"].([]any)
		for _, x := range l {
			a := x.(map[string]any)
			r[a["entree"].(string)+"|"+a["env"].(string)] = a
		}
		return r
	}
	nouvelle := func(nom, typ, source string) string {
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: typ, Source: source}, temoin)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	stripe := nouvelle("Stripe test", "stripe", "")
	stripe2 := nouvelle("Stripe live", "stripe", "")
	importee := nouvelle("OPENAI_API_KEY", "openai", "projet:demo")
	var2, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SITE_URL", Famille: "variable", Type: "autre"}, "https://x.test")
	if err != nil {
		t.Fatal(err)
	}

	// default environment dev; the entry imported from this project is granted on dev automatically
	code, v := appel("GET", "/api/acces?projet=demo", "")
	if code != 200 || v["env"] != "dev" || len(v["envs"].([]any)) != 3 {
		t.Fatalf("GET: %d %v", code, v)
	}
	if a := acces(v)[importee+"|dev"]; a == nil || a["auto"] != "migration" || a["variable"] != "OPENAI_API_KEY" || a["nom"] != "OPENAI_API_KEY" {
		t.Fatalf("automatic grant: %v", v)
	}
	if code, _ := appel("GET", "/api/acces?projet=inconnu", ""); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
	if _, v := appel("GET", "/api/acces?projet=autre", ""); len(acces(v)) != 0 {
		t.Errorf("another project got the import: %v", v)
	}

	// C25: granting needs the password retyped (none yet, then a wrong one); taking back does not
	if c, v := poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "dev"}); c != http.StatusForbidden {
		t.Fatalf("add without a password set: %d %v", c, v)
	}
	avecMdpPreuve(s)
	if c, _ := poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "dev", "mot_de_passe": "faux"}); c != http.StatusUnauthorized {
		t.Fatalf("add with a wrong password: %d", c)
	}
	if !strings.Contains(reponses.String(), "Wrong password") {
		t.Error("wrong password not said")
	}
	// proposed name, then a second one of the same type gets a free name
	_, v = poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "dev"})
	if a := acces(v)[stripe+"|dev"]; a == nil || a["variable"] != "STRIPE_KEY" || a["type"] != "stripe" || a["famille"] != "secret" {
		t.Fatalf("add: %v", v)
	}
	_, v = poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe2, "env": "dev"})
	if a := acces(v)[stripe2+"|dev"]; a == nil || a["variable"] != "STRIPE_KEY_2" {
		t.Fatalf("second of a type: %v", v)
	}
	// refusals: environment, name, duplicate name, same entry twice, unknown entry
	for nom, c := range map[string]struct {
		corps map[string]string
		code  int
	}{
		"environment":    {map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "test"}, 400},
		"forbidden name": {map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "prod", "variable": "LD_PRELOAD"}, 400},
		"lower case":     {map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "prod", "variable": "stripe"}, 400},
		"duplicate name": {map[string]string{"projet": "demo", "action": "ajouter", "entree": var2.ID, "env": "dev", "variable": "STRIPE_KEY"}, 409},
		"same entry":     {map[string]string{"projet": "demo", "action": "ajouter", "entree": stripe, "env": "dev"}, 409},
		"unknown entry":  {map[string]string{"projet": "demo", "action": "ajouter", "entree": "0000", "env": "dev"}, 404},
		"rename dup":     {map[string]string{"projet": "demo", "action": "renommer", "entree": stripe2, "env": "dev", "variable": "STRIPE_KEY"}, 409},
		"bad action":     {map[string]string{"projet": "demo", "action": "voler", "entree": stripe, "env": "dev"}, 400},
		"unknown env":    {map[string]string{"projet": "demo", "action": "env", "env": "test"}, 400},
		"remove unknown": {map[string]string{"projet": "demo", "action": "retirer", "entree": stripe, "env": "prod"}, 404},
	} {
		if code, v := poster(c.corps); code != c.code {
			t.Errorf("%s: %d %v, want %d", nom, code, v, c.code)
		}
	}
	// the same name in another environment, staging and prod accepted, a variable granted, a rename
	for _, c := range []map[string]string{
		{"projet": "demo", "action": "ajouter", "entree": stripe2, "env": "prod", "variable": "STRIPE_KEY"},
		{"projet": "demo", "action": "ajouter", "entree": var2.ID, "env": "staging"},
		{"projet": "demo", "action": "renommer", "entree": stripe2, "env": "dev", "variable": "STRIPE_LIVE"},
		{"projet": "demo", "action": "env", "env": "prod"},
	} {
		if code, v := poster(c); code != 200 {
			t.Fatalf("%v: %d %v", c, code, v)
		}
	}
	_, v = appel("GET", "/api/acces?projet=demo", "")
	g := acces(v)
	if v["env"] != "prod" || g[stripe2+"|dev"]["variable"] != "STRIPE_LIVE" || g[stripe2+"|prod"]["variable"] != "STRIPE_KEY" || g[var2.ID+"|staging"]["variable"] != "SITE_URL" {
		t.Fatalf("after changes: %v", v)
	}

	// M224: "*" gives, renames and takes back an entry in every environment under one name, with one password
	_, v = poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": var2.ID, "env": "*"})
	g = acces(v)
	for _, e := range envsProjet {
		if g[var2.ID+"|"+e]["variable"] != "SITE_URL" {
			t.Fatalf("* add, %s: %v", e, g)
		}
	}
	if code, _ := poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": var2.ID, "env": "*"}); code != http.StatusConflict {
		t.Errorf("* add twice: %d", code)
	}
	_, v = poster(map[string]string{"projet": "demo", "action": "renommer", "entree": var2.ID, "env": "*", "variable": "SITE_ADDRESS"})
	if g = acces(v); g[var2.ID+"|dev"]["variable"] != "SITE_ADDRESS" || g[var2.ID+"|prod"]["variable"] != "SITE_ADDRESS" || g[var2.ID+"|staging"]["variable"] != "SITE_ADDRESS" {
		t.Fatalf("* rename: %v", g)
	}
	_, v = poster(map[string]string{"projet": "demo", "action": "retirer", "entree": var2.ID, "env": "*"})
	if g = acces(v); g[var2.ID+"|dev"] != nil || g[var2.ID+"|staging"] != nil || g[var2.ID+"|prod"] != nil {
		t.Fatalf("* remove: %v", g)
	}
	if code, _ := poster(map[string]string{"projet": "demo", "action": "retirer", "entree": var2.ID, "env": "*"}); code != http.StatusNotFound {
		t.Errorf("* remove twice: %d", code)
	}
	if code, _ := poster(map[string]string{"projet": "demo", "action": "ajouter", "entree": var2.ID, "env": "staging"}); code != 200 {
		t.Fatal("give back on staging:", code)
	}

	// removing the automatic grant does not bring it back
	if code, _ := poster(map[string]string{"projet": "demo", "action": "retirer", "entree": importee, "env": "dev"}); code != 200 {
		t.Fatal("remove automatic grant:", code)
	}
	if _, v := appel("GET", "/api/acces?projet=demo", ""); acces(v)[importee+"|dev"] != nil {
		t.Fatal("automatic grant made again")
	}

	// the grants survive a restart
	if a, err := ouvrirAcces(s.acces.chemin); err != nil || len(a.Projet("demo").Acces) != 4 || a.Projet("demo").Env != "prod" {
		t.Fatalf("reread: %v %+v", err, a.Projet("demo"))
	}

	// an entry deleted from the vault takes its grants with it
	if code, _ := appel("DELETE", "/api/coffre/"+stripe2, ""); code != 200 {
		t.Fatal("delete entry:", code)
	}
	for _, a := range s.acces.Projet("demo").Acces {
		if a.Entree == stripe2 {
			t.Fatal("grant of a deleted entry kept")
		}
	}

	// a renamed project keeps its grants, a removed one loses them
	if code, v := appel("POST", "/api/projets/gestion", `{"nom":"demo","action":"renommer","valeur":"demo2"}`); code != 200 {
		t.Fatalf("rename: %d %v", code, v)
	}
	if _, v := appel("GET", "/api/acces?projet=demo2", ""); len(acces(v)) != 2 || v["env"] != "prod" {
		t.Fatalf("renamed project: %v", v)
	}
	if code, v := appel("DELETE", "/api/projets/demo2", ""); code != 200 || len(s.acces.Projet("demo2").Acces) != 0 {
		t.Fatalf("removed project kept its grants: %d %v", code, v)
	}

	// no value in any answer nor in the file
	b, _ := os.ReadFile(s.acces.chemin)
	if strings.Contains(reponses.String(), temoin) || strings.Contains(string(b), temoin) || strings.Contains(string(b), "https://x.test") {
		t.Fatal("a value leaked into an answer or acces.json")
	}
	if fi, err := os.Stat(s.acces.chemin); err != nil || (os.PathSeparator == '/' && fi.Mode().Perm() != 0o600) {
		t.Fatalf("acces.json: %v %v", err, fi)
	}
}

// TestAccesModeNatif: native Windows mode refuses infra entries (409) unless the project is forced,
// with a confirmation and a log line without any value; code entries and the other mode are unchanged.
func TestAccesModeNatif(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	avant := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(avant) })
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	avecMdpPreuve(s)
	poster := func(m map[string]any) (int, string) {
		t.Helper()
		if _, ok := m["mot_de_passe"]; !ok {
			m["mot_de_passe"] = mdpPreuveTest
		}
		b, _ := json.Marshal(m)
		r := httptest.NewRequest("POST", "/api/acces", strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	temoin := "TEMOIN-" + aleatoire(12)
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	infra, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	lier := func(e EntreeCoffre, env string) (int, string) {
		return poster(map[string]any{"projet": "demo", "action": "ajouter", "entree": e.ID, "env": env})
	}

	// outside native mode: infra and code entries are linked as before
	modeNatif.Store(false)
	if c, b := lier(infra, "staging"); c != 200 {
		t.Fatalf("infra outside native mode: %d %s", c, b)
	}
	// native mode: infra refused with the explanation, code accepted
	modeNatif.Store(true)
	if c, b := lier(infra, "dev"); c != 409 || !strings.Contains(b, "WSL") {
		t.Fatalf("infra in native mode: %d %s", c, b)
	}
	if c, b := lier(code, "dev"); c != 200 || !strings.Contains(b, `"natif":true`) {
		t.Fatalf("code in native mode: %d %s", c, b)
	}
	// forcing needs a confirmation, then infra goes through and every step is logged without a value
	if c, _ := poster(map[string]any{"projet": "demo", "action": "forcer", "forcer": true}); c != 400 || s.acces.Projet("demo").ForcerInfraNatif {
		t.Fatalf("forcing without confirmation: %d", c)
	}
	if c, b := poster(map[string]any{"projet": "demo", "action": "forcer", "forcer": true, "confirme": true}); c != 200 || !strings.Contains(b, `"forcer_infra_natif":true`) {
		t.Fatalf("forcing: %d %s", c, b)
	}
	if c, b := lier(infra, "dev"); c != 200 {
		t.Fatalf("infra once forced: %d %s", c, b)
	}
	if c, _ := poster(map[string]any{"projet": "demo", "action": "forcer", "forcer": false}); c != 200 || s.acces.Projet("demo").ForcerInfraNatif {
		t.Fatalf("unforcing: %d", c)
	}
	if c, _ := lier(infra, "prod"); c != 409 {
		t.Fatalf("infra after unforcing: %d", c)
	}
	// the audit log: 3 grants (the forced one marked), the two "force" changes, never the value
	var actions []string
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if l.Projet != "demo" || l.De != "fenetre" || strings.Contains(l.Resultat, temoin) {
			t.Fatalf("audit line: %+v", l)
		}
		actions = append(actions, l.Action+" "+l.Resultat)
	}
	j := strings.Join(actions, "\n")
	if len(actions) != 5 || strings.Count(j, "forcer_infra_natif") != 2 || !strings.Contains(j, "acces_accorde_force Cloudflare ["+infra.ID) {
		t.Fatalf("audit log: %s", j)
	}
}

// TestAccesClePartagee: the same key in two projects' old files reaches both after the import at start-up
// (the grants are loaded before the vault, M42 B16), and deleting its entry takes it out of both (B17).
func TestAccesClePartagee(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	sys := nouveauFauxSysteme()
	ancien := systemeCoffre
	systemeCoffre = func() enveloppeSysteme { return sys }
	t.Cleanup(func() { systemeCoffre = ancien })
	if _, err := os.Stat(cheminConfig()); err == nil {
		t.Skip("a config exists next to the test binary")
	}
	migrationAuto = true
	t.Cleanup(func() { migrationAuto = !testing.Testing() })
	partage := "temoin-partage-" + aleatoire(8)
	var projets []Projet
	for _, n := range []string{"a", "b"} {
		p := Projet{Nom: n, Dossier: filepath.Join(d, n)}
		if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.Atelier(), "secrets.env"), []byte("SHARED_API_TOKEN='"+partage+"'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		projets = append(projets, p)
	}
	s := nouveauServeur(Config{Kit: filepath.Join(d, "kit"), Projets: projets, ProjetActif: "a"})
	if s.coffre == nil || s.coffreErr != nil || s.acces == nil {
		t.Fatalf("vault %v access %v", s.coffreErr, s.acces)
	}
	t.Cleanup(func() { _ = os.Remove(s.coffre.chemin); _ = os.Remove(s.acces.chemin) })
	contient := func(p Projet) bool {
		b, _ := os.ReadFile(filepath.Join(p.Atelier(), "secrets.env"))
		return strings.Contains(string(b), partage)
	}
	for _, p := range projets {
		if _, err := s.injecterAcces(p, nil); err != nil {
			t.Fatal(err)
		}
		if !contient(p) {
			t.Fatalf("project %s: shared key gone from secrets.env after the import (grants %+v)", p.Nom, s.acces.Projet(p.Nom).Acces)
		}
	}
	liste := s.coffre.Liste()
	if len(liste) != 1 {
		t.Fatalf("entries %d, want 1", len(liste))
	}
	r := httptest.NewRequest("DELETE", "/api/coffre/"+liste[0].ID, nil)
	r.SetPathValue("id", liste[0].ID)
	w := httptest.NewRecorder()
	s.apiCoffreSupprimer(w, r)
	if w.Code != 200 {
		t.Fatal("delete:", w.Code, w.Body.String())
	}
	for _, p := range projets {
		if contient(p) {
			t.Errorf("project %s: the deleted entry's value stays in secrets.env", p.Nom)
		}
	}
}

// TestAccesAncienFichierInchange (M224): the single list of the Access panel changes nothing in hub/acces.json;
// a file written by an older version (grants per environment) reads the same and comes back byte for byte after
// a grant is given then taken back.
func TestAccesAncienFichierInchange(t *testing.T) {
	ancien := `{
  "version": 1,
  "projets": {
    "boutique": {
      "env": "staging",
      "forcer_infra_natif": false,
      "acces": [
        {
          "entree": "a1",
          "env": "dev",
          "variable": "STRIPE_KEY",
          "cree": "2026-10-02T08:15:00Z"
        },
        {
          "entree": "a2",
          "env": "prod",
          "variable": "STRIPE_KEY",
          "cree": "2026-10-02T08:16:00Z"
        },
        {
          "entree": "v1",
          "env": "staging",
          "variable": "SITE_URL",
          "auto": "migration",
          "cree": "2026-10-03T09:00:00Z"
        }
      ]
    }
  },
  "auto_faits": [
    "v1"
  ]
}`
	chemin := filepath.Join(t.TempDir(), "acces.json")
	if err := os.WriteFile(chemin, []byte(ancien), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if p := a.Projet("boutique"); p.Env != "staging" || len(p.Acces) != 3 || p.Acces[1].Env != "prod" {
		t.Fatalf("read: %+v", p)
	}
	if _, err := a.Ajouter("boutique", EntreeCoffre{ID: "v2", Famille: "variable", Type: "autre", Nom: "API_URL"}, "dev", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Retirer("boutique", "v2", "dev"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(chemin); string(b) != ancien {
		t.Fatalf("file changed:\n%s", b)
	}
}
