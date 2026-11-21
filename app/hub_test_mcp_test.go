package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// initMCPTest: the init event of a session's stream-json, with these athanor-hub tools.
func initMCPTest(statut string, outils ...string) string {
	l := []string{"Bash", "Read"}
	for _, o := range outils {
		l = append(l, "mcp__"+nomMCPHub+"__"+o)
	}
	b, _ := json.Marshal(map[string]any{"type": "system", "subtype": "init", "tools": l,
		"mcp_servers": []map[string]string{{"name": nomMCPHub, "status": statut}}})
	return string(b) + "\n"
}

func TestVerdictMCP(t *testing.T) {
	audit := []LigneAudit{{Action: "mcp environnements", Resultat: "ok"}, {Action: "mcp demander_acces", Resultat: "ok"}, {Action: "approbation accepter"}}
	bon := initMCPTest("connected", outilsPilote...) + `{"type":"user"}` + "\nATTENTE vue temoin-mcp-x\n"
	for _, cas := range []struct {
		nom, journal string
		accepte      bool
		audit        []LigneAudit
		ok           bool
	}{
		{"tout bon", bon, true, audit, true},
		{"outil en trop", initMCPTest("connected", append(outilsPilote, "lire_coffre")...) + "ATTENTE vue temoin-mcp-x", true, audit, false},
		{"outil manquant", initMCPTest("connected", outilsPilote[1:]...) + "ATTENTE vue temoin-mcp-x", true, audit, false},
		{"serveur absent", initMCPTest("failed") + "ATTENTE vue temoin-mcp-x", true, audit, false},
		{"valeur non lue", initMCPTest("connected", outilsPilote...) + "ATTENTE absente", true, audit, false},
		{"pas accepté", bon, false, audit, false},
		{"appel absent du journal", bon, true, audit[1:], false},
		{"appel en erreur", bon, true, []LigneAudit{{Action: "mcp environnements", Resultat: "erreur : x"}, audit[1], audit[2]}, false},
	} {
		if _, ok := verdictMCP(cas.journal, "temoin-mcp-x", cas.accepte, cas.audit); ok != cas.ok {
			t.Errorf("%s: ok=%v, attendu %v", cas.nom, ok, cas.ok)
		}
	}
}

