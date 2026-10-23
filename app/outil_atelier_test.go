package main

// athanor screenshots | map | archive | todo | etat-taches against athanor.py and taches.py: the same
// outputs and the same files.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// lancerAvecEntree: f run with entree as its standard input (nil: a terminal-less empty input).
func lancerAvecEntree(t *testing.T, entree []byte, f func() int) (string, string, int) {
	t.Helper()
	e, _ := os.CreateTemp(t.TempDir(), "entree")
	_, _ = e.Write(entree)
	_, _ = e.Seek(0, 0)
	vi := os.Stdin
	os.Stdin = e
	defer func() { os.Stdin = vi; e.Close() }()
	return capturer(t, f)
}

func pythonEntree(t *testing.T, env []string, entree []byte, script string, args ...string) (string, string, int) {
	t.Helper()
	c := execPython(script, args...)
	c.Env = append(os.Environ(), env...)
	c.Stdin = bytes.NewReader(entree)
	var so, se strings.Builder
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

var reHorodatage = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d`)

// contenus: every file under d (but the lock and the given folders), the dates replaced.
func contenus(t *testing.T, d string, sauf ...string) string {
	var out []string
	_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(d, p)
		for _, s := range sauf {
			if r == s || strings.HasPrefix(r, s+"/") {
				if e.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if e.IsDir() || e.Name() == ".lock" {
			return nil
		}
		b, _ := os.ReadFile(p)
		out = append(out, r+"\n"+reHorodatage.ReplaceAllString(string(b), "DATE"))
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, "\n----\n")
}

func ecrirePNG(t *testing.T, chemin string, l, h int, f func(x, y int) color.NRGBA) {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, l, h))
	for y := 0; y < h; y++ {
		for x := 0; x < l; x++ {
			im.Set(x, y, f(x, y))
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	_ = os.MkdirAll(filepath.Dir(chemin), 0o755)
	_ = os.WriteFile(chemin, b.Bytes(), 0o644)
}

func TestPariteCaptures(t *testing.T) {
	s := scriptPython(t, "athanor.py")
	pillow(t)
	d, _ := filepath.EvalSymlinks(t.TempDir())
	motif := func(dx int) func(x, y int) color.NRGBA {
		return func(x, y int) color.NRGBA {
			return color.NRGBA{uint8((x + dx) * 3), uint8(y * 5), uint8((x*y + dx) % 256), 255}
		}
	}
	preparer := func(p string) {
		sc, ref := filepath.Join(p, ".athanor/screenshots"), filepath.Join(p, ".athanor/screenshots_ref")
		ecrirePNG(t, filepath.Join(sc, "pareil.png"), 640, 360, motif(0))
		ecrirePNG(t, filepath.Join(ref, "pareil.png"), 640, 360, motif(0))
		ecrirePNG(t, filepath.Join(sc, "peu.png"), 640, 360, motif(1))
		ecrirePNG(t, filepath.Join(ref, "peu.png"), 640, 360, motif(0))
		ecrirePNG(t, filepath.Join(sc, "change.png"), 800, 450, motif(40))
		ecrirePNG(t, filepath.Join(ref, "change.png"), 800, 450, motif(0))
		ecrirePNG(t, filepath.Join(sc, "taille.png"), 300, 200, motif(0))
		ecrirePNG(t, filepath.Join(ref, "taille.png"), 320, 200, motif(0))
		ecrirePNG(t, filepath.Join(sc, "noir.png"), 1920, 1080, func(x, y int) color.NRGBA { return color.NRGBA{2, 2, uint8(x % 5), 255} })
		ecrirePNG(t, filepath.Join(sc, "nouveau.png"), 100, 900, motif(3))
		ecrirePNG(t, filepath.Join(sc, ".cache.png"), 50, 50, motif(3))
		ecrirePNG(t, filepath.Join(ref, "parti.png"), 64, 64, motif(0))
		ecrirePNG(t, filepath.Join(p, "autres/a.png"), 64, 64, motif(9))
		_ = os.WriteFile(filepath.Join(sc, "notes.txt"), []byte("x"), 0o644)
		_ = os.MkdirAll(filepath.Join(p, "vide"), 0o755)
	}
	for _, args := range [][]string{{}, {"autres"}, {"vide"}, {"absent"}, {"--accept"}, {"autres", "--accept"}} {
		py, gp := filepath.Join(d, "py"), filepath.Join(d, "go")
		os.RemoveAll(py)
		os.RemoveAll(gp)
		preparer(py)
		preparer(gp)
		so, se, code := pythonEntree(t, []string{"PROJECT=" + py}, nil, s, append([]string{"screenshots"}, args...)...)
		t.Setenv("PROJECT", gp)
		gso, gse, gcode := lancerAvecEntree(t, nil, func() int { return lancerAtelier("screenshots")(args) })
		if code != gcode || so != gso || se != gse {
			t.Errorf("screenshots %v:\n go %d %q %q\n py %d %q %q", args, gcode, gso, gse, code, so, se)
		}
		if a, b := contenus(t, py, ".athanor/sheets"), contenus(t, gp, ".athanor/sheets"); a != b {
			t.Errorf("screenshots %v: files differ", args)
		}
		// the sheets: the same names and sizes (the letters are drawn by each)
		taille := func(p string) string {
			var out []string
			fs, _ := filepath.Glob(filepath.Join(p, ".athanor/sheets/*"))
			for _, f := range fs {
				r, _ := os.Open(f)
				c, err := png.DecodeConfig(r)
				r.Close()
				out = append(out, fmt.Sprintf("%s %dx%d %v", filepath.Base(f), c.Width, c.Height, err))
			}
			return strings.Join(out, ";")
		}
		if a, b := taille(py), taille(gp); a != b {
			t.Errorf("screenshots %v: sheets\n go %s\n py %s", args, b, a)
		}
	}
}

func TestPariteCarte(t *testing.T) {
	s := scriptPython(t, "athanor.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	preparer := func(p string) {
		f := func(r, c string) {
			_ = os.MkdirAll(filepath.Dir(filepath.Join(p, r)), 0o755)
			_ = os.WriteFile(filepath.Join(p, r), []byte(c), 0o644)
		}
		f("main.go", "package main\n\nfunc main() {\n}\n\ntype Joueur struct{}\nfunc (j *Joueur) Saute() {}\n")
		f("src/jeu.gd", "extends Node\nclass_name Joueur\nsignal mort\nfunc _ready():\n\tpass\nstatic func outil(x):\n\tpass\n")
		f("src/app.ts", "export default class App {}\nexport const go = async (a) => a\nconst x = y => y\nexport async function lance() {}\ninterface Truc {}\nenum Couleur { A }\ntype T = string\n")
		f("src/lib.rs", "pub fn a() {}\npub(crate) fn b() {}\nimpl Truc {}\ntrait Z {}\nstruct Été {}\nfn ñandú() {}\n")
		f("shader.glsl", "uniform vec3 couleur;\nvoid main() {\n}\nfloat bruit(vec2 p) { return 0.0; }\nvec3 teinte (float t) {}\n")
		f("script.py", "def a():\n    pass\nclass B:\n    async def c(self): pass\n")
		f("crlf.lua", "function a()\r\nend\r\nlocal function b()\rend\r")
		f("latin1.c", "int f\xe9(void) {}\nvoid g(void) {\xff\xfe}\n")
		f("README.md", "# Titre\n\n## Sous-titre  \n```\n# pas un titre\n```\n### Trois\n#### Quatre\n#Collé\n#\u00a0Insécable\n  # indenté\n")
		f("docs/Guide.MD", "# Majuscules\n")
		f("vide.md", "")
		f("gros.js", "function x() {}\n"+strings.Repeat("//\n", 140000))
		f("node_modules/a/index.js", "function cache() {}\n")
		f(".athanor/docs/ARCHITECTURE.md", "# Archi\n## Parties\n")
		f(".athanor/docs/sous/plan.md", "# Sous\n")
		f(".athanor/PLAN.md", "# Plan\n")
		f("image.png", "x")
		f("sans.extension", "function x() {}\n")
		f(".cache.", "x")
		f("ignore/x.js", "function ignore() {}\n")
		f(".gitignore", "ignore/\n")
		_ = os.Symlink("main.go", filepath.Join(p, "lien.go"))
		_ = os.Symlink("/nulle/part", filepath.Join(p, "casse.go"))
		for _, c := range [][]string{{"init", "-q"}, {"add", "main.go", "src", "README.md"}} {
			cmd := exec.Command("git", c...)
			cmd.Dir = p
			_ = cmd.Run()
		}
	}
	for _, cas := range []string{"git", "sans-git", "git-absent"} {
		py, gp := filepath.Join(d, cas, "py"), filepath.Join(d, cas, "go")
		preparer(py)
		preparer(gp)
		if cas == "sans-git" {
			os.RemoveAll(filepath.Join(py, ".git"))
			os.RemoveAll(filepath.Join(gp, ".git"))
		}
		env := []string{"PROJECT=" + py}
		if cas == "git-absent" { // no git on this PC: every file of the folder
			env = append(env, "PATH=/nulle-part")
		}
		so, se, code := pythonEntree(t, env, nil, s, "map")
		if cas == "git-absent" {
			t.Setenv("PATH", "/nulle-part")
		}
		t.Setenv("PROJECT", gp)
		gso, gse, gcode := lancerAvecEntree(t, nil, func() int { return lancerAtelier("map")(nil) })
		a, _ := os.ReadFile(filepath.Join(py, ".athanor/MAP.md"))
		b, _ := os.ReadFile(filepath.Join(gp, ".athanor/MAP.md"))
		if code != gcode || so != gso || se != gse || !bytes.Equal(a, b) || (cas != "sans-git" && len(a) < 900) {
			t.Errorf("map %s:\n go %d %q %q\n%s\n py %d %q %q\n%s", cas, gcode, gso, gse, b, code, so, se, a)
		}
	}
}

