package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMissionPermissions (M271): the Servers lane's mode is the owner's: an unknown mode is refused, Ignore
// permissions needs the confirmation and Athanor's password, Manual keeps the plan rules (""), and a card without a
// mode takes Settings › Permissions (missions); the mode chosen is audited, never the password.
func TestMissionPermissions(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("POST /api/missions/{id}/lancer", s.garde(false, s.apiLancerMission)) // M301: the route is gone, its code goes with M302
	appel := func(chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	nom := "perm"
	p := Projet{Nom: nom, Dossier: filepath.Join(t.TempDir(), nom)}
	ecrireFichier(t, p.Demande(), "# perm\n", 0o644)
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site\n", 0o644)
	s.config.Projets = append(s.config.Projets, p)
	c := CarteMission{Titre: "Site perm", Projet: ProjetCarte{Nom: nom}, Dev: BriefCarte{Brief: "Faire le site"}, JalonsEstimes: 1}
	x := s.contexteCarte()
	if err := validerCarte(&c, x); err != nil {
		t.Fatal(err)
	}
	if v := c; true {
		v.Permissions = "tout"
		if validerCarte(&v, x) == nil {
			t.Error("an unknown mode in a card is accepted")
		}
	}
	id := aleatoire(8)
	b, _ := json.Marshal(calculerCarte(c, "prod", x))
	if err := enregistrerMission(Mission{ID: id, Phrase: "une mission", Etat: "proposee", Politique: "prod", Carte: b, Voies: []VoieMission{},
		Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
		t.Fatal(err)
	}
	if m := modeMission(id); m != "" {
		t.Errorf("no mode and no setting: %q, want the plan rules", m)
	}
	s.config.PermissionsUsage = map[string]string{"missions": "auto"}
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	if m := modeMission(id); m != "auto" {
		t.Errorf("Settings › Permissions not applied: %q", m)
	}
	// Settings › Permissions: a known use and mode, ignorer guarded as for a mission; the assistant's default is a server's without a mode
	for _, k := range []struct {
		corps string
		code  int
	}{
		{`{"usage":"tout","mode":"auto"}`, http.StatusBadRequest},
		{`{"usage":"assistant","mode":"tout"}`, http.StatusBadRequest},
		{`{"usage":"assistant","mode":"ignorer","confirmer":true}`, http.StatusUnauthorized},
		{`{"usage":"assistant","mode":"modifications"}`, http.StatusOK},
	} {
		if code, corps := appel("/api/reglages/permissions", k.corps); code != k.code {
			t.Errorf("settings %s: %d %s, want %d", k.corps, code, corps, k.code)
		}
	}
	if m := modeServeur(ServeurInventaire{}); m != "modifications" {
		t.Errorf("a server without a mode: %q, want Settings' modifications", m)
	}
	if m := modeServeur(ServeurInventaire{Autorisation: "auto"}); m != "auto" {
		t.Errorf("a server's own mode lost: %q", m)
	}
	if code, _ := appel("/api/reglages/permissions", `{"usage":"missions","mode":"ignorer","confirmer":true,"mot_de_passe":"mdp-test"}`); code != http.StatusOK || lireConfig().PermissionsUsage["missions"] != "ignorer" {
		t.Errorf("settings ignorer with the password: %d %v", code, lireConfig().PermissionsUsage)
	}
	r := httptest.NewRequest("GET", "/api/reglages/permissions", nil)
	r.Header.Set("X-Athanor", "jeton")
	r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
	r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"missions":"ignorer"`) || !strings.Contains(w.Body.String(), `"assistant":"modifications"`) {
		t.Errorf("GET settings: %d %s", w.Code, w.Body.String())
	}

	// the values typed at the launch: only for an entry the card waits for, kept in the vault, never in the audit
	c2 := CarteMission{Titre: "Site perm 2", Projet: ProjetCarte{Nom: nom}, Dev: BriefCarte{Brief: "Faire"}, Acces: []AccesCarte{{Entree: "SSH_PERM_271", Pour: nom}}, JalonsEstimes: 1}
	if err := validerCarte(&c2, x); err != nil || !c2.Acces[0].ARemplir {
		t.Fatalf("card with an entry to fill: %v %+v", err, c2.Acces)
	}
	id2 := aleatoire(8)
	b, _ = json.Marshal(calculerCarte(c2, "prod", x))
	if err := enregistrerMission(Mission{ID: id2, Phrase: "une autre", Etat: "proposee", Politique: "prod", Carte: b, Voies: []VoieMission{},
		Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
		t.Fatal(err)
	}
	if code, corps := appel("/api/missions/"+id2+"/lancer", `{"mot_de_passe":"mdp-test","secrets":[{"entree":"AUTRE","type":"mot_de_passe","valeur":"v"}]}`); code != http.StatusBadRequest {
		t.Errorf("a value for an entry the card does not wait for: %d %s", code, corps)
	}
	if m, _ := lireMission(id2); m.Etat != "proposee" {
		t.Errorf("launched after a refused value: %s", m.Etat)
	}
	rq := httptest.NewRequest("POST", "/", nil)
	if err := s.rangerSecretsLancement(rq, id2, []secretLancement{{Entree: "SSH_PERM_271", Type: "autre", Valeur: "v"}}); err == nil {
		t.Error("a value of another type is kept")
	}
	if err := s.rangerSecretsLancement(rq, id2, []secretLancement{{Entree: "ssh_perm_271", Type: "mot_de_passe", Valeur: "valeur-secrete-271"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.Nom == "ssh_perm_271" && e.Type == "mot_de_passe" }) {
		t.Error("the value typed at the launch is not in the vault")
	}
	if s.rangerSecretsLancement(rq, id2, []secretLancement{{Entree: "SSH_PERM_271", Type: "mot_de_passe", Valeur: "v2"}}) == nil {
		t.Error("a second entry of the same name is kept")
	}

	secretVu := false
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if strings.Contains(l.Resultat+l.Action, "valeur-secrete-271") {
			t.Errorf("a value typed at the launch in the audit: %+v", l)
		}
		secretVu = secretVu || l.Mission == id2 && l.Action == "secret rangé au lancement"
		if strings.Contains(l.Resultat, "mdp-test") || strings.Contains(l.Action, "mdp-test") {
			t.Errorf("the password in the audit: %+v", l)
		}
	}
	if !secretVu {
		t.Error("not audited: the value kept at the launch")
	}
}
