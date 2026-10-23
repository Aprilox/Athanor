//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ecrireTest(t *testing.T, chemin, contenu string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chemin, []byte(contenu), mode); err != nil {
		t.Fatal(err)
	}
}

func TestOuvertures(t *testing.T) {
	kit, projet := t.TempDir(), t.TempDir()
	ecoute, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ecoute.Close()
	port := strings.TrimPrefix(ecoute.Addr().String(), "127.0.0.1:")
	ecrireTest(t, filepath.Join(kit, "extensions", "bibliotheque.json"), `{"skills":{},"mcp":{
	  "blender":{"nom":"Blender","catalogue":"blender","config":{"type":"windows","command":"uvx"}},
	  "perso":{"nom":"Perso","config":{"command":"x"},"reglages":{"PORT":"1","DOSSIER":"C:\\Prog"},
	    "ouvrir":{"programme":"${DOSSIER}\\outil.exe","args":["--port","${PORT}"],"port":"${PORT}"}},
	  "manque":{"nom":"Manque","config":{"command":"x"},"ouvrir":{"programme":"blender","args":["${RIEN}"]}},
	  "aucun":{"nom":"Aucun","catalogue":"blender","config":{"command":"x"},"ouvrir":{"programme":""}},
	  "mauvais":{"nom":"Mauvais","config":{"command":"x"},"ouvrir":{"programme":"rm","args":["-rf","/"]}},
	  "lent":{"nom":"Lent","config":{"command":"x"},"ouvrir":{"programme":"unreal-editor","port":"1","delai":4}}}}`, 0o644)
	ecrireTest(t, filepath.Join(kit, "extensions", "secrets.json"), `{"perso":{"PORT":"`+port+`"}}`, 0o600)
	f := filepath.Join(kit, "prive", "ouvrir.json")
	notes := construireOuvertures(kit, []string{"blender", "perso", "manque", "aucun", "mauvais", "lent", "absent"}, f, projet)
	if strings.Join(notes, "|") != "manque: opening skipped (missing setting RIEN)|mauvais: opening not understood" {
		t.Fatalf("notes: %q", notes)
	}
	var l []ouvertureCycle
	b, _ := os.ReadFile(f)
	if err := json.Unmarshal(b, &l); err != nil || len(l) != 3 {
		t.Fatalf("%s %v", b, err)
	}
	if l[0].ID != "blender" || l[0].Programme != "blender" || strings.Join(l[0].Args, " ") != "--open {FICHIER}" || !l[0].SansFichier || l[0].Delai != 60 {
		t.Fatalf("blender (the catalog's): %+v", l[0])
	}
	if l[1].Programme != `C:\Prog\outil.exe` || l[1].Port != port || strings.Join(l[1].Args, " ") != "--port "+port || l[1].Processus != "outil.exe" {
		t.Fatalf("perso: %+v", l[1])
	}

	// opening: the gateways are bash scripts of system/bin; the project's .blend given to Blender
	trace := filepath.Join(kit, "trace")
	for _, g := range []string{"blender", "unreal-editor"} {
		ecrireTest(t, filepath.Join(kit, "system", "bin", g), "#!/usr/bin/env bash\necho \"$(basename \"$0\") $* [$PROJECT]\" >> "+trace+"\necho started\n", 0o755)
	}
	ecrireTest(t, filepath.Join(projet, ".athanor", "x.blend"), "", 0o644)
	ecrireTest(t, filepath.Join(projet, "scenes", "b.blend"), "", 0o644)
	ecrireTest(t, filepath.Join(projet, "scenes", "a.BLEND"), "", 0o644)
	attentes := 0
	lignes := ouvrirServeurs(kit, projet, f, func(time.Duration) { attentes++ })
	tr, _ := os.ReadFile(trace)
	attendu := "blender --open " + filepath.Join(projet, "scenes", "a.BLEND") + " [" + projet + "]\nunreal-editor  [" + projet + "]\n"
	if string(tr) != attendu {
		t.Fatalf("calls:\n%s\nexpected:\n%s", tr, attendu)
	}
	if len(lignes) != 2 || lignes[0] != "blender: blender on a.BLEND opened for its MCP server." ||
		lignes[1] != "lent: unreal-editor opened, but nothing answers on port 1 after 4 s (is its MCP add-on started, on that port?)." || attentes != 2 {
		t.Fatalf("lines (%d waits): %q", attentes, lignes)
	}

	// no file and not sans_fichier: said, nothing opened
	ecrireTest(t, f, `[{"id":"g","programme":"blender","args":["--open","{FICHIER}"],"fichiers":["*.glb"]}]`, 0o644)
	os.Remove(trace)
	lignes = ouvrirServeurs(kit, projet, f, func(time.Duration) {})
	if _, err := os.Stat(trace); err == nil || len(lignes) != 1 || lignes[0] != "g: no *.glb in the project, blender not opened." {
		t.Fatalf("%q", lignes)
	}
	// none left: the file removed
	construireOuvertures(kit, []string{"aucun"}, f, projet)
	if _, err := os.Stat(f); err == nil {
		t.Fatal("ouvrir.json kept")
	}
}

