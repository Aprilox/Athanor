package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// serveurMCPTest: a server with the projects demo and autre, three test tools, and its routes.
func serveurMCPTest(t *testing.T) (*Serveur, *http.ServeMux, *horlogeJeton) {
	t.Helper()
	s := serveurCoffreTest(t)
	refusMCP.Lock()
	refusMCP.derniere, refusMCP.n = time.Time{}, 0 // C30: each test sees its first refusal logged
	refusMCP.Unlock()
	h := &horlogeJeton{time.Now()}
	s.jetons.Maintenant = h.maintenant
	d := t.TempDir()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	ancien := outilsHub
	outilsHub = map[string]OutilHub{
		"t_a": {Description: "a", Schema: map[string]any{"type": "object"}, Faire: func(s *Serveur, x SessionHub, a json.RawMessage) (string, error) {
			return "a:" + x.Projet + ":" + string(a), nil
		}},
		"t_b":   {Description: "b", Schema: map[string]any{"type": "object"}, Faire: func(*Serveur, SessionHub, json.RawMessage) (string, error) { return "b", nil }},
		"t_err": {Description: "e", Schema: map[string]any{"type": "object"}, Faire: func(*Serveur, SessionHub, json.RawMessage) (string, error) { return "", errors.New("boom") }},
	}
	t.Cleanup(func() { outilsHub = ancien })
	mux := http.NewServeMux()
	s.routes(mux)
	return s, mux, h
}

type reponseMCP struct {
	Code   int
	ID     json.RawMessage
	Result struct {
		ProtocolVersion string `json:"protocolVersion"`
		Tools           []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	Error *erreurMCP
}

func appelMCP(t *testing.T, mux http.Handler, auth, corps string) reponseMCP {
	t.Helper()
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/mcp", strings.NewReader(corps))
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	rep := reponseMCP{Code: w.Code}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
			t.Fatalf("answer %q: %v", w.Body.String(), err)
		}
	}
	return rep
}

func appelOutil(nom, args string) string {
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + nom + `","arguments":` + args + `}}`
}

