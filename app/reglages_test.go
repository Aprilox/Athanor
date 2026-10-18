package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestReglagesMCP(t *testing.T) {
	kit := t.TempDir()
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	os.WriteFile(Kit{kit}.Env(), []byte("MODEL=\nUNREAL_MCP=1\nUNREAL_MCP_URL=http://127.0.0.1:9000/mcp\n"), 0o600)
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
	// the old UNREAL_MCP setting became the "unreal" server, on for the project, with its address
	bib := lireBiblio(s.kitCourant())
	if x, ok := bib.MCP["unreal"]; !ok || x.Catalogue != "unreal" || x.Reglages["UNREAL_MCP_URL"] != "http://127.0.0.1:8000/mcp" {
		t.Fatalf("migration: %+v", bib.MCP)
	}
	if lireSecrets(s.kitCourant())["unreal"]["UNREAL_MCP_URL"] != "http://127.0.0.1:9000/mcp" || !contient(lireChoix(s.kitCourant())[cleProjet(p)].MCP, "unreal") {
		t.Errorf("migration values: %v %v", lireSecrets(s.kitCourant()), lireChoix(s.kitCourant()))
	}
	for _, l := range LireEnv(Kit{kit}.Env()) {
		if l.Cle == "UNREAL_MCP" && l.Valeur != "0" {
			t.Errorf("UNREAL_MCP still %q", l.Valeur)
		}
	}
	_, v := appel("GET", "/api/env", nil)
	if strings.Contains(string(mustJSON(v)), "UNREAL") {
		t.Error("Unreal keys still on the Settings page")
	}
	// a pasted configuration: address, port and path become settings; the key stays a key
	conf := `{"mcpServers":{"studio":{"command":"npx","args":["-y","studio-mcp","--port","44755","--host=127.0.0.1"],"env":{"STUDIO_API_KEY":"sk-123","PROJECT_DIR":"/tmp/x","SERVER_URL":"http://localhost:3000"}},"local":{"type":"http","url":"http://127.0.0.1:8000/mcp"},"public":{"type":"http","url":"https://mcp.context7.com/mcp"}}}`
	if c, r := appel("POST", "/api/extensions/mcp", map[string]any{"texte": conf}); c != 200 {
		t.Fatalf("paste: %d %v", c, r)
	}
	bib = lireBiblio(s.kitCourant())
	st := bib.MCP["studio"]
	if st.Reglages["PORT"] != "44755" || st.Reglages["HOST"] != "127.0.0.1" || st.Reglages["PROJECT_DIR"] != "/tmp/x" || st.Reglages["SERVER_URL"] != "http://localhost:3000" || len(st.Reglages) != 4 {
		t.Errorf("settings: %v", st.Reglages)
	}
	if strings.Join(st.Config.Args, " ") != "-y studio-mcp --port ${PORT} --host=${HOST}" || st.Config.Env["STUDIO_API_KEY"] != "${STUDIO_API_KEY}" || st.Config.Env["PROJECT_DIR"] != "${PROJECT_DIR}" {
		t.Errorf("config: %+v", st.Config)
	}
	if lireSecrets(s.kitCourant())["studio"]["STUDIO_API_KEY"] != "sk-123" {
		t.Error("key not stored")
	}
	if bib.MCP["local"].Config.URL != "${URL}" || bib.MCP["local"].Reglages["URL"] != "http://127.0.0.1:8000/mcp" || bib.MCP["public"].Config.URL != "https://mcp.context7.com/mcp" || bib.MCP["public"].Reglages != nil {
		t.Errorf("urls: %+v %+v", bib.MCP["local"], bib.MCP["public"])
	}
	// the view: settings in clear with their value, keys never
	_, v = appel("GET", "/api/extensions?p=a", nil)
	var vus []map[string]any
	for _, x := range v["mcp"].([]any) {
		m := x.(map[string]any)
		if m["id"] == "studio" {
			for _, sc := range m["secrets"].([]any) {
				vus = append(vus, sc.(map[string]any))
			}
			if !strings.Contains(m["resume"].(string), "${PORT}") && !strings.Contains(m["resume"].(string), "44755") {
				t.Errorf("resume: %v", m["resume"])
			}
		}
		if m["id"] == "local" && m["resume"] != "http://127.0.0.1:8000/mcp" {
			t.Errorf("local resume: %v", m["resume"])
		}
	}
	trouve := map[string]map[string]any{}
	for _, sc := range vus {
		trouve[sc["nom"].(string)] = sc
	}
	if trouve["PORT"]["reglage"] != true || trouve["PORT"]["valeur"] != "44755" || trouve["STUDIO_API_KEY"]["reglage"] != nil || trouve["STUDIO_API_KEY"]["valeur"] != nil {
		t.Errorf("view: %v", trouve)
	}
	// saving a setting; a URL setting must be an address
	if c, _ := appel("POST", "/api/extensions/secret", map[string]any{"id": "local", "nom": "URL", "valeur": "not a url"}); c != 400 {
		t.Errorf("bad url: %d", c)
	}
	if c, r := appel("POST", "/api/extensions/secret", map[string]any{"id": "studio", "nom": "PORT", "valeur": "44800"}); c != 200 || r["message_key"] != "Setting saved. It applies from the next cycle." {
		t.Errorf("save: %d %v", c, r)
	}
	if lireSecrets(s.kitCourant())["studio"]["PORT"] != "44800" {
		t.Error("setting not saved")
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestMCPWindows(t *testing.T) {
	kit := t.TempDir()
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "en"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(chemin string, corps any) (int, map[string]any) {
		b, _ := json.Marshal(corps)
		methode := "POST"
		if corps == nil {
			methode = "GET"
		}
		r := httptest.NewRequest(methode, chemin, bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	// Studio's own configuration, pasted: recognized as a Windows program, added only once the user says so
	studio := `{"mcpServers":{"Roblox_Studio":{"command":"cmd.exe","args":["/c","%LOCALAPPDATA%\\Roblox\\mcp.bat"]}}}`
	if c, r := appel("/api/extensions/mcp", map[string]any{"texte": studio}); c != 400 || !strings.Contains(fmt.Sprint(r), "Tick") {
		t.Fatalf("a Windows program pasted without the option must be refused: %d %v", c, r)
	}
	if _, ok := lireBiblio(s.kitCourant()).MCP["roblox_studio"]; ok {
		t.Fatal("added without the option")
	}
	if c, r := appel("/api/extensions/mcp", map[string]any{"texte": studio, "windows": true}); c != 200 {
		t.Fatalf("paste: %d %v", c, r)
	}
	// the option forces it for a command that does not look like one; without it, a Linux server stays caged
	appel("/api/extensions/mcp", map[string]any{"nom": "outil", "texte": `{"command":"mon-serveur","args":["--stdio"]}`, "windows": true})
	appel("/api/extensions/mcp", map[string]any{"nom": "linux", "texte": `{"command":"npx","args":["-y","pkg"]}`})
	if c, r := appel("/api/extensions/catalogue", map[string]any{"type": "mcp", "id": "roblox-studio"}); c != 200 {
		t.Fatalf("catalog: %d %v", c, r)
	}
	b := lireBiblio(s.kitCourant())
	for id, attendu := range map[string]string{"roblox_studio": "windows", "outil": "windows", "linux": "stdio", "roblox-studio": "windows"} {
		if b.MCP[id].Config.Type != attendu {
			t.Errorf("%s: type %q, expected %q", id, b.MCP[id].Config.Type, attendu)
		}
	}
	if b.MCP["roblox-studio"].Reglages["ROBLOX_STUDIO_MCP"] != `%LOCALAPPDATA%\Roblox\mcp.bat` {
		t.Errorf("setting: %v", b.MCP["roblox-studio"].Reglages)
	}
	// the view keeps the type, shows the command and the setting in clear
	_, v := appel("/api/extensions?p=a", nil)
	for _, x := range v["mcp"].([]any) {
		m := x.(map[string]any)
		if m["id"] != "roblox-studio" {
			continue
		}
		if m["type"] != "windows" || m["resume"] != `cmd.exe /c %LOCALAPPDATA%\Roblox\mcp.bat` {
			t.Errorf("view: %v", m)
		}
		sc := m["secrets"].([]any)[0].(map[string]any)
		if sc["reglage"] != true || sc["valeur"] != `%LOCALAPPDATA%\Roblox\mcp.bat` {
			t.Errorf("setting in the view: %v", sc)
		}
		return
	}
	t.Fatal("roblox-studio missing from the view")
}
