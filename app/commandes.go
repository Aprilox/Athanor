package main

// The sub-commands of the single executable: "athanor COMMAND ARGUMENTS…" (Athanor.exe on Windows).
// The kit moves to Go step by step (docs: "Athanor en Go — plan de migration"): each script that becomes
// Go becomes one of these, and the scripts that remain call it. Without a sub-command (or with the app's
// own options, --kit=…), the executable is the application.
//
// The older spellings stay valid: --isole, --eveil, --retour (the gateways and the pilots of older kits).

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// versionAthanor: kept equal to ProductVersion of winres/athanor.rc (TestVersionAlignee).
const versionAthanor = "2.46.3"

type sousCommande struct {
	aide  string
	lance func(args []string) int
}

var sousCommandes map[string]sousCommande

// enregistrerCommande: a sub-command declared in its own file (func init), whatever the order of the files.
func enregistrerCommande(nom, aide string, lance func(args []string) int) {
	if sousCommandes == nil {
		sousCommandes = map[string]sousCommande{}
	}
	sousCommandes[nom] = sousCommande{aide, lance}
}

func init() {
	for n, c := range map[string]sousCommande{
		"equipe":            {"equipe importer KIT PROJECT | json KIT PROJECT AGENTS_DIR [--ouvrier] | fiche KIT PROJECT AGENTS_DIR: the team of sub-agents (the pilot)", lancerEquipe},
		"extensions":        {"extensions preparer KIT PROJECT PRIVATE_DIR [...] | mcp KIT PROJECT FILE [...] | tester KIT PROJECT ID | connexion KIT ID FILE: the skills, plugins and MCP servers of a session", lancerExtensions},
		"screenshots":       {"screenshots [folder] | --accept: Claude's screenshots against the reference, sheets to look at (athanor screenshots in the workshop)", lancerAtelier("screenshots")},
		"map":               {"map: writes .athanor/MAP.md, the files, functions and outlines of $PROJECT", lancerAtelier("map")},
		"archive":           {"archive: old checked milestones and old log entries of $PROJECT into .athanor/archives/", lancerAtelier("archive")},
		"todo":              {"todo add|edit|list|ok|missing|resolved|wait ...: the tasks for the user (Claude's command)", lancerTodo},
		"etat-taches":       {"etat-taches PROJECT: how many tasks wait for the user or for Claude (the pilot)", lancerEtatTaches},
		"plan":              {"plan lister PLAN | chevauchement PLAN LINE [RUNNING...] | documentation | cocher PLAN ID: the plan's milestones for the parallel mode (the pilot)", lancerPlan},
		"solutions":         {"solutions ranger FILE WHAT | fusionner MAIN COPY: .athanor/SOLUTIONS.md kept short, a worker's merged (the pilot)", lancerSolutions},
		"reglages-ouvrier":  {"reglages-ouvrier SETTINGS GIT: a parallel worker's settings, the project's .git opened in its sandbox (the pilot)", lancerReglagesOuvrier},
		"pont":              {"pont KIT [PROJECT]: the bridge between Claude's sandbox and the PC tools (the pilot, WSL and Linux)", lancerPont},
		"roblox-sync":       {"roblox-sync serve PROJECT PIDFILE | payload PROJECT: a Rojo project's scripts, live in the open Roblox Studio (the pilot)", lancerRobloxSync},
		"reparer":           {"reparer --diff | --recompiler [--tests] [--forcer] | --annuler [KIT]: rebuilds this executable from the kit's app/ (after a repair), checked, the previous one kept", lancerReparer},
		"github":            {"github etat|pousser|publie|tester|enregistrer|auto|branche|delier|suivre|identite|regler-identite|identite-pilote … | scanner | atelier | crochet: a project's GitHub link, on every system (the former system/github.sh)", lancerGitHub},
		"passerelle":        {"passerelle adb|blender|install-windows|pc-exec|roblox-studio|tool|unreal-cmd|unreal-editor|unreal-uat|windows-var ARGS: the PC side of the gateways of system/bin (WSL; the bridge and the pilot)", lancerPasserelle},
		"mcp-cage":          {"mcp-cage NAME TOOLS [--rw DIR]... [--ro DIR]... [--env NAME VALUE]... -- COMMAND...: a local MCP server in its cage (bubblewrap; Seatbelt on macOS)", lancerMCPCage},
		"mcp-windows":       {"mcp-windows [--env NAME VALUE]... -- PROGRAM...: an MCP server that is a Windows program, through WSL", lancerMCPWindows},
		"mcp-windows-garde": {"mcp-windows-garde PIDFILE SYS32 LAUNCHER PARENT: mcp-windows' watchdog", lancerGardeMCPWindows},
		"ecran":             {"ecran lancer PROGRAM... | capture PROJECT: Claude's screen on the Linux side", lancerEcran},
		"pilote":            {"pilote [PROJECT]: the autonomous work loop, parallel workers included (started by demarrer)", lancerPilote},
		"reprise":           {"reprise PROJECT PRIVATE_DIR: at the pilot's start, what a stop in the middle or another PC left (git locks repaired, the rest in .athanor/RESUME.md for Claude)", lancerReprise},
		"mcp-ouvrir":        {"mcp-ouvrir KIT PROJECT OPENINGS.json: the programs the cycle's MCP servers drive, opened before it (the pilot)", lancerMCPOuvrir},
		"detecter":          {"detecter [KIT]: the PC tools and the toolchain, into system/pc_tools.env and PROJECT/.athanor/PC_TOOLS.md (the pilot)", lancerDetecter},
		"hook":              {"hook fichiers ALLOWED... [--lecture=DIR...] | hook bash: the PreToolUse hooks of Claude's tools (stdin: the call)", lancerHook},
		"reglages":          {"reglages KIT OUTPUT.json: the Claude Code settings of a cycle (PROJECT in the environment; the pilot)", lancerReglages},
		"chat":              {"chat settings PROJECT OUT: the Claude Code settings of the app's chat", lancerChatOutil},
		"relais":            {"relais contexte LOG | sur LOG | resume LOG [HANDOFF]: taking over a milestone when a session's context is full (the pilot)", lancerRelais},
		"cout":              {"cout apprendre TARIFS LOG | estimer TARIFS LOG [CONFIG]: the prices learned on this PC and the cost of a cycle (the pilot)", lancerCout},
		"quota":             {"quota verifier RULES LOG...: the automatic stops on the subscription's quota (the pilot)", lancerQuota},
		"affichage":         {"affichage JOURNAL FIN: the live display of a session that writes its stream into its log file (native Windows), until FIN exists", lancerAffichage},
		"images":            {"images gel|passee JOURNAL: a session silent right after a large image (its size), the largest image a session got past (the pilots)", lancerImages},
		"configurer":        {"configurer [KIT]: the configuration assistant, a few questions written into the tool's .env", lancerConfigurer},
		"installer":         {"installer [KIT]: the installation of Linux, WSL and macOS, once: Claude Code, the sandbox, RTK, the basic tools", lancerInstaller},
		"version":           {"prints the version of Athanor", func([]string) int { fmt.Println(versionAthanor); return 0 }},
		"aide":              {"lists the sub-commands", func([]string) int { afficherAide(); return 0 }},
		"autotest":          {"autotest [KIT]: checks this executable and the kit (built-in files, scripts, texts, local/ replacements)", lancerAutotest},
		"isole":             {"isole [--detache] [--ici] [--fin-tout] [--pid FILE] -- PROGRAM ARGS: runs a Windows program in a job, on Claude's desktop when that option is on", LancerIsole},
		"eveil":             {"eveil FILE: keeps the PC awake while FILE is touched (a running pilot)", GarderEveille},
		"retour":            {"retour PID: brings the user back from Claude's desktop when the app disappears", GardienRetour},
	} {
		enregistrerCommande(n, c.aide, c.lance)
	}
}

