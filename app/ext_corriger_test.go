package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// claudeEssai: Claude's answers, one per question; the questions asked are kept.
func claudeEssai(t *testing.T, reponses ...string) *[]string {
	t.Helper()
	var questions []string
	ancien := demanderClaude
	demanderClaude = func(consigne string, _ time.Duration) (string, error) {
		questions = append(questions, consigne)
		if len(questions) > len(reponses) {
			t.Fatalf("Claude asked %d times", len(questions))
		}
		return reponses[len(questions)-1], nil
	}
	t.Cleanup(func() { demanderClaude = ancien })
	return &questions
}

func kitCorrectionEssai(t *testing.T) (kit, projet, d string) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/python3"); err != nil || runtime.GOOS == "windows" {
		t.Skip("python3 and sh, as on Linux and macOS")
	}
	d, _ = filepath.EvalSymlinks(t.TempDir())
	kit, projet = kitExtensions(t, d)
	ecrireFichier(t, filepath.Join(d, "srv.py"), `import json, sys
for l in sys.stdin:
    m = json.loads(l)
    if m.get("id") == 1:
        print(json.dumps({"jsonrpc": "2.0", "id": 1, "result": {"serverInfo": {"name": "essai", "version": "2"}}}), flush=True)
    elif m.get("id") == 2:
        print(json.dumps({"jsonrpc": "2.0", "id": 2, "result": {"tools": [{"name": "lire"}]}}), flush=True)
`, 0o644)
	ecrireFichier(t, filepath.Join(d, "mort.sh"), "#!/bin/sh\necho 'Error: unknown option --stdio' >&2\nexit 3\n", 0o755)
	b, _ := json.Marshal(map[string]any{"mcp": map[string]any{
		"casse": map[string]any{"nom": "Casse", "source": "github.com/x/casse", "catalogue": "playwright",
			"config":   map[string]any{"type": "stdio", "command": "programme-introuvable-xyz", "args": []any{"--stdio"}, "env": map[string]any{"TOKEN": "${CASSE_KEY}"}},
			"reglages": map[string]any{"PORT": "80"}},
		"cle": map[string]any{"nom": "Cle", "config": map[string]any{"url": "https://x.org/${K}"}},
	}})
	ecrireFichier(t, filepath.Join(kit, "extensions/bibliotheque.json"), string(b), 0o644)
	ecrireFichier(t, filepath.Join(kit, "extensions/secrets.json"), `{"casse": {"CASSE_KEY": "valeur-secrete-123", "PORT": "8080"}}`, 0o600)
	return kit, projet, d
}

// Claude's proposals tried one after the other: an answer not understood, one that fails, one that works.
func TestCorrigerServeurMCP(t *testing.T) {
	kit, projet, d := kitCorrectionEssai(t)
	bon := `{"config": {"type": "stdio", "command": "python3", "args": ["-I", "` + filepath.Join(d, "srv.py") + `"], "env": {"TOKEN": "${CASSE_KEY}"}}, "reglages": {"PORT": "9000"}, "explication": "Le paquet s'appelle autrement.", "athanor": ""}`
	questions := claudeEssai(t,
		"Je pense que c'est la commande.",
		`{"config": {"command": "`+filepath.Join(d, "mort.sh")+`", "env": {"TOKEN": "${CASSE_KEY}"}}, "explication": "essai 2"}`,
		"```json\n"+bon+"\n```")
	var etapes []string
	res := corrigerServeurMCP(kit, projet, "casse", func(q string, n int) { etapes = append(etapes, q+string(rune('0'+n))) })
	if !res.OK || len(res.Essais) != 2 || res.Config == nil || res.Config.Command != "python3" || res.Reglages["PORT"] != "9000" || len(res.Outils) != 1 {
		t.Fatalf("%+v", res)
	}
	if strings.Join(etapes, ",") != "test0,claude1,claude2,test2,claude3,test3" {
		t.Fatalf("steps: %v", etapes)
	}
	if !strings.Contains(res.Initiale+strings.Join(res.Details, " "), "programme-introuvable-xyz") || !strings.Contains(res.Essais[1].Erreur, "no answer") || !strings.Contains(strings.Join(res.Essais[1].Details, " "), "unknown option --stdio") {
		t.Fatalf("errors: %+v", res)
	}
	q := (*questions)[2]
	for _, x := range []string{"programme-introuvable-xyz", "CASSE_KEY (saved)", "PORT = 8080 (the default is 80)", "github.com/x/casse", "result: no answer to initialize", "unknown option --stdio", "Programs on the server's PATH: found:"} {
		if !strings.Contains(q, x) {
			t.Errorf("the question lacks %q", x)
		}
	}
	if strings.Contains(strings.Join(*questions, ""), "valeur-secrete-123") {
		t.Fatal("a key's value given to Claude")
	}
	// kept in the library, with what it replaced; Undo puts it back
	k := Kit{Racine: kit}
	if err := appliquerCorrectionMCP(k, "casse", res); err != nil {
		t.Fatal(err)
	}
	e := lireBiblio(k).MCP["casse"]
	if e.Config.Command != "python3" || e.Correction == nil || e.Correction.ConfigAvant.Command != "programme-introuvable-xyz" || e.Reglages["PORT"] != "9000" || e.Correction.ReglagesRetablis["PORT"] != "8080" {
		t.Fatalf("library: %+v %+v", e, e.Correction)
	}
	if v, ok := lireSecrets(k)["casse"]["PORT"]; ok || lireSecrets(k)["casse"]["CASSE_KEY"] != "valeur-secrete-123" {
		t.Fatalf("settings: %v", v)
	}
	if r := testerServeur(kit, projet, "casse", 20*time.Second); r["ok"] != true {
		t.Fatalf("the fixed server: %v", r)
	}
	if err := annulerCorrectionMCP(k, "casse"); err != nil {
		t.Fatal(err)
	}
	e = lireBiblio(k).MCP["casse"]
	if e.Config.Command != "programme-introuvable-xyz" || e.Correction != nil || e.Reglages["PORT"] != "80" || lireSecrets(k)["casse"]["PORT"] != "8080" {
		t.Fatalf("undone: %+v", e)
	}
}

