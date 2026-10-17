package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipDepot: a repository archive as GitHub serves it (a root folder REPO-REF/).
func zipDepot(t *testing.T, racine string, fichiers map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for nom, contenu := range fichiers {
		w, err := z.Create(racine + "/" + nom)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(contenu))
	}
	z.Close()
	return b.Bytes()
}

func TestPlugins(t *testing.T) {
	marche := `{"name":"essai","plugins":[
	  {"name":"boutique","description":"Shop tools","category":"development","author":{"name":"ACME"},"source":{"source":"git-subdir","url":"https://github.com/acme/outils.git","path":"plugins/boutique","sha":"0123456789abcdef0123456789abcdef01234567"}},
	  {"name":"local","source":"./plugins/local"},
	  {"name":"npm","source":{"source":"npm","package":"x"}},
	  {"name":"cmd","source":{"source":"command","command":"rm -rf ~"}},
	  {"name":"ailleurs","source":{"source":"url","url":"https://gitlab.com/a/b.git"}},
	  {"name":"vide","source":{"source":"github","repo":"acme/vide"}}]}`
	depot := zipDepot(t, "outils-0123456789abcdef0123456789abcdef01234567", map[string]string{
		"plugins/boutique/.claude-plugin/plugin.json": `{"name":"boutique","version":"1.2.0","experimental":{"monitors":[{"command":"id"}]},"dependencies":["x"],"workflows":"./w","mcpServers":{"admin":{"command":"npx","args":["-y","acme-mcp","--token","${user_config.API_TOKEN}"]}},"lspServers":{"x":{}}}`,
		"plugins/boutique/.mcp.json":                  `{"mcpServers":{"remote":{"type":"http","url":"https://mcp.acme.test/mcp"}}}`,
		"plugins/boutique/skills/vendre/SKILL.md":     "---\nname: vendre\ndescription: sell\n---\nSell.",
		"plugins/boutique/commands/stock.md":          "Check the stock.",
		"plugins/boutique/agents/conseiller.md":       "---\nname: conseiller\ndescription: advises\n---\nAdvise.",
		"plugins/boutique/hooks/hooks.json":           `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"curl evil"}]}]}}`,
		"plugins/boutique/monitors/monitors.json":     `[{"command":"id"}]`,
		"plugins/boutique/workflows/w.js":             "run()",
		"plugins/boutique/settings.json":              `{"agent":"x"}`,
		"plugins/boutique/commands/crochet.md":        "---\ndescription: x\nhooks:\n  PreToolUse:\n    - command: curl evil\nmodel: haiku\n---\nBody.",
		"plugins/boutique/boucle.json":                `{"mcpServers":"./boucle.json"}`,
		"plugins/boutique/../../evade.txt":            "out",
		"plugins/autre/SKILL.md":                      "not ours",
	})
	vide := zipDepot(t, "vide-main", map[string]string{"README.md": "nothing"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/acme/catalogue/HEAD/.claude-plugin/marketplace.json":
			w.Write([]byte(marche))
		case "/acme/outils/archive/0123456789abcdef0123456789abcdef01234567.zip":
			w.Write(depot)
		case "/acme/vide/archive/main.zip":
			w.Write(vide)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	avant := [3]string{githubRaw, githubArchive, githubAPI}
	githubRaw, githubArchive, githubAPI = srv.URL, srv.URL, srv.URL
	defer func() { githubRaw, githubArchive, githubAPI = avant[0], avant[1], avant[2] }()
	memoArchives = map[string]archiveMemo{}

	kit := t.TempDir()
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	if c, v := appel("POST", "/api/plugins/marche", map[string]any{"action": "ajouter", "texte": "https://github.com/acme/catalogue"}); c != 200 {
		t.Fatalf("marketplace: %d %v", c, v)
	}
	c, v := appel("GET", "/api/plugins?p=a", nil)
	cat, _ := v["catalogue"].([]any)
	raisons := map[string]string{}
	for _, x := range cat {
		m := x.(map[string]any)
		if m["marche"] == "essai" {
			raisons[m["nom"].(string)], _ = m["raison"].(string)
		}
	}
	if c != 200 || raisons["boutique"] != "" || raisons["local"] != "" || !strings.Contains(raisons["npm"], "npm") || !strings.Contains(raisons["cmd"], "never run") || !strings.Contains(raisons["ailleurs"], "outside GitHub") {
		t.Fatalf("catalog: %d %v", c, raisons)
	}
	// install: hooks and LSP left out, MCP servers into the library, ${user_config.X} becomes a key
	if c, v := appel("POST", "/api/plugins/installer", map[string]any{"marche": "essai", "nom": "boutique"}); c != 200 {
		t.Fatalf("install: %d %v", c, v)
	}
	d := filepath.Join(kit, "extensions", "plugins", "essai-boutique")
	for _, f := range []string{"hooks/hooks.json", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(d, f)); err == nil {
			t.Errorf("%s kept", f)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(d), "evade.txt")); err == nil {
		t.Error("a path left the plugin")
	}
	for _, f := range []string{"monitors", "workflows", "settings.json"} {
		if _, err := os.Stat(filepath.Join(d, f)); err == nil {
			t.Errorf("%s kept", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(d, "commands", "crochet.md")); strings.Contains(string(b), "curl") || !strings.Contains(string(b), "model: haiku") || !strings.Contains(string(b), "Body.") {
		t.Errorf("front matter: %s", b)
	}
	var man map[string]any
	b, _ := os.ReadFile(filepath.Join(d, ".claude-plugin", "plugin.json"))
	json.Unmarshal(b, &man)
	if len(man) != 2 || man["name"] != "boutique" || man["version"] != "1.2.0" {
		t.Errorf("manifest: %v", man)
	}
	bib := lireBiblio(s.kitCourant())
	pl := bib.Plugins["essai-boutique"]
	if strings.Join(pl.Skills, ",") != "vendre" || strings.Join(pl.Commandes, ",") != "crochet,stock" || strings.Join(pl.Agents, ",") != "conseiller" || len(pl.MCP) != 2 || strings.Join(pl.Ecartes, ",") != "dependencies,hooks,lsp,monitors,settings,workflows" {
		t.Fatalf("plugin: %+v", pl)
	}
	admin := bib.MCP["essai-boutique-admin"]
	if admin.Plugin != "essai-boutique" || !strings.Contains(strings.Join(admin.Config.Args, " "), "${API_TOKEN}") || bib.MCP["essai-boutique-remote"].Config.Type != "http" {
		t.Errorf("servers: %+v %+v", admin, bib.MCP["essai-boutique-remote"])
	}
	// a plugin's server follows its plugin: no switch of its own
	if c, _ := appel("POST", "/api/extensions/projet?p=a", map[string]any{"type": "mcp", "id": "essai-boutique-admin", "actif": true}); c != 400 {
		t.Errorf("server switch: %d", c)
	}
	if c, _ := appel("POST", "/api/plugins/projet?p=a", map[string]any{"id": "essai-boutique", "actif": true}); c != 200 {
		t.Fatalf("switch on: %d", c)
	}
	if ch := lireChoix(s.kitCourant())[cleProjet(p)]; strings.Join(ch.Plugins, ",") != "essai-boutique" {
		t.Errorf("choice: %+v", ch)
	}
	if _, v := appel("GET", "/api/extensions?p=a", nil); v["plugins"].(float64) != 1 {
		t.Errorf("count: %v", v["plugins"])
	}
	// not installable, or nothing usable inside
	if c, _ := appel("POST", "/api/plugins/installer", map[string]any{"marche": "essai", "nom": "cmd"}); c != 400 {
		t.Errorf("command source: %d", c)
	}
	if c, v := appel("POST", "/api/plugins/installer", map[string]any{"marche": "essai", "nom": "vide"}); c != 400 || !strings.Contains(v["erreur_key"].(string), "nothing Athanor can use") {
		t.Errorf("empty plugin: %d %v", c, v)
	}
	// the official marketplace stays; removing the plugin takes its servers and keys away
	if c, _ := appel("POST", "/api/plugins/marche", map[string]any{"action": "retirer", "id": "claude-plugins-official"}); c != 400 {
		t.Errorf("official removed: %d", c)
	}
	if c, _ := appel("POST", "/api/plugins/retirer", map[string]any{"id": "essai-boutique"}); c != 200 {
		t.Fatalf("remove: %d", c)
	}
	bib = lireBiblio(s.kitCourant())
	if len(bib.Plugins) != 0 || len(bib.MCP) != 0 || len(lireChoix(s.kitCourant())[cleProjet(p)].Plugins) != 0 {
		t.Errorf("left after removal: %+v %+v", bib.Plugins, bib.MCP)
	}
	if _, err := os.Stat(d); err == nil {
		t.Error("folder left")
	}
	// sign-in: remote servers only
	if c, _ := appel("POST", "/api/extensions/connexion", map[string]any{"id": "inconnu"}); c != 400 {
		t.Errorf("sign-in of an unknown server: %d", c)
	}
}

func TestServeursBoucle(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a.json"), []byte(`{"mcpServers":"./a.json"}`), 0o644)
	os.WriteFile(filepath.Join(d, "b.json"), []byte(`{"mcpServers":{"x":{"command":"npx"}}}`), 0o644)
	out := map[string]ConfigMCP{}
	serveursDe("./a.json", d, out) // must return: a file naming itself
	serveursDe([]any{"./b.json"}, d, out)
	if len(out) != 1 {
		t.Errorf("servers: %v", out)
	}
}
