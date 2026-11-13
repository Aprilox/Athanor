package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestHubRegistre(t *testing.T) {
	ids := map[string]bool{}
	for _, m := range modulesHub() {
		if ids[m.ID()] || m.ID() != strings.ToLower(m.ID()) || m.Nom() == "" {
			t.Fatalf("bad module id or name: %q %q", m.ID(), m.Nom())
		}
		ids[m.ID()] = true
	}
	if !ids["dev"] {
		t.Fatal("the Dev module is not registered")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a module registered twice must panic")
		}
	}()
	enregistrerModule(moduleDev{})
}

func TestHubDossierModule(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, d)
	}
	m := dossierModule("essai")
	if filepath.Base(m) != "essai" || filepath.Base(filepath.Dir(m)) != "modules" {
		t.Fatalf("module folder: %s", m)
	}
	if st, err := os.Stat(m); err != nil || !st.IsDir() {
		t.Fatalf("module folder not created: %v", err)
	}
}

func TestHubAPI(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(entete, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/hub", nil)
		if entete != "" {
			r.Header.Set("X-Athanor", entete)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookieAcces, Value: cookie})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, c := range [][2]string{{"", ""}, {"jeton", ""}, {"", "cle"}, {"autre", "cle"}} {
		if w := appel(c[0], c[1]); w.Code != http.StatusForbidden {
			t.Fatalf("/api/hub without the header or the cookie (%q, %q): %d", c[0], c[1], w.Code)
		}
	}
	w := appel("jeton", "cle")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/hub: %d %s", w.Code, w.Body.String())
	}
	var v struct {
		Modules []ModuleVue `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	var dev *ModuleVue
	for i := range v.Modules {
		if v.Modules[i].ID == "dev" {
			dev = &v.Modules[i]
		}
	}
	if len(v.Modules) < 4 || v.Modules[0].ID != "hub" || v.Modules[len(v.Modules)-1].ID != "reglages" {
		t.Fatalf("modules not in the menu's order (hub first, Settings last): %s", w.Body.String())
	}
	if dev == nil || dev.Nom != (moduleDev{}).Nom() || len(dev.Pages) == 0 || dev.Pages[0].Hash != "#suivi" || dev.Pages[0].Module != "dev" {
		t.Fatalf("Dev in /api/hub: %s", w.Body.String())
	}
	if dev.Resume.Etat != "inactif" || dev.Resume.Besoin != 0 || len(dev.Resume.Stats) == 0 {
		t.Fatalf("Dev summary without projects: %+v", dev.Resume)
	}
}

func TestHubBesoinProjet(t *testing.T) {
	for _, c := range []struct {
		pv   ProjetVue
		veut string
	}{
		{ProjetVue{PourVous: 2}, "pour_vous"},
		{ProjetVue{PourVous: 2, Archive: true}, ""},
		{ProjetVue{Actif: true, Phase: "attente_vous"}, "attente_vous"},
		{ProjetVue{Actif: true, Phase: "connexion"}, "connexion"},
		{ProjetVue{Actif: true, Phase: "budget"}, "budget"},
		{ProjetVue{Actif: false, Phase: "attente_vous"}, ""},
		{ProjetVue{Actif: true, Phase: "travail"}, ""},
		{ProjetVue{Actif: true, Phase: "quota"}, ""},
		{ProjetVue{Actif: true, Phase: "budget", PourVous: 1}, "pour_vous"},
	} {
		if g := besoinProjet(c.pv); g != c.veut {
			t.Errorf("besoinProjet(%+v) = %q, want %q", c.pv, g, c.veut)
		}
	}
}

// The display name of a module lives in one place: the Dev module's Nom(); the menu renders /api/hub.
func TestHubNomUnSeulEndroit(t *testing.T) {
	fichiers, _ := filepath.Glob("*.go")
	n := 0
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if c := strings.Count(string(b), `"Studio"`); c > 0 {
			if f != "hub_dev.go" {
				t.Errorf(`%s holds the literal "Studio"`, f)
			}
			n += c
		}
	}
	if n != 1 {
		t.Errorf(`the literal "Studio" appears %d times in the Go code, want 1 (hub_dev.go)`, n)
	}
	for _, f := range []string{"static/app.js", "static/index.html"} {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), `"Studio"`) || strings.Contains(string(b), ">Studio<") {
			t.Errorf(`%s names the Dev module itself (render the nom of /api/hub)`, f)
		}
	}
}

// TestHubLiensNotifications: every page a Web Push notification opens (the Go messages, their fallback and
// sw.js's) is an old page of app.js, never a bare "/" (it opened the Overview before the hub, Home now).
func TestHubLiensNotifications(t *testing.T) {
	js, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]bool{"options": true, "rejoindre": true} // the aliases naviguer() turns into a page
	for _, m := range regexp.MustCompile(`(?m)^pages\.([a-z]+) =`).FindAllStringSubmatch(string(js), -1) {
		pages[m[1]] = true
	}
	if !pages["suivi"] || !pages["pourvous"] {
		t.Fatalf("pages of app.js not found: %v", pages)
	}
	var urls []string
	goURL := regexp.MustCompile(`MessagePush\{[^}]*URL:\s*"([^"]*)"|m\.URL = "([^"]*)"`)
	fichiers, _ := filepath.Glob("*.go")
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range goURL.FindAllStringSubmatch(string(b), -1) {
			urls = append(urls, f+" "+m[1]+m[2])
		}
	}
	sw, err := os.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	// the fallback addresses (`: "/…"`, `|| "/…"`), not the startsWith("/") checks
	for _, m := range regexp.MustCompile(`(?:: |\|\| )"(/(?:#[a-z@-]*)?)"`).FindAllStringSubmatch(string(sw), -1) {
		urls = append(urls, "sw.js "+m[1])
	}
	if len(urls) < 7 {
		t.Fatalf("only %d notification addresses found: %v", len(urls), urls)
	}
	for _, u := range urls {
		_, adresse, _ := strings.Cut(u, " ")
		page, _, _ := strings.Cut(strings.TrimPrefix(adresse, "/#"), "@")
		page, _, _ = strings.Cut(page, "/") // #missions/<id>: the page, then its element
		if !strings.HasPrefix(adresse, "/#") || !pages[page] || page == "accueil" {
			t.Errorf("%s: a notification must open an old page (/#suivi…)", u)
		}
	}
}

// TestHubAncienneConfig: a config from before the hub (projects, settings, request queue, secrets) goes
// through two starts of the server and the "Got it" of the tour; none of its files is changed or lost.
func TestHubAncienneConfig(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	kit := filepath.Join(d, "kit")
	fichiers := map[string]string{
		filepath.Join(kit, ".env"):                            "LANGUAGE=fr\nGITHUB_TOKEN=temoin-env-7731\n",
		filepath.Join(kit, "extensions", "secrets.json"):      `{"essai":{"API_KEY":"temoin-ext-4402"}}`,
		filepath.Join(kit, "extensions", "bibliotheque.json"): `{"essai":{"command":"npx","args":["essai-mcp"]}}`,
	}
	var projets []Projet
	for _, nom := range []string{"site-vitrine", "jeu-2d", "outil cli"} {
		p := Projet{Nom: nom, Dossier: filepath.Join(d, "projets", nom)}
		projets = append(projets, p)
		fichiers[p.Demande()] = "# Demande\n\nUn " + nom + ".\n"
		fichiers[p.Plan()] = "# Plan\n\n- [x] M1 : squelette\n- [ ] M2 : tranche\n"
		fichiers[filepath.Join(p.FileAttente(), "01-suite.md")] = "Ajouter un mode sombre.\n"
		fichiers[filepath.Join(p.Atelier(), "secrets.env")] = "API_TOKEN=temoin-projet-9150\n"
		fichiers[filepath.Join(p.Dossier, ".env")] = "DB_PASSWORD=temoin-db-2286\n"
		fichiers[filepath.Join(p.Dossier, "index.html")] = "<h1>" + nom + "</h1>\n"
	}
	projets = append(projets, Projet{Nom: "ancien", Dossier: filepath.Join(d, "projets", "ancien"), Archive: true})
	ancienne := Config{Kit: kit, Projets: projets, ProjetActif: "jeu-2d", Port: 8765, Langue: "fr", Theme: "sombre", Accent: "vert", BienvenueVue: true, ProgrammesSupplementaires: "blender.exe"}
	b, err := json.MarshalIndent(ancienne, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	cfg := cheminConfig()
	if avant, err := os.ReadFile(cfg); err == nil {
		t.Cleanup(func() { _ = os.WriteFile(cfg, avant, 0o600) })
	} else {
		t.Cleanup(func() { _ = os.Remove(cfg) })
	}
	fichiers[cfg] = string(b)
	for f, contenu := range fichiers {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(contenu), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	verifier := func(quand string, sauf string) {
		t.Helper()
		for f, contenu := range fichiers {
			if f == sauf {
				continue
			}
			if lu, err := os.ReadFile(f); err != nil || string(lu) != contenu {
				t.Fatalf("%s: %s changed or lost (%v)", quand, f, err)
			}
		}
	}
	demarrer := func() http.Handler {
		s := nouveauServeur(lireConfig())
		s.csrf, s.cleAcces = "jeton", "cle"
		mux := http.NewServeMux()
		s.routes(mux)
		return mux
	}
	appel := func(mux http.Handler, methode, url, corps string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(methode, url, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v any
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("%s %s: %d %s", methode, url, w.Code, w.Body.String())
		}
		m, _ := v.(map[string]any)
		return m
	}
	for i := 1; i <= 2; i++ {
		mux := demarrer()
		e := appel(mux, "GET", "/api/etat", "")
		if e["hub_vu"] != false || e["bienvenue"] != false || len(e["projets"].([]any)) < 3 {
			t.Fatalf("start %d, /api/etat of the old config: hub_vu %v bienvenue %v projets %v", i, e["hub_vu"], e["bienvenue"], e["projets"])
		}
		appel(mux, "GET", "/api/hub", "")
		appel(mux, "GET", "/api/projets", "")
		verifier("start "+string(rune('0'+i)), "")
	}
	// "Got it": only hub_vu is added to the config, everything else of it is kept
	appel(demarrer(), "POST", "/api/preferences", `{"hub_vu":true}`)
	verifier("after Got it", cfg)
	c := lireConfig()
	ancienne.HubVu = true
	if gb, _ := json.Marshal(c); string(gb) != func() string { a, _ := json.Marshal(ancienne); return string(a) }() {
		t.Fatalf("config after Got it:\n%s\nwanted:\n%+v", gb, ancienne)
	}
	if e := appel(demarrer(), "GET", "/api/etat", ""); e["hub_vu"] != true {
		t.Fatalf("hub_vu not kept after a restart: %v", e["hub_vu"])
	}
}

// TestHubPagesSansOrphelin: every page belongs to one module only (one tab bar), is a page of app.js,
// and the hub, Dev and Settings declare the pages of the one-level menu.
func TestHubPagesSansOrphelin(t *testing.T) {
	js, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	pagesJS := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^pages\.([a-z]+) =`).FindAllStringSubmatch(string(js), -1) {
		pagesJS["#"+m[1]] = true
	}
	de, par := map[string]string{}, map[string][]string{}
	precedent := -1
	for _, m := range modulesHub() {
		p := pagesModule(m)
		for i, x := range p {
			if x.Module != m.ID() || x.Icone == "" || x.Libelle == "" || (i > 0 && x.Ordre < p[i-1].Ordre) {
				t.Errorf("%s: page %+v (module, icon, label, order)", m.ID(), x)
			}
			if autre, ok := de[x.Hash]; ok {
				t.Errorf("%s is declared by %s and %s", x.Hash, autre, m.ID())
			}
			if !pagesJS[x.Hash] {
				t.Errorf("%s (%s) is no page of app.js", x.Hash, m.ID())
			}
			de[x.Hash] = m.ID()
			par[m.ID()] = append(par[m.ID()], x.Hash)
		}
		if len(p) > 0 {
			if p[0].Ordre < precedent {
				t.Errorf("module %s out of the menu's order", m.ID())
			}
			precedent = p[0].Ordre
		}
	}
	for id, veut := range map[string]string{
		"hub":      "#accueil #global #audit",
		"dev":      "#suivi #equipe #demandes #chat #pourvous #acces #journal #projet",
		"reglages": "#reglages #extensions #sante #distance",
		"coffre":   "#coffre",
	} {
		if g := strings.Join(par[id], " "); g != veut {
			t.Errorf("pages of %s: %q, want %q", id, g, veut)
		}
	}
}

