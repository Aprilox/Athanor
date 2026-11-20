package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestClientsMCPAppairage (M182): the pairing of an external MCP client (5 wrong codes close the code, no
// token without the owner's yes, the token said once and stored hashed), refusal, the emergency stop that
// suspends clients, and revocation; each step in the audit log as client:<name>.
func TestClientsMCPAppairage(t *testing.T) {
	s := serveurCoffreTest(t)
	ancien := pauseRefusClient
	pauseRefusClient = 0
	t.Cleanup(func() { pauseRefusClient = ancien })
	p := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	s.config.Projets = []Projet{p}
	_ = os.Remove(cheminClients())
	t.Cleanup(func() { _ = os.Remove(cheminClients()) })

	appel := func(h http.HandlerFunc, methode, corps string, v any) int {
		t.Helper()
		r := httptest.NewRequest(methode, "/", strings.NewReader(corps))
		w := httptest.NewRecorder()
		h(w, r)
		if v != nil {
			_ = json.Unmarshal(w.Body.Bytes(), v)
		}
		return w.Code
	}
	type vue struct {
		Code     string
		Clients  []ClientMCP
		Demandes []demandeClient
		Erreur   string
	}
	fenetre := func(corps string) vue {
		t.Helper()
		var v vue
		methode := "POST"
		if corps == "" {
			methode = "GET"
		}
		if c := appel(s.apiClients, methode, corps, &v); c != 200 {
			t.Fatalf("window %s: %d %s", corps, c, v.Erreur)
		}
		return v
	}
	appairer := func(code string) (int, string) {
		var r struct{ Demande string }
		c := appel(s.apiAppairerClient, "POST", `{"code":"`+code+`","nom":"Claude Desktop","sorte":"desktop"}`, &r)
		return c, r.Demande
	}
	statut := func(dem string) (int, string) {
		var r struct {
			Jeton   string
			Attente bool
		}
		c := appel(s.apiStatutClient, "POST", `{"demande":"`+dem+`"}`, &r)
		return c, r.Jeton
	}

	// no code open: refused
	if c, _ := appairer("123456"); c != 403 {
		t.Fatal("pairing without an open code:", c)
	}
	// 5 wrong codes close the code: the right one is then refused
	code := fenetre(`{"action":"code"}`).Code
	if len(code) != 6 {
		t.Fatalf("code %q", code)
	}
	faux := "000000"
	if code == faux {
		faux = "111111"
	}
	for range 5 {
		if c, _ := appairer(faux); c != 403 {
			t.Fatal("wrong code accepted:", c)
		}
	}
	if c, _ := appairer(code); c != 403 {
		t.Fatal("the right code still works after 5 wrong ones:", c)
	}
	if fenetre("").Code != "" {
		t.Fatal("the window still shows the closed code")
	}

	// pairings that went through never add up: the 25th one from this PC still opens a request
	for i := range 25 {
		if c, _ := appairer(fenetre(`{"action":"code"}`).Code); c != 202 {
			t.Fatalf("pairing %d from this PC refused: %d", i+1, c)
		}
	}
	s.cli().mu.Lock()
	s.cli().demandes = map[string]*demandeClient{}
	s.cli().mu.Unlock()

	// a new code: the right one opens a request, no token before the owner's yes
	code = fenetre(`{"action":"code"}`).Code
	c, dem := appairer(code)
	if c != 202 || dem == "" {
		t.Fatal("right code:", c, dem)
	}
	if c, _ := appairer(code); c != 403 {
		t.Fatal("a code works twice:", c)
	}
	if c, j := statut(dem); c != 200 || j != "" {
		t.Fatal("token before the owner's yes:", c, j)
	}
	v := fenetre("")
	if len(v.Demandes) != 1 || v.Demandes[0].Nom != "Claude Desktop" || len(v.Clients) != 0 {
		t.Fatalf("window view: %+v", v)
	}
	ref := v.Demandes[0].Ref
	// a scope with an unknown tool or project is refused
	for _, corps := range []string{`{"action":"accepter","ref":"` + ref + `","projets":["demo"],"outils":["coffre_lire"]}`,
		`{"action":"accepter","ref":"` + ref + `","projets":["inconnu"],"outils":["etat"]}`,
		`{"action":"accepter","ref":"` + ref + `","projets":["demo"],"outils":[]}`} {
		if c := appel(s.apiClients, "POST", corps, nil); c != 400 {
			t.Fatalf("scope %s accepted: %d", corps, c)
		}
	}
	v = fenetre(`{"action":"accepter","ref":"` + ref + `","projets":["demo"],"outils":["plan","etat"]}`)
	if len(v.Clients) != 1 || v.Clients[0].Jeton != "" || strings.Join(v.Clients[0].Outils, ",") != "etat,plan" || v.Clients[0].Projets[0] != "demo" {
		t.Fatalf("client after the yes: %+v", v.Clients)
	}
	c, jeton := statut(dem)
	if c != 200 || len(jeton) < 32 {
		t.Fatal("token after the yes:", c, jeton)
	}
	if c, j := statut(dem); c != 404 || j != "" {
		t.Fatal("the token said twice:", c, j)
	}

	// stored hashed, 0600, never the token
	b, err := os.ReadFile(cheminClients())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), jeton) || !strings.Contains(string(b), hacherJeton(jeton)) {
		t.Fatal("clients.json does not hold the token's hash only")
	}
	if fi, _ := os.Stat(cheminClients()); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatal("clients.json mode", fi.Mode().Perm())
	}
	if x, ok := s.verifierClient(jeton); !ok || x.Nom != "Claude Desktop" {
		t.Fatal("the client's token is not recognised")
	}
	if _, ok := s.verifierJeton(jeton); ok {
		t.Fatal("a client token opens a session")
	}

	// a refused request: 403, no client added
	code = fenetre(`{"action":"code"}`).Code
	_, dem2 := appairer(code)
	ref2 := fenetre("").Demandes[0].Ref
	fenetre(`{"action":"refuser","ref":"` + ref2 + `"}`)
	if c, j := statut(dem2); c != 403 || j != "" {
		t.Fatal("refused request:", c, j)
	}
	if n := len(fenetre("").Clients); n != 1 {
		t.Fatal("clients after a refusal:", n)
	}

	// the emergency stop suspends the client (kept) and closes pairing; Resume brings it back
	s.arretUrgence("fenetre")
	if _, ok := s.verifierClient(jeton); ok {
		t.Fatal("client token accepted during the emergency stop")
	}
	if c := appel(s.apiClients, "POST", `{"action":"code"}`, nil); c != 400 {
		t.Fatal("a code opened during the emergency stop:", c)
	}
	if c, _ := appairer("123456"); c != 403 {
		t.Fatal("pairing during the emergency stop:", c)
	}
	s.reprendreApresUrgence("fenetre")
	if _, ok := s.verifierClient(jeton); !ok {
		t.Fatal("client lost after Resume")
	}

	// revocation works at once and leaves the file
	id := fenetre("").Clients[0].ID
	if v := fenetre(`{"action":"revoquer","id":"` + id + `"}`); len(v.Clients) != 0 {
		t.Fatal("client still listed after revocation")
	}
	if _, ok := s.verifierClient(jeton); ok {
		t.Fatal("revoked token still accepted")
	}
	if b, _ := os.ReadFile(cheminClients()); strings.Contains(string(b), hacherJeton(jeton)) {
		t.Fatal("revoked client still in clients.json")
	}

	// the audit log names the client
	var vu []string
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if l.De == "client:Claude Desktop" {
			vu = append(vu, l.Action+" → "+l.Resultat)
		}
	}
	tout := strings.Join(vu, "\n")
	for _, attendu := range []string{"5 essais : code fermé", "en attente de l'accord", "accepté : projets demo ; outils etat,plan", "refusé à l'écran", "mcp révocation → jeton révoqué"} {
		if !strings.Contains(tout, attendu) {
			t.Errorf("audit lacks %q:\n%s", attendu, tout)
		}
	}
}
