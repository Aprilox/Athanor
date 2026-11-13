package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMentions: the @ list (projects, servers by readable name, vault entries by name and type, never a value,
// at most 20) and resoudreMentions (an unknown or hostile ref refused, duplicates dropped).
func TestMentions(t *testing.T) {
	s := serveurCoffreTest(t)
	const temoin = "TEMOIN-mention-9f3c2a"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "ancien", Dossier: t.TempDir()}, {Nom: "boutique", Dossier: t.TempDir()},
		{Nom: "range", Dossier: t.TempDir(), Archive: true}, {Nom: "vitrine", Dossier: t.TempDir()}}
	s.config.ProjetActif = "boutique"
	s.mu.Unlock()
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		l = append(l, ServeurInventaire{ID: "srv1", Nom: "web-1", Hote: "203.0.113.5"})
		for i := range 30 {
			l = append(l, ServeurInventaire{ID: fmt.Sprintf("x%02d", i), Nom: fmt.Sprintf("noeud-%02d", i), Hote: "198.51.100.1"})
		}
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(q string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/mentions?q="+q, nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	code, corps := appel("")
	if code != 200 || strings.Count(corps, `"type"`) != maxMentions || strings.Contains(corps, temoin) || !strings.Contains(corps, cle.ID) { // 31 servers never hide the key
		t.Fatalf("empty q: %d %s", code, corps)
	}
	// the active project first, then the newest; the archived one never
	if i, j := strings.Index(corps, `"boutique"`), strings.Index(corps, `"vitrine"`); i < 0 || j < i || strings.Index(corps, `"ancien"`) < j || strings.Contains(corps, `"range"`) {
		t.Errorf("project order: %s", corps)
	}
	if _, corps := appel("WEB"); !strings.Contains(corps, `{"type":"serveur","ref":"srv1","nom":"web-1","detail":"203.0.113.5"}`) {
		t.Errorf("server by readable name: %s", corps)
	}
	if _, corps := appel("cloudflare"); !strings.Contains(corps, `"ref":"`+cle.ID+`","nom":"Jeton Cloudflare","detail":"cloudflare"`) || strings.Contains(corps, temoin) || strings.Contains(corps, "valeur") {
		t.Errorf("vault entry by type: %s", corps)
	}
	if _, corps := appel("zzz"); corps != `{"mentions":[]}`+"\n" && corps != `{"mentions":[]}` {
		t.Errorf("no match: %q", corps)
	}

	m, err := s.resoudreMentions([]MentionDemandee{{"projet", "boutique"}, {"serveur", "srv1"}, {"cle", cle.ID}, {"projet", "boutique"}, {"nouveau", ""}})
	if err != nil || len(m.Projets) != 1 || m.Serveurs[0] != "srv1" || m.Cles[0] != cle.ID || !m.Nouveau {
		t.Fatalf("resolution: %+v %v", m, err)
	}
	for _, d := range []MentionDemandee{{"projet", "range"}, {"projet", "inconnu"}, {"serveur", "web-1"}, {"cle", "Jeton Cloudflare"},
		{"cle", "../coffre"}, {"invente", "boutique"}, {"", ""}} {
		if _, err := s.resoudreMentions([]MentionDemandee{d}); err != errMentionInconnue {
			t.Errorf("%+v accepted: %v", d, err)
		}
	}
	if _, err := s.resoudreMentions(make([]MentionDemandee, maxMentions+1)); err == nil {
		t.Error("21 mentions accepted")
	}
}
