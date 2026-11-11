package main

import (
	"path/filepath"
	"testing"
)

// moduleFuite: a dummy module on the bus that answers whatever path it is given (the I4 check).
type moduleFuite struct{ moduleFactice }

func (moduleFuite) Actions() []string { return []string{"chemin"} }
func (moduleFuite) Recevoir(_ *Serveur, m MessageHub) (MessageHub, error) {
	return MessageHub{Donnees: map[string]any{"liste": []any{map[string]any{"p": m.Donnees["p"]}}}}, nil
}

func serveurBusTest(t *testing.T) *Serveur {
	t.Helper()
	s := serveurCoffreTest(t)
	if s.acces == nil || s.accesErr != nil {
		t.Fatal("access not loaded:", s.accesErr)
	}
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}}
	modulesMu.Lock()
	avant := append([]Module{}, modules...)
	modulesMu.Unlock()
	t.Cleanup(func() { modulesMu.Lock(); modules = avant; modulesMu.Unlock() })
	enregistrerModule(moduleFuite{moduleFactice{id: "fuite", ordre: 50}})
	return s
}

// TestBusDev: the Dev module answers etat_projet; request and answer go to the audit log with the same ID.
func TestBusDev(t *testing.T) {
	s := serveurBusTest(t)
	rep := s.envoyerBus(MessageHub{De: "fuite", Vers: "dev", Action: "etat_projet", Projet: "demo", Env: "dev", Agent: "pilote"})
	if rep.Etat != "ok" || rep.Type != "reponse" || rep.De != "dev" || rep.Vers != "fuite" || rep.Donnees["nom"] != "demo" || rep.ID == "" {
		t.Fatalf("answer: %+v", rep)
	}
	if cheminInterdit(rep.Donnees, "fuite") {
		t.Fatalf("the Dev answer names a forbidden path: %+v", rep.Donnees)
	}
	l := lignesAudit(t, dossierJournalAudit())
	if len(l) != 2 || l[0].ID != rep.ID || l[1].ID != rep.ID || l[0].Type != "demande" || l[1].Type != "reponse" ||
		l[0].Env != "dev" || l[0].Agent != "pilote" || l[1].Resultat != "ok" || l[0].De != "fuite" || l[1].De != "dev" {
		t.Fatalf("audit: %+v", l)
	}
	if v, err := s.verifierAudit(); err != nil || !v.OK {
		t.Fatalf("check: %+v %v", v, err)
	}
}

// TestBusRefus: unknown sender or recipient, undeclared action, unknown project, environment the
// project is not linked to: each refused, logged, and the answer carries no data.
func TestBusRefus(t *testing.T) {
	s := serveurBusTest(t)
	ok := MessageHub{De: "fuite", Vers: "dev", Action: "etat_projet", Projet: "demo", Env: "dev"}
	cas := []struct {
		nom    string
		change func(*MessageHub)
		raison string
	}{
		{"servers absent", func(m *MessageHub) { m.Vers = "servers" }, "module absent"},
		{"unknown sender", func(m *MessageHub) { m.De = "inconnu" }, "unknown sender"},
		{"undeclared action", func(m *MessageHub) { m.Action = "supprimer" }, "action not declared"},
		{"module without bus", func(m *MessageHub) { m.Vers = "hub" }, "action not declared"},
		{"unknown project", func(m *MessageHub) { m.Projet = "autre" }, "unknown project"},
		{"no project", func(m *MessageHub) { m.Projet = "" }, "unknown project"},
		{"env not linked", func(m *MessageHub) { m.Env = "prod" }, "no access"},
		{"no env", func(m *MessageHub) { m.Env = "" }, "no access"},
	}
	for _, c := range cas {
		m := ok
		c.change(&m)
		if rep := s.envoyerBus(m); rep.Etat != "refuse" || rep.Raison != c.raison || rep.Donnees != nil {
			t.Errorf("%s: %+v", c.nom, rep)
		}
	}
	l := lignesAudit(t, dossierJournalAudit())
	if len(l) != 2*len(cas) {
		t.Fatalf("%d audit lines for %d refusals", len(l), len(cas))
	}
	for i := 0; i < len(l); i += 2 {
		if l[i].ID != l[i+1].ID || l[i].Type != "demande" || l[i+1].Resultat != "refuse: "+cas[i/2].raison {
			t.Fatalf("audit pair %d: %+v %+v", i/2, l[i], l[i+1])
		}
	}
	// a grant on prod links the project to prod
	m := ok
	m.Env = "prod"
	if _, err := s.acces.Ajouter("demo", EntreeCoffre{ID: "e1", Nom: "Stripe"}, "prod", "STRIPE_KEY", ""); err != nil {
		t.Fatal(err)
	}
	if rep := s.envoyerBus(m); rep.Etat != "ok" {
		t.Fatalf("prod after a grant: %+v", rep)
	}
}

// TestBusI4: no answer hands the asker a path into the hub's data or into another module's data.
func TestBusI4(t *testing.T) {
	s := serveurBusTest(t)
	base := dossierAthanor()
	cas := map[string]bool{ // path → refused
		filepath.Join(base, "hub", "acces.json"):        true,
		filepath.Join(base, "hub"):                      true,
		filepath.ToSlash(filepath.Join(base, "hub")):    true,
		filepath.Join(base, "modules", "fuite", "x"):    true,
		filepath.Join(base, "modules"):                  true,
		"voir " + filepath.Join(base, "modules", "dev"): false, // the asker's own data
		filepath.Join(base, "modules", "dev", "a.json"): false,
		filepath.Join(base, "modules", "devx"):          true,
		filepath.Join(base, "hubert"):                   false,
		"rien":                                          false,
	}
	for p, refuse := range cas {
		rep := s.envoyerBus(MessageHub{De: "dev", Vers: "fuite", Action: "chemin", Projet: "demo", Env: "dev", Donnees: map[string]any{"p": p}})
		if (rep.Etat == "refuse") != refuse || refuse && (rep.Raison != "forbidden path" || rep.Donnees != nil) {
			t.Errorf("%s: %+v", p, rep)
		}
	}
}
