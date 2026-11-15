package main

import (
	"encoding/json"
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

// RT1: an account moved to another SMTP server does not take the kept password along without it being retyped.
func TestRedTeamEmailHoteChange(t *testing.T) {
	s := serveurCoffreTest(t)
	avecMdpPreuve(s)
	appel := appelRedTeam(t, s)
	temoin := "témoin-" + aleatoire(12)
	compte := map[string]any{"nom": "Boutique", "hote": "smtp.exemple.fr", "port": 587, "securite": "starttls",
		"utilisateur": "contact@exemple.fr", "expediteur": "contact@exemple.fr", "mot_de_passe": temoin}
	code, v := appel("POST", "/api/email/comptes", compte)
	if code != 200 {
		t.Fatalf("add: %d %v", code, v)
	}
	id := v["id"].(string)
	t.Cleanup(func() { appel("DELETE", "/api/email/comptes/"+id, nil) })
	compte["mot_de_passe"] = ""
	for champ, valeur := range map[string]any{"hote": "pirate.exemple.net", "port": 2525, "securite": "tls"} {
		modif := map[string]any{}
		for k, x := range compte {
			modif[k] = x
		}
		modif[champ] = valeur
		if code, v := appel("POST", "/api/email/comptes/"+id, modif); code != http.StatusBadRequest || v["erreur"] != errEmailMdpServeur.Error() {
			t.Errorf("%s changed without the password: %d %v", champ, code, v)
		}
	}
	if c, _ := lireComptesEmail(); len(c) != 1 || c[0].Hote != "smtp.exemple.fr" || c[0].Port != 587 {
		t.Fatalf("the refused change was written: %+v", c)
	}
	// renaming keeps the server: no password needed; a new server with its password retyped passes
	compte["nom"] = "Boutique 2"
	if code, v := appel("POST", "/api/email/comptes/"+id, compte); code != 200 {
		t.Fatalf("rename: %d %v", code, v)
	}
	compte["hote"], compte["mot_de_passe"] = "smtp.autre.fr", temoin
	if code, v := appel("POST", "/api/email/comptes/"+id, compte); code != 200 || v["hote"] != "smtp.autre.fr" {
		t.Fatalf("new server with the password: %d %v", code, v)
	}
}

// RT2: moving a project to staging or prod, where it holds keys, needs Athanor's password retyped (C25).
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
	env := func(cible, mdp string) int {
		code, _ := appel("POST", "/api/acces", map[string]string{"projet": "demo", "action": "env", "env": cible, "mot_de_passe": mdp})
		return code
	}

	if c := env("prod", ""); c != http.StatusForbidden || strings.Contains(lire(), tProd) { // no password for Athanor yet
		t.Fatalf("prod without a password set: %d\n%s", c, lire())
	}
	avecMdpPreuve(s)
	if c := env("prod", "faux"); c != http.StatusUnauthorized || strings.Contains(lire(), tProd) {
		t.Fatalf("prod with a wrong password: %d\n%s", c, lire())
	}
	if c := env("staging", ""); c != 200 { // no key on staging: nothing to prove
		t.Fatalf("staging without keys: %d", c)
	}
	if c := env("prod", mdpPreuveTest); c != 200 || !strings.Contains(lire(), tProd) {
		t.Fatalf("prod with the password: %d\n%s", c, lire())
	}
	if c := env("dev", ""); c != 200 || strings.Contains(lire(), tProd) { // going back to dev gives nothing
		t.Fatalf("back to dev: %d\n%s", c, lire())
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
