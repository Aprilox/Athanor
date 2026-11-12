package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// moduleServeursTest: a fake Servers module on the bus; it counts what it really did.
type moduleServeursTest struct {
	moduleFactice
	faits *int
}

func (moduleServeursTest) Actions() []string { return []string{"deployer", "detruire"} }
func (moduleServeursTest) Risque(a string) string {
	if a == "detruire" {
		return "destructif"
	}
	return ""
}
func (m moduleServeursTest) Recevoir(_ *Serveur, x MessageHub) (MessageHub, error) {
	*m.faits++
	return MessageHub{Donnees: map[string]any{"deploye": x.Donnees["commit"]}}, nil
}

// TestBusAccordI6 (C28): a deployment in prod waits for the owner on the bus, before the module acts;
// asked again, the same approval; accepted, that very request goes through once.
func TestBusAccordI6(t *testing.T) {
	s := serveurBusTest(t)
	faits := 0
	modulesMu.Lock() // the real Servers module gives way to the fake one (serveurBusTest restores the list)
	modules = slices.DeleteFunc(modules, func(m Module) bool { return m.ID() == "servers" })
	modulesMu.Unlock()
	enregistrerModule(moduleServeursTest{moduleFactice{id: "servers", ordre: 60}, &faits})
	if _, err := s.acces.Ajouter("demo", EntreeCoffre{ID: "e1", Nom: "Stripe"}, "prod", "STRIPE_KEY", ""); err != nil {
		t.Fatal(err)
	}
	x := SessionHub{Projet: "demo", Agent: "pilote"}
	deployer := func(env, commit string) string {
		t.Helper()
		rep := s.envoyerBus(MessageHub{De: "dev", Vers: "servers", Action: "deployer", Projet: x.Projet, Env: env, Agent: x.Agent,
			Donnees: map[string]any{"commit": commit}})
		switch rep.Etat {
		case "ok":
			return enJSON(rep.Donnees)
		case "attente":
			return rep.Raison + ":"
		}
		t.Fatalf("%s %s: %+v", env, commit, rep)
		return ""
	}
	idDe := func(txt string) string {
		t.Helper()
		_, apres, ok := strings.Cut(txt, "waiting for the owner, id ")
		if !ok {
			t.Fatalf("not waiting: %s", txt)
		}
		return apres[:strings.Index(apres, ":")]
	}
	if txt := deployer("dev", "abc"); faits != 1 || !strings.Contains(txt, "abc") {
		t.Fatalf("dev without accord: %d %s", faits, txt)
	}
	id := idDe(deployer("prod", "abc"))
	if faits != 1 || s.approbations.PourAgent(id) != "attente" {
		t.Fatalf("prod went through without accord: %d", faits)
	}
	if id2 := idDe(deployer("prod", "abc")); id2 != id || faits != 1 {
		t.Errorf("asked again: %s, want %s", id2, id)
	}
	autre := idDe(deployer("prod", "def"))
	if autre == id {
		t.Error("another commit reuses the approval")
	}
	if _, err := s.approbations.Decider(id, "accepter", "fenetre"); err != nil {
		t.Fatal(err)
	}
	if idDe(deployer("prod", "def")) != autre || faits != 1 {
		t.Error("the accord for abc let def through")
	}
	if txt := deployer("prod", "abc"); faits != 2 || !strings.Contains(txt, "abc") {
		t.Fatalf("accepted request refused: %d %s", faits, txt)
	}
	if idDe(deployer("prod", "abc")) == id || faits != 2 {
		t.Error("one accord let two deployments through")
	}
	if rep := s.envoyerBus(MessageHub{De: "dev", Vers: "servers", Action: "detruire", Projet: "demo", Env: "dev"}); rep.Etat != "attente" || faits != 2 {
		t.Errorf("destructive in dev: %+v", rep)
	}
	if rep := s.envoyerBus(MessageHub{De: "fuite", Vers: "dev", Action: "etat_projet", Projet: "demo", Env: "prod"}); rep.Etat != "ok" {
		t.Errorf("a read in prod waits: %+v", rep)
	}
	a := auditTexte(t)
	for _, attendu := range []string{"approbation demandée: deployer", "approbation utilisée: deployer", "attente: waiting for the owner"} {
		if !strings.Contains(a, attendu) {
			t.Errorf("audit log without %q", attendu)
		}
	}
}

