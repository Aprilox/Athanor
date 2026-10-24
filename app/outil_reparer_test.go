//go:build !windows

package main

// athanor reparer: the diff applies (patch gives the new file back), and a real rebuild in a copy of the
// kit replaces the executable only when everything passes, keeps the previous one, and puts it back.

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffUnifie(t *testing.T) {
	if _, err := exec.LookPath("patch"); err != nil {
		t.Skip("no patch")
	}
	r := rand.New(rand.NewSource(3))
	d := t.TempDir()
	for essai := 0; essai < 60; essai++ {
		var a []string
		for i := 0; i < r.Intn(80); i++ {
			a = append(a, fmt.Sprintf("ligne %d", r.Intn(30)))
		}
		b := append([]string(nil), a...)
		for k := 0; k < r.Intn(8); k++ {
			switch i := r.Intn(len(b) + 1); r.Intn(3) {
			case 0:
				b = append(b[:i], append([]string{fmt.Sprintf("neuve %d", k)}, b[i:]...)...)
			case 1:
				if i < len(b) {
					b = append(b[:i], b[i+1:]...)
				}
			default:
				if i < len(b) {
					b[i] = "changée " + b[i]
				}
			}
		}
		texte := func(l []string) []byte {
			if len(l) == 0 {
				return []byte{}
			}
			return []byte(strings.Join(l, "\n") + "\n")
		}
		x, y := texte(a), texte(b)
		diff := diffUnifie("f.txt", x, y)
		if (diff == "") != (string(x) == string(y)) {
			t.Fatalf("essai %d: empty diff mismatch", essai)
		}
		if diff == "" {
			continue
		}
		f, p, o := filepath.Join(d, "f.txt"), filepath.Join(d, "f.diff"), filepath.Join(d, "sortie.txt")
		_ = os.WriteFile(f, x, 0o644)
		_ = os.WriteFile(p, []byte(diff), 0o644)
		os.Remove(o)
		if out, err := exec.Command("patch", "-s", "-o", o, f, p).CombinedOutput(); err != nil {
			t.Fatalf("essai %d: patch: %v %s\n%s", essai, err, out, diff)
		}
		if g, _ := os.ReadFile(o); string(g) != string(y) {
			t.Fatalf("essai %d: the diff does not give the new file back\n%s", essai, diff)
		}
	}
}

func TestReparerRecompiler(t *testing.T) {
	if testing.Short() {
		t.Skip("a real build")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	bin := binaireAthanor(t)
	d := t.TempDir()
	kit := filepath.Join(d, "kit")
	src, _ := filepath.Abs("..")
	for _, x := range []string{"app", "template", ".env.example"} {
		if out, err := exec.Command("cp", "-a", filepath.Join(src, x), filepath.Join(kit, x)).CombinedOutput(); err != nil {
			_ = os.MkdirAll(kit, 0o755)
			if out, err = exec.Command("cp", "-a", filepath.Join(src, x), filepath.Join(kit, x)).CombinedOutput(); err != nil {
				t.Fatalf("cp %s: %v %s", x, err, out)
			}
		}
	}
	os.RemoveAll(filepath.Join(kit, "app", "athanor"))
	exe := filepath.Join(kit, "athanor")
	b, _ := os.ReadFile(bin)
	_ = os.WriteFile(exe, b, 0o755)
	lancer := func(args ...string) (string, int) {
		c := exec.Command(exe, append([]string{"reparer"}, append(args, kit)...)...)
		out, err := c.CombinedOutput()
		code := 0
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		}
		return string(out), code
	}
	if out, code := lancer("--diff"); code != 0 || !strings.Contains(out, "No change") {
		t.Fatalf("diff of the same sources: %d %s", code, out)
	}
	// a change that does not build: nothing replaced
	f := filepath.Join(kit, "app", "commandes.go")
	orig, _ := os.ReadFile(f)
	_ = os.WriteFile(f, append(append([]byte(nil), orig...), []byte("\nfunc casse( {\n")...), 0o644)
	out, code := lancer("--recompiler")
	if code == 0 || !strings.Contains(out, "nothing replaced") || !strings.Contains(out, "+func casse( {") || estFichier(filepath.Join(kit, "athanor.ancien")) {
		t.Fatalf("a broken change: %d %s", code, out)
	}
	if nb, _ := os.ReadFile(exe); string(nb) != string(b) {
		t.Fatal("the executable was replaced by a broken build")
	}
	// a good change: built, autotested, put in place, the previous one kept
	_ = os.WriteFile(f, []byte(strings.Replace(string(orig), "prints the version of Athanor", "prints the version of Athanor (rebuilt)", 1)), 0o644)
	out, code = lancer("--recompiler")
	if code != 0 || !strings.Contains(out, "Done: athanor replaced") || !strings.Contains(out, "(rebuilt)") {
		t.Fatalf("a good change: %d %s", code, out)
	}
	aide, _ := exec.Command(exe, "aide").Output()
	if !strings.Contains(string(aide), "(rebuilt)") || !estFichier(filepath.Join(kit, "athanor.ancien")) {
		t.Fatalf("the new executable is not in place: %s", aide)
	}
	if out, _ := lancer("--diff"); !strings.Contains(out, "No change") {
		t.Fatalf("the new executable carries its own sources: %s", out)
	}
	// back to the previous one
	if out, code := lancer("--annuler"); code != 0 || !strings.Contains(out, "the previous one is back") {
		t.Fatalf("annuler: %d %s", code, out)
	}
	if aide, _ := exec.Command(exe, "aide").Output(); strings.Contains(string(aide), "(rebuilt)") {
		t.Fatal("annuler did not put the previous executable back")
	}
}

// The portable Go: the archive of go.dev's list for this PC, refused when its SHA-256 differs.
func TestInstallerGo(t *testing.T) {
	d := t.TempDir()
	// an archive like go.dev's: go/bin/go
	archive := filepath.Join(d, "go.tar.gz")
	src := filepath.Join(d, "src")
	ecrireFichier(t, filepath.Join(src, "go/bin/go"), "#!/bin/sh\necho go1.24.7\n", 0o755)
	if out, err := exec.Command("tar", "-C", src, "-czf", archive, "go").CombinedOutput(); err != nil {
		t.Skip("no tar: ", string(out))
	}
	b, _ := os.ReadFile(archive)
	somme := fmt.Sprintf("%x", sha256Somme(b))
	nom := fmt.Sprintf("go1.24.7.%s-%s.tar.gz", runtimeGOOS(), runtimeGOARCH())
	for _, bonne := range []bool{false, true} {
		s := somme
		if !bonne {
			s = strings.Repeat("0", 64)
		}
		srv := httptestServeur(t, map[string][]byte{
			"/dl/":       []byte(fmt.Sprintf(`[{"version": "go1.24.7", "files": [{"filename": %q, "os": %q, "arch": %q, "sha256": %q, "kind": "archive"}]}]`, nom, runtimeGOOS(), runtimeGOARCH(), s)),
			"/dl/" + nom: b,
		})
		vieux := urlGoDl
		urlGoDl = srv + "/dl/"
		dest := filepath.Join(d, fmt.Sprintf("portable-%v", bonne))
		err := installerGo("go1.24.7", dest)
		urlGoDl = vieux
		if bonne {
			if err != nil || versionDe(goExe(dest)) != [3]int{1, 24, 7} {
				t.Fatalf("install: %v", err)
			}
		} else if err == nil || estFichier(goExe(dest)) {
			t.Fatal("an archive with a wrong SHA-256 was installed")
		}
	}
}
