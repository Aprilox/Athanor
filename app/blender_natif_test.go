//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Blender on native Linux: found on the PATH (never the kit's own gateway), the Snap or the user's Flatpak;
// run as it is, or opened detached on a .blend of the project (nothing when one is open).
func TestBlenderNatif(t *testing.T) {
	d := t.TempDir()
	home, vide, nonExe, bon, moi := filepath.Join(d, "home"), filepath.Join(d, "vide"), filepath.Join(d, "nonexe"), filepath.Join(d, "bon"), filepath.Join(d, "moi")
	for _, x := range []string{vide, nonExe, bon, moi} {
		_ = os.MkdirAll(x, 0o755)
	}
	ecrireEssai(t, filepath.Join(nonExe, "blender"), "#!/bin/sh\n")
	trace := filepath.Join(d, "trace")
	ecrireEssai(t, filepath.Join(bon, "blender"), "#!/bin/sh\necho \"$@\" >> "+trace+"\necho blender-dit \"$1\"\nexit 3\n")
	_ = os.Chmod(filepath.Join(bon, "blender"), 0o755)
	// the kit's gateway: a link to this very executable
	exe, _ := os.Executable()
	if err := os.Symlink(exe, filepath.Join(moi, "blender")); err != nil {
		t.Fatal(err)
	}
	if f := trouverBlenderLinux([]string{vide, nonExe, moi, bon}, home); f != filepath.Join(bon, "blender") {
		t.Fatalf("on the PATH: %q", f)
	}
	if f := trouverBlenderLinux([]string{moi}, home); f != "" && !strings.HasPrefix(f, "/snap/") && !strings.HasPrefix(f, "/var/lib/flatpak/") {
		t.Fatalf("the gateway taken for Blender: %q", f)
	}
	flatpak := filepath.Join(home, ".local/share/flatpak/exports/bin/org.blender.Blender")
	ecrireEssai(t, flatpak, "#!/bin/sh\n")
	_ = os.Chmod(flatpak, 0o755)
	if f := trouverBlenderLinux([]string{vide}, home); f != flatpak && !strings.HasPrefix(f, "/snap/") && !strings.HasPrefix(f, "/var/lib/flatpak/") {
		t.Fatalf("the user's Flatpak: %q", f)
	}

	projet := filepath.Join(d, "projet")
	ecrireEssai(t, filepath.Join(projet, "scene.blend"), "x")
	ecrireEssai(t, filepath.Join(d, "dehors.blend"), "x")
	t.Setenv("BLENDER_EXE", filepath.Join(bon, "blender"))
	var ouvert atomic.Bool
	ancien, ancienneAttente := blenderOuvertNatif, attenteBlender
	blenderOuvertNatif, attenteBlender = func() bool { return ouvert.Load() }, 50*time.Millisecond
	defer func() { blenderOuvertNatif, attenteBlender = ancien, ancienneAttente }()
	appel := func(args ...string) (code int, sortie string) {
		var so, se bytes.Buffer
		p := &passerelle{kit: t.TempDir(), projet: projet, sortie: &so, erreur: &se}
		defer func() {
			if r := recover(); r != nil {
				code = int(r.(sortiePasserelle))
			}
			sortie = so.String() + se.String()
		}()
		return p.blenderNatif(args), ""
	}
	if c, s := appel("-b", "-P", "x.py"); c != 3 || !strings.Contains(s, "blender-dit -b") {
		t.Fatalf("run as it is: %d %q", c, s)
	}
	if c, s := appel("--open", filepath.Join(d, "dehors.blend")); c != 2 || !strings.Contains(s, "inside the project") {
		t.Fatalf("a file outside the project: %d %q", c, s)
	}
	if c, s := appel("--open", filepath.Join(projet, "x.txt")); c != 2 || !strings.Contains(s, ".blend file only") {
		t.Fatalf("not a .blend: %d %q", c, s)
	}
	go func() { time.Sleep(150 * time.Millisecond); ouvert.Store(true) }()
	if c, s := appel("--open", filepath.Join(projet, "scene.blend")); c != 0 || !strings.Contains(s, "Blender started") {
		t.Fatalf("open: %d %q", c, s)
	}
	for i := 0; i < 50; i++ {
		if b, _ := os.ReadFile(trace); strings.Contains(string(b), "scene.blend") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, _ := os.ReadFile(trace); !strings.Contains(string(b), realpathP(filepath.Join(projet, "scene.blend"))) {
		t.Fatalf("Blender not given the project's file: %q", b)
	}
	if c, s := appel("--open", filepath.Join(projet, "scene.blend")); c != 0 || !strings.Contains(s, "already open") {
		t.Fatalf("already open: %d %q", c, s)
	}
	t.Setenv("BLENDER_EXE", "")
	t.Setenv("PATH", vide)
	t.Setenv("HOME", filepath.Join(d, "personne"))
	if trouverBlenderLinux(nil, "") == "" { // (none of this machine's own Snap or Flatpak)
		if c, s := appel("--open"); c != 127 || !strings.Contains(s, "Blender is not installed") {
			t.Fatalf("no Blender: %d %q", c, s)
		}
	}
}
