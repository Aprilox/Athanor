package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cleEssai() string {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return base64.StdEncoding.EncodeToString(k)
}

// depotEquipeEssai: a local "GitHub" and a computer A whose project is linked to it.
func depotEquipeEssai(t *testing.T) (string, pcEssai) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	t.Cleanup(func() { depotEssaiGH = ancien })
	return depot, nouveauPCEssai(t, "PC-A", "", "", depot, false)
}

// The team's workshop on GitHub: only the team reads it (paths, contents and names encrypted), two
// computers write it one at a time (a write on an old version refused), and it synchronises like the
// master's store.
func TestMagasinGitHub(t *testing.T) {
	depot, a := depotEquipeEssai(t)
	cle := cleEssai()
	var m1, m2 *magasinGitHub
	a.faire(t, func() {
		var err error
		if m1, err = ouvrirMagasinGitHub(a.projet, cle); err != nil {
			t.Fatal(err)
		}
		m2, _ = ouvrirMagasinGitHub(a.projet, cle)
	})
	x, y := nouveauPCAtelier(t), nouveauPCAtelier(t)
	x.ecrire(t, "REQUEST.md", "# Request\n\nBuild the secret game.\n")
	x.ecrire(t, "PLAN.md", "# Plan\n\n- [ ] M1 : The secret level\n")
	a.faire(t, func() {
		if r := x.sync(t, m1); r.Version != 1 || len(r.Envoyes) != 2 {
			t.Fatalf("first: %+v", r)
		}
		if r := y.sync(t, m2); len(r.Recus) != 2 || y.lire("PLAN.md") != x.lire("PLAN.md") {
			t.Fatalf("second computer: %+v", r)
		}
	})
	// GitHub sees nothing of it
	tout := gitEssai(t, depot, "log", "-p", "--all", "--format=%s%n%b", "--", ".")
	for _, ref := range strings.Fields(gitEssai(t, depot, "for-each-ref", "--format=%(objectname)", refAtelierGH)) {
		for _, l := range strings.Split(gitEssai(t, depot, "ls-tree", ref), "\n") {
			f := strings.Fields(l)
			tout += f[3] + gitEssai(t, depot, "cat-file", "-p", f[2])
		}
	}
	for _, secret := range []string{"secret", "PLAN", "REQUEST", "Build", "M1"} {
		if strings.Contains(tout, secret) {
			t.Fatalf("GitHub can read %q", secret)
		}
	}
	if l := gitEssai(t, depot, "branch", "--list"); strings.Contains(l, "atelier") {
		t.Fatal("a branch for the workshop")
	}
	a.faire(t, func() {
		// a write on an old version: refused, then merged
		v1, _ := m1.dernier()
		v2, _ := m2.dernier()
		c := []byte("- log\n")
		if _, err := m1.ecrire(v1.Version, map[string]string{"LOG.md": shaAtelier(c)}, map[string][]byte{shaAtelier(c): c}, "x"); err != nil {
			t.Fatal(err)
		}
		if _, err := m2.ecrire(v2.Version, map[string]string{}, nil, "y"); !errors.Is(err, errConflitAtelier) {
			t.Fatalf("a write on an old version: %v", err)
		}
		// another team's key reads nothing
		autre, _ := ouvrirMagasinGitHub(a.projet, cleEssai())
		if _, err := autre.dernier(); err == nil || !strings.Contains(err.Error(), "not this team's") {
			t.Fatalf("another key: %v", err)
		}
		y.ecrire(t, "LOG.md", y.lire("LOG.md")+"- more\n")
		if r := y.sync(t, m2); !strings.Contains(strings.Join(r.Recus, ","), "LOG.md") && !strings.Contains(strings.Join(r.Envoyes, ","), "LOG.md") {
			t.Fatalf("after the race: %+v", r)
		}
	})
	// one entry per content (and the version): an unchanged file keeps its blob, never sent again
	var v ManifesteAtelier
	a.faire(t, func() { v, _ = m1.dernier() })
	avant := gitEssai(t, depot, "ls-tree", refAtelierGH)
	if n := len(strings.Split(avant, "\n")); n != len(v.Fichiers)+1 {
		t.Fatalf("%d entries for %d files", n, len(v.Fichiers))
	}
	x.ecrire(t, "LOG.md", "- a change of the log only\n")
	a.faire(t, func() { x.sync(t, m1) })
	apres := gitEssai(t, depot, "ls-tree", refAtelierGH)
	blobs := func(l string) map[string]bool {
		m := map[string]bool{}
		for _, x := range strings.Split(l, "\n") {
			m[strings.Fields(x)[2]] = true
		}
		return m
	}
	communs := 0
	for b := range blobs(apres) {
		if blobs(avant)[b] {
			communs++
		}
	}
	if communs != len(v.Fichiers)-1 {
		t.Fatalf("%d blobs kept of %d (only the log changed)", communs, len(v.Fichiers))
	}
}

