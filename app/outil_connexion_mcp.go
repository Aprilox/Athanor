package main

// athanor connexion-mcp KIT ID LINE…: the window where the user signs in once to a remote MCP server (its
// OAuth): the lines of explanation, then Claude Code with that server only.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("connexion-mcp", "connexion-mcp KIT ID LINE…: signing in once to a remote MCP server of the library, in Claude Code (the app's window)", lancerConnexionMCP)
}

// chargerEnvKit: the tool's .env into this process (KEY=VALUE lines, never executed; what is already set
// stays), and the PATH Claude Code and the gateways are found in.
func chargerEnvKit(kit string) {
	if b, err := os.ReadFile(filepath.Join(kit, ".env")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if m := reCleEnvPilote.FindStringSubmatch(strings.TrimSuffix(l, "\r")); m != nil {
				if _, ok := os.LookupEnv(m[1]); !ok {
					_ = os.Setenv(m[1], m[2])
				}
			}
		}
	}
	home, _ := os.UserHomeDir()
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	sep := string(os.PathListSeparator)
	_ = os.Setenv("PATH", filepath.Join(home, ".local", "bin")+sep+filepath.Join(kit, "system", "bin")+sep+os.Getenv("PATH"))
}

func lancerConnexionMCP(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor connexion-mcp KIT ID [LINE…]")
		return 2
	}
	kit := abspathOS(args[0])
	_ = os.Chdir(kit)
	chargerEnvKit(kit)
	f, err := os.CreateTemp("", "athanor-mcp-*.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	f.Close()
	defer os.Remove(f.Name())
	if lancerExtensions([]string{"connexion", kit, args[1], f.Name()}) != 0 {
		time.Sleep(8 * time.Second)
		return 1
	}
	for _, l := range args[2:] {
		fmt.Println(l)
	}
	fmt.Println()
	claude := trouverClaudePilote(estWindows)
	if claude == "" {
		claude = "claude"
	}
	c := commande(claude, "--mcp-config", f.Name(), "--strict-mcp-config")
	c.Env = append(os.Environ(), "ENABLE_CLAUDEAI_MCP_SERVERS=false")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = c.Run()
	return 0
}
