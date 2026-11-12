package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fauxCF: a fake Cloudflare API (httptest) that records every call; jetons: token → "ok" | "expire" | "vide".
type fauxCF struct {
	mu     sync.Mutex
	appels []string
	jetons map[string]string
}

// relever: the calls received since the last time ("METHOD /path?query"), then forgotten.
func (f *fauxCF) relever() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.appels
	f.appels = nil
	return a
}

// fauxCloudflare: urlCloudflare on a fake server for the test. Zone z1 (exemple.fr) grants every right,
// zone z2 (autre.org) only DNS reads; the account acc1 lets the token read its Access applications.
func fauxCloudflare(t *testing.T) *fauxCF {
	t.Helper()
	f := &fauxCF{jetons: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appel := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			appel += "?" + r.URL.RawQuery
		}
		f.mu.Lock()
		f.appels = append(f.appels, appel)
		profil := f.jetons[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		f.mu.Unlock()
		if profil == "" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"Invalid API Token"}]}`))
			return
		}
		ok := func(corps string) { _, _ = w.Write([]byte(corps)) }
		switch appel {
		case "GET /client/v4/user/tokens/verify":
			statut := map[string]string{"expire": "expired"}[profil]
			if statut == "" {
				statut = "active"
			}
			ok(`{"success":true,"result":{"id":"t","status":"` + statut + `"}}`)
		case "GET /client/v4/zones?per_page=50":
			if profil == "vide" {
				ok(`{"success":true,"result":[]}`)
				return
			}
			ok(`{"success":true,"result":[{"id":"z1","name":"exemple.fr","account":{"id":"acc1"}},{"id":"z2","name":"autre.org","account":{"id":"acc1"}}]}`)
		case "GET /client/v4/accounts/acc1/access/apps", "GET /client/v4/zones/z1/dns_records?per_page=1",
			"GET /client/v4/zones/z2/dns_records?per_page=1", "GET /client/v4/zones/z1/settings/ssl", "GET /client/v4/certificates?zone_id=z1":
			ok(`{"success":true,"result":[]}`)
		case "POST /client/v4/zones/z1/dns_records": // permission granted, record rejected: nothing created
			w.WriteHeader(400)
			ok(`{"success":false,"errors":[{"code":9005,"message":"Content is required"}]}`)
		default:
			w.WriteHeader(403)
			ok(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
		}
	}))
	t.Cleanup(srv.Close)
	ancien := urlCloudflare
	urlCloudflare = srv.URL
	t.Cleanup(func() { urlCloudflare = ancien })
	return f
}

// TestFournisseurs: a provider added from an infra Cloudflare entry of the vault (any other refused),
// fournisseurs.json 0600 without the token, Test (the exact calls received, zones and rights deduced and
// saved, each call in the audit log), a refused / expired / zoneless token said plainly, the infra vault
// closed → 423, removal refused while an environment uses the provider, and the token in no answer.
func TestFournisseurs(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	_ = os.RemoveAll(dossierModule("servers"))
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	_ = os.RemoveAll(dossierJournalAudit())
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}}
	s.mu.Unlock()
	cf := fauxCloudflare(t)
	mux := http.NewServeMux()
	s.routes(mux)
	reponses := &strings.Builder{}
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.Write(w.Body.Bytes())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	entree := func(nom, typ, portee, valeur string) string {
		t.Helper()
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: typ, Portee: portee}, valeur)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	temoin := "témoin-" + aleatoire(12) // the token: built at run time, never in an answer or a file
	cf.jetons[temoin] = "ok"
	cle := entree("Cloudflare martin", "cloudflare", "infra", temoin)
	cleCode := entree("Cloudflare du code", "cloudflare", "code", "témoin-code-"+aleatoire(8))
	cleGH := entree("GitHub", "github", "infra", "témoin-gh-"+aleatoire(8))

	if code, v := appel("GET", "/api/fournisseurs", ""); code != 200 || len(v["fournisseurs"].([]any)) != 0 {
		t.Fatalf("no provider yet: %d %v", code, v)
	}
	for _, c := range []struct{ corps, erreur string }{
		{`{"cle":"` + cleCode + `"}`, errFournisseurPortee.Error()},
		{`{"cle":"` + cleGH + `"}`, errFournisseurCle.Error()},
		{`{"cle":"inconnue"}`, errFournisseurCle.Error()},
		{`{"cle":"` + cle + `","nom":"` + strings.Repeat("n", 101) + `"}`, errFournisseurNom.Error()},
	} {
		if code, v := appel("POST", "/api/fournisseurs", c.corps); code != 400 || v["erreur"] != c.erreur {
			t.Errorf("%s: %d %v, want 400 %q", c.corps, code, v, c.erreur)
		}
	}
	code, v := appel("POST", "/api/fournisseurs", `{"cle":"`+cle+`"}`)
	id, _ := v["id"].(string)
	if code != 200 || v["nom"] != "Cloudflare martin" || v["type"] != "cloudflare" || v["etat"] != "" || id == "" {
		t.Fatalf("added: %d %v", code, v)
	}
	b, err := os.ReadFile(fichierFournisseursChemin())
	if err != nil || !strings.Contains(string(b), cle) || strings.Contains(string(b), temoin) {
		t.Fatalf("fournisseurs.json: %v %s", err, b)
	}
	if st, _ := os.Stat(fichierFournisseursChemin()); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("fournisseurs.json mode %v", st.Mode().Perm())
	}

	// Test: verify, zones, then the probes of each zone (the account's Access apps once)
	cf.relever()
	code, v = appel("POST", "/api/fournisseurs/"+id+"/tester", "")
	attendus := []string{"GET /client/v4/user/tokens/verify", "GET /client/v4/zones?per_page=50", "GET /client/v4/accounts/acc1/access/apps"}
	for _, z := range []string{"z1", "z2"} {
		attendus = append(attendus, "GET /client/v4/zones/"+z+"/dns_records?per_page=1", "POST /client/v4/zones/"+z+"/dns_records",
			"GET /client/v4/zones/"+z+"/settings/ssl", "GET /client/v4/certificates?zone_id="+z)
	}
	if recus := cf.relever(); !slices.Equal(recus, attendus) {
		t.Errorf("calls received:\n%s\nwant:\n%s", strings.Join(recus, "\n"), strings.Join(attendus, "\n"))
	}
	var f Fournisseur
	brut, _ := json.Marshal(v)
	_ = json.Unmarshal(brut, &f)
	if code != 200 || f.Etat != "ok" || f.Compte != "acc1" || len(f.Zones) != 2 || f.Teste.IsZero() ||
		f.Bilan == nil || f.Bilan.Cle != "Cloudflare token active; zones: {0}" || f.Bilan.Args[0] != "exemple.fr, autre.org" {
		t.Fatalf("tested: %d %s", code, brut)
	}
	if !slices.Equal(f.Zones[0].Droits, droitsCloudflare) || !slices.Equal(f.Zones[1].Droits, []string{"dns:lire", "access"}) ||
		!slices.Equal(f.Droits, droitsCloudflare) || f.Zones[0].Nom != "exemple.fr" || f.Zones[0].ID != "z1" {
		t.Errorf("rights: %+v %v", f.Zones, f.Droits)
	}
	if _, v = appel("GET", "/api/fournisseurs", ""); len(v["fournisseurs"].([]any)) != 1 || v["fournisseurs"].([]any)[0].(map[string]any)["etat"] != "ok" {
		t.Errorf("the test is not saved: %v", v)
	}
	var journal strings.Builder
	_ = filepath.WalkDir(dossierJournalAudit(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			x, _ := os.ReadFile(p)
			journal.Write(x)
		}
		return nil
	})
	for _, a := range []string{`"cloudflare GET /user/tokens/verify","resultat":"200"`, `"cloudflare POST /zones/z2/dns_records","resultat":"403"`,
		`"fournisseur testé"`, `"fournisseur ajouté"`} {
		if !strings.Contains(journal.String(), a) {
			t.Errorf("audit log without %s", a)
		}
	}
	if strings.Contains(journal.String(), temoin) || strings.Contains(journal.String(), "Bearer") {
		t.Error("the token or a header in the audit log")
	}

	// a refused, an expired and a zoneless token: said plainly, saved
	for _, c := range []struct{ profil, etat, bilan string }{
		{"", "refuse", "Cloudflare refused this token."},
		{"expire", "expire", "This Cloudflare token has expired: create a new one and replace it in the Vault."},
		{"vide", "sans_zone", "This Cloudflare token sees no zone: give it access to your domain's zone."},
	} {
		valeur := "témoin-" + c.etat + "-" + aleatoire(8)
		if c.profil != "" {
			cf.jetons[valeur] = c.profil
		}
		_, v = appel("POST", "/api/fournisseurs", `{"cle":"`+entree("Cloudflare "+c.etat, "cloudflare", "infra", valeur)+`"}`)
		_, v = appel("POST", "/api/fournisseurs/"+v["id"].(string)+"/tester", "")
		if b, _ := v["bilan"].(map[string]any); v["etat"] != c.etat || b["cle"] != c.bilan || len(v["zones"].([]any)) != 0 {
			t.Errorf("%s token: %v", c.etat, v)
		}
	}

	// the infra vault closed: no test (423), nothing called
	s.coffre.Verrouiller()
	cf.relever()
	if code, _ := appel("POST", "/api/fournisseurs/"+id+"/tester", ""); code != http.StatusLocked || len(cf.relever()) != 0 {
		t.Errorf("infra vault closed: %d", code)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := appel("POST", "/api/fournisseurs/inconnu/tester", ""); code != 404 {
		t.Errorf("unknown provider tested: %d", code)
	}

	// an environment names only a known provider, which cannot be removed while it is used
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"staging","url":"https://staging.exemple.fr","protection":"aucune","politique":"accord","fournisseur":"abc123"}`); code != 400 || v["erreur"] != errEnvFourn.Error() {
		t.Errorf("unknown provider in an environment: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/projets/demo/environnements", `{"env":"staging","url":"https://staging.exemple.fr","protection":"aucune","politique":"accord","fournisseur":"`+id+`"}`); code != 200 {
		t.Fatalf("environment with the provider: %d %v", code, v)
	}
	if code, v := appel("DELETE", "/api/fournisseurs/"+id, ""); code != 409 || v["erreur_key"] != errFournisseurUtilise || v["args"].([]any)[0] != "demo/staging" {
		t.Errorf("removal of a used provider: %d %v", code, v)
	}
	if _, v := appel("GET", "/api/fournisseurs", ""); v["environnements"].(map[string]any)[id].([]any)[0] != "demo/staging" {
		t.Errorf("uses of the provider: %v", v)
	}
	appel("POST", "/api/projets/demo/environnements", `{"env":"staging","url":"https://staging.exemple.fr","protection":"aucune","politique":"accord"}`)
	if code, v := appel("DELETE", "/api/fournisseurs/"+id, ""); code != 200 || len(v["fournisseurs"].([]any)) != 3 {
		t.Errorf("removal: %d %v", code, v)
	}
	if code, _ := appel("DELETE", "/api/fournisseurs/"+id, ""); code != 404 {
		t.Errorf("second removal: %d", code)
	}
	if strings.Contains(reponses.String(), "témoin") || strings.Contains(reponses.String(), `témoin`) {
		t.Error("a token in an answer")
	}
}