// TestTesterMCP: the application side against the hub's real route and the real relay, the session played by
// the test (init event, tools/list, environnements, demander_acces, then secrets.env read again).
func TestTesterMCP(t *testing.T) {
	s := serveurCoffreTest(t)
	s.chargerAcces()
	mux := http.NewServeMux()
	s.routes(mux)
	hub := httptest.NewServer(mux)
	defer hub.Close()
	fInst := filepath.Join(t.TempDir(), "instance.json")
	ecrireInstanceTest(t, fInst, hub.URL)
	k := s.kitCourant()
	var sortie []string
	ok := s.testerMCP(k, func(l string) { sortie = append(sortie, l) }, func(projet string) error {
		p, connu := s.projetParNom(projetTestMCP)
		if !connu || p.Dossier != projet {
			t.Errorf("le projet temporaire n'est pas connu du hub: %+v", p)
		}
		var out strings.Builder
		relaisMCPHub(strings.NewReader(strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
			appelOutil("environnements", "{}"), appelOutil("demander_acces", `{"nom":"`+variableTestMCP+`","raison":"test"}`)}, "\n")),
			&out, s.fichierJeton(p), fInst, 10*time.Second)
		reps := strings.Split(strings.TrimSpace(out.String()), "\n")
		var liste reponseMCP
		if len(reps) != 3 || json.Unmarshal([]byte(reps[0]), &liste) != nil || strings.Contains(reps[0], `"required":null`) {
			t.Fatalf("relay: %q", out.String())
		}
		var outils []string
		for _, o := range liste.Result.Tools {
			outils = append(outils, o.Name)
		}
		journal := initMCPTest("connected", outils...) + strings.Join(reps, "\n") + "\n"
		vu := "ATTENTE absente"
		for i := 0; i < 100 && vu == "ATTENTE absente"; i++ {
			b, _ := os.ReadFile(filepath.Join(p.Atelier(), "secrets.env"))
			for _, l := range strings.Split(string(b), "\n") {
				if v, a := strings.CutPrefix(strings.TrimSpace(l), variableTestMCP+"="); a && v != "" {
					vu = "ATTENTE vue " + v
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		return os.WriteFile(filepath.Join(projet, filepath.FromSlash(journalTestMCP)), []byte(journal+vu+"\n"), 0o644)
	})
	if !ok {
		t.Fatalf("le test a échoué:\n%s", strings.Join(sortie, "\n"))
	}
	verifierNettoyageMCP(t, s, k)
	// the session never started (no Claude): skipped cleanly, nothing left
	sortie = nil
	if s.testerMCP(k, func(l string) { sortie = append(sortie, l) }, func(string) error { return nil }) ||
		!strings.Contains(strings.Join(sortie, "\n"), "SKIPPED") {
		t.Errorf("sans session: %v", sortie)
	}
	verifierNettoyageMCP(t, s, k)
	// Claude Code not logged in where the pilot runs: skipped too, with the reason
	sortie = nil
	if s.testerMCP(k, func(l string) { sortie = append(sortie, l) }, func(projet string) error {
		return os.WriteFile(filepath.Join(projet, filepath.FromSlash(journalTestMCP)), []byte(`{"type":"assistant","error":"authentication_failed"}`+"\n"), 0o644)
	}) || !strings.Contains(strings.Join(sortie, "\n"), "not logged in") {
		t.Errorf("sans connexion: %v", sortie)
	}
	verifierNettoyageMCP(t, s, k)
}

func verifierNettoyageMCP(t *testing.T, s *Serveur, k Kit) {
	t.Helper()
	for _, e := range s.coffre.Liste() {
		if e.Source == sourceTestMCP {
			t.Error("valeur témoin restée dans le coffre")
		}
	}
	if slices.ContainsFunc(s.jetons.Liste(), func(j SessionHub) bool { return j.Projet == projetTestMCP }) || len(s.acces.Projet(projetTestMCP).Acces) != 0 {
		t.Error("jeton ou accès de test restés")
	}
	if _, connu := s.projetParNom(projetTestMCP); connu {
		t.Error("le projet temporaire reste connu du hub")
	}
	if _, err := os.Stat(filepath.Join(k.Journaux(), dossierTestMCP)); err == nil {
		t.Error("dossier de test resté")
	}
}

// TestTesterMCPReel: the real test, by hand (ATHANOR_TEST_MCP_REEL = a kit holding freshly built athanor and
// Athanor.exe, the repository itself): a real Claude session, in WSL from Windows (WSL mode) or native.
func TestTesterMCPReel(t *testing.T) {
	kit := os.Getenv("ATHANOR_TEST_MCP_REEL")
	if kit == "" {
		t.Skip("ATHANOR_TEST_MCP_REEL not set (a real Claude session, run by hand)")
	}
	s := serveurCoffreTest(t)
	s.config.Kit, s.kit = kit, Kit{Racine: kit}
	s.chargerAcces()
	mux := http.NewServeMux()
	s.routes(mux)
	hub := httptest.NewServer(mux)
	defer hub.Close()
	fInst := filepath.Join(t.TempDir(), "instance.json")
	ecrireInstanceTest(t, fInst, hub.URL)
	ancien := modeNatif.Load()
	modeNatif.Store(runtime.GOOS == "windows" && os.Getenv("ATHANOR_TEST_MCP_NATIF") == "1")
	t.Cleanup(func() { modeNatif.Store(ancien) })
	ok := s.testerMCP(Kit{Racine: kit}, func(l string) { t.Log(l) }, func(projet string) error {
		c := commandeKit(Kit{Racine: kit}, "tester-mcp", ".", "logs/"+dossierTestMCP, fInst)
		if modeNatif.Load() || runtime.GOOS != "windows" {
			c.Env = os.Environ()
		}
		out, err := c.CombinedOutput()
		t.Log(strings.TrimSpace(string(out)))
		return err
	})
	if !ok {
		t.Error("the real hub MCP test failed")
	}
}
