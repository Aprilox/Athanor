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
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	poster := func(m map[string]string) (int, map[string]any) {
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
