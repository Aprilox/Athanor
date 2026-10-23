package main

import (
	"os"
	"regexp"
	"testing"
)

func TestVersionAlignee(t *testing.T) {
	b, err := os.ReadFile("winres/athanor.rc")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`VALUE "ProductVersion",\s*"([0-9.]+)"`).FindSubmatch(b)
	if m == nil || string(m[1]) != versionAthanor {
		t.Fatalf("versionAthanor %s, athanor.rc %q: update both", versionAthanor, m)
	}
}

func TestSousCommandes(t *testing.T) {
	if _, ok := lancerSousCommande(nil); ok {
		t.Fatal("no argument: the application")
	}
	if _, ok := lancerSousCommande([]string{"--kit=/x"}); ok {
		t.Fatal("the app's own options are not sub-commands")
	}
	if code, ok := lancerSousCommande([]string{"version"}); !ok || code != 0 {
		t.Fatal("version")
	}
	if _, ok := lancerSousCommande([]string{"--version"}); !ok {
		t.Fatal("--version")
	}
	for _, n := range []string{"isole", "--isole", "eveil", "--eveil", "retour", "--retour", "aide", "--help"} {
		c := n
		if c[0] == '-' && c != "--help" {
			c = c[2:]
		}
		if c == "--help" {
			c = "aide"
		}
		if _, ok := sousCommandes[c]; !ok {
			t.Fatalf("%s missing", n)
		}
	}
}
