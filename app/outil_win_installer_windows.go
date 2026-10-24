//go:build windows

package main

// Athanor.exe installer-windows: the Windows installation of the WSL mode, once (was commands\install.ps1):
//   1. installs or checks WSL2 (the Linux built into Windows)
//   2. sets WSL networking to "mirrored" to reach Unreal (MCP) on this PC
//   3. runs athanor installer in WSL (Claude Code, sandbox, tools)
//   4. offers administrator rights for Claude's software installations
// Athanor.exe installer-natif: what the native Windows mode needs (was commands\install-natif.ps1).
// Athanor.exe wsl COMMAND ARGS: the kit's athanor in its WSL distribution, in this console (was
// system\windows\wsl-run.ps1, for the .cmd shortcuts).
// Opened by the application in a window of their own (fenetreWindows): they ask and print there.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func init() {
	enregistrerCommande("installer-windows", "installer-windows [--kit DIR]: the Windows installation of the WSL mode (WSL2, its network, then athanor installer in WSL, administrator rights)", lancerInstallerWindows)
	enregistrerCommande("installer-natif", "installer-natif [--kit DIR]: what the native Windows mode needs (winget: Git for Windows, jq, Python, Node.js, GitHub CLI, uv; Claude Code for Windows)", lancerInstallerNatif)
	enregistrerCommande("wsl", "wsl COMMAND ARGS…: the kit's athanor in its WSL distribution, in this console (e.g. Athanor.exe wsl configurer)", lancerWSLKit)
}

