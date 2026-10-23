package main

// athanor reparer: the self-repair of the executable itself (plan 1.6). Claude (or the user) changes the
// Go sources of <kit>/app; then
//   athanor reparer --diff [KIT]              what changed in app/ since this executable was built
//   athanor reparer --recompiler [--tests] [--forcer] [KIT]
//       shows that change, finds Go (installs a portable copy outside Claude's reach when it is missing or
//       too old), checks the code (go vet, and go test with --tests), builds the kit's executables for
//       this PC, runs the new one's autotest, and only then puts it in place; the previous one is kept
//       (Athanor.ancien.exe, athanor.ancien). Nothing is replaced when any step fails.
//   athanor reparer --annuler [KIT]           puts the previous executables back
// The executable carries the sources it was built from (sources_integrees.go): the change is always
// shown against what is running, offline.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed *.go go.mod
var sourcesIntegrees embed.FS

// sourcesComparees: the files of the executable's sources (relative to app/, "/"-separated) and their
// contents, as built in (or read from dir).
func sourcesComparees(dir string) map[string][]byte {
	out := map[string][]byte{}
	if dir == "" {
		_ = fs.WalkDir(sourcesIntegrees, ".", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				out[p], _ = fs.ReadFile(sourcesIntegrees, p)
			}
			return nil
		})
		_ = fs.WalkDir(statique, "static", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				out[p], _ = fs.ReadFile(statique, p)
			}
			return nil
		})
		out["catalogue.json"] = catalogueJSON
		return out
	}
	ajouter := func(motif string) {
		fs, _ := filepath.Glob(filepath.Join(dir, motif))
		for _, f := range fs {
			if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
				r, _ := filepath.Rel(dir, f)
				out[filepath.ToSlash(r)], _ = os.ReadFile(f)
			}
		}
	}
	ajouter("*.go")
	ajouter("go.mod")
	ajouter("catalogue.json")
	_ = filepath.WalkDir(filepath.Join(dir, "static"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			r, _ := filepath.Rel(dir, p)
			out[filepath.ToSlash(r)], _ = os.ReadFile(p)
		}
		return nil
	})
	return out
}

// ---------------------------------------------------------------------------- unified diff (Myers)

func lignesDiff(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if s == "" {
		return nil
	}
	l := strings.Split(s, "\n")
	if l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	return l
}

type operation struct {
	genre byte // ' ', '-', '+'
	texte string
}

