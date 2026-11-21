package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestApprobationsMagasin: creation, 24 h expiry (refused for the agent), decisions, rules, files.
func TestApprobationsMagasin(t *testing.T) {
	d := t.TempDir()
	h := &horlogeJeton{time.Now()}
	a, err := ouvrirApprobations(d)
	if err != nil {
		t.Fatal(err)
	}
	a.Maintenant = h.maintenant
	x, err := a.Demander(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer",
		Raison: strings.Repeat("é", 600), Details: map[string]string{"commit": "abc"}})
	if err != nil || x.ID == "" || x.Etat != "attente" || len([]rune(x.Raison)) != 501 {
		t.Fatalf("created: %+v %v", x, err)
	}
	if exp, _ := time.Parse(time.RFC3339, x.Expire); exp.Sub(h.t.UTC().Truncate(time.Second)) != dureeApprobation {
		t.Errorf("expiry %s, created %s", x.Expire, x.Cree)
	}
	if e := a.PourAgent(x.ID); e != "attente" {
		t.Errorf("agent sees %q", e)
	}
	if e := a.PourAgent("inconnue"); e != "refusee" {
		t.Errorf("unknown id seen as %q", e)
	}
	if _, err := a.Decider(x.ID, "peut-etre", "fenetre"); !errors.Is(err, errApprobationDecision) {
		t.Errorf("bad decision: %v", err)
	}
	if _, err := a.Decider("inconnue", "accepter", "fenetre"); !errors.Is(err, errApprobationInconnue) {
		t.Errorf("unknown approval: %v", err)
	}
	h.t = h.t.Add(dureeApprobation)
	if l, _ := a.Liste(); len(l) != 1 || l[0].Etat != "expiree" {
		t.Fatalf("after 24 h: %+v", l)
	}
	if e := a.PourAgent(x.ID); e != "refusee" {
		t.Errorf("expired approval seen as %q by the agent", e)
	}
	if _, err := a.Decider(x.ID, "accepter", "fenetre"); !errors.Is(err, errApprobationDecidee) {
		t.Errorf("an expired approval was accepted: %v", err)
	}

	y, _ := a.Demander(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer"})
	z, _ := a.Demander(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "vider_base"})
	if y, err = a.Decider(y.ID, "toujours", "appareil:tel"); err != nil || y.Etat != "acceptee" || y.Par != "appareil:tel" {
		t.Fatalf("toujours: %+v %v", y, err)
	}
	if _, err := a.Decider(y.ID, "refuser", "fenetre"); !errors.Is(err, errApprobationDecidee) {
		t.Errorf("decided twice: %v", err)
	}
	if z, _ = a.Decider(z.ID, "refuser", "fenetre"); z.Etat != "refusee" || a.PourAgent(z.ID) != "refusee" {
		t.Errorf("refused: %+v", z)
	}
	if !a.Regle(RegleAccord{Projet: "demo", Env: "prod", Action: "deployer"}) || a.Regle(RegleAccord{Projet: "demo", Env: "staging", Action: "deployer"}) || a.Regle(RegleAccord{Projet: "autre", Env: "prod", Action: "deployer"}) || a.Regle(RegleAccord{Projet: "demo", Env: "dev", Action: "vider_base"}) {
		t.Error("rules wrong")
	}

	b, err := ouvrirApprobations(d) // read back from the files
	if err != nil {
		t.Fatal(err)
	}
	l, regles := b.Liste()
	if len(l) != 3 || len(regles) != 1 || l[0].ID != z.ID && l[0].ID != y.ID || b.PourAgent(y.ID) != "acceptee" {
		t.Errorf("read back: %+v %+v", l, regles)
	}
	if runtime.GOOS != "windows" {
		for _, f := range []string{"approbations.json", "regles.json"} {
			if st, err := os.Stat(filepath.Join(d, f)); err != nil || st.Mode().Perm() != 0o600 {
				t.Errorf("%s: %v %v", f, st.Mode(), err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(d, "regles.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := ouvrirApprobations(d); err == nil || c.Regle(RegleAccord{Projet: "demo", Env: "prod", Action: "deployer"}) {
		t.Error("unreadable rules file accepted")
	}
}

// TestExigerAccordI6: prod or destructive waits for the owner; only an owner's "toujours" lifts it, for
// its project, environment and action; no MCP tool can decide or set a rule.
func TestExigerAccordI6(t *testing.T) {
	s := serveurCoffreTest(t)
	cas := []struct {
		env, action string
		destructif  bool
		attend      bool
	}{{"dev", "deployer", false, false}, {"staging", "deployer", false, false}, {"prod", "deployer", false, true},
		{"dev", "vider_base", true, true}, {"staging", "supprimer", true, true}}
	for _, c := range cas {
		x, attend, err := s.exigerAccord(Approbation{Projet: "demo", Env: c.env, Agent: "pilote", Action: c.action}, c.destructif)
		if err != nil || attend != c.attend || attend && (x.ID == "" || s.approbations.PourAgent(x.ID) != "attente") {
			t.Errorf("%+v: waits %v, %+v, %v", c, attend, x, err)
		}
	}
	l, _ := s.approbations.Liste()
	if len(l) != 3 {
		t.Fatalf("%d approvals, want 3", len(l))
	}
	var prod, vider Approbation
	for _, x := range l {
		switch x.Env {
		case "prod":
			prod = x
		case "dev":
			vider = x
		}
	}
	if _, err := s.approbations.Decider(vider.ID, "toujours", "fenetre"); err != nil {
		t.Fatal(err)
	}
	if _, attend, _ := s.exigerAccord(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "vider_base"}, true); attend {
		t.Error("the owner's rule did not lift the wait")
	}
	for _, x := range []Approbation{{Projet: "autre", Env: "dev", Action: "vider_base"}, {Projet: "demo", Env: "dev", Action: "rollback"}} {
		if _, attend, _ := s.exigerAccord(x, true); !attend {
			t.Errorf("rule applied beyond its project and action: %+v", x)
		}
	}
	// M247: under stronger caution no rule lifts the wait (and the decision route refuses "toujours")
	if _, err := s.approbations.Decider(prod.ID, "toujours", "fenetre"); err != nil {
		t.Fatal(err)
	}
	if _, attend, _ := s.exigerAccord(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer"}, false); !attend {
		t.Error("a rule lifted a prod action")
	}
	// B52: Ask again removes only that rule (from a paired device too), then the wait is back; no environment named
	regle := map[string]string{"decision": "redemander", "projet": "demo", "action": "vider_base"}
	if w := appelApprobation(t, s, "tel", regle); w.Code != 200 {
		t.Fatalf("ask again: %d %s", w.Code, w.Body)
	}
	if w := appelApprobation(t, s, "", regle); w.Code != http.StatusNotFound {
		t.Errorf("rule removed twice: %d", w.Code)
	}
	if _, attend, _ := s.exigerAccord(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "vider_base"}, true); !attend {
		t.Error("the rule still applies after Ask again")
	}
	if !strings.Contains(auditTexte(t), "règle retirée: vider_base") {
		t.Error("rule removal not in the audit log")
	}
	if !strings.Contains(auditTexte(t), "approbation demandée: deployer") {
		t.Error("request not in the audit log")
	}
	// only the owner's decision route decides or sets a rule: never a tool of the hub's MCP
	fichiers, _ := filepath.Glob("*.go")
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") || f == "hub_approbations.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		if regexp.MustCompile(`\.Decider(Avec)?\(|approbations\.regles\b|\bregles\.json`).Match(b) {
			t.Errorf("%s decides an approval or touches the rules", f)
		}
	}
}

