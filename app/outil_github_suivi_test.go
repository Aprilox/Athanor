package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnvoiAuJalon: the automatic push goes with every new commit, once (never again for the same one),
// and .athanor/github.json says what GitHub has: the last commit pushed, kept when a later push is
// refused (with the reason), and that the automatic push is off (also said in the log, once).
func TestEnvoiAuJalon(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	defer func() { depotEssaiGH = ancien }()

	base := t.TempDir()
	m := pcEssai{nom: "A", donnees: filepath.Join(base, "donnees"), projet: filepath.Join(base, "projet")}
	_ = os.MkdirAll(m.projet, 0o755)
	gitEssai(t, m.projet, "init", "-q", "-b", "main")
	gitEssai(t, m.projet, "config", "user.name", "A")
	gitEssai(t, m.projet, "config", "user.email", "a@example.com")
	_ = os.MkdirAll(filepath.Join(m.projet, ".athanor"), 0o755)
	if err := ecrireExclusionAtelier(m.projet, false); err != nil { // as linking does: the workshop out of git
		t.Fatal(err)
	}
	lierPCEssai(t, m, "", "")
	commit := func(fichier, sujet string) {
		t.Helper()
		ecrireEssai(t, filepath.Join(m.projet, fichier), sujet+"\n")
		gitEssai(t, m.projet, "add", "-A")
		gitEssai(t, m.projet, "commit", "-q", "-m", sujet)
	}
	suivi := func() SuiviGitHub {
		t.Helper()
		var g SuiviGitHub
		b, err := os.ReadFile(filepath.Join(m.projet, ".athanor", "github.json"))
		if err != nil || json.Unmarshal(b, &g) != nil {
			t.Fatalf("github.json: %v %s", err, b)
		}
		return g
	}
	p := &pilote{projet: m.projet, prive: t.TempDir(), etat: filepath.Join(m.projet, ".athanor"), arretCh: make(chan struct{}), battementStop: make(chan struct{})}
	lignesGH := func() int {
		b, _ := os.ReadFile(filepath.Join(p.prive, "pilot.log"))
		return strings.Count(string(b), "] GitHub: ")
	}

	m.faire(t, func() {
		commit("niveau1.txt", "M1: level one")
		p.githubPousserNouveau()
		if strings.TrimSpace(gitEssai(t, depot, "rev-parse", "main")) != strings.TrimSpace(gitEssai(t, m.projet, "rev-parse", "HEAD")) {
			t.Fatal("M1 not on GitHub")
		}
		g := suivi()
		if !g.Lie || !g.Auto || g.Branche != "main" || g.Envoye == nil || !strings.HasSuffix(g.Envoye.Commit, "M1: level one") {
			t.Fatalf("after M1: %+v", g)
		}
		// the same commit: not tried again
		p.githubPousserNouveau()
		if n := lignesGH(); n != 1 {
			t.Fatalf("%d GitHub lines for one commit", n)
		}

		// someone else pushed meanwhile: refused, the reason kept, GitHub's last commit still M1
		autre := filepath.Join(t.TempDir(), "autre")
		gitEssai(t, filepath.Dir(autre), "clone", "-q", depot, autre)
		ecrireEssai(t, filepath.Join(autre, "b.txt"), "b\n")
		gitEssai(t, autre, "add", "-A")
		gitEssai(t, autre, "-c", "user.name=B", "-c", "user.email=b@example.com", "commit", "-q", "-m", "B's work")
		gitEssai(t, autre, "push", "-q", "origin", "main")
		commit("niveau2.txt", "M2: level two")
		p.githubPousserNouveau()
		g = suivi()
		if g.Envoye == nil || !strings.HasSuffix(g.Envoye.Commit, "M1: level one") || g.Dernier == nil || g.Dernier.Resultat != "rejete" || g.Dernier.Modele == "" {
			t.Fatalf("refused: %+v %+v", g.Envoye, g.Dernier)
		}

		// automatic push off: nothing pushed, said once
		r, _ := lireReglagesGH(dossierGH(m.projet))
		r.Auto = false
		if err := ecrireReglagesGH(dossierGH(m.projet), r); err != nil {
			t.Fatal(err)
		}
		commit("niveau3.txt", "M3: level three")
		p.githubPousserNouveau()
		commit("niveau4.txt", "M4: level four")
		p.githubPousserNouveau()
		b, _ := os.ReadFile(filepath.Join(p.prive, "pilot.log"))
		if strings.Count(string(b), "automatic push is off") != 1 {
			t.Fatalf("automatic push off, in the log:\n%s", b)
		}
		if g = suivi(); g.Auto {
			t.Fatal("github.json: automatic push still on")
		}
	})
}

