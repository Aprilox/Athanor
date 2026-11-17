package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMissionRedemarrage (M170): what Athanor does with its missions when it starts again — in this process, one
// subtest per rule; then the real executable, stopped in the middle of a mission and started again: it finishes,
// nothing made twice.
func TestMissionRedemarrage(t *testing.T) {
	t.Run("regles", testRepriseRegles)
	t.Run("vrai_executable", testRepriseExecutable)
}

func journalCles(m Mission) (ids []string, cles []string) {
	for _, e := range m.Journal {
		ids, cles = append(ids, e.ID), append(cles, e.Cle)
	}
	return
}

func testRepriseRegles(t *testing.T) {
	s := serveurCoffreTest(t)
	if err := ecrireConfig(s.config); err != nil { // dossierAthanor() fixed for the whole test (as TestMissionControle)
		t.Fatal(err)
	}
	ancienPas := pasSuiviMission
	pasSuiviMission = time.Hour // one reading at the start, the test reads the rest itself
	t.Cleanup(func() { pasSuiviMission = ancienPas })
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	p := Projet{Nom: "site", Dossier: filepath.Join(t.TempDir(), "site")}
	ecrireFichier(t, p.Demande(), "# Site\n", 0o644)
	s.config.Projets = append(s.config.Projets, p)
	const mdp = "phrase-du-coffre-1"
	if err := s.coffre.ChangerMotDePasse("", mdp); err != nil {
		t.Fatal(err)
	}
	s.coffre.Verrouiller() // Athanor just started: the infra key waits for the password
	if s.etatCoffre().Infra {
		t.Fatal("infra key still open")
	}

	sansInfra, _ := json.Marshal(CarteMission{Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.site.test"}}})
	avecServeur, _ := json.Marshal(CarteMission{Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.site.test", Serveurs: []string{"a1"}}}})
	cree := time.Now().UTC().Format(formatCree)
	lancee := func(id string, carte []byte) Mission {
		return Mission{ID: id, Phrase: "un site", Etat: "lancee", Carte: carte, Politique: "prod", Cree: cree,
			Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}},
			Historique: []EvenementMission{evenementMission("lancee", "proprietaire")}, Relais: &RelaisMission{Etape: "construction"}}
	}
	missions := []Mission{
		lancee("reprendre1", sansInfra),
		lancee("attendinfra1", avecServeur),
		func() Mission { m := lancee("pause1", sansInfra); m.Etat = "en_pause"; return m }(),
	}
	for _, m := range missions {
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
	}
	// two pending prod accords of reprendre1: one for its card as it is, one for a card it no longer has
	m1, _ := lireMission("reprendre1")
	bon, err := s.demanderApprobation(s.accordProd(m1, "site", "aaaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	vieux := s.accordProd(m1, "site", "bbbbbbb")
	vieux.Details["empreinte"] = "une-autre-carte"
	perime, err := s.demanderApprobation(vieux)
	if err != nil {
		t.Fatal(err)
	}

	s.reprendreMissions()

	t.Run("accord_perime_refuse", func(t *testing.T) {
		if x, _ := s.approbations.Lire(perime.ID); x.Etat != "refusee" {
			t.Fatalf("accord for another card: %s", x.Etat)
		}
		if x, _ := s.approbations.Lire(bon.ID); x.Etat != "attente" {
			t.Fatalf("accord for the card as it is: %s", x.Etat)
		}
	})
	t.Run("pause_sans_ligne", func(t *testing.T) { // M240l1: a paused mission is followed, never told "resumes"
		m, _ := lireMission("pause1")
		if ids, cles := journalCles(m); slices.ContainsFunc(ids, func(i string) bool { return strings.HasPrefix(i, "redemarrage:") }) {
			t.Fatalf("journal: %v", cles)
		}
		if _, suivie := suivisMission.Load("pause1"); !suivie || m.Etat != "en_pause" {
			t.Fatalf("paused mission: %s, followed %v", m.Etat, suivie)
		}
	})
	t.Run("reprise_sans_relancer", func(t *testing.T) {
		m, _ := lireMission("reprendre1")
		ids, cles := journalCles(m)
		if !slices.Contains(cles, "Athanor restarted: the mission resumes") || !slices.ContainsFunc(ids, func(i string) bool { return strings.HasPrefix(i, "redemarrage:") }) {
			t.Fatalf("journal: %v", cles)
		}
		if n := len(slices.DeleteFunc(slices.Clone(m.Historique), func(e EvenementMission) bool { return e.Etat != "lancee" })); n != 1 {
			t.Fatalf("launched %d times", n)
		}
		if _, suivie := suivisMission.Load("reprendre1"); !suivie {
			t.Fatal("not followed again")
		}
	})
	t.Run("attend_le_coffre", func(t *testing.T) {
		m, _ := lireMission("attendinfra1")
		if _, cles := journalCles(m); !slices.Contains(cles, "Athanor restarted: the mission waits for the vault to be unlocked") {
			t.Fatalf("journal: %v", cles)
		}
		if _, suivie := suivisMission.Load("attendinfra1"); suivie {
			t.Fatal("followed while the infra key is closed")
		}
		cloche := func() bool {
			return slices.ContainsFunc(moduleMissions{}.Elements(s), func(e ElementHub) bool {
				return e.ID == "attendinfra1" && e.Attente != nil && e.Attente.Raison == "The mission waits for the vault to be unlocked"
			})
		}
		if !cloche() {
			t.Fatal("no bell alert")
		}
		s.deverrouillerCoffre(mdp)
		m, _ = lireMission("attendinfra1")
		if _, cles := journalCles(m); !slices.Contains(cles, "The vault is unlocked: the mission resumes") {
			t.Fatalf("journal after the unlock: %v", cles)
		}
		if _, suivie := suivisMission.Load("attendinfra1"); !suivie || cloche() {
			t.Fatalf("after the unlock: followed %v, bell %v", suivie, cloche())
		}
		// unlocked again: nothing written twice
		s.missionsDeverrouillees()
		if m2, _ := lireMission("attendinfra1"); len(m2.Journal) != len(m.Journal) {
			t.Fatalf("journal grew: %d → %d", len(m.Journal), len(m2.Journal))
		}
	})
}

// testRepriseExecutable: Athanor built, started with a launched mission, killed in the middle (its fake pilot goes on
// meanwhile: a milestone, the request finished, dev deployed and checked), started again: the mission finishes.
func testRepriseExecutable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	kit := t.TempDir() // Athanor's folder: the executable with its config next to it (installationAncienne)
	exe := filepath.Join(kit, "athanor")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-ldflags=-s -w", "-o", exe, ".").CombinedOutput(); err != nil {
		sautSiDefender(t, string(out))
		t.Fatalf("go build: %v\n%s", err, out)
	}
	p := Projet{Nom: "site", Dossier: filepath.Join(t.TempDir(), "site")}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", p.Dossier, "-c", "user.name=essai", "-c", "user.email=essai@exemple.test"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ecrireFichier(t, filepath.Join(p.Dossier, "index.html"), "v1\n", 0o644)
	ecrireFichier(t, p.Demande(), "# Site\n", 0o644)
	git("init", "-q")
	git("add", "index.html")
	git("commit", "-q", "-m", "M1: Add the page")
	b, _ := json.Marshal(Config{Kit: kit, Projets: []Projet{p}, PasVerifierMaj: true})
	ecrireFichier(t, filepath.Join(kit, nomConfig()), string(b), 0o600)
	// the environment and its access, as the launch left them (lancerMission does not run again)
	a, err := ouvrirAcces(filepath.Join(kit, "hub", "acces.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.definirAncien("site", "dev", Environnement{URL: "https://dev.site.test", Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	accesAvant, _ := os.ReadFile(filepath.Join(kit, "hub", "acces.json"))
	lance := time.Now().Add(-time.Minute).UTC()
	carte, _ := json.Marshal(CarteMission{Titre: "Un site", Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.site.test"}}})
	m := Mission{ID: "redemarrage1", Phrase: "un site", Etat: "lancee", Carte: carte, Politique: "prod", Cree: lance.Format(formatCree),
		Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}},
		Historique: []EvenementMission{{T: lance.Format(formatCree), Etat: "lancee", Detail: "proprietaire"}}, Relais: &RelaisMission{Etape: "construction"}}
	ecrireJSON := func(chemin string, v any) {
		t.Helper()
		b, _ := json.MarshalIndent(v, "", "  ")
		ecrireFichier(t, chemin, string(b), 0o600)
	}
	fichierMission := filepath.Join(kit, "hub", "missions", m.ID+".json")
	ecrireJSON(fichierMission, m)
	jalons := []JalonFait{{Projet: p.Dossier, Jalon: "M1", Date: time.Now()}}
	ecrireJSON(filepath.Join(kit, "hub", "jalons.json"), jalons)

	local := filepath.Join(kit, "local") // the instance file and the window's folders: never the user's
	env := append(os.Environ(), "ATHANOR_SANS_FENETRE=1", "LocalAppData="+local, "LOCALAPPDATA="+local,
		"HOME="+filepath.Join(kit, "home"), "XDG_CACHE_HOME="+filepath.Join(kit, "cache"), "XDG_CONFIG_HOME="+filepath.Join(kit, "config"))
	demarrer := func() *exec.Cmd {
		t.Helper()
		c := exec.Command(exe)
		c.Dir, c.Env = kit, env
		c.Stdout, c.Stderr = nil, nil
		if err := c.Start(); err != nil {
			sautSiDefender(t, err.Error())
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	lire := func() Mission {
		var x Mission
		b, _ := os.ReadFile(fichierMission)
		_ = json.Unmarshal(b, &x)
		return x
	}
	attendre := func(quoi string, f func(Mission) bool) Mission {
		t.Helper()
		for i := 0; i < 600; i++ { // a minute: the follower reads every 5 s
			if x := lire(); f(x) {
				return x
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s: never reached; mission %+v", quoi, lire())
		return Mission{}
	}
	aEntree := func(id string) func(Mission) bool {
		return func(x Mission) bool { ids, _ := journalCles(x); return slices.Contains(ids, id) }
	}

	premier := demarrer()
	attendre("milestone M1 in the journal", aEntree("jalon:M1"))
	_ = premier.Process.Kill() // stopped in the middle: no clean shutdown
	_ = premier.Wait()

	// meanwhile the pilot goes on: M2, the request finished, dev deployed and checked on HEAD
	ecrireFichier(t, filepath.Join(p.Dossier, "index.html"), "v2\n", 0o644)
	git("commit", "-q", "-am", "M2: Finish the page")
	head := git("rev-parse", "HEAD")
	jalons = append(jalons, JalonFait{Projet: p.Dossier, Jalon: "M2", Date: time.Now()})
	ecrireJSON(filepath.Join(kit, "hub", "jalons.json"), jalons)
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644)
	maintenant := time.Now().UTC().Format(time.RFC3339)
	ecrireJSON(filepath.Join(kit, "modules", "servers", "deploiements", "site-dev.json"), []Deploiement{{ID: "dep1", Commit: head, Etat: "ok",
		Debut: maintenant, Fin: maintenant, Verification: &VerifDeploiement{OK: true, T: maintenant}}})

	second := demarrer()
	fin := attendre("mission finished", func(x Mission) bool { return x.Etat == "terminee" })
	_ = second.Process.Kill()
	_ = second.Wait()

	ids, _ := journalCles(fin)
	for _, id := range []string{"jalon:M1", "jalon:M2", "dev:termine", "deploi:dep1:ok", "verif:dep1:ok", "mission:terminee"} {
		if !slices.Contains(ids, id) {
			t.Errorf("journal without %s: %v", id, ids)
		}
	}
	vus := map[string]bool{}
	redemarrages := 0
	for _, id := range ids {
		if vus[id] {
			t.Errorf("journal entry twice: %s", id)
		}
		vus[id] = true
		if strings.HasPrefix(id, "redemarrage:") {
			redemarrages++
		}
	}
	if redemarrages != 2 { // one per start of Athanor
		t.Errorf("%d restart entries: %v", redemarrages, ids)
	}
	if n := len(slices.DeleteFunc(slices.Clone(fin.Historique), func(e EvenementMission) bool { return e.Etat != "lancee" })); n != 1 {
		t.Errorf("launched %d times", n)
	}
	if fin.Relais == nil || fin.Relais.Etape != "termine" || fin.Relais.Commit != head {
		t.Errorf("relay: %+v", fin.Relais)
	}
	if accesApres, _ := os.ReadFile(filepath.Join(kit, "hub", "acces.json")); string(accesApres) != string(accesAvant) {
		t.Errorf("accesses or environments changed:\n%s\n→\n%s", accesAvant, accesApres)
	}
}