// lancerSousCommande: the exit code and true when args name a sub-command ("isole" or "--isole").
// consolePartagee: the sub-command shares the console of the program that started it (console_windows.go).
var consolePartagee bool

func lancerSousCommande(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	// --fenetre TITLE COMMAND…: an interactive step in a window of its own (the application opens it):
	// the step's own console on Windows, then "Press Enter to close" whatever its outcome
	// --env-connexion: the user's login environment first (a call from the application: wsl.exe --exec and
	// a Finder-started app have a bare PATH); env_connexion_unix.go
	fenetre := ""
	for len(args) > 0 {
		if len(args) >= 3 && args[0] == "--fenetre" {
			fenetre, args = args[1], args[2:]
		} else if args[0] == "--env-connexion" {
			chargerEnvConnexion()
			args = args[1:]
		} else {
			break
		}
	}
	if len(args) == 0 {
		return 0, false
	}
	nom := args[0]
	switch nom {
	case "--isole", "--eveil", "--retour", "--version":
		nom = strings.TrimPrefix(nom, "--")
	case "pilot":
		nom = "pilote"
	case "help", "--help", "-h":
		nom = "aide"
	}
	c, ok := sousCommandes[nom]
	if !ok {
		return 0, false
	}
	if fenetre == "" {
		consolePartagee = partagerConsoleParent()
		return c.lance(args[1:]), true
	}
	ouvrirConsole(fenetre)
	consolePartagee = true
	code := c.lance(args[1:])
	fmt.Fprint(os.Stdout, "\nPress Enter to close")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	return code, true
}

func afficherAide() {
	noms := make([]string, 0, len(sousCommandes))
	for n := range sousCommandes {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	fmt.Fprintf(os.Stdout, "Athanor %s\nUsage: athanor [COMMAND ARGUMENTS…]   (no command: the application)\n"+
		"       athanor tui | gui              the application in the terminal (Linux, macOS) | in its window\n\n", versionAthanor)
	for _, n := range noms {
		fmt.Fprintf(os.Stdout, "  %-9s %s\n", n, sousCommandes[n].aide)
	}
}
