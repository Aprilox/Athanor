package main

// athanor chat session KIT PROJECT ID MODE MODEL SESSION: one turn of the app's chat about a project (the
// application wrote logs/chat/ID.msg, .prompt and .subject): Claude Code reads the project (MODE "modif":
// may edit it, the change is committed), its stream into logs/chat/ID.jsonl, its errors into ID.err.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func sessionChat(kit, projet, id, mode, modele, session string) int {
	kit = abspathOS(kit)
	chargerEnvKit(kit)
	_ = os.Setenv("ENABLE_CLAUDEAI_MCP_SERVERS", "false") // the connectors of the user's claude.ai account stay out
	if !reIdSur.MatchString(id) {
		fmt.Fprintln(os.Stderr, "invalid id")
		return 2
	}
	c := filepath.Join(kit, "logs", "chat", id)
	if st, err := os.Stat(projet); err != nil || !st.IsDir() {
		fmt.Fprintln(os.Stderr, "Project folder not found: "+projet)
		return 2
	}
	if err := os.Chdir(projet); err != nil {
		fmt.Fprintln(os.Stderr, "Project folder not found: "+projet)
		return 2
	}
	if err := reglagesChat(projet, c+".settings.json"); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write the chat's settings")
		return 2
	}
	msg, _ := os.ReadFile(c + ".msg")
	consigne, _ := os.ReadFile(c + ".prompt")
	if ponytailActif() { // Ponytail (ponytail.go): a change the smallest complete one, a question answered lean
		if mode == "modif" {
			consigne = append(consigne, consignesPonytail...)
		} else {
			consigne = append(consigne, consignesPonytailLecture...)
		}
	}
	args := []string{"-p", string(msg), "--output-format", "stream-json", "--verbose", "--max-turns", "40", "--strict-mcp-config",
		"--settings", c + ".settings.json", "--append-system-prompt", string(consigne)}
	if mode == "modif" {
		args = append(args, "--permission-mode", "acceptEdits", "--allowedTools", "Read", "Glob", "Grep", "Edit", "Write", "MultiEdit",
			"--disallowedTools", "Bash", "NotebookEdit", "WebFetch", "WebSearch", "Task")
	} else {
		args = append(args, "--allowedTools", "Read", "Glob", "Grep", "--disallowedTools", "Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch", "WebSearch", "Task")
	}
	if modele != "" {
		args = append(args, "--model", modele)
	}
	if session != "" {
		args = append(args, "--resume", session)
	}
	claude := trouverClaudePilote(estWindows)
	if claude == "" {
		claude = "claude"
	}
	sortie, err := os.Create(c + ".jsonl")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer sortie.Close()
	erreurs, err := os.Create(c + ".err")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer erreurs.Close()
	ctx, fin := context.WithTimeout(context.Background(), 1200*time.Second)
	defer fin()
	cmd := commandeContexte(ctx, claude, args...)
	cmd.Stdout, cmd.Stderr = sortie, erreurs
	err = cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0 {
			code = cmd.ProcessState.ExitCode()
		}
		if ctx.Err() != nil {
			code = 124
		}
		if cmd.ProcessState == nil {
			fmt.Fprintln(erreurs, err.Error())
			code = 127
		}
	}
	if mode == "modif" && dossierExistePilote(filepath.Join(projet, ".git")) {
		if out, err := commande("git", "status", "--porcelain").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			if commande("git", "add", "-A").Run() == nil && commande("git", "commit", "-q", "-F", c+".subject").Run() == nil {
				if h, err := commande("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
					_ = os.WriteFile(c+".commit", h, 0o644)
				}
			}
		}
	}
	return code
}

func init() {
	enregistrerCommande("ameliorer", "ameliorer KIT FILE: one turn of Claude Code on the request written in FILE, in an empty folder (the app's “Improve”)", lancerAmeliorer)
}

// lancerAmeliorer: one turn, no project around it: an empty temporary folder as working directory, so
// that the session has nothing to read or change. The answer on stdout.
func lancerAmeliorer(args []string) int {
	args = cheminsOutil(args)
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor ameliorer KIT FILE")
		return 2
	}
	kit := abspathOS(args[0])
	chargerEnvKit(kit)
	consigne, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	d, err := os.MkdirTemp("", "athanor-ameliorer-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(d)
	claude := trouverClaudePilote(estWindows)
	if claude == "" {
		claude = "claude"
	}
	a := []string{"-p", string(consigne), "--max-turns", "1", "--output-format", "text"}
	if m := os.Getenv("MODEL"); m != "" {
		a = append(a, "--model", m)
	}
	ctx, fin := context.WithTimeout(context.Background(), 240*time.Second)
	defer fin()
	c := commandeContexte(ctx, claude, a...)
	c.Dir = d
	c.Env = append(os.Environ(), "ENABLE_CLAUDEAI_MCP_SERVERS=false")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ctx.Err() != nil {
			return 124
		}
		if c.ProcessState != nil {
			return c.ProcessState.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	return 0
}
