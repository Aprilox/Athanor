//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func objetEssai(t *testing.T, s string) objetJSON {
	t.Helper()
	var o objetJSON
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// The catalog's Roblox Studio as each system runs it: Windows keeps mcp.bat, macOS and Linux find Studio's
// server on the computer (nothing written in).
func TestCatalogueRobloxParSysteme(t *testing.T) {
	trouver := func() (ConfigMCP, map[string]string, string) {
		for _, m := range lireCatalogue().MCP {
			if m.ID == "roblox-studio" {
				r := map[string]string{}
				for _, x := range m.Reglages {
					r[x.Nom] = x.Defaut
				}
				return m.Config, r, m.Prerequis
			}
		}
		t.Fatal("no roblox-studio in the catalog")
		return ConfigMCP{}, nil, ""
	}
	systemeEssai(t, "windows")
	if c, r, _ := trouver(); c.Type != "windows" || r["ROBLOX_STUDIO_MCP"] != `%LOCALAPPDATA%\Roblox\mcp.bat` {
		t.Fatalf("windows: %+v %v", c, r)
	}
	for _, sys := range []string{"macos", "linux"} {
		systemeEssai(t, sys)
		c, r, p := trouver()
		if c.Type != "hote" || c.Command != "{ATHANOR}" || strings.Join(c.Args, " ") != "passerelle roblox-mcp ${ROBLOX_STUDIO_MCP}" || r["ROBLOX_STUDIO_MCP"] != "" || strings.Contains(p, "(Windows)") {
			t.Fatalf("%s: %+v %v %q", sys, c, r, p)
		}
	}
}

// A server installed with Windows' configuration, on macOS: this system's variant, the Windows default
// left by the user dropped, another value kept; a server with no variant left out, said why.
func TestAdapterAuSysteme(t *testing.T) {
	systemeEssai(t, "macos")
	kit := t.TempDir()
	def := objetEssai(t, `{"nom":"Roblox Studio","catalogue":"roblox-studio","config":{"type":"windows","command":"cmd.exe","args":["/c","${ROBLOX_STUDIO_MCP}"]},"reglages":{"ROBLOX_STUDIO_MCP":"%LOCALAPPDATA%\\Roblox\\mcp.bat"}}`)
	sec := objetEssai(t, `{"ROBLOX_STUDIO_MCP":"%LOCALAPPDATA%\\Roblox\\mcp.bat"}`)
	s, raison := serveurMCP(kit, "roblox-studio", def, sec, "/p/projet", "")
	if s == nil {
		t.Fatal(raison)
	}
	var v map[string]any
	b, _ := json.Marshal(s)
	_ = json.Unmarshal(b, &v)
	if v["type"] != "stdio" || v["command"] != filepath.Join(kit, "athanor") || strings.Join(strSlice(v["args"]), " ") != "passerelle roblox-mcp " {
		t.Fatalf("macos: %s", b)
	}
	// a path of the user's: kept
	s, _ = serveurMCP(kit, "roblox-studio", def, objetEssai(t, `{"ROBLOX_STUDIO_MCP":"/Applications/Autre.app/Contents/MacOS/StudioMCP"}`), "/p/projet", "")
	if b, _ := json.Marshal(s); !strings.Contains(string(b), "Autre.app") {
		t.Fatalf("the user's path: %s", b)
	}
	// the library's entry stays as it was
	if c := def.objet("config"); c.brut("type") != "windows" {
		t.Fatal("the library's entry changed")
	}
	// Blender has no macOS variant yet: left out, with the reason
	bl := objetEssai(t, `{"catalogue":"blender","config":{"type":"windows","command":"cmd.exe","args":["/c","x"]}}`)
	if s, raison := serveurMCP(kit, "blender", bl, objetJSON{}, "/p/projet", ""); s != nil || !strings.Contains(raison, "a Windows program") || !strings.Contains(raison, "not on this system") {
		t.Fatalf("blender: %v %q", s, raison)
	}
	// "hote" is the catalog's only
	if s, raison := serveurMCP(kit, "x", objetEssai(t, `{"config":{"type":"hote","command":"/bin/sh"}}`), objetJSON{}, "/p/projet", ""); s != nil || !strings.Contains(raison, "only from the verified catalog") {
		t.Fatalf("hote pasted: %v %q", s, raison)
	}
	// the app's page shows the same
	x := installeDuSysteme(MCPInstalle{Catalogue: "roblox-studio", Config: ConfigMCP{Type: "windows", Command: "cmd.exe"}, Reglages: map[string]string{"ROBLOX_STUDIO_MCP": `%LOCALAPPDATA%\Roblox\mcp.bat`}}, lireCatalogue())
	if x.Config.Type != "hote" || x.Reglages["ROBLOX_STUDIO_MCP"] != "" {
		t.Fatalf("page: %+v", x)
	}
	// Windows: unchanged
	systemeEssai(t, "windows")
	if d, _, r := adapterAuSysteme(kit, def, sec); r != "" || d.objet("config").brut("type") != "windows" {
		t.Fatal("windows: unchanged")
	}
}

