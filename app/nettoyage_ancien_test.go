package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNettoyageAncienKit: the files of older versions go when they are as shipped (LF or CRLF), a changed
// one is kept aside, everything else of the folder stays; the folders left empty go.
func TestNettoyageAncienKit(t *testing.T) {
	d := t.TempDir()
	ecrire := func(rel, c string) {
		t.Helper()
		f := filepath.Join(d, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	existe := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(d, filepath.FromSlash(rel)))
		return err == nil
	}
	start, pilot, guide := "@echo off\nathanor\n", "#!/bin/bash\necho pilot\n", "# Docs\n"
	ancienne := listeAnciens
	listeAnciens = func() map[string]map[string]bool {
		return lireListeAnciens(blobGit([]byte(start)) + "\tcommands/start.cmd\n" + blobGit([]byte(pilot)) + "\tsystem/pilot.sh\n" +
			blobGit([]byte(guide)) + "\tdocs/guide/a.md\n" + blobGit([]byte("x")) + "\tcommands/stop.cmd\n" +
			blobGit([]byte("y")) + "\tsystem/bin/adb\n" + blobGit([]byte("z")) + "\t../dehors\n" + blobGit([]byte("w")) + "\t.env\n")
	}
	defer func() { listeAnciens = ancienne }()

	ecrire("commands/start.cmd", strings.ReplaceAll(start, "\n", "\r\n")) // as Git for Windows checks it out
	ecrire("commands/stop.cmd", "my own change\n")
	ecrire("commands/mine.cmd", "a file of the user's\n")
	ecrire("system/pilot.sh", pilot)
	ecrire("system/__pycache__/guard.cpython-312.pyc", "\x00")
	ecrire("system/distro.txt", "Ubuntu\n")
	ecrire("system/bin/adb", "y")
	ecrire("system/bin/rtk", "RTK")
	ecrire("docs/guide/a.md", guide)
	ecrire(".env", "w")
	ecrire("logs/x.log", "log\n")

	b := nettoyerAncienKit(d)
	for _, rel := range []string{"commands/start.cmd", "system/pilot.sh", "docs/guide/a.md", "system/__pycache__", "docs", "system/bin/adb"} {
		if existe(rel) {
			t.Fatalf("still there: %s (%+v)", rel, b)
		}
	}
	for _, rel := range []string{"commands/mine.cmd", "system/distro.txt", "system/bin/rtk", ".env", "logs/x.log", "system"} {
		if !existe(rel) {
			t.Fatalf("removed: %s", rel)
		}
	}
	if existe("commands/stop.cmd") || len(b.Ecartes) != 1 {
		t.Fatalf("the changed file not moved aside: %+v", b)
	}
	if c, _ := os.ReadFile(filepath.Join(b.Garde, "commands", "stop.cmd")); string(c) != "my own change\n" {
		t.Fatalf("the changed file kept: %q", c)
	}
	if !strings.HasPrefix(b.Garde, filepath.Join(d, "logs")) {
		t.Fatalf("kept outside Athanor's logs: %s", b.Garde)
	}
	if l, _ := filepath.Glob(filepath.Join(d, "logs", "nettoyage-*.txt")); len(l) != 1 {
		t.Fatal("no report of the clean-up")
	}
	// nothing more to do the second time
	if b2 := nettoyerAncienKit(d); len(b2.Retires)+len(b2.Ecartes) != 0 {
		t.Fatalf("second clean-up: %+v", b2)
	}
}

// TestNettoyageAncienLien: a folder of the list that is a link (to elsewhere) is never followed.
func TestNettoyageAncienLien(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need rights on Windows")
	}
	d, ailleurs := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(ailleurs, "start.cmd"), []byte("a"), 0o644)
	if err := os.Symlink(ailleurs, filepath.Join(d, "commands")); err != nil {
		t.Skip(err)
	}
	ancienne := listeAnciens
	listeAnciens = func() map[string]map[string]bool {
		return lireListeAnciens(blobGit([]byte("a")) + "\tcommands/start.cmd\n")
	}
	defer func() { listeAnciens = ancienne }()
	nettoyerAncienKit(d)
	if _, err := os.Stat(filepath.Join(ailleurs, "start.cmd")); err != nil {
		t.Fatal("a file behind a link removed")
	}
}

// TestListeAnciensReelle: the list built in: paths Athanor no longer uses, valid hashes, and its known files.
func TestListeAnciensReelle(t *testing.T) {
	l := lireListeAnciens(anciensFichiersKit)
	if len(l) < 100 || l["commands/start.cmd"] == nil || l["system/pilot.sh"] == nil || l["README.md"] == nil || l["system/bin/adb"] == nil {
		t.Fatalf("list: %d paths", len(l))
	}
	for p := range l {
		if strings.HasPrefix(p, "system/bin-windows/") || strings.HasPrefix(p, "template/") || p == ".env" || p == "system/bin/rtk" {
			t.Fatalf("a path Athanor uses: %s", p)
		}
	}
	if blobGit([]byte("hello\n")) != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatal("blob hash")
	}
}
