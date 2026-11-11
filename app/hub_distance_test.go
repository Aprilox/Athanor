package main

import (
	"net/http"
	"os"
	"regexp"
	"testing"
)

// TestDistanceToutesRoutes: every route of serveur.go through gestionDistance with a paired device.
// The hub's routes are an allowlist (refused by default), the old ones keep interditDistance.
func TestDistanceToutesRoutes(t *testing.T) {
	s, h, _ := serveurDistance(t)
	cle := appairerTest(t, s, h)
	atteint := false
	// a handler that only notes it was reached: the decision is gestionDistance's, no route really runs
	hf := s.gestionDistance(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atteint = true
		w.WriteHeader(http.StatusTeapot)
	}))
	appel := func(methode, chemin string) (int, bool) {
		atteint = false
		w := appelDistance(hf, methode, chemin, cle, "")
		return w.Code, atteint
	}

	src, err := os.ReadFile("serveur.go")
	if err != nil {
		t.Fatal(err)
	}
	motifs := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/[^"]*)"`).FindAllStringSubmatch(string(src), -1)
	if len(motifs) < 50 {
		t.Fatalf("only %d routes read in serveur.go", len(motifs))
	}
	joker := regexp.MustCompile(`\{[^}]*\}`)
	vus := map[string]bool{}
	for _, m := range motifs {
		methode, chemin := m[1], joker.ReplaceAllString(m[2], "x")
		vus[methode+" "+m[2]] = true
		r, _ := http.NewRequest(methode, "https://abc-def.trycloudflare.com"+chemin, nil)
		refus := interditDistance(r) || refuseHubDistance(r)
		code, ok := appel(methode, chemin)
		switch {
		case refus && (code != http.StatusForbidden || ok):
			t.Errorf("%s %s: %d (reached %v), want 403", methode, chemin, code, ok)
		case !refus && (code == http.StatusForbidden || !ok):
			t.Errorf("%s %s: %d (reached %v), want the route reached", methode, chemin, code, ok)
		}
	}
	for _, r := range []string{"GET /api/hub", "POST /api/hub/alertes", "POST /api/mot-de-passe", "POST /api/ouvrir", "POST /api/instance/{action}", "POST /api/distance", "GET /api/projets"} {
		if !vus[r] {
			t.Errorf("route %q no longer in serveur.go: update this test", r)
		}
	}

	cas := []struct {
		methode, chemin string
		refus           bool
	}{
		{"GET", "/api/hub", false},         // allowed
		{"GET", "/api/coffre", false},      // allowed (the vault's list, no secret value)
		{"POST", "/api/hub", true},         // hub route not in the allowlist
		{"POST", "/api/hub/alertes", true}, // Ignore / Reopen: written on this computer only
		{"GET", "/api/coffre/x", true},     // invented, no allowlist entry: refused by default
		{"POST", "/api/coffre/x", true},
		{"DELETE", "/api/coffre/x", true},
		{"POST", "/api/coffre", true},
		{"GET", "/api/acces", false},               // a project's grants, no value
		{"GET", "/api/hub/audit", false},           // the audit log, read from a device
		{"POST", "/api/hub/audit/verifier", false}, // and checked
		{"POST", "/api/hub/audit", true},
		{"GET", "/api/hub/audit/verifier", true},
		{"GET", "/api/hub/approbations", false},  // P7: the owner reads the approvals from the phone
		{"POST", "/api/hub/approbations", false}, // and decides them
		{"DELETE", "/api/hub/approbations", true},
		{"POST", "/api/hub/approbations/x", true},
		{"POST", "/api/acces", true},
		{"GET", "/api/acces/x", true},
		{"GET", "/api/hubx", true}, // plain prefix
		{"POST", "/api/mot-de-passe", true},
		{"POST", "/api/ouvrir", true},
		{"POST", "/api/mcp", true}, // the hub's MCP: never through the tunnel
		{"POST", "/api/instance/x", true},
		{"POST", "/api/distance", true},
		{"GET", "/api/distance", false},     // unchanged: a device reads the Remote page
		{"GET", "/api/projets", false},      // unchanged
		{"POST", "/api/preferences", false}, // unchanged
	}
	for _, c := range cas {
		code, ok := appel(c.methode, c.chemin)
		if c.refus != (code == http.StatusForbidden) || c.refus == ok {
			t.Errorf("%s %s: %d (reached %v), refused expected %v", c.methode, c.chemin, code, ok, c.refus)
		}
	}
}
