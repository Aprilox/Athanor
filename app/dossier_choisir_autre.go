//go:build !windows

package main

import (
	"os/exec"
	"runtime"
	"strings"
)

// choisirDossierSysteme: the system's folder dialog; dispo false when there is none (no desktop).
func choisirDossierSysteme(depart, titre string) (chemin string, ok, dispo bool) {
	if titre == "" {
		titre = "Athanor"
	}
	switch runtime.GOOS {
	case "darwin":
		// item 1: the prompt, item 2: where it opens (passed as arguments: never in the script's text)
		c := exec.Command("osascript", "-e", "on run argv", "-e",
			"try", "-e", "return POSIX path of (choose folder with prompt (item 1 of argv) default location (POSIX file (item 2 of argv)))",
			"-e", "on error number -128", "-e", `return ""`, "-e", "end try", "-e", "end run", titre, depart)
		out, err := c.Output()
		if err != nil {
			return "", false, false
		}
		p := strings.TrimRight(strings.TrimSpace(string(out)), "/")
		if p == "" {
			return "", false, true
		}
		return p, true, true
	default:
		if z, err := exec.LookPath("zenity"); err == nil {
			out, err := exec.Command(z, "--file-selection", "--directory", "--title="+titre, "--filename="+strings.TrimRight(depart, "/")+"/").Output()
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 1 {
					return "", false, true // cancelled
				}
				return "", false, false
			}
			return strings.TrimSpace(string(out)), strings.TrimSpace(string(out)) != "", true
		}
		if k, err := exec.LookPath("kdialog"); err == nil {
			out, err := exec.Command(k, "--title", titre, "--getexistingdirectory", depart).Output()
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok && e.ExitCode() == 1 {
					return "", false, true
				}
				return "", false, false
			}
			return strings.TrimSpace(string(out)), strings.TrimSpace(string(out)) != "", true
		}
		return "", false, false
	}
}