// A program missing here, a key to give, a key Claude made up: never worked around.
func TestCorrigerServeurMCPLimites(t *testing.T) {
	kit, projet, _ := kitCorrectionEssai(t)
	claudeEssai(t, `{"config": null, "explication": "uv manque.", "manque": "uv (uvx)"}`)
	if res := corrigerServeurMCP(kit, projet, "casse", func(string, int) {}); res.OK || res.Manque != "uv (uvx)" || res.Explication != "uv manque." {
		t.Fatalf("missing: %+v", res)
	}
	claudeEssai(t) // never asked
	if res := corrigerServeurMCP(kit, projet, "cle", func(string, int) {}); res.OK || !strings.HasPrefix(res.Erreur, "missing key") {
		t.Fatalf("a key to give: %+v", res)
	}
	invente := `{"config": {"command": "npx", "args": ["-y", "x"], "env": {"T": "${AUTRE_CLE}"}}, "explication": "x"}`
	windows := `{"config": {"type": "windows", "command": "x.exe"}, "explication": "x"}`
	claudeEssai(t, invente, windows, `{"config": null, "explication": "La cage bloque.", "athanor": "The cage should let it write ~/.cache."}`)
	res := corrigerServeurMCP(kit, projet, "casse", func(string, int) {})
	if res.OK || len(res.Essais) != 2 || !strings.Contains(res.Essais[0].Erreur, "AUTRE_CLE") || !strings.Contains(res.Essais[1].Erreur, "not allowed") || res.Athanor != "The cage should let it write ~/.cache." {
		t.Fatalf("%+v", res)
	}
	// the report, for everyone
	if err := ecrireRapportCorrection(kit, "casse", res); err != nil {
		t.Fatal(err)
	}
	l := lireRapports(kit)
	if len(l) != 1 || l[0].Athanor == "" || l[0].Apres != nil || l[0].Catalogue != "playwright" || l[0].Avant == nil || l[0].Version != versionAthanor {
		t.Fatalf("reports: %+v", l)
	}
}

// athanor extensions corriger … --appliquer: its steps, then the result, the library fixed, the report.
func TestLancerCorrigerExt(t *testing.T) {
	kit, projet, d := kitCorrectionEssai(t)
	claudeEssai(t, `{"config": {"command": "python3", "args": ["-I", "`+filepath.Join(d, "srv.py")+`"]}, "explication": "ok"}`)
	var sortie strings.Builder
	if c := lancerCorrigerExt(kit, projet, []string{"casse", "--appliquer"}, &sortie); c != 0 {
		t.Fatal(c)
	}
	l := strings.Split(strings.TrimSpace(sortie.String()), "\n")
	var res ResultatCorrection
	if len(l) != 4 || l[0] != "ETAPE test 0" || l[1] != "ETAPE claude 1" || l[2] != "ETAPE test 1" || json.Unmarshal([]byte(l[3]), &res) != nil || !res.OK {
		t.Fatalf("%q", l)
	}
	if lireBiblio(Kit{Racine: kit}).MCP["casse"].Config.Command != "python3" || len(lireRapports(kit)) != 1 || lireRapports(kit)[0].Apres == nil {
		t.Fatal("not applied")
	}
	// fixed already: nothing asked
	claudeEssai(t)
	sortie.Reset()
	_ = lancerCorrigerExt(kit, projet, []string{"casse"}, &sortie)
	if !strings.Contains(sortie.String(), `"deja":true`) {
		t.Fatal(sortie.String())
	}
}

