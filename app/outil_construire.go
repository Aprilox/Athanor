package main

// athanor construire [--all]: builds Athanor from its Go sources (app/), for a copy of the source code
// instead of a release: Athanor.exe and athanor (this system) into the tool folder, or with --all every
// target into dist/ (what a release publishes), Athanor.app included. Each executable carries the kit's files (embarque.go),
// and Athanor.exe the Linux executable of the WSL mode. From the sources: cd app && go run . construire. Go 1.24 or later is needed.
// The Windows icon and version info (winres/athanor.rc) are compiled into rsrc_windows_amd64.syso when a
// resource compiler is present (llvm-windres or x86_64-w64-mingw32-windres); the committed one is used
// otherwise.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("construire", "construire [--all]: builds Athanor from app/ (Athanor.exe and athanor into the tool folder; --all: every system into dist/)", lancerConstruire)
}

// dossierSourcesConstruire: app/ (the current folder, or app/ under it, or next to the executable).
func dossierSourcesConstruire() string {
	candidats := []string{"."}
	if wd, err := os.Getwd(); err == nil {
		candidats = append(candidats, filepath.Join(wd, "app"))
	}
	if exe, err := os.Executable(); err == nil {
		candidats = append(candidats, filepath.Join(filepath.Dir(exe), "app"))
	}
	for _, c := range candidats {
		if st, err := os.Stat(filepath.Join(c, "go.mod")); err == nil && st.Mode().IsRegular() {
			if a, err := filepath.Abs(c); err == nil {
				return a
			}
		}
	}
	return ""
}

