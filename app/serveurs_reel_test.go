package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlanAgentTest(t *testing.T) {
	ecrire := func(chemin, contenu string) EtapePlan {
		return EtapePlan{Outil: "fichier_ecrire", Arguments: []byte(`{"chemin":"` + chemin + `","contenu":"` + contenu + `"}`)}
	}
	for _, cas := range []struct {
		nom    string
		etapes []EtapePlan
		ok     bool
	}{
		{"the asked step", []EtapePlan{ecrire(fichierAgentTest, "agent-test-x")}, true},
		{"no step", nil, false},
		{"another file", []EtapePlan{ecrire("/etc/passwd", "agent-test-x")}, false},
		{"another text", []EtapePlan{ecrire(fichierAgentTest, "autre")}, false},
		{"one step more", []EtapePlan{ecrire(fichierAgentTest, "agent-test-x"), {Outil: "ssh_commande", Arguments: []byte(`{"commande":"reboot"}`)}}, false},
	} {
		if planAgentTest(PlanServeur{Etapes: cas.etapes}, "agent-test-x") != cas.ok {
			t.Errorf("%s: expected %v", cas.nom, cas.ok)
		}
	}
}

// TestTesterAgentServeurs (M102): the application side of Test the Servers agent on the test container, the ops
// lead played by a scripted fake Claude through the real MCP route: the guards tried by the session, or by the
// test when the session skips them; a plan that does something else refused; Claude not logged in skipped.
// The test server and its key never stay.
func TestTesterAgentServeurs(t *testing.T) {
	s, _, _, _, direct := serveurConteneurTest(t) // the container's own server stays, to look at it afterwards
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	cles := len(s.coffre.Liste())
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(jeton, nom, args string) (string, bool) {
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		if rep.Error != nil {
			return rep.Error.Message, true
		}
		return rep.Result.Content[0].Text, rep.Result.IsError
	}
	var cas, temoin string // the witness the first session read, applied by the resumed one
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, f))
			return strings.TrimSpace(string(b))
		}
		consigne, jeton, j := lire("consigne.txt"), lire("hub-jeton"), ""
		res := func(id, r string) string {
			return `{"type":"system","subtype":"init","session_id":"` + id + `"}` + "\n" + `{"type":"result","is_error":false,"result":"` + r + `"}` + "\n"
		}
		l, _ := lireServeurs()
		id := l[len(l)-1].ID // the server the test added
		if _, apres, ok := strings.Cut(consigne, `contenu "`); ok {
			temoin, _, _ = strings.Cut(apres, `"`)
		}
		switch {
		case cas == "non connecté":
			j = `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"assistant","error":"authentication_failed"}` + "\n"
		case strings.Contains(consigne, "security critic"):
			j = res("crit", "VERDICT: ok")
		case strings.Contains(consigne, "external verifier"):
			j = res("verif", "VERIFICATION: ok")
		case lire("reprise") == "":
			appel(jeton, "serveur_fiche", `{"serveur":"`+id+`"}`)
			appel(jeton, "runbook_lire", `{"serveur":"`+id+`"}`)
			if cas == "complet" {
				appel(jeton, "serveur_fiche", `{"serveur":"`+serveurHorsPerimetre+`"}`)
				appel(jeton, "ssh_commande", `{"serveur":"`+id+`","commande":"rm -f `+gardeAgentTest+`"}`)
			}
			chemin := fichierAgentTest
			if cas == "autre plan" {
				chemin = "/tmp/autre.txt"
			}
			if txt, e := appel(jeton, "proposer_plan", `{"serveur":"`+id+`","resume":"essai","etapes":[{"outil":"fichier_ecrire","arguments":{"chemin":"`+chemin+`","contenu":"`+temoin+`"},"pourquoi":"Athanor agent test"}]}`); e {
				t.Errorf("proposer_plan: %s", txt)
			}
			j = res("ops", "Plan proposed.")
		default:
			if txt, e := appel(jeton, "fichier_ecrire", `{"serveur":"`+id+`","chemin":"`+fichierAgentTest+`","contenu":"`+temoin+`"}`); e {
				t.Errorf("accepted step: %s", txt)
			}
			appel(jeton, "runbook_ajouter", `{"serveur":"`+id+`","section":"historique","texte":"fichier de test écrit"}`)
			j = res("ops", "Done.")
		}
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	cible := os.Getenv("ATHANOR_SSH_TEST")
	for _, c := range []struct {
		nom, attendu string
		ok           bool
	}{
		{"complet", "intact (by the session)", true},
		{"sans gardes", "the session skipped outside the perimeter and destructive, tried by the test with its token", true},
		{"autre plan", "FAIL NO PLAN DOING WHAT WAS ASKED", false},
		{"non connecté", "SKIPPED Claude Code is not logged in", false},
	} {
		cas = c.nom
		var sortie []string
		ok := s.testerAgentServeurs(func(l string) { sortie = append(sortie, l) }, cible)
		txt := strings.Join(sortie, "\n")
		if ok != c.ok || !strings.Contains(txt, c.attendu) {
			t.Errorf("%s: ok=%v\n%s", c.nom, ok, txt)
		}
		if n := strings.Count(txt, "\n  OK  ") + strings.Count(txt, "\n  FAIL "); c.ok && n != 6 {
			t.Errorf("%s: %d result lines, expected 6", c.nom, n)
		}
		if l, _ := lireServeurs(); len(l) != 1 || len(s.coffre.Liste()) != cles {
			t.Errorf("%s: the test server or its key stayed: %+v", c.nom, l)
		}
	}
	if r := direct("ssh_commande", map[string]any{"commande": "ls /tmp"}); strings.Contains(r.Sortie, "athanor-agent") { // nothing left there
		t.Errorf("test files left on the container: %q", r.Sortie)
	}
}

// TestServeursAgentReel: the real test, by hand (ATHANOR_TEST_SERVEURS_REEL = a kit holding freshly built athanor
// and Athanor.exe, the repository itself; ATHANOR_TEST_SERVEURS_NATIF=1: the session runs natively on Windows):
// Athanor's test container started like the button does, a real Claude session as the ops lead, critic and verifier.
func TestServeursAgentReel(t *testing.T) {
	kit := os.Getenv("ATHANOR_TEST_SERVEURS_REEL")
	if kit == "" {
		t.Skip("ATHANOR_TEST_SERVEURS_REEL not set (a real Claude session on the test container, run by hand)")
	}
	s, _, _ := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s.config.Kit, s.kit = kit, Kit{Racine: kit}
	mux := http.NewServeMux()
	s.routes(mux)
	hub := httptest.NewServer(mux)
	defer hub.Close()
	fInst := filepath.Join(t.TempDir(), "instance.json")
	ecrireInstanceTest(t, fInst, hub.URL)
	instanceSessionsServeurs = fInst
	avant := delaiTestAgent
	delaiTestAgent = 12 * time.Minute
	t.Cleanup(func() { instanceSessionsServeurs, delaiTestAgent = "", avant })
	modeNatif.Store(runtime.GOOS == "windows" && os.Getenv("ATHANOR_TEST_SERVEURS_NATIF") == "1")
	cible, arreter, err := demarrerConteneurTest(func(l string) { t.Log(l) })
	if err != nil {
		t.Skip(err)
	}
	defer arreter()
	if !s.testerAgentServeurs(func(l string) { t.Log(l) }, cible) {
		t.Error("the real Servers agent test failed")
	}
}