// appelApprobation: POST to the decision handler (garde skipped) from the window or a paired device.
func appelApprobation(t *testing.T, s *Serveur, appareil string, c map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	if _, ok := c["mot_de_passe"]; !ok && appareil == "" {
		c["mot_de_passe"] = mdpPreuveTest // avecMdpPreuve
	}
	b, _ := json.Marshal(c)
	return appelCoffre(t, s.apiApprobationDecider, appareil, string(b), nil)
}

func natif(t *testing.T, oui bool) {
	ancien := modeNatif.Load()
	modeNatif.Store(oui)
	t.Cleanup(func() { modeNatif.Store(ancien) })
}

// TestApprobationAcces: accepting a demander_acces grants the code entry and rewrites secrets.env at once
// (no pilot restart); refusals leave it pending; every decision in the audit log, never the value.
func TestApprobationAcces(t *testing.T) {
	s := serveurCoffreTest(t)
	natif(t, false) // after nouveauServeur, which sets it from the config
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	demo := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(demo.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.config.Projets = []Projet{demo}
	temoin := "temoin-appr-" + aleatoire(6)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Région", Famille: "variable", Type: "autre"}, "eu")
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.demanderApprobation(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces",
		Details: map[string]string{"nom": "STRIPE_SECRET_KEY"}, Raison: "paiements de test"})
	if err != nil {
		t.Fatal(err)
	}
	// C24: WSL mode too, nothing is accepted before a password exists
	if w := appelApprobation(t, s, "", map[string]string{"id": x.ID, "decision": "accepter", "entree": e.ID}); w.Code != http.StatusForbidden {
		t.Fatalf("accepted without a password set: %d %s", w.Code, w.Body)
	}
	avecMdpPreuve(s)
	for _, c := range []map[string]string{{"id": x.ID, "decision": "accepter"}, {"id": x.ID, "decision": "accepter", "entree": v.ID},
		{"id": x.ID, "decision": "accepter", "entree": "inconnue"}, {"id": x.ID, "decision": "oui", "entree": e.ID}} {
		if w := appelApprobation(t, s, "", c); w.Code < 400 {
			t.Errorf("%v accepted: %d %s", c, w.Code, w.Body)
		}
	}
	if s.approbations.PourAgent(x.ID) != "attente" {
		t.Fatal("a refused decision changed the approval")
	}
	w := appelApprobation(t, s, "", map[string]string{"id": x.ID, "decision": "accepter", "entree": e.ID})
	if w.Code != 200 || s.approbations.PourAgent(x.ID) != "acceptee" {
		t.Fatalf("accept: %d %s", w.Code, w.Body)
	}
	if w := appelApprobation(t, s, "", map[string]string{"id": x.ID, "decision": "refuser"}); w.Code != http.StatusConflict {
		t.Errorf("decided twice: %d", w.Code)
	}
	if g := s.acces.Projet("demo").Acces; len(g) != 1 || g[0].Entree != e.ID || g[0].Variable != "STRIPE_SECRET_KEY" || g[0].Env != "dev" {
		t.Errorf("grant: %+v", g)
	}
	b, _ := os.ReadFile(filepath.Join(demo.Atelier(), "secrets.env"))
	if !strings.Contains(string(b), "STRIPE_SECRET_KEY='"+temoin+"'") {
		t.Errorf("secrets.env without the value: %q", b)
	}
	if strings.Contains(w.Body.String(), temoin) {
		t.Error("value in the answer")
	}
	// a refusal: nothing granted
	y, _ := s.demanderApprobation(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "AUTRE_KEY"}})
	if w := appelApprobation(t, s, "", map[string]string{"id": y.ID, "decision": "refuser", "entree": e.ID}); w.Code != 200 || s.approbations.PourAgent(y.ID) != "refusee" {
		t.Errorf("refuse: %d %s", w.Code, w.Body)
	}
	if len(s.acces.Projet("demo").Acces) != 1 {
		t.Error("a refusal granted an access")
	}
	j := auditTexte(t)
	for _, m := range []string{"approbation accepter", "acces_accorde", "approbation refuser", "décision refusée: oui"} {
		if !strings.Contains(j, m) {
			t.Errorf("%q not in the audit log", m)
		}
	}
	if strings.Contains(j, temoin) {
		t.Error("value in the audit log")
	}
	// GET for the page
	r := httptest.NewRequest("GET", "/", nil)
	g := httptest.NewRecorder()
	s.apiApprobations(g, r)
	var l struct {
		Approbations []Approbation `json:"approbations"`
		Requis       bool          `json:"appareil_requis"`
	}
	if json.Unmarshal(g.Body.Bytes(), &l) != nil || len(l.Approbations) != 2 || l.Requis {
		t.Errorf("list: %s", g.Body)
	}
}

