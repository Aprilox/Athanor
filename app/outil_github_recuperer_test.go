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

// TestRecupererGitHub: two people on one repository, in turns (no Several computers): the one who takes
// over is told GitHub is ahead, brings its work in (fast-forward), is never merged over files not
// committed, and a conflict is left to Claude with nothing changed.
func TestRecupererGitHub(t *testing.T) {
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
	pousser := func(m pcEssai) {
		t.Helper()
		m.faire(t, func() {
			var o bytes.Buffer
			if code := pousserGH([]string{m.projet}, &o); code != 0 {
				t.Fatal(o.String())
			}
		})
	}
	recuperer := func(m pcEssai, args ...string) EtatRecuperer {
		t.Helper()
		var e EtatRecuperer
		m.faire(t, func() {
			var o bytes.Buffer
			recupererGH(append([]string{m.projet}, args...), &o)
			l := strings.TrimSpace(o.String())
			if err := json.Unmarshal([]byte(l[strings.LastIndex(l, "\n")+1:]), &e); err != nil {
				t.Fatalf("%v: %s", err, o.String())
			}
		})
		return e
	}
	nouveau := func(nom string, cloner bool) pcEssai {
		base := t.TempDir()
		m := pcEssai{nom: nom, donnees: filepath.Join(base, "donnees"), projet: filepath.Join(base, "projet")}
		if cloner {
			gitEssai(t, base, "clone", "-q", depot, m.projet)
		} else {
			_ = os.MkdirAll(m.projet, 0o755)
			gitEssai(t, m.projet, "init", "-q", "-b", "main")
		}
		gitEssai(t, m.projet, "config", "user.name", nom)
		gitEssai(t, m.projet, "config", "user.email", strings.ToLower(nom)+"@example.com")
		_ = os.MkdirAll(filepath.Join(m.projet, ".athanor"), 0o755)
		lierPCEssai(t, m, "", "") // linked alone: no Several computers
		return m
	}

	a := nouveau("A", false)
	ecrireEssai(t, filepath.Join(a.projet, "jeu.txt"), "v1\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "start")
	pousser(a)
	b := nouveau("B", true)
	if e := recuperer(b, "--verifier"); e.Resultat != "verifie" || e.Derriere != 0 || e.Devant != 0 {
		t.Fatalf("B, just cloned: %+v", e)
	}

	// A works and pushes; B is told before starting, then brings it in
	ecrireEssai(t, filepath.Join(a.projet, "niveau.txt"), "level 1\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "M1: level one")
	pousser(a)
	if e := recuperer(b, "--verifier"); e.Derriere != 1 || e.Devant != 0 || e.Sujet != "M1: level one" || e.Auteur != "A" || e.Quand == "" {
		t.Fatalf("B behind: %+v", e)
	}
	avant := gitEssai(t, b.projet, "rev-parse", "HEAD")
	if e := recuperer(b); e.Resultat != recuFusion {
		t.Fatalf("B brings it in: %+v", e)
	}
	if c, _ := os.ReadFile(filepath.Join(b.projet, "niveau.txt")); string(c) != "level 1\n" || gitEssai(t, b.projet, "rev-parse", "HEAD") == avant {
		t.Fatal("A's work not in B's folder")
	}
	if strings.TrimSpace(gitEssai(t, b.projet, "rev-parse", "HEAD")) != strings.TrimSpace(gitEssai(t, depot, "rev-parse", "main")) {
		t.Fatal("not a fast-forward: B had nothing of its own")
	}
	if e := recuperer(b); e.Resultat != recuAJour {
		t.Fatalf("B again: %+v", e)
	}

	// B has changes not committed: nothing merged over them
	ecrireEssai(t, filepath.Join(a.projet, "jeu.txt"), "v2 by A\n")
	gitEssai(t, a.projet, "commit", "-q", "-am", "A changes jeu")
	pousser(a)
	ecrireEssai(t, filepath.Join(b.projet, "jeu.txt"), "B's change, not committed\n")
	if e := recuperer(b); e.Resultat != recuSale {
		t.Fatalf("B with changes not committed: %+v", e)
	}
	if c, _ := os.ReadFile(filepath.Join(b.projet, "jeu.txt")); string(c) != "B's change, not committed\n" {
		t.Fatal("B's change lost")
	}
	// committed: the same line on both sides, a conflict left to Claude, nothing changed
	gitEssai(t, b.projet, "commit", "-q", "-am", "B changes jeu")
	tete := gitEssai(t, b.projet, "rev-parse", "HEAD")
	if e := recuperer(b, "--verifier"); e.Derriere != 1 || e.Devant != 1 {
		t.Fatalf("B diverged: %+v", e)
	}
	if e := recuperer(b); e.Resultat != recuConflit {
		t.Fatalf("B's conflict: %+v", e)
	}
	if gitEssai(t, b.projet, "rev-parse", "HEAD") != tete || gitEssai(t, b.projet, "status", "--porcelain", "--untracked-files=no") != "" {
		t.Fatal("B's folder changed by a merge that did not go through")
	}
	if _, err := os.Stat(filepath.Join(b.projet, ".athanor", "MERGE_NEEDED.md")); err != nil {
		t.Fatal("no MERGE_NEEDED.md for Claude")
	}

	// a project not linked: said so, nothing else
	c := pcEssai{nom: "C", donnees: t.TempDir(), projet: t.TempDir()}
	if e := recuperer(c, "--verifier"); e.Resultat != recuErreur {
		t.Fatalf("not linked: %+v", e)
	}
}
