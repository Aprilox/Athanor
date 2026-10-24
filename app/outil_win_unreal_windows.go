//go:build windows

package main

// Athanor.exe reseau-unreal: makes Unreal's MCP reachable from WSL (was system\windows\unreal-network.ps1
// and commands\unreal-network.cmd):
//   1. updates WSL and retries "mirrored" networking (the most direct solution)
//   2. if it still fails: installs a local relay reserved for WSL (administrator rights, once)
// Athanor.exe relais-unreal [--retirer] [--port-relais N] [--port-unreal N]: that relay (was
// system\windows\unreal-relay.ps1): Windows listens on port 8001 and forwards to Unreal (127.0.0.1:8000); the
// firewall accepts this port only from WSL's virtual network adapter: nothing is open to the local network.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("reseau-unreal", "reseau-unreal [--kit DIR]: makes Unreal's MCP reachable from WSL (\"mirrored\" networking, else a local relay reserved for WSL)", lancerReseauUnreal)
	enregistrerCommande("relais-unreal", "relais-unreal [--retirer] [--port-relais N] [--port-unreal N]: the local relay to Unreal's MCP for WSL (administrator rights)", lancerRelaisUnreal)
}

const regleRelaisUnreal = "Athanor - Unreal MCP relay (WSL only)"

func lancerReseauUnreal(args []string) int {
	kit, _ := kitWindows(args)
	e := nouvelleEtapeWin(kit, "")
	distro := lireDistroKit(kit)
	if distro == "" {
		e.alerte("Run Install everything (Setup & health) first.")
		return 1
	}
	cfg := filepath.Join(os.Getenv("USERPROFILE"), ".wslconfig")
	echec := filepath.Join(kit, "system", "mirrored_unavailable.txt")
	relais := filepath.Join(kit, "system", "unreal_relay.txt")
	sortie := func(cmd string) string { return wslSortie("-d", distro, "--exec", "sh", "-c", cmd) }
	reseau := func() bool {
		return strings.Contains(sortie("getent hosts claude.ai >/dev/null && echo RESEAU_OK"), "RESEAU_OK")
	}
	mirrored := func() bool { return strings.Contains(sortie("wslinfo --networking-mode 2>/dev/null"), "mirrored") }
	lignes := func() []string {
		b, err := os.ReadFile(cfg)
		if err != nil {
			return nil
		}
		return lignesReseauUnreal(string(b))
	}
	ecrire := func(l []string) { _ = os.WriteFile(cfg, []byte(texteLignesWSL(l)), 0o644) }

	e.ecrire("97", "== 1. WSL update")
	if _, err := executerEleve("wsl.exe", []string{"--update"}, false); err != nil {
		e.alerte("WSL update not run: " + err.Error())
	}
	for i, l := range lignesFichier(wslSortie("--version")) {
		if i >= 2 {
			break
		}
		e.ligne(l)
	}

	e.ligne("")
	e.ecrire("97", `== 2. New attempt at "mirrored" networking`)
	ecrire(avecMirroredLignes(lignes()))
	wslArret()
	if mirrored() && reseau() {
		e.ok(`"mirrored" networking active: WSL reaches Unreal directly on 127.0.0.1:8000.`)
		_ = os.Remove(echec)
		if fichierRegulierPilote(relais) { // relay no longer needed
			_, _ = relancerEleveJournal(e, []string{"relais-unreal", "--retirer"})
			_ = os.Remove(relais)
		}
		return 0
	}
	e.alerte(`"mirrored" mode still fails: back to normal networking.`)
	ecrire(lignes())
	_ = os.WriteFile(echec, []byte(time.Now().Format("2006-01-02 15:04")+"\r\n"), 0o644)
	wslArret()
	if !reseau() {
		e.alerte("WSL has no Internet any more: check " + cfg + " (no networkingMode line), then wsl --shutdown.")
		return 1
	}

	e.ligne("")
	e.ecrire("97", "== 3. Local relay reserved for WSL (administrator rights, once)")
	_ = commande("wsl.exe", "-d", distro, "--exec", "true").Run() // the WSL network adapter must exist
	_, _ = relancerEleveJournal(e, []string{"relais-unreal"})
	out, _ := sansFenetreWin(commande("netsh.exe", "interface", "portproxy", "show", "v4tov4")).CombinedOutput()
	if strings.Contains(string(out), "8001") {
		_ = os.WriteFile(relais, []byte("8001\r\n"), 0o644)
		e.ok("Relay installed: the pilot will use http://<Windows address as seen from WSL>:8001/mcp.")
	} else {
		e.alerte("Relay not installed (administrator rights refused?).")
	}
	return 0
}

