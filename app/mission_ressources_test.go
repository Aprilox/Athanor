package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMissionCarteRessources (M252): "Add a resource" on a card not launched. A server brings the environment
// and the Servers lane, a key and a provider (its token's entry) are accesses of the project; removing undoes
// it; an unknown resource is refused with the card untouched; every change changes the print, so a launch asked
// on the old card is refused; no value ever comes back. The @ list gives the providers when asked by type only.
func TestMissionCarteRessources(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	temoin := "TEMOIN-" + aleatoire(10)
	jeton, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	smtp, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Mot de passe SMTP", Famille: "secret", Type: "autre", Portee: "code"}, temoin+"-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "cf1", Type: "cloudflare", Nom: "Cloudflare perso", Cle: jeton.ID}), nil
	}); err != nil {
		t.Fatal(err)
	}
	boutique := filepath.Join(t.TempDir(), "boutique")
	_ = os.MkdirAll(boutique, 0o755)
	s.config.Projets = append(s.config.Projets, Projet{Nom: "boutique", Dossier: boutique})
	s.session = aleatoire(8)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	var reponses strings.Builder
	appel := func(methode, chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		return w.Code, w.Body.String()
	}
	const id = "missres01"
	carte := `{"titre":"Boutique","projet":{"nom":"boutique"},"dev":{"brief":"Finir la boutique"},"envs":[],"serveurs":{"brief":""},"acces":[],"jalons_estimes":3}`
	if err := enregistrerMission(Mission{ID: id, Phrase: "Finir la boutique", Etat: "proposee", Politique: "prod", Carte: json.RawMessage(carte),
		Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{}, Cree: "2026-11-16T10:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	lue := func() (Mission, CarteMission) {
		t.Helper()
		m, err := lireMission(id)
		var c CarteMission
		if err != nil || json.Unmarshal(m.Carte, &c) != nil {
			t.Fatalf("mission: %v", err)
		}
		return m, c
	}
	ressource := func(corps string, code int) {
		t.Helper()
		if c, rep := appel("POST", "/api/missions/"+id+"/ressources", corps); c != code {
			t.Fatalf("%s: %d %s, expected %d", corps, c, rep, code)
		}
	}
	m0, _ := lue()
	avant := empreinteCarte(m0)

	// a server: the "site" environment and the Servers lane appear; the print changes
	ressource(`{"type":"serveur","ref":"a1"}`, http.StatusOK)
	m, c := lue()
	if len(c.Envs) != 1 || c.Envs[0].Nom != "site" || !slices.Equal(c.Envs[0].Serveurs, []string{"a1"}) || c.Serveurs.Brief == "" ||
		!slices.Equal(c.Touche.Serveurs, []string{"web-1"}) || empreinteCarte(m) == avant {
		t.Fatalf("server added: %+v", c)
	}
	if _, err := s.lancerMission(id, "test", "", avant); err != errCarteChangee { // the launch asked on the old card
		t.Fatalf("launch on the old print: %v", err)
	}
	ressource(`{"type":"serveur","ref":"a1"}`, http.StatusOK) // twice: nothing more
	if m2, c2 := lue(); len(c2.Envs[0].Serveurs) != 1 || empreinteCarte(m2) != empreinteCarte(m) {
		t.Fatalf("server added twice: %+v", c2)
	}

	// a key and a provider (its token's entry), for the project
	ressource(`{"type":"cle","ref":"`+smtp.ID+`"}`, http.StatusOK)
	ressource(`{"type":"fournisseur","ref":"cf1"}`, http.StatusOK)
	_, c = lue()
	noms := func(c CarteMission) (l []string) {
		for _, a := range c.Acces {
			l = append(l, a.Entree+"|"+a.Pour+"|"+a.ID)
		}
		return l
	}
	if !slices.Equal(noms(c), []string{"Mot de passe SMTP|boutique|" + smtp.ID, "Jeton Cloudflare|boutique|" + jeton.ID}) {
		t.Fatalf("key and provider added: %v", noms(c))
	}

	// unknown or hostile resources: refused, the card untouched
	m, _ = lue()
	for _, corps := range []string{`{"type":"serveur","ref":"zzz"}`, `{"type":"cle","ref":"Jeton Cloudflare"}`, `{"type":"fournisseur","ref":"zz"}`,
		`{"type":"projet","ref":"boutique"}`, `{"type":"serveur","ref":""}`, `{"type":"cle","ref":"zzz","retirer":true}`, `{`} {
		ressource(corps, http.StatusBadRequest)
	}
	if m2, _ := lue(); empreinteCarte(m2) != empreinteCarte(m) {
		t.Fatal("a refused resource changed the card")
	}

	// removing: the provider's entry, then the last server takes the lane and the environment away
	ressource(`{"type":"fournisseur","ref":"cf1","retirer":true}`, http.StatusOK)
	ressource(`{"type":"serveur","ref":"a1","retirer":true}`, http.StatusOK)
	m, c = lue()
	if !slices.Equal(noms(c), []string{"Mot de passe SMTP|boutique|" + smtp.ID}) || len(c.Envs) != 0 || c.Serveurs.Brief != "" || len(c.Touche.Serveurs) != 0 {
		t.Fatalf("removed: %v %+v", noms(c), c)
	}
	if n := len(m.Historique); n < 5 || m.Historique[n-1].Detail != "resource removed: serveur" {
		t.Fatalf("history: %+v", m.Historique)
	}

	// the @ list: the providers asked by type, never in the home's groups; a provider chip resolves to its entry
	if _, rep := appel("GET", "/api/mentions?type=fournisseur", ""); !strings.Contains(rep, `{"type":"fournisseur","ref":"cf1","nom":"Cloudflare perso","detail":"cloudflare"}`) || strings.Contains(rep, `"type":"cle"`) {
		t.Fatalf("providers by type: %s", rep)
	}
	if _, rep := appel("GET", "/api/mentions", ""); strings.Contains(rep, `"fournisseur"`) {
		t.Fatalf("a provider in the home's list: %s", rep)
	}
	if mm, err := s.resoudreMentions([]MentionDemandee{{"fournisseur", "cf1"}}); err != nil || !slices.Equal(mm.Cles, []string{jeton.ID}) {
		t.Fatalf("provider chip: %+v %v", mm, err)
	}

	// M253: an entry to fill (not in the vault yet) is removed by its name, once; a vault entry's name is not enough
	cr := CarteMission{Projet: ProjetCarte{Nom: "boutique"}, Acces: []AccesCarte{{Entree: "SMTP_A_REMPLIR", Pour: "boutique", ARemplir: true}, {Entree: "Jeton Cloudflare", Pour: "boutique", ID: jeton.ID}}}
	if err := changerRessource(&cr, "cle", "smtp_a_remplir", true, contexteCarte{}, nil); err != nil || len(cr.Acces) != 1 {
		t.Fatalf("entry to fill removed: %v %+v", err, cr.Acces)
	}
	if err := changerRessource(&cr, "cle", "SMTP_A_REMPLIR", true, contexteCarte{}, nil); !errors.Is(err, errMentionInconnue) {
		t.Fatalf("removed twice: %v", err)
	}
	if err := changerRessource(&cr, "cle", "Jeton Cloudflare", true, contexteCarte{}, nil); !errors.Is(err, errMentionInconnue) || len(cr.Acces) != 1 {
		t.Fatalf("a vault entry removed by its name: %v", err)
	}

	// a mission no longer proposed: its card stays as it is
	if _, err := modifierMission(id, func(m *Mission) error { m.Etat = "lancee"; return nil }); err != nil {
		t.Fatal(err)
	}
	ressource(`{"type":"serveur","ref":"a1"}`, http.StatusConflict)
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("a vault value in an answer")
	}
}
