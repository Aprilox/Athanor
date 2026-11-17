package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMissionsStockage: round trip in 0600, a damaged file skipped, hostile ids refused, the API and the tile.
func TestMissionsStockage(t *testing.T) {
	s := serveurCoffreTest(t)
	m := Mission{ID: "abc123", Phrase: "Mettre ma-boutique en ligne", Etat: "proposee", Politique: "prod",
		Mentions: Mentions{Projets: []string{"ma-boutique"}}, Carte: []byte(`{"titre":"Boutique"}`),
		Historique: []EvenementMission{{T: "2026-11-13T10:00:00.000Z", Etat: "proposee"}}, Cree: "2026-11-13T10:00:00.000Z"}
	if err := enregistrerMission(m); err != nil {
		t.Fatal(err)
	}
	if err := enregistrerMission(Mission{ID: "def456", Etat: "lancee", Cree: "2026-11-13T11:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []Mission{{ID: "../x", Etat: "proposee"}, {ID: "abc123", Etat: "invente"}} {
		if enregistrerMission(x) == nil {
			t.Errorf("%+v saved", x)
		}
	}
	lu, err := lireMission("abc123")
	if err != nil || lu.Phrase != m.Phrase || lu.Mentions.Projets[0] != "ma-boutique" || !strings.Contains(string(lu.Carte), `"Boutique"`) || len(lu.Historique) != 1 {
		t.Fatalf("round trip: %+v %v", lu, err)
	}
	if st, err := os.Stat(filepath.Join(dossierMissions(), "abc123.json")); err != nil || (st.Mode().Perm() != 0o600 && os.PathSeparator == '/') {
		t.Errorf("file: %v %v", st, err)
	}
	// damaged file and a file whose content names another mission: skipped, never fatal
	_ = os.WriteFile(filepath.Join(dossierMissions(), "abimee1.json"), []byte(`{"id":"abimee1",`), 0o600)
	_ = os.WriteFile(filepath.Join(dossierMissions(), "autre12.json"), []byte(`{"id":"abc123","etat":"lancee"}`), 0o600)
	l := lireMissions()
	if len(l) != 2 || l[0].ID != "def456" || l[1].ID != "abc123" {
		t.Fatalf("list: %+v", l)
	}
	for _, id := range []string{"abimee1", "autre12", "../hub/coffre", "ABC123", "abc", "", "abc123/../abc123"} {
		if _, err := lireMission(id); err == nil {
			t.Errorf("%q read", id)
		}
	}

	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(chemin string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", chemin, nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		mux.ServeHTTP(w, r)
		return w
	}
	if w := appel("/api/missions"); w.Code != 200 || !strings.Contains(w.Body.String(), `"def456"`) || strings.Contains(w.Body.String(), "abimee1") {
		t.Errorf("list: %d %s", w.Code, w.Body)
	}
	if w := appel("/api/missions/abc123"); w.Code != 200 || !strings.Contains(w.Body.String(), "ma-boutique") {
		t.Errorf("one: %d %s", w.Code, w.Body)
	}
	for _, id := range []string{"abimee1", "zzz999", "..%2Fx"} {
		if w := appel("/api/missions/" + id); w.Code != 404 {
			t.Errorf("%s: %d", id, w.Code)
		}
	}
	r := moduleMissions{}.Resume(s)
	if r.Stats[0].Valeur != 1 || r.Stats[1].Valeur != 1 || r.Stats[2].Valeur != 0 || r.Etat != "attention" {
		t.Errorf("tile: %+v", r)
	}
	// the menu: Home, Projects, Missions, Studio
	var ids []string
	for _, x := range modulesHub() {
		ids = append(ids, x.ID())
	}
	if strings.Join(ids, " ")[:len("hub missions dev")] != "hub missions dev" {
		t.Errorf("menu order: %v", ids)
	}
}
