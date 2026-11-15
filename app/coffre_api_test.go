package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveurCoffreTest: a server with a fake keystore, in an isolated Athanor folder.
func serveurCoffreTest(t *testing.T) *Serveur {
	t.Helper()
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	sys := nouveauFauxSysteme()
	ancien := systemeCoffre
	systemeCoffre = func() enveloppeSysteme { return sys }
	t.Cleanup(func() { systemeCoffre = ancien })
	// the config (password) is written next to the test binary: later tests must not inherit it
	cfg := cheminConfig()
	if avant, err := os.ReadFile(cfg); err == nil {
		t.Cleanup(func() { _ = os.WriteFile(cfg, avant, 0o600) })
	} else {
		t.Cleanup(func() { _ = os.Remove(cfg) })
	}
	s := nouveauServeur(Config{Kit: filepath.Join(d, "kit")})
	if s.coffre == nil || s.coffreErr != nil {
		t.Fatal("vault not loaded:", s.coffreErr)
	}
	// a config left by an earlier test makes the test binary's folder Athanor's: drop the vault written there
	t.Cleanup(func() { _ = os.Remove(s.coffre.chemin) })
	_ = os.RemoveAll(dossierJournalAudit()) // the audit log is written there too: each test starts with an empty one
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	for _, f := range []string{"approbations.json", "regles.json"} { // the approvals too
		_ = os.Remove(filepath.Join(dossierApprobations(), f))
		t.Cleanup(func() { _ = os.Remove(filepath.Join(dossierApprobations(), f)) })
	}
	s.chargerApprobations()
	// the hub's session tokens, project accesses and missions too: a test must not depend on which tests ran
	// before it in this process (check.sh splits the tests over several processes)
	for _, f := range []string{cheminSessions(), cheminAcces(), dossierMissions()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	s.chargerSessions()
	s.chargerAcces()
	s.csrf, s.cleAcces = "jeton", "cle"
	return s
}

// TestCoffreTemoinNullePart walks the whole API (list, add, change, delete, 400/423 errors, hub tile)
// with a witness value, then checks it is in no answer and no log.
func TestCoffreTemoinNullePart(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "TEMOIN-" + aleatoire(12) // built at run time: no fake secret in the repository
	var reponses strings.Builder
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	entree := func(m map[string]string) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	// empty vault: nothing on disk, the state says no password
	code, v := appel("GET", "/api/coffre", "")
	if code != 200 || len(v["entrees"].([]any)) != 0 || v["etat"].(map[string]any)["mot_de_passe"] != false {
		t.Fatalf("empty vault: %d %v", code, v)
	}

	code, v = appel("POST", "/api/coffre", entree(map[string]string{"nom": "API_URL", "famille": "variable", "type": "autre", "valeur": "https://x.test"}))
	if code != 200 || v["valeur"] != "https://x.test" || v["portee"] != "code" {
		t.Fatalf("variable: %d %v", code, v)
	}
	code, v = appel("POST", "/api/coffre", entree(map[string]string{"nom": "ANTHROPIC_API_KEY", "famille": "secret", "type": "anthropic", "note": "pour l'IA", "valeur": temoin}))
	if code != 200 || v["valeur"] != "" || v["portee"] != "code" || v["id"] == "" {
		t.Fatalf("secret: %d %v", code, v)
	}
	id := v["id"].(string)
	// the same name in the same scope (here the default one): refused, nothing added
	if code, v = appel("POST", "/api/coffre", entree(map[string]string{"nom": "API_URL", "famille": "variable", "type": "autre", "valeur": "y"})); code != http.StatusBadRequest || v["erreur"] != "An entry with this name already exists in this scope" {
		t.Fatalf("duplicate: %d %v", code, v)
	}

	// infra without a password: refused, the key is not there
	code, v = appel("POST", "/api/coffre", entree(map[string]string{"nom": "SSH", "famille": "secret", "type": "ssh", "valeur": temoin}))
	if code != http.StatusLocked || v["erreur"] != errCoffreSansMdp.Error() {
		t.Fatalf("infra without a password: %d %v", code, v)
	}

	// input validation (every refusal is a fixed message, never the input)
	for nom, corps := range map[string]string{
		"name too long":   entree(map[string]string{"nom": strings.Repeat("é", 101), "famille": "secret", "type": "jeton", "valeur": temoin}),
		"value too large": entree(map[string]string{"nom": "X", "famille": "secret", "type": "jeton", "valeur": strings.Repeat("a", 64<<10+1)}),
		"no value":        entree(map[string]string{"nom": "X", "famille": "secret", "type": "jeton"}),
		"empty value":     entree(map[string]string{"nom": "X", "famille": "secret", "type": "jeton", "valeur": ""}),
		"no name":         entree(map[string]string{"nom": " ", "famille": "secret", "type": "jeton", "valeur": temoin}),
		"family":          entree(map[string]string{"nom": "X", "famille": "cle", "type": "jeton", "valeur": temoin}),
		"type":            entree(map[string]string{"nom": "X", "famille": "secret", "type": "inconnu", "valeur": temoin}),
		"scope":           entree(map[string]string{"nom": "X", "famille": "secret", "type": "jeton", "portee": "partout", "valeur": temoin}),
		"infra variable":  entree(map[string]string{"nom": "X", "famille": "variable", "type": "ssh", "portee": "infra", "valeur": "y"}),
		"bad JSON":        `{"nom":"X","valeur":` + temoin + `}`,
		"note too long":   entree(map[string]string{"nom": "X", "famille": "secret", "type": "jeton", "note": strings.Repeat("n", 2001), "valeur": temoin}),
	} {
		if code, v := appel("POST", "/api/coffre", corps); code != http.StatusBadRequest || v["erreur"] == "" {
			t.Errorf("%s: %d %v, want 400", nom, code, v)
		}
	}
	if code, _ := appel("POST", "/api/coffre", entree(map[string]string{"nom": strings.Repeat("é", 100), "famille": "secret", "type": "jeton", "valeur": strings.Repeat("a", 64<<10)})); code != 200 {
		t.Errorf("100 characters and 64 KiB are allowed: %d", code)
	}

	// metadata, then a new value
	code, v = appel("POST", "/api/coffre/"+id, `{"note":"clé de test"}`)
	if code != 200 || v["note"] != "clé de test" || v["nom"] != "ANTHROPIC_API_KEY" || v["valeur"] != "" {
		t.Fatalf("metadata: %d %v", code, v)
	}
	if code, v = appel("POST", "/api/coffre/"+id, `{"valeur":"`+temoin+`-2"}`); code != 200 || v["valeur"] != "" {
		t.Fatalf("new value: %d %v", code, v)
	}
	if err := lisible(s.coffre, id, temoin+"-2"); err != nil {
		t.Fatal(err)
	}
	if code, _ = appel("POST", "/api/coffre/"+id, `{"type":"inconnu"}`); code != http.StatusBadRequest {
		t.Errorf("bad type on change: %d", code)
	}
	if code, _ = appel("POST", "/api/coffre/0123456789abcdef", `{"note":"x"}`); code != http.StatusNotFound {
		t.Errorf("unknown entry: %d", code)
	}
	if code, _ = appel("DELETE", "/api/coffre/0123456789abcdef", ""); code != http.StatusNotFound {
		t.Errorf("delete an unknown entry: %d", code)
	}

	// with a password: infra opens with Athanor, then locking closes it (423, the list still reads)
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	s.config.MotDePasseHash, s.session = hacherMotDePasse("mdp-test"), "session"
	code, v = appel("POST", "/api/coffre", entree(map[string]string{"nom": "SSH_PROD", "famille": "secret", "type": "ssh", "valeur": temoin}))
	if code != 200 || v["portee"] != "infra" {
		t.Fatalf("infra entry: %d %v", code, v)
	}
	infra := v["id"].(string)
	// C35: infra to code reopens it alone on this PC: the owner's password, WSL mode too; logged
	natif := modeNatif.Load()
	t.Cleanup(func() { modeNatif.Store(natif) })
	for _, m := range []bool{true, false} {
		modeNatif.Store(m)
		for _, corps := range []string{`{"portee":"code"}`, `{"portee":"code","mot_de_passe":"faux"}`} {
			if code, v = appel("POST", "/api/coffre/"+infra, corps); code != http.StatusUnauthorized || v["portee"] != nil {
				t.Errorf("infra to code without proof (native %v): %d %v", m, code, v)
			}
		}
	}
	if code, v = appel("POST", "/api/coffre/"+infra, `{"portee":"code","mot_de_passe":"mdp-test"}`); code != 200 || v["portee"] != "code" {
		t.Errorf("infra to code with the password: %d %v", code, v)
	}
	if code, v = appel("POST", "/api/coffre/"+infra, `{"portee":"infra"}`); code != 200 || v["portee"] != "infra" {
		t.Errorf("code to infra: %d %v", code, v)
	}
	if j := journalAuditTexte(t); strings.Count(j, "portée infra → code refusée") != 4 || strings.Count(j, `"portée infra → code"`) != 1 {
		t.Errorf("infra to code not logged: %s", j)
	}
	s.coffre.Verrouiller()
	code, v = appel("GET", "/api/coffre", "")
	etat, _ := v["etat"].(map[string]any)
	if code != 200 || len(v["entrees"].([]any)) != 4 || etat["infra"] != false || etat["code"] != true || etat["systeme"] != true || etat["mot_de_passe"] != true {
		t.Fatalf("locked list: %d %v", code, v)
	}
	if code, v = appel("POST", "/api/coffre/"+infra, `{"valeur":"`+temoin+`"}`); code != http.StatusLocked || v["erreur"] != errCoffreVerrouille.Error() {
		t.Errorf("infra value while locked: %d %v", code, v)
	}
	if code, _ = appel("POST", "/api/coffre/"+infra, `{"portee":"code","mot_de_passe":"mdp-test"}`); code != http.StatusLocked {
		t.Errorf("infra scope change while locked: %d", code)
	}
	if code, _ = appel("DELETE", "/api/coffre/"+infra, ""); code != 200 {
		t.Errorf("delete: %d", code)
	}
	if code, v = appel("GET", "/api/coffre", ""); len(v["entrees"].([]any)) != 3 {
		t.Errorf("after delete: %d %v", code, v)
	}

	// the hub shows the vault's tile
	code, v = appel("GET", "/api/hub", "")
	var tuile map[string]any
	for _, m := range v["modules"].([]any) {
		if mm := m.(map[string]any); mm["id"] == "coffre" {
			tuile = mm
		}
	}
	if code != 200 || tuile == nil || tuile["pages"].([]any)[0].(map[string]any)["hash"] != "#coffre" {
		t.Fatalf("vault in /api/hub: %d %v", code, v)
	}
	stats := tuile["resume"].(map[string]any)["stats"].([]any)
	if stats[0].(map[string]any)["valeur"] != 2.0 || stats[1].(map[string]any)["valeur"] != 1.0 || stats[2].(map[string]any)["valeur"] != "Locked" {
		t.Errorf("vault tile: %v", stats)
	}

	// the witness value: in no answer and no log, in any common encoding
	tout := reponses.String()
	for _, logs := range []string{filepath.Join(dossierAthanor(), "logs")} {
		_ = filepath.Walk(logs, func(p string, i os.FileInfo, err error) error {
			if err == nil && !i.IsDir() {
				b, _ := os.ReadFile(p)
				tout += string(b)
			}
			return nil
		})
	}
	for _, forme := range []string{temoin, base64.StdEncoding.EncodeToString([]byte(temoin)), hex.EncodeToString([]byte(temoin)), temoin[7:]} {
		if strings.Contains(tout, forme) {
			t.Fatalf("the witness value leaks (%q)", forme)
		}
	}
}

// TestHubTraductionModules: every module's name, menu labels, figure keys and text figures exist in
// the 5 languages (the web and the terminal translate them from i18n.js).
func TestHubTraductionModules(t *testing.T) {
	s := serveurCoffreTest(t)
	cles := []string{"Locked", "Unlocked", "No password"} // every text figure of the vault tile
	for _, m := range modulesHub() {
		cles = append(cles, m.Nom())
		for _, p := range m.Pages() {
			cles = append(cles, p.Libelle)
		}
		for _, e := range m.Tutoriel() { // the tours, in the 5 languages (TestTutoriels)
			cles = append(cles, e.Titre, e.Texte)
		}
		for _, st := range m.Resume(s).Stats {
			cles = append(cles, st.Cle)
			if v, ok := st.Valeur.(string); ok && m.ID() != "dev" { // Dev's quota status has its own labels
				cles = append(cles, v)
			}
		}
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range cles {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}
