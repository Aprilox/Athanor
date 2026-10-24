package main

// The application runs the kit's executable on the pilot's side, never a script: "athanor COMMAND ARGS"
// in WSL (wsl.exe --exec, no shell in between), Athanor.exe itself in native Windows, the kit's athanor on
// Linux and macOS. The arguments go as they are (a project folder as Windows names it: the Linux side
// converts it, cheminNatifPilote). lanceur_windows.go and lanceur_autre.go hold the system parts.

import (
	"fmt"
	"strings"
	"time"
)

// argsKit: the global options every call made by the application gets: the user's login environment on
// the Linux and macOS side (their PATH: ~/.local/bin, Homebrew, nvm…), as a login shell would give it.
func argsKit(args []string) []string {
	return append([]string{"--env-connexion"}, args...)
}

// executerKitArgs: COMMAND ARGS on the pilot's side, its output (both streams), stopped after delai.
func executerKitArgs(k Kit, delai time.Duration, args ...string) (string, error) {
	c := commandeKit(k, args...)
	cacherFenetre(c)
	c.WaitDelay = 5 * time.Second
	var bout strings.Builder
	c.Stdout, c.Stderr = &bout, &bout
	err := c.Start()
	if err == nil {
		minuteur := time.AfterFunc(delai, func() { _ = c.Process.Kill() })
		err = c.Wait()
		if !minuteur.Stop() {
			err = fmt.Errorf("no answer after %d s", int(delai.Seconds()))
		}
	}
	texte := strings.TrimSpace(nettoyerANSI(bout.String()))
	if err != nil {
		return texte, fmt.Errorf("%s (%v)", texte, err)
	}
	return texte, nil
}

// citerLigne: a command line as a POSIX shell reads it (the macOS .command file, messages).
func citerLigne(ligne []string) string {
	parts := make([]string, len(ligne))
	for i, a := range ligne {
		parts[i] = citerShell(a)
	}
	return strings.Join(parts, " ")
}
