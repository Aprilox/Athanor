package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// relaisTest: the relay's answers, one decoded line each, for the lines given on stdin.
func relaisTest(t *testing.T, fJeton, fInst string, lignes ...string) []reponseMCP {
	t.Helper()
	var out strings.Builder
	debut := time.Now()
	relaisMCPHub(strings.NewReader(strings.Join(lignes, "\n")), &out, fJeton, fInst, 5*time.Second)
	if d := time.Since(debut); d > 4*time.Second {
		t.Fatalf("relay too slow: %v", d)
	}
	var reps []reponseMCP
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var r reponseMCP
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		reps = append(reps, r)
	}
	return reps
}

func ecrireInstanceTest(t *testing.T, f, adresse string) {
	t.Helper()
	u, _ := url.Parse(adresse)
	b, _ := json.Marshal(map[string]any{"port": json.Number(u.Port()), "jeton": "x", "pid": 1})
	if err := os.WriteFile(f, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRelaisMCPHub: initialize, list and call through the relay against the hub's real route; then
// Athanor closed, no instance, no token, a revoked token: clear errors, quickly, the server kept connected.
func TestRelaisMCPHub(t *testing.T) {
	s, mux, _ := serveurMCPTest(t)
	jeton, err := s.jetons.Creer("demo", []string{"t_a"})
	if err != nil {
		t.Fatal(err)
	}
	hub := httptest.NewServer(mux)
	defer hub.Close()
	d := t.TempDir()
	fJeton, fInst := filepath.Join(d, "hub-jeton"), filepath.Join(d, "instance.json")
	_ = os.WriteFile(fJeton, []byte(jeton+"\n"), 0o600)
	ecrireInstanceTest(t, fInst, hub.URL)

	ouvrir := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	liste := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	reps := relaisTest(t, fJeton, fInst, ouvrir, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, liste, appelOutil("t_a", `{"x":1}`), "pas du json")
	if len(reps) != 4 {
		t.Fatalf("4 answers expected (the notification has none): %+v", reps)
	}
	if string(reps[0].ID) != "1" || reps[0].Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("initialize: %+v", reps[0])
	}
	if string(reps[1].ID) != "2" || len(reps[1].Result.Tools) != 1 || reps[1].Result.Tools[0].Name != "t_a" {
		t.Fatalf("tools/list: %+v", reps[1])
	}
	if string(reps[2].ID) != "7" || reps[2].Result.IsError || reps[2].Result.Content[0].Text != `a:demo:{"x":1}` {
		t.Fatalf("tools/call: %+v", reps[2])
	}
	if reps[3].Error == nil || reps[3].Error.Code != -32700 {
		t.Fatalf("parse error: %+v", reps[3])
	}

	verifierPanne := func(nom, fJ, fI, attendu string) {
		t.Helper()
		reps := relaisTest(t, fJ, fI, ouvrir, liste, appelOutil("t_a", `{}`), `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
		if len(reps) != 4 || reps[0].Result.ProtocolVersion == "" || reps[1].Error != nil || len(reps[1].Result.Tools) != 0 || string(reps[3].ID) != "9" || reps[3].Error != nil {
			t.Fatalf("%s: %+v", nom, reps)
		}
		if !reps[2].Result.IsError || !strings.Contains(reps[2].Result.Content[0].Text, attendu) {
			t.Fatalf("%s: tool error %+v", nom, reps[2])
		}
	}
	verifierPanne("no instance.json", fJeton, filepath.Join(d, "absent.json"), "Athanor is not open")
	verifierPanne("no token file", filepath.Join(d, "absent"), fInst, "No hub session token")
	ferme := httptest.NewServer(mux)
	fFerme := filepath.Join(d, "ferme.json")
	ecrireInstanceTest(t, fFerme, ferme.URL)
	ferme.Close()
	verifierPanne("Athanor closed", fJeton, fFerme, "Athanor is not open")
	s.revoquerJetons("test", func(SessionHub) bool { return true })
	verifierPanne("revoked token", fJeton, fInst, "refused this session's token")
}

// TestConfigMCPHub: construireMCP adds athanor-hub without any extension, for the main pilot only and only
// with a token file; the command of each system; the merge's file without it.
func TestConfigMCPHub(t *testing.T) {
	kit, projet := t.TempDir(), t.TempDir()
	prive := filepath.Join(kit, "logs", "pilotes", "abc")
	_ = os.MkdirAll(prive, 0o755)
	fichier := filepath.Join(prive, "mcp.json")
	ancien := systemeMCPHub
	defer func() { systemeMCPHub = ancien }()

	if ids, _ := construireMCP(kit, nil, fichier, projet, ""); len(ids) != 0 || fichierRegulierPilote(fichier) {
		t.Fatalf("no token: %v", ids)
	}
	jeton := filepath.Join(prive, "hub-jeton")
	_ = os.WriteFile(jeton, []byte("j"), 0o600)
	for sys, attendu := range map[string][]string{
		"windows": {filepath.Join(kit, "Athanor.exe"), "mcp-hub", jeton},
		"unix":    {filepath.Join(kit, "athanor"), "mcp-hub", jeton},
		"wsl":     {filepath.Join(kit, "athanor"), "mcp-windows", "--", filepath.Join(kit, "Athanor.exe"), "mcp-hub", cheminWindowsWSL(jeton)},
	} {
		systemeMCPHub = func() string { return sys }
		ids, _ := construireMCP(kit, nil, fichier, projet, "")
		var m struct {
			MCPServers map[string]struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		b, _ := os.ReadFile(fichier)
		if json.Unmarshal(b, &m) != nil || strings.Join(ids, ",") != nomMCPHub {
			t.Fatalf("%s: %v %s", sys, ids, b)
		}
		h := m.MCPServers[nomMCPHub]
		if h.Type != "stdio" || strings.Join(append([]string{h.Command}, h.Args...), "|") != strings.Join(attendu, "|") {
			t.Fatalf("%s: %+v", sys, h)
		}
	}
	if f := mcpSansHub(fichier); f != "" {
		t.Fatalf("merge with the hub alone: %q", f)
	}
	// a worker (principal given): never the hub, even with the token next to its file
	fOuvrier := filepath.Join(prive, "mcp-M3.json")
	if ids, _ := construireMCP(kit, nil, fOuvrier, projet, projet); len(ids) != 0 || fichierRegulierPilote(fOuvrier) {
		t.Fatalf("worker: %v", ids)
	}
	// the merge: the other servers kept, the hub left out
	var autres, entree objetJSON
	entree.mettre("command", "x")
	autres.mettre("autre", entree)
	autres.mettre(nomMCPHub, entree)
	if ecrireMCP(fichier, autres) != nil {
		t.Fatal("write")
	}
	f := mcpSansHub(fichier)
	serveurs := lireJSONOrdonne(f).objet("mcpServers")
	if f == fichier || strings.Join(serveurs.cles, ",") != "autre" {
		t.Fatalf("merge: %s %v", f, serveurs.cles)
	}
}
