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
	for _, r := range []string{"GET /api/hub", "POST /api/hub/alertes", "POST /api/mot-de-passe", "POST /api/ouvrir", "POST /api/instance/{action}", "POST /api/distance", "GET /api/projets", "POST /api/mcp/appairer", "POST /api/mcp/appairer/statut", "GET /api/clients", "POST /api/clients", "GET /api/annonce", "GET /api/instances", "POST /api/instances", "POST /api/instances/mdns"} {
		if !vus[r] {
			t.Errorf("route %q no longer in serveur.go: update this test", r)
		}
	}

	cas := []struct {
		methode, chemin string
		refus           bool
	}{
		{"GET", "/api/hub", false},                          // allowed
		{"POST", "/api/projets/demo/cle-deploiement", true}, // B68: a key registered on GitHub, at the computer only
		{"GET", "/api/projets/demo/environnements", false},
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
		{"POST", "/api/hub/urgence", false}, // the emergency stop from the phone
		{"GET", "/api/hub/urgence", true},
		{"POST", "/api/acces", true},
		{"GET", "/api/acces/x", true},
		{"GET", "/api/serveurs", false},   // the inventory, read from a device
		{"GET", "/api/serveurs/x", false}, // and a server's sheet
		{"POST", "/api/serveurs/x", true}, // renamed, roles changed, removed: on this computer only
		{"DELETE", "/api/serveurs/x", true},
		{"POST", "/api/serveurs/ajouter", true},
		{"GET", "/api/serveurs/x/y", true},
		{"GET", "/api/serveurs/x/taches", false}, // a server's tasks, read from a device (M97)
		{"GET", "/api/missions", false},          // the missions, read from a device (M150)
		{"GET", "/api/missions/x", false},
		{"POST", "/api/missions/x", true},
		{"GET", "/api/missions/x/y", true},
		{"POST", "/api/missions", true},             // a mission is created on the PC (M155)
		{"POST", "/api/missions/x/modifier", false}, // changed, answered and cancelled from an unlocked device
		{"POST", "/api/missions/x/repondre", false},
		{"POST", "/api/missions/x/annuler", false},
		{"POST", "/api/missions/x/lancer", false},
		{"GET", "/api/missions/x/annuler", true},
		{"POST", "/api/missions/x/supprimer", true}, // invented: refused by default
		{"POST", "/api/missions/x/y/annuler", true},
		{"GET", "/api/fournisseurs", false}, // the providers, zones and rights, read from a device (M114)
		{"POST", "/api/fournisseurs", true}, // added, tested, removed on this computer only
		{"POST", "/api/fournisseurs/x/tester", true},
		{"DELETE", "/api/fournisseurs/x", true},
		{"GET", "/api/fournisseursx", true},
		{"POST", "/api/serveurs/x/taches", true}, // launched and stopped on this computer only
		{"POST", "/api/serveurs/x/taches/t/arreter", true},
		{"GET", "/api/serveurs/x/y/taches", true},
		{"GET", "/api/hubx", true}, // plain prefix
		{"POST", "/api/mot-de-passe", true},
		{"POST", "/api/ouvrir", true},
		{"POST", "/api/mcp", true},                 // the hub's MCP: never through the tunnel
		{"POST", "/api/mcp/appairer", true},        // an MCP client's pairing: local only (M182)
		{"POST", "/api/mcp/appairer/statut", true}, // and its answer
		{"GET", "/api/annonce", true},              // the announcement and the instances of this PC (M185)
		{"GET", "/api/instances", true},
		{"POST", "/api/instances", true},
		{"POST", "/api/instances/mdns", true}, // the local network discovery: the computer only (M186)
		{"GET", "/api/clients", true},         // MCP clients: the computer's window only
		{"POST", "/api/clients", true},
		{"POST", "/api/instance/x", true},
		{"POST", "/api/distance", true},
		{"GET", "/api/distance", false},     // unchanged: a device reads the Remote page
		{"GET", "/api/projets", false},      // unchanged
		{"POST", "/api/preferences", false}, // unchanged
		// M174b (2.52.0): the working modes, field rapide included, as before from an unlocked device; ?defaut=1
		// as the project's creation (POST /api/projets)
		{"GET", "/api/mode?p=demo", false},
		{"POST", "/api/mode?p=demo", false},
		{"GET", "/api/mode?defaut=1", false},
		{"POST", "/api/projets", false},
	}
	for _, c := range cas {
		code, ok := appel(c.methode, c.chemin)
		if c.refus != (code == http.StatusForbidden) || c.refus == ok {
			t.Errorf("%s %s: %d (reached %v), refused expected %v", c.methode, c.chemin, code, ok, c.refus)
		}
	}
}
