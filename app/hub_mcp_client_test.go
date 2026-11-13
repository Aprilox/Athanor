package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRelaisClientMCP (M184): «mcp-client appairer» in the terminal (wrong code refused, the right one then
// the owner's yes on screen, the token written 0600), then «mcp-client NAME»: initialize, tools/list (only the
// granted tools) and tools/call (only the granted projects) through the real /api/mcp; a revoked token and
// Athanor closed give a clear tool error, quickly, the server staying connected.
func TestRelaisClientMCP(t *testing.T) {
	s, mux, _ := serveurMCPTest(t)
	_ = os.Remove(cheminClients())
	t.Cleanup(func() { _ = os.Remove(cheminClients()) })
	anciens := []time.Duration{pauseRefusClient, pauseStatutClient}
	pauseRefusClient, pauseStatutClient = 0, 20*time.Millisecond
	t.Cleanup(func() { pauseRefusClient, pauseStatutClient = anciens[0], anciens[1] })
	conf := t.TempDir()
	for _, v := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(v, conf)
	}
	hub := httptest.NewServer(mux)
	defer hub.Close()
	d := t.TempDir()
	fInst := filepath.Join(d, "instance.json")
	ecrireInstanceTest(t, fInst, hub.URL)

	ouvrirCode := func() string {
		t.Helper()
		r := httptest.NewRecorder()
		s.apiClients(r, httptest.NewRequest("POST", "/", strings.NewReader(`{"action":"code"}`)))
		m := s.cli()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.code == "" {
			t.Fatal("no code opened:", r.Body.String())
		}
		return m.code
	}
	var sortie strings.Builder
	if err := appairerClientTerminal(strings.NewReader("1\n"), &sortie, "../x", "code", fInst); err == nil {
		t.Fatal("a name with a path accepted")
	}
	if err := appairerClientTerminal(strings.NewReader("1\n"), &sortie, "claude", "code", filepath.Join(d, "absent.json")); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatal("Athanor closed:", err)
	}
	code := ouvrirCode()
	faux := "000000"
	if code == faux {
		faux = "111111"
	}
	if err := appairerClientTerminal(strings.NewReader(faux+"\n"), &sortie, "claude", "code", fInst); err == nil || !strings.Contains(err.Error(), "wrong or expired") {
		t.Fatal("wrong code:", err)
	}

	fini := make(chan error, 1)
	go func() { fini <- appairerClientTerminal(strings.NewReader(code+"\n"), &sortie, "claude", "code", fInst) }()
	var ref string
	for i := 0; i < 200 && ref == ""; i++ {
		if dem, _ := s.vueClients()["demandes"].([]demandeClient); len(dem) == 1 {
			ref = dem[0].Ref
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if ref == "" {
		t.Fatal("the request never reached the window")
	}
	if _, err := os.Stat(fichierJetonClient("claude")); err == nil {
		t.Fatal("a token before the owner's yes")
	}
	if err := s.deciderClient(ref, true, []string{"demo"}, []string{"etat", "projets"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-fini:
		if err != nil {
			t.Fatal("pairing:", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal never got its token")
	}
	fJeton := fichierJetonClient("claude")
	if !strings.HasPrefix(fJeton, conf) {
		t.Fatalf("token outside UserConfigDir: %s", fJeton)
	}
	st, err := os.Stat(fJeton)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v", st.Mode().Perm())
	}
	if !strings.Contains(sortie.String(), "claude mcp add athanor --") {
		t.Fatalf("no Claude Code line: %s", sortie.String())
	}

	relais := func(lignes ...string) []reponseMCP {
		t.Helper()
		var out strings.Builder
		debut := time.Now()
		relaisMCP(strings.NewReader(strings.Join(lignes, "\n")), &out, fJeton, fInst, 5*time.Second, textesClient)
		if dt := time.Since(debut); dt > 4*time.Second {
			t.Fatalf("relay too slow: %v", dt)
		}
		var reps []reponseMCP
		for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			var r reponseMCP
			if err := json.Unmarshal([]byte(l), &r); err != nil {
				t.Fatalf("line %q: %v", l, err)
			}
			reps = append(reps, r)
		}
		return reps
	}
	ouvrir := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	liste := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	projets := appelOutil("projets", `{}`)
	reps := relais(ouvrir, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, liste, projets)
	if len(reps) != 3 || reps[0].Result.ProtocolVersion == "" {
		t.Fatalf("initialize: %+v", reps)
	}
	if len(reps[1].Result.Tools) != 2 {
		t.Fatalf("tools/list: only the 2 granted tools expected: %+v", reps[1].Result.Tools)
	}
	if reps[2].Result.IsError || !strings.Contains(reps[2].Result.Content[0].Text, "demo") || strings.Contains(reps[2].Result.Content[0].Text, "autre") {
		t.Fatalf("tools/call projets: %+v", reps[2])
	}

	panne := func(nom, attendu string) {
		t.Helper()
		reps := relais(ouvrir, liste, projets)
		if len(reps) != 3 || reps[0].Result.ProtocolVersion == "" || len(reps[1].Result.Tools) != 0 {
			t.Fatalf("%s: %+v", nom, reps)
		}
		if !reps[2].Result.IsError || !strings.Contains(reps[2].Result.Content[0].Text, attendu) {
			t.Fatalf("%s: tool error %+v", nom, reps[2])
		}
	}
	id := s.vueClients()["clients"].([]ClientMCP)[0].ID
	if err := s.revoquerClient(id); err != nil {
		t.Fatal(err)
	}
	panne("revoked", "refused this client's token")
	hub.Close()
	panne("Athanor closed", "Athanor is not open")
}
