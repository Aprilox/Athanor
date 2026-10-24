package main

// athanor reparation INSTRUCTION_FILE: the Claude session Setup & health starts to repair the kit (a check
// that fails, a free request). It works in the kit folder OUTSIDE the sandbox, without sudo, for 30 minutes
// at most; its stream-json output goes to the application and into logs/reparations.jsonl.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func lancerReparation(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: athanor reparation INSTRUCTION_FILE")
		return 2
	}
	kit := kitSante(nil)
	fichier := args[0]
	consigne, err := os.ReadFile(fichier)
	if st, e := os.Stat(fichier); e != nil || !st.Mode().IsRegular() || err != nil {
		fmt.Println("Instruction file NOT FOUND: " + fichier)
		return 1
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Println(`Claude Code NOT FOUND: repair the "Claude Code" line first.`)
		return 1
	}
	_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	modele := os.Getenv("REPAIR_MODEL")
	if modele == "" {
		modele = os.Getenv("MODEL")
	}
	a := []string{"-p", string(consigne), "--permission-mode", "bypassPermissions", "--permission-prompts", "none",
		"--output-format", "stream-json", "--verbose", "--max-turns", "80"}
	if modele != "" {
		a = append(a, "--model", modele)
	}
	// no MCP server of the user's in a repair
	if aide, _ := sortieDe(30*time.Second, claude, "--help"); strings.Contains(aide, "--strict-mcp-config") {
		a = append(a, "--strict-mcp-config")
	}
	if s := os.Getenv("FALLBACK_MODEL"); s != "" && s != modele {
		a = append(a, "--fallback-model", s)
	}
	// no claude.ai connectors (Shopify, Gmail…), nothing in the background
	_ = os.Setenv("CLAUDE_CODE_DISABLE_BACKGROUND_TASKS", "1")
	_ = os.Setenv("ENABLE_CLAUDEAI_MCP_SERVERS", "false")
	precision := ""
	if modele != "" {
		precision = " (" + modele + ")"
	}
	fmt.Printf("[reparer] Claude session%s: %s\n", precision, filepath.Base(fichier))

	var sortie io.Writer = os.Stdout
	if j, err := os.OpenFile(filepath.Join(kit, "logs", "reparations.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		defer j.Close()
		sortie = io.MultiWriter(os.Stdout, j)
	}
	c := commande(claude, a...)
	c.Dir = kit
	c.Stdout, c.Stderr = sortie, sortie
	groupeSante(c)
	code := 0
	if err := c.Start(); err != nil {
		fmt.Println("Claude Code could not start: " + err.Error())
		return 1
	}
	fini := make(chan error, 1)
	go func() { fini <- c.Wait() }()
	select {
	case err := <-fini:
		code = codeSortieSante(err, c)
	case <-time.After(30 * time.Minute):
		tuerGroupeSante(c)
		<-fini
		code = 124
	}
	if st, err := os.Stat(filepath.Join(kit, "logs", "TODO_FOR_YOU.md")); err == nil && st.Mode().IsRegular() {
		fmt.Println()
		fmt.Println("[reparer] Claude left steps for you to do: logs/TODO_FOR_YOU.md")
	}
	return code
}