func lancerInstallerWindows(args []string) int {
	kit, _ := kitWindows(args)
	e := nouvelleEtapeWin(kit, "install-windows.log")
	defer e.fermer()
	_ = os.Chdir(kit)

	// ---------------------------------------------------------------- 1. WSL2
	e.titre("1. WSL2")
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		e.alerte("wsl.exe not found: Windows 10 (2004+) or 11 required.")
		return 1
	}
	distros := lireDistrosWSL(wslSortie("--list", "--verbose"))
	if len(distros) == 0 {
		e.alerte("No Linux distribution in WSL.")
		if e.ouiNon("Install WSL2 with Ubuntu now? (administrator rights, then PC restart)", true) {
			if _, err := executerEleve("wsl.exe", []string{"--install", "-d", "Ubuntu"}, false); err != nil {
				e.alerte("WSL installation not started: " + err.Error())
			}
			e.ligne("")
			e.info("Then: 1) restart the PC  2) open 'Ubuntu' in the Start menu and create your Linux user  3) run Install everything again")
		}
		return 0
	}
	choix := choisirDistroWSL(distros)
	if len(distros) > 1 {
		e.ligne("Distributions found:")
		for i, d := range distros {
			e.ligne(fmt.Sprintf("  %d) %s (WSL%d)", i+1, d.Nom, d.Version))
		}
		r := strings.TrimSpace(e.lire("Which one to use? [" + distros[choix].Nom + "]"))
		if n, err := strconv.Atoi(r); err == nil && n >= 1 && n <= len(distros) {
			choix = n - 1
		}
	}
	distro := distros[choix].Nom
	if distros[choix].Version != 2 {
		e.alerte(distro + " runs on WSL1: the sandbox requires WSL2.")
		if e.ouiNon("Convert "+distro+" to WSL2? (a few minutes)", true) {
			wslVisible("--set-version", distro, "2")
		} else {
			return 1
		}
	}
	_ = os.MkdirAll(filepath.Join(kit, "system"), 0o755)
	if err := os.WriteFile(filepath.Join(kit, "system", "distro.txt"), []byte(distro), 0o644); err != nil {
		e.alerte("Cannot write system\\distro.txt: " + err.Error())
		return 1
	}
	e.ok("Distribution: " + distro + " (WSL2)")

	// ---------------------------------------------------------------- 2. mirrored networking
	e.titre("2. WSL networking (to drive Unreal through its MCP)")
	cfg := filepath.Join(os.Getenv("USERPROFILE"), ".wslconfig")
	echecMirrored := filepath.Join(kit, "system", "mirrored_unavailable.txt")
	texteB, _ := os.ReadFile(cfg)
	texte := string(texteB)
	build := windows.RtlGetVersion().BuildNumber
	reseau := func(cmd string) string { return wslSortie("-d", distro, "--exec", "sh", "-c", cmd) }
	noterEchec := func() {
		_ = os.WriteFile(echecMirrored, []byte(time.Now().Format("2006-01-02 15:04")+"\r\n"), 0o644)
	}
	switch {
	case reMirroredActif.MatchString(texte):
		e.ok(`Networking already in "mirrored" mode`)
		if !strings.Contains(reseau("getent hosts claude.ai >/dev/null && echo RESEAU_OK"), "RESEAU_OK") {
			e.alerte(`"mirrored" mode is set but WSL has no network: back to normal networking.`)
			_ = os.WriteFile(cfg, []byte(sansNetworkingMode(texte)), 0o644)
			noterEchec()
			wslArret()
		}
	case fichierRegulierPilote(echecMirrored):
		e.alerte(`"mirrored" mode already tried without success on this PC: normal networking kept (Unreal MCP not directly reachable).`)
	case build < 22621:
		e.alerte(fmt.Sprintf("Windows 11 22H2 or newer is required for 'mirrored' mode (current build %d). The Unreal MCP will not be reachable from WSL.", build))
	case e.ouiNon("Switch WSL networking to 'mirrored' mode? (WSL sees this PC's programs on 127.0.0.1; modifies "+cfg+")", true):
		sauve := false
		if texte != "" {
			if os.WriteFile(cfg+".sauvegarde", texteB, 0o644) == nil {
				sauve = true
			}
		}
		if err := os.WriteFile(cfg, []byte(avecMirrored(texte)), 0o644); err != nil {
			e.alerte("Cannot write " + cfg + ": " + err.Error())
			return 1
		}
		if sauve {
			e.ok("Setting written (previous version: " + cfg + ".sauvegarde)")
		} else {
			e.ok("Setting written")
		}
		if e.ouiNon("Restart WSL now to apply it? (Docker Desktop will restart too)", true) {
			wslArret()
			if strings.Contains(reseau("getent hosts claude.ai >/dev/null && wslinfo --networking-mode 2>/dev/null"), "mirrored") {
				e.ok(`WSL restarted, "mirrored" networking active`)
			} else {
				e.alerte("'mirrored' mode does not work on this PC (WSL ends up without network). Back to normal networking.")
				b, _ := os.ReadFile(cfg)
				_ = os.WriteFile(cfg, []byte(sansNetworkingMode(string(b))), 0o644)
				noterEchec()
				wslArret()
				e.alerte("Unreal (MCP) will not be directly reachable from WSL; everything else works.")
			}
		} else {
			e.alerte("The setting will take effect at the next WSL restart (wsl --shutdown).")
		}
	}

	// Check: WSL must have Internet access for what follows.
	if !strings.Contains(reseau("getent hosts claude.ai >/dev/null && echo RESEAU_OK"), "RESEAU_OK") {
		e.alerte("WSL has no Internet access. Check " + cfg + " (remove the networkingMode line), then: wsl --shutdown, and run Install everything again.")
		return 1
	}
	e.ok("WSL has Internet access")

	// ---------------------------------------------------------------- 3. athanor installer in WSL
	e.titre("3. Installation in " + distro + " (your Linux password will be asked)")
	code := wslVisible("-d", distro, "--cd", kit, "--exec", "./athanor", "--env-connexion", "installer")
	e.ligne(`(details of the Linux installation: logs\install-wsl.log)`)
	if code != 0 {
		e.alerte(fmt.Sprintf("The Linux installation reported a problem (code %d).", code))
	}

	// ---------------------------------------------------------------- 4. administrator rights
	e.titre("4. Windows software installations by Claude")
	e.ligne("Claude installs what it needs on its own (SDKs, engines...). Some software requires administrator rights.")
	e.ligne("If you accept, Windows asks for your consent ONCE; afterwards Claude installs without bothering you,")
	e.gris(`only software from the official winget catalogue, each one recorded in tools\INSTALLS.md.`)
	e.gris(`The same authorisation configures the firewall (Unreal and the game, local network only): no more "Allow access" window.`)
	if e.ouiNon("Allow Claude to install software with administrator rights?", true) {
		_, err := relancerEleveJournal(e, []string{"autoriser-admin", "--eleve", "--kit", kit, "--utilisateur", utilisateurWindows()})
		switch {
		case err == erreurConsentementRefuse:
			e.alerte("Authorisation refused: Claude will only install what does not require administrator rights.")
		case tacheAdminExiste():
			e.ok(`Allowed (to remove: Setup & health → Administrator rights, or delete the task "Athanor - installations")`)
		default:
			e.alerte("Authorisation not created.")
		}
	} else if tacheAdminExiste() {
		if e.ouiNon("An authorisation already exists. Remove it?", false) {
			_, _ = relancerEleveJournal(e, []string{"autoriser-admin", "--eleve", "--retirer", "--kit", kit})
		}
	}

	// ---------------------------------------------------------------- 5. next
	// Starting with Windows and relaunching the pilots are set in the app (Options). Old startup task: removed.
	if sansFenetreWin(commande("schtasks.exe", "/query", "/tn", "Athanor WSL")).Run() == nil {
		_ = sansFenetreWin(commande("schtasks.exe", "/Delete", "/F", "/TN", "Athanor WSL")).Run()
		e.ok("Old task 'Athanor WSL' removed (replaced by the app option).")
	}
	e.ligne("")
	e.info("Installation complete. Go back to the Athanor app:")
	e.ligne("  1. Installation & health -> Check everything (and the token: Repair on the Token line)")
	e.ligne("  2. Test the sandbox")
	e.ligne("  3. + New project, then Start")
	return 0
}