// TestApprobationsBornees (C29): pending approvals capped per project, one phone ping per project and
// window, decided ones purged after 30 days.
func TestApprobationsBornees(t *testing.T) {
	d := t.TempDir()
	a, err := ouvrirApprobations(d)
	if err != nil {
		t.Fatal(err)
	}
	h := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	a.Maintenant = func() time.Time { return h }
	var premiere Approbation
	for i := range maxAttenteProjet {
		x, err := a.Demander(Approbation{Projet: "demo", Env: "prod", Action: "deployer", Details: map[string]string{"commit": fmt.Sprint(i)}})
		if err != nil {
			t.Fatal(i, err)
		}
		if i == 0 {
			premiere = x
		}
	}
	if _, err := a.Demander(Approbation{Projet: "demo", Env: "prod", Action: "deployer"}); !errors.Is(err, errTropDemandes) {
		t.Errorf("request beyond the cap: %v", err)
	}
	if _, err := a.Demander(Approbation{Projet: "autre", Env: "prod", Action: "deployer"}); err != nil {
		t.Errorf("another project blocked: %v", err)
	}
	if !a.ANotifier("demo") || a.ANotifier("demo") || !a.ANotifier("autre") {
		t.Error("notifications not one per project")
	}
	h = h.Add(delaiNotification)
	if !a.ANotifier("demo") {
		t.Error("no notification after the window")
	}
	if _, err := a.Decider(premiere.ID, "refuser", "fenetre"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Demander(Approbation{Projet: "demo", Env: "prod", Action: "deployer"}); err != nil {
		t.Errorf("a decision frees a place: %v", err)
	}
	h = h.Add(gardeApprobations + time.Hour) // the refused one is purged, the others just expired
	if l, _ := a.Liste(); len(l) != maxAttenteProjet+1 || l[0].Etat != "expiree" {
		t.Fatalf("%d approvals after 30 days, want %d", len(l), maxAttenteProjet+1)
	}
	h = h.Add(gardeApprobations + time.Hour)
	if l, _ := a.Liste(); len(l) != 0 {
		t.Errorf("%d approvals not purged", len(l))
	}
	if b, _ := ouvrirApprobations(d); len(b.liste) != 0 {
		t.Errorf("purge not written: %d", len(b.liste))
	}
}

// TestMCPRefusResumes (C30): refusals without a token summed up once a minute; a huge tool name cut.
func TestMCPRefusResumes(t *testing.T) {
	s, mux, _ := serveurMCPTest(t)
	for range 5 {
		if rep := appelMCP(t, mux, "Bearer faux", appelOutil("etat", "{}")); rep.Code != 401 {
			t.Fatalf("bad token: %d", rep.Code)
		}
	}
	refus := func() []string {
		var r []string
		for _, l := range lignesAudit(t, dossierJournalAudit()) {
			if l.Action == "mcp" && l.De == "inconnu" {
				r = append(r, l.Resultat)
			}
		}
		return r
	}
	if r := refus(); len(r) != 1 || r[0] != "jeton refusé" {
		t.Fatalf("5 refusals: %q", r)
	}
	refusMCP.Lock()
	refusMCP.derniere = refusMCP.derniere.Add(-time.Minute)
	refusMCP.Unlock()
	appelMCP(t, mux, "", appelOutil("etat", "{}"))
	if r := refus(); len(r) != 2 || !strings.Contains(r[1], "(5 fois") {
		t.Fatalf("after a minute: %q", r)
	}
	jeton, err := s.jetons.Creer("demo", []string{"t_a"})
	if err != nil {
		t.Fatal(err)
	}
	rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(strings.Repeat("x", 100000), "{}"))
	if rep.Error == nil || len(rep.Error.Message) > 100 {
		t.Fatalf("huge tool name: %d", len(rep.Error.Message))
	}
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if len(l.Action) > 80 {
			t.Errorf("audit action of %d bytes", len(l.Action))
		}
	}
}

// TestDemanderAccesNom (C31): the name of a requested key is a variable name, never free text.
func TestDemanderAccesNom(t *testing.T) {
	s := serveurBusTest(t)
	x := SessionHub{Projet: "demo", Agent: "pilote"}
	for _, nom := range []string{"PATH", "sk_live a b", "LD_PRELOAD", "x;rm", "1ABC"} {
		args, _ := json.Marshal(map[string]string{"nom": nom, "raison": "test"})
		if _, err := outilDemanderAcces(s, x, args); err == nil {
			t.Errorf("name %q accepted", nom)
		}
	}
	if l, _ := s.approbations.Liste(); len(l) != 0 {
		t.Errorf("%d approvals recorded", len(l))
	}
}
