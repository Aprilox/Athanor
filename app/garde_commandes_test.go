package main

import (
	"go/build"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every program the product starts on Windows goes through commande / commandeContexte (a hidden console):
// a bare exec.Command opens a console window, in Windows Terminal on Windows 11, which takes the focus.
// The exceptions are the programs started for a window the user wants.
var commandesDirectesVoulues = map[string]int{
	"commande_windows.go": 2, // the wrappers themselves
	"embarque.go":         1, // relancerKit: Athanor itself again, a window program
	"lanceur_windows.go":  1, // lancerDetacheExe: the updated Athanor.exe, a window program (HideWindow would blank it)
	"systeme_windows.go":  1, // explorer.exe: the "open with" window, asked for by the user
}

func TestAucuneCommandeNueSousWindows(t *testing.T) {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "windows", "amd64", false
	re := regexp.MustCompile(`\bexec\.Command(Context)?\(`)
	entrees, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entrees {
		nom := e.Name()
		if !strings.HasSuffix(nom, ".go") || strings.HasSuffix(nom, "_test.go") {
			continue
		}
		if ok, _ := ctx.MatchFile(".", nom); !ok {
			continue
		}
		b, _ := os.ReadFile(nom)
		n := 0
		for _, l := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "//") {
				n += len(re.FindAllString(l, -1))
			}
		}
		if n != commandesDirectesVoulues[nom] {
			t.Errorf("%s: %d direct exec.Command call(s), %d allowed: use commande / commandeContexte (or justify it in commandesDirectesVoulues)", nom, n, commandesDirectesVoulues[nom])
		}
	}
}