type moduleFactice struct {
	id    string
	ordre int
	el    []ElementHub
}

func (m moduleFactice) ID() string  { return m.id }
func (m moduleFactice) Nom() string { return m.id }
func (m moduleFactice) Pages() []PageModule {
	return []PageModule{{Hash: "#" + m.id, Ordre: m.ordre, Icone: m.id, Libelle: m.id}}
}
func (m moduleFactice) Resume(*Serveur) ResumeModule   { return ResumeModule{Etat: "inactif"} }
func (m moduleFactice) Elements(*Serveur) []ElementHub { return m.el }
func (m moduleFactice) Tutoriel() []EtapeTutoriel      { return nil }

// retirerModuleTest: takes a real module out of the list for a test that registers a fake one with its id
// (the caller restores the list in its Cleanup).
func retirerModuleTest(id string) {
	modulesMu.Lock()
	modules = slices.DeleteFunc(modules, func(m Module) bool { return m.ID() == id })
	modulesMu.Unlock()
}

// TestHubElements: GET /api/hub aggregates the elements of two dummy modules registered from outside
// (no other module's code touched), marks their module, puts what waits for the owner first; a Dev
// project gives its progress and link; no secret value of a project or of the vault is in the answer.
func TestHubElements(t *testing.T) {
	s := serveurCoffreTest(t)
	modulesMu.Lock()
	avant := append([]Module{}, modules...)
	modulesMu.Unlock()
	t.Cleanup(func() { modulesMu.Lock(); modules = avant; modulesMu.Unlock() })
	modulesMu.Lock() // the fake "serveurs" stands for the real Servers module, which has no element yet
	modules = slices.DeleteFunc(modules, func(m Module) bool { return m.ID() == "servers" })
	modulesMu.Unlock()
	enregistrerModule(moduleFactice{id: "serveurs", ordre: 30, el: []ElementHub{{ID: "vps-1", Titre: "vps-1", Etat: "actif", Lien: "#serveurs"}}})
	retirerModuleTest("missions") // the fake stands for the real Missions module
	enregistrerModule(moduleFactice{id: "missions", ordre: 10, el: []ElementHub{
		{ID: "m1", Titre: "Site en ligne", Etat: "actif", Lien: "#missions"},
		{ID: "m2", Titre: "Domaine", Etat: "actif", Lien: "#missions", Attente: &AttenteElement{Raison: "Choose a domain", Page: "#missions"}},
	}})
	temoin := "temoin-hub-" + aleatoire(6)
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, temoin); err != nil {
		t.Fatal(err)
	}
	p := Projet{Nom: "ma-boutique", Dossier: filepath.Join(t.TempDir(), "ma-boutique")}
	for f, c := range map[string]string{
		p.Plan(): "# Plan\n\n- [x] M1 : squelette\n- [ ] M2 : page d'accueil\n- [ ] M3 : paiement\n",
		filepath.Join(p.Atelier(), "secrets.env"): "STRIPE_KEY=" + temoin + "\n",
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p, Projet{Nom: "ancien", Dossier: t.TempDir(), Archive: true})
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	r := httptest.NewRequest("GET", "/api/hub", nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), temoin) {
		t.Fatalf("/api/hub: %d, witness in the answer: %v", w.Code, strings.Contains(w.Body.String(), temoin))
	}
	var v struct {
		Modules  []ModuleVue  `json:"modules"`
		Elements []ElementHub `json:"elements"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	var ordre, el []string
	for _, m := range v.Modules {
		ordre = append(ordre, m.ID)
	}
	for _, e := range v.Elements {
		el = append(el, e.Module+"/"+e.ID)
	}
	if g := strings.Join(ordre, " "); g != "hub missions dev serveurs coffre reglages" {
		t.Errorf("modules: %s", g)
	}
	if g := strings.Join(el, " "); g != "missions/m2 missions/m1 dev/ma-boutique serveurs/vps-1" {
		t.Errorf("elements (waiting first, then the menu's order): %s", g)
	}
	for _, e := range v.Elements {
		if e.ID == "ma-boutique" && (e.Avancement == nil || *e.Avancement != (Avancement{Fait: 1, Total: 3, Texte: "{0} / {1} milestones done", Etape: "Milestone {0}/{1}"}) || e.Etat != "inactif" || e.Lien != "#suivi" || e.Projet != "ma-boutique" || e.Attente != nil) {
			t.Errorf("Dev element: %+v", e)
		}
	}
}

// TestHubAttenteDev: each reason a project needs the owner has the bell's text and page.
func TestHubAttenteDev(t *testing.T) {
	for besoin, veut := range map[string]string{
		"":             "<nil>",
		"pour_vous":    "{0} task(s) on the For you page [2] #pourvous",
		"attente_vous": "Claude is waiting for you [] #pourvous",
		"connexion":    "Connection problem [] #suivi",
		"budget":       "API budget or credit used up, Claude is waiting [] #suivi",
	} {
		g := "<nil>"
		if a := attenteDev(ProjetVue{Besoin: besoin, PourVous: 2}); a != nil {
			g = fmt.Sprintf("%s %v %s", a.Raison, a.Args, a.Page)
		}
		if g != veut {
			t.Errorf("attenteDev(%q) = %q, want %q", besoin, g, veut)
		}
	}
}

// TestTutoriels: the Home declares a short tour (4 to 6 steps, each with a title and a text) in /api/hub,
// a module without one sends none; a tour marked seen through /api/preferences stays seen in /api/etat
// and in the config (shown once), can be reset (reopened as new), and an unknown tour id is refused.
func TestTutoriels(t *testing.T) {
	s := serveurCoffreTest(t)
	modulesMu.Lock()
	avant := append([]Module{}, modules...)
	modulesMu.Unlock()
	t.Cleanup(func() { modulesMu.Lock(); modules = avant; modulesMu.Unlock() })
	enregistrerModule(moduleFactice{id: "serveurs", ordre: 30})
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, route, corps string) (int, map[string]any) {
		r := httptest.NewRequest(methode, route, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	_, hub := appel("GET", "/api/hub", "")
	tours := map[string]int{}
	for _, x := range hub["modules"].([]any) {
		m := x.(map[string]any)
		if e, ok := m["tutoriel"].([]any); ok {
			tours[m["id"].(string)] = len(e)
			for _, y := range e {
				if et := y.(map[string]any); et["titre"] == "" || et["texte"] == "" {
					t.Fatalf("%s: step without title or text: %v", m["id"], et)
				}
			}
		}
	}
	for _, id := range []string{"hub", "dev", "coffre"} {
		if n := tours[id]; n < 4 || n > 6 {
			t.Fatalf("%s tour: %d steps, want 4 to 6 (%v)", id, n, tours)
		}
	}
	// the access tour says what is true: infra values never reach the AI, granted code values do, the
	// badge, removal; never a blanket "the AI never reads any key"
	var acces strings.Builder
	for _, e := range (moduleCoffre{}).Tutoriel() {
		acces.WriteString(e.Titre + " " + e.Texte + " " + e.Page + " ")
	}
	for _, v := range []string{"encrypted", "never shown again", "password", "the AI never receives their value", "only if you grant it", "environment", "as a variable", "badge", "#acces", "#suivi", "Remove"} {
		if !strings.Contains(acces.String(), v) {
			t.Fatalf("access tour without %q: %s", v, acces.String())
		}
	}
	if a := strings.ToLower(acces.String()); strings.Contains(a, "never read") || strings.Contains(a, "never see") || strings.Contains(a, "no key") {
		t.Fatalf("access tour with a blanket promise: %s", acces.String())
	}
	if _, ok := tours["serveurs"]; ok {
		t.Fatal("a module without a tour sent one")
	}
	if _, e := appel("GET", "/api/etat", ""); len(e["tours"].(map[string]any)) != 0 {
		t.Fatalf("new config: tours %v", e["tours"])
	}
	if c, _ := appel("POST", "/api/preferences", `{"tours":{"hub":true}}`); c != http.StatusOK {
		t.Fatalf("mark the Home tour seen: %d", c)
	}
	if _, e := appel("GET", "/api/etat", ""); e["tours"].(map[string]any)["hub"] != true {
		t.Fatalf("Home tour not seen after the POST: %v", e["tours"])
	}
	if c, _ := appel("POST", "/api/preferences", `{"tours":{"serveurs":true}}`); c != http.StatusBadRequest {
		t.Fatalf("tour of a module without one: %d, want 400", c)
	}
	if c, _ := appel("POST", "/api/preferences", `{"tours":{"../x":true}}`); c != http.StatusBadRequest {
		t.Fatalf("unknown tour id: %d, want 400", c)
	}
	// the Options button resets every tour at once (false: shown again, never only the reminder)
	if c, _ := appel("POST", "/api/preferences", `{"bienvenue_vue":false,"tours":{"hub":false,"dev":false}}`); c != http.StatusOK {
		t.Fatalf("reset every tour: %d", c)
	}
	if _, e := appel("GET", "/api/etat", ""); e["tours"].(map[string]any)["dev"] != false || e["bienvenue"] != true {
		t.Fatalf("tours not reset: %v", e)
	}
	appel("POST", "/api/preferences", `{"tours":{"hub":true}}`)
	if cfg := lireConfig(); !cfg.Tours["hub"] {
		t.Fatalf("seen state not written in the config: %v", cfg.Tours)
	}
	appel("POST", "/api/preferences", `{"tours":{"hub":false}}`)
	if _, e := appel("GET", "/api/etat", ""); e["tours"].(map[string]any)["hub"] != false {
		t.Fatalf("Home tour not reset: %v", e["tours"])
	}
}
