package main

// athanor detecter [KIT]: the PC tools (Unreal, Blender, Android SDK, Java, Roblox Studio, winget,
// Docker) and the toolchain, written into system/pc_tools.env (for the gateways) and
// PROJECT/.athanor/PC_TOOLS.md (for Claude). Replaces system/detect-tools.sh, same files; PROJECT,
// UE_DIR, BLENDER_EXE, ANDROID_SDK_DIR, ANDROID_JDK_DIR, UNREAL_MCP and ROBLOX_MCP from the environment.
// Paths are written as the scripts know them: /mnt/c/… in WSL, /c/… in native Windows (Git Bash).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// cheminGitBash: a Windows path as Git Bash writes it (C:\Users\x -> /c/Users/x); other paths unchanged.
func cheminGitBash(p string) string {
	if len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) {
		reste := strings.ReplaceAll(p[2:], `\`, "/")
		return "/" + strings.ToLower(p[:1]) + "/" + strings.TrimPrefix(reste, "/")
	}
	return p
}

// fichiersPy-like tests through the shell's path form (cheminOutil turns /c/… into C:/… in native Windows)
func estF(p string) bool {
	st, err := os.Stat(cheminOutil(p))
	return err == nil && st.Mode().IsRegular()
}
func estD(p string) bool { st, err := os.Stat(cheminOutil(p)); return err == nil && st.IsDir() }
func estE(p string) bool { _, err := os.Stat(cheminOutil(p)); return err == nil }

// globPrefixe: DIR/PREFIX* as bash expands it (names that do not start with a dot), sorted.
func globPrefixe(dossier, prefixe string) []string {
	es, err := os.ReadDir(cheminOutil(dossier))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		if n := e.Name(); strings.HasPrefix(n, prefixe) && !strings.HasPrefix(n, ".") {
			out = append(out, dossier+"/"+n)
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------- sort -V (gnulib's filevercmp)

func estChiffreAscii(c byte) bool { return c >= '0' && c <= '9' }
func estLettreAscii(c byte) bool  { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }

func longueurPrefixe(s string) int {
	n, prefixe := len(s), 0
	for i := 0; ; {
		if i == n {
			return prefixe
		}
		i++
		prefixe = i
		for i+1 < n && s[i] == '.' && (estLettreAscii(s[i+1]) || s[i+1] == '~') {
			for i += 2; i < n && (estLettreAscii(s[i]) || estChiffreAscii(s[i]) || s[i] == '~'); i++ {
			}
		}
	}
}

func ordreVersion(s string, pos int) int {
	if pos == len(s) {
		return -1
	}
	c := s[pos]
	switch {
	case estChiffreAscii(c):
		return 0
	case estLettreAscii(c):
		return int(c)
	case c == '~':
		return -2
	}
	return int(c) + 256
}

func verrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		premiere := 0
		for (i < len(a) && !estChiffreAscii(a[i])) || (j < len(b) && !estChiffreAscii(b[j])) {
			x, y := ordreVersion(a, i), ordreVersion(b, j)
			if x != y {
				return x - y
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		for i < len(a) && j < len(b) && estChiffreAscii(a[i]) && estChiffreAscii(b[j]) {
			if premiere == 0 {
				premiere = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && estChiffreAscii(a[i]) {
			return 1
		}
		if j < len(b) && estChiffreAscii(b[j]) {
			return -1
		}
		if premiere != 0 {
			return premiere
		}
	}
	return 0
}

func filevercmp(a, b string) int {
	switch {
	case a == "":
		if b == "" {
			return 0
		}
		return -1
	case b == "":
		return 1
	}
	if a[0] == '.' {
		if b[0] != '.' {
			return -1
		}
		for _, special := range []string{".", ".."} {
			if a == special {
				if b == special {
					return 0
				}
				return -1
			}
			if b == special {
				return 1
			}
		}
	} else if b[0] == '.' {
		return 1
	}
	pa, pb := longueurPrefixe(a), longueurPrefixe(b)
	r := verrevcmp(a[:pa], b[:pb])
	if r != 0 || (pa == len(a) && pb == len(b)) {
		return r
	}
	return verrevcmp(a, b)
}

// plusRecent: the existing candidate with the highest version in the name that carries it (UE_5.10,
// …/Blender 4.2/blender.exe), whatever drive it is on; "" when none.
func plusRecent(c []string) string {
	var l []string
	for _, x := range c {
		if estE(x) {
			l = append(l, x)
		}
	}
	if len(l) == 0 {
		return ""
	}
	cle := func(p string) string { // the name that carries the version
		if strings.HasSuffix(p, ".exe") {
			p = path.Dir(p)
		}
		return path.Base(p)
	}
	sort.SliceStable(l, func(i, j int) bool {
		if r := filevercmp(cle(l[i]), cle(l[j])); r != 0 {
			return r < 0
		}
		if a, b := cle(l[i]), cle(l[j]); a != b {
			return a < b
		}
		return l[i] < l[j]
	})
	return l[len(l)-1]
}

// trierVersions: LC_ALL=C sort -V.
func trierVersions(l []string) {
	sort.SliceStable(l, func(i, j int) bool {
		if r := filevercmp(l[i], l[j]); r != 0 {
			return r < 0
		}
		return l[i] < l[j]
	})
}

// ---------------------------------------------------------------------------- the toolchain

var reAnsi = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
var reNumeroVersion = regexp.MustCompile(`[0-9]+\.[0-9]+`)

// versionOutil: "- NAME: first line with a version" (or "installed", or "not installed"), as the script.
func versionOutil(kit, nom string, cmd ...string) string {
	natif := runtime.GOOS == "windows"
	var c *exec.Cmd
	ctx, fin := context.WithTimeout(context.Background(), 8*time.Second)
	defer fin()
	shim := cheminOutil(kit + "/system/natif/" + cmd[0])
	if natif && estFichier(shim) { // the pilot's Git Bash finds these shims first (python3, jq…)
		c = exec.CommandContext(ctx, "bash", append([]string{shim}, cmd[1:]...)...)
	} else {
		if _, err := exec.LookPath(cmd[0]); err != nil {
			return "- " + nom + ": not installed"
		}
		c = exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	}
	cacherFenetre(c)
	out, err := c.CombinedOutput()
	if err != nil {
		return "- " + nom + ": not installed"
	}
	v := ""
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
		l = reAnsi.ReplaceAllString(l, "")
		if strings.HasPrefix(l, "Picked up") || !reNumeroVersion.MatchString(l) {
			continue
		}
		if len(l) > 90 { // cut -c1-90: bytes
			l = l[:90]
		}
		v = l
		break
	}
	if v == "" {
		v = "installed"
	}
	return "- " + nom + ": " + v
}

// nomsDans: the names of the folder's entries kept by garder, sorted, at most n, each followed by a space.
func nomsDans(dossier string, n int, garder func(os.DirEntry) bool) string {
	es, err := os.ReadDir(cheminOutil(dossier))
	if err != nil {
		return ""
	}
	var noms []string
	for _, e := range es {
		if garder(e) {
			noms = append(noms, e.Name())
		}
	}
	sort.Strings(noms)
	if len(noms) > n {
		noms = noms[:n]
	}
	if len(noms) == 0 {
		return ""
	}
	return strings.Join(noms, " ") + " "
}

// avecDelai: runs the command (its output when sortie), stopped after d.
func avecDelai(d time.Duration, dossier string, sortie bool, nom string, args ...string) ([]byte, error) {
	ctx, fin := context.WithTimeout(context.Background(), d)
	defer fin()
	c := exec.CommandContext(ctx, nom, args...)
	c.Dir = dossier
	cacherFenetre(c)
	if sortie {
		return c.Output()
	}
	return nil, c.Run()
}

func detecter(kitArg string) error {
	natif := runtime.GOOS == "windows"
	kit := cheminGitBash(strings.TrimRight(kitArg, `/\`))
	systeme := kit + "/system"
	windows := natif
	if !windows {
		_, err := exec.LookPath("cmd.exe")
		windows = err == nil
	}
	profil := ""
	if natif {
		if u := os.Getenv("USERPROFILE"); u != "" {
			profil = cheminGitBash(u)
		}
	} else if windows {
		if out, err := avecDelai(60*time.Second, "/mnt/c", true, "cmd.exe", "/c", "echo %USERPROFILE%"); err == nil {
			if p := strings.TrimRight(strings.ReplaceAll(string(out), "\r", ""), "\n"); p != "" {
				if w, err := exec.Command("wslpath", "-u", p).Output(); err == nil {
					profil = strings.TrimRight(string(w), "\n")
				}
			}
		}
	}
	// Windows drives (WSL: /mnt/c…; Git Bash: /c…): software can be installed on any of them
	var lecteurs []string
	if windows {
		base := "/mnt/"
		if natif {
			base = "/"
		}
		for l := 'a'; l <= 'z'; l++ {
			m := base + string(l)
			if estD(m+"/Program Files") || estD(m+"/Epic Games") || estD(m+"/Windows") {
				lecteurs = append(lecteurs, m)
			}
		}
	}
	ue, blender, sdk, jdk := os.Getenv("UE_DIR"), os.Getenv("BLENDER_EXE"), os.Getenv("ANDROID_SDK_DIR"), os.Getenv("ANDROID_JDK_DIR")
	mac := runtime.GOOS == "darwin" || os.Getenv("ATHANOR_TEST_MAC") != ""
	var lm logicielsDuMac
	if mac {
		lm = detecterMac(racineMac(), os.Getenv("HOME"))
		ue, blender, sdk, jdk = firstNonEmpty(ue, lm.ue), firstNonEmpty(blender, lm.blender), firstNonEmpty(sdk, lm.sdk), firstNonEmpty(jdk, lm.jdk)
	}
	if ue == "" {
		var c []string
		for _, m := range lecteurs {
			for _, d := range append(globPrefixe(m+"/Program Files/Epic Games", "UE_"), globPrefixe(m+"/Epic Games", "UE_")...) {
				if estF(d + "/Engine/Binaries/Win64/UnrealEditor.exe") {
					c = append(c, d)
				}
			}
		}
		ue = plusRecent(c)
	}
	if blender == "" {
		var c []string
		for _, m := range lecteurs {
			for _, d := range globPrefixe(m+"/Program Files/Blender Foundation", "Blender") {
				if estF(d + "/blender.exe") {
					c = append(c, d+"/blender.exe")
				}
			}
		}
		blender = plusRecent(c)
	}
	if sdk == "" {
		p := profil
		if p == "" {
			p = "/nulle-part"
		}
		for _, d := range []string{kit + "/tools/windows/android-sdk", p + "/AppData/Local/Android/Sdk"} {
			if estD(d+"/platform-tools") || estD(d+"/cmdline-tools") {
				sdk = d
				break
			}
		}
	}
	if jdk == "" {
		var c []string
		for _, m := range lecteurs {
			c = append(c, m+"/Program Files/Android/Android Studio/jbr")
		}
		c = append(c, globPrefixe(kit+"/tools/windows", "jdk")...)
		for _, d := range c {
			if estF(d + "/bin/java.exe") {
				jdk = d
				break
			}
		}
	}
	roblox := lm.roblox
	if profil != "" && !mac {
		type v struct {
			p string
			t time.Time
		}
		var l []v
		for _, d := range globPrefixe(profil+"/AppData/Local/Roblox/Versions", "") {
			f := d + "/RobloxStudioBeta.exe"
			if st, err := os.Stat(cheminOutil(f)); err == nil {
				l = append(l, v{f, st.ModTime()})
			}
		}
		sort.SliceStable(l, func(i, j int) bool {
			if !l[i].t.Equal(l[j].t) {
				return l[i].t.After(l[j].t)
			}
			return l[i].p < l[j].p
		})
		if len(l) > 0 {
			roblox = l[0].p
		}
	}
	winget := false
	if natif {
		_, err := exec.LookPath("winget.exe")
		winget = err == nil
	} else if windows {
		_, err := avecDelai(30*time.Second, "/mnt/c", false, "powershell.exe", "-NoProfile", "-Command", "Get-Command winget")
		winget = err == nil
	}
	env := fmt.Sprintf("UE_DIR='%s'\nBLENDER_EXE='%s'\nANDROID_SDK_DIR='%s'\nANDROID_JDK_DIR='%s'\nWIN_PROFIL='%s'\n", ue, blender, sdk, jdk, profil)
	if err := os.WriteFile(cheminOutil(systeme+"/pc_tools.env"), []byte(env), 0o644); err != nil {
		return err
	}
	projet := cheminGitBash(os.Getenv("PROJECT"))
	if projet == "" {
		projet = kit + "/projet"
	}
	_ = os.MkdirAll(cheminOutil(projet+"/.athanor"), 0o777)
	_ = os.MkdirAll(cheminOutil(kit+"/tools/windows"), 0o777)
	var b strings.Builder
	l := func(s string) { b.WriteString(s + "\n") }
	l("# PC tools available (written by the pilot before each cycle, do not edit)")
	l("")
	if mac {
		lignesMac(l, ue, blender, sdk, jdk, roblox, lm.xcode, lm.godot)
	} else {
		if ue != "" {
			l("- **Unreal Engine**: " + path.Base(ue))
			l("  - `unreal-editor [--wait-mcp] Game.uproject`: opens the editor (window) without blocking")
			l("  - `unreal-cmd Game.uproject -ExecCmds=\"...\" -unattended -nullrhi`: commandlets and tests without a window")
			l("  - `unreal-uat BuildCookRun -project=Game.uproject -platform=Win64 -clientconfig=Shipping -build -cook -stage -pak -archive -archivedirectory=deliverable`: Windows game (`-platform=Android -cookflavor=ASTC` for Android)")
			if os.Getenv("UNREAL_MCP") == "1" {
				l("  - **Unreal MCP active**: `mcp__unreal__*` tools when the editor is open (the pilot opens it before each cycle if a .uproject exists)")
			}
		} else {
			l("- Unreal Engine: not detected")
		}
		if roblox != "" {
			l("- **Roblox Studio**: `roblox-studio [--wait] PLACE.rbxl` opens it on a place of the project, without blocking (nothing if a Studio is already open)")
			if os.Getenv("ROBLOX_MCP") == "1" {
				l("  - **Roblox Studio MCP active**: `mcp__roblox-studio__*` tools on the open Studio (the pilot opens it before each cycle; Rojo project: `.athanor/studio/SYNC.luau` updates its scripts live, `roblox-studio --rebuild` rebuilds the place; a capture that times out: `roblox-studio --screen` says whether the Windows session is locked)")
			}
		}
		if blender != "" {
			l("- **Blender**: `blender -b -P script.py` (headless), `blender --open FILE.blend` (its window, for the Blender MCP)")
		} else {
			l("- Blender: not detected")
		}
		if sdk != "" {
			l("- **Android SDK**: `" + sdk + "`; `adb devices`, `adb install -r game.apk` (phone plugged in over USB)")
		} else {
			l("- Android SDK: missing (install it as a portable copy in `$TOOLS/windows/android-sdk`, see PC_TOOLS_GUIDE.md)")
		}
		if jdk != "" {
			l("- **Java for Android**: `" + jdk + "`")
		} else {
			l("- Java (JDK) for Android: missing")
		}
		if winget {
			l("- **winget**: `install-windows ID` installs a Windows program (`install-windows --search NAME` to find the ID)")
		} else {
			l("- winget: unavailable")
		}
	}
	if natif {
		l("- **Native Windows**: you run on Windows itself (Git Bash): Windows programs run directly from your shell (`program.exe ...`, `cmd //c ...`, `powershell -Command ...`); portable ones go in `$TOOLS/windows`")
	} else if windows {
		l("- **Portable Windows programs**: install them in `$TOOLS/windows`, run them with `pc-exec PATH.exe ...`; Windows variables with `windows-var NAME PATH`")
	}
	if estF(kit+"/logs/bureau-isole") && natif {
		l("- **Claude's desktop**: the Windows programs you open never show on the user's screen (a Windows desktop of their own) and never take the focus. Check what they show with screenshots (the program's own, or a PowerShell capture of its window).")
	} else if estF(kit + "/logs/bureau-isole") {
		l("- **Claude's desktop**: the programs you open never show on the user's screen (Windows programs through the gateways: a Windows desktop of their own; Linux programs: a private X screen, `DISPLAY` is set). Check what they show with screenshots: the program's own, or `import -window root shot.png` for the Linux screen.")
	}
	docker := false
	if _, err := exec.LookPath("docker"); err == nil {
		_, err := avecDelai(20*time.Second, "", false, "docker", "info")
		docker = err == nil
	}
	if docker {
		l("- **Docker**: `tool IMAGE COMMAND...` (disposable container, project mounted on /work)")
	} else if natif || mac {
		l("- Docker: not available (Docker Desktop stopped)")
	} else {
		l("- Docker: not available (Docker Desktop stopped, or WSL integration not enabled)")
	}
	// what is already installed, checked here once: the agents never spend turns probing it again
	l("")
	l("## Toolchain (checked by the pilot when it started)")
	l("")
	l("Installed on the system and usable from your sandbox (`$TOOLS/bin` comes last in your PATH). Do not check these again: install what is missing into `$TOOLS` within a milestone.")
	l("")
	// never run from tools/: what is there was written from the sandbox, and this check runs outside it
	for _, v := range [][]string{{"Node", "node", "--version"}, {"npm", "npm", "--version"}, {"Python", "python3", "--version"},
		{"pip", "python3", "-m", "pip", "--version"}, {"PHP", "php", "--version"}, {"Composer", "composer", "--version"},
		{"Git", "git", "--version"}, {"Java", "java", "-version"}, {"Go", "go", "version"}, {"Rust", "rustc", "--version"},
		{".NET", "dotnet", "--version"}, {"PostgreSQL client", "psql", "--version"}, {"SQLite", "sqlite3", "--version"}} {
		l(versionOutil(kit, v[0], v[1:]...))
	}
	// (Windows has a convert.exe of its own, which converts disks: ImageMagick is "magick" there)
	if natif {
		l(versionOutil(kit, "ImageMagick", "magick", "-version"))
	} else {
		l(versionOutil(kit, "ImageMagick", "convert", "-version"))
	}
	l(versionOutil(kit, "ffmpeg", "ffmpeg", "-version"))
	exclus := map[string]bool{"cache": true, "bin": true, "mcp": true, "windows": true, "plugin-data": true}
	if s := nomsDans(kit+"/tools", 30, func(e os.DirEntry) bool { return e.IsDir() && !exclus[e.Name()] }); s != "" {
		l("- Portable installs already in `$TOOLS`: " + s)
	}
	if s := nomsDans(kit+"/tools/bin", 40, func(e os.DirEntry) bool { return e.Type().IsRegular() || e.Type()&os.ModeSymlink != 0 }); s != "" {
		l("- Commands already in `$TOOLS/bin` (not checked here): " + s)
	}
	return os.WriteFile(cheminOutil(projet+"/.athanor/PC_TOOLS.md"), []byte(b.String()), 0o666)
}

func lancerDetecter(args []string) int {
	kit := ""
	if len(args) > 0 {
		kit = args[0]
	} else {
		kit = devinerKit()
	}
	if kit == "" {
		fmt.Fprintln(os.Stderr, "usage: athanor detecter KIT")
		return 2
	}
	if err := detecter(abspathOS(cheminOutil(kit))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
