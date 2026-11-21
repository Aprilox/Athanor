package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests of the final red team (M210, BUGS.md § Red team finale): each one replays a finding.

// appelRedTeam: a call from the unlocked window (access cookie, X-Athanor and the session of avecMdpPreuve).
func appelRedTeam(t *testing.T, s *Serveur) func(methode, chemin string, corps any) (int, map[string]any) {
	mux := http.NewServeMux()
	s.routes(mux)
	return func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
}

// RT1: a secret's fields (an SMTP account's server) do not change without its value retyped: the kept password never
// follows its account to another server.
func TestRedTeamChampsCoffre(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	appel := appelRedTeam(t, s)
	temoin := "témoin-" + aleatoire(12)
	champs := []map[string]string{{"nom": "SMTP_HOST", "valeur": "smtp.exemple.fr"}, {"nom": "SMTP_PORT", "valeur": "587"}}
	code, v := appel("POST", "/api/coffre", map[string]any{"nom": "Boutique", "famille": "secret", "type": "smtp", "portee": "code", "valeur": temoin, "champs": champs})
	if code != 200 {
		t.Fatalf("add: %d %v", code, v)
	}
	id := v["id"].(string)
	t.Cleanup(func() { appel("DELETE", "/api/coffre/"+id, nil) })
	hote := func() string {
		for _, e := range s.coffre.Liste() {
			if e.ID == id && len(e.Champs) > 0 {
				return e.Champs[0].Valeur
			}
		}
		return ""
	}
	for _, pirate := range [][]map[string]string{{{"nom": "SMTP_HOST", "valeur": "pirate.exemple.net"}, champs[1]}, {champs[0], {"nom": "SMTP_PORT", "valeur": "2525"}}, {champs[0]}} {
		if code, v := appel("POST", "/api/coffre/"+id, map[string]any{"champs": pirate}); code != http.StatusBadRequest || v["erreur"] != errChampsValeur.Error() {
			t.Errorf("fields changed without the value: %d %v", code, v)
		}
	}
	if h := hote(); h != "smtp.exemple.fr" {
		t.Fatalf("the refused change was written: %q", h)
	}
	// the same fields with a new note need no value; another server with the value retyped passes, and it still opens
	if code, v := appel("POST", "/api/coffre/"+id, map[string]any{"champs": champs, "note": "Envoi des commandes"}); code != 200 {
		t.Fatalf("same fields: %d %v", code, v)
	}
	champs[0]["valeur"] = "smtp.autre.fr"
	if code, v := appel("POST", "/api/coffre/"+id, map[string]any{"champs": champs, "valeur": temoin}); code != 200 || hote() != "smtp.autre.fr" || strings.Contains(fmt.Sprint(v), temoin) {
		t.Fatalf("new server with the value: %d %v", code, v)
	}
	if x, err := s.coffre.Valeur(id); err != nil || x != temoin {
		t.Fatalf("the value after the move: %v", err)
	}
}

// RT2: nothing moves a project to another environment any more (M294): a key an older version gave to prod alone
// reaches the pilot only when the owner gives it, with Athanor's password retyped (C25).
func TestRedTeamEnvProd(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	demo := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(demo.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.config.Projets = []Projet{demo}
	s.mu.Unlock()
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	tProd := "temoin-prod-" + aleatoire(8)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe live", Famille: "secret", Type: "stripe"}, tProd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.acces.Ajouter("demo", e, "prod", "", ""); err != nil {
		t.Fatal(err)
	}
	appel := appelRedTeam(t, s)
	fichier := filepath.Join(demo.Atelier(), "secrets.env")
	lire := func() string { b, _ := os.ReadFile(fichier); return string(b) }
	poster := func(action, mdp string) int {
		code, _ := appel("POST", "/api/acces", map[string]string{"projet": "demo", "action": action, "entree": e.ID, "env": "*", "mot_de_passe": mdp})
		return code
	}

	if c := poster("ajouter", ""); c != http.StatusForbidden || strings.Contains(lire(), tProd) { // no password for Athanor yet
		t.Fatalf("given without a password set: %d\n%s", c, lire())
	}
	avecMdpPreuve(s)
	if c := poster("env", mdpPreuveTest); c != http.StatusBadRequest || strings.Contains(lire(), tProd) {
		t.Fatalf("the removed env action: %d\n%s", c, lire())
	}
	if c := poster("ajouter", "faux"); c != http.StatusUnauthorized || strings.Contains(lire(), tProd) {
		t.Fatalf("given with a wrong password: %d\n%s", c, lire())
	}
	if c := poster("ajouter", mdpPreuveTest); c != 200 || !strings.Contains(lire(), tProd) {
		t.Fatalf("given with the password: %d\n%s", c, lire())
	}
}

// RT3: what shows a service's environment is a change (a plan), like docker inspect, never a plain read.
func TestRedTeamClasseurEnvironnement(t *testing.T) {
	for _, cmd := range []string{"env", "printenv", "printenv DB_PASSWORD", "/usr/bin/env", "sudo env", "systemctl show app",
		"systemctl show -p Environment app", "systemctl cat app", "systemctl show-environment", "sudo systemctl --no-pager cat app",
		"cat /etc/systemd/system/app.service", "cat /etc/systemd/system/app.service.d/override.conf", "grep -r PASSWORD /etc/systemd",
		"less /lib/systemd/system/app.service", "cat /usr/lib/systemd/system/app.service", "head /run/systemd/transient/x.service",
		"cat /etc/*/system/app.service", "cat < /etc/systemd/system/app.service"} {
		if c, r := classerCommande(cmd); c < ClasseChangement {
			t.Errorf("%q: %v %q, at least a change expected", cmd, c, r)
		}
	}
	for _, cmd := range []string{"systemctl status app", "systemctl is-active app", "systemctl list-units", "cat /etc/hostname"} {
		if c, r := classerCommande(cmd); c != ClasseLecture {
			t.Errorf("%q: %v %q, still a read", cmd, c, r)
		}
	}
	for _, chemin := range []string{"/etc/systemd/system/app.service", "/lib/systemd/system/x.service"} {
		if c, _ := classerOutil("fichier_lire", argsOutilServeur{Chemin: chemin}); c != ClasseChangement {
			t.Errorf("fichier_lire %s: %v", chemin, c)
		}
	}
	if c, _ := classerOutil("fichier_lire", argsOutilServeur{Chemin: "/var/log/syslog"}); c != ClasseLecture {
		t.Errorf("fichier_lire /var/log/syslog: %v", c)
	}
}

// A foreign Origin is refused on every guarded API route, even with every token valid; the page's own is not.
func TestGardeOrigineEtrangere(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	for _, cas := range []struct {
		origine string
		code    int
	}{{"http://pirate.example", http.StatusForbidden}, {"http://127.0.0.1:1", http.StatusForbidden}, {"null", http.StatusForbidden}, {"http://example.com", http.StatusOK}, {"", http.StatusOK}} {
		r := httptest.NewRequest("GET", "/api/etat", nil) // Host: example.com
		r.Header.Set("X-Athanor", "jeton")
		if cas.origine != "" {
			r.Header.Set("Origin", cas.origine)
		}
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != cas.code {
			t.Errorf("Origin %q: %d, want %d", cas.origine, w.Code, cas.code)
		}
	}
}
