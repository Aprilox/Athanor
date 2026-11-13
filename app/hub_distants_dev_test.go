package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fauxDevDistant: a remote Athanor carrying dev, reduced to its MCP: it answers the tools its owner granted
// (tools/list), refuses the others, and records every tools/call it receives.
type fauxDevDistant struct {
	mu      sync.Mutex
	jeton   string
	outils  []string
	projets []map[string]any
	appels  []string // "name projet action"
}

func (f *fauxDevDistant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.jeton {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var q struct {
		Method string
		Params struct {
			Name      string
			Arguments map[string]any
		}
	}
	_ = json.NewDecoder(r.Body).Decode(&q)
	f.mu.Lock()
	defer f.mu.Unlock()
	resultat := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": v}) }
	texte := func(s string, erreur bool) {
		resultat(map[string]any{"content": []map[string]any{{"type": "text", "text": s}}, "isError": erreur})
	}
	switch q.Method {
	case "tools/list":
		l := []map[string]any{}
		for _, o := range f.outils {
			l = append(l, map[string]any{"name": o})
		}
		resultat(map[string]any{"tools": l})
		return
	case "tools/call":
	default:
		resultat(map[string]any{})
		return
	}
	a := q.Params.Arguments
	f.appels = append(f.appels, strings.TrimSpace(q.Params.Name+" "+toString(a["projet"])+" "+toString(a["action"])))
	if !slices.Contains(f.outils, q.Params.Name) {
		texte("tool not granted", true)
		return
	}
	switch q.Params.Name {
	case "etat":
		texte(`{"modules":[{"id":"dev"}]}`, false)
	case "projets":
		b, _ := json.Marshal(map[string]any{"projets": f.projets})
		texte(string(b), false)
	case "pilote":
		for _, p := range f.projets {
			if p["nom"] == a["projet"] {
				p["pilote_actif"] = a["action"] == "demarrer"
			}
		}
		texte("ok", false)
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (f *fauxDevDistant) appelsDe(nom string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l []string
	for _, a := range f.appels {
		if strings.HasPrefix(a, nom+" ") || a == nom {
			l = append(l, a)
		}
	}
	return l
}

// TestDevDistants (M189): two remote Devs under one hub. Their projects show as Dev elements nom@instance
// (offline: greyed, last state kept), in the Dev tile and the @mentions; a pilot order reaches the right
// remote only, through the bus (audited); a remote that did not grant pilote never receives one, and the
// hub asks nothing else than the tools granted.
func TestDevDistants(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(cheminDistants())
	t.Cleanup(func() { _ = os.Remove(cheminDistants()) })
	un := &fauxDevDistant{jeton: "jeton-un", outils: []string{"etat", "projets", "pilote"},
		projets: []map[string]any{{"nom": "jeu", "pilote_actif": false, "phase": "", "plan": "3/10 milestones", "jalon": "M4"}}}
	deux := &fauxDevDistant{jeton: "jeton-deux", outils: []string{"etat", "projets"},
		projets: []map[string]any{{"nom": "site", "pilote_actif": true, "plan": "1/4 milestones", "jalon": "M2 : la page", "besoin": "pour_vous"}}}
	h1, h2 := httptest.NewServer(un), httptest.NewServer(deux)
	defer h1.Close()
	defer h2.Close()
	m := s.dst()
	m.mu.Lock()
	m.liste = []Distant{{ID: "d1", Nom: "Atelier", URL: h1.URL}, {ID: "d2", Nom: "Portable", URL: h2.URL}}
	m.jetons["d1"], m.jetons["d2"] = un.jeton, deux.jeton
	m.mu.Unlock()
	s.rafraichirDistants()

	elements := func() map[string]ElementHub {
		el := map[string]ElementHub{}
		for _, e := range (moduleDev{}).Elements(s) {
			if e.Distant != "" {
				el[e.Titre] = e
			}
		}
		return el
	}
	el := elements()
	jeu, site := el["jeu@Atelier"], el["site@Portable"]
	if len(el) != 2 || jeu.Distant != "d1" || jeu.NomDistant != "jeu" || jeu.Etat != "inactif" || jeu.Avancement == nil || jeu.Avancement.Fait != 3 || jeu.Avancement.Total != 10 {
		t.Fatalf("remote elements: %+v", el)
	}
	if site.Etat != "actif" || site.Activite != "M2 : la page" || site.Attente == nil || site.HorsLigne {
		t.Errorf("site@Portable: %+v", site)
	}
	r := (moduleDev{}).Resume(s)
	if !slices.ContainsFunc(r.Stats, func(x StatModule) bool { return x.Cle == "Remote projects" && x.Valeur == 2 }) {
		t.Errorf("Dev tile without the remote projects: %+v", r.Stats)
	}
	if l := s.chercherMentions("jeu"); !slices.ContainsFunc(l, func(x MentionTrouvee) bool { return x.Type == "projet" && x.Nom == "jeu@Atelier" && x.Ref == "jeu@d1" }) {
		t.Errorf("@jeu does not find the remote project: %+v", l)
	}
	if _, err := s.resoudreMentions([]MentionDemandee{{Type: "projet", Ref: "jeu@d1"}}); err != nil {
		t.Errorf("remote mention refused: %v", err)
	}

	// orders through the window's API: to the right remote only
	ordre := func(id, projet, o string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		b, _ := json.Marshal(map[string]string{"action": "pilote", "id": id, "projet": projet, "ordre": o})
		s.apiDistants(w, httptest.NewRequest("POST", "/", strings.NewReader(string(b))))
		return w
	}
	if w := ordre("d1", "jeu", "demarrer"); w.Code != 200 {
		t.Fatalf("start refused: %d %s", w.Code, w.Body.String())
	}
	if l := un.appelsDe("pilote"); !slices.Equal(l, []string{"pilote jeu demarrer"}) {
		t.Errorf("remote 1 received %v", l)
	}
	if elements()["jeu@Atelier"].Etat != "actif" {
		t.Error("the remote is not read again after the order")
	}
	if w := ordre("d1", "jeu", "pause_fin_cycle"); w.Code != 200 || !slices.Contains(un.appelsDe("pilote"), "pilote jeu pause_fin_cycle") {
		t.Errorf("pause: %d %v", w.Code, un.appelsDe("pilote"))
	}
	for _, c := range [][3]string{{"d2", "site", "arreter"}, {"d1", "site", "arreter"}, {"d1", "jeu", "supprimer"}, {"d9", "jeu", "arreter"}} {
		if w := ordre(c[0], c[1], c[2]); w.Code != 400 {
			t.Errorf("order %v accepted: %d", c, w.Code)
		}
	}
	if l := deux.appelsDe("pilote"); len(l) != 0 {
		t.Errorf("remote 2 (pilote not granted) received %v", l)
	}
	for _, f := range []*fauxDevDistant{un, deux} {
		f.mu.Lock()
		for _, a := range f.appels {
			if nom, _, _ := strings.Cut(a, " "); !slices.Contains(f.outils, nom) {
				t.Errorf("the hub asked a tool not granted: %s", a)
			}
		}
		f.mu.Unlock()
	}
	if !strings.Contains(lireJournalAuditTest(), "pilote") {
		t.Error("the remote order is not in the audit log")
	}

	// remote 2 offline: its project stays, greyed, without the owner's alert, and no order goes through
	h2.Close()
	s.rafraichirDistants()
	site = elements()["site@Portable"]
	if !site.HorsLigne || site.Etat != "inactif" || site.Attente != nil {
		t.Errorf("offline remote: %+v", site)
	}
	deux.mu.Lock()
	deux.outils = append(deux.outils, "pilote")
	deux.mu.Unlock()
	if w := ordre("d2", "site", "arreter"); w.Code != 400 {
		t.Errorf("order to an offline remote accepted: %d", w.Code)
	}
}

func lireJournalAuditTest() string {
	journal := ""
	_ = filepath.WalkDir(dossierJournalAudit(), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			c, _ := os.ReadFile(p)
			journal += string(c)
		}
		return nil
	})
	return journal
}
