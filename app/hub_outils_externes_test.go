package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMCPExterne (M183): an external client's token on /api/mcp sees only its tools and its projects; without a
// token or with a revoked one, 401 and no data; no tool decides an approval or launches a mission; the vault's
// witness value never comes back; every call goes to the audit log; the client token opens no other route.
func TestMCPExterne(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(cheminClients())
	t.Cleanup(func() { _ = os.Remove(cheminClients()) })
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	if !slices.Equal(slices.Sorted(maps.Keys(outilsExternes)), slices.Sorted(slices.Values(append(append([]string{}, outilsClients...), outilsAthanor...)))) {
		t.Fatal("the tools offered at pairing and the client tools differ")
	}

	racine := t.TempDir()
	demo, autre := Projet{Nom: "demo", Dossier: filepath.Join(racine, "demo")}, Projet{Nom: "autre", Dossier: filepath.Join(racine, "autre")}
	for _, p := range []Projet{demo, autre} {
		ecrireFichier(t, filepath.Join(p.Atelier(), "PLAN.md"), "# Plan\n\n- [x] M1 : Squelette de "+p.Nom+"\n- [ ] M2 : Page d'accueil de "+p.Nom+"\n", 0o644)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo, autre}
	s.mu.Unlock()
	temoin := "TEMOIN-" + aleatoire(10)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.acces.Ajouter("demo", e, "dev", "", ""); err != nil {
		t.Fatal(err)
	}
	var approbations []Approbation
	for _, p := range []string{"demo", "autre"} {
		a, err := s.approbations.Demander(Approbation{Projet: p, Env: "dev", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "STRIPE_SECRET_KEY"}, Raison: "paiement de " + p})
		if err != nil {
			t.Fatal(err)
		}
		approbations = append(approbations, a)
	}
	if err := enregistrerMission(Mission{ID: "missautre1", Phrase: "Mettre autre en ligne", Etat: "proposee", Politique: "prod",
		Mentions: Mentions{Projets: []string{"autre"}}, Cree: "2026-11-13T10:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	arrets := []string{}
	s.arretTest = func(p Projet) (string, error) { arrets = append(arrets, p.Nom); return "", nil }

	ajouter := func(nom string, projets, outils []string) string {
		j := aleatoire(32)
		m := s.cli()
		m.mu.Lock()
		m.liste = append(m.liste, ClientMCP{ID: aleatoire(8), Nom: nom, Sorte: "code", Jeton: hacherJeton(j), Outils: outils, Projets: projets})
		_ = m.enregistrer()
		m.mu.Unlock()
		return j
	}
	restreint := ajouter("Restreint", []string{"demo"}, []string{"etat", "projets", "plan", "demandes", "ajouter_demande", "pilote", "missions", "approbations"})
	limite := ajouter("Limite", []string{"*"}, []string{"plan"})
	complet := ajouter("Complet", []string{"*"}, outilsClients)

	var reponses strings.Builder
	rpc := func(jeton, methode, params string) (int, string) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+methode+`","params":`+params+`}`))
		if jeton != "" {
			r.Header.Set("Authorization", "Bearer "+jeton)
		}
		w := httptest.NewRecorder()
		s.apiMCP(w, r)
		reponses.WriteString(w.Body.String())
		return w.Code, w.Body.String()
	}
	outil := func(jeton, nom, args string) (string, bool) {
		t.Helper()
		c, b := rpc(jeton, "tools/call", `{"name":"`+nom+`","arguments":`+args+`}`)
		var v struct {
			Result struct {
				Content []struct{ Text string }
				IsError bool
			}
			Error *erreurMCP
		}
		if c != 200 || json.Unmarshal([]byte(b), &v) != nil {
			t.Fatalf("%s %s: %d %s", nom, args, c, b)
		}
		if v.Error != nil {
			return v.Error.Message, true
		}
		return v.Result.Content[0].Text, v.Result.IsError
	}
	noms := func(jeton string) []string {
		t.Helper()
		c, b := rpc(jeton, "tools/list", `{}`)
		var v struct {
			Result struct{ Tools []struct{ Name string } }
		}
		if c != 200 || json.Unmarshal([]byte(b), &v) != nil {
			t.Fatalf("tools/list: %d %s", c, b)
		}
		l := []string{}
		for _, x := range v.Result.Tools {
			l = append(l, x.Name)
		}
		return slices.Sorted(slices.Values(l))
	}

	// no token, a wrong one: 401 and nothing about the projects
	for _, j := range []string{"", "faux-" + aleatoire(20)} {
		c, b := rpc(j, "tools/list", `{}`)
		if c != 401 || strings.Contains(b, "demo") || strings.Contains(b, "plan") {
			t.Fatalf("token %q: %d %s", j, c, b)
		}
	}

	// tools/list shows the granted tools only
	if l := noms(limite); !slices.Equal(l, []string{"plan"}) {
		t.Fatal("tools of Limite:", l)
	}
	if l := noms(restreint); slices.Contains(l, "proposer_mission") || len(l) != 8 {
		t.Fatal("tools of Restreint:", l)
	}
	if txt, err := outil(limite, "projets", `{}`); !err || !strings.Contains(txt, "unknown tool") {
		t.Fatal("a tool outside the grant answered:", txt)
	}

	// the scope: demo yes, autre and an unknown project no (same answer)
	if txt, err := outil(restreint, "plan", `{"projet":"demo"}`); err || !strings.Contains(txt, "Page d'accueil de demo") {
		t.Fatal("plan demo:", txt)
	}
	for nom, plus := range map[string]string{"plan": "", "demandes": "", "ajouter_demande": `,"texte":"x"`, "pilote": `,"action":"arreter"`} {
		for _, p := range []string{"autre", "inconnu"} {
			txt, err := outil(restreint, nom, `{"projet":"`+p+`"`+plus+`}`)
			if !err || !strings.Contains(txt, "outside what this client may see") || strings.Contains(txt, "Page d'accueil") {
				t.Fatalf("%s %s outside the scope: %s", nom, p, txt)
			}
		}
	}
	if txt, _ := outil(restreint, "projets", `{}`); !strings.Contains(txt, `"demo"`) || strings.Contains(txt, `"autre"`) {
		t.Fatal("projets of Restreint:", txt)
	}
	if txt, err := outil(restreint, "ajouter_demande", `{"projet":"demo","texte":"Ajouter un formulaire de contact"}`); err || !strings.Contains(txt, "request") {
		t.Fatal("ajouter_demande:", txt)
	}
	if txt, _ := outil(restreint, "demandes", `{"projet":"demo"}`); !strings.Contains(txt, "formulaire de contact") {
		t.Fatal("demandes after ajouter_demande:", txt)
	}
	if txt, err := outil(restreint, "pilote", `{"projet":"demo","action":"pause_fin_cycle"}`); err || !slices.Equal(arrets, []string{"demo"}) {
		t.Fatal("pause at the end of the cycle:", txt, arrets)
	}
	if txt, err := outil(restreint, "pilote", `{"projet":"demo","action":"supprimer"}`); !err {
		t.Fatal("unknown pilot action accepted:", txt)
	}

	// approvals and missions: listed within the scope, never decided nor launched
	txt, _ := outil(restreint, "approbations", `{}`)
	if !strings.Contains(txt, approbations[0].ID) || strings.Contains(txt, approbations[1].ID) {
		t.Fatal("approbations of Restreint:", txt)
	}
	if txt, err := outil(complet, "approbations", `{"id":"`+approbations[0].ID+`","decision":"accepter"}`); !err {
		t.Fatal("approbations took a decision:", txt)
	}
	if txt, _ := outil(restreint, "missions", `{}`); strings.Contains(txt, "missautre1") {
		t.Fatal("a mission on another project shown:", txt)
	}
	if txt, err := outil(restreint, "missions", `{"id":"missautre1"}`); !err {
		t.Fatal("mission detail outside the scope:", txt)
	}
	if txt, err := outil(complet, "missions", `{"id":"missautre1"}`); err || !strings.Contains(txt, "Mettre autre en ligne") {
		t.Fatal("mission detail:", txt)
	}
	// M301: proposer_mission keeps its name for the installed kits, refuses and points to ajouter_demande; no mission is made
	avant := len(lireMissions())
	if txt, err := outil(complet, "proposer_mission", `{"phrase":"Mettre demo en ligne"}`); !err || !strings.Contains(txt, "ajouter_demande") {
		t.Fatal("proposer_mission is not refused with its replacement:", txt)
	}
	if n := len(lireMissions()); n != avant {
		t.Fatalf("proposer_mission made a mission: %d → %d", avant, n)
	}
	for _, a := range approbations {
		if x, _ := s.approbations.Lire(a.ID); x.Etat != "attente" {
			t.Fatalf("approval %s decided: %s", a.ID, x.Etat)
		}
	}
	if txt, err := outil(complet, "etat", `{}`); err || !strings.Contains(txt, "modules") {
		t.Fatal("etat:", txt)
	}
	if txt, _ := outil(restreint, "etat", `{}`); strings.Contains(txt, "paiement de autre") {
		t.Fatal("etat of Restreint shows another project's alert:", txt)
	}

	// the vault's value never comes back
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("the vault's witness value in an answer")
	}

	// the client token opens no other route
	mux := http.NewServeMux()
	s.routes(mux)
	for _, chemin := range []string{"/api/projets", "/api/hub", "/api/hub/approbations", "/api/coffre", "/api/clients", "/api/missions", "/api/audit"} {
		r := httptest.NewRequest("GET", chemin, nil)
		r.Header.Set("Authorization", "Bearer "+complet)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code == 200 || strings.Contains(w.Body.String(), "demo") {
			t.Fatalf("client token on %s: %d %s", chemin, w.Code, w.Body.String())
		}
	}

	// revoked or during the emergency stop: 401 without data
	s.arretUrgence("fenetre")
	if c, _ := rpc(complet, "tools/list", `{}`); c != 401 {
		t.Fatal("client during the emergency stop:", c)
	}
	s.reprendreApresUrgence("fenetre")
	var id string
	for _, c := range s.cli().liste {
		if c.Nom == "Restreint" {
			id = c.ID
		}
	}
	if err := s.revoquerClient(id); err != nil {
		t.Fatal(err)
	}
	if c, b := rpc(restreint, "tools/call", `{"name":"plan","arguments":{"projet":"demo"}}`); c != 401 || strings.Contains(b, "Page d'accueil") {
		t.Fatal("revoked client:", c, b)
	}

	// every call in the audit log, refusals included
	vu := map[string]int{}
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if strings.HasPrefix(l.De, "client:") && strings.HasPrefix(l.Action, "mcp ") {
			vu[l.De+" "+l.Action+" "+strings.SplitN(l.Resultat, " ", 2)[0]]++
		}
	}
	for _, attendu := range []string{"client:Restreint mcp plan ok", "client:Restreint mcp plan erreur", "client:Limite mcp projets refusé", "client:Complet mcp proposer_mission erreur", "client:Restreint mcp pilote ok"} {
		if vu[attendu] == 0 {
			t.Fatalf("audit lacks %q: %v", attendu, vu)
		}
	}
}