// myers: the shortest edit script from a to b.
func myers(a, b []string) []operation {
	n, m := len(a), len(b)
	max := n + m
	v := make([]int, 2*max+2)
	var traces [][]int
	for d := 0; d <= max; d++ {
		traces = append(traces, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[max+k-1] < v[max+k+1]) {
				x = v[max+k+1]
			} else {
				x = v[max+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[max+k] = x
			if x >= n && y >= m {
				return remonter(a, b, traces, d, max)
			}
		}
	}
	return nil
}

func remonter(a, b []string, traces [][]int, dFin, max int) []operation {
	var ops []operation
	x, y := len(a), len(b)
	for d := dFin; d > 0; d-- {
		v := traces[d]
		k := x - y
		var kPrec int
		if k == -d || (k != d && v[max+k-1] < v[max+k+1]) {
			kPrec = k + 1
		} else {
			kPrec = k - 1
		}
		xPrec := v[max+kPrec]
		yPrec := xPrec - kPrec
		for x > xPrec && y > yPrec {
			ops = append(ops, operation{' ', a[x-1]})
			x--
			y--
		}
		if x == xPrec {
			ops = append(ops, operation{'+', b[y-1]})
			y--
		} else {
			ops = append(ops, operation{'-', a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		ops = append(ops, operation{' ', a[x-1]})
		x--
		y--
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

// diffUnifie: a unified diff of one file (3 lines of context), "" when identical.
func diffUnifie(nom string, avant, apres []byte) string {
	if bytes.Equal(avant, apres) {
		return ""
	}
	a, b := lignesDiff(avant), lignesDiff(apres)
	ops := myers(a, b)
	var out strings.Builder
	ancien, neuf := "a/"+nom, "b/"+nom
	if avant == nil {
		ancien = "/dev/null"
	}
	if apres == nil {
		neuf = "/dev/null"
	}
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", ancien, neuf)
	const contexte = 3
	for i := 0; i < len(ops); {
		if ops[i].genre == ' ' {
			i++
			continue
		}
		debut := max(0, i-contexte)
		fin := i
		for fin < len(ops) {
			if ops[fin].genre != ' ' {
				fin++
				continue
			}
			j := fin
			for j < len(ops) && ops[j].genre == ' ' {
				j++
			}
			if j-fin > 2*contexte || j == len(ops) {
				fin = min(len(ops), fin+contexte)
				break
			}
			fin = j
		}
		la, lb := 1, 1
		for _, o := range ops[:debut] {
			if o.genre != '+' {
				la++
			}
			if o.genre != '-' {
				lb++
			}
		}
		na, nb := 0, 0
		for _, o := range ops[debut:fin] {
			if o.genre != '+' {
				na++
			}
			if o.genre != '-' {
				nb++
			}
		}
		if na == 0 {
			la--
		}
		if nb == 0 {
			lb--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", la, na, lb, nb)
		for _, o := range ops[debut:fin] {
			out.WriteByte(o.genre)
			out.WriteString(o.texte)
			out.WriteByte('\n')
		}
		i = fin
	}
	return out.String()
}

// changementsSources: the diff of app/ against the running executable's sources, and the changed files.
func changementsSources(app string) (string, []string) {
	avant, apres := sourcesComparees(""), sourcesComparees(app)
	noms := map[string]bool{}
	for k := range avant {
		noms[k] = true
	}
	for k := range apres {
		noms[k] = true
	}
	var tri []string
	for k := range noms {
		tri = append(tri, k)
	}
	sort.Strings(tri)
	var d strings.Builder
	var changes []string
	for _, n := range tri {
		x, okA := avant[n]
		y, okB := apres[n]
		if !okA {
			x = nil
		}
		if !okB {
			y = nil
		}
		if s := diffUnifie(n, x, y); s != "" {
			d.WriteString(s)
			etat := "changed"
			if !okA {
				etat = "new"
			} else if !okB {
				etat = "removed"
			}
			changes = append(changes, n+" ("+etat+")")
		}
	}
	return d.String(), changes
}

// ---------------------------------------------------------------------------- Go

var reVersionGo = regexp.MustCompile(`go(\d+)\.(\d+)(?:\.(\d+))?`)

func versionNumeros(s string) [3]int {
	var v [3]int
	if m := reVersionGo.FindStringSubmatch(s); m != nil {
		for i := 0; i < 3; i++ {
			v[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return v
}

func versionAuMoins(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

// versionGoVoulue: the "go 1.24.7" line of go.mod.
func versionGoVoulue(app string) string {
	b, _ := os.ReadFile(filepath.Join(app, "go.mod"))
	if m := regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)\s*$`).FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return "1.24.7"
}

// dossierGoPortable: where the portable Go goes, outside the kit and out of Claude's sandbox.
func dossierGoPortable() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Athanor", "go")
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "athanor", "go")
}

func goExe(racine string) string {
	n := "go"
	if runtime.GOOS == "windows" {
		n = "go.exe"
	}
	return filepath.Join(racine, "go", "bin", n)
}

func versionDe(goBin string) [3]int {
	c := exec.Command(goBin, "env", "GOVERSION")
	cacherFenetre(c)
	out, err := c.Output()
	if err != nil {
		return [3]int{}
	}
	return versionNumeros(string(out))
}

// trouverGo: a Go recent enough for go.mod: the PC's, else the portable one (downloaded when needed).
func trouverGo(app string, journal func(string)) (string, error) {
	voulue := "go" + versionGoVoulue(app)
	v := versionNumeros(voulue)
	if p, err := exec.LookPath("go"); err == nil && versionAuMoins(versionDe(p), v) {
		return p, nil
	}
	portable := goExe(dossierGoPortable())
	if st, err := os.Stat(portable); err == nil && st.Mode().IsRegular() && versionAuMoins(versionDe(portable), v) {
		return portable, nil
	}
	journal(fmt.Sprintf("Go %s is not installed: downloading a portable copy into %s…", voulue[2:], dossierGoPortable()))
	if err := installerGo(voulue, dossierGoPortable()); err != nil {
		return "", fmt.Errorf("portable Go: %w", err)
	}
	if !versionAuMoins(versionDe(portable), v) {
		return "", errors.New("the portable Go does not run")
	}
	return portable, nil
}

// urlGoDl: Go's downloads (their list, with each archive's SHA-256).
var urlGoDl = "https://go.dev/dl/"

type fichierGoDl struct {
	Filename, OS, Arch, Sha256, Kind string
}
type versionGoDl struct {
	Version string
	Files   []fichierGoDl
}

// installerGo: go.dev's archive for this PC, its SHA-256 checked against go.dev's list, unpacked.
func installerGo(version, dest string) error {
	client := &http.Client{Timeout: 15 * time.Minute}
	r, err := client.Get(urlGoDl + "?mode=json&include=all")
	if err != nil {
		return err
	}
	var versions []versionGoDl
	err = json.NewDecoder(r.Body).Decode(&versions)
	r.Body.Close()
	if err != nil {
		return err
	}
	var f *fichierGoDl
	for _, v := range versions {
		if v.Version != version {
			continue
		}
		for i, x := range v.Files {
			if x.OS == runtime.GOOS && x.Arch == runtime.GOARCH && x.Kind == "archive" {
				f = &v.Files[i]
			}
		}
	}
	if f == nil {
		return fmt.Errorf("no %s archive for %s/%s on go.dev", version, runtime.GOOS, runtime.GOARCH)
	}
	r, err = client.Get(urlGoDl + f.Filename)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	tmp, err := os.CreateTemp("", "go-*.archive")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r.Body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if hex.EncodeToString(h.Sum(nil)) != f.Sha256 {
		return errors.New("the archive's SHA-256 does not match go.dev's: not installed")
	}
	neuf := dest + ".neuf"
	os.RemoveAll(neuf)
	if strings.HasSuffix(f.Filename, ".zip") {
		err = deballerZip(tmp.Name(), neuf)
	} else {
		err = deballerTarGz(tmp.Name(), neuf)
	}
	if err != nil {
		os.RemoveAll(neuf)
		return err
	}
	os.RemoveAll(dest)
	return os.Rename(neuf, dest)
}

func cheminDansDossier(dest, nom string) (string, error) {
	p := filepath.Join(dest, filepath.FromSlash(nom))
	if r, err := filepath.Rel(dest, p); err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("unsafe path in the archive: %s", nom)
	}
	return p, nil
}

func deballerTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, err := cheminDansDossier(dest, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			o, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777)
			if err != nil {
				return err
			}
			_, err = io.Copy(o, t)
			o.Close()
			if err != nil {
				return err
			}
		}
	}
}

func deballerZip(archive, dest string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		p, err := cheminDansDossier(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(p, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		r, err := f.Open()
		if err != nil {
			return err
		}
		o, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			r.Close()
			return err
		}
		_, err = io.Copy(o, r)
		r.Close()
		o.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------- build, check, replace

type cibleExe struct {
	goos, goarch, nom string
}

// ciblesKit: the executables of the kit this PC uses: its own, plus the other side of WSL when the kit
// has it (Athanor.exe for the Windows app, athanor for the pilot in WSL).
func ciblesKit(kit string) []cibleExe {
	propre := cibleExe{runtime.GOOS, runtime.GOARCH, "athanor"}
	if runtime.GOOS == "windows" {
		propre.nom = "Athanor.exe"
	}
	c := []cibleExe{propre}
	if runtime.GOOS == "linux" && estFichier(filepath.Join(kit, "Athanor.exe")) {
		c = append(c, cibleExe{"windows", "amd64", "Athanor.exe"})
	}
	if runtime.GOOS == "windows" && estFichier(filepath.Join(kit, "athanor")) {
		c = append(c, cibleExe{"linux", "amd64", "athanor"})
	}
	return c
}

func ancienDe(nom string) string {
	if strings.HasSuffix(nom, ".exe") {
		return strings.TrimSuffix(nom, ".exe") + ".ancien.exe"
	}
	return nom + ".ancien"
}

func commandeGo(goBin, app string, env []string, args ...string) *exec.Cmd {
	c := exec.Command(goBin, args...)
	c.Dir = app
	c.Env = append(append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local"), env...)
	cacherFenetre(c) // from the app on Windows: no console window for go
	return c
}

func executer(c *exec.Cmd, journal func(string)) error {
	out, err := c.CombinedOutput()
	if s := strings.TrimSpace(string(out)); s != "" {
		for _, l := range strings.Split(s, "\n") {
			journal("  " + l)
		}
	}
	return err
}

// interopWSL: from WSL, the Windows executable can run (to autotest it).
func interopWSL() bool { return runtime.GOOS == "linux" && interopWindows() }

func recompiler(kit string, tests, forcer bool, journal func(string)) error {
	app := filepath.Join(kit, "app")
	if !estFichier(filepath.Join(app, "go.mod")) {
		return fmt.Errorf("no Go sources in %s", app)
	}
	diff, changes := changementsSources(app)
	dossier := filepath.Join(kit, "logs", "reparation")
	_ = os.MkdirAll(dossier, 0o755)
	date := time.Now().Format("20060102-150405")
	if len(changes) == 0 {
		journal("No change in app/ since this executable (" + versionAthanor + ") was built.")
		if !forcer {
			return nil
		}
	} else {
		f := filepath.Join(dossier, date+".diff")
		_ = os.WriteFile(f, []byte(diff), 0o644)
		journal(fmt.Sprintf("Changes since this executable was built (%d file(s); the whole diff: %s):", len(changes), f))
		for _, c := range changes {
			journal("  " + c)
		}
		lignes := strings.Split(strings.TrimRight(diff, "\n"), "\n")
		if len(lignes) > 300 {
			lignes = append(lignes[:300], fmt.Sprintf("… %d more lines in the file", len(lignes)-300))
		}
		for _, l := range lignes {
			journal("| " + l)
		}
	}
	goBin, err := trouverGo(app, journal)
	if err != nil {
		return err
	}
	journal("Go: " + goBin)
	journal("Checking the code (go vet)…")
	if err := executer(commandeGo(goBin, app, nil, "vet", "."), journal); err != nil {
		return fmt.Errorf("go vet found problems: nothing replaced")
	}
	if tests {
		journal("Running the tests (go test)…")
		if err := executer(commandeGo(goBin, app, nil, "test", "-count=1", "."), journal); err != nil {
			return fmt.Errorf("the tests fail: nothing replaced")
		}
	}
	neuf := filepath.Join(dossier, "neuf-"+date)
	_ = os.MkdirAll(neuf, 0o755)
	defer os.RemoveAll(neuf)
	cibles := ciblesKit(kit)
	for _, c := range cibles {
		drapeaux := "-s -w"
		if c.goos == "windows" {
			drapeaux += " -H windowsgui"
		}
		journal(fmt.Sprintf("Building %s (%s/%s)…", c.nom, c.goos, c.goarch))
		if err := executer(commandeGo(goBin, app, []string{"GOOS=" + c.goos, "GOARCH=" + c.goarch}, "build", "-trimpath", "-ldflags", drapeaux, "-o", filepath.Join(neuf, c.nom), "."), journal); err != nil {
			return fmt.Errorf("the build of %s failed: nothing replaced", c.nom)
		}
	}
	for _, c := range cibles {
		exe := filepath.Join(neuf, c.nom)
		if c.goos != runtime.GOOS && !(c.goos == "windows" && interopWSL()) {
			journal(c.nom + ": built for " + c.goos + ", its autotest runs on that side (not from here)")
			continue
		}
		journal("Autotest of the new " + c.nom + "…")
		cmd := exec.Command(exe, "autotest", kit)
		if c.goos == "windows" && runtime.GOOS == "linux" {
			cmd = exec.Command(exe, "autotest", cheminWindowsWSL(kit))
		}
		cacherFenetre(cmd)
		if err := executer(cmd, journal); err != nil {
			return fmt.Errorf("the new %s fails its autotest: nothing replaced", c.nom)
		}
	}
	var remplaces []string
	for _, c := range cibles {
		actuel, ancien := filepath.Join(kit, c.nom), filepath.Join(kit, ancienDe(c.nom))
		if estFichier(actuel) {
			os.Remove(ancien)
			if err := os.Rename(actuel, ancien); err != nil {
				return annulerRemplacement(kit, remplaces, fmt.Errorf("%s cannot be set aside (%v): nothing replaced", c.nom, err))
			}
		}
		if err := deplacerFichier(filepath.Join(neuf, c.nom), actuel); err != nil {
			_ = os.Rename(ancien, actuel)
			return annulerRemplacement(kit, remplaces, fmt.Errorf("the new %s cannot be put in place (%v): nothing replaced", c.nom, err))
		}
		remplaces = append(remplaces, c.nom)
	}
	journal("Done: " + strings.Join(remplaces, ", ") + " replaced (the previous ones: " + ancienDe(cibles[0].nom) + "…; `athanor reparer --annuler` puts them back).")
	journal("The pilot uses the new one from its next step; restart the app to use the new interface.")
	return nil
}

func annulerRemplacement(kit string, remplaces []string, err error) error {
	for _, n := range remplaces {
		_ = os.Rename(filepath.Join(kit, ancienDe(n)), filepath.Join(kit, n))
	}
	return err
}

// deplacerFichier: a rename, or a copy when the two folders are on different drives.
func deplacerFichier(src, dst string) error {
	if os.Rename(src, dst) == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

func annulerReparation(kit string, journal func(string)) error {
	n := 0
	for _, c := range []string{"Athanor.exe", "athanor"} {
		ancien := filepath.Join(kit, ancienDe(c))
		if !estFichier(ancien) {
			continue
		}
		actuel := filepath.Join(kit, c)
		rejete := filepath.Join(kit, strings.Replace(ancienDe(c), ".ancien", ".rejete", 1))
		os.Remove(rejete)
		if estFichier(actuel) {
			if err := os.Rename(actuel, rejete); err != nil {
				return fmt.Errorf("%s cannot be set aside: %v", c, err)
			}
		}
		if err := os.Rename(ancien, actuel); err != nil {
			return err
		}
		journal(c + ": the previous one is back (the rebuilt one: " + filepath.Base(rejete) + ")")
		n++
	}
	if n == 0 {
		return errors.New("no previous executable to put back")
	}
	return nil
}

func lancerReparer(args []string) int {
	args = cheminsOutil(args)
	var action, kit string
	tests, forcer := false, false
	for _, a := range args {
		switch a {
		case "--recompiler", "--annuler", "--diff":
			action = a
		case "--tests":
			tests = true
		case "--forcer":
			forcer = true
		default:
			kit = a
		}
	}
	if kit == "" {
		if c := lireConfig(); c.Kit != "" && (Kit{c.Kit}).Valide() {
			kit = c.Kit
		} else {
			kit = devinerKit()
		}
	}
	if kit == "" || action == "" {
		fmt.Fprintln(os.Stderr, "usage: athanor reparer --diff | --recompiler [--tests] [--forcer] | --annuler [KIT]")
		return 2
	}
	kit = abspathOS(kit)
	journal := func(s string) { fmt.Println(s) }
	var err error
	switch action {
	case "--diff":
		d, changes := changementsSources(filepath.Join(kit, "app"))
		if len(changes) == 0 {
			fmt.Println("No change in app/ since this executable (" + versionAthanor + ") was built.")
			return 0
		}
		fmt.Print(d)
		return 0
	case "--recompiler":
		err = recompiler(kit, tests, forcer, journal)
	case "--annuler":
		err = annulerReparation(kit, journal)
	}
	if err != nil {
		fmt.Println("FAILED: " + err.Error())
		return 1
	}
	return 0
}