func lancerConstruire(args []string) int {
	src := dossierSourcesConstruire()
	if src == "" {
		fmt.Fprintln(os.Stderr, "The Go sources (app/go.mod) are not here: run it from the source code's app/ folder.")
		return 1
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		for _, c := range []string{"/usr/local/go/bin/go", `C:\Program Files\Go\bin\go.exe`, "/opt/homebrew/bin/go"} {
			if st, e := os.Stat(c); e == nil && st.Mode().IsRegular() {
				goBin, err = c, nil
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Go (the compiler of Athanor) is not installed: https://go.dev/dl/")
		fmt.Fprintln(os.Stderr, "or with your package manager: winget install GoLang.Go | sudo apt install golang-go | brew install go")
		return 1
	}
	for _, rc := range []string{"llvm-windres", "llvm-windres-18", "llvm-windres-17", "x86_64-w64-mingw32-windres"} {
		if p, err := exec.LookPath(rc); err == nil {
			a := []string{"-F", "pe-x86-64", "-i", "winres/athanor.rc", "-o", "rsrc_windows_amd64.syso"}
			if filepath.Base(p)[:4] == "llvm" {
				a = []string{"--target=pe-x86-64", "-i", "winres/athanor.rc", "-o", "rsrc_windows_amd64.syso"}
			}
			c := exec.Command(p, a...)
			c.Dir = src
			if c.Run() == nil {
				fmt.Println("resources compiled with " + rc)
			}
			break
		}
	}
	racine := filepath.Dir(src)
	// what the executables carry (embarque.go): the kit's files, then for Windows the Linux executable
	emb := filepath.Join(src, "embarque")
	if err := os.MkdirAll(emb, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "Build failed:", err)
		return 1
	}
	defer os.RemoveAll(emb) // nothing of it stays in the sources
	if err := archiverKit(racine, filepath.Join(emb, "kit.tar.gz")); err != nil {
		fmt.Fprintln(os.Stderr, "Build failed (the kit's files):", err)
		return 1
	}
	construire := func(goos, goarch, sortie string) bool {
		drapeaux := "-s -w"
		if goos == "windows" {
			drapeaux = "-s -w -H windowsgui"
		}
		c := exec.Command(goBin, "build", "-trimpath", "-tags", "embarque", "-ldflags", drapeaux, "-o", sortie, ".")
		c.Dir = src
		c.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if c.Run() != nil {
			fmt.Fprintln(os.Stderr, "Build failed: "+sortie)
			return false
		}
		fmt.Println(sortie + " ready")
		return true
	}
	// Windows: its Linux twin of the same processor first, carried compressed
	windows := func(goarch, linux, sortie string) bool {
		if !construire("linux", goarch, linux) {
			return false
		}
		if err := gzipFichier(linux, filepath.Join(emb, "athanor-linux.gz")); err != nil {
			fmt.Fprintln(os.Stderr, "Build failed:", err)
			return false
		}
		return construire("windows", goarch, sortie)
	}
	if len(args) > 0 && args[0] == "--all" {
		dist := filepath.Join(racine, "dist")
		_ = os.MkdirAll(dist, 0o755)
		for _, a := range []string{"amd64", "arm64"} {
			if !windows(a, filepath.Join(dist, "athanor-linux-"+a), filepath.Join(dist, "Athanor-"+a+".exe")) {
				return 1
			}
		}
		for _, a := range []string{"amd64", "arm64"} {
			if !construire("darwin", a, filepath.Join(dist, "athanor-darwin-"+a)) {
				return 1
			}
		}
		// Athanor.app: both Mac executables in one, with its icon (app_mac_bundle.go)
		if err := construireAppMac(src, dist, versionAthanor); err != nil {
			fmt.Fprintln(os.Stderr, "Build failed (Athanor.app):", err)
			return 1
		}
		fmt.Println(filepath.Join(dist, "Athanor.app") + " ready")
		return 0
	}
	// Athanor.exe, and this system's executable (on Windows, the Linux one of the WSL mode)
	linux := filepath.Join(racine, "athanor")
	if runtime.GOOS != "windows" && (runtime.GOOS != "linux" || runtime.GOARCH != "amd64") {
		linux = filepath.Join(emb, "athanor-linux-amd64")
	}
	if !windows("amd64", linux, filepath.Join(racine, "Athanor.exe")) {
		return 1
	}
	if linux != filepath.Join(racine, "athanor") && !construire(runtime.GOOS, runtime.GOARCH, filepath.Join(racine, "athanor")) {
		return 1
	}
	return 0
}

// archiverKit: the tar.gz of what a kit folder needs besides the executable: template/, env.example (the
// tool's .env.example), LICENSE and the Go sources of app/ (not the executables, nor embarque/). Fixed
// dates and order: the same sources always give the same archive.
func archiverKit(racine, sortie string) error {
	var noms []string
	ajouter := func(base, prefixe string, garder func(rel string, d fs.DirEntry) bool) error {
		return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if !garder(rel, d) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() {
				noms = append(noms, prefixe+rel)
			}
			return nil
		})
	}
	tout := func(string, fs.DirEntry) bool { return true }
	if err := ajouter(filepath.Join(racine, "template"), "template/", tout); err != nil {
		return err
	}
	if err := ajouter(filepath.Join(racine, "app"), "app/", func(rel string, d fs.DirEntry) bool {
		haut := strings.SplitN(rel, "/", 2)[0]
		if haut == "embarque" || strings.HasPrefix(path.Base(rel), ".") {
			return false
		}
		if !d.IsDir() && !strings.Contains(rel, "/") && (rel == "athanor" || strings.HasSuffix(strings.ToLower(rel), ".exe")) {
			return false
		}
		return true
	}); err != nil {
		return err
	}
	sort.Strings(noms)
	source := func(nom string) string {
		if nom == "env.example" {
			return filepath.Join(racine, ".env.example")
		}
		return filepath.Join(racine, filepath.FromSlash(nom))
	}
	noms = append([]string{"env.example"}, noms...)
	if estFichier(filepath.Join(racine, "LICENSE")) { // a tool folder made before 2.26 may have none
		noms = append([]string{"LICENSE"}, noms...)
	}
	f, err := os.Create(sortie)
	if err != nil {
		return err
	}
	z := gzip.NewWriter(f)
	t := tar.NewWriter(z)
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, nom := range noms {
		b, err := os.ReadFile(source(nom))
		if err != nil {
			f.Close()
			return err
		}
		mode := int64(0o644)
		if st, err := os.Stat(source(nom)); err == nil && st.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := t.WriteHeader(&tar.Header{Name: nom, Mode: mode, Size: int64(len(b)), ModTime: date, Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			f.Close()
			return err
		}
		if _, err := t.Write(b); err != nil {
			f.Close()
			return err
		}
	}
	err = t.Close()
	if e := z.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

// gzipFichier: src compressed into dest.
func gzipFichier(src, dest string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	z, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := z.Write(b); err != nil {
		return err
	}
	if err := z.Close(); err != nil {
		return err
	}
	return os.WriteFile(dest, buf.Bytes(), 0o644)
}
