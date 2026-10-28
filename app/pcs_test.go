package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// a computer of the test: its own Athanor data (GitHub settings, machine), its own copy of the project
type pcEssai struct {
	nom, donnees, projet string
}

func (m pcEssai) faire(t *testing.T, f func()) {
	t.Helper()
	avant, avait := os.LookupEnv("ATHANOR_GITHUB_DIR")
	os.Setenv("ATHANOR_GITHUB_DIR", m.donnees)
	defer func() {
		if avait {
			os.Setenv("ATHANOR_GITHUB_DIR", avant)
		} else {
			os.Unsetenv("ATHANOR_GITHUB_DIR")
		}
	}()
	f()
}

func gitEssai(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func ecrireEssai(t *testing.T, f, contenu string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(contenu), 0o644); err != nil {
		t.Fatal(err)
	}
}

// nouveauPCEssai: a computer whose project is linked to the test's "GitHub" with several computers on
func nouveauPCEssai(t *testing.T, nom, mots, capacites string, depot string, cloner bool) pcEssai {
	t.Helper()
	base := dossierEssaiPilote(t) // on Windows, a folder whose removal tolerates a program of the pilot still closing
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
	lierPCEssai(t, m, mots, capacites)
	m.faire(t, func() {
		var out bytes.Buffer
		if code := pcsActiverGH([]string{m.projet, "1"}, &out); code != 0 {
			t.Fatal(out.String())
		}
	})
	return m
}

// pcVideEssai: a computer that adds the project from a folder of its own (no git yet), linked to the
// same repository, not yet working with the others.
func pcVideEssai(t *testing.T, nom string, preparer func(projet string)) pcEssai {
	t.Helper()
	base := dossierEssaiPilote(t) // on Windows, a folder whose removal tolerates a program of the pilot still closing
	m := pcEssai{nom: nom, donnees: filepath.Join(base, "donnees"), projet: filepath.Join(base, "projet")}
	_ = os.MkdirAll(filepath.Join(m.projet, ".athanor"), 0o755)
	if preparer != nil {
		preparer(m.projet)
	}
	lierPCEssai(t, m, "", "")
	return m
}

func activerEssai(t *testing.T, m pcEssai) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := 0
	m.faire(t, func() { code = pcsActiverGH([]string{m.projet, "1"}, &out) })
	return code, out.String()
}

func lierPCEssai(t *testing.T, m pcEssai, mots, capacites string) {
	t.Helper()
	m.faire(t, func() {
		d := dossierGH(m.projet)
		_ = os.MkdirAll(d, 0o700)
		ecrireEssai(t, d+"/cle", "test key\n")
		ecrireEssai(t, d+"/known_hosts", "github.com ssh-ed25519 AAAA\n")
		if err := ecrireReglagesGH(d, reglagesGH{Depot: "aprilox/essai", Branche: "main", Auto: true}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if code := pcsMachineGH([]string{"--nom", m.nom, "--capacites", capacites, "--mots", mots}, &out); code != 0 {
			t.Fatal(out.String())
		}
	})
}

const planEssai = `# Plan

- [ ] M1 : Armor models — the 3D armors. Needs: gpu. Files: models/. Acceptance: three armors.
- [ ] M2 : Menus — the menus. Files: ui/.
- [ ] M3 : Assets download — the big sound packs. Needs: network.
- [ ] M4 : Armor colors — painted armors. Depends on: M1. Files: models/colors/.
`

func cocherEssai(t *testing.T, projet, id string) {
	t.Helper()
	f := filepath.Join(projet, ".athanor", "PLAN.md")
	b, _ := os.ReadFile(f)
	nb := strings.Replace(string(b), "- [ ] "+id+" ", "- [x] "+id+" ", 1)
	ecrireEssai(t, f, nb)
}

func vuePCsEssai(t *testing.T, m pcEssai, args ...string) VuePCs {
	t.Helper()
	var v VuePCs
	m.faire(t, func() {
		var out bytes.Buffer
		pcsGH(append([]string{m.projet}, args...), &out)
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("pcs: %v %s", err, out.String())
		}
	})
	return v
}

func piloteEssai(t *testing.T, m pcEssai) *pilote {
	return &pilote{projet: m.projet, etat: filepath.Join(m.projet, ".athanor"), prive: t.TempDir()}
}

// TestPlusieursOrdinateurs: four computers on one project, with the user's git as on Linux or macOS (LF)
// and as Git for Windows installs it (core.autocrlf=true: files checked out with CRLF).
func TestPlusieursOrdinateurs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	for _, crlf := range []bool{false, true} {
		nom := "LF"
		if crlf {
			nom = "CRLF"
		}
		t.Run(nom, func(t *testing.T) {
			conf := filepath.Join(t.TempDir(), "gitconfig")
			contenu := ""
			if crlf {
				contenu = "[core]\n\tautocrlf = true\n"
			}
			ecrireEssai(t, conf, contenu)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1") // the same git configuration for the test and for Athanor
			t.Setenv("GIT_CONFIG_GLOBAL", conf)
			essaiPlusieursOrdinateurs(t)
		})
	}
}