// A private team's presences on GitHub: an id and a time only; the team reads them all.
func TestPresenceChiffree(t *testing.T) {
	depot, a := depotEquipeEssai(t)
	cle := cleEssai()
	a.faire(t, func() {
		d, miroir, r, err := contexteGH(a.projet)
		if err != nil {
			t.Fatal(err)
		}
		p := presenceLocalePC(a.projet, d)
		p.Nom, p.Jalon, p.Lien = "Tour secrète", "M7", "QUJDREVGR0hJSktMTU5PUA"
		if err := publierPresencePC(d, miroir, r, p, cle); err != nil {
			t.Fatal(err)
		}
		brut := gitEssai(t, depot, "cat-file", "-p", refPresencePC+p.ID+":pc.json") + gitEssai(t, depot, "log", "--format=%s", refPresencePC+p.ID)
		if strings.Contains(brut, "secrète") || strings.Contains(brut, "M7") || strings.Contains(brut, "QUJD") || !strings.Contains(brut, p.ID) {
			t.Fatalf("GitHub reads the presence: %s", brut)
		}
		if err := recupererPCs(d, miroir, r); err != nil {
			t.Fatal(err)
		}
		v := lirePCs(miroir, "autre000000a", cle)
		if len(v) != 1 || v[0].Nom != "Tour secrète" || v[0].Jalon != "M7" || v[0].Lien != "QUJDREVGR0hJSktMTU5PUA" || !v[0].Prive {
			t.Fatalf("the team reads it: %+v", v)
		}
		if v := lirePCs(miroir, "autre000000a", ""); len(v) != 0 {
			t.Fatalf("read without the key: %+v", v)
		}
		if v := lirePCs(miroir, "autre000000a", cleEssai()); len(v) != 0 {
			t.Fatalf("read with another team's key: %+v", v)
		}
	})
}

// A node whose master is out of reach goes on through the team's copy on GitHub; the master, back,
// merges it into its store.
func TestAtelierSecoursGitHub(t *testing.T) {
	_, a := depotEquipeEssai(t)
	cle := cleEssai()
	kit := t.TempDir()
	priveM, priveN := filepath.Join(kit, "maitre"), filepath.Join(kit, "noeud")
	etatM := filepath.Join(a.projet, ".athanor")
	b := pcVideEssai(t, "PC-B", nil)
	_ = os.MkdirAll(filepath.Join(b.projet, ".athanor"), 0o755)
	ecrireEssai(t, filepath.Join(priveM, "equipe-directe.json"), `{"role":"maitre","equipe":"e","cle":"`+cle+`","atelier_lien":true}`)
	ecrireEssai(t, filepath.Join(priveN, "equipe-directe.json"), `{"role":"noeud","equipe":"e","cle":"`+cle+`","adresse":"http://127.0.0.1:1","jeton":"j","atelier_lien":true}`)
	ecrireEssai(t, filepath.Join(etatM, "REQUEST.md"), "# Request\n")
	maitre := &pilote{prive: priveM, etat: etatM, projet: a.projet}
	// the master: its store, then its copy on GitHub
	a.faire(t, func() {
		if !maitre.atelierLien() {
			t.Fatal("not through the link")
		}
	})
	journal := func(prive string) string { b, _ := os.ReadFile(filepath.Join(prive, "pilot.log")); return string(b) }
	if strings.Contains(journal(priveM), "not up to date") {
		t.Fatalf("the master's copy on GitHub: %s", journal(priveM))
	}
	// the node, its master out of reach: through GitHub (it shares the project's GitHub link: same folder)
	noeud := &pilote{prive: priveN, etat: filepath.Join(b.projet, ".athanor"), projet: a.projet}
	a.faire(t, func() {
		noeud.atelierLien()
	})
	if b, _ := os.ReadFile(filepath.Join(b.projet, ".athanor", "REQUEST.md")); string(b) != "# Request\n" || !strings.Contains(journal(priveN), "through the team's copy on GitHub") {
		t.Fatalf("the node through GitHub: %q\n%s", b, journal(priveN))
	}
	ecrireEssai(t, filepath.Join(b.projet, ".athanor", "LOG.md"), "- M1 by B, the master away\n")
	a.faire(t, func() { noeud.atelierLien() })
	// the master, back: B's log comes into its workshop and its store
	a.faire(t, func() { maitre.atelierLien() })
	if l, _ := os.ReadFile(filepath.Join(etatM, "LOG.md")); !strings.Contains(string(l), "M1 by B") {
		t.Fatalf("the master did not merge GitHub's copy:\n%s", journal(priveM))
	}
	v, _ := (magasinLocal{filepath.Join(priveM, "atelier-equipe")}).dernier()
	if v.Fichiers["LOG.md"] == "" {
		t.Fatalf("not in the master's store: %+v\n%s", v, journal(priveM))
	}
}
