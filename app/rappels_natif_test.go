package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRappelsNatif (M240r2): native mode's reminders turned off by the owner. Turning them off asks for
// the password, turning them on does not; a Servers task is then forced without the question nor the
// password and logged; infra entries stay refused; outside native mode nothing changes; never remotely.
func TestRappelsNatif(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	avant := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(avant) })
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}}
	s.mu.Unlock()
	mux := http.NewServeMux()
	s.routes(mux)
	avecMdpPreuve(s)
	appel := func(chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest("POST", chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	tache := func() (int, string) { return appel("/api/serveurs/inconnu/taches", `{"texte":"x"}`) }
	modeNatif.Store(true)

	// off by default: the current behaviour, the WSL proposal
	if c, b := tache(); c != http.StatusConflict || !strings.Contains(b, "proposer_wsl") {
		t.Fatalf("reminder by default: %d %s", c, b)
	}
	// turning them off: the password, retyped
	if c, _ := appel("/api/options/rappels-natif", `{"actif":true}`); c == 200 || s.config.SansRappelsNatif {
		t.Fatalf("turned off without the password: %d", c)
	}
	if c, b := appel("/api/options/rappels-natif", `{"actif":true,"mot_de_passe":"`+mdpPreuveTest+`"}`); c != 200 || !s.sansRappelsNatif() {
		t.Fatalf("turned off: %d %s", c, b)
	}
	// no question nor password: the gate is passed (the unknown server answers, not the native mode)
	if c, b := tache(); c == http.StatusConflict || c == http.StatusUnauthorized || strings.Contains(b, "proposer_wsl") {
		t.Fatalf("still reminded: %d %s", c, b)
	}
	// infra entries are never given by this setting alone
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	infra, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, "TEMOIN-"+aleatoire(8))
	if err != nil {
		t.Fatal(err)
	}
	if c, b := appel("/api/acces", `{"projet":"demo","action":"ajouter","entree":"`+infra.ID+`","env":"dev","mot_de_passe":"`+mdpPreuveTest+`"}`); c != http.StatusConflict {
		t.Fatalf("infra given by the setting: %d %s", c, b)
	}
	// outside native mode the setting changes nothing
	modeNatif.Store(false)
	if s.sansRappelsNatif() {
		t.Fatal("setting effective outside native mode")
	}
	modeNatif.Store(true)
	// a device never turns them off
	if r := httptest.NewRequest("POST", "https://abc-def.trycloudflare.com/api/options/rappels-natif", nil); !interditDistance(r) {
		t.Fatal("route allowed remotely")
	}
	// turning them back on asks nothing, and every change is logged
	if c, _ := appel("/api/options/rappels-natif", `{"actif":false}`); c != 200 || s.sansRappelsNatif() {
		t.Fatalf("turned back on: %d", c)
	}
	if c, _ := tache(); c != http.StatusConflict {
		t.Fatalf("reminder back: %d", c)
	}
	var j []string
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		j = append(j, l.Action+" "+l.Resultat)
	}
	if s := strings.Join(j, "\n"); !strings.Contains(s, "sans_rappels_natif true") || !strings.Contains(s, "sans_rappels_natif false") || !strings.Contains(s, "rappels du mode natif refusé") {
		t.Fatalf("audit log: %s", s)
	}
}
