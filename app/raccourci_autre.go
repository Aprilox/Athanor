//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// dossierRaccourciDefaut: the Mac's Applications (the user's when the system's cannot be written), the
// Linux desktop (as xdg-user-dir names it), else the applications menu's folder.
func dossierRaccourciDefaut() string {
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		if t, err := os.CreateTemp("/Applications", ".athanor-"); err == nil {
			t.Close()
			_ = os.Remove(t.Name())
			return "/Applications"
		}
		d := filepath.Join(h, "Applications")
		_ = os.MkdirAll(d, 0o755)
		return d
	}
	if out, err := exec.Command("xdg-user-dir", "DESKTOP").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" && d != h {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d
			}
		}
	}
	for _, n := range []string{"Desktop", "Bureau"} {
		if st, err := os.Stat(filepath.Join(h, n)); err == nil && st.IsDir() {
			return filepath.Join(h, n)
		}
	}
	d := dossierMenuLinux()
	_ = os.MkdirAll(d, 0o755)
	return d
}

// dossierMenuLinux: the applications menu's folder (~/.local/share/applications).
func dossierMenuLinux() string {
	x := os.Getenv("XDG_DATA_HOME")
	if x == "" {
		h, _ := os.UserHomeDir()
		x = filepath.Join(h, ".local", "share")
	}
	return filepath.Join(x, "applications")
}

func creerRaccourciSysteme(dossier, exe, kit string) ([]string, error) {
	if runtime.GOOS == "darwin" {
		app, err := creerAppRaccourciMac(dossier, exe, iconeMac)
		if err != nil {
			return nil, err
		}
		return []string{app}, nil
	}
	// the icon, next to the installed Athanor (the window's own)
	icone := ""
	if b, err := statique.ReadFile("static/icone-512.png"); err == nil {
		icone = filepath.Join(kit, "system", "athanor.png")
		if ecrireAtomique(icone, b, 0o644) != nil {
			icone = ""
		}
	}
	entree := []byte(entreeDesktop(exe, kit, icone))
	f := filepath.Join(dossier, "athanor.desktop")
	if err := ecrireAtomique(f, entree, 0o755); err != nil {
		return nil, err
	}
	// a desktop's launcher is trusted only once said so (GNOME); best effort
	_ = exec.Command("gio", "set", f, "metadata::trusted", "true").Run()
	l := []string{f}
	if menu := dossierMenuLinux(); filepath.Clean(dossier) != filepath.Clean(menu) {
		if err := ecrireAtomique(filepath.Join(menu, "athanor.desktop"), entree, 0o644); err == nil {
			l = append(l, filepath.Join(menu, "athanor.desktop"))
		}
	}
	return l, nil
}
