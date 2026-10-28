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

// projetSansLienEssai: a repository GitHub created with files of its own (a README, a licence: a history
// sharing nothing with the project's), and a project of its own, linked to it (one computer).
func projetSansLienEssai(t *testing.T, fichiersGH map[string]string) (depot string, p pcEssai, shaGH string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	depot = filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien := depotEssaiGH
	depotEssaiGH = depot
	t.Cleanup(func() { depotEssaiGH = ancien })
	g := filepath.Join(t.TempDir(), "github-web")
	gitEssai(t, filepath.Dir(g), "clone", "-q", depot, g)
	for f, c := range fichiersGH {
		ecrireEssai(t, filepath.Join(g, f), c)
	}
	gitEssai(t, g, "add", "-A")
	gitEssai(t, g, "-c", "user.name=GitHub", "-c", "user.email=noreply@github.com", "commit", "-q", "-m", "Initial commit")
	gitEssai(t, g, "push", "-q", "origin", "HEAD:main")
	shaGH = gitEssai(t, g, "rev-parse", "HEAD")
	p = pcVideEssai(t, "PC", nil)
	gitEssai(t, p.projet, "init", "-q", "-b", "main")
	gitEssai(t, p.projet, "config", "user.name", "Moi")
	gitEssai(t, p.projet, "config", "user.email", "moi@example.com")
	ecrireEssai(t, filepath.Join(p.projet, ".git", "info", "exclude"), ".athanor/\n")
	ecrireEssai(t, filepath.Join(p.projet, "app.txt"), "the game\n")
	ecrireEssai(t, filepath.Join(p.projet, "README.md"), "# The game (the project's)\n")
	gitEssai(t, p.projet, "add", "-A")
	gitEssai(t, p.projet, "commit", "-q", "-m", "M1: the game")
	return depot, p, shaGH
}

func recupererEssai(t *testing.T, p pcEssai, opt string) EtatRecuperer {
	t.Helper()
	var b bytes.Buffer
	args := []string{p.projet}
	if opt != "" {
		args = append(args, opt)
	}
	p.faire(t, func() { recupererGH(args, &b) })
	var e EtatRecuperer
	l := strings.Split(strings.TrimSpace(b.String()), "\n")
	if err := json.Unmarshal([]byte(l[len(l)-1]), &e); err != nil {
		t.Fatalf("recuperer %s: %s", opt, b.String())
	}
	return e
}

func pousserEssai(t *testing.T, p pcEssai, args ...string) (int, string) {
	t.Helper()
	var b strings.Builder
	code := 0
	p.faire(t, func() { code = pousserGH(append([]string{p.projet}, args...), &b) })
	return code, b.String()
}

// A repository made on GitHub with a README and a licence: the push is refused and says what to do; its
// files brought into the project (both histories kept), then the push goes through.
func TestIntegrerBrancheSansLien(t *testing.T) {
	depot, p, sha := projetSansLienEssai(t, map[string]string{"LICENSE": "MIT\n", ".gitignore": "build/\n"})
	if code, out := pousserEssai(t, p); code != 5 {
		t.Fatalf("push: %d %s", code, out)
	}
	d := ""
	p.faire(t, func() { d = dossierGH(p.projet) })
	if b, _ := os.ReadFile(filepath.Join(d, "dernier.json")); !strings.Contains(string(b), "replace GitHub's branch with this project there") {
		t.Fatalf("the refusal says what to do: %s", b)
	}
	v := recupererEssai(t, p, "--verifier")
	if v.Resultat != "verifie" || !v.SansLien || v.Derriere != 1 || v.Devant != 1 || v.Sha != sha || v.Sujet != "Initial commit" {
		t.Fatalf("verifier: %+v", v)
	}
	if e := recupererEssai(t, p, ""); e.Resultat != recuBloque || !e.SansLien {
		t.Fatalf("recuperer: %+v", e)
	}
	if e := recupererEssai(t, p, "--integrer"); e.Resultat != recuFusion {
		t.Fatalf("integrer: %+v", e)
	}
	for _, f := range []string{"LICENSE", ".gitignore", "app.txt", "README.md"} {
		if _, err := os.Stat(filepath.Join(p.projet, f)); err != nil {
			t.Fatalf("%s missing after the merge", f)
		}
	}
	if code, out := pousserEssai(t, p); code != 0 {
		t.Fatalf("push after: %d %s", code, out)
	}
	if l := gitEssai(t, depot, "log", "--format=%s", "main"); !strings.Contains(l, "Initial commit") || !strings.Contains(l, "M1: the game") {
		t.Fatalf("GitHub's history: %s", l)
	}
	// a team's project never merges two histories
	p.faire(t, func() {
		r, _ := lireReglagesGH(dossierGH(p.projet))
		r.Equipe = true
		_ = ecrireReglagesGH(dossierGH(p.projet), r)
	})
	if e := recupererEssai(t, p, "--integrer"); e.Resultat != recuBloque || !strings.Contains(e.Detail, "several computers") {
		t.Fatalf("a team: %+v", e)
	}
}