func strSlice(v any) []string {
	var out []string
	for _, x := range liste(v) {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// Studio's MCP server found on the computer: macOS in Applications, Linux in Vinegar's prefix (native or
// Flatpak, its own Wine), a setting first.
func TestCommandeRobloxMCP(t *testing.T) {
	racine, maison := t.TempDir(), t.TempDir()
	t.Setenv("ATHANOR_RACINE_MAC", racine)
	t.Setenv("ATHANOR_MAISON_ROBLOX", maison)
	if c, r := commandeRobloxMCP("macos", ""); c != nil || !strings.Contains(r, "not found") {
		t.Fatalf("macos without Studio: %v %q", c, r)
	}
	mcp := filepath.Join(maison, "Applications", "RobloxStudio.app", "Contents", "MacOS", "StudioMCP")
	ecrireEssai(t, mcp, "")
	if c, _ := commandeRobloxMCP("macos", ""); strings.Join(c, " ") != mcp {
		t.Fatalf("macos, ~/Applications: %v", c)
	}
	if c, _ := commandeRobloxMCP("macos", "/autre/StudioMCP"); strings.Join(c, " ") != "/autre/StudioMCP" {
		t.Fatalf("the setting: %v", c)
	}
	// Linux, nothing; then the Flatpak, Studio not yet started (no mcp.bat); then with it
	if c, r := commandeRobloxMCP("linux", ""); c != nil || !strings.Contains(r, "Vinegar") {
		t.Fatalf("linux without Vinegar: %q", r)
	}
	donnees := filepath.Join(maison, ".var", "app", idFlatpakVinegar, "data", "vinegar")
	_ = os.MkdirAll(filepath.Join(donnees, "prefixes", "studio", "drive_c"), 0o755)
	if c, r := commandeRobloxMCP("linux", ""); c != nil || !strings.Contains(r, "mcp.bat not found") {
		t.Fatalf("no mcp.bat: %q", r)
	}
	ecrireEssai(t, filepath.Join(donnees, "prefixes", "studio", "drive_c", "users", "alex", "AppData", "Local", "Roblox", "mcp.bat"), "")
	ecrireEssai(t, filepath.Join(donnees, "kombucha", "bin", "wine"), "")
	c, _ := commandeRobloxMCP("linux", "")
	attendu := "flatpak run --env=WINEPREFIX=" + filepath.Join(donnees, "prefixes", "studio") + " --env=WINEDEBUG=-all --command=" + filepath.Join(donnees, "kombucha", "bin", "wine") + " " + idFlatpakVinegar + ` cmd /c C:\users\alex\AppData\Local\Roblox\mcp.bat`
	if strings.Join(c, " ") != attendu {
		t.Fatalf("flatpak:\n%s\n%s", strings.Join(c, " "), attendu)
	}
	// native Vinegar (it comes first), with the Wine its settings name
	natif := filepath.Join(maison, ".local", "share", "vinegar")
	ecrireEssai(t, filepath.Join(natif, "prefixes", "studio", "drive_c", "users", "alex", "AppData", "Local", "Roblox", "mcp.bat"), "")
	ecrireEssai(t, filepath.Join(maison, ".config", "vinegar", "config.toml"), "[studio]\nwineroot = \""+filepath.Join(maison, "wine-ge")+"\"\n")
	ecrireEssai(t, filepath.Join(maison, "wine-ge", "bin", "wine64"), "")
	if c, _ := commandeRobloxMCP("linux", ""); strings.Join(c, " ") != filepath.Join(maison, "wine-ge", "bin", "wine64")+` cmd /c C:\users\alex\AppData\Local\Roblox\mcp.bat` {
		t.Fatalf("native: %v", c)
	}
}

// Studio's processes outside Windows (Linux: Wine's RobloxStudioBeta.exe), and those a rebuild closes:
// the ones on a place of the project only.
func TestStudiosDuProjet(t *testing.T) {
	proc := t.TempDir()
	t.Setenv("ATHANOR_PROC", proc)
	ligne := func(pid, cmd string) {
		ecrireEssai(t, filepath.Join(proc, pid, "cmdline"), strings.ReplaceAll(cmd, " ", "\x00"))
	}
	ligne("101", `C:\x\RobloxStudioBeta.exe --task EditFile --localPlaceFile Z:\home\alex\jeu\.athanor\studio\place.rbxl`)
	ligne("102", `C:\x\RobloxStudioBeta.exe --task EditFile --localPlaceFile Z:\home\alex\autre\place.rbxl`)
	ligne("103", `/usr/bin/bash -c ls`)
	ligne("abc", `RobloxStudioBeta.exe`)
	if n := len(processusStudioHorsWindows("linux")); n != 2 {
		t.Fatalf("studios: %d", n)
	}
	if l := studiosDuProjet("linux", "/home/alex/jeu"); len(l) != 1 || l[0] != 101 {
		t.Fatalf("the project's: %v", l)
	}
	if cheminZ("/home/alex/a b.rbxl") != `Z:\home\alex\a b.rbxl` {
		t.Fatal(cheminZ("/home/alex/a b.rbxl"))
	}
}

// roblox-studio on Linux: Vinegar opens Studio on the project's place (as Wine names it); a place
// outside the project refused; --screen says whether Studio runs.
func TestRobloxStudioLinux(t *testing.T) {
	systemeEssai(t, "linux")
	maison, proc, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("ATHANOR_MAISON_ROBLOX", maison)
	t.Setenv("ATHANOR_PROC", proc)
	_ = os.MkdirAll(filepath.Join(maison, ".var", "app", idFlatpakVinegar, "data", "vinegar", "prefixes", "studio"), 0o755)
	vu := filepath.Join(bin, "vu")
	ecrireEssai(t, filepath.Join(bin, "flatpak"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > '"+vu+"'\n")
	_ = os.Chmod(filepath.Join(bin, "flatpak"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	projet := t.TempDir()
	projet, _ = filepath.EvalSymlinks(projet)
	ecrireEssai(t, filepath.Join(projet, "jeu.rbxl"), "x")
	var so, se bytes.Buffer
	p := &passerelle{kit: t.TempDir(), projet: projet, sortie: &so, erreur: &se}
	appeler := func(args ...string) (code int) {
		defer func() {
			if r := recover(); r != nil {
				code = int(r.(sortiePasserelle))
			}
		}()
		return p.robloxStudio(args)
	}
	if c := appeler("--screen"); c != 0 || !strings.Contains(so.String(), "not running") {
		t.Fatalf("--screen: %d %s", c, so.String())
	}
	if c := appeler(filepath.Join(projet, "jeu.rbxl")); c != 0 {
		t.Fatalf("open: %d %s %s", c, so.String(), se.String())
	}
	for i := 0; i < 50 && !fichierP(vu); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if b, _ := os.ReadFile(vu); strings.TrimSpace(string(b)) != "run "+idFlatpakVinegar+" run --task EditFile --localPlaceFile "+cheminZ(filepath.Join(projet, "jeu.rbxl")) {
		t.Fatalf("vinegar: %q", b)
	}
	dehors := filepath.Join(t.TempDir(), "x.rbxl")
	ecrireEssai(t, dehors, "x")
	if c := appeler(dehors); c != 2 {
		t.Fatalf("outside the project: %d", c)
	}
}
