package main

import (
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
