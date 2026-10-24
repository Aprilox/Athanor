//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// langueSystemeOS: macOS' language (its locale), English elsewhere.
func langueSystemeOS() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output(); err == nil && strings.HasPrefix(strings.ToLower(string(out)), "fr") {
			return "fr"
		}
	}
	return "en"
}

// demanderConsentement: a dialog of the system: macOS' own (osascript), on Linux zenity or kdialog, else
// the question in the terminal Athanor was started from. Nobody to ask: no.
func demanderConsentement(titre, texte string) bool {
	oui, non := "Create", "Quit"
	if langueSystemeOS() == "fr" || strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "fr") {
		oui, non = "Créer", "Quitter"
	}
	if runtime.GOOS == "darwin" {
		q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
		script := "display dialog " + q(texte) + " with title " + q(titre) + " buttons {" + q(non) + ", " + q(oui) + "} default button " + q(oui) + " with icon note"
		out, err := exec.Command("osascript", "-e", script).Output()
		return err == nil && strings.Contains(string(out), oui)
	}
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		if z, err := exec.LookPath("zenity"); err == nil {
			return exec.Command(z, "--question", "--title="+titre, "--text="+texte, "--ok-label="+oui, "--cancel-label="+non, "--width=520").Run() == nil
		}
		if k, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command(k, "--title", titre, "--yesno", texte).Run() == nil
		}
	}
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n%s\n\n[y/N] ", titre, texte)
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		l = strings.ToLower(strings.TrimSpace(l))
		return l == "y" || l == "yes" || l == "o" || l == "oui"
	}
	fmt.Fprintf(os.Stderr, "%s\n\n%s\n\nNo terminal to answer in: start Athanor again with --oui to accept.\n", titre, texte)
	return false
}
