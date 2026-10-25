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

// TestLierCompteGitHub: with this computer connected to GitHub, a project is linked to a repository with
// no window: the access checked at once, the default branch taken, a repository with workflows asked
// first, the first push done; disconnected, the project is no longer linked.
func TestLierCompteGitHub(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "dev", depot)
	ancien, pause := depotEssaiGH, pauseLierGH
	depotEssaiGH, pauseLierGH = depot, 0
	defer func() { depotEssaiGH, pauseLierGH = ancien, pause }()
	m := pcEssai{nom: "A", donnees: filepath.Join(t.TempDir(), "donnees"), projet: filepath.Join(t.TempDir(), "projet")}
	_ = os.MkdirAll(filepath.Join(m.projet, ".athanor"), 0o755)
	gitEssai(t, m.projet, "init", "-q", "-b", "dev")
	ecrireEssai(t, filepath.Join(m.projet, "jeu.txt"), "v1\n")
	gitEssai(t, m.projet, "add", "-A")
	gitEssai(t, m.projet, "-c", "user.name=A", "-c", "user.email=a@example.com", "commit", "-q", "-m", "start")
	lier := func(args ...string) (r ResultatLierCompte) {
		t.Helper()
		m.faire(t, func() {
			var o bytes.Buffer
			lierCompteGH(append([]string{m.projet}, args...), &o)
			l := strings.TrimSpace(o.String())
			if err := json.Unmarshal([]byte(l[strings.LastIndex(l, "\n")+1:]), &r); err != nil {
				t.Fatalf("%v: %s", err, o.String())
			}
		})
		return r
	}
	if r := lier("moi/jeu"); r.Resultat != "deconnecte" {
		t.Fatalf("not connected: %+v", r)
	}
	// connected (as connecter leaves it)
	m.faire(t, func() {
		c := dossierConnexionGH()
		_ = os.MkdirAll(c, 0o700)
		ecrireEssai(t, c+"/cle", "key\n")
		ecrireEssai(t, c+"/known_hosts", "github.com ssh-ed25519 AAAA\n")
		ecrireEssai(t, c+"/info", "login=moi\nnom=Moi\ncle_id=888\nlie_le=2026-10-25T10:00:00\n")
		ecrireEssai(t, c+"/compte", "login=moi\nemail=m@x.org\n")
	})
	// a repository with workflows: asked first, nothing linked
	autre := filepath.Join(t.TempDir(), "autre")
	gitEssai(t, filepath.Dir(autre), "clone", "-q", depot, autre)
	ecrireEssai(t, filepath.Join(autre, ".github", "workflows", "ci.yml"), "on: push\n")
	gitEssai(t, autre, "add", "-A")
	gitEssai(t, autre, "-c", "user.name=B", "-c", "user.email=b@example.com", "commit", "-q", "-m", "ci")
	gitEssai(t, autre, "push", "-q", "origin", "HEAD:dev")
	gitEssai(t, depot, "symbolic-ref", "HEAD", "refs/heads/dev")
	if r := lier("moi/jeu", "1", "", "1"); r.Resultat != "workflows" || r.Workflows != 1 || r.Branche != "dev" {
		t.Fatalf("workflows: %+v", r)
	}
	m.faire(t, func() {
		if _, lie := lireReglagesGH(dossierGH(m.projet)); lie {
			t.Fatal("linked before the user said yes to the workflows")
		}
	})
	// yes: linked, the default branch, the connection's key, the author proposed
	if r := lier("moi/jeu", "1", "", "1", "0", "1"); r.Resultat != "lie" || r.Branche != "dev" || r.Login != "moi" {
		t.Fatalf("link: %+v", r)
	}
	m.faire(t, func() {
		d := dossierGH(m.projet)
		r, lie := lireReglagesGH(d)
		if !lie || !r.Connexion || r.Depot != "moi/jeu" || r.Branche != "dev" || !r.Atelier || r.Compte != "moi" {
			t.Fatalf("settings: %+v %v", r, lie)
		}
		if b, _ := os.ReadFile(d + "/compte"); !strings.Contains(string(b), "m@x.org") {
			t.Fatal("the commit author not proposed from the connection")
		}
		if cle, _ := fichiersCleGH(d, r); cle != dossierConnexionGH()+"/cle" || !strings.Contains(commandeSSHGH(d, "22"), dossierConnexionGH()) {
			t.Fatalf("the project does not push with the connection's key: %s", cle)
		}
		// disconnected: no longer linked
		var o bytes.Buffer
		deconnecterGH(nil, &o)
		if _, lie := lireReglagesGH(d); lie {
			t.Fatal("still linked once the computer is disconnected")
		}
	})
	// a repository the account cannot reach
	depotEssaiGH = filepath.Join(t.TempDir(), "absent.git")
	m.faire(t, func() {
		c := dossierConnexionGH()
		_ = os.MkdirAll(c, 0o700)
		ecrireEssai(t, c+"/cle", "key\n")
		ecrireEssai(t, c+"/known_hosts", "github.com ssh-ed25519 AAAA\n")
		ecrireEssai(t, c+"/info", "login=moi\n")
	})
	if r := lier("moi/absent"); r.Resultat != "introuvable" {
		t.Fatalf("unreachable: %+v", r)
	}
	m.faire(t, func() {
		if r, _ := lireReglagesGH(dossierGH(m.projet)); r.Depot != "moi/jeu" {
			t.Fatalf("the settings of before not put back: %+v", r)
		}
	})
}
