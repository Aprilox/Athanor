//go:build !windows

package main

// athanor github scanner | atelier | crochet against the Python written in system/github.sh (read from
// the script itself): the same lines, the same exit codes, the same files.

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pythonGitHub(t *testing.T, variable string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	return pythonDuScript(t, "github.sh", variable+"='", variable+"='\n", "\n'\n")
}

func scannerPython(t *testing.T, py, mode string, entree []byte) (string, int) {
	t.Helper()
	c := exec.Command("python3", "-I", py, mode)
	c.Stdin = bytes.NewReader(entree)
	out, err := c.Output()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func scannerGo(mode string, entree []byte) (string, int) {
	var b bytes.Buffer
	code := scannerSecrets(mode, bytes.NewReader(entree), &b)
	return b.String(), code
}

var jetonsDeTest = []string{
	"-----BEGIN OPENSSH PRIVATE KEY-----", "-----BEGIN PRIVATE KEY", "AKIA" + strings.Repeat("A1", 8), "ASIA" + strings.Repeat("Z", 16) + "x",
	"ghp_" + strings.Repeat("a", 36), "github_pat_" + strings.Repeat("A_", 21), "sk-ant-" + strings.Repeat("b-", 11),
	"sk-proj-" + strings.Repeat("c", 20) + "T3BlbkFJ" + strings.Repeat("d", 20), "sk-" + strings.Repeat("e", 48), "sk-" + strings.Repeat("e", 49),
	"xoxb-" + strings.Repeat("1", 10), "AIza" + strings.Repeat("f", 35), "AIza" + strings.Repeat("f", 34) + "-", "AIza" + strings.Repeat("f", 34) + "-é",
	"AIza" + strings.Repeat("g", 36), "glpat-" + strings.Repeat("h", 20), "sk_live_" + strings.Repeat("i", 20), "rk_live_" + strings.Repeat("j", 24),
	"npm_" + strings.Repeat("k", 36), "npm_" + strings.Repeat("k", 37), "hf_" + strings.Repeat("l", 34), "hf_" + strings.Repeat("l", 40) + "é",
	"M" + strings.Repeat("m", 23) + ".abcdef." + strings.Repeat("n", 27), "N" + strings.Repeat("m", 25) + ".abc-ef." + strings.Repeat("n", 26) + "-",
	"N" + strings.Repeat("m", 25) + ".abc-ef." + strings.Repeat("n", 26) + "-x", "nothing here", "TOKEN=abc", "ghp_short",
}

var bordsDeTest = []string{"", " ", "x", "_", "é", "-", "=", "\"", "1", "日", "٣", "\t", "/"}

// ligneAuHasard: a line with tokens between random edges (Python's \b against its Go emulation).
func ligneAuHasard(r *rand.Rand) string {
	var b strings.Builder
	for i := 0; i < 1+r.Intn(3); i++ {
		b.WriteString(bordsDeTest[r.Intn(len(bordsDeTest))])
		b.WriteString(jetonsDeTest[r.Intn(len(jetonsDeTest))])
		b.WriteString(bordsDeTest[r.Intn(len(bordsDeTest))])
	}
	return b.String()
}

func TestPariteScannerMessages(t *testing.T) {
	py := pythonGitHub(t, "SCANNER")
	r := rand.New(rand.NewSource(7))
	var b strings.Builder
	for i := 0; i < 400; i++ {
		if i%9 == 0 {
			fmt.Fprintf(&b, "\x01%040x\n", r.Int63())
		}
		b.WriteString(ligneAuHasard(r) + "\n")
	}
	b.WriteString("\x01é日\n" + jetonsDeTest[4] + "\n\xff\xfe invalid " + jetonsDeTest[0] + "\n")
	for _, e := range [][]byte{[]byte(b.String()), {}, []byte("\x01\n"), []byte("no newline at the end " + jetonsDeTest[5])} {
		ep, cp := scannerPython(t, py, "messages", e)
		eg, cg := scannerGo("messages", e)
		if ep != eg || cp != cg {
			t.Fatalf("messages:\nPython (%d):\n%s\nGo (%d):\n%s", cp, ep, cg, eg)
		}
	}
}

func TestPariteScannerDiff(t *testing.T) {
	py := pythonGitHub(t, "SCANNER")
	r := rand.New(rand.NewSource(11))
	fichiers := []string{"a.txt", ".env", ".env.example", ".env.local", "src/.env.defaults", "x/.env.prod", "id_rsa", "k/server.pem", "cert.P12",
		".athanor/PLAN.md", "sous/.athanor", ".claude/settings.json", "a/.claude/x", ".github/workflows/ci.yml", ".github/actions/a/x.yml",
		".github/dependabot.yml", `"caf\303\251/\"q\".txt"`, `"oct\377\\bad\x41.txt"`, `"\t\n.env"`, "été.txt"}
	var b strings.Builder
	for i := 0; i < 120; i++ {
		f := fichiers[r.Intn(len(fichiers))]
		switch r.Intn(7) {
		case 0:
			fmt.Fprintf(&b, "\x01%040x\n", r.Int63())
		case 1:
			b.WriteString("diff --cc " + f + "\n")
		}
		b.WriteString("diff --git a/x b/x\nindex 1..2 100644\n--- a/x\n")
		if r.Intn(8) == 0 {
			b.WriteString("+++ /dev/null\n")
		} else if strings.HasPrefix(f, `"`) {
			b.WriteString("+++ \"b/" + f[1:] + "\n")
		} else {
			b.WriteString("+++ b/" + f + "\n")
		}
		for h := 0; h < 1+r.Intn(3); h++ {
			a, n := r.Intn(4), r.Intn(4)
			if a+n == 0 {
				n = 1
			}
			l0 := 1 + r.Intn(50)
			entete := fmt.Sprintf("@@ -%d,%d +%d,%d @@ ctx\n", l0, a, l0, n)
			if a == 1 && r.Intn(2) == 0 {
				entete = fmt.Sprintf("@@ -%d +%d,%d @@\n", l0, l0, n)
			}
			b.WriteString(entete)
			for a > 0 || n > 0 {
				switch {
				case a > 0 && n > 0 && r.Intn(3) == 0:
					b.WriteString(" " + ligneAuHasard(r) + "\n")
					a--
					n--
				case a > 0 && (n == 0 || r.Intn(2) == 0):
					b.WriteString("-" + ligneAuHasard(r) + "\n")
					a--
				default:
					b.WriteString("+" + ligneAuHasard(r) + "\n")
					n--
				}
				if r.Intn(15) == 0 {
					b.WriteString("\\ No newline at end of file\n")
				}
			}
		}
	}
	bon := b.String()
	cas := map[string]string{
		"diff":       bon,
		"vide":       "",
		"tronque":    bon + "diff --git a/y b/y\n+++ b/y\n@@ -0,0 +1,3 @@\n+" + jetonsDeTest[6] + "\n",
		"intrus":     "+++ b/z\n@@ -1,2 +1,2 @@\n-a\nnot a diff line " + jetonsDeTest[4] + "\n+b\n",
		"ligne vide": "+++ b/z\n@@ -1 +1 @@\n\n+b\n",
		"binaire":    "+++ b/bin\n@@ -0,0 +1,2 @@\n+\x00\xff\xfe" + jetonsDeTest[8] + "\n+\r\n",
		"crlf":       "+++ b/w.txt\r\n@@ -1 +1 @@\r\n-a\r\n+" + jetonsDeTest[10] + "\r\n",
		"sans fin":   "+++ b/z\n@@ -1 +1 @@\n-a\n+" + jetonsDeTest[12],
	}
	for nom, e := range cas {
		ep, cp := scannerPython(t, py, "diff", []byte(e))
		eg, cg := scannerGo("diff", []byte(e))
		if ep != eg || cp != cg {
			t.Fatalf("%s:\nPython (%d):\n%q\nGo (%d):\n%q", nom, cp, ep, cg, eg)
		}
	}
}

// A real repository: git log -p of commits with secrets, renamed and binary files, unusual names.
func TestPariteScannerGit(t *testing.T) {
	py := pythonGitHub(t, "SCANNER")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	d := t.TempDir()
	git := func(args ...string) []byte {
		c := exec.Command("git", args...)
		c.Dir = d
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@b.c", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@b.c")
		out, err := c.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	git("init", "-q")
	ecrire := func(nom, contenu string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(d, nom)), 0o755)
		_ = os.WriteFile(filepath.Join(d, nom), []byte(contenu), 0o644)
	}
	ecrire("a.txt", "one\ntwo\n")
	git("add", "-A")
	git("commit", "-qm", "first "+jetonsDeTest[4])
	ecrire("a.txt", "one\nkey "+jetonsDeTest[6]+"\ntwo\n")
	ecrire("café \"q\".txt", "x "+jetonsDeTest[11]+"\n")
	ecrire(".env", "A=1\n")
	ecrire(".github/workflows/ci.yml", "on: push\n")
	ecrire("bin.dat", "\x00\x01\xff"+jetonsDeTest[8]+"\x00\n")
	git("add", "-A")
	git("commit", "-qm", "second\n\nbody "+jetonsDeTest[17])
	git("mv", "a.txt", "b.txt")
	ecrire("b.txt", "one\nkey "+jetonsDeTest[6]+"\ntwo\nthree "+jetonsDeTest[21]+"\n")
	git("add", "-A")
	git("commit", "-qm", "third")
	diff := git("log", "-p", "--text", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--diff-merges=first-parent", "--format=%x01%H", "-U0", "HEAD")
	msgs := git("log", "--format=%x01%H%n%B", "HEAD")
	for mode, e := range map[string][]byte{"diff": diff, "messages": msgs} {
		ep, cp := scannerPython(t, py, mode, e)
		eg, cg := scannerGo(mode, e)
		if ep != eg || cp != cg || !strings.HasSuffix(eg, "#FIN#\n") {
			t.Fatalf("%s:\nPython (%d):\n%s\nGo (%d):\n%s", mode, cp, ep, cg, eg)
		}
	}
}

