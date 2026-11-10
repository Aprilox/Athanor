package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestCoffreMigration: an old config (3 projects, 2 MCP servers, a kit .env) is imported once over two
// start-ups, undone exactly, not imported again at the next start-up, then imported again on demand; the
// old files never change and no value reaches an answer.
func TestCoffreMigration(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	sys := nouveauFauxSysteme()
	ancienSys, ancienAuto := systemeCoffre, migrationAuto
	systemeCoffre, migrationAuto = func() enveloppeSysteme { return sys }, true
	t.Cleanup(func() { systemeCoffre, migrationAuto = ancienSys, ancienAuto })

	// tokens made at run time (the secret scan only exempts the tests' witness pattern)
	gh := "ghp_" + strings.Repeat("a1B2", 9)
	valeurs := map[string]string{ // name → value expected in the vault
		"STRIPE_SECRET_KEY":            "sk_test_" + strings.Repeat("Zq9", 8),
		"DB_PASSWORD":                  "it's-temoin-77",
		"CF_API_TOKEN":                 "cf-temoin-" + strings.Repeat("x7", 10),
		"DATABASE_URL":                 "postgres://u:temoin-pw@h/db",
		"SESSION_SECRET":               "temoin-session-" + strings.Repeat("k", 20),
		"GITHUB_PERSONAL_ACCESS_TOKEN": gh,
		"BRAVE_API_KEY":                "temoin-brave-" + strings.Repeat("9", 20),
		"ANTHROPIC_API_KEY":            "sk-ant-" + strings.Repeat("Tm0", 8),
	}
	attendus := map[string][2]string{ // name → source, type
		"STRIPE_SECRET_KEY": {"projet:alpha", "stripe"}, "DB_PASSWORD": {"projet:alpha", "mot_de_passe"},
		"CF_API_TOKEN": {"projet:beta", "cloudflare"}, "DATABASE_URL": {"projet:beta", "mot_de_passe"},
		"SESSION_SECRET": {"projet:gamma", "jeton"}, "GITHUB_PERSONAL_ACCESS_TOKEN": {"mcp:github", "github"},
		"BRAVE_API_KEY": {"mcp:brave", "jeton"}, "ANTHROPIC_API_KEY": {".env", "anthropic"},
	}
	kit := filepath.Join(d, "kit")
	fichiers := map[string]string{
		filepath.Join(d, "alpha", ".athanor", "secrets.env"): "# Values you gave Claude\nSTRIPE_SECRET_KEY='" + valeurs["STRIPE_SECRET_KEY"] + "'\nDB_PASSWORD='it'\\''s-temoin-77'\n",
		filepath.Join(d, "beta", ".athanor", "secrets.env"):  "CF_API_TOKEN='" + valeurs["CF_API_TOKEN"] + "'\r\nDATABASE_URL='" + valeurs["DATABASE_URL"] + "'\r\n",
		filepath.Join(d, "gamma", ".athanor", "secrets.env"): "OPENAI_API_KEY='your_key_here'\nSESSION_SECRET='" + valeurs["SESSION_SECRET"] + "'\n",
		filepath.Join(kit, "extensions", "secrets.json"):     `{"github": {"GITHUB_PERSONAL_ACCESS_TOKEN": "` + gh + `"}, "brave": {"BRAVE_API_KEY": "` + valeurs["BRAVE_API_KEY"] + `"}}`,
		// GH_TOKEN repeats the MCP token: one entry; NODE_ENV and API_STOP_USD are not secrets; EMPTY_KEY is empty
		filepath.Join(kit, ".env"): "# Claude\nANTHROPIC_API_KEY=" + valeurs["ANTHROPIC_API_KEY"] + "\nGH_TOKEN=\"" + gh + "\"\nNODE_ENV=production\nAPI_STOP_USD=5\nEMPTY_KEY=\n",
	}
	empreintes := map[string][32]byte{}
	for f, texte := range fichiers {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(texte), 0o600); err != nil {
			t.Fatal(err)
		}
		empreintes[f] = sha256.Sum256([]byte(texte))
	}
	cfg := Config{Kit: kit, Langue: "fr"}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		cfg.Projets = append(cfg.Projets, Projet{Nom: n, Dossier: filepath.Join(d, n)})
	}
	var chemin string
	demarrer := func() *Serveur {
		t.Helper()
		s := nouveauServeur(cfg)
		if s.coffre == nil || s.coffreErr != nil {
			t.Fatal("vault not loaded:", s.coffreErr)
		}
		if chemin == "" { // a config left by an earlier test makes the test binary's folder Athanor's
			chemin = s.coffre.chemin
			t.Cleanup(func() { _ = os.Remove(chemin); _ = os.Remove(cheminMigration(s.coffre)) })
		}
		return s
	}
	importees := func(s *Serveur) []EntreeCoffre {
		var l []EntreeCoffre
		for _, e := range s.coffre.Liste() {
			if e.Source != "" {
				l = append(l, e)
			}
		}
		return l
	}

	// 1st start-up: everything imported, code scope, type recognised, note in the user's language
	s := demarrer()
	l := importees(s)
	if len(l) != len(attendus) {
		t.Fatalf("%d entries imported, %d expected: %+v", len(l), len(attendus), l)
	}
	for _, e := range l {
		a, ok := attendus[e.Nom]
		switch {
		case !ok:
			t.Fatal("unexpected entry", e.Nom)
		case e.Source != a[0] || e.Type != a[1] || e.Portee != "code" || e.Famille != "secret":
			t.Errorf("%s: source %q type %q scope %q family %q", e.Nom, e.Source, e.Type, e.Portee, e.Famille)
		case !strings.HasPrefix(e.Note, "Importé de "):
			t.Errorf("%s: note %q", e.Nom, e.Note)
		}
		if err := lisible(s.coffre, e.ID, valeurs[e.Nom]); err != nil {
			t.Error(e.Nom, err)
		}
	}
	manuelle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "MANUELLE", Famille: "secret", Type: "jeton"}, "temoin-manuelle-1")
	if err != nil {
		t.Fatal(err)
	}

	// 2nd start-up: nothing more; an imported entry the user deleted is not brought back
	s = demarrer()
	if n := len(importees(s)); n != len(attendus) {
		t.Fatalf("second start-up: %d imported entries, want %d", n, len(attendus))
	}
	// an agent adds a line to a secrets.env after the first import: never taken in by a start-up (C7)
	alpha := filepath.Join(d, "alpha", ".athanor", "secrets.env")
	avantAlpha, _ := os.ReadFile(alpha)
	if err := os.WriteFile(alpha, append(slices.Clone(avantAlpha), "LATE_TOKEN='temoin-tardif-9'\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	s = demarrer()
	if n := len(importees(s)); n != len(attendus) {
		t.Fatalf("a line written after the first import came in at start-up: %d imported entries", n)
	}
	if err := os.WriteFile(alpha, avantAlpha, 0o600); err != nil {
		t.Fatal(err)
	}
	supprimee := importees(s)[0]
	if err := s.coffre.Supprimer(supprimee.ID); err != nil {
		t.Fatal(err)
	}
	s = demarrer()
	if n := len(importees(s)); n != len(attendus)-1 {
		t.Fatalf("a deleted entry came back: %d imported entries", n)
	}
	if m := s.resumeMigration(); m["importees"] != len(attendus)-1 || m["annulee"] != false {
		t.Fatalf("summary %v", m)
	}

	// undo: exactly the untouched imported entries leave; the manual one and one whose value the user
	// replaced since (B9) stay; no import at the next start-up
	modifiee, nomModifiee := importees(s)[0].ID, importees(s)[0].Nom
	time.Sleep(2 * time.Millisecond)
	if err := s.coffre.RemplacerValeur(modifiee, "temoin-nouvelle-valeur"); err != nil {
		t.Fatal(err)
	}
	if w := appelCoffre(t, s.apiCoffreMigrationAnnuler, "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"retirees":6`) {
		t.Fatal("undo:", w.Code, w.Body.String())
	}
	s = demarrer()
	if l := s.coffre.Liste(); len(l) != 2 || !slices.ContainsFunc(l, func(e EntreeCoffre) bool { return e.ID == manuelle.ID }) ||
		!slices.ContainsFunc(l, func(e EntreeCoffre) bool { return e.ID == modifiee }) {
		t.Fatalf("after undo and restart: %+v", l)
	}
	if m := s.resumeMigration(); m["importees"] != 0 || m["annulee"] != true {
		t.Fatalf("summary after undo %v", m)
	}

	// "Import again": the undone entries come back, neither the one the user deleted nor the old value of
	// the one they changed (C13); the undo cleared
	if w := appelCoffre(t, s.apiCoffreMigration, "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"importees":6`) {
		t.Fatal("import again:", w.Code, w.Body.String())
	}
	if m := s.resumeMigration(); m["importees"] != len(attendus)-2 || m["annulee"] != false {
		t.Fatalf("summary after import again %v", m)
	}
	noms := map[string]int{}
	for _, e := range importees(s) {
		noms[e.Nom]++
	}
	if noms[supprimee.Nom] != 0 || noms[nomModifiee] != 1 || len(noms) != len(attendus)-1 {
		t.Fatalf("after import again: %v (deleted %s, changed %s)", noms, supprimee.Nom, nomModifiee)
	}

	// B10: a key changed in its old file updates the entry of the same name and source, no 2nd entry
	beta := filepath.Join(d, "beta", ".athanor", "secrets.env")
	avantBeta, _ := os.ReadFile(beta)
	nouvelle := "cf-temoin-" + strings.Repeat("y8", 10)
	if err := os.WriteFile(beta, []byte(strings.Replace(string(avantBeta), valeurs["CF_API_TOKEN"], nouvelle, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := appelCoffre(t, s.apiCoffreMigration, "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"importees":1`) {
		t.Fatal("import of a changed key:", w.Code, w.Body.String())
	}
	var cf []EntreeCoffre
	for _, e := range importees(s) {
		if e.Nom == "CF_API_TOKEN" {
			cf = append(cf, e)
		}
	}
	if len(cf) != 1 || cf[0].Source != "projet:beta" || lisible(s.coffre, cf[0].ID, nouvelle) != nil {
		t.Fatalf("changed key: %+v", cf)
	}
	if err := os.WriteFile(beta, avantBeta, 0o600); err != nil {
		t.Fatal(err)
	}
	w := appelCoffre(t, s.apiCoffre, "", "", nil)
	for nom, v := range valeurs {
		if strings.Contains(w.Body.String(), v) {
			t.Error("value of", nom, "in GET /api/coffre")
		}
	}
	for f, e := range empreintes {
		if b, err := os.ReadFile(f); err != nil || sha256.Sum256(b) != e {
			t.Error("old file changed:", f, err)
		}
	}
}
