package main

import (
	"strings"
	"testing"
)

func TestArgsGitHub(t *testing.T) {
	// no shell anywhere: each argument goes as it is, a quote or a $ included
	a := argsGitHub(Projet{Dossier: `C:\Users\R&D\jeu`}, "lier", "creer", "x'; rm -rf ~; '")
	if strings.Join(a, "|") != `github|lier|C:\Users\R&D\jeu|creer|x'; rm -rf ~; '` {
		t.Errorf("%q", a)
	}
}

func TestLierGitHubRefuse(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	p := Projet{Nom: "a", Dossier: t.TempDir()}
	for _, c := range [][3]string{{"creer", "..", ""}, {"creer", "a b", ""}, {"creer", "x;y", ""}, {"existant", "seul", ""},
		{"existant", "a/b/c", ""}, {"existant", "-a/b", ""}, {"creer", "ok", "../x"}, {"creer", "ok", "a b"}, {"autre", "ok", ""}} {
		err := s.lierGitHub(p, c[0], c[1], true, c[2], false)
		if err == nil || !strings.Contains(err.Error(), "Invalid") && err.Error() != "Unknown" {
			t.Errorf("%v accepted (%v)", c, err)
		}
	}
	// an existing repository can be pasted as its address
	err := s.lierGitHub(p, "existant", "https://github.com/octocat/hello.git", true, "", false)
	if err != nil && strings.Contains(err.Error(), "Invalid") {
		t.Errorf("address refused: %v", err)
	}
}

func TestNomDepotPropose(t *testing.T) {
	for in, want := range map[string]string{"Mon Jeu été!": "Mon-Jeu-t", "...": "athanor-project", "watch-store": "watch-store", ".x.": "x"} {
		if got := nomDepotPropose(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestCheminWindowsPropre(t *testing.T) {
	for in, want := range map[string]string{`C:\Users\W\Desktop\montre3.`: `C:\Users\W\Desktop\montre3`, `D:\Dev\site. \x..`: `D:\Dev\site\x`,
		`C:\a\..\b`: `C:\a\..\b`, `/home/x/site.`: `/home/x/site.`, `C:\`: `C:\`, `E:/a/b `: `E:\a\b`} {
		if got := cheminWindowsPropre(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