// A fix of the verified catalog written into Athanor's code: only that entry's text changes, the catalog
// still reads, the system's variant is where the fix goes.
func TestCorrigerEntreeCatalogue(t *testing.T) {
	b, err := os.ReadFile("catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	texte := string(b)
	c := &ConfigMCP{Type: "stdio", Command: "npx", Args: []string{"-y", "@playwright/mcp@latest", "--headless"}}
	n, err := corrigerEntreeCatalogue(texte, RapportCorrection{Catalogue: "playwright", Systeme: "linux", Apres: c})
	if err != nil {
		t.Fatal(err)
	}
	var cat Catalogue
	_ = json.Unmarshal([]byte(n), &cat)
	appliquerSystemes(&cat, "linux")
	trouve := false
	for _, m := range cat.MCP {
		if m.ID == "playwright" {
			trouve = strings.Join(m.Config.Args, " ") == "-y @playwright/mcp@latest --headless"
		}
	}
	if !trouve {
		t.Fatal("the linux variant not used")
	}
	compte := func(a, b []string) int { // the lines of a absent from b
		vu := map[string]int{}
		for _, l := range b {
			vu[l]++
		}
		n := 0
		for _, l := range a {
			if vu[l] > 0 {
				vu[l]--
			} else {
				n++
			}
		}
		return n
	}
	av, ap := strings.Split(texte, "\n"), strings.Split(n, "\n")
	if retirees, ajoutees := compte(av, ap), compte(ap, av); retirees != 1 || ajoutees != 2 ||
		!strings.Contains(n, ",\n     "+`"systemes": {"linux": {"config": {"type": "stdio", "command": "npx", "args": ["-y", "@playwright/mcp@latest", "--headless"]}}}}`) {
		t.Fatalf("lines: -%d +%d\n%s", retirees, ajoutees, n)
	}
	// Windows: the entry's own configuration, with a setting's default
	n, err = corrigerEntreeCatalogue(texte, RapportCorrection{Catalogue: "blender", Systeme: "windows", Apres: &ConfigMCP{Type: "stdio", Command: "uvx", Args: []string{"blender-mcp"}}, Reglages: map[string]string{"BLENDER_PORT": "9877"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n, `"config": {"type": "stdio", "command": "uvx", "args": ["blender-mcp"]}`) || !strings.Contains(n, `"defaut": "9877"`) {
		t.Fatal("windows fix not written")
	}
	if _, err := corrigerEntreeCatalogue(texte, RapportCorrection{Catalogue: "absent", Systeme: "linux", Apres: c}); err == nil {
		t.Fatal("an unknown entry")
	}
}

// appliquer-corrections: the catalog's fixes into a copy of the code, the others listed.
func TestAppliquerAuCode(t *testing.T) {
	kit, code := t.TempDir(), t.TempDir()
	b, _ := os.ReadFile("catalogue.json")
	ecrireFichier(t, filepath.Join(code, "app", "catalogue.json"), string(b), 0o644)
	_ = ecrireRapport(kit, RapportCorrection{Genre: "mcp", ID: "pw", Catalogue: "playwright", Systeme: "macos", Apres: &ConfigMCP{Command: "npx", Args: []string{"-y", "@playwright/mcp"}}})
	time.Sleep(1100 * time.Millisecond)
	_ = ecrireRapport(kit, RapportCorrection{Genre: "mcp", ID: "a-moi", Systeme: "macos", Apres: &ConfigMCP{Command: "x"}})
	a, v, err := appliquerAuCode(kit, code)
	if err != nil || len(a) != 1 || a[0] != "playwright (macos)" || len(v) != 1 {
		t.Fatalf("%v %v %v", a, v, err)
	}
	n, _ := os.ReadFile(filepath.Join(code, "app", "catalogue.json"))
	if !strings.Contains(string(n), `"macos": {"config": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`) {
		t.Fatal("not written")
	}
	if l := lireRapports(kit); len(l) != 1 || l[0].ID != "a-moi" {
		t.Fatalf("waiting: %+v", l)
	}
	if _, _, err := appliquerAuCode(kit, t.TempDir()); err == nil {
		t.Fatal("not Athanor's code")
	}
}
