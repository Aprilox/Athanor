package main

// The sub-commands of the single executable: "athanor COMMAND ARGUMENTS…" (Athanor.exe on Windows).
// The kit moves to Go step by step (docs: "Athanor en Go — plan de migration"): each script that becomes
// Go becomes one of these, and the scripts that remain call it. Without a sub-command (or with the app's
// own options, --kit=…), the executable is the application.
//
// The older spellings stay valid: --isole, --eveil, --retour (the gateways and the pilots of older kits).

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// versionAthanor: kept equal to ProductVersion of winres/athanor.rc (TestVersionAlignee).
const versionAthanor = "2.24.7"

type sousCommande struct {
	aide  string
	lance func(args []string) int
}

var sousCommandes map[string]sousCommande

func init() {
	sousCommandes = map[string]sousCommande{
		"relais":   {"relais contexte LOG | sur LOG | resume LOG [HANDOFF]: taking over a milestone when a session's context is full (the pilot)", lancerRelais},
		"cout":     {"cout apprendre TARIFS LOG | estimer TARIFS LOG [CONFIG]: the prices learned on this PC and the cost of a cycle (the pilot)", lancerCout},
		"quota":    {"quota verifier RULES LOG...: the automatic stops on the subscription's quota (the pilot)", lancerQuota},
		"version":  {"prints the version of Athanor", func([]string) int { fmt.Println(versionAthanor); return 0 }},
		"aide":     {"lists the sub-commands", func([]string) int { afficherAide(); return 0 }},
		"autotest": {"autotest [KIT]: checks this executable and the kit (built-in files, scripts, texts, local/ replacements)", lancerAutotest},
		"isole":    {"isole [--detache] [--ici] [--fin-tout] [--pid FILE] -- PROGRAM ARGS: runs a Windows program in a job, on Claude's desktop when that option is on", LancerIsole},
		"eveil":    {"eveil FILE: keeps the PC awake while FILE is touched (a running pilot)", GarderEveille},
		"retour":   {"retour PID: brings the user back from Claude's desktop when the app disappears", GardienRetour},
	}
}

// lancerSousCommande: the exit code and true when args name a sub-command ("isole" or "--isole").
func lancerSousCommande(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	nom := args[0]
	switch nom {
	case "--isole", "--eveil", "--retour", "--version":
		nom = strings.TrimPrefix(nom, "--")
	case "help", "--help", "-h":
		nom = "aide"
	}
	c, ok := sousCommandes[nom]
	if !ok {
		return 0, false
	}
	return c.lance(args[1:]), true
}

func afficherAide() {
	noms := make([]string, 0, len(sousCommandes))
	for n := range sousCommandes {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	fmt.Fprintf(os.Stdout, "Athanor %s\nUsage: athanor [COMMAND ARGUMENTS…]   (no command: the application)\n\n", versionAthanor)
	for _, n := range noms {
		fmt.Fprintf(os.Stdout, "  %-9s %s\n", n, sousCommandes[n].aide)
	}
}
