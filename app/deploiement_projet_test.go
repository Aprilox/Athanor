package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// ancienAccesM281: a project of the dev/staging/prod era, without grants (nothing for the vault to tidy).
const ancienAccesM281 = `{"version":1,"projets":{"demo":{"env":"dev","forcer_infra_natif":false,"acces":[],` +
	`"environnements":{"dev":{"url":"https://dev.exemple.fr","serveurs":["m1"],"protection":"auth_proxy","politique":"auto","deployer_au_jalon":true},` +
	`"staging":{"url":"https://pre.exemple.fr","serveurs":["p1"],"protection":"access","politique":"accord"},` +
	`"prod":{"url":"https://exemple.fr","serveurs":["p1"],"protection":"aucune","politique":"accord","fournisseur":"cf1","certificat":"c9","deploiement":{"methode":"statique","source":"artefact"}}}}}}`

// deploiementTest: a server with projects demo, autre and neuf, servers m1 (old roles dev and prod), p1 (old role
// prod) and a1 (no role), the access file given (none when empty), and a call through the real routes.
func deploiementTest(t *testing.T, acces string) (*Serveur, func(methode, chemin, corps string) (int, map[string]any)) {
	t.Helper()
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	if acces != "" {
		if err := errors.Join(os.MkdirAll(filepath.Dir(s.acces.chemin), 0o700), os.WriteFile(s.acces.chemin, []byte(acces), 0o600)); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "m1", Nom: "mixte", Hote: "203.0.113.6", Port: 22, Roles: []string{"dev", "prod"}, Cree: time.Now()},
			ServeurInventaire{ID: "p1", Nom: "vps-prod", Hote: "203.0.113.5", Port: 22, Roles: []string{"prod"}, Cree: time.Now()},
			ServeurInventaire{ID: "a1", Nom: "vps", Hote: "203.0.113.7", Port: 22, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	s.chargerAcces() // after the inventory: an older environment on a prod server reads as Production
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}, {Nom: "neuf", Dossier: filepath.Join(d, "neuf")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	return s, func(methode, chemin, corps string) (int, map[string]any) {
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
}

// TestDeploiementProjetAPI (M281): GET and POST /api/projets/{nom}/deploiement read and write the project's one
// deployment: each validation, the moment by default (at delivery), "on request" only where an older project
// already has it, the plan stored without a vault value, and what the route does not carry left as it was.
func TestDeploiementProjetAPI(t *testing.T) {
	s, appel := deploiementTest(t, "")
	const valeur = "temoin-valeur-m281-9f3k"
	api, err := s.coffre.Ajouter(EntreeCoffre{Nom: "CLOUDFLARE_TOKEN", Famille: "secret", Type: "autre"}, valeur)
	if err != nil {
		t.Fatal(err)
	}
	dep := func(v map[string]any) map[string]any { d, _ := v["deploiement"].(map[string]any); return d }
	const route = "/api/projets/demo/deploiement"

	code, v := appel("GET", route, "")
	if code != 200 || v["courant"] != "site" || dep(v)["moment"] != "livraison" || len(v["sites"].(map[string]any)) != 0 || len(dep(v)["serveurs"].([]any)) != 0 {
		t.Fatalf("GET of a project without a deployment: %d %v", code, v)
	}
	if code, _ := appel("GET", "/api/projets/inconnu/deploiement", ""); code != 404 {
		t.Errorf("unknown project: %d", code)
	}
	for _, c := range []string{
		`{"serveurs":["inconnu"]}`, `{"url":"https://203.0.113.9"}`, `{"url":"https://a:b@exemple.fr"}`, `{"moment":"hebdo"}`,
		`{"plan":"` + strings.Repeat("é", 4001) + `"}`, `{"apis":["inconnue"]}`, `{"courant":"absent"}`, `pas du json`,
	} {
		if code, v := appel("POST", route, c); code != 400 || v["erreur"] == nil {
			t.Errorf("%s: %d %v", c[:min(len(c), 40)], code, v)
		}
	}
	if e := s.acces.Environnements("demo"); len(e) != 0 {
		t.Fatalf("a refused request wrote: %+v", e)
	}

	// a new project: at delivery by default, stored under the internal name, never Production
	code, v = appel("POST", route, `{"serveurs":["a1"],"url":"https://Site.exemple.fr","plan":" nginx on port 8080, token `+valeur+` ","apis":["`+api.ID+`","`+api.ID+`"]}`)
	e := s.acces.Environnements("demo")["site"]
	if code != 200 || v["courant"] != "site" || dep(v)["moment"] != "livraison" || e.Moment != "livraison" || e.DeployerAuJalon || e.Production ||
		e.URL != "https://site.exemple.fr" || !slices.Equal(e.Serveurs, []string{"a1"}) || !slices.Equal(e.APIs, []string{api.ID}) ||
		strings.Contains(e.Plan, valeur) || !strings.HasPrefix(e.Plan, "nginx on port 8080") || e.Protection != "aucune" || e.Politique != "accord" {
		t.Fatalf("first write: %d %v %+v", code, v, e)
	}
	if b, _ := os.ReadFile(s.acces.chemin); strings.Contains(string(b), valeur) || strings.Contains(string(b), `"production"`) {
		t.Error("the access file holds the secret or Production")
	}
	// after each milestone: both fields written, for an older reader; the relay reads it
	if code, _ = appel("POST", route, `{"serveurs":["a1"],"url":"https://site.exemple.fr","moment":"jalon"}`); code != 200 {
		t.Fatalf("jalon: %d", code)
	}
	if e = s.acces.Environnements("demo")["site"]; e.Moment != "jalon" || !e.DeployerAuJalon || !s.relaisActif("demo") || e.Plan != "" || len(e.APIs) != 0 {
		t.Errorf("after each milestone: %+v", e)
	}
	// "on request" is not a choice for this project; a plan of exactly 4,000 characters is
	if code, _ = appel("POST", route, `{"serveurs":["a1"],"url":"https://site.exemple.fr"}`); code != 400 {
		t.Errorf("on request accepted for a project that never had it: %d", code)
	}
	if code, _ = appel("POST", route, `{"serveurs":["a1"],"moment":"livraison","plan":"`+strings.Repeat("é", 4000)+`"}`); code != 200 {
		t.Errorf("a plan of 4,000 characters: %d", code)
	}
	if code, _ = appel("POST", route, `{"serveurs":["a1"],"url":"https://site.exemple.fr","moment":"livraison"}`); code != 200 {
		t.Fatalf("livraison: %d", code)
	}
	// one host name per site, across projects
	if code, v = appel("POST", "/api/projets/neuf/deploiement", `{"url":"https://SITE.exemple.fr/x"}`); code != 400 || !strings.Contains(v["erreur"].(string), "site.exemple.fr") {
		t.Errorf("host already taken: %d %v", code, v)
	}

	// an older project deployed on request keeps that choice while it sends it, with its protection and policy
	if err := s.acces.DefinirEnvironnement("autre", "dev", Environnement{URL: "https://autre.exemple.fr", Serveurs: []string{"a1"}, Protection: "access", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	const autre = "/api/projets/autre/deploiement"
	if code, v = appel("GET", autre, ""); code != 200 || v["courant"] != "dev" || dep(v)["moment"] != nil && dep(v)["moment"] != "" {
		t.Fatalf("GET of an older project on request: %d %v", code, v)
	}
	if code, _ = appel("POST", autre, `{"serveurs":["a1"],"url":"https://autre.exemple.fr","plan":"uptime script"}`); code != 200 {
		t.Fatalf("on request kept: %d", code)
	}
	if e = s.acces.Environnements("autre")["dev"]; e.Moment != "" || e.DeployerAuJalon || e.Plan != "uptime script" || e.Protection != "access" || e.Politique != "auto" || len(s.acces.Environnements("autre")) != 1 {
		t.Errorf("an older project on request: %+v", e)
	}
	if code, _ = appel("POST", autre, `{"serveurs":["a1"],"url":"https://autre.exemple.fr","moment":"livraison"}`); code != 200 {
		t.Fatalf("leaving on request: %d", code)
	}
	if code, _ = appel("POST", autre, `{"serveurs":["a1"],"url":"https://autre.exemple.fr"}`); code != 400 {
		t.Errorf("back to on request: %d", code)
	}
	// an older writer stops the relay with DeployerAuJalon alone: the moment follows
	if err := s.acces.DefinirEnvironnement("demo", "site", Environnement{Serveurs: []string{"a1"}, Moment: "jalon"}); err != nil || s.acces.Environnements("demo")["site"].Moment != "" {
		t.Errorf("DeployerAuJalon false with Moment jalon: %v %+v", err, s.acces.Environnements("demo")["site"])
	}
}

// TestUnSeulDeploiement (M281): a project has one deployment: a second name is refused; an older access file
// with three environments is read without being rewritten, with each site's history; "courant" changes the
// site deployed, and the request then writes that one only.
func TestUnSeulDeploiement(t *testing.T) {
	s, appel := deploiementTest(t, ancienAccesM281)
	const route = "/api/projets/demo/deploiement"
	for _, d := range []Deploiement{{ID: "d1", Projet: "demo", Env: "prod", Etat: "ok", Commit: strings.Repeat("a", 40)}, {ID: "d2", Projet: "demo", Env: "dev", Etat: "ok", Commit: strings.Repeat("b", 40)}} {
		if err := noterHistorique(d); err != nil {
			t.Fatal(err)
		}
	}
	site := func(v map[string]any, nom string) map[string]any {
		m, _ := v["sites"].(map[string]any)
		x, _ := m[nom].(map[string]any)
		return x
	}
	histo := func(v map[string]any, nom string) string {
		m, _ := v["deploiements"].(map[string]any)
		l, _ := m[nom].([]any)
		if len(l) != 1 {
			return ""
		}
		return l[0].(map[string]any)["id"].(string)
	}

	// the older file: its three sites, the one deployed after each milestone is the project's, nothing rewritten
	code, v := appel("GET", route, "")
	if code != 200 || v["courant"] != "dev" || len(v["sites"].(map[string]any)) != 3 || v["deploiement"].(map[string]any)["moment"] != "jalon" ||
		site(v, "prod")["production"] != true || site(v, "staging")["url"] != "https://pre.exemple.fr" || histo(v, "prod") != "d1" || histo(v, "dev") != "d2" || histo(v, "staging") != "" {
		t.Fatalf("GET of an older project: %d %v", code, v)
	}
	if code, v = appel("POST", route, `{"courant":"absent"}`); code != 400 || v["erreur"] != "Unknown site" {
		t.Errorf("an unknown site as the deployment: %d %v", code, v)
	}
	if b, _ := os.ReadFile(s.acces.chemin); string(b) != ancienAccesM281 {
		t.Fatal("reading the deployment rewrote the older file")
	}

	// "courant": another site becomes the project's deployment, kept after a restart
	if code, v = appel("POST", route, `{"courant":"prod","url":"https://ignore.exemple.fr"}`); code != 200 || v["courant"] != "prod" || s.deploiementProjet("demo") != "prod" || s.envJalon("demo") != "prod" {
		t.Fatalf("courant: %d %v", code, v["courant"])
	}
	if a, err := ouvrirAcces(s.acces.chemin); err != nil || a.Projet("demo").DeploiementCourant != "prod" || len(a.Environnements("demo")) != 3 || a.Environnements("demo")["prod"].URL != "https://exemple.fr" {
		t.Errorf("courant read back: %v", err)
	}
	// only the current site is written; what Athanor set on it stays, and its Production still refuses the milestones
	if code, _ = appel("POST", route, `{"serveurs":["p1"],"url":"https://exemple.fr","moment":"jalon"}`); code != 400 {
		t.Errorf("an older Production site after each milestone: %d", code)
	}
	if code, v = appel("POST", route, `{"serveurs":["p1"],"url":"https://exemple.fr","plan":"certificate, domain","moment":"livraison"}`); code != 200 || histo(v, "prod") != "d1" {
		t.Fatalf("writing the current site: %d %v", code, v)
	}
	e := s.acces.Environnements("demo")
	if p := e["prod"]; len(e) != 3 || p.Plan != "certificate, domain" || p.Moment != "livraison" || !p.Production || p.Fournisseur != "cf1" || p.Certificat != "c9" || p.Deploiement == nil ||
		e["dev"].URL != "https://dev.exemple.fr" || !e["dev"].DeployerAuJalon || e["dev"].Plan != "" || e["staging"].Protection != "access" {
		t.Errorf("the sites after writing the current one: %+v", e)
	}
	// a site that no longer exists is no longer the deployment
	if err := s.acces.modifier(func(f *fichierAcces) error { delete(f.Projets["demo"].Environnements, "prod"); return nil }); err != nil || s.deploiementProjet("demo") != "dev" {
		t.Errorf("the chosen site removed: %v %q", err, s.deploiementProjet("demo"))
	}

	// a new project: one name, a second one refused by the store and by the older route, the same one written again
	un := Environnement{URL: "https://neuf.exemple.fr", Serveurs: []string{"a1"}, Protection: "aucune", Politique: "accord"}
	if err := s.acces.DefinirEnvironnement("neuf", "site", un); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.DefinirEnvironnement("neuf", "second", Environnement{Serveurs: []string{"a1"}}); err != errEnvUnSeul {
		t.Errorf("a second name: %v", err)
	}
	if code, v = appel("POST", "/api/projets/neuf/environnements", `{"env":"second","serveurs":["a1"]}`); code != 400 || v["erreur"] != string(errEnvUnSeul) {
		t.Errorf("a second name by the older route: %d %v", code, v)
	}
	un.Plan, un.Moment = "p", "livraison"
	if err := s.acces.DefinirEnvironnement("neuf", "site", un); err != nil || len(s.acces.Environnements("neuf")) != 1 || s.acces.Environnements("neuf")["site"].Plan != "p" {
		t.Errorf("the same name again: %v %+v", err, s.acces.Environnements("neuf"))
	}
	// the older route does not know the plan, the moment nor the API: saving there keeps them
	if code, _ = appel("POST", "/api/projets/neuf/environnements", `{"env":"site","url":"https://neuf.exemple.fr","serveurs":["a1"],"plan":"x","moment":"jalon"}`); code != 200 {
		t.Fatalf("the older route on the one site: %d", code)
	}
	if n := s.acces.Environnements("neuf")["site"]; n.Plan != "p" || n.Moment != "livraison" || n.DeployerAuJalon {
		t.Errorf("the older route changed the plan or the moment: %+v", n)
	}
	// the older project keeps its sites and may still write them (health, certificates)
	if err := s.acces.DefinirEnvironnement("demo", "staging", e["staging"]); err != nil || len(s.acces.Environnements("demo")) != 2 {
		t.Errorf("an older project's site: %v", err)
	}
}
