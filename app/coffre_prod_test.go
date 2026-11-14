package main

// M225: one entry, a testing value and a production value; the right one goes to each environment, the
// file stays readable by older versions (they see the testing value), no value is ever sent in clear.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoffreValeurProd(t *testing.T) {
	sys := nouveauFauxSysteme()
	c, chemin := coffreTest(t, sys)
	sfx := aleatoire(6)
	tEssai, tProd := "sk_test_temoin"+sfx, "sk_live_temoin"+sfx
	e := ajouter(t, c, "STRIPE_SECRET_KEY", "secret", "stripe", "code", tEssai)
	if err := c.PoserValeurProd(e.ID, tProd); err != nil {
		t.Fatal(err)
	}
	pour := func(c *Coffre, env, attendu string) {
		t.Helper()
		if v, err := c.ValeurPour(e.ID, env); err != nil || v != attendu {
			t.Fatalf("ValeurPour(%s) = %q, %v; want %q", env, v, err, attendu)
		}
	}
	pour(c, "dev", tEssai)
	pour(c, "staging", tEssai)
	pour(c, "prod", tProd)
	valeurAttendue(t, c, e.ID, tEssai)
	l := c.Liste()
	if len(l) != 1 || !l[0].DeuxValeurs || l[0].Valeur != "" || l[0].Prod != "" || l[0].EmpreinteProd != "" {
		t.Fatalf("Liste must say two values without any: %+v", l)
	}
	b, _ := os.ReadFile(chemin)
	if strings.Contains(string(b), tEssai) || strings.Contains(string(b), tProd) || strings.Contains(string(b), "deux_valeurs") {
		t.Fatal("the vault file holds a value in clear or the derived flag")
	}
	// reopened, renamed (the binding changes: both values resealed)
	c = rouvrir(t, chemin, sys)
	pour(c, "prod", tProd)
	if _, err := c.ModifierMeta(e.ID, "STRIPE_KEY", "stripe", "", "code", nil); err != nil {
		t.Fatal(err)
	}
	pour(c, "prod", tProd)
	pour(c, "dev", tEssai)
	// an older version: its entry type has no prod fields and still opens the testing value
	type ancienneEntree struct {
		ID, Nom, Famille, Type, Note, Portee, Valeur, Source, Empreinte string
		Cree, Modifie                                                   time.Time
	}
	var f struct{ Entrees []ancienneEntree }
	b, _ = os.ReadFile(chemin)
	if err := json.Unmarshal(b, &f); err != nil || len(f.Entrees) != 1 {
		t.Fatal("old reader:", err)
	}
	a := f.Entrees[0]
	if v, err := c.dechiffrer(EntreeCoffre{ID: a.ID, Nom: a.Nom, Famille: a.Famille, Type: a.Type, Portee: a.Portee, Valeur: a.Valeur}); err != nil || v != tEssai {
		t.Fatalf("an older version reads %q, %v; want the testing value", v, err)
	}
	// back to one value
	if err := c.PoserValeurProd(e.ID, ""); err != nil {
		t.Fatal(err)
	}
	pour(c, "prod", tEssai)
	if c.Liste()[0].DeuxValeurs {
		t.Fatal("still two values")
	}
	// a variable: both values readable, as its value already is
	v := ajouter(t, c, "API_URL", "variable", "autre", "code", "https://test.example")
	if err := c.PoserValeurProd(v.ID, "https://example"); err != nil {
		t.Fatal(err)
	}
	if x, _ := c.ValeurPour(v.ID, "prod"); x != "https://example" {
		t.Fatalf("variable prod = %q", x)
	}
}

func TestInjectionValeurProd(t *testing.T) {
	s := serveurCoffreTest(t)
	p := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(p.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{p}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
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
	sfx := aleatoire(6)
	tEssai, tProd := "sk_test_temoin"+sfx, "sk_live_temoin"+sfx
	code, v := appel("POST", "/api/coffre", `{"nom":"STRIPE_SECRET_KEY","famille":"secret","type":"stripe","valeur":"`+tEssai+`","prod":"`+tProd+`"}`)
	if code != 200 || v["deux_valeurs"] != true {
		t.Fatalf("add with two values: %d %v", code, v)
	}
	id := v["id"].(string)
	e := s.coffre.Liste()[0]
	for _, env := range envsProjet {
		if _, err := s.acces.Ajouter("demo", e, env, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	fichier := filepath.Join(p.Atelier(), "secrets.env")
	injecter := func(env, attendu, absent string) {
		t.Helper()
		if err := s.acces.ChangerEnv("demo", env); err != nil {
			t.Fatal(err)
		}
		if _, err := s.injecterAcces(p, nil); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(fichier)
		if !strings.Contains(string(b), "STRIPE_SECRET_KEY='"+attendu+"'") || strings.Contains(string(b), absent) {
			t.Fatalf("%s: secrets.env must hold %s only", env, attendu)
		}
	}
	injecter("dev", tEssai, tProd)
	injecter("prod", tProd, tEssai) // the testing value is recognised as a vault value and leaves
	injecter("staging", tEssai, tProd)
	// the production value removed: prod gets the one value
	if code, v = appel("POST", "/api/coffre/"+id, `{"prod":""}`); code != 200 || v["deux_valeurs"] == true {
		t.Fatalf("remove the production value: %d %v", code, v)
	}
	injecter("prod", tEssai, tProd)
	appel("POST", "/api/coffre/"+id, `{"prod":"`+tProd+`"}`)
	injecter("prod", tProd, tEssai)
	appel("GET", "/api/coffre", "")
	if strings.Contains(reponses.String(), tEssai) || strings.Contains(reponses.String(), tProd) {
		t.Fatal("an API answer holds a value")
	}
	if m := s.masquerSecrets("x " + tProd + " y"); strings.Contains(m, tProd) {
		t.Fatal("the production value is not masked out of the audit log")
	}
}
