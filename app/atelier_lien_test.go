package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// one computer of a test: its project's workshop, its base for the store
type pcAtelier struct {
	etat, base, garde string
}

func nouveauPCAtelier(t *testing.T) pcAtelier {
	d := t.TempDir()
	p := pcAtelier{etat: filepath.Join(d, "projet", ".athanor"), base: filepath.Join(d, "prive", "atelier-base", "maitre"), garde: filepath.Join(d, "prive", "avant")}
	if err := os.MkdirAll(p.etat, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p pcAtelier) ecrire(t *testing.T, rel, texte string) {
	t.Helper()
	ecrireEssai(t, filepath.Join(p.etat, filepath.FromSlash(rel)), texte)
}

func (p pcAtelier) lire(rel string) string {
	b, _ := os.ReadFile(filepath.Join(p.etat, filepath.FromSlash(rel)))
	return string(b)
}

func (p pcAtelier) sync(t *testing.T, m magasinAtelier) ResultatAtelier {
	t.Helper()
	r, err := synchroniserAtelier(p.etat, p.base, m, fusionAtelierGit, "essai", p.garde)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The store: versions taken one at a time (a writer at an old version is told so), contents checked
// against their names, paths kept inside the shared workshop, frozen during a move.
func TestMagasinAtelier(t *testing.T) {
	m := magasinLocal{t.TempDir()}
	if v, err := m.dernier(); err != nil || v.Version != 0 || len(v.Fichiers) != 0 {
		t.Fatalf("empty: %+v %v", v, err)
	}
	a := []byte("# Plan\n")
	if _, err := m.ecrire(0, map[string]string{"PLAN.md": shaAtelier(a)}, map[string][]byte{shaAtelier(a): a}, "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ecrire(0, map[string]string{}, nil, "B"); !errors.Is(err, errConflitAtelier) {
		t.Fatalf("a write at an old version: %v", err)
	}
	if _, err := m.ecrire(1, map[string]string{"PLAN.md": shaAtelier([]byte("x"))}, map[string][]byte{shaAtelier([]byte("x")): []byte("y")}, "B"); err == nil {
		t.Fatal("a content that does not match its name")
	}
	for _, mauvais := range []string{"../x", ".env", "logs/a.log", "docs/../../x", "/etc/passwd", "merge/PLAN.md", `docs\a`, "attachments/private/a"} {
		if cheminAtelierValide(mauvais) {
			t.Errorf("%q accepted", mauvais)
		}
		if _, err := m.ecrire(1, map[string]string{mauvais: shaAtelier(a)}, nil, "B"); err == nil {
			t.Errorf("%q written", mauvais)
		}
	}
	for _, bon := range []string{"PLAN.md", "docs/ARCHITECTURE.md", "queue/002.md", "attachments/request/a.png", "agents/x.md"} {
		if !cheminAtelierValide(bon) {
			t.Errorf("%q refused", bon)
		}
	}
	// two writers at the same version: one wins, the other is told
	var wg sync.WaitGroup
	gagnes := make([]bool, 8)
	for i := range gagnes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := []byte{byte('a' + i)}
			_, err := m.ecrire(1, map[string]string{"LOG.md": shaAtelier(c)}, map[string][]byte{shaAtelier(c): c}, "X")
			gagnes[i] = err == nil
		}(i)
	}
	wg.Wait()
	n := 0
	for _, g := range gagnes {
		if g {
			n++
		}
	}
	if v, _ := m.dernier(); n != 1 || v.Version != 2 {
		t.Fatalf("concurrent writers: %d won, version %d", n, v.Version)
	}
	// a writer gone after its claim: the next one goes on past it
	ecrireEssai(t, filepath.Join(m.dossier, "versions", "000000000003.claim"), "")
	ancien := filepath.Join(m.dossier, "versions", "000000000003.claim")
	_ = os.Chtimes(ancien, tempsAncien(), tempsAncien())
	if v, err := m.ecrire(2, map[string]string{}, nil, "Y"); err != nil || v != 4 {
		t.Fatalf("past a dead claim: %d %v", v, err)
	}
	// frozen (the team moves to another master): nothing written
	ecrireEssai(t, filepath.Join(m.dossier, "gele"), "")
	if _, err := m.ecrire(4, map[string]string{}, nil, "Z"); !errors.Is(err, errGeleAtelier) {
		t.Fatalf("frozen: %v", err)
	}
	_ = os.Remove(filepath.Join(m.dossier, "gele"))
	// the oldest versions go, the contents still named stay
	for v := int64(4); v < 45; v++ {
		c := []byte(strings.Repeat("l", int(v)))
		if _, err := m.ecrire(v, map[string]string{"LOG.md": shaAtelier(c), "PLAN.md": shaAtelier(a)}, map[string][]byte{shaAtelier(c): c, shaAtelier(a): a}, "W"); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := os.ReadDir(filepath.Join(m.dossier, "versions"))
	if len(l) > (versionsGardeesAtelier+10)*2 { // (each with its claim)
		t.Fatalf("%d versions kept", len(l))
	}
	if o, err := m.objets([]string{shaAtelier(a)}); err != nil || string(o[shaAtelier(a)]) != "# Plan\n" {
		t.Fatal("a content still named was removed")
	}
}

// Two computers and the store: what each changes reaches the other; the notes every computer adds to are
// joined; the plan's ticks are joined; other text merged; the same lines changed on both sides: the
// team's kept, this computer's put aside with a note for Claude; a removal never wins over a change.
func TestSynchroniserAtelier(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	m := magasinLocal{t.TempDir()}
	a, b := nouveauPCAtelier(t), nouveauPCAtelier(t)
	plan := "# Plan\n\n- [ ] M1 : Models\n- [ ] M2 : Menus\n- [ ] M3 : Sounds\n"
	a.ecrire(t, "REQUEST.md", "# Request\n\nBuild the game.\n")
	a.ecrire(t, "PLAN.md", plan)
	a.ecrire(t, "LOG.md", "- framing by A\n")
	a.ecrire(t, "docs/ARCHITECTURE.md", "# Architecture\n\nline 1\nline 2\nline 3\nline 4\nline 5\n")
	a.ecrire(t, "STATE.json", "{}")           // not shared
	a.ecrire(t, "logs/cycle-1.jsonl", "{}")   // not shared
	a.ecrire(t, "attachments/private/x", "x") // not shared
	if r := a.sync(t, m); len(r.Envoyes) != 4 || len(r.Recus) != 0 {
		t.Fatalf("A's first: %+v", r)
	}
	// B joins with an old request of its own: the team's wins, B's kept aside
	b.ecrire(t, "REQUEST.md", "# Old request\n")
	if r := b.sync(t, m); len(r.Recus) != 4 {
		t.Fatalf("B's first: %+v", r)
	}
	if b.lire("PLAN.md") != plan || b.lire("REQUEST.md") != a.lire("REQUEST.md") || b.lire("STATE.json") != "" {
		t.Fatal("B did not get the team's workshop as it is")
	}
	if g, _ := os.ReadFile(filepath.Join(b.garde, "REQUEST.md")); string(g) != "# Old request\n" {
		t.Fatalf("B's old request not kept: %q", g)
	}
	// both work: A ticks M1 and logs, B ticks M2 and logs, both change the architecture on other lines
	a.ecrire(t, "PLAN.md", strings.Replace(plan, "[ ] M1", "[x] M1", 1))
	a.ecrire(t, "LOG.md", "- framing by A\n- M1 by A\n")
	a.ecrire(t, "docs/ARCHITECTURE.md", "# Architecture\n\nline 1 (A)\nline 2\nline 3\nline 4\nline 5\n")
	b.ecrire(t, "PLAN.md", strings.Replace(plan, "[ ] M2", "[x] M2", 1))
	b.ecrire(t, "LOG.md", "- framing by A\n- M2 by B\n")
	b.ecrire(t, "docs/ARCHITECTURE.md", "# Architecture\n\nline 1\nline 2\nline 3\nline 4\nline 5 (B)\n")
	b.ecrire(t, "TEAM_NOTES.md", "- B: the menu API is Menu.open()\n")
	a.sync(t, m)
	if r := b.sync(t, m); len(r.Conflits) != 0 {
		t.Fatalf("B's merge: %+v", r)
	}
	a.sync(t, m)
	for _, pc := range []pcAtelier{a, b} {
		p := pc.lire("PLAN.md")
		if !strings.Contains(p, "[x] M1") || !strings.Contains(p, "[x] M2") || !strings.Contains(p, "[ ] M3") {
			t.Fatalf("plan: %s", p)
		}
		l := pc.lire("LOG.md")
		if strings.Count(l, "M1 by A") != 1 || strings.Count(l, "M2 by B") != 1 || strings.Count(l, "framing") != 1 {
			t.Fatalf("log: %s", l)
		}
		if x := pc.lire("docs/ARCHITECTURE.md"); !strings.Contains(x, "line 1 (A)") || !strings.Contains(x, "line 5 (B)") {
			t.Fatalf("architecture: %s", x)
		}
		if !strings.Contains(pc.lire("TEAM_NOTES.md"), "Menu.open()") {
			t.Fatal("team notes")
		}
	}
	// the same line on both sides: the team's version (A's, first) stays, B's in .athanor/merge/
	a.ecrire(t, "docs/ARCHITECTURE.md", strings.Replace(a.lire("docs/ARCHITECTURE.md"), "line 3", "line 3 by A", 1))
	b.ecrire(t, "docs/ARCHITECTURE.md", strings.Replace(b.lire("docs/ARCHITECTURE.md"), "line 3", "line 3 by B", 1))
	a.sync(t, m)
	r := b.sync(t, m)
	if len(r.Conflits) != 1 || !strings.Contains(b.lire("docs/ARCHITECTURE.md"), "line 3 by A") || strings.Contains(b.lire("docs/ARCHITECTURE.md"), "<<<<") {
		t.Fatalf("conflict: %+v\n%s", r, b.lire("docs/ARCHITECTURE.md"))
	}
	if !strings.Contains(b.lire("merge/docs/ARCHITECTURE.md"), "line 3 by B") || !strings.Contains(b.lire("WORKSHOP_MERGE.md"), "`docs/ARCHITECTURE.md`") {
		t.Fatal("B's version not put aside, or no note for Claude")
	}
	// the note and the copy are this computer's: never shared
	if v, _ := m.dernier(); v.Fichiers["WORKSHOP_MERGE.md"] != "" || v.Fichiers["merge/docs/ARCHITECTURE.md"] != "" {
		t.Fatal("the merge note went to the team")
	}
	// a queue item moved to done/ by A: gone from B's queue; a file A removed that B changed: B's stays
	a.ecrire(t, "queue/001.md", "do X")
	a.sync(t, m)
	b.sync(t, m)
	if b.lire("queue/001.md") != "do X" {
		t.Fatal("queue item")
	}
	_ = os.Remove(filepath.Join(a.etat, "queue", "001.md"))
	a.ecrire(t, "done/001.md", "do X")
	a.sync(t, m)
	b.sync(t, m)
	if _, err := os.Stat(filepath.Join(b.etat, "queue", "001.md")); err == nil || b.lire("done/001.md") != "do X" {
		t.Fatal("the move did not reach B")
	}
	b.ecrire(t, "SOLUTIONS.md", "x\n")
	b.sync(t, m)
	a.sync(t, m)
	_ = os.Remove(filepath.Join(a.etat, "SOLUTIONS.md"))
	b.ecrire(t, "SOLUTIONS.md", "x\ny\n")
	a.sync(t, m)
	b.sync(t, m)
	a.sync(t, m)
	if a.lire("SOLUTIONS.md") != "x\ny\n" {
		t.Fatalf("a removal won over a change: %q", a.lire("SOLUTIONS.md"))
	}
	// a link in the workshop is never followed, a file too big stays here
	if err := os.Symlink("/etc/passwd", filepath.Join(a.etat, "BUGS.md")); err == nil {
		if r := a.sync(t, m); strings.Contains(strings.Join(r.Envoyes, ","), "BUGS.md") {
			t.Fatal("a link was shared")
		}
	}
	gros := strings.Repeat("x", tailleMaxFichierAtelier+1)
	a.ecrire(t, "docs/BIG.md", gros)
	if r := a.sync(t, m); len(r.Trop) != 1 || r.Trop[0] != "docs/BIG.md" {
		t.Fatalf("too big: %+v", r)
	}
	// the store changes between the read and the write: merged again, nothing lost
	c1 := pcAtelier{etat: a.etat, base: a.base}
	a.ecrire(t, "LOG.md", a.lire("LOG.md")+"- race by A\n")
	course := &magasinCourse{magasinLocal: m, avant: func() {
		b.ecrire(t, "LOG.md", b.lire("LOG.md")+"- race by B\n")
		b.sync(t, m)
	}}
	c1.sync(t, course)
	b.sync(t, m)
	for _, pc := range []pcAtelier{a, b} {
		if l := pc.lire("LOG.md"); !strings.Contains(l, "race by A") || !strings.Contains(l, "race by B") {
			t.Fatalf("race: %s", l)
		}
	}
}

// magasinCourse: a store another computer writes to once, between this one's read and its write.
type magasinCourse struct {
	magasinLocal
	avant func()
	fait  bool
}

func (m *magasinCourse) ecrire(base int64, f map[string]string, o map[string][]byte, par string) (int64, error) {
	if !m.fait {
		m.fait = true
		m.avant()
	}
	return m.magasinLocal.ecrire(base, f, o, par)
}

func tempsAncien() time.Time { return time.Now().Add(-time.Hour) }
