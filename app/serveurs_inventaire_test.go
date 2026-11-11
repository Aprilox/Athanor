package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestServeursInventaire: the Servers module's tile, the inventory API (list, one server with its sheet,
// rename, roles, remove), serveurs.json 0600 without any key value, and a vault entry used by a server
// that cannot be removed (the server's name in the refusal) until the server is gone.
func TestServeursInventaire(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierModule("servers")) // the test binary's folder may hold an earlier run's
	t.Cleanup(func() { _ = os.RemoveAll(dossierModule("servers")) })
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	tuile := func() ResumeModule { return moduleServeurs{}.Resume(s) }

	if code, v := appel("GET", "/api/serveurs", ""); code != 200 || len(v["serveurs"].([]any)) != 0 {
		t.Fatalf("empty inventory: %d %v", code, v)
	}
	if r := tuile(); r.Etat != "inactif" || r.Stats[0].Valeur != 0 {
		t.Errorf("empty tile: %+v", r)
	}

	temoin := "TEMOIN-" + aleatoire(12) // the key's value: built at run time
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Clé du VPS", Famille: "secret", Type: "autre", Portee: "code"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{aleatoire(8), aleatoire(8)}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l,
			ServeurInventaire{ID: ids[0], Nom: "vps-1", Hote: "203.0.113.5", Port: 22, Utilisateur: "athanor", Cle: cle.ID, EmpreinteHote: "SHA256:abc", Roles: []string{"prod"}, Cree: time.Now(), Etat: "ok"},
			ServeurInventaire{ID: ids[1], Nom: "vps-2", Hote: "203.0.113.6", Port: 2222, Utilisateur: "deploy", Cle: "autre", Cree: time.Now(), Etat: "injoignable"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if r := tuile(); r.Etat != "attention" || r.Stats[0].Valeur != 2 || r.Stats[1].Valeur != 1 || r.Besoin != 1 {
		t.Errorf("tile with an unreachable server: %+v", r)
	}
	fichier := filepath.Join(dossierModule("servers"), "serveurs.json")
	if fi, err := os.Stat(fichier); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("serveurs.json: %v %v", fi, err)
	}

	// one server: no sheet yet, then the sheet written in its folder
	if code, v := appel("GET", "/api/serveurs/"+ids[0], ""); code != 200 || v["fiche"] != nil || v["serveur"].(map[string]any)["nom"] != "vps-1" {
		t.Errorf("one server: %d %v", code, v)
	}
	_ = os.MkdirAll(filepath.Join(dossierModule("servers"), ids[0]), 0o700)
	if err := os.WriteFile(filepath.Join(dossierModule("servers"), ids[0], "fiche.json"), []byte(`{"systeme":"Debian 12"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, v := appel("GET", "/api/serveurs/"+ids[0], ""); v["fiche"].(map[string]any)["systeme"] != "Debian 12" {
		t.Errorf("sheet: %v", v)
	}
	if code, _ := appel("GET", "/api/serveurs/inconnu", ""); code != 404 {
		t.Errorf("unknown server: %d", code)
	}

	// rename and roles, with their checks
	if code, v := appel("POST", "/api/serveurs/"+ids[1], `{"nom":"  staging  ","roles":["prod","dev","prod"]}`); code != 200 ||
		v["serveur"].(map[string]any)["nom"] != "staging" || len(v["serveur"].(map[string]any)["roles"].([]any)) != 2 {
		t.Errorf("rename: %d %v", code, v)
	}
	for _, c := range []struct{ corps, cle string }{{`{"nom":" "}`, "Give the server a name"}, {`{"roles":["root"]}`, "Unknown server role"},
		{`{"nom":"` + strings.Repeat("x", 101) + `"}`, "The name is too long (100 characters at most)"}} {
		if code, v := appel("POST", "/api/serveurs/"+ids[1], c.corps); code != 400 || v["erreur_key"] != c.cle {
			t.Errorf("%s: %d %v", c.corps, code, v)
		}
	}
	if code, _ := appel("POST", "/api/serveurs/inconnu", `{"nom":"x"}`); code != 404 {
		t.Errorf("rename unknown: %d", code)
	}

	// the vault entry a server uses stays, the refusal names the server
	code, v := appel("DELETE", "/api/coffre/"+cle.ID, "")
	if code != http.StatusConflict || !strings.Contains(v["erreur"].(string), "vps-1") || v["args"].([]any)[0] != "vps-1" {
		t.Fatalf("used key removed: %d %v", code, v)
	}
	if code, _ := appel("DELETE", "/api/serveurs/"+ids[0], ""); code != 200 {
		t.Fatalf("remove server: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dossierModule("servers"), ids[0])); !os.IsNotExist(err) {
		t.Errorf("the server's folder stays: %v", err)
	}
	if code, _ := appel("DELETE", "/api/serveurs/"+ids[0], ""); code != 404 {
		t.Errorf("removed twice: %d", code)
	}
	if code, _ := appel("DELETE", "/api/coffre/"+cle.ID, ""); code != 200 {
		t.Errorf("unused key not removed: %d", code)
	}

	b, _ := os.ReadFile(fichier)
	if strings.Contains(string(b), temoin) || !strings.Contains(string(b), `"version": 1`) || !strings.Contains(string(b), "staging") {
		t.Errorf("serveurs.json: %s", b)
	}
	// an unreadable file: a clear error, and no vault entry can be removed blindly
	_ = os.WriteFile(fichier, []byte("{"), 0o600)
	if code, v := appel("GET", "/api/serveurs", ""); code != 500 || v["erreur_key"] != "The servers file cannot be read" {
		t.Errorf("damaged file: %d %v", code, v)
	}
}