// Both have a README: nothing is changed, Claude merges them at its next cycle (both histories kept).
func TestIntegrerBrancheSansLienConflit(t *testing.T) {
	_, p, _ := projetSansLienEssai(t, map[string]string{"README.md": "# repo\n\nMade on GitHub.\n"})
	avant := gitEssai(t, p.projet, "rev-parse", "HEAD")
	if e := recupererEssai(t, p, "--integrer"); e.Resultat != recuConflit {
		t.Fatalf("integrer: %+v", e)
	}
	if apres := gitEssai(t, p.projet, "rev-parse", "HEAD"); apres != avant || gitEssai(t, p.projet, "status", "--porcelain", "--untracked-files=no") != "" {
		t.Fatal("the project changed")
	}
	m, _ := os.ReadFile(filepath.Join(p.projet, ".athanor", "MERGE_NEEDED.md"))
	if !strings.Contains(string(m), "`git merge --allow-unrelated-histories refs/remotes/athanor/main`") || !strings.Contains(string(m), "Bring GitHub's files in first") || !strings.Contains(string(m), "`README.md`") {
		t.Fatalf("MERGE_NEEDED.md:\n%s", m)
	}
	// what Claude then runs works
	c := exec.Command("git", "merge", "--allow-unrelated-histories", "-X", "ours", "--no-edit", "refs/remotes/athanor/main")
	c.Dir = p.projet
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("Claude's merge: %v %s", err, out)
	}
}

// GitHub's branch replaced with the project: only as the user saw it (that commit), never for a team, never
// by the automatic push; the secret check still runs.
func TestRemplacerBrancheGitHub(t *testing.T) {
	depot, p, sha := projetSansLienEssai(t, map[string]string{"README.md": "# repo\n"})
	if code, out := pousserEssai(t, p, "--remplacer="+strings.Repeat("a", 40)); code != 5 {
		t.Fatalf("replace with another commit than GitHub's: %d %s", code, out)
	}
	d := ""
	p.faire(t, func() { d = dossierGH(p.projet) })
	if b, _ := os.ReadFile(filepath.Join(d, "dernier.json")); !strings.Contains(string(b), "changed since you looked at it") {
		t.Fatalf("a stale replace: %s", b)
	}
	if gitEssai(t, depot, "rev-parse", "main") != sha {
		t.Fatal("GitHub's branch changed")
	}
	if code, _ := pousserEssai(t, p, "--auto", "--remplacer="+sha); code != 2 {
		t.Fatal("the automatic push replaced the branch")
	}
	if code, out := pousserEssai(t, p, "--remplacer="+sha); code != 0 {
		t.Fatalf("replace: %d %s", code, out)
	}
	if gitEssai(t, depot, "rev-parse", "main") != gitEssai(t, p.projet, "rev-parse", "HEAD") {
		t.Fatal("GitHub's branch not replaced")
	}
	// a secret is never pushed this way either
	ecrireEssai(t, filepath.Join(p.projet, ".env"), "API_KEY=sk-ant-api03-"+strings.Repeat("x", 90)+"\n")
	gitEssai(t, p.projet, "add", "-f", ".env")
	gitEssai(t, p.projet, "commit", "-q", "-m", "oops")
	if code, _ := pousserEssai(t, p, "--remplacer="+gitEssai(t, depot, "rev-parse", "main")); code != 4 {
		t.Fatalf("a secret replaced GitHub's branch: %d", code)
	}
	p.faire(t, func() {
		r, _ := lireReglagesGH(dossierGH(p.projet))
		r.Equipe = true
		_ = ecrireReglagesGH(dossierGH(p.projet), r)
	})
	if code, out := pousserEssai(t, p, "--remplacer="+gitEssai(t, depot, "rev-parse", "main")); code != 2 || !strings.Contains(out, "Several computers") {
		t.Fatalf("a team: %d %s", code, out)
	}
}