func lancerRelaisUnreal(args []string) int {
	e, args := sortieJournal(args)
	portRelais, portUnreal, retirer := 8001, 8000, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--retirer":
			retirer = true
		case "--port-relais", "--port-unreal":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 && n < 65536 {
					if args[i] == "--port-relais" {
						portRelais = n
					} else {
						portUnreal = n
					}
				}
				i++
			}
		}
	}
	if !estAdministrateur() {
		code, err := relancerEleveJournal(e, append([]string{"relais-unreal"}, args...))
		if err != nil {
			e.alerte("Administrator rights refused: " + err.Error())
			return 1
		}
		return code
	}
	netsh := func(a ...string) ([]byte, error) {
		return sansFenetreWin(commande("netsh.exe", a...)).CombinedOutput()
	}
	_, _ = netsh("interface", "portproxy", "delete", "v4tov4", "listenaddress=0.0.0.0", "listenport="+strconv.Itoa(portRelais))
	_, _ = netsh("advfirewall", "firewall", "delete", "rule", "name="+regleRelaisUnreal)
	// rule of the previous name of the tool (Claude Autopilote)
	_, _ = netsh("advfirewall", "firewall", "delete", "rule", "name=Claude Autopilote - relais MCP Unreal (WSL uniquement)")
	if retirer {
		e.ligne("Relay removed.")
		return 0
	}
	carte := ""
	if l, err := net.Interfaces(); err == nil {
		for _, i := range l {
			if strings.HasPrefix(i.Name, "vEthernet (WSL") {
				carte = i.Name
				break
			}
		}
	}
	if carte == "" { // a hidden adapter: PowerShell's network module lists those too
		out, _ := powershellCache(`Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'vEthernet (WSL*' } | Select-Object -First 1 -ExpandProperty Name`)
		carte = strings.TrimSpace(out)
	}
	if carte == "" {
		e.alerte("WSL network adapter not found: run WSL once (wsl), then try again.")
		return 1
	}
	_, _ = netsh("interface", "portproxy", "add", "v4tov4", "listenaddress=0.0.0.0", "listenport="+strconv.Itoa(portRelais),
		"connectaddress=127.0.0.1", "connectport="+strconv.Itoa(portUnreal))
	// the rule bound to WSL's adapter: only PowerShell's firewall module takes an interface by name
	if out, err := powershellCache(`New-NetFirewallRule -DisplayName $env:ATHANOR_REGLE -Direction Inbound -Action Allow -Protocol TCP -LocalPort ([int]$env:ATHANOR_PORT) -InterfaceAlias $env:ATHANOR_CARTE -RemoteAddress LocalSubnet -Profile Any | Out-Null`,
		"ATHANOR_REGLE="+regleRelaisUnreal, "ATHANOR_PORT="+strconv.Itoa(portRelais), "ATHANOR_CARTE="+carte); err != nil {
		e.alerte("Firewall rule not created: " + strings.TrimSpace(out))
	}
	e.ligne(fmt.Sprintf("Relay active: port %d (adapter '%s' only) -> Unreal 127.0.0.1:%d", portRelais, carte, portUnreal))
	return 0
}
