package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestEmailModule: the e-mail module is registered, its page and tile come from the registry, its API
// answers an empty list, and the hub still works with the module taken out of the registry.
func TestEmailModule(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, d)
	}
	var email Module
	for _, m := range modulesHub() {
		if m.ID() == "email" {
			email = m
		}
	}
	if email == nil {
		t.Fatal("the e-mail module is not registered")
	}
	if _, ok := email.(moduleRoutes); !ok {
		t.Fatal("the e-mail module declares no routes")
	}
	if p := email.Pages(); len(p) != 1 || p[0].Hash != "#email" || p[0].Ordre <= 31 || p[0].Ordre >= 40 {
		t.Fatalf("pages: %+v (want #email between Servers and the Vault)", p)
	}

	s := &Serveur{}
	mux := http.NewServeMux()
	email.(moduleRoutes).Routes(mux, s)
	w := httptest.NewRecorder()
	s.apiEmailComptes(w, httptest.NewRequest("GET", "/api/email/comptes", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), `{"comptes":[],`) {
		t.Fatalf("GET /api/email/comptes: %d %s", w.Code, w.Body.String())
	}
	if r := email.Resume(s); len(r.Stats) != 1 || r.Etat != "inactif" {
		t.Fatalf("resume: %+v", r)
	}
	// one account on disk: listed, the tile counts it, no password field
	os.WriteFile(fichierComptesEmail(), []byte(`[{"id":"a1","nom":"Boutique","hote":"smtp.exemple.fr","port":587,"securite":"starttls","utilisateur":"u","expediteur":"u@exemple.fr","entree":"e1"}]`), 0o600)
	w = httptest.NewRecorder()
	s.apiEmailComptes(w, httptest.NewRequest("GET", "/api/email/comptes", nil))
	var rep struct{ Comptes []CompteEmail }
	if json.Unmarshal(w.Body.Bytes(), &rep) != nil || len(rep.Comptes) != 1 || rep.Comptes[0].Nom != "Boutique" {
		t.Fatalf("list: %s", w.Body.String())
	}
	if email.Resume(s).Etat != "ok" {
		t.Fatal("an account must make the tile ok")
	}

	// from a device: the list readable, the rest of the module refused by default
	for _, c := range []struct {
		m, chemin string
		refus     bool
	}{{"GET", "/api/email/comptes", false}, {"POST", "/api/email/comptes", true}, {"POST", "/api/email/tester", true}} {
		r := httptest.NewRequest(c.m, c.chemin, nil)
		if refuseHubDistance(r) != c.refus {
			t.Errorf("%s %s from a device: refused %v, want %v", c.m, c.chemin, !c.refus, c.refus)
		}
	}

	// the module taken out: the other modules and the home's data are built as before
	modulesMu.Lock()
	avant := modules
	var sans []Module
	for _, m := range modules {
		if m.ID() != "email" {
			sans = append(sans, m)
		}
	}
	modules = sans
	modulesMu.Unlock()
	defer func() { modulesMu.Lock(); modules = avant; modulesMu.Unlock() }()
	ids := map[string]bool{}
	for _, m := range modulesHub() {
		ids[m.ID()] = true
	}
	if ids["email"] || !ids["dev"] || !ids["servers"] || !ids["coffre"] {
		t.Fatalf("modules without e-mail: %v", ids)
	}
}
