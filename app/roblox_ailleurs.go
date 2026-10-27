//go:build !windows

package main

// Roblox Studio outside Windows: on macOS the application itself (RobloxStudio.app, its MCP server
// StudioMCP inside it); on Linux through Vinegar (https://vinegarhq.org), which runs the Windows Studio in
// a Wine prefix of its own, installed natively or as the Flatpak org.vinegarhq.Vinegar. Nothing is
// written in: every path is found on the computer.
//   athanor passerelle roblox-mcp [COMMAND]   Studio's MCP server on stdio (the catalog's "hote" variant);
//                                             COMMAND: the setting ROBLOX_STUDIO_MCP, empty = found here
//   roblox-studio [--wait|--rebuild|--screen] [PLACE]   as on Windows (outil_passerelle.go)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// idFlatpakVinegar: Vinegar's Flatpak.
const idFlatpakVinegar = "org.vinegarhq.Vinegar"

// vinegarTrouve: where Vinegar keeps Studio, and how to run its Wine.
type vinegarTrouve struct {
	donnees string // .../vinegar (prefixes/, kombucha/, versions/)
	config  string // .../vinegar/config.toml's folder
	flatpak bool
}

// maisonRoblox: the user's home (a fake one in the tests).
func maisonRoblox() string {
	if h := os.Getenv("ATHANOR_MAISON_ROBLOX"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// trouverVinegar: Vinegar's data on this computer (its Studio prefix present), Flatpak or native.
func trouverVinegar() (vinegarTrouve, bool) {
	h := maisonRoblox()
	var l []vinegarTrouve
	if x := os.Getenv("XDG_DATA_HOME"); x != "" && os.Getenv("ATHANOR_MAISON_ROBLOX") == "" {
		l = append(l, vinegarTrouve{donnees: filepath.Join(x, "vinegar"), config: filepath.Join(firstNonEmpty(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(h, ".config")), "vinegar")})
	}
	l = append(l,
		vinegarTrouve{donnees: filepath.Join(h, ".local", "share", "vinegar"), config: filepath.Join(h, ".config", "vinegar")},
		vinegarTrouve{donnees: filepath.Join(h, ".var", "app", idFlatpakVinegar, "data", "vinegar"), config: filepath.Join(h, ".var", "app", idFlatpakVinegar, "config", "vinegar"), flatpak: true})
	for _, v := range l {
		if estD(filepath.Join(v.donnees, "prefixes", "studio")) {
			return v, true
		}
	}
	return vinegarTrouve{}, false
}

func (v vinegarTrouve) prefixe() string { return filepath.Join(v.donnees, "prefixes", "studio") }

var reWineRoot = regexp.MustCompile(`(?m)^\s*wineroot\s*=\s*"([^"]+)"`)

// wine: Vinegar's Wine (the "wineroot" of its settings, else the build it downloads, kombucha).
func (v vinegarTrouve) wine() string {
	racine := filepath.Join(v.donnees, "kombucha")
	if b, err := os.ReadFile(filepath.Join(v.config, "config.toml")); err == nil {
		if m := reWineRoot.FindSubmatch(b); m != nil {
			racine = string(m[1])
		}
	}
	for _, n := range []string{"wine", "wine64"} {
		if f := filepath.Join(racine, "bin", n); estF(f) {
			return f
		}
	}
	return filepath.Join(racine, "bin", "wine")
}

// mcpBat: Studio's mcp.bat in Vinegar's prefix (Studio writes it in the Wine user's Roblox folder), as
// its Linux path and as Wine sees it.
func (v vinegarTrouve) mcpBat() (string, string) {
	l, _ := filepath.Glob(filepath.Join(v.prefixe(), "drive_c", "users", "*", "AppData", "Local", "Roblox", "mcp.bat"))
	sort.Strings(l)
	if len(l) == 0 {
		return "", ""
	}
	rel, _ := filepath.Rel(filepath.Join(v.prefixe(), "drive_c"), l[0])
	return l[0], `C:\` + strings.ReplaceAll(rel, "/", `\`)
}

// commandeWine: a command in Vinegar's Wine prefix (through its Flatpak when it is one).
func (v vinegarTrouve) commandeWine(args ...string) []string {
	if v.flatpak {
		return append([]string{"flatpak", "run", "--env=WINEPREFIX=" + v.prefixe(), "--env=WINEDEBUG=-all", "--command=" + v.wine(), idFlatpakVinegar}, args...)
	}
	return append([]string{v.wine()}, args...)
}

// lancerVinegar: Vinegar's own command line (it starts Studio with these arguments).
func (v vinegarTrouve) lancerVinegar(args ...string) []string {
	if v.flatpak {
		return append([]string{"flatpak", "run", idFlatpakVinegar}, args...)
	}
	if c, err := exec.LookPath("vinegar"); err == nil {
		return append([]string{c}, args...)
	}
	return nil
}

// studioMCPMac: StudioMCP inside RobloxStudio.app (/Applications or ~/Applications).
func studioMCPMac() string {
	for _, d := range []string{filepath.Join(racineMac(), "Applications"), filepath.Join(maisonRoblox(), "Applications")} {
		if f := filepath.Join(d, "RobloxStudio.app", "Contents", "MacOS", "StudioMCP"); estF(f) {
			return f
		}
	}
	return ""
}

// commandeRobloxMCP: Studio's MCP server for this computer: the setting when given, else found here.
func commandeRobloxMCP(sys, reglage string) ([]string, string) {
	if r := strings.TrimSpace(reglage); r != "" {
		if strings.HasSuffix(strings.ToLower(r), ".bat") && sys == "linux" {
			v, ok := trouverVinegar()
			if !ok {
				return nil, "a .bat runs in Vinegar's Wine, and Vinegar was not found"
			}
			return v.commandeWine("cmd", "/c", r), ""
		}
		return []string{r}, ""
	}
	switch sys {
	case "macos":
		if f := studioMCPMac(); f != "" {
			return []string{f}, ""
		}
		return nil, "Roblox Studio not found (RobloxStudio.app in Applications)"
	case "linux":
		v, ok := trouverVinegar()
		if !ok {
			return nil, "Roblox Studio not found: on Linux it runs through Vinegar (flatpak install flathub " + idFlatpakVinegar + "), started once"
		}
		_, w := v.mcpBat()
		if w == "" {
			return nil, "Studio's mcp.bat not found in Vinegar's prefix: open Studio once, then Assistant > … > Manage MCP Servers, Enable Studio as MCP server"
		}
		return v.commandeWine("cmd", "/c", w), ""
	}
	return nil, "Windows: the catalog's configuration runs mcp.bat itself"
}

// robloxMCP: athanor passerelle roblox-mcp [COMMAND]: becomes Studio's MCP server (its stdin and stdout).
func (p *passerelle) robloxMCP(args []string) int {
	c, raison := commandeRobloxMCP(systemeMCP(), argGH(args, 0))
	if c == nil {
		fmt.Fprintln(p.erreur, "Roblox Studio MCP: "+raison)
		return 1
	}
	chemin, err := exec.LookPath(c[0])
	if err != nil {
		fmt.Fprintln(p.erreur, "Roblox Studio MCP: "+c[0]+" not found")
		return 127
	}
	env := append(os.Environ(), "WINEDEBUG=-all")
	if v, ok := trouverVinegar(); ok && !v.flatpak {
		env = append(env, "WINEPREFIX="+v.prefixe())
	}
	if err := syscall.Exec(chemin, c, env); err != nil { // the server itself: nothing between it and Claude
		fmt.Fprintln(p.erreur, "Roblox Studio MCP: "+err.Error())
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------- Studio's window

// processusStudioHorsWindows: the PIDs of Roblox Studio (macOS: RobloxStudio; Linux: the Wine process of
// RobloxStudioBeta.exe) and their command lines.
func processusStudioHorsWindows(sys string) map[int]string {
	out := map[int]string{}
	racine := firstNonEmpty(os.Getenv("ATHANOR_PROC"), "/proc")
	if sys == "macos" {
		b, err := commande("pgrep", "-x", "RobloxStudio").Output()
		if err != nil {
			return out
		}
		for _, l := range strings.Fields(string(b)) {
			if n, err := strconv.Atoi(l); err == nil {
				out[n] = ""
			}
		}
		return out
	}
	entrees, _ := os.ReadDir(racine)
	for _, e := range entrees {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(racine, e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		l := strings.ReplaceAll(string(b), "\x00", " ")
		if strings.Contains(strings.ToLower(l), "robloxstudiobeta.exe") {
			out[n] = l
		}
	}
	return out
}

// cheminZ: a Linux path as Wine sees it (Z: is the root).
func cheminZ(p string) string { return `Z:` + strings.ReplaceAll(p, "/", `\`) }

// robloxStudioHorsWindows: roblox-studio on macOS and Linux.
func (p *passerelle) robloxStudioHorsWindows(sys string, args []string) int {
	ouvert := func() bool { return len(processusStudioHorsWindows(sys)) > 0 }
	attendre, reconstruire := false, false
	switch argGH(args, 0) {
	case "--screen":
		if ouvert() {
			fmt.Fprintln(p.sortie, "Roblox Studio running: it can draw.")
		} else {
			fmt.Fprintln(p.sortie, "Roblox Studio is not running (roblox-studio --wait opens it).")
		}
		return 0
	case "--wait":
		attendre, args = true, args[1:]
	case "--rebuild":
		reconstruire, attendre, args = true, true, args[1:]
	}
	place := argGH(args, 0)
	rojoF := p.projetRojo()
	if reconstruire {
		if rojoF == "" {
			p.refus(2, "No Rojo project (default.project.json) here: --rebuild needs one.")
		}
		place = p.placeStudio(rojoF)
		if n := p.fermerStudiosHorsWindows(sys); n > 0 {
			fmt.Fprintf(p.sortie, "Closed %d Studio window(s) of the project.\n", n)
			time.Sleep(2 * time.Second)
		}
		os.Remove(place + ".lock")
		if p.construireRojo(rojoF, place) != 0 {
			p.refus(1, "rojo build FAILED: the place is not rebuilt.")
		}
		fmt.Fprintf(p.sortie, "Place rebuilt: %s\n", strings.TrimPrefix(place, p.projet+"/"))
	} else if place == "" && rojoF != "" {
		place = p.placeStudio(rojoF)
		if !ouvert() && !fichierP(place) && p.construireRojo(rojoF, place) != 0 {
			fmt.Fprintln(p.erreur, "rojo build FAILED: Studio opens without the place.")
			place = ""
		}
	}
	if !reconstruire && ouvert() {
		fmt.Fprintln(p.sortie, "Roblox Studio is already open.")
		return 0
	}
	if place != "" {
		if !fichierP(place) {
			p.refus(2, "Place not found: %s", place)
		}
		if b := minuscules(place); !strings.HasSuffix(b, ".rbxl") && !strings.HasSuffix(b, ".rbxlx") {
			p.refus(2, "Refused: a .rbxl or .rbxlx place only.")
		}
		place = realpathP(place)
		if !prefixeP(place, realpathP(p.projet)+"/") {
			p.refus(2, "Refused: the place must be inside the project (%s).", place)
		}
	}
	var c []string
	switch sys {
	case "macos":
		if studioMCPMac() == "" && !estD(filepath.Join(racineMac(), "Applications", "RobloxStudio.app")) && !estD(filepath.Join(maisonRoblox(), "Applications", "RobloxStudio.app")) {
			p.absent("Roblox Studio")
		}
		c = []string{"open", "-a", "RobloxStudio"}
		if place != "" {
			c = append(c, place)
		}
	default:
		v, ok := trouverVinegar()
		if !ok {
			p.absent("Roblox Studio (through Vinegar)")
		}
		c = v.lancerVinegar("run")
		if c == nil {
			p.absent("vinegar")
		}
		if place != "" {
			c = append(c, "--task", "EditFile", "--localPlaceFile", cheminZ(place))
		}
	}
	cmd := commande(c[0], c[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // Studio outlives this command
	if err := cmd.Start(); err != nil {
		p.refus(1, "Roblox Studio did not start: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	if attendre {
		for i := 0; i < 30 && !ouvert(); i++ {
			time.Sleep(2 * time.Second)
		}
		if !ouvert() {
			p.refus(1, "Roblox Studio did not start (signed in to Roblox? up to date?).")
		}
		if reconstruire {
			fmt.Fprintln(p.sortie, "Roblox Studio started: its MCP server is ready about 30 s later (list_roblox_studios, a new studio_id).")
			return 0
		}
	}
	fmt.Fprintln(p.sortie, "Roblox Studio started.")
	return 0
}

// fermerStudiosHorsWindows: Linux: the Studios opened on a place of the project (their command line
// names it); macOS: Studio itself (a place opened with open leaves no trace in its command line).
func (p *passerelle) fermerStudiosHorsWindows(sys string) int {
	n := 0
	for _, pid := range studiosDuProjet(sys, realpathP(p.projet)) {
		if pr, err := os.FindProcess(pid); err == nil && pr.Signal(syscall.SIGTERM) == nil {
			n++
		}
	}
	return n
}

// studiosDuProjet: the Studios to close for a rebuild of the project's place.
func studiosDuProjet(sys, projet string) []int {
	dossier := strings.ToLower(strings.TrimSuffix(cheminZ(projet), `\`) + `\`)
	var l []int
	for pid, ligne := range processusStudioHorsWindows(sys) {
		if sys == "macos" || strings.Contains(strings.ToLower(strings.ReplaceAll(ligne, "/", `\`)), dossier) {
			l = append(l, pid)
		}
	}
	sort.Ints(l)
	return l
}

// studioRobloxOuvert: a Roblox Studio is running on this computer (the pilot, before opening one).
func studioRobloxOuvert() bool {
	if sys := systemeMCP(); sys != "windows" {
		return len(processusStudioHorsWindows(sys)) > 0
	}
	return processusTourne("RobloxStudioBeta.exe")
}
