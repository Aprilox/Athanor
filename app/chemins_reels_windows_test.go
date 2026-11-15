//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// courtWindows: the 8.3 short name of an existing path, or the path itself when the volume makes none.
func courtWindows(p string) string {
	l, _ := windows.UTF16PtrFromString(p)
	buf := make([]uint16, 1024)
	if n, err := windows.GetShortPathName(l, &buf[0], uint32(len(buf))); err == nil && n > 0 && int(n) < len(buf) {
		return windows.UTF16ToString(buf[:n])
	}
	return p
}

// M240k2: a device prefix (\\?\, \\.\), a network path (UNC), a junction or an 8.3 short name that leads to
// Athanor's or a system folder is refused like the folder itself: in the chat and for a project.
func TestCheminsReelsWindows(t *testing.T) {
	kit := cheminReel(t.TempDir())
	k := Kit{Racine: kit}
	_ = os.MkdirAll(filepath.Join(kit, "hub"), 0o755)
	base := cheminReel(t.TempDir())
	win := os.Getenv("SystemRoot")
	unc := `\\localhost\` + strings.ToLower(kit[:1]) + `$` + kit[2:]
	for _, c := range []string{`\\?\` + kit, `\\.\` + kit, `\\?\` + win, `//?/` + filepath.ToSlash(win), unc, `\\127.0.0.1\c$\Windows`} {
		if _, err := verifierDossierChat(k, c); !errors.Is(err, errDossierAbsolu) {
			t.Errorf("chat %q: %v", c, err)
		}
		if dossierInterdit(filepath.Join(c, "x"), kit) == "" {
			t.Errorf("project under %q accepted", c)
		}
	}
	// 8.3 short names (when the volume makes them)
	if court := courtWindows(kit); !strings.EqualFold(court, kit) {
		if _, err := verifierDossierChat(k, court); !errors.Is(err, errDossierAthanor) {
			t.Errorf("chat, short name of the kit %q: %v", court, err)
		}
		if dossierInterdit(filepath.Join(court, "hub", "x"), kit) == "" {
			t.Errorf("project in the kit by its short name %q accepted", court)
		}
	} else {
		t.Log("no 8.3 short name on this volume")
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		if court := courtWindows(pf); !strings.EqualFold(court, pf) {
			if _, err := verifierDossierChat(k, court); !errors.Is(err, errDossierSysteme) {
				t.Errorf("chat %q: %v", court, err)
			}
			if dossierInterdit(filepath.Join(court, "x"), kit) == "" {
				t.Errorf("project in %q accepted", court)
			}
		}
	}
	// a junction to the kit's hub folder: the project under it, or the junction itself
	lien := filepath.Join(base, "lien")
	if exec.Command("cmd", "/c", "mklink", "/J", lien, filepath.Join(kit, "hub")).Run() != nil {
		t.Fatal("mklink /J")
	}
	if r := cheminReel(filepath.Join(lien, "x")); !strings.EqualFold(r, filepath.Join(kit, "hub", "x")) {
		t.Errorf("cheminReel: %q", r)
	}
	for _, c := range []string{lien, filepath.Join(lien, "x"), filepath.Join(lien, "x", "y")} {
		if dossierInterdit(c, kit) == "" {
			t.Errorf("project %q (junction into the kit) accepted", c)
		}
	}
	// a normal folder still passes, existing or not
	_ = os.MkdirAll(filepath.Join(base, "site"), 0o755)
	for _, c := range []string{filepath.Join(base, "site"), filepath.Join(base, "neuf", "projet")} {
		if cle := dossierInterdit(c, kit); cle != "" {
			t.Errorf("%q refused: %s", c, cle)
		}
	}
	if c, err := verifierDossierChat(k, filepath.Join(base, "site")); err != nil || c != filepath.Join(base, "site") {
		t.Errorf("chat, good folder: %q %v", c, err)
	}
}