// utilisateurWindows: DOMAIN\USER of this session (the administrator task runs in it).
func utilisateurWindows() string {
	return os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
}

// tacheAdminExiste: the scheduled task "Athanor - installations" is there.
func tacheAdminExiste() bool {
	return sansFenetreWin(commande("schtasks.exe", "/query", "/tn", nomTacheAdmin)).Run() == nil
}

func lancerInstallerNatif(args []string) int {
	kit, _ := kitWindows(args)
	e := nouvelleEtapeWin(kit, "install-natif.log")
	defer e.fermer()
	winget, err := exec.LookPath("winget.exe")
	if err != nil {
		e.alerte(`winget is missing: install "App Installer" from the Microsoft Store, then run this again.`)
		return 1
	}
	paquet := func(id, nom string) {
		out, _ := commande(winget, "list", "--id", id, "-e", "--accept-source-agreements").Output()
		if strings.Contains(strings.ToLower(string(out)), strings.ToLower(id)) {
			e.vert("ok  " + nom)
			return
		}
		e.info("Installing " + nom + "...")
		c := commande(winget, "install", "--id", id, "-e", "--silent", "--accept-source-agreements", "--accept-package-agreements")
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			code := -1
			if c.ProcessState != nil {
				code = c.ProcessState.ExitCode()
			}
			e.alerte(fmt.Sprintf("  %s could not be installed (code %d).", nom, code))
		}
	}
	paquet("Git.Git", "Git for Windows (Git Bash)")
	paquet("jqlang.jq", "jq")
	paquet("Python.Python.3.12", "Python")
	paquet("OpenJS.NodeJS.LTS", "Node.js")
	paquet("GitHub.cli", "GitHub CLI (to link a project to GitHub)")
	paquet("astral-sh.uv", "uv (MCP servers written in Python, such as Blender's)")

	claude := filepath.Join(os.Getenv("USERPROFILE"), ".local", "bin", "claude.exe")
	if fichierRegulierPilote(claude) {
		// already there: brought up to date (an old version does not know the latest models)
		e.info("Updating Claude Code for Windows...")
		c := commande(claude, "update")
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		_ = c.Run()
		v, _ := commande(claude, "--version").Output()
		e.vert("ok  Claude Code for Windows " + strings.TrimSpace(string(v)))
	} else {
		// Claude Code's official installer for Windows (a PowerShell script published by Anthropic)
		e.info("Installing Claude Code for Windows...")
		c := commande("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "irm https://claude.ai/install.ps1 | iex")
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			e.alerte("  Claude Code could not be installed: " + err.Error())
		}
	}
	e.ligne("")
	e.vert("Done. Close this window, then in Athanor: Setup & health -> Check again.")
	e.ligne("(A program just installed is found once Athanor is restarted, if a check still says it is missing.)")
	return 0
}

func lancerWSLKit(args []string) int {
	kit, reste := kitWindows(args)
	e := &etapeWin{sortie: os.Stdout, couleur: activerCouleursConsole()}
	distro := lireDistroKit(kit)
	if distro == "" {
		e.alerte("Run Install everything (Setup & health) first.")
		return 1
	}
	if len(reste) < 1 {
		e.ligne("Usage: Athanor.exe wsl COMMAND [ARGUMENTS...]   (e.g. wsl configurer)")
		return 2
	}
	code := wslVisible(append([]string{"-d", distro, "--cd", kit, "--exec", "./athanor", "--env-connexion"}, reste...)...)
	if code != 0 {
		liste := wslSortie("--list", "--quiet")
		if !strings.Contains(liste, distro) {
			e.alerte("Distribution '" + distro + "' not found. Distributions present:\r\n" + liste + "\r\nRun Install everything again.")
		}
	}
	return code
}
