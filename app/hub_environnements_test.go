package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValiderEnvironnement(t *testing.T) {
	inv := []ServeurInventaire{{ID: "a1", Nom: "vps-prod", Roles: []string{"prod"}}, {ID: "b2", Nom: "vps-dev", Roles: []string{"dev"}},
		{ID: "c3", Nom: "nu", Roles: []string{}}, {ID: "d4", Nom: "mixte", Roles: []string{"dev", "prod"}}}
	ok := func(nom string, e Environnement) Environnement {
		t.Helper()
		r, err := validerEnvironnement(nom, e, inv)
		if err != nil {
			t.Fatalf("%s %+v: %v", nom, e, err)
		}
		return r
	}
	r := ok("dev", Environnement{URL: " https://dev.exemple.fr/app ", Serveurs: []string{"b2", "c3", "b2"}})
	if r.URL != "https://dev.exemple.fr/app" || !slices.Equal(r.Serveurs, []string{"b2", "c3"}) || r.Protection != "aucune" || r.Politique != "accord" {
		t.Errorf("defaults: %+v", r)
	}
	if r := ok("staging", Environnement{URL: "HTTPS://Staging.Exemple.FR:8443/x?a=B", Politique: "auto"}); r.URL != "https://staging.exemple.fr:8443/x?a=B" {
		t.Errorf("case: %q", r.URL) // B73: scheme and host in lower case, the path as typed
	}
	ok("staging", Environnement{URL: "https://xn--bcher-kva.fr:65535", Politique: "auto", Protection: "auth_proxy"})
	if r := ok("prod", Environnement{URL: "https://Exemple.fr", Serveurs: []string{"a1", "d4"}, Protection: "access"}); !r.Production {
		t.Error("an environment named prod is Production")
	}
	if r := ok("staging", Environnement{Serveurs: []string{"d4"}}); r.Production { // a VPS given both roles served dev and prod
		t.Error("a shared dev+prod VPS made staging Production")
	}
	// M246: free names, any server anywhere; a server with only the old prod role keeps its environment Production
	if r := ok("dev", Environnement{Serveurs: []string{"a1"}}); !r.Production {
		t.Error("a prod-role server must keep its environment Production")
	}
	if r := ok("demo.exemple.fr", Environnement{Serveurs: []string{"b2", "c3"}, Politique: "auto"}); r.Production || r.Politique != "auto" {
		t.Errorf("free name: %+v", r)
	}
	if r := ok("dev", Environnement{}); r.URL != "" || r.Serveurs == nil {
		t.Errorf("empty environment: %+v", r)
	}
	for _, c := range []struct {
		nom string
		e   Environnement
		mot string
	}{
		{"Bad_Name", Environnement{}, "Invalid environment name"},
		{"*", Environnement{}, "Invalid environment name"},
		{"../x", Environnement{}, "Invalid environment name"},
		{"", Environnement{}, "Invalid environment name"},
		{"dev", Environnement{URL: "ftp://exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://moi:secret@exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://"}, "Invalid URL"},
		{"dev", Environnement{URL: "exemple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://ex_emple.fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "javascript:alert(1)"}, "Invalid URL"},
		{"dev", Environnement{URL: "https://x.fr:"}, "Invalid port"},
		{"dev", Environnement{URL: "https://x.fr:0"}, "Invalid port"},
		{"dev", Environnement{URL: "https://x.fr:65536"}, "Invalid port"},
		{"dev", Environnement{URL: "https://x.fr:99999"}, "Invalid port"},
		{"dev", Environnement{URL: "https://x.fr/" + strings.Repeat("a", 2048)}, "Invalid URL"},
		{"dev", Environnement{URL: "https://" + strings.Repeat("a.", 127) + "fr"}, "Invalid URL"},
		{"dev", Environnement{URL: "http://10.0.0.5:8080"}, "not an IP address"},
		{"dev", Environnement{URL: "https://[::1]/"}, "not an IP address"},
		{"dev", Environnement{URL: "http://LocalHost:3000"}, "not an IP address"},
		{"dev", Environnement{URL: "http://app.localhost"}, "not an IP address"},
		{"dev", Environnement{URL: "https://bücher.fr"}, "punycode"},
		{"dev", Environnement{Protection: "mur"}, "Unknown protection"},
		{"dev", Environnement{URL: "HTTP://dev.exemple.fr", Protection: "auth_proxy"}, "needs an https:// address"},
		{"dev", Environnement{Politique: "jamais"}, "Unknown policy"},
		{"prod", Environnement{Politique: "auto"}, "always asks for your approval"},
		{"site", Environnement{Production: true, Politique: "auto"}, "always asks for your approval"},
		{"site", Environnement{Serveurs: []string{"a1"}, Politique: "auto"}, "always asks for your approval"},
		{"dev", Environnement{Serveurs: []string{"zz"}}, "Unknown server"},
		{"dev", Environnement{Fournisseur: "../x"}, "Unknown provider"},
	} {
		_, err := validerEnvironnement(c.nom, c.e, inv)
		_, entree := err.(erreurEntree)
		_, avecArgs := err.(erreurEntreeArgs)
		if err == nil || !(entree || avecArgs) || !strings.Contains(err.Error(), c.mot) {
			t.Errorf("%s %+v: %v, want %q", c.nom, c.e, err, c.mot)
		}
	}
}

// ancienAccesM246: an access file of an older version, with the three environments dev, staging and prod.
const ancienAccesM246 = `{"version":1,"projets":{"demo":{"env":"dev","forcer_infra_natif":false,"acces":[{"entree":"e1","env":"prod","variable":"API_KEY","cree":"2026-11-01T00:00:00Z"}],` +
	`"environnements":{"dev":{"url":"https://dev.exemple.fr","serveurs":["m1"],"protection":"auth_proxy","politique":"auto","deployer_au_jalon":true},` +
	`"staging":{"url":"https://pre.exemple.fr","serveurs":["p1"],"protection":"access","politique":"accord"},` +
	`"prod":{"url":"https://exemple.fr","serveurs":["p1"],"protection":"aucune","politique":"accord","fournisseur":"cf1","certificat":"c9","deploiement":{"methode":"statique","source":"artefact"}}},` +
	`"cle_deploiement":"k1"}},"auto_faits":["e1"]}`

// TestDeploiementProjet (M279): a project has one deployment, whose internal name the server finds: "site" for
// none, the only environment, and of the three of an older file the one deployed after each milestone; that
// file is read whole and never rewritten, its old prod keeps its caution, and an old deployment's history is read.
func TestDeploiementProjet(t *testing.T) {
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	env := func(e Environnement) Environnement { e.Serveurs = []string{"m1"}; return e }
	for _, c := range []struct {
		envs map[string]Environnement
		veut string
	}{
		{nil, "site"},
		{map[string]Environnement{"vitrine": env(Environnement{})}, "vitrine"},
		{map[string]Environnement{"prod": env(Environnement{Production: true})}, "prod"},
		{map[string]Environnement{"dev": env(Environnement{}), "staging": env(Environnement{}), "prod": env(Environnement{Production: true})}, "dev"},
		{map[string]Environnement{"dev": env(Environnement{}), "staging": env(Environnement{DeployerAuJalon: true}), "prod": env(Environnement{Production: true})}, "staging"},
		{map[string]Environnement{"a": env(Environnement{Production: true}), "b": env(Environnement{Production: true})}, "a"},
	} {
		if n := nomDeploiement(c.envs); n != c.veut {
			t.Errorf("nomDeploiement(%v) = %q, want %q", slices.Sorted(maps.Keys(c.envs)), n, c.veut)
		}
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "p1", Nom: "vps-prod", Hote: "203.0.113.5", Port: 22, Roles: []string{"prod"}, Cree: time.Now()},
			ServeurInventaire{ID: "m1", Nom: "mixte", Hote: "203.0.113.6", Port: 22, Roles: []string{"dev", "prod"}, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	chemin := filepath.Join(t.TempDir(), "acces.json")
	if err := os.WriteFile(chemin, []byte(ancienAccesM246), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	s := &Serveur{acces: a}
	if n := s.deploiementProjet("demo"); n != "dev" || s.envJalon("demo") != "dev" || !s.relaisActif("demo") {
		t.Errorf("the old file's deployment: %q, relay %v", n, s.relaisActif("demo"))
	}
	if n := s.deploiementProjet("inconnu"); n != "site" || s.relaisActif("inconnu") || (&Serveur{}).deploiementProjet("demo") != "site" {
		t.Errorf("a project without an environment: %q", n)
	}
	if e := a.Environnements("demo"); len(e) != 3 || e["prod"].Deploiement == nil || e["prod"].Certificat != "c9" || e["staging"].URL != "https://pre.exemple.fr" || len(a.Projet("demo").Acces) != 1 {
		t.Errorf("lost on reading: %+v", e)
	}
	if !s.prudenceRenforcee("demo", "prod") || !s.prudenceRenforcee("demo", "staging") || s.prudenceRenforcee("demo", "dev") {
		t.Error("the caution of the old environments changed")
	}
	// an old deployment's history, under its old environment's name
	if err := noterHistorique(Deploiement{ID: "d1", Projet: "demo", Env: "prod", Etat: "ok", Commit: strings.Repeat("a", 40)}); err != nil {
		t.Fatal(err)
	}
	if h := historiqueDeploiements("demo", "prod"); len(h) != 1 || h[0].ID != "d1" || len(historiqueDeploiements("demo", s.deploiementProjet("demo"))) != 0 {
		t.Errorf("history: %+v", h)
	}
	if b, _ := os.ReadFile(chemin); string(b) != ancienAccesM246 {
		t.Fatal("reading the deployment rewrote the file")
	}

	// the mission card (M279): an older card keeps its three environments, the linked one found without its name
	trois := CarteMission{Envs: []EnvCarte{{Nom: "staging"}, {Nom: "dev"}, {Nom: "prod"}}}
	sansNom := CarteMission{Titre: "t", Dev: BriefCarte{Brief: "b"}, JalonsEstimes: 1, Projet: ProjetCarte{Nom: "demo"}, Envs: []EnvCarte{{URL: "https://x.exemple.fr"}}}
	x := contexteCarte{projets: []Projet{{Nom: "demo"}}, envs: map[string]map[string]Environnement{"demo": a.Environnements("demo")}}
	if err := validerCarte(&sansNom, x); err != nil || sansNom.Envs[0].Nom != "dev" {
		t.Errorf("a card's environment without a name: %v %+v", err, sansNom.Envs)
	}
	if envJalonCarte(trois) != "dev" || envFinal(trois) != "prod" || envJalonCarte(CarteMission{Envs: []EnvCarte{{Nom: "site"}}}) != "site" ||
		envJalonCarte(CarteMission{Envs: []EnvCarte{{Nom: "site", Production: true}}}) != "" {
		t.Errorf("the card's environments: %q %q", envJalonCarte(trois), envFinal(trois))
	}
}

// TestEnvironnementsMigrationM246: a file of the dev/staging/prod era is read without loss, its prod (and an
// environment on a server with only the prod role) reads as Production, and a mere read never rewrites it.
func TestEnvironnementsMigrationM246(t *testing.T) {
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "p1", Nom: "vps-prod", Hote: "203.0.113.5", Port: 22, Roles: []string{"prod"}, Cree: time.Now()},
			ServeurInventaire{ID: "m1", Nom: "mixte", Hote: "203.0.113.6", Port: 22, Roles: []string{"dev", "prod"}, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	chemin := filepath.Join(t.TempDir(), "acces.json")
	ancien := ancienAccesM246
	if err := os.WriteFile(chemin, []byte(ancien), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	e := a.Environnements("demo")
	if len(e) != 3 || !e["prod"].Production || !e["staging"].Production || e["dev"].Production {
		t.Fatalf("Production after reading: %+v", e)
	}
	if d, p := e["dev"], e["prod"]; d.URL != "https://dev.exemple.fr" || d.Protection != "auth_proxy" || d.Politique != "auto" || !d.DeployerAuJalon ||
		p.Fournisseur != "cf1" || p.Certificat != "c9" || p.Deploiement == nil || p.Deploiement.Methode != "statique" || p.CleDeploiement != "k1" ||
		!slices.Equal(e["staging"].Serveurs, []string{"p1"}) {
		t.Errorf("lost on reading: %+v", e)
	}
	if pa := a.Projet("demo"); len(pa.Acces) != 1 || pa.Acces[0].Env != "prod" || !a.EnvConnu("demo", "prod") || a.EnvConnu("demo", "qa") {
		t.Errorf("grants: %+v", pa)
	}
	_ = a.EnvsDe("demo")
	if b, _ := os.ReadFile(chemin); string(b) != ancien {
		t.Fatal("a mere read rewrote the file")
	}
	// changed: written with Production, read back the same, the other environments untouched
	if err := a.DefinirEnvironnement("demo", "site", Environnement{URL: "https://site.exemple.fr", Serveurs: []string{"m1"}, Politique: "accord"}); err != nil {
		t.Fatal(err)
	}
	if a, err = ouvrirAcces(chemin); err != nil {
		t.Fatal(err)
	}
	e = a.Environnements("demo")
	if len(e) != 4 || !e["prod"].Production || e["site"].Production || e["site"].URL != "https://site.exemple.fr" || !slices.Contains(a.EnvsDe("demo"), "site") {
		t.Errorf("after a change: %+v", e)
	}
	if b, _ := os.ReadFile(chemin); !strings.Contains(strings.ReplaceAll(string(b), " ", ""), `"production":true`) {
		t.Error("Production not written once the file changed")
	}
}

// An access file written before stage 7 is read without loss, and keeps its grants once environments are added.
func TestEnvironnementsAncienFichier(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "acces.json")
	ancien := `{"version":1,"projets":{"demo":{"env":"staging","forcer_infra_natif":true,"acces":[{"entree":"e1","env":"dev","variable":"API_KEY","cree":"2026-11-01T00:00:00Z"}]}},"auto_faits":["e1"]}`
	if err := os.WriteFile(chemin, []byte(ancien), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if e := a.Environnements("demo"); len(e) != 0 || !slices.Equal(a.EnvsDe("demo"), envsProjet) { // M246: no environment made up
		t.Fatalf("environments of an old file: %+v", e)
	}
	if err := a.DefinirEnvironnement("demo", "dev", Environnement{URL: "https://dev.exemple.fr", Serveurs: []string{"b2"}, Protection: "aucune", Politique: "auto", CleDeploiement: "volee"}); err != nil {
		t.Fatal(err)
	}
	a, err = ouvrirAcces(chemin)
	if err != nil {
		t.Fatal(err)
	}
	p := a.Projet("demo")
	if p.Env != "staging" || !p.ForcerInfraNatif || len(p.Acces) != 1 || p.Acces[0].Variable != "API_KEY" || !slices.Contains(a.f.AutoFaits, "e1") {
		t.Errorf("grants lost: %+v %v", p, a.f.AutoFaits)
	}
	if e := a.Environnements("demo")["dev"]; e.URL != "https://dev.exemple.fr" || e.Politique != "auto" || e.CleDeploiement != "" {
		t.Errorf("environment read back (a request never sets the deployment key): %+v", e)
	}
}

func TestEnvironnementsAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	prod, dev := aleatoire(8), aleatoire(8)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: prod, Nom: "vps-prod", Hote: "203.0.113.5", Port: 22, Roles: []string{"prod"}, Cree: time.Now()},
			ServeurInventaire{ID: dev, Nom: "vps-dev", Hote: "203.0.113.6", Port: 22, Roles: []string{"dev"}, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	env := func(v map[string]any, nom string) map[string]any {
		e, _ := v["environnements"].(map[string]any)
		x, _ := e[nom].(map[string]any)
		return x
	}

	code, v := appel("GET", "/api/projets/demo/environnements", "")
	if code != 200 || len(v["envs"].([]any)) != 0 || len(v["prudence"].([]any)) != 0 { // M248: no card for an environment not made
		t.Fatalf("GET: %d %v", code, v)
	}
	if code, _ := appel("GET", "/api/projets/inconnu/environnements", ""); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
	// M281: a project has one deployment; this one is an older project with its three environments
	for _, nom := range []string{"dev", "staging", "prod"} {
		if err := s.acces.definirAncien("demo", nom, envDefaut()); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{
		`{"env":"prod","serveurs":["` + prod + `"],"politique":"auto"}`, `{"env":"dev","serveurs":["inconnu"]}`,
		`{"env":"dev","url":"https://a:b@exemple.fr"}`, `{"env":"Q A"}`, `{"env":"*"}`, `pas du json`,
	} {
		if code, v := appel("POST", "/api/projets/demo/environnements", c); code != 400 || v["erreur"] == nil {
			t.Errorf("%s: %d %v", c, code, v)
		}
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"prod","url":"https://exemple.fr","serveurs":["`+prod+`"],"protection":"access","cle_deploiement":"x"}`); code != 200 ||
		env(v, "prod")["url"] != "https://exemple.fr" || env(v, "prod")["cle_deploiement"] != nil {
		t.Errorf("prod: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"dev","url":"https://dev.exemple.fr","serveurs":["`+dev+`"],"politique":"auto"}`); code != 200 ||
		env(v, "dev")["politique"] != "auto" || env(v, "prod")["protection"] != "access" || fmt.Sprint(v["envs"], v["prudence"]) != "[dev prod staging] [prod]" {
		t.Errorf("dev: %d %v", code, v)
	}
	// B71: saving an environment with "Ask for my accord" ends its "never ask again" deployment rule, only that one
	s.approbations.mu.Lock()
	s.approbations.regles = append(s.approbations.regles, RegleAccord{Projet: "demo", Env: "staging", Action: actionDeploiement},
		RegleAccord{Projet: "demo", Env: "dev", Action: actionDeploiement}, RegleAccord{Projet: "demo", Env: "staging", Action: "demander_acces", Nom: "CLE"})
	s.approbations.mu.Unlock()
	if accord, _ := s.politiqueDeploiement("demo", "staging", false); accord {
		t.Error("the staging rule is not applied")
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"staging","politique":"accord"}`); code != 200 {
		t.Fatalf("staging: %d %v", code, v)
	}
	if accord, raison := s.politiqueDeploiement("demo", "staging", false); !accord {
		t.Errorf("the rule survived the accord policy: %s", raison)
	}
	if _, regles := s.approbations.Liste(); len(regles) != 2 || slices.Contains(regles, RegleAccord{Projet: "demo", Env: "staging", Action: actionDeploiement}) {
		t.Errorf("rules after staging: %+v", regles)
	}
	// M113: the inventory and a server's sheet say which environments use it
	if code, v := appel("GET", "/api/serveurs", ""); code != 200 || fmt.Sprint(v["environnements"].(map[string]any)[prod]) != "[demo/prod]" ||
		fmt.Sprint(v["environnements"].(map[string]any)[dev]) != "[demo/dev]" {
		t.Errorf("inventory: %d %v", code, v["environnements"])
	}
	if code, v := appel("GET", "/api/serveurs/"+dev, ""); code != 200 || fmt.Sprint(v["environnements"]) != "[demo/dev]" {
		t.Errorf("sheet: %d %v", code, v["environnements"])
	}
	if code, _ := appel("POST", "/api/projets/inconnu/environnements", `{"env":"dev"}`); code != 404 {
		t.Errorf("unknown project: %d", code)
	}

	// M133: going back; prod only with the owner's password, an environment never deployed says so
	for _, c := range []struct {
		corps string
		code  int
		texte string
	}{
		{`{"env":"prod"}`, 401, "Wrong password"},
		{`{"env":"prod","mot_de_passe":"faux"}`, 401, "Wrong password"},
		{`{"env":"prod","mot_de_passe":"` + mdpPreuveTest + `"}`, 409, "nothing was deployed"},
		{`{"env":"dev"}`, 409, "nothing was deployed"},
		{`{}`, 409, "nothing was deployed"}, // M279: no env, the project's one deployment
		{`{"env":"qa"}`, 400, "Invalid request"},
	} {
		if code, v := appel("POST", "/api/projets/demo/revenir", c.corps); code != c.code || !strings.Contains(fmt.Sprint(v["erreur"]), c.texte) {
			t.Errorf("going back %s: %d %v", c.corps, code, v)
		}
	}
	if _, v := appel("GET", "/api/projets/demo/environnements", ""); len(v["deploiements"].(map[string]any)) != 3 {
		t.Errorf("deployments shown: %v", v["deploiements"])
	}

	// M246: no dev/prod role rule between servers and environments any more
	if code, v := appel("POST", "/api/serveurs/"+dev, `{"roles":["dev","prod"]}`); code != 200 {
		t.Errorf("role change: %d %v", code, v)
	}

	// renaming then deleting a project carries its environments along
	if err := s.acces.RenommerProjet("demo", "demo2"); err != nil {
		t.Fatal(err)
	}
	if e := s.acces.Environnements("demo2"); e["dev"].URL != "https://dev.exemple.fr" || s.acces.Environnements("demo")["dev"].URL != "" {
		t.Errorf("rename: %+v", e)
	}
	if err := s.acces.RenommerProjet("demo2", "demo"); err != nil {
		t.Fatal(err)
	}

	// removing a server takes it out of the environments and says so
	if code, v := appel("DELETE", "/api/serveurs/"+dev, `{"mot_de_passe":"`+mdpPreuveTest+`"}`); code != 200 || len(v["environnements"].([]any)) != 1 || v["environnements"].([]any)[0] != "demo/dev" {
		t.Errorf("server removed: %d %v", code, v)
	}
	if e := s.acces.Environnements("demo"); len(e["dev"].Serveurs) != 0 || e["dev"].URL != "https://dev.exemple.fr" || len(e["prod"].Serveurs) != 1 {
		t.Errorf("after the removal: %+v", e)
	}
	if err := s.acces.SupprimerProjet("demo"); err != nil {
		t.Fatal(err)
	}
	if e := s.acces.Environnements("demo"); e["prod"].URL != "" {
		t.Errorf("deleted project kept its environments: %+v", e)
	}
}
