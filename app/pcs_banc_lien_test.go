package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBancEquipeLien: two real pilots build one project together, the team's workshop (the plan, the
// request, the log, the notes) going through the master's direct link and never through GitHub: GitHub has
// the code only; the node, which has no request, waits for the master's; the framing, each milestone and
// the delivery done once, every computer ends with the whole workshop.
func TestBancEquipeLien(t *testing.T) {
	if runtime.GOOS == "windows" && trouverBashNatifPilote() == "" {
		t.Skip("Git Bash not found on Windows (the fake Claude Code is a bash script)")
	}
	for _, x := range []string{"git", "go"} {
		if _, err := exec.LookPath(x); err != nil {
			t.Skip(x + " missing")
		}
	}
	bin := filepath.Join(t.TempDir(), "athanor-essai")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-tags", "essai", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	t.Run("lien", func(t *testing.T) { bancEquipeLien(t, bin, false) })
	// the master's link goes down once the plan is written: the node goes on through GitHub (encrypted),
	// the master merges what it wrote there
	t.Run("secours", func(t *testing.T) { bancEquipeLien(t, bin, true) })
}

func bancEquipeLien(t *testing.T, bin string, couper bool) {

	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	defer func() { depotEssaiGH = ancien }()

	// A starts the project: its request stays in its workshop, its code goes to GitHub
	a := nouveauPCEssai(t, "PC-GPU", "gpu, blender", "RTX 4090", depot, false)
	a.faire(t, func() {
		var o bytes.Buffer
		if reglageGH("partage", []string{a.projet, "0"}, &o) != 0 {
			t.Fatal(o.String())
		}
	})
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "REQUEST.md"), "# Request\n\nBuild the game.\n")
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "# The game\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "Start")
	a.faire(t, func() {
		var o strings.Builder
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatalf("A's first push: %s", o.String())
		}
	})
	if l := gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main"); strings.Contains(l, ".athanor") {
		t.Fatalf("the workshop went to GitHub: %s", l)
	}
	b := pcVideEssai(t, "PC-Fibre", nil)
	b.faire(t, func() {
		var o strings.Builder
		if code := pcsMachineGH([]string{"--mots", "network"}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	if code, out := activerEssai(t, b); code != 0 || !strings.Contains(out, "starts from the other computers' work") {
		t.Fatalf("B's switch: %d %s", code, out)
	}
	b.faire(t, func() {
		var o bytes.Buffer
		if reglageGH("partage", []string{b.projet, "0"}, &o) != 0 {
			t.Fatal(o.String())
		}
	})
	gitEssai(t, b.projet, "config", "user.name", "PC-Fibre")
	gitEssai(t, b.projet, "config", "user.email", "fibre@example.com")
	if _, err := os.Stat(filepath.Join(b.projet, ".athanor", "REQUEST.md")); err == nil {
		t.Fatal("B has the request from GitHub")
	}

	// the team: A is the master (its listener, no tunnel here), B a node
	kits := []string{preparerKitEssaiPilote(t, bin), preparerKitEssaiPilote(t, bin)}
	srv := nouveauServeur(Config{Kit: kits[0], Projets: []Projet{{Nom: "jeu", Dossier: a.projet}}, ProjetActif: "jeu"})
	pA := srv.config.Projets[0]
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	cle, jeton := base64.StdEncoding.EncodeToString(k), aleatoire(32)
	if err := srv.ecrireDirect(pA, equipeDirecte{Role: "maitre", Equipe: "essai", Cle: cle, AtelierLien: true,
		Noeuds: map[string]noeudConnu{"idfibre": {Nom: "PC-Fibre", Jeton: hacherJeton(jeton)}}}); err != nil {
		t.Fatal(err)
	}
	sansTunnelDirect = true
	defer func() { sansTunnelDirect = false }()
	if err := srv.demarrerMaitre(pA); err != nil {
		t.Fatal(err)
	}
	defer srv.arreterMaitre(pA.Nom)
	var adresse string
	for i := 0; i < 100 && adresse == ""; i++ {
		if v := srv.vueDirecte(pA); v != nil {
			adresse = v.Adresse
		}
		time.Sleep(20 * time.Millisecond)
	}
	priveB := priveDePilote(kits[1], b.projet, estWindows)
	_ = os.MkdirAll(priveB, 0o755)
	eb, _ := json.Marshal(equipeDirecte{Role: "noeud", Equipe: "essai", Cle: cle, Adresse: adresse, Jeton: jeton, Maitre: "PC-GPU", AtelierLien: true})
	ecrireEssai(t, filepath.Join(priveB, "equipe-directe.json"), string(eb))

	if couper {
		go func() {
			for i := 0; i < 3000; i++ {
				if v, err := srv.magasinProjet(pA).dernier(); err == nil && v.Fichiers["PLAN.md"] != "" {
					srv.arreterMaitre(pA.Nom)
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
	sorties := make([]string, 2)
	var wg sync.WaitGroup
	for i, m := range []pcEssai{b, a} { // the node first: it waits for the master's request
		kit := kits[map[bool]int{true: 0, false: 1}[m.projet == a.projet]]
		faux := filepath.Join(kit, "fake-claude.sh")
		if err := os.WriteFile(faux, []byte(scriptFauxClaudeEquipe), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(kit, ".env"), []byte("LANGUAGE=en\n"), 0o644)
		wg.Add(1)
		go func(i int, m pcEssai, kit, faux string) {
			defer wg.Done()
			ctx, fin := context.WithTimeout(context.Background(), 5*time.Minute)
			defer fin()
			c := exec.CommandContext(ctx, bin, "demarrer", m.projet)
			c.WaitDelay = 10 * time.Second
			c.Dir = kit
			c.Env = append(os.Environ(), "KIT="+kit, "CLAUDE_BIN="+faux, "CLAUDE_CODE_OAUTH_TOKEN=fake-token-for-test",
				"KEEP_AWAKE=0", "CLOSE_AFTER_MILESTONE=0", "WATCH_INTERVAL=1", "SECONDS_PER_MINUTE=1", "QUOTA_WAIT_MIN=1",
				"ERROR_WAIT_MIN=0", "ATHANOR_GITHUB_DIR="+m.donnees, "ATHANOR_DEPOT_ESSAI="+depot)
			out, err := c.CombinedOutput()
			sorties[i] = string(out)
			if err != nil {
				sorties[i] += "\n(exit: " + err.Error() + ")"
			}
		}(i, m, kit, faux)
		time.Sleep(2 * time.Second)
	}
	wg.Wait()
	journal := "\n--- B (node) ---\n" + sorties[0] + "\n--- A (master) ---\n" + sorties[1]
	for _, m := range []pcEssai{a, b} {
		if x, err := os.ReadFile(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt")); err == nil {
			journal += "\n--- " + m.nom + "'s Claude saw ---\n" + string(x)
		}
	}
	for i, s := range sorties {
		if !strings.Contains(s, "DONE") || strings.Contains(s, "(exit:") {
			t.Fatalf("pilot %d did not end with the delivery%s", i, journal)
		}
	}
	if !strings.Contains(sorties[0], "the request came with the team's workshop") {
		t.Fatalf("the node did not get the master's request%s", journal)
	}
	// GitHub: the code only, never the workshop
	l := gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main")
	if strings.Contains(l, ".athanor") {
		t.Fatalf("the workshop went to GitHub:\n%s%s", l, journal)
	}
	// the team's workshop: every milestone ticked, each built once, the framing and the delivery once
	v, err := srv.magasinProjet(pA).dernier()
	if err != nil {
		t.Fatal(err)
	}
	lire := func(rel string) string {
		o, err := srv.magasinProjet(pA).objets([]string{v.Fichiers[rel]})
		if err != nil {
			t.Fatalf("%s not in the team's workshop%s", rel, journal)
		}
		return string(o[v.Fichiers[rel]])
	}
	if p := lire("PLAN.md"); strings.Contains(p, "- [ ]") {
		t.Fatalf("plan:\n%s%s", p, journal)
	}
	log := lire("LOG.md")
	compte := func(motif string) int { return len(regexp.MustCompile(motif).FindAllString(log, -1)) }
	if compte(`- framing by`) != 1 || compte(`- delivery by`) != 1 {
		t.Fatalf("framing or delivery twice:\n%s%s", log, journal)
	}
	for _, id := range []string{"M1", "M2", "M3", "M4", "M5"} {
		if n := compte(`- ` + id + ` by`); n != 1 {
			t.Fatalf("%s built %d times:\n%s%s", id, n, log, journal)
		}
		if !strings.Contains(l, "work/"+id+".txt") {
			t.Fatalf("%s's work not on GitHub%s", id, journal)
		}
	}
	if !strings.Contains(lire("FINAL_REPORT.md"), "STATUS: DONE") || !strings.Contains(lire("TEAM_NOTES.md"), "Armor.paint(color)") {
		t.Fatal("final report or team notes missing from the team's workshop" + journal)
	}
	// each computer has the whole workshop
	for _, m := range []pcEssai{a, b} {
		if p, _ := os.ReadFile(filepath.Join(m.projet, ".athanor", "PLAN.md")); strings.Contains(string(p), "- [ ]") || len(p) == 0 {
			t.Fatalf("%s's plan:\n%s%s", m.nom, p, journal)
		}
		if _, err := os.Stat(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt")); err == nil {
			x, _ := os.ReadFile(filepath.Join(m.projet, ".athanor", "fake-unexpected.txt"))
			t.Fatalf("%s's Claude got an unexpected goal: %s%s", m.nom, x, journal)
		}
	}
	if !strings.Contains(sorties[0], "Team workshop (direct link)") {
		t.Fatalf("no workshop exchange in the node's log%s", journal)
	}
	if couper && (!strings.Contains(sorties[0], "through the team's copy on GitHub") || !strings.Contains(sorties[1], "what the other computers wrote on GitHub while the direct link was down")) {
		t.Fatalf("no fallback through GitHub%s", journal)
	}
	// a private team: its presences on GitHub say nothing of the computers
	for _, ref := range strings.Fields(gitEssai(t, depot, "for-each-ref", "--format=%(refname)", refPresencePC)) {
		p := gitEssai(t, depot, "cat-file", "-p", ref+":pc.json")
		// the encrypted part left out: "PC-" may well appear in its random letters
		if clair := regexp.MustCompile(`"chiffre": *"[A-Za-z0-9_-]*"`).ReplaceAllString(p, `"chiffre": ""`); strings.Contains(clair, "PC-") || clair == p {
			t.Fatalf("a presence in clear: %s", p)
		}
	}
	for i, s := range sorties {
		var l []string
		for _, x := range strings.Split(s, "\n") {
			if strings.Contains(x, "workshop") || strings.Contains(x, "takes") || strings.Contains(x, "request") {
				l = append(l, x)
			}
		}
		t.Logf("%s:\n%s", []string{"node", "master"}[i], strings.Join(l, "\n"))
	}
}

// A computer whose workshop is not shared on GitHub (here: through the team's direct link) keeps its
// .athanor/ files when another computer's commit takes them out of git.
func TestGarderAtelierPC(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	defer func() { depotEssaiGH = ancien }()
	a := nouveauPCEssai(t, "PC-A", "", "", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [ ] M1 : x\n")
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "x\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "Start, the workshop shared")
	a.faire(t, func() {
		var o strings.Builder
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	// B joins with its workshop through the direct link: it gets the files tracked, not shared any more
	b := pcVideEssai(t, "PC-B", nil)
	kit := t.TempDir()
	t.Setenv("KIT", kit)
	prive := priveDePilote(kit, b.projet, estWindows)
	ecrireEssai(t, filepath.Join(prive, "equipe-directe.json"), `{"role":"noeud","adresse":"https://x.trycloudflare.com","jeton":"j","atelier_lien":true}`)
	if code, out := activerEssai(t, b); code != 0 {
		t.Fatal(out)
	}
	var r reglagesGH
	b.faire(t, func() { r, _ = lireReglagesGH(dossierGH(b.projet)) })
	if r.Atelier {
		t.Fatal("the workshop through the link was shared on GitHub again by the switch")
	}
	gitEssai(t, b.projet, "config", "user.name", "PC-B")
	gitEssai(t, b.projet, "config", "user.email", "b@example.com")
	// A stops sharing: its commit takes .athanor/ out of git
	a.faire(t, func() {
		var o bytes.Buffer
		if reglageGH("partage", []string{a.projet, "0"}, &o) != 0 {
			t.Fatal(o.String())
		}
	})
	gitEssai(t, a.projet, "commit", "-q", "-m", "The workshop out of git")
	a.faire(t, func() {
		var o strings.Builder
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	b.faire(t, func() {
		e, err := ouvrirEquipePC(b.projet)
		if err != nil || e == nil {
			t.Fatal(err)
		}
		if res, detail, err := e.recevoir(); err != nil || res != recuFusion {
			t.Fatalf("B's merge: %s %s %v", res, detail, err)
		}
	})
	if p, err := os.ReadFile(filepath.Join(b.projet, ".athanor", "PLAN.md")); err != nil || !strings.Contains(string(p), "M1") {
		t.Fatalf("B's workshop removed by A's commit: %v", err)
	}
	if out := gitEssai(t, b.projet, "ls-files", ".athanor"); out != "" {
		t.Fatalf("still in git: %s", out)
	}
}
