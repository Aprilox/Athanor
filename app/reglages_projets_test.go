package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reglagesTest: projects demo and autre, the config on disk (reglageProjet reads it) and MODEL=sonnet in the kit's
// .env. appel calls /api/projets/{projet}/reglages from the window, or from a paired device when appareil is named.
func reglagesTest(t *testing.T) (*Serveur, func(methode, projet, appareil string, corps map[string]any) (int, map[string]any)) {
	t.Helper()
	s := serveurCoffreTest(t)
	_ = os.Remove(fichierReglagesProjets())
	t.Cleanup(func() { _ = os.Remove(fichierReglagesProjets()) })
	d := t.TempDir()
	s.mu.Lock()
	s.config.Projets = []Projet{{Nom: "demo", Dossier: filepath.Join(d, "demo")}, {Nom: "autre", Dossier: filepath.Join(d, "autre")}}
	err := ecrireConfig(s.config)
	kit := s.config.Kit
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kit, ".env"), []byte("MODEL=sonnet\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return s, func(methode, projet, appareil string, corps map[string]any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, "/api/projets/"+projet+"/reglages", strings.NewReader(string(b)))
		r.SetPathValue("nom", projet)
		if appareil != "" {
			r = r.WithContext(context.WithValue(r.Context(), cleContexte(nomCleAppareil), appareil))
		}
		w := httptest.NewRecorder()
		if methode == "GET" {
			s.apiReglagesProjet(w, r)
		} else {
			s.apiReglageProjetModifier(w, r)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
}

// TestReglageProjetResolution (M280, note 01h48): the one rule, the project's own value when it has a valid one,
// else the global one; back to the default, invalid values refused, each change in the audit log with the value
// before and after; the settings follow the project's new name and leave with it.
func TestReglageProjetResolution(t *testing.T) {
	s, appel := reglagesTest(t)
	vue := func(v map[string]any, cle string) map[string]any {
		t.Helper()
		l, _ := v["reglages"].([]any)
		for _, x := range l {
			if m, _ := x.(map[string]any); m["cle"] == cle {
				return m
			}
		}
		t.Fatalf("no setting %q in %v", cle, v)
		return nil
	}
	// by default: the global value (a project's models are no setting here: its Working mode, modeles_projet.go)
	code, v := appel("GET", "demo", "", nil)
	if m := vue(v, "permission"); code != 200 || m["valeur"] != "manuel" || m["propre"] != "" || m["defaut"] != "manuel" {
		t.Fatalf("by default: %d %v", code, m)
	}
	if len(v["reglages"].([]any)) != len(clesReglagesProjet) || vue(v, "quota_five_hour")["valeur"] != "0" {
		t.Errorf("the global values: %v", v["reglages"])
	}
	if c, _ := appel("GET", "inconnu", "", nil); c != http.StatusNotFound {
		t.Errorf("unknown project: %d", c)
	}
	// its own value: this project only
	code, v = appel("POST", "demo", "", map[string]any{"cle": "permission", "valeur": "auto"})
	if m := vue(v, "permission"); code != 200 || m["valeur"] != "auto" || m["propre"] != "auto" || m["defaut"] != "manuel" {
		t.Fatalf("own value: %d %v", code, v)
	}
	if a, b := reglageProjet("demo", "permission"), reglageProjet("autre", "permission"); a != "auto" || b != "manuel" {
		t.Errorf("demo %q (want its own auto), autre %q (want the global manuel)", a, b)
	}
	// invalid: refused, nothing changes (a model is no longer a setting of this list)
	for _, c := range []map[string]any{{"cle": "modele", "valeur": "opus"}, {"cle": "langue", "valeur": "fr"}, {"cle": "permission", "valeur": "tout"},
		{"cle": "quota_five_hour", "valeur": "80:plus"}, {"cle": "quota_five_hour", "valeur": "101:apres"}, {"cle": "quota_mois", "valeur": "80:apres"}} {
		if code, v := appel("POST", "demo", "", c); code != http.StatusBadRequest || v["error"] == nil && v["erreur"] == nil && v["message_key"] == nil {
			t.Errorf("%v: %d %v, want 400 with its message", c, code, v)
		}
	}
	for _, c := range []map[string]any{{"cle": "quota_five_hour", "valeur": "80:apres"}, {"cle": "quota_modele", "valeur": "0"}} {
		if code, v := appel("POST", "demo", "", c); code != 200 || vue(v, c["cle"].(string))["valeur"] != c["valeur"] {
			t.Errorf("%v: %d %v", c, code, v)
		}
	}
	if a := reglageProjet("demo", "permission"); a != "auto" {
		t.Errorf("after the refusals: %q", a)
	}
	// a value written in the file that is no longer valid follows the default
	m := lireReglagesProjets()
	m["autre"] = map[string]string{"permission": "tout"}
	if err := ecrireReglagesProjets(m); err != nil {
		t.Fatal(err)
	}
	if b := reglageProjet("autre", "permission"); b != "manuel" {
		t.Errorf("invalid stored value: %q, want the global one", b)
	}
	// the settings follow the project's name
	deplacerReglagesProjet("demo", "demo2")
	if a, b := reglageProjet("demo2", "permission"), reglageProjet("demo", "permission"); a != "auto" || b != "manuel" {
		t.Errorf("after the renaming: demo2 %q, demo %q", a, b)
	}
	deplacerReglagesProjet("demo2", "demo")
	// back to the default
	code, v = appel("POST", "demo", "", map[string]any{"cle": "permission", "valeur": "defaut"})
	if m := vue(v, "permission"); code != 200 || m["valeur"] != "manuel" || m["propre"] != "" {
		t.Fatalf("back to the default: %d %v", code, v)
	}
	if _, ok := lireReglagesProjets()["demo"]["permission"]; ok {
		t.Error("the value given back is still in the file")
	}
	// the audit: project, setting, before and after
	b := journalAuditTexte(t)
	for _, x := range []string{`"projet":"demo"`, "reglage du projet: permission", "defaut → auto", "auto → defaut", "reglage du projet: quota_five_hour", "defaut → 80:apres"} {
		if !strings.Contains(b, x) {
			t.Errorf("audit: %q missing", x)
		}
	}
	if strings.Contains(b, "→ opus") {
		t.Error("audit: a refused value was logged as a change")
	}
	// and leave with the project
	if err := s.retirerProjet(Projet{Nom: "demo", Dossier: s.config.Projets[0].Dossier}); err != nil {
		t.Fatal(err)
	}
	if _, ok := lireReglagesProjets()["demo"]; ok {
		t.Error("the removed project's settings are still in the file")
	}
}

// TestReglagesProjetsExistants (M280): without the file, every project follows the global values as before the
// update, and reading or asking for the default writes nothing.
func TestReglagesProjetsExistants(t *testing.T) {
	s, appel := reglagesTest(t)
	s.mu.Lock()
	s.config.PermissionsUsage = map[string]string{"missions": "auto"}
	err := ecrireConfig(s.config)
	k := s.kitCourant()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fichierReglesQuota(k), []byte(`{"seven_day":{"seuil":90,"action":"pause"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k.Env(), []byte("MODEL=opus\nFALLBACK_MODEL=sonnet\nSUBAGENT_MODEL=auto\nCHEAP_MODEL=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	attendu := map[string]string{"quota_five_hour": "0", "quota_seven_day": "90:pause", "quota_modele": "0", "permission": "auto"}
	if len(attendu) != len(clesReglagesProjet) {
		t.Fatalf("%d settings expected, %d known", len(attendu), len(clesReglagesProjet))
	}
	for _, p := range []string{"demo", "autre"} {
		for cle, v := range attendu {
			if got := reglageProjet(p, cle); got != v {
				t.Errorf("%s, %s: %q, want the global %q", p, cle, got, v)
			}
		}
	}
	if code, _ := appel("GET", "demo", "", nil); code != 200 {
		t.Errorf("GET: %d", code)
	}
	if code, _ := appel("POST", "demo", "", map[string]any{"cle": "quota_modele", "valeur": ""}); code != 200 {
		t.Errorf("POST the default: %d", code)
	}
	if _, err := os.Stat(fichierReglagesProjets()); !os.IsNotExist(err) {
		t.Errorf("a file was written for projects that chose nothing: %v", err)
	}
	if b := journalAuditTexte(t); strings.Contains(b, "reglage du projet") {
		t.Error("audit: a change logged although nothing changed")
	}
}

// TestModeProjet (M280): the permission mode of a project's tasks is the project's own setting, else Settings ›
// Permissions, else manuel; Ignore permissions only from Athanor's window, confirmed, with the password.
func TestModeProjet(t *testing.T) {
	s, appel := reglagesTest(t)
	choisir := func(appareil, mode string, plus map[string]any) int {
		t.Helper()
		c := map[string]any{"cle": "permission", "valeur": mode}
		for k, v := range plus {
			c[k] = v
		}
		code, _ := appel("POST", "demo", appareil, c)
		return code
	}
	if m := modeProjet("demo"); m != "manuel" {
		t.Errorf("by default: %q, want manuel", m)
	}
	s.mu.Lock()
	s.config.PermissionsUsage = map[string]string{"missions": "auto"}
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if m := modeProjet("demo"); m != "auto" {
		t.Errorf("Settings › Permissions: %q, want auto", m)
	}
	if c := choisir("", "modifications", nil); c != 200 || modeProjet("demo") != "modifications" || modeProjet("autre") != "auto" {
		t.Errorf("own value: %d, demo %q, autre %q", c, modeProjet("demo"), modeProjet("autre"))
	}
	if c := choisir("tel", "auto", nil); c != 200 || modeProjet("demo") != "auto" { // a mode that still asks: chosen from a device too
		t.Errorf("auto from a device: %d, %q", c, modeProjet("demo"))
	}

	// a task for the project follows it; Manual keeps the plan rules ("")
	e := &tacheEnCours{t: TacheServeurs{ID: "t280", Env: "demo/dev"}, hache: "h280"}
	tachesEnCours.Lock()
	tachesEnCours.m[e.t.ID] = e
	tachesEnCours.Unlock()
	t.Cleanup(func() { tachesEnCours.Lock(); delete(tachesEnCours.m, "t280"); tachesEnCours.Unlock() })
	x := SessionHub{Mission: "t280", Hache: "h280"}
	if m := modeAutorisation(x, "srv"); m != "auto" {
		t.Errorf("a task of the project: mode %q, want the project's auto", m)
	}
	if c := choisir("", "manuel", nil); c != 200 || modeAutorisation(x, "srv") != "" {
		t.Errorf("manuel: %d, task mode %q, want the plan rules", c, modeAutorisation(x, "srv"))
	}
	if m := modeAutorisation(SessionHub{Mission: "t280", Hache: "autre"}, "srv"); m != "" {
		t.Errorf("another session: mode %q", m)
	}

	// Ignore permissions: no password for Athanor, not confirmed, wrong password, from a device: refused
	confirme := map[string]any{"confirmer": true, "mot_de_passe": mdpPreuveTest}
	if c := choisir("", "ignorer", confirme); c != http.StatusForbidden {
		t.Errorf("ignorer without a password for Athanor: %d, want 403", c)
	}
	avecMdpPreuve(s)
	if c := choisir("", "ignorer", map[string]any{"mot_de_passe": mdpPreuveTest}); c != http.StatusConflict {
		t.Errorf("ignorer not confirmed: %d, want 409", c)
	}
	if c := choisir("", "ignorer", map[string]any{"confirmer": true, "mot_de_passe": "faux"}); c != http.StatusUnauthorized {
		t.Errorf("ignorer with a wrong password: %d, want 401", c)
	}
	if c := choisir("tel", "ignorer", confirme); c != http.StatusForbidden {
		t.Errorf("ignorer from a device: %d, want 403", c)
	}
	if m := modeProjet("demo"); m != "manuel" {
		t.Errorf("after the refusals: %q, want manuel", m)
	}
	if c := choisir("", "ignorer", confirme); c != 200 || modeProjet("demo") != "ignorer" || modeAutorisation(x, "srv") != "ignorer" {
		t.Errorf("ignorer with the proof: %d, %q", c, modeProjet("demo"))
	}
	// M311d: the audit says whose mode let a call run: the project's task here, the terminal's assistant otherwise
	if o, a := origineMode(x), origineMode(SessionHub{Hache: "aucune"}); o != "de la tâche" || a != "de l'assistant" {
		t.Errorf("origin of the mode: task %q, no task %q", o, a)
	}
	if b := journalAuditTexte(t); !strings.Contains(b, "reglage du projet: permission") || !strings.Contains(b, "manuel → ignorer") || strings.Contains(b, mdpPreuveTest) {
		t.Error("audit: the change to ignorer is missing, or the password is in the log")
	}
}