func essaiPlusieursOrdinateurs(t *testing.T) {
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancienDepot, ancienneAttente := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancienDepot, ancienneAttente }()

	// A, with a big graphics card, starts the project and pushes it with its plan (shared workshop)
	a := nouveauPCEssai(t, "PC-GPU", "GPU, 3D", "RTX 4090, 24 GB", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "# Game\n\nfirst line\n")
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), planEssai)
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "LOG.md"), "# Log\n")
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "STATE.json"), `{"phase":"travail"}`) // the pilot's, never shared
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "M0: start")
	var out bytes.Buffer
	a.faire(t, func() {
		if code := pousserGH([]string{a.projet}, &out); code != 0 {
			t.Fatalf("first push: %d %s", code, out.String())
		}
	})
	if files := gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(files, ".athanor/PLAN.md") || strings.Contains(files, "STATE.json") {
		t.Fatalf("shared workshop on GitHub: %s", files)
	}

	// B, on a fast network, then C, with nothing special, take the same repository
	b := nouveauPCEssai(t, "PC-Fibre", "network", "1 Gb/s fibre", depot, true)
	c := nouveauPCEssai(t, "PC-Bureau", "", "", depot, true)

	// each one says it is there: the others see it, online
	for _, m := range []pcEssai{a, b, c} {
		v := vuePCsEssai(t, m, "--pilote", "1", "--publier")
		if !v.Actif || v.Erreur != "" || v.Moi == nil || v.Moi.Nom != m.nom {
			t.Fatalf("%s: %+v", m.nom, v)
		}
	}
	v := vuePCsEssai(t, a)
	if len(v.PCs) != 2 || !v.PCs[0].EnLigne || !v.PCs[1].EnLigne {
		t.Fatalf("A sees: %+v", v.PCs)
	}
	noms := v.PCs[0].Nom + "," + v.PCs[1].Nom
	if noms != "PC-Bureau,PC-Fibre" {
		t.Fatalf("A sees %s", noms)
	}
	if v.PCs[1].Capacites != "1 Gb/s fibre" || strings.Join(v.PCs[1].Mots, ",") != "network" {
		t.Fatalf("B's capacities: %+v", v.PCs[1])
	}

	// the work is shared out: A the GPU milestone, B the network one, C what is left
	prendre := func(m pcEssai, attendu string) *equipePC {
		t.Helper()
		var e *equipePC
		m.faire(t, func() {
			var err error
			e, err = ouvrirEquipePC(m.projet)
			if err != nil || e == nil {
				t.Fatalf("%s team: %v", m.nom, err)
			}
			if err := e.lire(); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(m.projet, ".athanor", "PLAN.md"))
			j, att, raison := choisirJalonPC(analyserPlan(string(b)), e.moi, e.pcs, lireEtatLocalPC(e.d).Jalon)
			if att || j.ID != attendu {
				t.Fatalf("%s chooses %q (wait %v: %s), not %s", m.nom, j.ID, att, raison, attendu)
			}
			if pris, err := e.prendre(j); !pris || err != nil {
				t.Fatalf("%s takes %s: %v %v", m.nom, j.ID, pris, err)
			}
		})
		return e
	}
	ea := prendre(a, "M1")
	eb := prendre(b, "M3")
	prendre(c, "M2")
	// everything left is held, or waits for M1: a fourth computer waits
	d := nouveauPCEssai(t, "PC-Quatre", "", "", depot, true)
	d.faire(t, func() {
		e, _ := ouvrirEquipePC(d.projet)
		_ = e.lire()
		b, _ := os.ReadFile(filepath.Join(d.projet, ".athanor", "PLAN.md"))
		if j, att, raison := choisirJalonPC(analyserPlan(string(b)), e.moi, e.pcs, ""); !att || !strings.Contains(raison, "PC-GPU: M1") {
			t.Fatalf("D: %q %v %q", j.ID, att, raison)
		}
	})
	// the same milestone taken twice: the earlier keeps it
	d.faire(t, func() {
		e, _ := ouvrirEquipePC(d.projet)
		time.Sleep(1100 * time.Millisecond)
		if pris, _ := e.prendre(Jalon{ID: "M2", Titre: "M2 : Menus"}); pris {
			t.Fatal("D kept M2, taken by C before")
		}
		if lireEtatLocalPC(e.d).Jalon != "" {
			t.Fatal("D still holds M2")
		}
	})
	v = vuePCsEssai(t, a)
	for _, p := range v.PCs {
		attendu := map[string]string{"PC-Fibre": "M3", "PC-Bureau": "M2", "PC-Quatre": ""}[p.Nom]
		if p.Jalon != attendu {
			t.Fatalf("A sees %s on %q, not %q", p.Nom, p.Jalon, attendu)
		}
	}

	// A and B build at the same time and push: B's push merges A's work first (the log joined)
	travailler := func(m pcEssai, e *equipePC, id, fichier, ligne string) int {
		t.Helper()
		ecrireEssai(t, filepath.Join(m.projet, fichier), ligne+"\n")
		cocherEssai(t, m.projet, id)
		f := filepath.Join(m.projet, ".athanor", "LOG.md")
		l, _ := os.ReadFile(f)
		ecrireEssai(t, f, string(l)+"\n## "+id+" by "+m.nom+"\n")
		gitEssai(t, m.projet, "add", "-A")
		gitEssai(t, m.projet, "commit", "-q", "-m", id+": done by "+m.nom)
		var code int
		m.faire(t, func() {
			p := piloteEssai(t, m)
			p.equipePC = e
			code, _ = p.pousserPC()
		})
		return code
	}
	if code := travailler(a, ea, "M1", "models/armor.glb", "armor"); code != 0 {
		t.Fatalf("A's push: %d", code)
	}
	if code := travailler(b, eb, "M3", "sounds/pack.ogg", "sounds"); code != 0 {
		t.Fatalf("B's push: %d", code)
	}
	tout := gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main")
	if !strings.Contains(tout, "models/armor.glb") || !strings.Contains(tout, "sounds/pack.ogg") {
		t.Fatalf("GitHub has: %s", tout)
	}
	log := gitEssai(t, depot, "show", "main:.athanor/LOG.md")
	plan := gitEssai(t, depot, "show", "main:.athanor/PLAN.md")
	if !strings.Contains(log, "M1 by PC-GPU") || !strings.Contains(log, "M3 by PC-Fibre") || !strings.Contains(plan, "- [x] M1 ") || !strings.Contains(plan, "- [x] M3 ") {
		t.Fatalf("merged log and plan:\n%s\n%s", log, plan)
	}
	// A, at its next cycle, gets B's work; and M4 (which waited for M1) is now free, for A (GPU)
	a.faire(t, func() {
		p := piloteEssai(t, a)
		ecrireEssai(t, filepath.Join(p.prive, "x"), "")
		if att, raison := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M4" {
			t.Fatalf("A's next cycle: wait %v %q, %+v", att, raison, p.jalonPC)
		}
	})
	if _, err := os.Stat(filepath.Join(a.projet, "sounds", "pack.ogg")); err != nil {
		t.Fatal("A did not get B's work")
	}
	if g, _ := os.ReadFile(filepath.Join(a.projet, ".athanor", ".gitattributes")); !strings.Contains(string(g), "LOG.md merge=union") {
		t.Fatal("no union merge for the log")
	}

	// a real conflict: both change the same line; B's merge is undone and left to Claude
	gitEssai(t, a.projet, "pull", "-q", "--no-rebase", depot, "main")
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "# Game\n\nA's line\n")
	gitEssai(t, a.projet, "commit", "-q", "-am", "A changes the readme")
	a.faire(t, func() {
		p := piloteEssai(t, a)
		if code, _ := p.pousserPC(); code != 0 {
			t.Fatalf("A's push: %d", code)
		}
	})
	ecrireEssai(t, filepath.Join(b.projet, "README.md"), "# Game\n\nB's line\n")
	gitEssai(t, b.projet, "commit", "-q", "-am", "B changes the readme")
	teteB := gitEssai(t, b.projet, "rev-parse", "HEAD")
	b.faire(t, func() {
		p := piloteEssai(t, b)
		if code, _ := p.pousserPC(); code == 0 {
			t.Fatal("B pushed over a conflict")
		}
	})
	if gitEssai(t, b.projet, "rev-parse", "HEAD") != teteB || gitEssai(t, b.projet, "status", "--porcelain", "--untracked-files=no") != "" {
		t.Fatalf("B's project left in the middle of a merge: %v / %s", gitEssai(t, b.projet, "rev-parse", "HEAD") != teteB, gitEssai(t, b.projet, "status", "--porcelain"))
	}
	consigne, err := os.ReadFile(filepath.Join(b.projet, ".athanor", "MERGE_NEEDED.md"))
	if err != nil || !strings.Contains(string(consigne), "README.md") || !strings.Contains(string(consigne), "git merge refs/remotes/athanor/main") {
		t.Fatalf("MERGE_NEEDED.md: %v %s", err, consigne)
	}
	// Claude merges as told: then the push goes through, and the note goes away at the next reception
	gitEssai(t, b.projet, "-c", "user.name=B", "merge", "-q", "refs/remotes/athanor/main", "-X", "ours", "-m", "merge")
	b.faire(t, func() {
		p := piloteEssai(t, b)
		if code, _ := p.pousserPC(); code != 0 {
			t.Fatalf("B's push after the merge: %d", code)
		}
	})

	// B's pilot stops: its milestone is free; B leaves the team: the others no longer see it
	b.faire(t, func() { piloteEssai(t, b).finPC() })
	for _, p := range vuePCsEssai(t, a).PCs {
		if p.Nom == "PC-Fibre" && (p.Pilote || p.Jalon != "" || !p.EnLigne) {
			t.Fatalf("B after its pilot stopped: %+v", p)
		}
	}
	b.faire(t, func() {
		var o bytes.Buffer
		if code := pcsActiverGH([]string{b.projet, "0"}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	for _, p := range vuePCsEssai(t, a).PCs {
		if p.Nom == "PC-Fibre" {
			t.Fatal("B still seen after it left")
		}
	}
	if v := vuePCsEssai(t, b); v.Actif {
		t.Fatal("B still on")
	}

	// E joins from a new folder: at the switch, the project starts from the team's work; what Athanor
	// wrote in its workshop when the project was added is put aside, never lost
	e := pcVideEssai(t, "PC-Neuf", func(p string) { ecrireEssai(t, filepath.Join(p, ".athanor", "LOG.md"), "# my own log\n") })
	ancienNatif := piloteNatifPC
	piloteNatifPC = func() bool { return true } // E as a native Windows pilot: it sets core.autocrlf false
	codeE, outE := activerEssai(t, e)
	piloteNatifPC = ancienNatif
	if codeE != 0 || !strings.Contains(outE, "starts from the other computers' work") {
		t.Fatalf("E's switch: %d %s", codeE, outE)
	}
	gitEssai(t, e.projet, "config", "core.autocrlf", "false") // what its pilot does at its start
	if st := gitEssai(t, e.projet, "status", "--porcelain", "--untracked-files=no"); st != "" {
		t.Fatalf("E's files look changed once its pilot sets its line endings: %s", st)
	}
	if gitEssai(t, e.projet, "rev-parse", "HEAD") != gitEssai(t, depot, "rev-parse", "main") {
		t.Fatal("E does not start from GitHub's branch")
	}
	if b, _ := os.ReadFile(filepath.Join(e.projet, ".athanor", "PLAN.md")); !strings.Contains(string(b), "M4 : Armor colors") {
		t.Fatal("E has no plan")
	}
	var garde string
	e.faire(t, func() { garde = dossierAvantEquipePC(dossierGH(e.projet)) })
	trouve := false
	_ = filepath.Walk(garde, func(f string, i os.FileInfo, err error) error {
		if err == nil && !i.IsDir() {
			b, _ := os.ReadFile(f)
			trouve = trouve || string(b) == "# my own log\n"
		}
		return nil
	})
	if !trouve {
		t.Fatal("E's own log was not kept")
	}
	if v := vuePCsEssai(t, e); !v.Actif || len(v.PCs) == 0 {
		t.Fatalf("E's view: %+v", v)
	}
	if st := gitEssai(t, e.projet, "status", "--porcelain"); strings.Contains(st, "STATE.json") {
		t.Fatalf("E's pilot state not excluded: %s", st)
	}

	// two computers tick milestones on lines next to each other (a conflict for git): merged by itself
	recevoirEssai := func(m pcEssai) string {
		t.Helper()
		res := ""
		m.faire(t, func() {
			eq, err := ouvrirEquipePC(m.projet)
			if err != nil || eq == nil {
				t.Fatalf("%s team: %v", m.nom, err)
			}
			res, _, err = eq.recevoir()
			if err != nil {
				t.Fatal(err)
			}
		})
		return res
	}
	pousserEssai := func(m pcEssai) {
		t.Helper()
		m.faire(t, func() {
			if code, _ := piloteEssai(t, m).pousserPC(); code != 0 {
				t.Fatalf("%s's push: %d", m.nom, code)
			}
		})
	}
	recevoirEssai(a)
	fp := filepath.Join(a.projet, ".athanor", "PLAN.md")
	pl, _ := os.ReadFile(fp)
	ecrireEssai(t, fp, string(pl)+"- [ ] M5 : Sounds\n- [ ] M6 : Music\n")
	gitEssai(t, a.projet, "commit", "-q", "-am", "plan: M5 M6")
	pousserEssai(a)
	recevoirEssai(e)
	cocherEssai(t, a.projet, "M5")
	gitEssai(t, a.projet, "commit", "-q", "-am", "M5 done")
	pousserEssai(a)
	cocherEssai(t, e.projet, "M6")
	gitEssai(t, e.projet, "-c", "user.name=E", "-c", "user.email=e@example.com", "commit", "-q", "-am", "M6 done")
	if res := recevoirEssai(e); res != recuFusion {
		t.Fatalf("ticks next to each other: %s", res)
	}
	if pl, _ := os.ReadFile(filepath.Join(e.projet, ".athanor", "PLAN.md")); !strings.Contains(string(pl), "- [x] M5 ") || !strings.Contains(string(pl), "- [x] M6 ") {
		t.Fatalf("merged plan:\n%s", pl)
	}
	if _, err := os.Stat(filepath.Join(e.projet, ".athanor", "MERGE_NEEDED.md")); err == nil || !propreTravailPC(e.projet) {
		t.Fatal("the ticks' merge left something to do")
	}
	pousserEssai(e)

	// F has files of its own that GitHub has too: nothing overwritten; on, it waits until they are moved
	f := pcVideEssai(t, "PC-Plein", func(p string) { ecrireEssai(t, filepath.Join(p, "README.md"), "mine\n") })
	if code, out := activerEssai(t, f); code != 0 || !strings.Contains(out, "README.md") || !strings.Contains(out, "does not overwrite") {
		t.Fatalf("F's switch: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(f.projet, "README.md")); string(b) != "mine\n" {
		t.Fatal("F's README overwritten")
	}
	if v := vuePCsEssai(t, f); !v.Actif || !strings.Contains(v.Erreur, "README.md") {
		t.Fatalf("F's view does not say why: %+v", v)
	}
	f.faire(t, func() {
		p := piloteEssai(t, f)
		if p.rejoindrePC() {
			t.Fatal("F's pilot would start a history of its own")
		}
		if att, raison := p.avantCyclePC(); !att || !strings.Contains(raison, "README.md") {
			t.Fatalf("F's pilot: %v %s", att, raison)
		}
	})
	_ = os.Remove(filepath.Join(f.projet, "README.md"))
	f.faire(t, func() {
		p := piloteEssai(t, f)
		if !p.rejoindrePC() || gitEssai(t, f.projet, "rev-parse", "HEAD") == "" {
			t.Fatal("F does not join once its file moved")
		}
	})

	// G has a history of its own: refused, and left off
	g := pcVideEssai(t, "PC-Autre", func(p string) {
		gitEssai(t, p, "init", "-q", "-b", "main")
		ecrireEssai(t, filepath.Join(p, "other.txt"), "x\n")
		gitEssai(t, p, "add", "other.txt")
		gitEssai(t, p, "-c", "user.name=G", "-c", "user.email=g@example.com", "commit", "-qm", "another project")
	})
	if code, out := activerEssai(t, g); code == 0 || !strings.Contains(out, "nothing in common") {
		t.Fatalf("G's switch: %d %s", code, out)
	}
	if v := vuePCsEssai(t, g); v.Actif {
		t.Fatal("G left on")
	}
}

func TestFusionnerCochesPC(t *testing.T) {
	nous := "# Plan\n- [x] M1 : a\n- [ ] M2 : b\n  - [ ] sub\n"
	eux := "# Plan\n- [ ] M1 : a\n- [X] M2 : b\n  - [x] sub\n"
	if f, ok := fusionnerCochesPC(nous, eux); !ok || f != "# Plan\n- [x] M1 : a\n- [X] M2 : b\n  - [x] sub\n" {
		t.Fatalf("%v %q", ok, f)
	}
	if _, ok := fusionnerCochesPC(nous, "# Plan\n- [ ] M1 : a, changed\n- [x] M2 : b\n  - [ ] sub\n"); ok {
		t.Fatal("a changed title merged as a tick")
	}
	if _, ok := fusionnerCochesPC(nous, nous+"- [ ] M3 : c\n"); ok {
		t.Fatal("an added milestone merged as a tick")
	}
}

func TestChoisirJalonPC(t *testing.T) {
	moi := MachinePC{ID: "aaaaaaaaaaaa", Nom: "Moi", Mots: []string{"gpu"}}
	// milestones written without an identifier: shared out by their title, never a delivery before them
	sansID := analyserPlan("# Plan\n\n- [x] Build the skeleton\n- [ ] Build the homepage\n- [ ] Write the about page\n")
	j, att, _ := choisirJalonPC(sansID, moi, nil, "")
	if att || j.ID != "«Build the homepage»" {
		t.Fatalf("id-less plan: %q %v", j.ID, att)
	}
	autre := PresencePC{ID: "bbbbbbbbbbbb", Nom: "B", EnLigne: true, Pilote: true, Jalon: j.ID}
	if j2, att, _ := choisirJalonPC(sansID, moi, []PresencePC{autre}, ""); att || j2.ID != "«Write the about page»" {
		t.Fatalf("id-less plan, the first one held: %q %v", j2.ID, att)
	}
	if !strings.Contains(butJalonPC(j, nil, 50, ""), "Milestone «Build the homepage» of .athanor/PLAN.md") {
		t.Fatal("goal of an id-less milestone")
	}
	en := func(nom, id, jalon string, mots ...string) PresencePC {
		return PresencePC{ID: id, Nom: nom, EnLigne: true, Pilote: true, Jalon: jalon, Mots: mots}
	}
	plan := analyserPlan(planEssai)
	// no plan: one computer frames, the others wait
	if j, att, _ := choisirJalonPC(nil, moi, nil, ""); att || j.ID != jalonCadragePC {
		t.Fatalf("framing: %q %v", j.ID, att)
	}
	if _, att, raison := choisirJalonPC(nil, moi, []PresencePC{en("Autre", "bbbbbbbbbbbb", jalonCadragePC)}, ""); !att || !strings.Contains(raison, "Autre") {
		t.Fatal("framing taken: no wait")
	}
	// its words first; a milestone another running computer has the words for is left to it
	if j, _, _ := choisirJalonPC(plan, moi, nil, ""); j.ID != "M1" {
		t.Fatalf("GPU first: %s", j.ID)
	}
	sans := MachinePC{ID: "cccccccccccc", Nom: "Sans"}
	if j, _, _ := choisirJalonPC(plan, sans, []PresencePC{en("GPU", "dddddddddddd", "", "gpu"), en("Fibre", "eeeeeeeeeeee", "", "network")}, ""); j.ID != "M2" {
		t.Fatalf("left to the others: %s", j.ID)
	}
	// nobody running has the words (nobody declared them): the plan's order, like the others
	if j, _, _ := choisirJalonPC(plan, sans, nil, ""); j.ID != "M1" {
		t.Fatalf("no words anywhere: the plan's order: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, sans, []PresencePC{en("X", "ffffffffffff", "M2")}, ""); j.ID != "M1" {
		t.Fatalf("needs nobody has: %s", j.ID)
	}
	// an offline computer, or one whose pilot is stopped, holds nothing
	hors := en("GPU", "dddddddddddd", "M1", "gpu")
	hors.EnLigne = false
	if j, _, _ := choisirJalonPC(plan, moi, []PresencePC{hors}, ""); j.ID != "M1" {
		t.Fatalf("offline holder: %s", j.ID)
	}
	// the milestone held at the previous cycle goes on (a relay)
	if j, _, _ := choisirJalonPC(plan, moi, nil, "M2"); j.ID != "M2" {
		t.Fatalf("relay: %s", j.ID)
	}
	// a dependency not ticked: not yet
	for i := range plan {
		if plan[i].ID != "M4" {
			plan[i].Fait = plan[i].ID != "M1"
		}
	}
	if _, att, _ := choisirJalonPC(plan, moi, []PresencePC{en("GPU", "dddddddddddd", "M1", "gpu")}, ""); !att {
		t.Fatal("M4 taken before M1")
	}
	// everything ticked: the delivery, by one computer
	for i := range plan {
		plan[i].Fait = true
	}
	if j, att, _ := choisirJalonPC(plan, moi, nil, ""); att || j.ID != jalonLivraisonPC {
		t.Fatalf("delivery: %q %v", j.ID, att)
	}
	if _, att, _ := choisirJalonPC(plan, moi, []PresencePC{en("X", "ffffffffffff", jalonLivraisonPC)}, ""); !att {
		t.Fatal("delivery taken twice")
	}
	// Claude is told who builds what
	c := consignePCs(moi, []PresencePC{en("Fibre", "eeeeeeeeeeee", "M3", "network")}, Jalon{ID: "M1"}, false)
	if !strings.Contains(c, "milestone **M1**") || !strings.Contains(c, "Fibre") || !strings.Contains(c, "building M3") || !strings.Contains(c, "Needs: <word>.") || !strings.Contains(c, "gpu, network") {
		t.Fatal(c)
	}
	if g := butJalonPC(Jalon{ID: "M1"}, []PresencePC{en("Fibre", "eeeeeeeeeeee", "M3")}, 80, ""); !strings.Contains(g, "Milestone M1 of") || !strings.Contains(g, "build M3 at the same time") || !strings.Contains(g, "MERGE_NEEDED") {
		t.Fatal(g)
	}
	if j := decouperJalon("M7 : Red armors — models. Needs: GPU, fast network. Files: a.glb. Depends on: M2."); strings.Join(j.Besoins, ",") != "gpu,fast-network" || strings.Join(j.Depend, ",") != "M2" || j.Fichiers != "a.glb" {
		t.Fatalf("needs: %+v", j)
	}
}

// TestRecevoirSansCommandesDuProjet: what the project's .git/config asks git to run (a filter, a merge
// driver: Claude can write it) never runs while the pilot merges the others' work, outside the sandbox.
func TestRecevoirSansCommandesDuProjet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the trap commands are POSIX shell")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien, ancienne := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancien, ancienne }()

	a := nouveauPCEssai(t, "PC-A", "", "", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "# Game\n\nfirst\n")
	ecrireEssai(t, filepath.Join(a.projet, "notes.txt"), "a\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "start")
	a.faire(t, func() {
		var o bytes.Buffer
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	b := nouveauPCEssai(t, "PC-B", "", "", depot, true)
	piege := filepath.Join(t.TempDir(), "ran")
	gitEssai(t, b.projet, "config", "filter.piege.smudge", "touch '"+piege+"-smudge'; cat")
	gitEssai(t, b.projet, "config", "filter.piege.clean", "touch '"+piege+"-clean'; cat")
	gitEssai(t, b.projet, "config", "merge.piege.driver", "touch '"+piege+"-driver'; true")
	ecrireEssai(t, filepath.Join(b.projet, ".git", "info", "attributes"), "* filter=piege merge=piege\n")

	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "# Game\n\nA's line\n")
	ecrireEssai(t, filepath.Join(a.projet, "notes.txt"), "a\nb\n")
	gitEssai(t, a.projet, "commit", "-q", "-am", "A")
	a.faire(t, func() {
		if code, _ := piloteEssai(t, a).pousserPC(); code != 0 {
			t.Fatalf("A's push: %d", code)
		}
	})
	ecrireEssai(t, filepath.Join(b.projet, "README.md"), "# Game\n\nB's line\n")
	gitEssai(t, b.projet, "-c", "filter.piege.clean=cat", "commit", "-q", "-am", "B")
	var res string
	b.faire(t, func() {
		e, _ := ouvrirEquipePC(b.projet)
		res, _, _ = e.recevoir()
	})
	if res != recuConflit {
		t.Fatalf("a merge driver decided the conflict: %s", res)
	}
	for _, s := range []string{"-smudge", "-clean", "-driver"} {
		if _, err := os.Stat(piege + s); err == nil {
			t.Fatalf("the project's %s command ran", s[1:])
		}
	}
}

// TestRelaisOutilPC: a program that does not work on a computer: the others try in its place; when none
// can, the computer that was meant to build the milestone takes it back and Claude asks the user.
func TestRelaisOutilPC(t *testing.T) {
	plan := analyserPlan("# Plan\n\n- [ ] M1 : Props — Needs: blender.\n- [ ] M2 : Menus\n")
	maint := time.Now().UTC()
	quand := func(min int) string { return maint.Add(time.Duration(-min) * time.Minute).Format(time.RFC3339) }
	a := MachinePC{ID: "aaaaaaaaaaaa", Nom: "PC-A", Mots: []string{"blender"}}
	refusA := []RefusPC{{Outil: "blender", Jalon: "M1", Pourquoi: "crashes at start", Quand: quand(10)}}
	b := PresencePC{ID: "bbbbbbbbbbbb", Nom: "PC-B", EnLigne: true, Pilote: true, Mots: []string{"blender"}}
	c := PresencePC{ID: "cccccccccccc", Nom: "PC-C", EnLigne: true, Pilote: true}

	// A failed on M1: it takes M2, B (which has the word) tries M1 first, C too if B does not
	if j, att, _ := choisirJalonRefusPC(plan, a, []PresencePC{b, c}, "M1", refusA); att || j.ID != "M2" {
		t.Fatalf("A after its failure: %q %v", j.ID, att)
	}
	aP := PresencePC{ID: a.ID, Nom: a.Nom, EnLigne: true, Pilote: true, Mots: a.Mots, Refus: refusA}
	if j, _, _ := choisirJalonRefusPC(plan, MachinePC{ID: c.ID, Nom: c.Nom}, []PresencePC{aP}, "", nil); j.ID != "M1" {
		t.Fatalf("C tries in A's place first: %q", j.ID)
	}
	// B failed too, C is not running: A, meant to build it, takes it back and tells Claude who tried
	bR := b
	bR.Refus = []RefusPC{{Outil: "blender", Jalon: "M1", Pourquoi: "no GPU driver", Quand: quand(2)}}
	plan2 := analyserPlan("# Plan\n\n- [ ] M1 : Props — Needs: blender.\n- [x] M2 : Menus\n")
	j, att, _ := choisirJalonRefusPC(plan2, a, []PresencePC{bR}, "", refusA)
	if att || j.ID != "M1" || !strings.Contains(j.Reprise, "PC-A (the computer meant to build it): crashes at start; PC-B: no GPU driver") || !strings.Contains(j.Reprise, "athanor todo add") {
		t.Fatalf("A takes it back: %q %v %q", j.ID, att, j.Reprise)
	}
	if !strings.Contains(butJalonPC(j, nil, 50, ""), "No computer of the team could run blender") {
		t.Fatal("the goal does not say it")
	}
	// B leaves it to A while A runs; with A gone, B takes it back
	if j, att, _ := choisirJalonRefusPC(plan2, MachinePC{ID: b.ID, Nom: b.Nom}, []PresencePC{aP}, "", bR.Refus); !att {
		t.Fatalf("B took A's milestone back: %q", j.ID)
	}
	if j, _, _ := choisirJalonRefusPC(plan2, MachinePC{ID: b.ID, Nom: b.Nom}, nil, "", bR.Refus); j.ID != "M1" || j.Reprise == "" {
		t.Fatalf("B alone: %q %q", j.ID, j.Reprise)
	}
	// alone from the start: straight back, with the user asked
	if j, _, _ := choisirJalonRefusPC(plan2, a, nil, "", refusA); j.ID != "M1" || j.Reprise == "" {
		t.Fatalf("A alone: %q", j.Reprise)
	}
	// a refusal 12 hours old is forgotten: the computer tries again (it may have been repaired)
	vieux := []RefusPC{{Outil: "blender", Jalon: "M1", Quand: maint.Add(-13 * time.Hour).Format(time.RFC3339)}}
	if j, _, _ := choisirJalonRefusPC(plan, a, []PresencePC{b}, "", vieux); j.ID != "M1" || j.Reprise != "" {
		t.Fatalf("old refusal: %q %q", j.ID, j.Reprise)
	}

	// what Claude writes, read once
	dir := t.TempDir()
	ecrireEssai(t, filepath.Join(dir, fichierRefusClaude), "# Cannot here\n\n- tool: Blender 4.2\n- why: it crashes at start (GPU driver)\n")
	if o, w, ok := lireRefusClaude(dir); !ok || o != "blender" || w != "it crashes at start (GPU driver)" {
		t.Fatalf("CANNOT_HERE: %q %q %v", o, w, ok)
	}
	if _, _, ok := lireRefusClaude(dir); ok {
		t.Fatal("CANNOT_HERE read twice")
	}
}

// TestNiveauxPC: a word with a level (gpu:3): the heavy milestone goes to the most capable computer, the
// lighter ones to the others at the same time; nobody at that level: the most capable one takes it.
func TestBesoinsMultiplesPC(t *testing.T) {
	plan := analyserPlan("# Plan\n\n- [ ] M1 : Cinematic — Needs: gpu:3, unreal.\n- [ ] M2 : Menus\n")
	gpu := MachinePC{ID: "aaaaaaaaaaaa", Nom: "GPU", Mots: []string{"gpu:3"}}
	tout := PresencePC{ID: "bbbbbbbbbbbb", Nom: "All", Mots: []string{"gpu:3", "unreal"}, EnLigne: true, Pilote: true}
	if j, _, _ := choisirJalonPC(plan, gpu, []PresencePC{tout}, ""); j.ID != "M2" {
		t.Fatalf("the computer with gpu alone took the milestone that needs gpu and unreal: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, gpu, nil, ""); j.ID != "M1" {
		t.Fatalf("alone, the closest computer takes it: %s", j.ID)
	}
}

// TestSansMotsPC: nobody declared any word: the milestones that name one are shared out in the plan's
// order like the others, never left for last; they wait only for a computer of the team that has the word.
func TestSansMotsPC(t *testing.T) {
	plan := analyserPlan("# Plan\n\n- [ ] M1 : Render — Needs: gpu.\n- [ ] M2 : Menus\n")
	a := MachinePC{ID: "aaaaaaaaaaaa", Nom: "A"}
	b := PresencePC{ID: "bbbbbbbbbbbb", Nom: "B", EnLigne: true, Pilote: true}
	if j, _, _ := choisirJalonPC(plan, a, []PresencePC{b}, ""); j.ID != "M1" {
		t.Fatalf("no words anywhere: M1 first, in the plan's order: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, a, nil, ""); j.ID != "M1" {
		t.Fatalf("alone, no words: %s", j.ID)
	}
	absent := PresencePC{ID: "cccccccccccc", Nom: "C", Mots: []string{"gpu"}}
	if j, _, _ := choisirJalonPC(plan, a, []PresencePC{b, absent}, ""); j.ID != "M2" {
		t.Fatalf("the team's gpu computer is away: M2 first: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(analyserPlan("# Plan\n\n- [ ] M1 : Render — Needs: gpu.\n"), a, []PresencePC{absent}, ""); j.ID != "M1" {
		t.Fatalf("nothing else to do: it takes M1 all the same: %s", j.ID)
	}
}

func TestNiveauxPC(t *testing.T) {
	if b, n := niveauMotPC("gpu:3"); b != "gpu" || n != 3 {
		t.Fatalf("gpu:3 → %s %d", b, n)
	}
	if b, n := niveauMotPC("gpu"); b != "gpu" || n != 1 {
		t.Fatalf("gpu → %s %d", b, n)
	}
	if b, n := niveauMotPC("c++:x"); b != "c++:x" || n != 1 {
		t.Fatalf("odd word → %s %d", b, n)
	}
	plan := analyserPlan("# Plan\n\n- [ ] M1 : Cinematic render — Needs: gpu:3.\n- [ ] M2 : Props — Needs: gpu.\n- [ ] M3 : Menus\n")
	gros := MachinePC{ID: "aaaaaaaaaaaa", Nom: "Big", Mots: []string{"gpu:3"}}
	petit := MachinePC{ID: "bbbbbbbbbbbb", Nom: "Small", Mots: []string{"gpu:1"}}
	enMarche := func(m MachinePC, jalon string) PresencePC {
		return PresencePC{ID: m.ID, Nom: m.Nom, Mots: m.Mots, EnLigne: true, Pilote: true, Jalon: jalon}
	}
	if j, _, _ := choisirJalonPC(plan, gros, []PresencePC{enMarche(petit, "")}, ""); j.ID != "M1" {
		t.Fatalf("the big computer takes the heavy one: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, petit, []PresencePC{enMarche(gros, "M1")}, ""); j.ID != "M2" {
		t.Fatalf("the small one takes the light gpu one: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, petit, []PresencePC{enMarche(gros, "")}, ""); j.ID != "M2" {
		t.Fatalf("the small one leaves the heavy one to the big one: %s", j.ID)
	}
	// nobody has level 3: the most capable computer takes it, the other leaves it
	moyen := MachinePC{ID: "cccccccccccc", Nom: "Mid", Mots: []string{"gpu:2"}}
	if j, _, _ := choisirJalonPC(plan, moyen, []PresencePC{enMarche(petit, "")}, ""); j.ID != "M1" && j.ID != "M2" {
		t.Fatalf("mid without big: %s", j.ID)
	}
	if j, _, _ := choisirJalonPC(plan, petit, []PresencePC{enMarche(moyen, "")}, ""); j.ID == "M1" {
		t.Fatal("the small one took the heavy one while a more capable one runs")
	}
	if j, _, _ := choisirJalonPC(plan, petit, nil, ""); j.ID != "M1" && j.ID != "M2" {
		t.Fatalf("alone: %s", j.ID)
	}
}

// TestEnCoursPC: a computer that goes away in the middle of a milestone: its unfinished work (a commit not
// pushed, a file not committed) reaches the computer that takes the milestone next; when it comes back,
// its own copy of that work is put aside, never mixed with the team's.
func TestEnCoursPC(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien, ancienne := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancien, ancienne }()

	a := nouveauPCEssai(t, "PC-A", "", "", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [ ] M1 : Level one\n- [ ] M2 : Level two\n")
	ecrireEssai(t, filepath.Join(a.projet, "game.txt"), "game\n")
	ecrireEssai(t, filepath.Join(a.projet, ".gitignore"), "*.local\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "start")
	a.faire(t, func() {
		var o bytes.Buffer
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})
	b := nouveauPCEssai(t, "PC-B", "", "", depot, true)
	// what the branches of work in progress hold: "<computer>/<milestone>" (one branch per computer)
	enCours := func() string {
		var l []string
		for _, r := range strings.Fields(gitEssai(t, depot, "for-each-ref", "--format=%(refname)", "refs/athanor/encours/")) {
			for _, x := range strings.Fields(gitEssai(t, depot, "ls-tree", "--name-only", r)) {
				l = append(l, strings.TrimPrefix(r, "refs/athanor/encours/")+"/"+x)
			}
		}
		return strings.Join(l, " ")
	}

	// A takes M1, works: a commit it did not push, a file it did not commit; then its pilot stops
	a.faire(t, func() {
		e, _ := ouvrirEquipePC(a.projet)
		_ = e.lire()
		if pris, _ := e.prendre(Jalon{ID: "M1", Titre: "M1 : Level one"}); !pris {
			t.Fatal("A does not take M1")
		}
	})
	ecrireEssai(t, filepath.Join(a.projet, "level1.txt"), "half of level one\n")
	gitEssai(t, a.projet, "add", "level1.txt")
	gitEssai(t, a.projet, "commit", "-q", "-m", "M1: half")
	ecrireEssai(t, filepath.Join(a.projet, "level1-boss.txt"), "boss sketch\n")
	ecrireEssai(t, filepath.Join(a.projet, "keys.local"), "A's own keys\n")
	a.faire(t, func() { piloteEssai(t, a).finPC() })
	if !strings.Contains(enCours(), "/M1") {
		t.Fatalf("A's work in progress is not on GitHub: %q", enCours())
	}
	if n := strings.Fields(gitEssai(t, depot, "for-each-ref", "--format=%(refname)", "refs/athanor/encours/")); len(n) != 1 {
		t.Fatalf("not one branch for A: %v", n)
	}
	if strings.Contains(gitEssai(t, depot, "ls-tree", "-r", "--name-only", "main"), "level1") {
		t.Fatal("unfinished work pushed to main")
	}

	// B pushes a file that A's git ignores (added with -f): A's own, in no commit, must not be lost
	ecrireEssai(t, filepath.Join(b.projet, "keys.local"), "the team's keys\n")
	gitEssai(t, b.projet, "add", "-f", "keys.local")
	gitEssai(t, b.projet, "commit", "-q", "-m", "keys")
	b.faire(t, func() {
		var o bytes.Buffer
		if code := pousserGH([]string{b.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
	})

	// B takes M1 and goes on from A's work
	b.faire(t, func() {
		p := piloteEssai(t, b)
		ecrireEssai(t, filepath.Join(b.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [ ] M1 : Level one\n- [ ] M2 : Level two\n")
		if att, raison := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M1" {
			t.Fatalf("B: %v %s %+v", att, raison, p.jalonPC)
		}
		if !strings.Contains(p.jalonPC.Suite, "PC-A started this milestone") || !strings.Contains(butJalonPC(*p.jalonPC, nil, 50, ""), "unfinished work is merged") {
			t.Fatalf("B's goal: %q", p.jalonPC.Suite)
		}
	})
	for f, c := range map[string]string{"level1.txt": "half of level one\n", "level1-boss.txt": "boss sketch\n"} {
		if got, _ := os.ReadFile(filepath.Join(b.projet, f)); string(got) != c {
			t.Fatalf("B lacks A's %s: %q", f, got)
		}
	}
	idDe := func(m pcEssai) (id string) {
		m.faire(t, func() {
			e, err := ouvrirEquipePC(m.projet)
			if err != nil {
				t.Fatal(err)
			}
			id = e.moi.ID
		})
		return id
	}
	idA, idB := idDe(a), idDe(b)
	if !strings.Contains(enCours(), idB+"/M1") {
		t.Fatalf("B's work in progress (holding A's) not sent once it goes on from it: %q", enCours())
	}
	if br := gitEssai(t, depot, "for-each-ref", "--format=%(refname)", "refs/heads/athanor/"); br != "" {
		t.Fatalf("branches on GitHub (shown in its list, delete workflows): %s", br)
	}

	// A comes back: B builds M1 now; A's own copy is put aside, its folder back to the team's last push
	b.faire(t, func() { _ = vuePCsEssai(t, b, "--pilote", "1", "--publier") })
	a.faire(t, func() {
		// (its pilot stopped cleanly: it let M1 go, and remembers it)
		if j, _, _ := lireLaissePC(dossierGH(a.projet)); j != "M1" {
			t.Fatalf("A does not remember the milestone it let go: %q", j)
		}
		p := piloteEssai(t, a)
		if att, _ := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M2" {
			t.Fatalf("A after coming back: %v %+v", att, p.jalonPC)
		}
	})
	if strings.Contains(enCours(), idA+"/M1") {
		t.Fatalf("A's out-of-date work in progress still on GitHub: %q", enCours())
	}
	if r := gitEssai(t, depot, "for-each-ref", "refs/athanor/encours/"+idA); r != "" {
		t.Fatalf("A's ref of work in progress, empty, still on GitHub: %s", r)
	}
	for _, f := range []string{"level1.txt", "level1-boss.txt"} {
		if _, err := os.Stat(filepath.Join(a.projet, f)); err == nil {
			t.Fatalf("A kept its own copy of M1 in its folder: %s", f)
		}
	}
	if sauve := gitEssai(t, a.projet, "for-each-ref", "--format=%(objectname)", "refs/athanor/sauve/"); !strings.Contains(gitEssai(t, a.projet, "show", sauve+":level1-boss.txt"), "boss sketch") {
		t.Fatal("A's file never committed is not in the kept commit")
	}
	if !strings.Contains(gitEssai(t, a.projet, "for-each-ref", "--format=%(refname)", "refs/athanor/sauve/"), "M1") {
		t.Fatal("A's own unfinished work not kept")
	}
	var copies []string
	a.faire(t, func() {
		copies, _ = filepath.Glob(filepath.Join(dossierAvantEquipePC(dossierGH(a.projet)), "ignores-*", "keys.local"))
	})
	if len(copies) != 1 {
		t.Fatal("A's ignored file replaced by the team's is not copied aside")
	}
	if c, _ := os.ReadFile(copies[0]); string(c) != "A's own keys\n" {
		t.Fatalf("copy of A's ignored file: %q", c)
	}
}

// TestLaissePuisRepriseSeulPC: a computer stops in the middle of a milestone and nobody takes it: at its
// next start it goes on with it, its unfinished work untouched.
func TestLaissePuisRepriseSeulPC(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien, ancienne := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancien, ancienne }()
	a := nouveauPCEssai(t, "PC-A", "", "", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), "# Plan\n\n- [ ] M1 : Level one\n- [ ] M2 : Level two\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "start")
	a.faire(t, func() {
		var o bytes.Buffer
		if code := pousserGH([]string{a.projet}, &o); code != 0 {
			t.Fatal(o.String())
		}
		p := piloteEssai(t, a)
		if att, _ := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M1" {
			t.Fatalf("first take: %+v", p.jalonPC)
		}
	})
	ecrireEssai(t, filepath.Join(a.projet, "level1.txt"), "half\n")
	a.faire(t, func() { piloteEssai(t, a).finPC() })
	a.faire(t, func() {
		p := piloteEssai(t, a)
		if att, _ := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M1" {
			t.Fatalf("after its restart: %+v", p.jalonPC)
		}
	})
	if b, _ := os.ReadFile(filepath.Join(a.projet, "level1.txt")); string(b) != "half\n" {
		t.Fatal("its unfinished work went")
	}
	if r := gitEssai(t, a.projet, "for-each-ref", "refs/athanor/sauve/"); r != "" {
		t.Fatalf("put aside while nobody took it: %s", r)
	}
}

// TestRefsPCsGitHub: on GitHub, the presences and the works in progress are refs, never branches: the
// branches 2.36 made go once, a computer not seen for 30 days leaves GitHub, and one that leaves the team
// removes its presence at once.
func TestRefsPCsGitHub(t *testing.T) {
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

	a := pcVideEssai(t, "PC-A", nil)
	var idA string
	a.faire(t, func() { idA = lireMachinePC().ID })
	ecrireEssai(t, filepath.Join(a.projet, "README.md"), "a\n")
	gitEssai(t, a.projet, "init", "-q", "-b", "main")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "-c", "user.name=A", "-c", "user.email=a@example.com", "commit", "-q", "-m", "start")
	// what 2.36 left: A's branches; and a computer gone for 40 days
	gitEssai(t, a.projet, "push", "-q", depot, "HEAD:main", "HEAD:refs/heads/athanor/pc/"+idA, "HEAD:refs/heads/athanor/encours/"+idA,
		"HEAD:refs/heads/athanor/pc/ffffffffffff")
	fantome := filepath.Join(t.TempDir(), "fantome")
	gitEssai(t, filepath.Dir(fantome), "init", "-q", fantome)
	vieux, _ := json.Marshal(PresencePC{ID: "eeeeeeeeeeee", Nom: "Ghost", Maj: time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	ecrireEssai(t, filepath.Join(fantome, "pc.json"), string(vieux))
	gitEssai(t, fantome, "add", "-A")
	gitEssai(t, fantome, "-c", "user.name=G", "-c", "user.email=g@example.com", "commit", "-q", "-m", "ghost")
	gitEssai(t, fantome, "push", "-q", depot, "HEAD:refs/athanor/pc/eeeeeeeeeeee", "HEAD:refs/athanor/encours/eeeeeeeeeeee")

	if code, out := activerEssai(t, a); code != 0 {
		t.Fatal(out)
	}
	_ = vuePCsEssai(t, a, "--publier")
	refs := gitEssai(t, depot, "for-each-ref", "--format=%(refname)")
	if !strings.Contains(refs, "refs/athanor/pc/"+idA) {
		t.Fatalf("A's presence is not a ref of its own: %s", refs)
	}
	if strings.Contains(refs, "refs/heads/athanor/pc/"+idA) || strings.Contains(refs, "refs/heads/athanor/encours/"+idA) {
		t.Fatalf("A's branches of 2.36 still on GitHub: %s", refs)
	}
	if !strings.Contains(refs, "refs/heads/athanor/pc/ffffffffffff") {
		t.Fatal("another computer's branch removed: only each computer removes its own")
	}
	if strings.Contains(refs, "eeeeeeeeeeee") {
		t.Fatalf("the computer gone for 40 days still on GitHub: %s", refs)
	}
	// once only: a branch made again later stays (it is not A's doing to remove it twice)
	gitEssai(t, a.projet, "push", "-q", depot, "HEAD:refs/heads/athanor/pc/"+idA)
	_ = vuePCsEssai(t, a, "--publier")
	if !strings.Contains(gitEssai(t, depot, "for-each-ref", "--format=%(refname)"), "refs/heads/athanor/pc/"+idA) {
		t.Fatal("the old branches looked for at each presence")
	}

	// A leaves the team: its presence leaves GitHub at once
	a.faire(t, func() {
		var out bytes.Buffer
		if code := pcsActiverGH([]string{a.projet, "0"}, &out); code != 0 {
			t.Fatal(out.String())
		}
	})
	if strings.Contains(gitEssai(t, depot, "for-each-ref", "--format=%(refname)", "refs/athanor/pc/"), idA) {
		t.Fatal("A's presence stays after it left the team")
	}
}

// TestMenageIgnoresPC: the copies of ignored files older than 30 days go, the five most recent stay.
func TestMenageIgnoresPC(t *testing.T) {
	d := t.TempDir()
	vieux := time.Now().Add(-40 * 24 * time.Hour)
	for i := 1; i <= 8; i++ {
		f := filepath.Join(d, fmt.Sprintf("ignores-2026010%d-120000", i))
		ecrireEssai(t, filepath.Join(f, "keys.local"), "x\n")
		if i <= 7 {
			_ = os.Chtimes(f, vieux, vieux)
		}
	}
	ecrireEssai(t, filepath.Join(d, "autre", "x"), "x\n")
	_ = os.Chtimes(filepath.Join(d, "autre"), vieux, vieux)
	menageIgnoresPC(d)
	l, _ := filepath.Glob(filepath.Join(d, "ignores-*"))
	if len(l) != 5 || filepath.Base(l[0]) != "ignores-20260104-120000" {
		t.Fatalf("left: %v", l)
	}
	if _, err := os.Stat(filepath.Join(d, "autre")); err != nil {
		t.Fatal("a folder that is not a copy of ignored files removed")
	}
}

// TestChaineEnCoursPC: B goes on from A's unfinished work but cannot merge it, then goes away in its turn:
// C, which takes the milestone next, gets B's work and A's (carried on in B's), never B's alone.
func TestChaineEnCoursPC(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	conf := filepath.Join(t.TempDir(), "gitconfig")
	ecrireEssai(t, conf, "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", conf)
	depot := filepath.Join(t.TempDir(), "github.git")
	gitEssai(t, filepath.Dir(depot), "init", "-q", "--bare", "-b", "main", depot)
	ancien, ancienne := depotEssaiGH, attentePrisePC
	depotEssaiGH, attentePrisePC = depot, 0
	defer func() { depotEssaiGH, attentePrisePC = ancien, ancienne }()
	plan := "# Plan\n\n- [ ] M1 : Level one\n- [ ] M2 : Level two\n"
	pousser := func(m pcEssai) {
		m.faire(t, func() {
			var o bytes.Buffer
			if code := pousserGH([]string{m.projet}, &o); code != 0 {
				t.Fatal(o.String())
			}
		})
	}

	a := nouveauPCEssai(t, "PC-A", "", "", depot, false)
	ecrireEssai(t, filepath.Join(a.projet, ".athanor", "PLAN.md"), plan)
	ecrireEssai(t, filepath.Join(a.projet, "level1.txt"), "v0\n")
	gitEssai(t, a.projet, "add", "-A")
	gitEssai(t, a.projet, "commit", "-q", "-m", "start")
	pousser(a)
	b := nouveauPCEssai(t, "PC-B", "", "", depot, true)
	c := nouveauPCEssai(t, "PC-C", "", "", depot, true)

	// A starts M1 and goes away
	a.faire(t, func() {
		e, _ := ouvrirEquipePC(a.projet)
		_ = e.lire()
		if pris, _ := e.prendre(Jalon{ID: "M1", Titre: "M1 : Level one"}); !pris {
			t.Fatal("A does not take M1")
		}
	})
	ecrireEssai(t, filepath.Join(a.projet, "level1.txt"), "A's level\n")
	ecrireEssai(t, filepath.Join(a.projet, "a.txt"), "A's part\n")
	a.faire(t, func() { piloteEssai(t, a).finPC() })

	// B changed the same lines meanwhile: A's work does not merge by itself in B's folder
	ecrireEssai(t, filepath.Join(b.projet, "level1.txt"), "B's level\n")
	gitEssai(t, b.projet, "commit", "-q", "-am", "B's level")
	pousser(b)
	b.faire(t, func() {
		p := piloteEssai(t, b)
		if att, raison := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M1" {
			t.Fatalf("B: %v %s %+v", att, raison, p.jalonPC)
		}
		if !strings.Contains(p.jalonPC.Suite, "does not merge by itself") {
			t.Fatalf("B's goal: %q", p.jalonPC.Suite)
		}
	})
	// B works a little, then goes away too
	ecrireEssai(t, filepath.Join(b.projet, "b.txt"), "B's part\n")
	b.faire(t, func() { piloteEssai(t, b).finPC() })
	var idB string
	b.faire(t, func() { idB = lireMachinePC().ID })
	if l := gitEssai(t, depot, "ls-tree", "--name-only", "refs/athanor/encours/"+idB+":M1"); !strings.Contains(l, "avant") {
		t.Fatalf("B's work in progress does not carry A's: %s", l)
	}

	// C takes M1: B's work and A's, both
	c.faire(t, func() {
		p := piloteEssai(t, c)
		if att, raison := p.avantCyclePC(); att || p.jalonPC == nil || p.jalonPC.ID != "M1" {
			t.Fatalf("C: %v %s %+v", att, raison, p.jalonPC)
		}
		if !strings.Contains(p.jalonPC.Suite, "PC-B") || !strings.Contains(p.jalonPC.Suite, "PC-A") {
			t.Fatalf("C is not told of both works: %q", p.jalonPC.Suite)
		}
	})
	if got, _ := os.ReadFile(filepath.Join(c.projet, "b.txt")); string(got) != "B's part\n" {
		t.Fatalf("C lacks B's work: %q", got)
	}
	var idA string
	a.faire(t, func() { idA = lireMachinePC().ID })
	if aIci, _ := os.ReadFile(filepath.Join(c.projet, "a.txt")); string(aIci) != "A's part\n" &&
		!strings.Contains(gitEssai(t, c.projet, "show", "refs/athanor/repris/M1-"+idA+":a.txt"), "A's part") {
		t.Fatal("C has A's work neither merged nor kept")
	}
}
