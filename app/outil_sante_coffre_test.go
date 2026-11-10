package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerdictCoffre(t *testing.T) {
	tm := temoinsCoffre{Accordee: "temoin-code-a1", NonAccordee: "temoin-code-b2", Infra: "temoin-infra-c3", CleCode: []string{"ab12cd"}}
	sonde := "SONDE secrets.env\nATHANOR_TEST_GRANTED='temoin-code-a1'\nSONDE dpapi code=126\nSONDE security code=127\n"
	etats := func(l []ligneVerdict) string {
		var b strings.Builder
		for _, x := range l {
			b.WriteString(x.Etat + ":" + x.Texte + "\n")
		}
		return b.String()
	}
	for _, c := range []struct {
		nom, journal string
		t            temoinsCoffre
		natif, ok    bool
		contient     string
	}{
		{"témoin accordée vue", sonde, tm, false, true, "dpapi attempt: reported, exit code 126"},
		{"infra vue", sonde + "temoin-infra-c3", tm, false, false, "echec:THE INFRA VALUE WAS READ"},
		{"infra vue en natif", sonde + "temoin-infra-c3", tm, true, false, "echec:THE INFRA VALUE WAS READ"},
		{"non accordée vue", sonde + "x temoin-code-b2", tm, false, false, "echec:A VALUE NOT GRANTED WAS READ"},
		{"accordée absente", "SONDE secrets.env\n", tm, false, false, "echec:THE GRANTED VALUE DID NOT REACH"},
		{"sonde absente", "", tm, false, false, "echec:THE PROBE DID NOT RUN"},
		{"clé code ouverte (WSL)", sonde + "ab12cd", tm, false, false, "echec:THE CODE KEY WAS OPENED"},
		{"clé code ouverte (natif)", sonde + "ab12cd", tm, true, true, "info:code key: opened through DPAPI"},
		{"secret-tool non rapporté", sonde, tm, false, true, "info:secret-tool attempt: not reported"},
		{"sans infra", sonde, temoinsCoffre{Accordee: "temoin-code-a1", NonAccordee: "temoin-code-b2"}, false, true, "info:infra value: not tested"},
	} {
		l, ok := verdictCoffre(c.journal, c.t, c.natif)
		if ok != c.ok || !strings.Contains(etats(l), c.contient) {
			t.Errorf("%s: ok=%v (attendu %v), lignes:\n%s", c.nom, ok, c.ok, etats(l))
		}
	}
}

// The test values reach only the injected file, are removed even when the session fails, and a test cut
// short is purged at the next start.
func TestTesterCoffre(t *testing.T) {
	s := serveurCoffreTest(t)
	if err := s.coffre.ChangerMotDePasse("", "mdp-test-coffre"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test-coffre"); err != nil {
		t.Fatal(err)
	}
	vraie, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe", Famille: "secret", Type: "stripe"}, "temoin-vraie-"+aleatoire(4))
	if err != nil {
		t.Fatal(err)
	}
	k := Kit{Racine: filepath.Join(t.TempDir(), "kit")}
	for _, cas := range []struct {
		nom     string
		session func(projet string) error
		ok      bool
	}{
		{"la session lit le fichier injecté", func(projet string) error {
			if n := len(s.coffre.Liste()); n != 4 {
				t.Errorf("%d entrées pendant le test, 4 attendues", n)
			}
			b, err := os.ReadFile(filepath.Join(projet, ".athanor", "secrets.env"))
			if err != nil || strings.Count(string(b), "temoin-") != 1 || !strings.Contains(string(b), "ATHANOR_TEST_GRANTED='temoin-code-") {
				t.Errorf("fichier injecté inattendu: %q %v", b, err)
			}
			return os.WriteFile(filepath.Join(projet, journalTestCoffre), append([]byte("SONDE secrets.env\n"), b...), 0o600)
		}, true},
		{"la session échoue", func(projet string) error { return errors.New("claude absent") }, false},
	} {
		var sortie []string
		ok := s.testerCoffre(k, func(l string) { sortie = append(sortie, l) }, cas.session)
		if ok != cas.ok {
			t.Errorf("%s: ok=%v, sortie:\n%s", cas.nom, ok, strings.Join(sortie, "\n"))
		}
		if l := s.coffre.Liste(); len(l) != 1 || l[0].ID != vraie.ID {
			t.Errorf("%s: témoins restés dans le coffre: %v", cas.nom, l)
		}
		if _, err := os.Stat(filepath.Join(k.Journaux(), dossierTestCoffre)); !os.IsNotExist(err) {
			t.Errorf("%s: projet temporaire resté", cas.nom)
		}
		if strings.Contains(strings.Join(sortie, "\n"), "temoin-") {
			t.Errorf("%s: une valeur témoin dans la sortie", cas.nom)
		}
	}
	// a test cut short: its values are purged when the vault is loaded again
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "ATHANOR_TEST_GRANTED", Famille: "secret", Type: "jeton", Source: sourceTestCoffre}, "temoin-x"); err != nil {
		t.Fatal(err)
	}
	s.chargerCoffre()
	if l := s.coffre.Liste(); len(l) != 1 || l[0].ID != vraie.ID {
		t.Errorf("purge au démarrage: %v", l)
	}
}

// TestTesterCoffreReel: the real test, once by hand (ATHANOR_TEST_COFFRE_REEL = a freshly built
// executable): a temporary vault with this PC's keystore, a real Claude session in the kit (the repository).
func TestTesterCoffreReel(t *testing.T) {
	exe := os.Getenv("ATHANOR_TEST_COFFRE_REEL")
	if exe == "" {
		t.Skip("ATHANOR_TEST_COFFRE_REEL not set (a real Claude session, run by hand)")
	}
	env := os.Environ() // the session keeps the real HOME (Claude's sign-in)
	s := serveurCoffreTest(t)
	c, err := ouvrirCoffre(s.coffre.chemin, enveloppeSystemeLocale())
	if err != nil {
		t.Fatal(err)
	}
	s.coffre = c
	if err := c.ChangerMotDePasse("", "mdp-test-coffre-reel"); err != nil {
		t.Fatal(err)
	}
	if err := c.Deverrouiller("mdp-test-coffre-reel"); err != nil {
		t.Fatal(err)
	}
	kit, _ := filepath.Abs("..")
	ancien := modeNatif.Load()
	modeNatif.Store(runtime.GOOS == "windows")
	t.Cleanup(func() { modeNatif.Store(ancien) })
	ok := s.testerCoffre(Kit{Racine: kit}, func(l string) { t.Log(l) }, func(projet string) error {
		cmd := exec.Command(exe, "tester-coffre", kit, projet, c.chemin)
		cmd.Dir, cmd.Env = kit, env
		out, err := cmd.CombinedOutput()
		t.Log(strings.TrimSpace(string(out)))
		return err
	})
	if !ok {
		t.Error("the real vault test failed")
	}
}
