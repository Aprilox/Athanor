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

// TestEmailConfigurer (M204): « Configure sending » gives SMTP_HOST/PORT/SECURE/USER/FROM/PASSWORD to a
// project through the existing grants: secrets.env in dev, the Compose .env in staging; giving them retypes
// Athanor's password (prod too); a taken SMTP_ name refuses all; a changed account reaches the projects; a
// deleted one takes its grants back; the password never in an answer, comptes.json nor the audit log.
func TestEmailConfigurer(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	s.mu.Unlock()
	for _, p := range []string{"demo", "autre"} {
		if err := os.MkdirAll(filepath.Join(d, p, ".athanor"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "témoin-" + aleatoire(12)
	var reponses strings.Builder
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	compte := map[string]any{"nom": "Boutique", "hote": "smtp.exemple.fr", "port": 587, "securite": "starttls",
		"utilisateur": "contact@exemple.fr", "expediteur": "Boutique <contact@exemple.fr>", "mot_de_passe": temoin}
	code, v := appel("POST", "/api/email/comptes", compte)
	if code != 200 {
		t.Fatalf("add: %d %v", code, v)
	}
	id := v["id"].(string)
	configurer := func(projet, env, mdp string, retirer bool) (int, map[string]any) {
		t.Helper()
		return appel("POST", "/api/email/configurer", map[string]any{"compte": id, "projet": projet, "env": env, "mot_de_passe": mdp, "retirer": retirer})
	}
	secrets := func(projet string) map[string]string {
		t.Helper()
		b, _ := os.ReadFile(filepath.Join(d, projet, ".athanor", "secrets.env"))
		m := map[string]string{}
		for _, l := range lireLignesSecrets(b) {
			m[l.nom] = l.valeur
		}
		return m
	}

	// C25: no password set, then a wrong one: nothing given
	if c, _ := configurer("demo", "dev", "", false); c != http.StatusForbidden {
		t.Fatalf("without a password set: %d", c)
	}
	avecMdpPreuve(s)
	if c, _ := configurer("demo", "dev", "faux", false); c != http.StatusUnauthorized || len(s.acces.Projet("demo").Acces) != 0 {
		t.Fatalf("wrong password: %d %+v", c, s.acces.Projet("demo").Acces)
	}
	for nom, c := range map[string]struct {
		corps map[string]any
		code  int
	}{
		"unknown account": {map[string]any{"compte": "inconnu", "projet": "demo", "env": "dev", "mot_de_passe": mdpPreuveTest}, 404},
		"unknown project": {map[string]any{"compte": id, "projet": "inconnu", "env": "dev", "mot_de_passe": mdpPreuveTest}, 404},
		"unknown env":     {map[string]any{"compte": id, "projet": "demo", "env": "recette", "mot_de_passe": mdpPreuveTest}, 400},
	} {
		if code, v := appel("POST", "/api/email/configurer", c.corps); code != c.code {
			t.Errorf("%s: %d %v", nom, code, v)
		}
	}

	// dev: the pilot's secrets.env holds the six values; twice is the same
	for range 2 {
		if c, v := configurer("demo", "dev", mdpPreuveTest, false); c != 200 || len(v["projets"].([]any)) != 1 {
			t.Fatalf("configure dev: %d %v", c, v)
		}
	}
	attendu := map[string]string{"SMTP_HOST": "smtp.exemple.fr", "SMTP_PORT": "587", "SMTP_SECURE": "false",
		"SMTP_USER": "contact@exemple.fr", "SMTP_FROM": "Boutique <contact@exemple.fr>", "SMTP_PASSWORD": temoin}
	sec := secrets("demo")
	for n, val := range attendu {
		if sec[n] != val {
			t.Errorf("secrets.env %s = %q, want %q", n, sec[n], val)
		}
	}
	if n := len(s.acces.Projet("demo").Acces); n != 6 {
		t.Errorf("grants after two configures: %d", n)
	}

	// staging: the Compose .env of a deployment; prod: the password again
	if c, v := configurer("demo", "staging", mdpPreuveTest, false); c != 200 {
		t.Fatalf("configure staging: %d %v", c, v)
	}
	env, err := s.fichierEnvCompose("demo", "staging")
	if err != nil || !strings.Contains(env, "SMTP_PASSWORD='"+temoin+"'\n") || !strings.Contains(env, "SMTP_HOST='smtp.exemple.fr'\n") || strings.Count(env, "SMTP_") != 6 {
		t.Errorf("Compose .env of staging: %v %q", err, strings.ReplaceAll(env, temoin, "TEMOIN"))
	}
	if c, _ := configurer("demo", "prod", "faux", false); c != http.StatusUnauthorized {
		t.Errorf("prod with a wrong password: %d", c)
	}
	if c, _ := configurer("demo", "prod", mdpPreuveTest, false); c != 200 {
		t.Errorf("prod: %d", c)
	}

	// another SMTP_HOST already given to autre/dev: refused, nothing given
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "SMTP_HOST", Famille: "variable", Type: "autre"}, "mail.ailleurs.fr")
	if err == nil {
		_, err = s.acces.Ajouter("autre", e, "dev", "SMTP_HOST", "")
	}
	if err != nil {
		t.Fatal(err)
	}
	if c, v := configurer("autre", "dev", mdpPreuveTest, false); c != http.StatusConflict || v["erreur"] != errEmailConflit.Error() || len(s.acces.Projet("autre").Acces) != 1 {
		t.Errorf("conflict: %d %v %+v", c, v, s.acces.Projet("autre").Acces)
	}

	// a changed account reaches the projects it sends for
	compte["port"], compte["securite"], compte["mot_de_passe"] = 465, "tls", temoin // another server: retyped (RT1)
	if c, v := appel("POST", "/api/email/comptes/"+id, compte); c != 200 {
		t.Fatalf("change: %d %v", c, v)
	}
	if sec := secrets("demo"); sec["SMTP_PORT"] != "465" || sec["SMTP_SECURE"] != "true" || sec["SMTP_PASSWORD"] != temoin {
		t.Errorf("after the change: port %q secure %q", sec["SMTP_PORT"], sec["SMTP_SECURE"])
	}

	// the list says where it sends (the delete confirmation names them)
	_, v = appel("GET", "/api/email/comptes", nil)
	cs := v["comptes"].([]any)
	if len(cs) != 1 || len(cs[0].(map[string]any)["projets"].([]any)) != 3 || len(v["projets_hub"].([]any)) != 2 {
		t.Errorf("GET: %v", v)
	}

	// taking back staging; a second time is unknown
	if c, _ := configurer("demo", "staging", "", true); c != 200 {
		t.Errorf("remove staging: %d", c)
	}
	if env, _ := s.fichierEnvCompose("demo", "staging"); strings.Contains(env, "SMTP_") {
		t.Errorf("staging after the removal: %d SMTP_ lines", strings.Count(env, "SMTP_"))
	}
	if c, _ := configurer("demo", "staging", "", true); c != 404 {
		t.Errorf("remove twice: %d", c)
	}

	// M304: no environment named = the whole project, given and taken back at once
	if c, v := configurer("demo", "", mdpPreuveTest, false); c != 200 || len(v["projets"].([]any)) != 3 {
		t.Fatalf("configure the project: %d %v", c, v)
	}
	if c, v := configurer("demo", "", "", true); c != 200 || len(v["projets"].([]any)) != 0 {
		t.Errorf("remove the project: %d %v", c, v)
	}
	if c, _ := configurer("demo", "", "", true); c != 404 {
		t.Errorf("remove the project twice: %d", c)
	}
	if c, v := configurer("demo", "", mdpPreuveTest, false); c != 200 || len(v["projets"].([]any)) != 3 {
		t.Fatalf("configure the project again: %d %v", c, v)
	}

	// deleting the account takes every grant back and its variables leave the vault
	if c, v := appel("DELETE", "/api/email/comptes/"+id, nil); c != 200 {
		t.Fatalf("delete: %d %v", c, v)
	}
	for n := range secrets("demo") {
		if strings.HasPrefix(n, "SMTP_") {
			t.Errorf("secrets.env keeps %s after the delete", n)
		}
	}
	if a := s.acces.Projet("demo").Acces; len(a) != 0 {
		t.Errorf("grants after the delete: %+v", a)
	}
	for _, x := range s.coffre.Liste() {
		if strings.HasPrefix(x.Source, "email:") {
			t.Errorf("vault keeps %s", x.Nom)
		}
	}

	// the password: never in an answer, comptes.json nor the audit log
	if strings.Contains(reponses.String(), temoin) {
		t.Error("the password is in an API answer")
	}
	if b, _ := os.ReadFile(fichierComptesEmail()); strings.Contains(string(b), temoin) {
		t.Error("the password is in comptes.json")
	}
	journal := ""
	fichiers, _ := filepath.Glob(filepath.Join(dossierJournalAudit(), "*.jsonl"))
	for _, f := range fichiers {
		b, _ := os.ReadFile(f)
		journal += string(b)
	}
	if strings.Contains(journal, temoin) || strings.Count(journal, "envoi e-mail configuré") < 4 || !strings.Contains(journal, "envoi e-mail retiré") {
		t.Errorf("audit log: %d configured", strings.Count(journal, "envoi e-mail configuré"))
	}
}