// TestApprobationNatif: in native Windows mode the window decides only with Athanor's password retyped;
// without a password, only a paired device decides.
func TestApprobationNatif(t *testing.T) {
	s := serveurCoffreTest(t)
	natif(t, true) // after nouveauServeur, which sets it from the config
	nouvelle := func() string {
		x, err := s.demanderApprobation(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer"})
		if err != nil {
			t.Fatal(err)
		}
		return x.ID
	}
	a := nouvelle()
	if w := appelApprobation(t, s, "", map[string]string{"id": a, "decision": "accepter"}); w.Code != http.StatusForbidden {
		t.Errorf("window without a password: %d", w.Code)
	}
	if w := appelApprobation(t, s, "tel", map[string]string{"id": a, "decision": "accepter"}); w.Code != http.StatusForbidden {
		t.Errorf("paired device without a password (it may be the agent's own): %d %s", w.Code, w.Body)
	}
	s.config.MotDePasseHash = hacherMotDePasse("le-bon")
	if w := appelApprobation(t, s, "tel", map[string]string{"id": a, "decision": "accepter"}); w.Code != 200 {
		t.Errorf("paired device, unlocked with the password: %d %s", w.Code, w.Body)
	}
	x, _ := s.approbations.Lire(a)
	if x.Par != "appareil:tel" {
		t.Errorf("decided by %q", x.Par)
	}
	s.config.MotDePasseHash = hacherMotDePasse("le-bon")
	b := nouvelle()
	for _, m := range []string{"", "le-mauvais"} {
		if w := appelApprobation(t, s, "", map[string]string{"id": b, "decision": "accepter", "mot_de_passe": m}); w.Code != http.StatusUnauthorized {
			t.Errorf("window, password %q: %d", m, w.Code)
		}
	}
	if s.approbations.PourAgent(b) != "attente" {
		t.Fatal("decided without the password")
	}
	if w := appelApprobation(t, s, "", map[string]string{"id": b, "decision": "refuser", "mot_de_passe": "le-bon"}); w.Code != 200 {
		t.Errorf("window with the password: %d %s", w.Code, w.Body)
	}
	g := httptest.NewRecorder()
	s.apiApprobations(g, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(g.Body.String(), `"mot_de_passe_requis":true`) {
		t.Errorf("the page is not told to ask the password: %s", g.Body)
	}
	if !strings.Contains(auditTexte(t), "Wrong password") {
		t.Error("refused decisions not in the audit log")
	}
}

// TestApprobationDisqueEntier: in native mode, a session that reads the whole disk (instance.json, then a
// window cookie and the page's CSRF through /api/instance/terminal) cannot accept its own request.
func TestApprobationDisqueEntier(t *testing.T) {
	s := serveurCoffreTest(t)
	natif(t, true) // after nouveauServeur, which sets it from the config
	mux := http.NewServeMux()
	s.routes(mux)
	s.routesInterface(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ancien := lienApp
	lienApp = func() string { return s.lienOuverture(srv.URL + "/") }
	t.Cleanup(func() { lienApp = ancien })
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	ecrireInstance(port)
	t.Cleanup(effacerInstance)
	x, err := s.demanderApprobation(Approbation{Projet: "demo", Env: "prod", Agent: "pilote", Action: "deployer"})
	if err != nil {
		t.Fatal(err)
	}

	agent := func(motDePasse string) int {
		lien, existe := lienInstanceTerminal() // what athanor tui does: instance.json read from the disk
		if !existe || lien == "" {
			t.Fatal("no window link through instance.json")
		}
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar}
		rep, err := c.Get(lien)
		if err != nil {
			t.Fatal(err)
		}
		page, _ := io.ReadAll(rep.Body)
		rep.Body.Close()
		m := regexp.MustCompile(`<meta name="csrf" content="([^"]+)">`).FindSubmatch(page)
		if m == nil {
			t.Fatalf("no window page: %d", rep.StatusCode)
		}
		corps, _ := json.Marshal(map[string]string{"id": x.ID, "decision": "toujours", "mot_de_passe": motDePasse})
		req, _ := http.NewRequest("POST", srv.URL+"/api/hub/approbations", strings.NewReader(string(corps)))
		req.Header.Set("X-Athanor", string(m[1]))
		rep, err = c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		rep.Body.Close()
		return rep.StatusCode
	}
	if code := agent(""); code != http.StatusForbidden {
		t.Errorf("no Athanor password: the agent's decision got %d", code)
	}
	s.config.MotDePasseHash = hacherMotDePasse("le-bon")
	s.session = "session-de-la-vraie-fenetre" // the owner's window is unlocked: the agent has no session cookie
	for _, m := range []string{"", "devine"} {
		if code := agent(m); code < 400 {
			t.Errorf("password %q: the agent's decision got %d", m, code)
		}
	}
	if s.approbations.PourAgent(x.ID) != "attente" || s.approbations.Regle(RegleAccord{Projet: "demo", Env: "prod", Action: "deployer"}) {
		t.Error("the agent accepted its own request")
	}
}

// TestAccordsMissionRefuses (M301): missions are no longer launched; at Athanor's start a pending launch request is
// refused with its reason in the audit log, and nothing else is decided.
func TestAccordsMissionRefuses(t *testing.T) {
	s := serveurCoffreTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	demander := func(x Approbation) Approbation {
		t.Helper()
		y, err := s.approbations.Demander(x)
		if err != nil {
			t.Fatal(err)
		}
		return y
	}
	lancer := demander(Approbation{Projet: prefixeMission + "vieille1", Mission: "vieille1", Agent: "orchestrateur", Action: actionMission, Raison: "Launch the mission"})
	acces := demander(Approbation{Projet: "demo", Env: "dev", Agent: "pilote", Action: "demander_acces", Raison: "essai"})
	s.refuserLancementsMission()
	if e, f := s.approbations.PourAgent(lancer.ID), s.approbations.PourAgent(acces.ID); e != "refusee" || f != "attente" {
		t.Fatalf("the old launch is %q (want refusee), the access %q (want attente)", e, f)
	}
	l := lignesAudit(t, dossierJournalAudit())
	if len(l) != 1 || l[0].Mission != "vieille1" || l[0].Action != "approbation refuser" || !strings.Contains(l[0].Resultat, "les missions ne se lancent plus") {
		t.Fatalf("audit: %+v", l)
	}
	if _, _, err := s.deciderApprobation("fenetre", lancer.ID, "accepter", "", "", ""); err == nil { // a refused request is never accepted afterwards
		t.Fatal("the refused launch was accepted")
	}
}
