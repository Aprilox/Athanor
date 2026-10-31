//go:build darwin

package main

// macOS: athanor mcp-cage runs a local MCP server in a Seatbelt cage (sandbox-exec, the same mechanism
// Claude Code's own sandbox uses on macOS), with the rules of the Linux cage (bubblewrap): the system
// read-only, the project folder and the tool caches (tools/) writable, the network; the user's home
// hidden (except the toolchains: nvm, uv, cargo…), and Athanor's folder and its secrets; the Claude token
// never reaches the server.

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"syscall"
)

var reNomCage = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func lancerMCPCage(args []string) int {
	os.Setenv("PATH", cheminCage("/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin", os.Getenv("HOME"), estDossierCage))
	for _, v := range []string{"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH"} {
		os.Unsetenv(v)
	}
	erreur := func(code int, m string) int { fmt.Fprintln(os.Stderr, "mcp-jail: "+m); return code }
	c, err := lireArgsCage(args)
	if err != "" {
		return erreur(2, err)
	}
	if !reNomCage.MatchString(c.nom) {
		return erreur(2, "invalid name")
	}
	if !estDossierCage(c.outils) {
		return erreur(2, "tool folder not found")
	}
	sb := "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sb); err != nil {
		return erreur(1, "sandbox-exec is missing (macOS)")
	}
	maison := c.outils + "/mcp/" + c.nom
	for _, d := range []string{maison, c.outils + "/cache/npm", c.outils + "/cache/uv"} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			return 1
		}
	}
	repertoire := maison
	if len(c.ecriture) > 0 {
		repertoire = c.ecriture[0]
	}
	if err := os.Chdir(repertoire); err != nil {
		return erreur(1, "cannot enter "+repertoire)
	}
	profil := profilSeatbelt(os.Getenv("HOME"), c.outils, c.ecriture, c.lecture, estDossierCage)
	env := environnementCage(os.Environ(), c.variables, maison, c.outils)
	prog, errLook := exec.LookPath(c.commande[0])
	if errLook != nil {
		prog = c.commande[0] // sandbox-exec says it
	}
	err2 := syscall.Exec(sb, append([]string{"sandbox-exec", "-p", profil, prog}, c.commande[1:]...), env)
	fmt.Fprintf(os.Stderr, "sandbox-exec: %v\n", err2)
	return 126
}
