package main

// athanor construire [--all]: builds Athanor from its Go sources (app/), for a copy of the source code
// instead of a release: Athanor.exe and athanor (this system) into the tool folder, or with --all every
// target into dist/. From the sources: cd app && go run . construire. Go 1.24 or later is needed.
// The Windows icon and version info (winres/athanor.rc) are compiled into rsrc_windows_amd64.syso when a
// resource compiler is present (llvm-windres or x86_64-w64-mingw32-windres); the committed one is used
// otherwise.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	construire := func(goos, goarch, sortie string) bool {
		drapeaux := "-s -w"
		if goos == "windows" {
			drapeaux = "-s -w -H windowsgui"
		}
		c := exec.Command(goBin, "build", "-trimpath", "-ldflags", drapeaux, "-o", sortie, ".")
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
	racine := filepath.Dir(src)
	if len(args) > 0 && args[0] == "--all" {
		dist := filepath.Join(racine, "dist")
		_ = os.MkdirAll(dist, 0o755)
		for _, c := range [][2]string{{"windows", "amd64"}, {"windows", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
			nom := "athanor-" + c[0] + "-" + c[1]
			if c[0] == "windows" {
				nom = "Athanor-" + c[1] + ".exe"
			}
			if !construire(c[0], c[1], filepath.Join(dist, nom)) {
				return 1
			}
		}
		return 0
	}
	if !construire("windows", "amd64", filepath.Join(racine, "Athanor.exe")) {
		return 1
	}
	// its Linux twin on Windows (the pilots of the WSL mode), this system's otherwise
	goos, goarch := runtime.GOOS, runtime.GOARCH
	if goos == "windows" {
		goos, goarch = "linux", "amd64"
	}
	if !construire(goos, goarch, filepath.Join(racine, "athanor")) {
		return 1
	}
	return 0
}