// TestMCPProtocole: initialize, notification, tools/list limited to the token, tools/call (ok, tool error,
// tool of another scope refused), JSON-RPC errors, every call in the audit log.
func TestMCPProtocole(t *testing.T) {
	s, mux, _ := serveurMCPTest(t)
	jeton, err := s.jetons.Creer("demo", []string{"t_a", "t_err", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	b := "Bearer " + jeton

	rep := appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if rep.Code != 200 || rep.Error != nil || rep.Result.ProtocolVersion != "2025-03-26" || string(rep.ID) != "1" {
		t.Fatalf("initialize: %+v", rep)
	}
	if rep = appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":"x","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`); rep.Result.ProtocolVersion != versionsMCP[0] || string(rep.ID) != `"x"` {
		t.Fatalf("initialize, unknown version: %+v", rep)
	}
	if rep = appelMCP(t, mux, b, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); rep.Code != http.StatusAccepted {
		t.Fatalf("notification: %d", rep.Code)
	}
	rep = appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var noms []string
	for _, o := range rep.Result.Tools {
		noms = append(noms, o.Name)
	}
	if strings.Join(noms, ",") != "t_a,t_err" { // t_b: another scope; "absent": no such tool
		t.Fatalf("tools/list: %v", noms)
	}
	if rep = appelMCP(t, mux, b, appelOutil("t_a", `{"projet":"autre"}`)); rep.Error != nil || rep.Result.IsError || len(rep.Result.Content) != 1 || rep.Result.Content[0].Text != `a:demo:{"projet":"autre"}` {
		t.Fatalf("t_a: %+v", rep)
	}
	if rep = appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"t_a"}}`); rep.Result.Content[0].Text != "a:demo:{}" {
		t.Fatalf("t_a without arguments: %+v", rep)
	}
	if rep = appelMCP(t, mux, b, appelOutil("t_err", "{}")); !rep.Result.IsError || rep.Result.Content[0].Text != "boom" {
		t.Fatalf("t_err: %+v", rep)
	}
	for _, nom := range []string{"t_b", "absent", "inexistant"} {
		if rep = appelMCP(t, mux, b, appelOutil(nom, "{}")); rep.Error == nil || rep.Error.Code != -32602 {
			t.Fatalf("%s called: %+v", nom, rep)
		}
	}
	for corps, code := range map[string]int{
		`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`:              -32601,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{}}`:      -32602,
		`{"jsonrpc":"1.0","id":3,"method":"tools/list"}`:                  -32600,
		`[{"jsonrpc":"2.0","id":3,"method":"tools/list"}]`:                -32700, // no batch
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"`:                   -32700,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"texte"}`: -32602,
	} {
		if rep = appelMCP(t, mux, b, corps); rep.Code != 200 || rep.Error == nil || rep.Error.Code != code {
			t.Errorf("%s: %+v, want %d", corps, rep, code)
		}
	}
	journal := ""
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if strings.HasPrefix(l.Action, "mcp ") {
			journal += l.Projet + " " + l.Agent + " " + l.Action + " " + l.Resultat + "\n"
		}
	}
	for _, attendu := range []string{"demo pilote mcp t_a ok", "demo pilote mcp t_err erreur : boom", "demo pilote mcp t_b refusé", "demo pilote mcp absent refusé", "demo pilote mcp inexistant refusé"} {
		if !strings.Contains(journal, attendu) {
			t.Errorf("audit log without %q:\n%s", attendu, journal)
		}
	}
	if strings.Contains(auditTexte(t), jeton) {
		t.Error("token in the audit log")
	}
}

// TestMCPJetonsRefuses: absent, of another scheme, in the URL, invented, of an earlier launch, revoked,
// expired, of a pilot stopped: 401, and the tool never runs.
func TestMCPJetonsRefuses(t *testing.T) {
	s, mux, h := serveurMCPTest(t)
	appels := 0
	outilsHub["t_a"] = OutilHub{Schema: map[string]any{}, Faire: func(*Serveur, SessionHub, json.RawMessage) (string, error) { appels++; return "", nil }}
	creer := func(projet string) string {
		j, err := s.jetons.Creer(projet, []string{"t_a"})
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	refuse := func(cas, auth string) {
		t.Helper()
		if rep := appelMCP(t, mux, auth, appelOutil("t_a", "{}")); rep.Code != http.StatusUnauthorized || rep.Error == nil || rep.Result.Content != nil {
			t.Errorf("%s: %+v, want 401", cas, rep)
		}
	}
	ancien := creer("demo")
	jeton := creer("demo") // a new launch: the earlier session is revoked
	refuse("absent", "")
	refuse("empty", "Bearer ")
	refuse("basic", "Basic "+jeton)
	refuse("without the scheme", jeton)
	refuse("invented", "Bearer "+aleatoire(32))
	refuse("earlier launch", "Bearer "+ancien)
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/mcp?jeton="+jeton, strings.NewReader(appelOutil("t_a", "{}")))
	r.AddCookie(&http.Cookie{Name: "athanor_session", Value: jeton})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("token in the URL and a cookie: %d", w.Code)
	}
	if rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil("t_a", "{}")); rep.Code != 200 || appels != 1 {
		t.Fatalf("valid token refused: %+v", rep)
	}

	p, _ := s.projetParNom("demo")
	s.revoquerJetonProjet(p, "test")
	refuse("revoked", "Bearer "+jeton)

	jeton = creer("demo")
	h.t = h.t.Add(dureeJeton + time.Minute)
	refuse("expired", "Bearer "+jeton)

	jeton = creer("autre")
	h.t = h.t.Add(graceJeton + time.Minute) // the pilot of autre never started
	refuse("pilot stopped", "Bearer "+jeton)
	if appels != 1 {
		t.Errorf("the tool ran %d times", appels)
	}
	if !strings.Contains(auditTexte(t), "mcp jeton refusé") {
		t.Error("refused token not in the audit log")
	}
}

// TestMCPJetonAutresRoutes: a valid session token, or an external client's (M183), opens no other route of serveur.go.
func TestMCPJetonAutresRoutes(t *testing.T) {
	s, mux, _ := serveurMCPTest(t)
	jeton, err := s.jetons.Creer("demo", outilsPilote)
	if err != nil {
		t.Fatal(err)
	}
	client := aleatoire(32)
	_ = os.Remove(cheminClients())
	t.Cleanup(func() { _ = os.Remove(cheminClients()) })
	m := s.cli()
	m.mu.Lock()
	m.liste = append(m.liste, ClientMCP{ID: "c1", Nom: "Claude Code", Sorte: "code", Jeton: hacherJeton(client), Outils: outilsClients, Projets: []string{"*"}})
	m.mu.Unlock()
	if _, ok := s.verifierClient(client); !ok {
		t.Fatal("test client not recognised")
	}
	src, err := os.ReadFile("serveur.go")
	if err != nil {
		t.Fatal(err)
	}
	motifs := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/api/[^"]*)"`).FindAllStringSubmatch(string(src), -1)
	if len(motifs) < 50 {
		t.Fatalf("only %d routes read in serveur.go", len(motifs))
	}
	joker := regexp.MustCompile(`\{[^}]*\}`)
	vue := false
	for _, m := range motifs {
		if m[2] == "/api/mcp" {
			vue = true
			continue
		}
		if strings.HasPrefix(m[2], "/api/mcp/appairer") || m[2] == "/api/annonce" { // public pairing (a code, never a token): "{}" is a bad request
			continue
		}
		for sorte, j := range map[string]string{"session": jeton, "client": client} {
			r := httptest.NewRequest(m[1], "http://127.0.0.1"+joker.ReplaceAllString(m[2], "x"), strings.NewReader("{}"))
			r.Header.Set("Authorization", "Bearer "+j)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with a %s token: %d", m[1], m[2], sorte, w.Code)
			}
		}
	}
	if !vue {
		t.Error("POST /api/mcp no longer in serveur.go")
	}
	r := httptest.NewRequest("GET", "https://abc-def.trycloudflare.com/api/mcp", nil)
	r.Method = "POST"
	if !interditDistance(r) {
		t.Error("/api/mcp allowed through the tunnel")
	}
}

// TestI7Listeners: every listener of the application is on 127.0.0.1 (never all the interfaces).
func TestI7Listeners(t *testing.T) {
	fichiers, _ := filepath.Glob("*.go")
	ecoute := regexp.MustCompile(`\bnet\.Listen\w*\(|ListenAndServe\w*\(|\.Listen\(ctx|ListenConfig`)
	n, multicast := 0, 0
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, ligne := range strings.Split(string(b), "\n") {
			if ecoute.MatchString(ligne) {
				n++
				if f == "hub_mdns.go" && strings.Contains(ligne, "net.ListenMulticastUDP(") {
					multicast++ // the one exception: mDNS, off by default (checked below)
				} else if !strings.Contains(ligne, `"127.0.0.1:`) {
					t.Errorf("%s:%d listens elsewhere than on 127.0.0.1: %s", f, i+1, strings.TrimSpace(ligne))
				}
			}
		}
	}
	if n < 4 {
		t.Fatalf("only %d listeners found", n)
	}
	if multicast != 1 {
		t.Fatalf("%d multicast sockets in hub_mdns.go (exactly one expected)", multicast)
	}
	verifierMDNSOffOn(t) // hub_mdns_test.go: off by default, no socket outside the loopback while off
}