func TestPariteArchive(t *testing.T) {
	s := scriptPython(t, "athanor.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	plan := func(coches, ouverts int, titre bool) string {
		var b strings.Builder
		if titre {
			b.WriteString("# Plan\n\nIntro.\n\n")
		}
		for i := 1; i <= coches+ouverts; i++ {
			c := " "
			if i <= coches {
				c = "x"
				if i%2 == 0 {
					c = "X"
				}
			}
			fmt.Fprintf(&b, "- [%s] M%d : jalon %d — Files: f%d.\n  Détail %d\n\tSuite\n", c, i, i, i, i)
			if i%3 == 0 {
				b.WriteString("\n\n\n")
			}
		}
		b.WriteString("\nFin.\n")
		return b.String()
	}
	journal := func(n int) string {
		var b strings.Builder
		b.WriteString("# Journal\n\nEn-tête\r\n\n")
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "## Cycle %d\n\nFait %d.\n\n\n", i, i)
		}
		return b.String()
	}
	cas := []struct {
		nom       string
		plan, log string
		note      bool
	}{
		{"rien", plan(4, 2, true), journal(5), false},
		{"seuil", plan(9, 3, true), journal(10), false},
		{"archive", plan(12, 3, true), journal(16), false},
		{"sans-titre", plan(20, 1, false), journal(30), true},
		{"absents", "", "", false},
		{"crlf", strings.ReplaceAll(plan(15, 2, true), "\n", "\r\n"), strings.ReplaceAll(journal(12), "\n", "\r"), false},
	}
	for _, c := range cas {
		for _, deux := range []bool{false, true} { // a second run on what the first wrote
			py, gp := filepath.Join(d, c.nom, "py"), filepath.Join(d, c.nom, "go")
			if !deux {
				for _, p := range []string{py, gp} {
					_ = os.MkdirAll(filepath.Join(p, ".athanor"), 0o755)
					if c.plan != "" {
						_ = os.WriteFile(filepath.Join(p, ".athanor/PLAN.md"), []byte(c.plan), 0o644)
						_ = os.WriteFile(filepath.Join(p, ".athanor/LOG.md"), []byte(c.log), 0o644)
					}
				}
			}
			so, se, code := pythonEntree(t, []string{"PROJECT=" + py}, nil, s, "archive")
			t.Setenv("PROJECT", gp)
			gso, gse, gcode := lancerAvecEntree(t, nil, func() int { return lancerAtelier("archive")(nil) })
			if code != gcode || so != gso || se != gse {
				t.Errorf("archive %s:\n go %d %q %q\n py %d %q %q", c.nom, gcode, gso, gse, code, so, se)
			}
			if a, b := contenus(t, py), contenus(t, gp); a != b {
				t.Errorf("archive %s: files differ\n go %s\n py %s", c.nom, b, a)
			}
		}
	}
}