func TestDocMCP(t *testing.T) {
	avant := MCPInstalle{Config: ConfigMCP{Type: "stdio", Command: "npx"}}
	cas := []struct{ texte, erreur string }{
		{`{"config":{"command":"npx"},"autre":1}`, "Invalid JSON: "},
		{`{"config":{"command":"C:\\x\\y.exe"}}`, `Set "type": "windows"`},
		{`{"config":{"command":"npx"},"reglages":{"API_TOKEN":"x"}}`, "A key is not a setting"},
		{`{"config":{"url":"https://a.b/mcp","type":"stdio"}}`, `"http" or "sse"`},
		{`{"config":{"command":"npx"},"ouvrir":{"programme":"bash"}}`, "is a gateway"},
		{`{"config":{"command":"npx"},"ouvrir":{"programme":"blender","fichiers":["../x"]}}`, "Invalid file pattern"},
	}
	for _, c := range cas {
		if _, _, err := lireDocMCP(c.texte, avant); err == nil || !strings.Contains(err.Error(), c.erreur) {
			t.Errorf("%s: %v", c.texte, err)
		}
	}
	d, secrets, err := lireDocMCP(`{"config":{"command":"npx","env":{"API_KEY":"sk-123","HOST":"${HOST}"}},"reglages":{"HOST":"localhost"},"ouvrir":{"programme":"C:\\P\\b.exe","port":"9876"}}`, avant)
	if err != nil || secrets["API_KEY"] != "sk-123" || d.Config.Env["API_KEY"] != "${API_KEY}" || d.Config.Type != "stdio" || d.Ouvrir.Port != "9876" {
		t.Fatalf("%+v %v %v", d, secrets, err)
	}
	x := MCPInstalle{Config: d.Config, Reglages: d.Reglages, Ouvrir: d.Ouvrir}
	if got := strings.Join(champsMCP(x, Catalogue{}), ","); got != "API_KEY,HOST" {
		t.Fatalf("fields: %s", got)
	}
	// a Windows server stays one, written with its type
	if _, _, err := lireDocMCP(`{"config":{"type":"windows","command":"C:\\x\\y.exe"}}`, avant); err != nil {
		t.Fatal(err)
	}
}

func TestModifierMCPApp(t *testing.T) {
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
	if c, v := appel("POST", "/api/extensions/catalogue", map[string]string{"type": "mcp", "id": "blender"}); c != 200 {
		t.Fatalf("install: %d %v", c, v)
	}
	// the catalog's opening shown, not copied into the library
	c, v := appel("GET", "/api/extensions/voir?type=mcp&id=blender", nil)
	texte, _ := v["texte"].(string)
	if c != 200 || v["modifiable"] != true || !strings.Contains(texte, `"programme": "blender"`) || lireBiblio(s.kitCourant()).MCP["blender"].Ouvrir != nil {
		t.Fatalf("view: %d %v", c, v)
	}
	// unchanged: still the catalog's
	if c, v := appel("POST", "/api/extensions/mcp/modifier", map[string]string{"id": "blender", "texte": texte}); c != 200 || lireBiblio(s.kitCourant()).MCP["blender"].Ouvrir != nil {
		t.Fatalf("save as is: %d %v", c, v)
	}
	// a port: a setting, used by the opening, shown below the server
	neuf := strings.Replace(texte, `"sans_fichier": true`, `"sans_fichier": true, "port": "${PORT}", "delai": 30`, 1)
	neuf = strings.Replace(neuf, `"config": {`, `"reglages": {"PORT": "9876"}, "config": {`, 1)
	if c, v := appel("POST", "/api/extensions/mcp/modifier", map[string]string{"id": "blender", "texte": neuf}); c != 200 {
		t.Fatalf("save: %d %v", c, v)
	}
	x := lireBiblio(s.kitCourant()).MCP["blender"]
	if x.Ouvrir == nil || x.Ouvrir.Port != "${PORT}" || x.Reglages["PORT"] != "9876" || x.Config.Type != "windows" || x.Catalogue != "blender" {
		t.Fatalf("library: %+v", x)
	}
	_, v = appel("GET", "/api/extensions?p=a", nil)
	b, _ := json.Marshal(v["mcp"])
	if !strings.Contains(string(b), `{"defaut":"9876","defini":true,"nom":"PORT","optionnel":false,"reglage":true,"valeur":"9876"}`) {
		t.Fatalf("fields: %s", b)
	}
	// the opening removed: none, the catalog's not used either
	if c, v := appel("POST", "/api/extensions/mcp/modifier", map[string]string{"id": "blender", "texte": `{"config":{"type":"windows","command":"uvx"}}`}); c != 200 {
		t.Fatalf("remove: %d %v", c, v)
	}
	_, v = appel("GET", "/api/extensions/voir?type=mcp&id=blender", nil)
	if strings.Contains(v["texte"].(string), "ouvrir") {
		t.Fatalf("still an opening: %v", v["texte"])
	}
	if notes := construireOuvertures(kit, []string{"blender"}, filepath.Join(kit, "o.json"), p.Dossier); len(notes) != 0 {
		t.Fatal(notes)
	}
	if _, err := os.Stat(filepath.Join(kit, "o.json")); err == nil {
		t.Fatal("opened although removed")
	}
	// refused: a key pasted as a setting, an unknown field
	if c, _ := appel("POST", "/api/extensions/mcp/modifier", map[string]string{"id": "blender", "texte": `{"config":{"command":"x"},"reglages":{"API_KEY":"x"}}`}); c != 400 {
		t.Fatalf("key as a setting: %d", c)
	}
	if c, _ := appel("POST", "/api/extensions/mcp/modifier", map[string]string{"id": "inconnu", "texte": `{}`}); c != 404 {
		t.Fatalf("unknown: %d", c)
	}
}
