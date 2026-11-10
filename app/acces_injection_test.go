package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// I1: on a realistic project, secrets.env holds exactly the code entries granted for the current
// environment; nothing else carries a value (envClaude, logs, acces.json, API answers).
func TestInjectionAcces(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	demo := Projet{Nom: "demo", Dossier: filepath.Join(d, "demo")}
	autre := Projet{Nom: "autre", Dossier: filepath.Join(d, "autre")}
	for _, p := range []Projet{demo, autre} {
		if err := os.MkdirAll(filepath.Join(p.Atelier(), "logs"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo, autre}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
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
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil { // for the infra entry
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	sfx := aleatoire(6)
	tDev, tProd, tNon, tInfra := "temoin-dev-"+sfx, "temoin-prod-"+sfx, "temoin-non-"+sfx, "temoin-infra-"+sfx
	tAncien := "temoin-ancien-" + sfx
	nouvelle := func(nom, typ, valeur string) EntreeCoffre {
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: typ}, valeur)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	dev, prod, _, infra := nouvelle("Stripe test", "stripe", tDev), nouvelle("Stripe live", "stripe", tProd),
		nouvelle("OpenAI", "openai", tNon), nouvelle("Cloudflare", "cloudflare", tInfra)
	for _, a := range []struct {
		e   EntreeCoffre
		env string
	}{{dev, "dev"}, {prod, "prod"}, {infra, "dev"}} {
		if _, err := s.acces.Ajouter("demo", a.e, a.env, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	// the old file: a value the vault lacks (kept), one the vault holds but does not grant (goes)
	fichier := filepath.Join(demo.Atelier(), "secrets.env")
	ecrireFichier(t, fichier, "# old\nANCIEN_JETON='"+tAncien+"'\nOPENAI_API_KEY='"+tNon+"'\n", 0o600)

	etat, err := s.injecterAcces(demo, nil)
	lire := func() string { b, _ := os.ReadFile(fichier); return string(b) }
	contenu := lire()
	if err != nil || etat.Etat != "ok" || !slices.Equal(etat.Donnees, []string{"STRIPE_KEY"}) || !slices.Equal(etat.Repli, []string{"ANCIEN_JETON"}) {
		t.Fatalf("injection: %v %+v", err, etat)
	}
	if !strings.Contains(contenu, "STRIPE_KEY='"+tDev+"'") || !strings.Contains(contenu, "ANCIEN_JETON='"+tAncien+"'") {
		t.Fatalf("missing in the file:\n%s", contenu)
	}
	for _, v := range []string{tProd, tNon, tInfra} {
		if strings.Contains(contenu, v) {
			t.Fatalf("not granted here, yet in the file: %s\n%s", v, contenu)
		}
	}
	if d := secretsDonnes(demo); len(d) != 2 || !d["STRIPE_KEY"] || !d["ANCIEN_JETON"] {
		t.Fatalf("secretsDonnes: %v", d)
	}
	if fi, err := os.Stat(fichier); err == nil && fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	// nothing changed: nothing written
	ecrit, _ := os.Stat(fichier)
	if _, err := s.injecterAcces(demo, nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fichier); !fi.ModTime().Equal(ecrit.ModTime()) || lire() != contenu {
		t.Error("rewritten without a change")
	}

	// Start: the injection runs even when the start itself fails (no kit here)
	ecrireFichier(t, fichier, "ANCIEN_JETON='"+tAncien+"'\n", 0o600)
	_, _ = s.demarrerPilote(demo)
	if !strings.Contains(lire(), "STRIPE_KEY='"+tDev+"'") {
		t.Fatalf("Start did not inject:\n%s", lire())
	}

	// the environment changes through the API: prod's entry instead of dev's
	code, v := appel("POST", "/api/acces", `{"projet":"demo","action":"env","env":"prod"}`)
	if c := lire(); code != 200 || !strings.Contains(c, tProd) || strings.Contains(c, tDev) {
		t.Fatalf("env prod: %d\n%s", code, c)
	}
	if inj, _ := v["injection"].(map[string]any); inj["etat"] != "ok" {
		t.Fatalf("GET injection: %v", v)
	}
	// the badge next to Start: the current environment and its state, in GET /api/etat too
	if r, _ := v["resume"].(map[string]any); r["env"] != "prod" || r["etat"] != "ok" {
		t.Fatalf("resume: %v", v["resume"])
	}
	if _, e := appel("GET", "/api/etat?p=demo", ""); e["projet"] == nil || e["projet"].(map[string]any)["acces"].(map[string]any)["env"] != "prod" {
		t.Fatalf("GET /api/etat badge: %v", e["projet"])
	}
	appel("POST", "/api/acces", `{"projet":"demo","action":"env","env":"dev"}`)
	// a granted value changed in the vault: the file follows
	tDev2 := "temoin-dev2-" + sfx
	if code, _ := appel("POST", "/api/coffre/"+dev.ID, `{"valeur":"`+tDev2+`"}`); code != 200 || !strings.Contains(lire(), tDev2) {
		t.Fatalf("value changed: %d\n%s", code, lire())
	}

	// no value anywhere else: the pilot's environment, the logs, acces.json, the API answers
	env := strings.Join((&pilote{}).envClaude(), "\n")
	temoins := []string{tDev, tDev2, tProd, tNon, tInfra, tAncien}
	for _, x := range temoins {
		if strings.Contains(env, x) || strings.Contains(reponses.String(), x) {
			t.Fatalf("value %s in envClaude or an API answer", x)
		}
	}
	for _, racine := range []string{d, filepath.Dir(s.acces.chemin)} {
		_ = filepath.WalkDir(racine, func(c string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || filepath.Base(c) == "secrets.env" {
				return nil
			}
			b, _ := os.ReadFile(c)
			for _, x := range temoins {
				if bytes.Contains(b, []byte(x)) {
					t.Errorf("value %s in %s", x, c)
				}
			}
			return nil
		})
	}

	// code key closed: the file stays as it is, the state says so
	avant := lire()
	s.coffre.mu.Lock()
	sys := s.coffre.systeme
	s.coffre.systeme, s.coffre.cleCode = nil, nil
	s.coffre.mu.Unlock()
	appel("POST", "/api/acces", `{"projet":"demo","action":"env","env":"prod"}`)
	_, v = appel("GET", "/api/acces?projet=demo", "")
	if inj, _ := v["injection"].(map[string]any); inj["etat"] != "verrouille" || lire() != avant {
		t.Fatalf("locked: %v\n%s", v, lire())
	}
	if _, e := appel("GET", "/api/etat?p=demo", ""); e["projet"].(map[string]any)["acces"].(map[string]any)["etat"] != "verrouille" {
		t.Fatalf("GET /api/etat locked: %v", e["projet"])
	}
	s.coffre.mu.Lock()
	s.coffre.systeme = sys
	s.coffre.mu.Unlock()
	appel("POST", "/api/acces", `{"projet":"demo","action":"env","env":"dev"}`)

	// Undo the import: the values it takes out of the vault go back into the files that had them
	tImport := "temoin-import-" + sfx + "-" + strings.Repeat("q7", 10)
	ecrireFichier(t, filepath.Join(autre.Atelier(), "secrets.env"), "IMPORTEE='"+tImport+"'\n", 0o600)
	ecrireFichier(t, fichier, lire()+"PARTAGEE='"+tImport+"'\n", 0o600)
	if code, v := appel("POST", "/api/coffre/migration", ""); code != 200 {
		t.Fatalf("import: %d %v", code, v)
	}
	fAutre := filepath.Join(autre.Atelier(), "secrets.env")
	lireAutre := func() string { b, _ := os.ReadFile(fAutre); return string(b) }
	if !strings.Contains(lireAutre(), tImport) || !strings.Contains(lire(), tImport) {
		t.Fatalf("the shared value left a file after the import:\n%s\n%s", lireAutre(), lire())
	}
	appel("POST", "/api/acces", `{"projet":"autre","action":"env","env":"staging"}`)
	if strings.Contains(lireAutre(), tImport) {
		t.Fatalf("not granted on staging, yet in the file:\n%s", lireAutre())
	}
	if code, v := appel("POST", "/api/coffre/migration/annuler", ""); code != 200 {
		t.Fatalf("undo: %d %v", code, v)
	}
	if !strings.Contains(lireAutre(), "IMPORTEE='"+tImport+"'") || !strings.Contains(lire(), "PARTAGEE='"+tImport+"'") ||
		!strings.Contains(lire(), "ANCIEN_JETON='"+tAncien+"'") {
		t.Fatalf("a value was lost by the undo:\n%s\n%s", lireAutre(), lire())
	}
	if strings.Contains(reponses.String(), tImport) {
		t.Fatal("imported value in an API answer")
	}
}
