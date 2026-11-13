package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestHubAlertes: the bell gathers the alerts of two dummy modules in the generic format; Ignore hides
// one without deleting anything (the element still waits, the history shows it), Reopen brings it
// back, and an ignored alert comes back by itself when its reason changes.
func TestHubAlertes(t *testing.T) {
	s := serveurCoffreTest(t)
	modulesMu.Lock()
	avant := append([]Module{}, modules...)
	modulesMu.Unlock()
	t.Cleanup(func() { modulesMu.Lock(); modules = avant; modulesMu.Unlock() })
	domaine := &AttenteElement{Raison: "Choose a domain", Page: "#missions"}
	retirerModuleTest("missions") // the fake stands for the real Missions module
	enregistrerModule(moduleFactice{id: "missions", ordre: 10, el: []ElementHub{{ID: "m2", Titre: "Domaine", Etat: "actif", Lien: "#missions", Attente: domaine}}})
	enregistrerModule(moduleFactice{id: "serveurs", ordre: 30, el: []ElementHub{
		{ID: "vps-1", Titre: "vps-1", Etat: "actif", Lien: "#serveurs", Attente: &AttenteElement{Raison: "Disk {0} % full", Args: []any{91}, Page: "#serveurs"}},
		{ID: "vps-2", Titre: "vps-2", Etat: "actif", Lien: "#serveurs"},
	}})
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	type vue struct {
		Elements []ElementHub `json:"elements"`
		Alertes  []AlerteHub  `json:"alertes"`
		Ignorees []AlerteHub  `json:"ignorees"`
	}
	lire := func() vue {
		t.Helper()
		w := appel("GET", "/api/hub", "")
		var v vue
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("/api/hub: %d %s", w.Code, w.Body.String())
		}
		return v
	}
	cles := func(l []AlerteHub) string {
		var c []string
		for _, a := range l {
			c = append(c, a.Module+"/"+a.Element)
		}
		return strings.Join(c, " ")
	}
	ignorer := func(e string, oui bool) int {
		b, _ := json.Marshal(map[string]any{"empreinte": e, "ignorer": oui})
		return appel("POST", "/api/hub/alertes", string(b)).Code
	}

	v := lire()
	if g := cles(v.Alertes); g != "missions/m2 serveurs/vps-1" || len(v.Ignorees) != 0 {
		t.Fatalf("alerts: %q, ignored: %d", g, len(v.Ignorees))
	}
	a := v.Alertes[0]
	if a.Titre != "Domaine" || a.Raison != "Choose a domain" || len(a.Actions) != 2 || a.Actions[0].ID != "voir" || a.Actions[0].Page != "#missions" || a.Actions[1].ID != "ignorer" || a.Empreinte == "" {
		t.Fatalf("generic format: %+v", a)
	}
	if v.Alertes[1].Raison != "Disk {0} % full" || len(v.Alertes[1].Args) != 1 {
		t.Errorf("reason's arguments: %+v", v.Alertes[1])
	}

	if c := ignorer(a.Empreinte, true); c != http.StatusOK {
		t.Fatalf("ignore: %d", c)
	}
	v = lire()
	if cles(v.Alertes) != "serveurs/vps-1" || cles(v.Ignorees) != "missions/m2" || v.Ignorees[0].Ignoree == "" {
		t.Fatalf("after Ignore: alerts %q, history %q", cles(v.Alertes), cles(v.Ignorees))
	}
	if len(v.Elements) != 3 || v.Elements[0].ID != "m2" || v.Elements[0].Attente == nil {
		t.Errorf("Ignore deleted something: %+v", v.Elements)
	}
	if _, err := os.Stat(fichierAlertesIgnorees()); err != nil || !strings.HasPrefix(fichierAlertesIgnorees(), dossierModule("hub")) {
		t.Errorf("ignored alerts not kept in the hub's folder: %v", err)
	}

	if c := ignorer(a.Empreinte, false); c != http.StatusOK || cles(lire().Alertes) != "missions/m2 serveurs/vps-1" {
		t.Fatalf("reopen: %d", c)
	}
	ignorer(a.Empreinte, true)
	domaine.Raison = "Domain expired" // the situation changed: a new alert
	v = lire()
	if cles(v.Alertes) != "missions/m2 serveurs/vps-1" || len(v.Ignorees) != 0 || v.Alertes[0].Empreinte == a.Empreinte {
		t.Fatalf("changed reason: alerts %q, history %q", cles(v.Alertes), cles(v.Ignorees))
	}
	// the same for an argument (91 % then 97 %)
	vps := v.Alertes[1]
	ignorer(vps.Empreinte, true)
	modulesMu.Lock()
	for i, m := range modules {
		if m.ID() == "serveurs" {
			f := m.(moduleFactice)
			f.el = []ElementHub{{ID: "vps-1", Titre: "vps-1", Etat: "actif", Attente: &AttenteElement{Raison: "Disk {0} % full", Args: []any{97}, Page: "#serveurs"}}}
			modules[i] = f
		}
	}
	modulesMu.Unlock()
	if g := cles(lire().Alertes); g != "missions/m2 serveurs/vps-1" {
		t.Errorf("changed argument: %q", g)
	}

	// B32: a new waiting task (Cle) rings even with the same count; an ignored situation which ended
	// and comes back later rings again (its fingerprint was dropped while it was gone)
	domaine.Cle = "t1"
	ignorer(lire().Alertes[0].Empreinte, true)
	domaine.Cle = "t2"
	if g := cles(lire().Alertes); g != "missions/m2 serveurs/vps-1" {
		t.Errorf("new task, same count: %q", g)
	}
	ignorer(lire().Alertes[0].Empreinte, true)
	domaine.Cle = "t3"
	lire()
	domaine.Cle = "t2"
	if g := cles(lire().Alertes); g != "missions/m2 serveurs/vps-1" {
		t.Errorf("situation back after it ended: %q", g)
	}

	if c := ignorer("inconnue", true); c != http.StatusNotFound {
		t.Errorf("unknown alert: %d, want 404", c)
	}
	if c := appel("POST", "/api/hub/alertes", "{").Code; c != http.StatusBadRequest {
		t.Errorf("bad body: %d, want 400", c)
	}
}

// TestAlertesApprobations: a pending approval is an alert keyed by its id, first in the bell, with no
// Ignore and its three decisions sent as is (allowed from a paired device); a decided one is gone.
func TestAlertesApprobations(t *testing.T) {
	l := []Approbation{
		{ID: "a1", Projet: "boutique", Env: "dev", Action: "demander_acces", Raison: "Stripe", Etat: "attente"},
		{ID: "a2", Projet: "boutique", Env: "prod", Action: "demander_deploiement", Raison: "v1", Etat: "refusee"},
	}
	al := alertesApprobations(l)
	if len(al) != 1 || al[0].Empreinte != "appr-a1" || al[0].Approbation == nil || al[0].Raison != "Access requested ({0}): {1}" {
		t.Fatalf("alertes: %+v", al)
	}
	ids := []string{}
	for _, x := range al[0].Actions {
		ids = append(ids, x.ID)
		if x.Route != "hub/approbations" || x.Corps["id"] != "a1" || x.Corps["decision"] != x.ID || !x.Distance {
			t.Fatalf("action: %+v", x)
		}
	}
	if strings.Join(ids, ",") != "accepter,refuser,toujours" {
		t.Fatalf("actions: %v", ids)
	}
	if a := ignorerOuRouvrir("e", false); a.ID != "rouvrir" || a.Corps["ignorer"] != false || a.Distance {
		t.Fatalf("rouvrir: %+v", a)
	}
}