// programmeGitHub: the Python of a github.sh function (atelier, crochet), as a file.
func programmeGitHub(t *testing.T, fonction string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	return pythonDuScript(t, "github.sh", "\n"+fonction+"() {", "python3 -I -c '\n", "\n' ")
}

// arbreGitHub: a folder's files (links shown as such, modes kept), to compare two runs.
func arbreGitHub(t *testing.T, racine string) string {
	var b strings.Builder
	_ = filepath.Walk(racine, func(p string, st os.FileInfo, err error) error {
		if err != nil || p == racine {
			return nil
		}
		r, _ := filepath.Rel(racine, p)
		r = strings.NewReplacer(fmt.Sprint(os.Getpid()), "PID").Replace(r)
		switch {
		case st.Mode()&os.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			fmt.Fprintf(&b, "%s -> %s\n", r, l)
		case st.IsDir():
			fmt.Fprintf(&b, "%s/ %v\n", r, st.Mode().Perm())
		default:
			c, _ := os.ReadFile(p)
			fmt.Fprintf(&b, "%s %v %q\n", r, st.Mode().Perm(), c)
		}
		return nil
	})
	return b.String()
}

func TestPariteAtelierEtCrochet(t *testing.T) {
	pyA, pyC := programmeGitHub(t, "atelier"), programmeGitHub(t, "crochet")
	dehors := t.TempDir()
	// each case prepares a project; then the action runs on a copy for Python and one for Go
	type cas struct {
		nom      string
		preparer func(p string)
		args     []string // atelier: NAME ecrire|effacer ; crochet: 0|1
	}
	lien := func(cible, l string) { _ = os.Symlink(cible, l) }
	mk := func(p ...string) { _ = os.MkdirAll(filepath.Join(p...), 0o755) }
	wr := func(contenu string, p ...string) { _ = os.WriteFile(filepath.Join(p...), []byte(contenu), 0o644) }
	ateliers := []cas{
		{"nouveau", func(p string) { mk(p, ".athanor") }, []string{"SECRETS_FOUND.md", "ecrire"}},
		{"remplace", func(p string) { mk(p, ".athanor"); wr("old", p, ".athanor", "SECRETS_FOUND.md") }, []string{"SECRETS_FOUND.md", "ecrire"}},
		{"lien fichier", func(p string) { mk(p, ".athanor"); lien(filepath.Join(dehors, "x"), filepath.Join(p, ".athanor", "SECRETS_FOUND.md")) }, []string{"SECRETS_FOUND.md", "ecrire"}},
		{"lien dossier", func(p string) { lien(dehors, filepath.Join(p, ".athanor")) }, []string{"SECRETS_FOUND.md", "ecrire"}},
		{"sans atelier", func(p string) {}, []string{"SECRETS_FOUND.md", "ecrire"}},
		{"efface", func(p string) { mk(p, ".athanor"); wr("x", p, ".athanor", "SECRETS_FOUND.md") }, []string{"SECRETS_FOUND.md", "effacer"}},
		{"efface absent", func(p string) { mk(p, ".athanor") }, []string{"SECRETS_FOUND.md", "effacer"}},
		{"efface lien", func(p string) { mk(p, ".athanor"); lien(filepath.Join(dehors, "y"), filepath.Join(p, ".athanor", "SECRETS_FOUND.md")) }, []string{"SECRETS_FOUND.md", "effacer"}},
		{"efface dossier", func(p string) { mk(p, ".athanor", "SECRETS_FOUND.md") }, []string{"SECRETS_FOUND.md", "effacer"}},
	}
	crochets := []cas{
		{"pose", func(p string) { mk(p, ".git", "hooks") }, []string{"1"}},
		{"pose sans hooks", func(p string) { mk(p, ".git") }, []string{"1"}},
		{"retire sans hooks", func(p string) { mk(p, ".git") }, []string{"0"}},
		{"retire", func(p string) { mk(p, ".git", "hooks"); wr("#!/bin/sh\n"+marqueCrochet+"\n", p, ".git", "hooks", "commit-msg") }, []string{"0"}},
		{"celui de l'utilisateur", func(p string) { mk(p, ".git", "hooks"); wr("#!/bin/sh\necho mine\n", p, ".git", "hooks", "commit-msg") }, []string{"1"}},
		{"celui de l'utilisateur, retire", func(p string) { mk(p, ".git", "hooks"); wr("#!/bin/sh\necho mine\n", p, ".git", "hooks", "commit-msg") }, []string{"0"}},
		{"remplace le nôtre", func(p string) { mk(p, ".git", "hooks"); wr(marqueCrochet+" old\n", p, ".git", "hooks", "commit-msg") }, []string{"1"}},
		{"lien hooks", func(p string) { mk(p, ".git"); lien(dehors, filepath.Join(p, ".git", "hooks")) }, []string{"1"}},
		{"lien commit-msg", func(p string) { mk(p, ".git", "hooks"); lien(filepath.Join(dehors, "h"), filepath.Join(p, ".git", "hooks", "commit-msg")) }, []string{"1"}},
		{"lien .git", func(p string) { lien(dehors, filepath.Join(p, ".git")) }, []string{"1"}},
		{"sans .git", func(p string) {}, []string{"1"}},
		{".git fichier", func(p string) { wr("gitdir: /elsewhere\n", p, ".git") }, []string{"1"}},
	}
	lancer := func(c cas, crochet bool) {
		pp, pg := filepath.Join(t.TempDir(), "p"), filepath.Join(t.TempDir(), "p")
		mk(pp)
		mk(pg)
		c.preparer(pp)
		c.preparer(pg)
		contenu := "# Secrets\n\n- `a`\n"
		var cmd *exec.Cmd
		var errGo error
		if crochet {
			cmd = exec.Command("python3", "-I", pyC, pp, c.args[0])
			errGo = crochetCommit(pg, c.args[0] == "1")
		} else {
			cmd = exec.Command("python3", "-I", pyA, pp, c.args[0], c.args[1])
			errGo = fichierAtelier(pg, c.args[0], c.args[1] == "effacer", strings.NewReader(contenu))
		}
		cmd.Stdin = strings.NewReader(contenu)
		_, errPy := cmd.Output()
		if (errPy == nil) != (errGo == nil) {
			t.Errorf("%s: Python %v, Go %v", c.nom, errPy, errGo)
		}
		if a, b := arbreGitHub(t, pp), arbreGitHub(t, pg); a != b {
			t.Errorf("%s:\nPython:\n%s\nGo:\n%s", c.nom, a, b)
		}
	}
	for _, c := range ateliers {
		lancer(c, false)
	}
	for _, c := range crochets {
		lancer(c, true)
	}
	if l, _ := os.ReadDir(dehors); len(l) != 0 {
		t.Errorf("written outside the project: %v", l)
	}
}