// TestPartagerAtelier: ticking Share the workshop commits the shared workshop alone and pushes it at once
// (the request and the plan reach GitHub, the pilot's state and the code being written do not); Push now
// takes what changed in it since.
func TestPartagerAtelier(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	defer func() { depotEssaiGH = ancien }()

	base := t.TempDir()
	m := pcEssai{nom: "A", donnees: filepath.Join(base, "donnees"), projet: filepath.Join(base, "projet")}
	_ = os.MkdirAll(m.projet, 0o755)
	gitEssai(t, m.projet, "init", "-q", "-b", "main")
	gitEssai(t, m.projet, "config", "user.name", "A")
	gitEssai(t, m.projet, "config", "user.email", "a@example.com")
	_ = os.MkdirAll(filepath.Join(m.projet, ".athanor"), 0o755)
	if err := ecrireExclusionAtelier(m.projet, false); err != nil {
		t.Fatal(err)
	}
	lierPCEssai(t, m, "", "")
	ecrireEssai(t, filepath.Join(m.projet, "jeu.txt"), "v1\n")
	gitEssai(t, m.projet, "add", "-A")
	gitEssai(t, m.projet, "commit", "-q", "-m", "M1: start")
	ecrireEssai(t, filepath.Join(m.projet, ".athanor", "REQUEST.md"), "# A game\n\nA small game.\n")
	ecrireEssai(t, filepath.Join(m.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [x] M1 : start\n- [ ] M2 : levels\n")
	ecrireEssai(t, filepath.Join(m.projet, ".athanor", "STATE.json"), "{}\n")
	ecrireEssai(t, filepath.Join(m.projet, "jeu.txt"), "v2, half done\n") // Claude's work in progress
	surGitHub := func(f string) bool {
		return exec.Command("git", "--git-dir", depot, "cat-file", "-e", "main:"+f).Run() == nil
	}

	m.faire(t, func() {
		var o bytes.Buffer
		if code, _ := commandeGitHub([]string{"partage", m.projet, "1"}, &o); code != 0 {
			t.Fatalf("partage: %d %s", code, o.String())
		}
		if !surGitHub(".athanor/REQUEST.md") || !surGitHub(".athanor/PLAN.md") {
			t.Fatalf("the workshop not on GitHub: %s", o.String())
		}
		if surGitHub(".athanor/STATE.json") {
			t.Fatal("the pilot's state on GitHub")
		}
		if b, _ := exec.Command("git", "--git-dir", depot, "show", "main:jeu.txt").Output(); string(b) != "v1\n" {
			t.Fatalf("the code in progress went with the workshop: %q", b)
		}
		if b, _ := os.ReadFile(filepath.Join(m.projet, "jeu.txt")); string(b) != "v2, half done\n" {
			t.Fatal("the work in progress touched")
		}
		if s := gitEssai(t, m.projet, "status", "--porcelain", "--", "jeu.txt"); !strings.Contains(s, "jeu.txt") {
			t.Fatal("the work in progress was committed")
		}
		// the plan changes; Push now takes it
		ecrireEssai(t, filepath.Join(m.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [x] M1 : start\n- [x] M2 : levels\n")
		o.Reset()
		if code := pousserGH([]string{m.projet}, &o); code != 0 {
			t.Fatalf("Push now: %d %s", code, o.String())
		}
		if b, _ := exec.Command("git", "--git-dir", depot, "show", "main:.athanor/PLAN.md").Output(); !strings.Contains(string(b), "[x] M2") {
			t.Fatalf("Push now without the workshop's change: %q", b)
		}
		var g SuiviGitHub
		b, _ := os.ReadFile(filepath.Join(m.projet, ".athanor", "github.json"))
		if json.Unmarshal(b, &g) != nil || g.Envoye == nil || !strings.Contains(g.Envoye.Commit, "Share the workshop") {
			t.Fatalf("github.json: %s", b)
		}
	})
}

// TestEquipeAtelierPartage: switching Work with other computers on puts the workshop on GitHub at once,
// so that a computer joining right after gets the request and the plan (it waited for Claude's next
// commit, and the second computer could not start).
func TestEquipeAtelierPartage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien, ancienneAttente := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancien, ancienneAttente }()

	// A: a project with its work committed and pushed, its workshop not shared yet
	base := dossierEssaiPilote(t)
	a := pcEssai{nom: "PC-A", donnees: filepath.Join(base, "donnees"), projet: filepath.Join(base, "projet")}
	_ = os.MkdirAll(filepath.Join(a.projet, ".athanor"), 0o755)
	gitEssai(t, a.projet, "init", "-q", "-b", "main")
	gitEssai(t, a.projet, "config", "user.name", "A")
	gitEssai(t, a.projet, "config", "user.email", "a@example.com")
	if err := ecrireExclusionAtelier(a.projet, false); err != nil {
		t.Fatal(err)
	}
	lierPCEssai(t, a, "", "")
	ecrireEssai(t, filepath.Join(a.projet, "jeu.txt"), "v1\n")
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "REQUEST.md"), "# A game\n\nA small game.\n")
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), planEssai)
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "M0: start")
	a.faire(t, func() {
		var o bytes.Buffer
		if code := pousserGH([]string{a.projet, "--auto"}, &o); code != 0 {
			t.Fatalf("first push: %s", o.String())
		}
		o.Reset()
		if code, _ := commandeGitHub([]string{"pcs-activer", a.projet, "1"}, &o); code != 0 {
			t.Fatalf("A works with others: %d %s", code, o.String())
		}
	})
	if files := gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(files, ".athanor/REQUEST.md") || !strings.Contains(files, ".athanor/PLAN.md") {
		t.Fatalf("the workshop not on GitHub after the switch: %s", files)
	}

	// B joins from an empty folder: the request and the plan arrive
	b := pcVideEssai(t, "PC-B", nil)
	if code, out := activerEssai(t, b); code != 0 {
		t.Fatalf("B joins: %d %s", code, out)
	}
	if r, err := os.ReadFile(filepath.Join(b.projet, ".athanor", "REQUEST.md")); err != nil || !strings.Contains(string(r), "A small game") {
		t.Fatalf("B without the request: %v %q", err, r)
	}
	if _, err := os.Stat(filepath.Join(b.projet, ".athanor", "PLAN.md")); err != nil {
		t.Fatal("B without the plan")
	}
}
