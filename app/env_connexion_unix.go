//go:build !windows

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"time"
)

// chargerEnvConnexion: the environment a login shell of the user gives (its PATH above all: ~/.local/bin,
// Homebrew, nvm, cargo…), taken once into this process (and so into every program it starts). The
// application calls the kit's executable without any shell (wsl.exe --exec, an app started from the Finder),
// where the PATH is the bare system one. The user's shell prints its environment and nothing else is run.
func chargerEnvConnexion() {
	if os.Getenv("ATHANOR_ENV_CONNEXION") == "1" {
		return
	}
	_ = os.Setenv("ATHANOR_ENV_CONNEXION", "1")
	for _, sh := range []string{os.Getenv("SHELL"), "/bin/bash", "/bin/zsh", "/bin/sh"} {
		if sh == "" || !strings.HasPrefix(sh, "/") {
			continue
		}
		if st, err := os.Stat(sh); err != nil || !st.Mode().IsRegular() {
			continue
		}
		ctx, fin := context.WithTimeout(context.Background(), 15*time.Second)
		c := commandeContexte(ctx, sh, "-l", "-c", "env -0")
		c.Stdin, c.Stderr = nil, nil
		if d, err := os.UserHomeDir(); err == nil {
			c.Dir = d
		}
		out, err := c.Output()
		fin()
		if err != nil || len(out) == 0 {
			continue
		}
		for _, e := range bytes.Split(out, []byte{0}) {
			cle, val, ok := strings.Cut(string(e), "=")
			if !ok || cle == "" || strings.ContainsAny(cle, " \n") {
				continue
			}
			switch cle {
			case "PWD", "OLDPWD", "SHLVL", "_", "ATHANOR_ENV_CONNEXION":
				continue
			}
			// what the caller set on purpose stays (the app's own variables, a test's CLAUDE_BIN…), except
			// the PATH, which the login shell completes
			if _, deja := os.LookupEnv(cle); deja && cle != "PATH" {
				continue
			}
			if cle == "PATH" {
				// the login shell's PATH first, then what this process had that it lacks
				vus := map[string]bool{}
				parts := []string{}
				for _, d := range append(strings.Split(val, ":"), strings.Split(os.Getenv("PATH"), ":")...) {
					if d != "" && !vus[d] {
						vus[d] = true
						parts = append(parts, d)
					}
				}
				val = strings.Join(parts, ":")
			}
			_ = os.Setenv(cle, val)
		}
		return
	}
}
