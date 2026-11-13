package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDistantAppairage (M187): a hub (A) pairs with a remote Athanor (B) through B's tunnel, simulated by
// a local proxy in front of B's gestionDistance: a wrong code is refused, no token before B's owner says
// yes, the token goes to A's vault (infra) or waits in memory while it is locked, the remote is online and
// relays the bus to its MCP within the granted tools, the tunnel lets only an Athanor through, removal
// revokes the token on both sides, an Athanor of this PC paired by "Pair" is taken over; the token is in
// no API answer nor in the audit log.
func TestDistantAppairage(t *testing.T) {
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "appdata")) // the announcements: never the real folder
	a := serveurCoffreTest(t)
	for _, v := range []*time.Duration{&pauseRefusClient, &pauseStatutDistant} {
		ancien := *v
		*v = 10 * time.Millisecond
		t.Cleanup(func() { *v = ancien })
	}
	for _, f := range []string{cheminClients(), cheminDistants()} {
		_ = os.Remove(f)
		t.Cleanup(func() { _ = os.Remove(f) })
	}
	if err := a.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := a.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	b, hB, muxB := serveurDistance(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "abc-def.trycloudflare.com" // what Cloudflare hands to B's tunnel
		r.Header.Set("Cf-Connecting-Ip", "203.0.113.9")
		hB.ServeHTTP(w, r)
	}))
	defer proxy.Close()

	codeB := func() string {
		w := httptest.NewRecorder()
		b.apiClients(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"action":"code"}`)))
		var v struct{ Code string }
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if len(v.Code) != 6 {
			t.Fatalf("no code on B: %s", w.Body.String())
		}
		return v.Code
	}
	demandesB := func() []demandeClient { return b.vueClients()["demandes"].([]demandeClient) }
	attendre := func(quoi string, ok func() bool) {
		t.Helper()
		for fin := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(fin) {
				t.Fatal("timeout: " + quoi)
			}
		}
	}
	distants := func() []Distant {
		m := a.dst()
		m.mu.Lock()
		defer m.mu.Unlock()
		return slices.Clone(m.liste)
	}

	// addresses: https, or loopback http only
	for _, u := range []string{"http://exemple.com", "ftp://x.org", "https://u:p@x.com", "https://x.com/chemin", "x.com"} {
		if _, err := urlDistant(u); err == nil {
			t.Errorf("address %q accepted", u)
		}
	}
	if u, err := urlDistant(" https://ab-cd.trycloudflare.com/ "); err != nil || u != "https://ab-cd.trycloudflare.com" {
		t.Errorf("tunnel address = %q, %v", u, err)
	}

	// a wrong code: refused, nothing kept on either side
	codeB()
	if err := a.ajouterDistant(proxy.URL, "000000"); err == nil {
		t.Fatal("wrong code accepted")
	}
	if len(demandesB()) != 0 || len(distants()) != 0 {
		t.Fatal("a wrong code left a request")
	}

	// through the tunnel: Desktop or Code may not pair, no MCP call without an Athanor's token
	poster := func(chemin, corps, jeton string) int {
		r, _ := http.NewRequest("POST", proxy.URL+chemin, strings.NewReader(corps))
		if jeton != "" {
			r.Header.Set("Authorization", "Bearer "+jeton)
		}
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		rep.Body.Close()
		return rep.StatusCode
	}
	if c := poster("/api/mcp/appairer", `{"code":"`+codeB()+`","nom":"Claude","sorte":"desktop"}`, ""); c != 403 {
		t.Errorf("Desktop pairing through the tunnel: %d, want 403", c)
	}
	initMCP := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	if c := poster("/api/mcp", initMCP, ""); c != 401 {
		t.Errorf("MCP through the tunnel without a token: %d, want 401", c)
	}
	if c := poster("/api/mcp", initMCP, "faux-jeton"); c != 401 {
		t.Errorf("MCP through the tunnel with a wrong token: %d, want 401", c)
	}

	// the right code: a request on B, nothing on A before B's owner accepts
	if err := a.ajouterDistant(proxy.URL, codeB()); err != nil {
		t.Fatal(err)
	}
	d := demandesB()
	if len(d) != 1 || d[0].Sorte != "athanor" {
		t.Fatalf("B's requests = %+v", d)
	}
	time.Sleep(50 * time.Millisecond)
	if len(distants()) != 0 || len(a.vueDistants()["attentes"].([]map[string]any)) != 1 {
		t.Fatal("a remote kept before its owner's yes")
	}
	if err := b.deciderClient(d[0].Ref, true, []string{"*"}, []string{"etat", "projets"}); err != nil {
		t.Fatal(err)
	}
	attendre("the token", func() bool { return len(distants()) == 1 })
	dist := distants()[0]
	if dist.Coffre == "" {
		t.Fatal("token not in the vault")
	}
	jeton, err := a.coffre.Valeur(dist.Coffre)
	if err != nil || jeton == "" {
		t.Fatal("token unreadable in the vault:", err)
	}
	if e := a.coffre.Liste(); !slices.ContainsFunc(e, func(x EntreeCoffre) bool {
		return x.Nom == "athanor-distant-"+dist.ID && x.Portee == "infra"
	}) {
		t.Error("vault entry athanor-distant-<id> (infra) missing")
	}

	// online: its tools and modules; the bus relays a granted tool, refuses another
	a.rafraichirDistant(dist.ID)
	dist = distants()[0]
	if e := a.dst().etats[dist.ID]; e != "en_ligne" || !slices.Equal(dist.Outils, []string{"etat", "projets"}) || !slices.Contains(dist.Modules, "dev") {
		t.Fatalf("remote = %+v, state %q", dist, e)
	}
	vers := "distant-" + dist.ID
	rep := a.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "etat"})
	if rep.Etat != "ok" || !strings.Contains(rep.Donnees["texte"].(string), `"modules"`) {
		t.Errorf("bus etat = %+v", rep)
	}
	if rep = a.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "ajouter_demande", Projet: "x"}); rep.Etat != "refuse" {
		t.Errorf("tool not granted relayed: %+v", rep)
	}
	v, _ := a.vueHub()
	if i := slices.IndexFunc(v, func(m ModuleVue) bool { return m.ID == vers }); i < 0 || v[i].Resume.Stats[0].Valeur != "Online" {
		t.Errorf("no online tile for the remote: %+v", v)
	}

	// vault locked: the remote says so, the bus refuses
	a.coffre.Verrouiller()
	a.rafraichirDistant(dist.ID)
	if e := a.dst().etats[dist.ID]; e != "coffre" {
		t.Errorf("locked vault: state %q", e)
	}
	if rep = a.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "etat"}); rep.Etat != "refuse" {
		t.Errorf("bus with the vault locked: %+v", rep)
	}

	// paired while the vault is locked: the token waits in memory, then goes to the vault once it opens
	if err := a.ajouterDistant(proxy.URL, codeB()); err != nil {
		t.Fatal(err)
	}
	if err := b.deciderClient(demandesB()[0].Ref, true, []string{"*"}, []string{"etat"}); err != nil {
		t.Fatal(err)
	}
	attendre("the second token", func() bool { return len(distants()) == 2 })
	deux := distants()[1]
	if deux.Coffre != "" || a.vueDistants()["liste"].([]map[string]any)[1]["memoire"] != true {
		t.Fatal("token written while the vault is locked")
	}
	if err := a.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	a.rafraichirDistant(deux.ID)
	if deux = distants()[1]; deux.Coffre == "" || a.dst().etats[deux.ID] != "en_ligne" {
		t.Fatalf("token not moved to the vault once open: %+v", deux)
	}

	// an Athanor of this PC paired by Pair (M185): taken over once its owner accepts
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/annonce" {
			repondreAnnonce(func() string { return "bbbbbbbbbbbb" })(w, r)
			return
		}
		muxB.ServeHTTP(w, r)
	}))
	defer local.Close()
	port, _ := strconv.Atoi(local.URL[strings.LastIndex(local.URL, ":")+1:])
	_ = os.MkdirAll(dossierAnnonces(), 0o700)
	ann, _ := json.Marshal(Annonce{ID: "bbbbbbbbbbbb", Nom: "pc", Version: versionAthanor, Port: port, PID: os.Getpid()})
	if err := os.WriteFile(filepath.Join(dossierAnnonces(), "bbbbbbbbbbbb.json"), ann, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.appairerInstance(dossierAnnonces(), "bbbbbbbbbbbb", codeB()); err != nil {
		t.Fatal(err)
	}
	if err := b.deciderClient(demandesB()[0].Ref, true, []string{"*"}, []string{"etat"}); err != nil {
		t.Fatal(err)
	}
	attendre("the instance's token", func() bool { return len(distants()) == 3 })
	trois := distants()[2]
	a.rafraichirDistant(trois.ID)
	if trois.Instance != "bbbbbbbbbbbb" || trois.URL != "" || a.dst().etats[trois.ID] != "en_ligne" {
		t.Fatalf("instance of this PC = %+v, state %q", trois, a.dst().etats[trois.ID])
	}
	attendre("the instance's request closed", func() bool {
		demandesInstances.Lock()
		defer demandesInstances.Unlock()
		return demandesInstances.m["bbbbbbbbbbbb"] == ""
	})

	// refused by B's owner: said once in A's window
	if err := a.ajouterDistant(proxy.URL, codeB()); err != nil {
		t.Fatal(err)
	}
	if err := b.deciderClient(demandesB()[0].Ref, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	attendre("the refusal", func() bool { a.dst().mu.Lock(); defer a.dst().mu.Unlock(); return a.dst().message != "" })

	// the token: in no answer of A's API nor B's window, nor in the audit log
	w := httptest.NewRecorder()
	a.apiInstances(w, httptest.NewRequest("GET", "/", nil))
	vueB, _ := json.Marshal(b.vueClients())
	journal := ""
	_ = filepath.WalkDir(dossierJournalAudit(), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			c, _ := os.ReadFile(p)
			journal += string(c)
		}
		return nil
	})
	if !strings.Contains(journal, "appairage distant") {
		t.Error("pairing not in the audit log")
	}
	for quoi, texte := range map[string]string{"GET /api/instances": w.Body.String(), "B's window": string(vueB), "audit log": journal} {
		if strings.Contains(texte, jeton) {
			t.Errorf("the token appears in %s", quoi)
		}
	}

	// removal: revoked on B too, the vault entry gone; B unreachable: removed here all the same
	nB := len(b.cli().liste)
	if err := a.retirerDistant(dist.ID); err != nil {
		t.Fatal(err)
	}
	if len(b.cli().liste) != nB-1 {
		t.Error("token not revoked on the remote")
	}
	if _, err := a.coffre.Valeur(dist.Coffre); err == nil {
		t.Error("vault entry kept after removal")
	}
	if c := poster("/api/mcp", initMCP, jeton); c != 401 {
		t.Errorf("revoked token through the tunnel: %d, want 401", c)
	}
	proxy.Close()
	if err := a.retirerDistant(deux.ID); err != nil || len(distants()) != 1 {
		t.Fatalf("removal of an unreachable remote: %v, %d left", err, len(distants()))
	}
}
