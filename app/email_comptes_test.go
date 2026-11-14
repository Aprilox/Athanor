package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestEmailComptes: add, change and delete an SMTP account through the API; the password lives only in the
// vault (type smtp, scope code), never in comptes.json nor in an answer; bad fields and a closed vault are refused.
func TestEmailComptes(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "témoin-" + aleatoire(12) // built at run time: no fake secret in the repository
	var reponses strings.Builder
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	compte := func(modifs map[string]any) map[string]any {
		c := map[string]any{"nom": "Boutique", "hote": "smtp.exemple.fr", "port": 587, "securite": "starttls",
			"utilisateur": "contact@exemple.fr", "expediteur": "Boutique <contact@exemple.fr>", "mot_de_passe": temoin}
		for k, v := range modifs {
			c[k] = v
		}
		return c
	}

	code, v := appel("POST", "/api/email/comptes", compte(nil))
	if code != 200 || v["id"] == "" || v["entree"] == "" || v["hote"] != "smtp.exemple.fr" {
		t.Fatalf("add: %d %v", code, v)
	}
	id, entree := v["id"].(string), v["entree"].(string)
	var e EntreeCoffre
	for _, x := range s.coffre.Liste() {
		if x.ID == entree {
			e = x
		}
	}
	if e.Type != "smtp" || e.Portee != "code" || e.Famille != "secret" || !strings.Contains(e.Nom, "Boutique") {
		t.Fatalf("vault entry: %+v", e)
	}
	if err := lisible(s.coffre, entree, temoin); err != nil {
		t.Fatal("password in the vault:", err)
	}

	// refusals: fixed messages, nothing written
	for nom, c := range map[string]struct {
		corps map[string]any
		err   error
	}{
		"host":          {compte(map[string]any{"hote": "smtp exemple.fr"}), errEmailHote},
		"host scheme":   {compte(map[string]any{"hote": "https://smtp.exemple.fr"}), errEmailHote},
		"port 0":        {compte(map[string]any{"port": 0}), errEmailPort},
		"port too high": {compte(map[string]any{"port": 70000}), errEmailPort},
		"no encryption": {compte(map[string]any{"securite": "none"}), errEmailSecurite},
		"security":      {compte(map[string]any{"securite": "ssl3"}), errEmailRequete},
		"sender":        {compte(map[string]any{"expediteur": "pas une adresse"}), errEmailExpediteur},
		"sender CRLF":   {compte(map[string]any{"expediteur": "a@b.fr\r\nBcc: x@y.fr"}), errEmailExpediteur},
		"user":          {compte(map[string]any{"utilisateur": "u\nv"}), errEmailUtilisateur},
		"name":          {compte(map[string]any{"nom": " "}), errEmailNom},
		"password":      {compte(map[string]any{"mot_de_passe": ""}), errEmailMdp},
	} {
		code, v := appel("POST", "/api/email/comptes", c.corps)
		if code != 400 || v["erreur"] != c.err.Error() {
			t.Errorf("%s: %d %v", nom, code, v)
		}
	}
	// no encryption towards this PC: accepted (the test send's fake server)
	code, v = appel("POST", "/api/email/comptes", compte(map[string]any{"nom": "Local", "hote": "127.0.0.1", "port": 2525, "securite": "none"}))
	if code != 200 {
		t.Fatalf("localhost without encryption: %d %v", code, v)
	}
	local := v["id"].(string)

	// change without a password: the vault keeps it; renamed, the vault entry follows
	code, v = appel("POST", "/api/email/comptes/"+id, compte(map[string]any{"nom": "Boutique pro", "port": 465, "securite": "tls", "mot_de_passe": ""}))
	if code != 200 || v["nom"] != "Boutique pro" || v["port"] != float64(465) || v["entree"] != entree {
		t.Fatalf("change: %d %v", code, v)
	}
	if err := lisible(s.coffre, entree, temoin); err != nil {
		t.Fatal("password after a change without one:", err)
	}
	// a new password replaces the vault's value
	temoin2 := "témoin-" + aleatoire(12)
	if code, v = appel("POST", "/api/email/comptes/"+id, compte(map[string]any{"nom": "Boutique pro", "mot_de_passe": temoin2})); code != 200 {
		t.Fatalf("new password: %d %v", code, v)
	}
	if err := lisible(s.coffre, entree, temoin2); err != nil {
		t.Fatal("new password:", err)
	}
	if code, _ = appel("POST", "/api/email/comptes/inconnu", compte(nil)); code != 404 {
		t.Fatalf("unknown account: %d", code)
	}

	// the code key closed: adding and changing the password are refused with 423, nothing half written
	s.coffre.Fermer()
	code, v = appel("POST", "/api/email/comptes", compte(map[string]any{"nom": "Fermé"}))
	if code != http.StatusLocked || v["erreur"] != errCoffreVerrouille.Error() {
		t.Fatalf("closed vault: %d %v", code, v)
	}
	if code, _ = appel("POST", "/api/email/comptes/"+id, compte(map[string]any{"nom": "Boutique pro"})); code != http.StatusLocked {
		t.Fatalf("new password with a closed vault: %d", code)
	}
	if l, _ := lireComptesEmail(); len(l) != 2 {
		t.Fatalf("accounts after the refusals: %+v", l)
	}
	s.coffre.Rouvrir() // the emergency flag off, the keystore opens the code key again
	if err := s.coffre.OuvrirCode(); err != nil {
		t.Fatal(err)
	}

	// the list and the file: never the password
	code, v = appel("GET", "/api/email/comptes", nil)
	if code != 200 || len(v["comptes"].([]any)) != 2 {
		t.Fatalf("list: %d %v", code, v)
	}
	if b, _ := os.ReadFile(fichierComptesEmail()); strings.Contains(string(b), temoin) || strings.Contains(string(b), temoin2) || strings.Contains(string(b), "mot_de_passe") {
		t.Fatal("comptes.json holds a password")
	}

	// delete: the vault entry goes with the account
	if code, _ = appel("DELETE", "/api/email/comptes/"+id, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	for _, x := range s.coffre.Liste() {
		if x.ID == entree {
			t.Fatal("the password stayed in the vault")
		}
	}
	if l, _ := lireComptesEmail(); len(l) != 1 || l[0].ID != local {
		t.Fatalf("accounts after the delete: %+v", l)
	}
	if code, _ = appel("DELETE", "/api/email/comptes/"+id, nil); code != 404 {
		t.Fatalf("delete twice: %d", code)
	}
	if strings.Contains(reponses.String(), temoin) || strings.Contains(reponses.String(), temoin2) {
		t.Fatal("an answer holds the password")
	}

	// from a device: every write of the module refused
	for _, c := range [][2]string{{"POST", "/api/email/comptes/" + local}, {"DELETE", "/api/email/comptes/" + local}} {
		if !refuseHubDistance(httptest.NewRequest(c[0], c[1], nil)) {
			t.Errorf("%s %s allowed from a device", c[0], c[1])
		}
	}
}

// TestEmailComptesTraduction: every refusal of the account form is translated in the 5 languages.
func TestEmailComptesTraduction(t *testing.T) {
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range append(append(errsEmail, errsEnvoi...), "The test e-mail of {0} failed: {1}") {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}
