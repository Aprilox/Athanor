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
	if code != 200 || strings.Count(corps, `"type":"serveur"`) != mentionsParGroupe || !strings.Contains(corps, `"autres":{"serveur":26}`) || strings.Contains(corps, temoin) || !strings.Contains(corps, cle.ID) { // 31 servers never hide the key
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
	// M244k7: the chips already chosen are left out before the cut, so the next ones come up
	if _, corps := appel("noeud&sauf=serveur:x00&sauf=serveur:x01"); strings.Contains(corps, `"ref":"x00"`) || !strings.Contains(corps, `"ref":"x06"`) || !strings.Contains(corps, `"autres":{"serveur":23}`) {
		t.Errorf("chosen chips not left out before the cut: %s", corps)
	}
	if _, corps := appel("zzz"); strings.TrimSpace(corps) != `{"autres":{},"mentions":[]}` {
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

// TestMentionsMemeNom: a project and a server both named "boutique" stay two typed mentions, each resolved to
// its own object by type and id; the name alone never resolves a server.
func TestMentionsMemeNom(t *testing.T) {
	s := serveurCoffreTest(t)
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "boutique", Dossier: t.TempDir()}}
	s.mu.Unlock()
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "srv9", Nom: "boutique", Hote: "203.0.113.9"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.chercherMentions("boutique")
	if len(l) != 2 || l[0].Type != "projet" || l[0].Ref != "boutique" || l[1].Type != "serveur" || l[1].Ref != "srv9" {
		t.Fatalf("list: %+v", l)
	}
	m, err := s.resoudreMentions([]MentionDemandee{{"serveur", "srv9"}})
	if err != nil || len(m.Projets) != 0 || len(m.Serveurs) != 1 || m.Serveurs[0] != "srv9" {
		t.Fatalf("server: %+v %v", m, err)
	}
	if got := s.mentionsLisibles(m); len(got) != 1 || got[0] != "server boutique (id srv9, 203.0.113.9)" {
		t.Errorf("server read as %q", got)
	}
	m, err = s.resoudreMentions([]MentionDemandee{{"projet", "boutique"}})
	if err != nil || len(m.Serveurs) != 0 || len(m.Projets) != 1 {
		t.Fatalf("project: %+v %v", m, err)
	}
	if got := s.mentionsLisibles(m); len(got) != 1 || got[0] != "project boutique" {
		t.Errorf("project read as %q", got)
	}
	if _, err := s.resoudreMentions([]MentionDemandee{{"serveur", "boutique"}}); err != errMentionInconnue {
		t.Errorf("a server named by its name alone: %v", err)
	}
}