func TestPariteTodo(t *testing.T) {
	s := scriptPython(t, "athanor.py")
	st := scriptPython(t, "taches.py")
	d, _ := filepath.EvalSymlinks(t.TempDir())
	py, gp := filepath.Join(d, "py"), filepath.Join(d, "go")
	for _, p := range []string{py, gp} {
		_ = os.MkdirAll(filepath.Join(p, ".athanor"), 0o755)
	}
	repondre := func(texte string) {
		for _, p := range []string{py, gp} {
			_ = os.WriteFile(filepath.Join(p, ".athanor/for-you/replies.json"), []byte(texte), 0o644)
		}
	}
	type etape struct {
		args   []string
		entree string
		avant  func()
	}
	long := strings.Repeat("é", 151)
	etapes := []etape{
		{args: []string{}},
		{args: []string{"inconnu"}},
		{args: []string{"list"}},
		{args: []string{"wait"}},
		{args: []string{"add"}},
		{args: []string{"add", long}},
		{args: []string{"add", "Créer", "la boutique", "--need", "l'adresse", "--secret", "SHOPIFY_TOKEN, API_KEY", "--secret", "API_KEY", "--milestone", "M2,M3"}, entree: "1. Va sur https://partners.shopify.com\n2. Crée\r\n\n"},
		{args: []string{"add", "créer LA boutique"}},
		{args: []string{"add", "Une clé", "--secret", "PATH"}},
		{args: []string{"add", "Une clé", "--secret", "ld_x"}},
		{args: []string{"add", "Une clé", "--secret", "LD_PRELOAD"}},
		{args: []string{"add", "Une clé", "--milestone", "M 3"}},
		{args: []string{"add", "Une clé", "--milestone", "it's"}},
		{args: []string{"add", "Une clé", "--need", strings.Repeat("x", 1001)}},
		{args: []string{"add", "Trop", "long"}, entree: strings.Repeat("d", 8001)},
		{args: []string{"add", "Pile", "8000"}, entree: strings.Repeat("ü", 8000)},
		{args: []string{"add", "  Un code SMS  ", "--need"}, entree: "  "},
		{args: []string{"list"}},
		{args: []string{"list", "--all"}},
		{args: []string{"edit"}},
		{args: []string{"edit", "t1", "--need", "l'URL de la boutique", "--secret", "OTHER_KEY"}, entree: "Nouvelles étapes"},
		{args: []string{"edit", "T99"}},
		{args: []string{"ok", "T1"}},
		{args: []string{"ok", "T1", "  "}},
		{args: []string{"missing", "T1", strings.Repeat("m", 2001)}},
		{args: []string{"wait"}},
		{args: []string{"ok", "T2", "rien", "reçu"}},
		{args: []string{"list", "--all"}},
		{avant: func() {
			repondre(`{"T1": [{"action": "fait", "texte": "C'est fait\nvoilà", "fichiers": ["a.txt", 3], "secrets": ["SHOPIFY_TOKEN"], "le": "2026-10-23T10:00:00"}], "T3": [{"action": "passer", "texte": ""}], "T9": "x"}`)
			for _, p := range []string{py, gp} {
				_ = os.WriteFile(filepath.Join(p, ".athanor/secrets.env"), []byte("SHOPIFY_TOKEN='abc'\nAPI_KEY=nope\nOTHER_KEY='a'\\''b'\n"), 0o600)
			}
		}, args: []string{"wait"}},
		{args: []string{"ok", "T1", "vérifié"}},
		{args: []string{"list"}},
		{args: []string{"ok", "T1", "vérifié", "après", "lecture"}},
		{args: []string{"resolved", "t3", "fait autrement"}},
		{args: []string{"missing", "T4", "il manque le code"}},
		{args: []string{"list", "--all"}},
		{args: []string{"wait"}},
		{args: []string{"add", "Nouvelle"}},
		{avant: func() { repondre(`{"T4": [{"action": "fait"}, {"action": "fait", "texte": "le code: 1234"}]}`) }, args: []string{"list"}},
		{args: []string{"ok", "T4", "ok"}},
		{args: []string{"list", "--all"}},
	}
	for i := 0; i < 32; i++ { // the open tasks' limit
		etapes = append(etapes, etape{args: []string{"add", fmt.Sprintf("Tâche %d", i)}})
	}
	etapes = append(etapes, etape{args: []string{"list"}})
	for i, e := range etapes {
		if e.avant != nil {
			e.avant()
		}
		so, se, code := pythonEntree(t, []string{"PROJECT=" + py}, []byte(e.entree), s, append([]string{"todo"}, e.args...)...)
		t.Setenv("PROJECT", gp)
		gso, gse, gcode := lancerAvecEntree(t, []byte(e.entree), func() int { return lancerTodo(e.args) })
		n := func(x string) string { return strings.ReplaceAll(reHorodatage.ReplaceAllString(x, "DATE"), py, "P") }
		ng := func(x string) string { return strings.ReplaceAll(reHorodatage.ReplaceAllString(x, "DATE"), gp, "P") }
		if code != gcode || n(so) != ng(gso) || n(se) != ng(gse) {
			t.Errorf("%d todo %q:\n go %d %q %q\n py %d %q %q", i, e.args, gcode, gso, gse, code, so, se)
		}
		if a, b := contenus(t, py), contenus(t, gp); a != b {
			t.Errorf("%d todo %q: files differ\n go %s\n py %s", i, e.args, b, a)
			break
		}
		eso, _, _ := pythonEntree(t, nil, nil, st, "etat", py)
		gso, _, _ = capturer(t, func() int { return lancerEtatTaches([]string{gp}) })
		if eso != gso {
			t.Errorf("%d etat:\n go %q\n py %q", i, gso, eso)
		}
	}
	// no workshop
	vide := filepath.Join(d, "vide")
	_ = os.MkdirAll(vide, 0o755)
	for _, args := range [][]string{{"add", "x"}, {"list"}, {"wait"}, {"ok", "T1", "x"}} {
		so, se, code := pythonEntree(t, []string{"PROJECT=" + vide}, nil, s, append([]string{"todo"}, args...)...)
		t.Setenv("PROJECT", vide)
		gso, gse, gcode := lancerAvecEntree(t, nil, func() int { return lancerTodo(args) })
		if code != gcode || so != gso || se != gse {
			t.Errorf("no workshop %q:\n go %d %q %q\n py %d %q %q", args, gcode, gso, gse, code, so, se)
		}
	}
}
