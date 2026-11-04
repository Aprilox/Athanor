package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GitHub's refusals are told apart from the computer's own errors, and from a connection's key not accepted.
func TestRefusCleGH(t *testing.T) {
	for _, s := range []string{"git@github.com: Permission denied (publickey).\r\nfatal: Could not read from remote repository.",
		"ERROR: Permission to Aprilox/APOGEE.git denied to autre.", "ERROR: Repository not found.", "ERROR: The key you are authenticating with has been marked as read only."} {
		if !contientGH(s, motifRefusGH) {
			t.Errorf("GitHub's refusal not seen: %q", s)
		}
	}
	for _, s := range []string{"error: cannot open .git/FETCH_HEAD: Permission denied", "fatal: Unable to create 'x/index.lock': Permission denied", "Read-only file system"} {
		if contientGH(s, motifRefusGH) {
			t.Errorf("a local error taken for GitHub's refusal: %q", s)
		}
	}
	m, a := refusCleGH(reglagesGH{Depot: "moi/x", Connexion: true}, "git@github.com: Permission denied (publickey).")
	if !strings.HasPrefix(m, "GitHub does not accept this computer's key") || a[0] != "moi/x" {
		t.Fatal(m)
	}
	if m, _ := refusCleGH(reglagesGH{Depot: "moi/x", Connexion: true, Compte: "moi"}, "ERROR: Repository not found."); !strings.HasPrefix(m, "Your GitHub account ({1}) cannot push to {0}") {
		t.Fatal(m)
	}
	if m, _ := refusCleGH(reglagesGH{Depot: "moi/x"}, "ERROR: Repository not found."); !strings.HasPrefix(m, "GitHub refused Athanor's key") {
		t.Fatal(m)
	}
	if d := detailSortieGH("a\n\nb\r\nc\nfatal: d\n"); d != "b · c · fatal: d" {
		t.Fatal(d)
	}
}

// Disconnected, a project of the connection stays linked and says why it does not push; connected again,
// the projects refused for want of the connection say they push again.
func TestReprendreProjetsGH(t *testing.T) {
	t.Setenv("ATHANOR_GITHUB_DIR", filepath.Join(t.TempDir(), "github"))
	projet := filepath.Join(t.TempDir(), "jeu")
	_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
	d := dossierGH(projet)
	_ = os.MkdirAll(d, 0o700)
	if err := ecrireReglagesGH(d, reglagesGH{Depot: "moi/jeu", Branche: "main", Auto: true, Compte: "moi", Connexion: true}); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(d+"/projet", []byte(cleProjetGH(projet)+"\n"), 0o644)
	var b bytes.Buffer
	if code := pousserGH([]string{projet, "--auto"}, &b); code != 1 || !strings.Contains(b.String(), "no longer connected to GitHub") {
		t.Fatalf("%d %s", code, b.String())
	}
	if r, lie := lireReglagesGH(d); !lie || r.Depot != "moi/jeu" {
		t.Fatal("unlinked")
	}
	b.Reset()
	if code := testerGH([]string{projet}, &b); code != 1 || !strings.Contains(b.String(), "no longer connected to GitHub") {
		t.Fatalf("test: %d %s", code, b.String())
	}
	// another project, refused for another reason: left as it is
	autre := filepath.Join(t.TempDir(), "autre")
	_ = os.MkdirAll(filepath.Join(autre, ".athanor"), 0o755)
	da := dossierGH(autre)
	_ = os.MkdirAll(da, 0o700)
	_ = ecrireReglagesGH(da, reglagesGH{Depot: "moi/autre", Connexion: true})
	noterGH(da, "rejete", "A file is larger than GitHub's limit (100 MB): keep it out of git (.gitignore) or use Git LFS.", "", noteGitHub{})
	b.Reset()
	reprendreProjetsGH(&b)
	lire := func(d string) (n struct{ Resultat, Modele string }) {
		x, _ := os.ReadFile(d + "/dernier.json")
		_ = json.Unmarshal(x, &n)
		return n
	}
	if n := lire(d); n.Resultat != "attente" || !strings.HasPrefix(n.Modele, "Connected to GitHub again") || !strings.Contains(b.String(), "moi/jeu") {
		t.Fatalf("%+v %s", n, b.String())
	}
	if n := lire(da); n.Resultat != "rejete" || strings.Contains(b.String(), "moi/autre") {
		t.Fatalf("the other: %+v", n)
	}
	if g, _ := os.ReadFile(filepath.Join(projet, ".athanor", "github.json")); !strings.Contains(string(g), `"attente"`) {
		t.Fatalf("the Overview's line: %s", g)
	}
}
