//go:build linux

package main

// athanor passerelle TOOL ARGUMENTS...: the PC side of the gateways of system/bin (adb, blender, install-windows,
// pc-exec, roblox-studio, tool, unreal-cmd, unreal-editor, unreal-uat, windows-var) in WSL, run outside
// Claude's sandbox (by the bridge, or by the pilot). The gateway keeps its name on Claude's PATH: inside the
// sandbox it hands the command to the bridge; outside, it runs this, with the same checks, messages and
// exit codes. Native Windows keeps the scripts.
//
// Environment: KIT, PROJECT, TOOLS, and what system/pc_tools.env gives (UE_DIR, BLENDER_EXE,
// ANDROID_SDK_DIR, ANDROID_JDK_DIR, WIN_PROFIL), exported by system/bin/_common.sh.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type passerelle struct {
	kit, projet string
	sortie      io.Writer
	erreur      io.Writer
}

// sortiePasserelle: exits with a code (the gateways' "exit N").
type sortiePasserelle int

func (p *passerelle) env(nom, defaut string) string {
	if v := os.Getenv(nom); v != "" {
		return v
	}
	return defaut
}

func (p *passerelle) absent(nom string) {
	fmt.Fprintf(p.erreur, "%s is not installed on this PC (or not detected). See .athanor/PC_TOOLS.md.\n", nom)
	panic(sortiePasserelle(127))
}

func (p *passerelle) refus(code int, format string, a ...any) {
	fmt.Fprintf(p.erreur, format+"\n", a...)
	panic(sortiePasserelle(code))
}

func existeP(p string) bool      { _, err := os.Stat(p); return err == nil }
func fichierP(p string) bool     { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
func dossierP(p string) bool     { st, err := os.Stat(p); return err == nil && st.IsDir() }
func minuscules(s string) string { return strings.ToLower(s) }

// realpathP: GNU realpath (links resolved; the last part may be missing).
func realpathP(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if r, err := filepath.EvalSymlinks(a); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(a)); err == nil {
		return filepath.Join(r, filepath.Base(a))
	}
	return a
}

// convertirArgs: Linux paths to Windows paths for a .exe program (also "-opt=/path").
func convertirArgs(args []string) []string {
	out := []string{}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "/") && (existeP(a) || existeP(filepath.Dir(a))):
			out = append(out, wslpathW(a))
		case strings.HasPrefix(a, "-") && strings.Contains(a, "=/"):
			cle, val, _ := strings.Cut(a, "=")
			if existeP(val) || existeP(filepath.Dir(val)) {
				out = append(out, cle+"="+wslpathW(val))
			} else {
				out = append(out, a)
			}
		default:
			out = append(out, a)
		}
	}
	return out
}

func codeCommande(err error, c *exec.Cmd) int {
	if err == nil {
		return 0
	}
	if c.ProcessState != nil {
		if ws, ok := c.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return c.ProcessState.ExitCode()
	}
	if errorsIs(err, exec.ErrNotFound) || os.IsNotExist(err) {
		return 127
	}
	return 126
}

func errorsIs(err, cible error) bool {
	for e := err; e != nil; {
		if e == cible {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// lancer: a command with the gateway's standard input and outputs; its exit code.
func (p *passerelle) lancer(prog string, args ...string) int {
	c := exec.Command(prog, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, p.sortie, p.erreur
	return codeCommande(c.Run(), c)
}

// dansC: a Windows command run from /mnt/c (a WSL folder as the current one makes Windows warn).
func dansC(prog string, args ...string) *exec.Cmd {
	c := exec.Command(prog, args...)
	if dossierP("/mnt/c") {
		c.Dir = "/mnt/c"
	}
	c.Env = append(os.Environ(), "MSYS2_ARG_CONV_EXCL=*")
	return c
}

// lancerWindows: _common.sh's lancer_windows: on Claude's desktop when that option is on (through
// Athanor.exe, which also takes a detached program out of the cycle's Windows job); its exit code
// (detached: at once, the program keeps running).
func (p *passerelle) lancerWindows(detache bool, prog string, args ...string) int {
	exe := filepath.Join(p.kit, "Athanor.exe")
	ici := !existeP(filepath.Join(p.kit, "logs", "bureau-isole"))
	if st, err := os.Stat(exe); err == nil && st.Mode()&0o111 != 0 && !ici {
		if !strings.Contains(prog, "/") {
			if l, err := exec.LookPath(prog); err == nil {
				prog = l
			}
		}
		if strings.HasPrefix(prog, "/") {
			prog = wslpathW(prog)
		}
		if detache {
			return p.lancer(exe, append([]string{"--isole", "--detache", "--", prog}, args...)...)
		}
		// A cancelled command (the bridge stops this process group) only stops the Linux side of WSL
		// interop: the Windows process tree is stopped explicitly, from the PID Athanor.exe writes.
		f, err := os.CreateTemp(filepath.Join(p.kit, "logs"), ".isole-")
		if err != nil {
			return 1
		}
		f.Close()
		pidF := f.Name()
		defer os.Remove(pidF)
		c := exec.Command(exe, append([]string{"--isole", "--pid", wslpathW(pidF), "--", prog}, args...)...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, p.sortie, p.erreur
		if err := c.Start(); err != nil {
			return codeCommande(err, c)
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		fini := make(chan error, 1)
		go func() { fini <- c.Wait() }()
		select {
		case err := <-fini:
			signal.Stop(sig)
			return codeCommande(err, c)
		case <-sig:
			if w, _ := os.ReadFile(pidF); regexp.MustCompile(`^[0-9]+$`).Match(bytes.TrimSpace(w)) {
				_ = dansC("taskkill.exe", "/T", "/F", "/PID", string(bytes.TrimSpace(w))).Run()
			}
			_ = c.Process.Kill()
			os.Remove(pidF)
			panic(sortiePasserelle(143))
		}
	}
	if detache {
		c := exec.Command(prog, args...)
		if nul, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
			c.Stdout, c.Stderr = nul, nul
		}
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if c.Start() == nil {
			_ = c.Process.Release()
		}
		return 0
	}
	return p.lancer(prog, args...)
}

// executerAdmin: _common.sh's executer_admin: the administrator task "Athanor - installations" runs it
// (allowed once by the user), no UAC prompt; 127 without that task, 124 after 20 minutes.
func (p *passerelle) executerAdmin(demande string, attente time.Duration) int {
	const tache = "Athanor - installations"
	if dansC("schtasks.exe", "/query", "/tn", tache).Run() != nil {
		return 127
	}
	dossier := filepath.Join(p.kit, "tools", "windows", "admin-requests")
	_ = os.MkdirAll(dossier, 0o755)
	n := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
	_ = os.WriteFile(filepath.Join(dossier, "demande-"+n+".txt"), []byte(demande), 0o644)
	_ = dansC("schtasks.exe", "/run", "/tn", tache).Run()
	res := filepath.Join(dossier, "resultat-"+n+".txt")
	for i := 0; i < 240 && !existeP(res); i++ {
		time.Sleep(attente)
	}
	b, err := os.ReadFile(res)
	if err != nil {
		os.Remove(filepath.Join(dossier, "demande-"+n+".txt"))
		fmt.Fprintln(p.erreur, "No answer from the administrator task after 20 min.")
		return 124
	}
	texte := strings.ReplaceAll(string(b), "\r", "")
	code := ""
	for _, l := range strings.SplitAfter(texte, "\n") {
		if strings.HasPrefix(l, "code=") {
			if code == "" {
				code = strings.TrimSuffix(strings.TrimPrefix(l, "code="), "\n")
			}
			continue
		}
		io.WriteString(p.sortie, l)
	}
	if texte != "" && !strings.HasSuffix(texte, "\n") {
		// grep adds the line end
		if l := texte[strings.LastIndex(texte, "\n")+1:]; !strings.HasPrefix(l, "code=") {
			io.WriteString(p.sortie, "\n")
		}
	}
	os.Remove(res)
	if c, err := strconv.Atoi(code); err == nil {
		return c & 0xff
	}
	return 1
}

// ---------------------------------------------------------------------------- the gateways

func (p *passerelle) adb(args []string) int {
	for _, exe := range []string{p.kit + "/tools/windows/android-sdk/platform-tools/adb.exe", p.env("ANDROID_SDK_DIR", "/nulle-part") + "/platform-tools/adb.exe"} {
		if fichierP(exe) {
			return p.lancer(exe, convertirArgs(args)...)
		}
	}
	p.absent("adb (SDK Android)")
	return 127
}

func (p *passerelle) windowsVar(args []string) int {
	nom, val := argGH(args, 0), argGH(args, 1)
	if !regexp.MustCompile(`^(ANDROID_HOME|ANDROID_SDK_ROOT|ANDROID_NDK_ROOT|NDKROOT|NDK_ROOT|JAVA_HOME)$`).MatchString(nom) {
		p.refus(2, "Refused: allowed variables ANDROID_HOME ANDROID_SDK_ROOT ANDROID_NDK_ROOT NDKROOT NDK_ROOT JAVA_HOME")
	}
	if !dossierP(val) {
		p.refus(2, "Folder not found: %s", val)
	}
	reel := realpathP(val)
	if !strings.HasPrefix(reel, p.kit+"/tools/windows/") && !strings.HasPrefix(reel, p.env("ANDROID_SDK_DIR", "/nulle-part")) &&
		!strings.HasPrefix(reel, p.env("ANDROID_JDK_DIR", "/nulle-part")) {
		p.refus(2, "Refused: the path must be inside $TOOLS/windows or a detected SDK (%s).", reel)
	}
	win := wslpathW(reel)
	c := exec.Command("powershell.exe", "-NoProfile", "-Command", "[Environment]::SetEnvironmentVariable('"+nom+"', '"+win+"', 'User')")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, p.sortie, p.erreur
	if code := codeCommande(c.Run(), c); code != 0 {
		return code
	}
	fmt.Fprintf(p.sortie, "%s=%s (Windows user variable; restart the Unreal editor so it sees it)\n", nom, win)
	return 0
}

func (p *passerelle) pcExec(args []string) int {
	admin := false
	if argGH(args, 0) == "--admin" {
		admin, args = true, args[1:]
	}
	prog := argGH(args, 0)
	if len(args) > 0 {
		args = args[1:]
	}
	if prog == "" || !fichierP(prog) {
		p.refus(2, "Usage: pc-exec PATH/program.exe [arguments...]")
	}
	reel := realpathP(prog)
	if !strings.HasPrefix(reel, p.kit+"/tools/windows/") && !strings.HasPrefix(reel, p.projet+"/") &&
		!strings.HasPrefix(reel, p.env("ANDROID_SDK_DIR", "/nulle-part")+"/") && !strings.HasPrefix(reel, p.env("ANDROID_JDK_DIR", "/nulle-part")+"/") {
		p.refus(2, "Refused: pc-exec only runs programs located in $TOOLS/windows or in the project (%s).", reel)
	}
	conv := convertirArgs(args)
	eleve := func() int {
		ligne := ""
		for _, a := range conv {
			if strings.Contains(a, " ") {
				ligne += " \"" + a + "\""
			} else {
				ligne += " " + a
			}
		}
		c := p.executerAdmin("run: \""+wslpathW(reel)+"\""+ligne, 5*time.Second)
		if c == 127 {
			fmt.Fprintln(p.erreur, "Administrator rights not allowed yet: in the app, Setup & health -> Administrator rights -> Repair (one UAC prompt, once). Note it in TODO_FOR_YOU.md and continue with a portable alternative.")
		}
		return c
	}
	bas := minuscules(reel)
	ext := ""
	for _, e := range []string{".exe", ".msi", ".bat", ".cmd", ".ps1"} {
		if strings.HasSuffix(bas, e) {
			ext = e
		}
	}
	if ext == "" {
		p.refus(2, "Refused: only .exe, .msi, .bat, .cmd or .ps1.")
	}
	if admin {
		return eleve()
	}
	var code int
	switch ext {
	case ".exe":
		code = p.lancerWindows(false, reel, conv...)
	case ".msi":
		code = p.lancer("msiexec.exe", append([]string{"/i", wslpathW(reel), "/qn", "/norestart"}, conv...)...)
	case ".ps1":
		code = p.lancerWindows(false, "powershell.exe", append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", wslpathW(reel)}, conv...)...)
	default:
		code = p.lancerWindows(false, "cmd.exe", append([]string{"/c", wslpathW(reel)}, conv...)...)
	}
	// 740 = ERROR_ELEVATION_REQUIRED (the program's manifest asks for administrator rights); 1603 = MSI needs
	// elevation (seen from WSL as 740 % 256 = 228)
	if code == 740 || code == 228 || (code == 1603 && ext == ".msi") {
		fmt.Fprintln(p.erreur, "(the program requires administrator rights: retrying through the administrator task, without UAC)")
		code = eleve()
	}
	return code
}

func (p *passerelle) tool(args []string) int {
	if _, err := exec.LookPath("docker"); err != nil {
		p.refus(127, "Docker not found: start Docker Desktop (WSL integration enabled).")
	}
	image := argGH(args, 0)
	if len(args) > 0 {
		args = args[1:]
	}
	if image == "" || strings.HasPrefix(image, "-") || len(args) == 0 {
		p.refus(2, "Usage: tool IMAGE COMMAND...   (no Docker options: the container only sees the project)")
	}
	ici, _ := os.Getwd()
	rel := ""
	if strings.HasPrefix(ici+"/", p.projet+"/") {
		rel = strings.TrimPrefix(ici, p.projet)
	}
	return p.lancer("docker", append([]string{"run", "--rm", "-i", "-v", p.projet + ":/work", "-w", "/work" + rel, "-e", "HOME=/tmp", image}, args...)...)
}

func (p *passerelle) unreal(outil string, args []string) int {
	ue := os.Getenv("UE_DIR")
	switch outil {
	case "unreal-cmd":
		exe := ue + "/Engine/Binaries/Win64/UnrealEditor-Cmd.exe"
		if !fichierP(exe) {
			p.absent("Unreal Engine")
		}
		return p.lancerWindows(false, exe, convertirArgs(args)...)
	case "unreal-uat":
		bat := ue + "/Engine/Build/BatchFiles/RunUAT.bat"
		if !fichierP(bat) {
			p.absent("Unreal Engine")
		}
		return p.lancerWindows(false, "cmd.exe", append([]string{"/c", wslpathW(bat)}, convertirArgs(args)...)...)
	}
	exe := ue + "/Engine/Binaries/Win64/UnrealEditor.exe"
	if !fichierP(exe) {
		p.absent("Unreal Engine")
	}
	attendre := argGH(args, 0) == "--wait-mcp"
	if attendre {
		args = args[1:]
	}
	conv := convertirArgs(args)
	if attendre { // starts the editor's MCP server without manual setup (Unreal MCP plugin on in the .uproject)
		conv = append(conv, "-ExecCmds=ModelContextProtocol.StartServer")
	}
	p.lancerWindows(true, exe, conv...)
	if attendre {
		url := p.env("UNREAL_MCP_URL", "http://127.0.0.1:8000/mcp")
		c := http.Client{Timeout: 3 * time.Second}
		for i := 0; i < 60; i++ {
			if r, err := c.Get(url); err == nil {
				r.Body.Close()
				fmt.Fprintf(p.sortie, "Unreal MCP ready (%s)\n", url)
				return 0
			}
			time.Sleep(5 * time.Second)
		}
		p.refus(1, "The editor is running but its MCP server is not answering on %s (Unreal MCP plugin enabled? auto-start?).", url)
	}
	fmt.Fprintln(p.sortie, "Unreal editor started.")
	return 0
}

// processusWindows: tasklist's answer for that image name mentions motif (grep -qi).
func processusWindows(nom, motif string) bool {
	out, _ := dansC("tasklist.exe", "/FI", "IMAGENAME eq "+nom, "/NH").Output()
	return strings.Contains(minuscules(string(out)), minuscules(motif))
}

func (p *passerelle) blender(args []string) int {
	exe := os.Getenv("BLENDER_EXE")
	if exe == "" || !fichierP(exe) {
		p.absent("Blender")
	}
	ouvert := func() bool { return processusWindows("blender.exe", "blender.exe") }
	if argGH(args, 0) != "--open" {
		return p.lancerWindows(false, exe, convertirArgs(args)...)
	}
	fichier := argGH(args, 1)
	if ouvert() {
		fmt.Fprintln(p.sortie, "Blender is already open.")
		return 0
	}
	if fichier != "" {
		if !strings.HasSuffix(minuscules(fichier), ".blend") {
			p.refus(2, "Refused: a .blend file only.")
		}
		if !fichierP(fichier) {
			p.refus(2, "File not found: %s", fichier)
		}
		reel := realpathP(fichier)
		if !strings.HasPrefix(reel, realpathP(p.projet)+"/") {
			p.refus(2, "Refused: the file must be inside the project (%s).", reel)
		}
		p.lancerWindows(true, exe, wslpathW(reel))
	} else {
		p.lancerWindows(true, exe)
	}
	for i := 0; i < 20; i++ {
		if ouvert() {
			fmt.Fprintln(p.sortie, "Blender started.")
			return 0
		}
		time.Sleep(2 * time.Second)
	}
	p.refus(1, "Blender did not start.")
	return 1
}

// ---------------------------------------------------------------------------- Roblox Studio (system/roblox.sh)

// projetRojo: default.project.json, else the first *.project.json (2 levels at most).
func (p *passerelle) projetRojo() string {
	if fichierP(p.projet + "/default.project.json") {
		return p.projet + "/default.project.json"
	}
	elague := func(n string) bool {
		return strings.HasPrefix(n, ".") || n == "node_modules" || n == "build" || n == "Packages"
	}
	if elague(filepath.Base(p.projet)) {
		return ""
	}
	trouve := ""
	racine := strings.Count(filepath.Clean(p.projet), "/")
	_ = filepath.WalkDir(p.projet, func(c string, e fs.DirEntry, err error) error {
		if err != nil || trouve != "" {
			return nil
		}
		prof := strings.Count(filepath.Clean(c), "/") - racine
		if c != p.projet && elague(e.Name()) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() && prof >= 2 {
			return filepath.SkipDir
		}
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".project.json") && prof <= 2 {
			trouve = c
			return filepath.SkipAll
		}
		return nil
	})
	return trouve
}

func (p *passerelle) rojo() string {
	var l []string
	if c, err := exec.LookPath("rojo"); err == nil {
		l = append(l, c)
	}
	l = append(l, p.env("TOOLS", "/nulle-part")+"/bin/rojo.exe", p.env("TOOLS", "/nulle-part")+"/bin/rojo",
		p.env("WIN_PROFIL", "/nulle-part")+"/.rokit/bin/rojo.exe", os.Getenv("HOME")+"/.rokit/bin/rojo")
	for _, c := range l {
		if fichierP(c) {
			return c
		}
	}
	return ""
}

func (p *passerelle) placeStudio(fichier string) string {
	nom := ""
	if b, err := os.ReadFile(fichier); err == nil {
		var v map[string]any
		if json.Unmarshal(b, &v) == nil {
			switch x := v["name"].(type) {
			case string:
				nom = x
			case float64:
				nom = strconv.FormatFloat(x, 'f', -1, 64)
			case bool:
				if x {
					nom = "true"
				}
			}
		}
	}
	nom = regexp.MustCompile(`[^A-Za-z0-9_-]`).ReplaceAllString(nom, "")
	if len(nom) > 60 {
		nom = nom[:60]
	}
	if nom == "" {
		nom = "place"
	}
	return p.projet + "/.athanor/studio/" + nom + ".rbxl"
}

func (p *passerelle) construireRojo(f, place string) int {
	rojo := p.rojo()
	if rojo == "" {
		fmt.Fprintln(p.erreur, "rojo is not installed (the project's tools: rokit install).")
		return 127
	}
	_ = os.MkdirAll(filepath.Dir(place), 0o755)
	journal := filepath.Join(filepath.Dir(place), "build.log")
	j, err := os.Create(journal)
	if err != nil {
		return 1
	}
	var c *exec.Cmd
	if strings.HasSuffix(rojo, ".exe") {
		c = exec.Command(rojo, "build", wslpathW(f), "-o", wslpathW(place))
	} else {
		c = exec.Command(rojo, "build", f, "-o", place)
	}
	c.Stdout, c.Stderr = j, j
	err = c.Run()
	j.Close()
	if err != nil {
		b, _ := os.ReadFile(journal)
		l := strings.SplitAfter(string(b), "\n")
		if len(l) > 0 && l[len(l)-1] == "" {
			l = l[:len(l)-1]
		}
		if len(l) > 15 {
			l = l[len(l)-15:]
		}
		io.WriteString(p.erreur, strings.Join(l, ""))
		return 1
	}
	return 0
}

func windowsVerrouille() bool { return processusWindows("LogonUI.exe", "LogonUI") }

func (p *passerelle) fermerStudiosProjet() int {
	w := strings.ReplaceAll(wslpathW(p.projet), "'", "''")
	w = strings.TrimSuffix(w, `\`) + `\`
	script := "$d = '" + w + "'.ToLower(); $n = 0; Get-CimInstance Win32_Process -Filter \"Name='RobloxStudioBeta.exe'\" | Where-Object { $_.CommandLine -and $_.CommandLine.Replace('/', '\\').ToLower().Contains($d) } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; $n++ }; $n"
	out, _ := dansC("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	n, _ := strconv.Atoi(regexp.MustCompile(`[^0-9]`).ReplaceAllString(string(out), ""))
	return n
}

func (p *passerelle) robloxStudio(args []string) int {
	ouvert := func() bool { return processusWindows("RobloxStudioBeta.exe", "RobloxStudioBeta") }
	attendre, reconstruire := false, false
	switch argGH(args, 0) {
	case "--screen":
		if windowsVerrouille() {
			fmt.Fprintln(p.sortie, "Windows session LOCKED (lock screen up): Studio draws nothing until the user unlocks it. Do not retry captures nor restart Studio: note it in .athanor/BUGS.md and go on with what needs no image.")
			return 3
		}
		if ouvert() {
			fmt.Fprintln(p.sortie, "Windows session unlocked, Roblox Studio running: it can draw.")
		} else {
			fmt.Fprintln(p.sortie, "Windows session unlocked, but Roblox Studio is not running (roblox-studio --wait opens it).")
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
		if n := p.fermerStudiosProjet(); n > 0 {
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
		if !ouvert() && !fichierP(place) {
			if p.construireRojo(rojoF, place) != 0 {
				fmt.Fprintln(p.erreur, "rojo build FAILED: Studio opens without the place.")
				place = ""
			}
		}
	}
	if !reconstruire && ouvert() {
		fmt.Fprintln(p.sortie, "Roblox Studio is already open.")
		return 0
	}
	// the newest version (Studio updates itself into a new Versions folder)
	versions, _ := filepath.Glob(p.env("WIN_PROFIL", "/nulle-part") + "/AppData/Local/Roblox/Versions/*/RobloxStudioBeta.exe")
	sort.SliceStable(versions, func(i, j int) bool {
		a, _ := os.Stat(versions[i])
		b, _ := os.Stat(versions[j])
		if a == nil || b == nil {
			return false
		}
		if !a.ModTime().Equal(b.ModTime()) {
			return a.ModTime().After(b.ModTime())
		}
		return versions[i] < versions[j]
	})
	exe := ""
	if len(versions) > 0 {
		exe = versions[0]
	}
	if exe == "" || !fichierP(exe) {
		p.absent("Roblox Studio")
	}
	if place != "" {
		if !fichierP(place) {
			p.refus(2, "Place not found: %s", place)
		}
		if b := minuscules(place); !strings.HasSuffix(b, ".rbxl") && !strings.HasSuffix(b, ".rbxlx") {
			p.refus(2, "Refused: a .rbxl or .rbxlx place only.")
		}
		reel := realpathP(place)
		if !strings.HasPrefix(reel, realpathP(p.projet)+"/") {
			p.refus(2, "Refused: the place must be inside the project (%s).", reel)
		}
		p.lancerWindows(true, exe, "--task", "EditFile", "--localPlaceFile", wslpathW(reel))
	} else {
		p.lancerWindows(true, exe)
	}
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

// ---------------------------------------------------------------------------- install-windows

var reDejaInstalle = regexp.MustCompile(`(?i)already installed|d.{1,2}j.{1,2} install|No newer package|Aucune mise .{1,2} niveau`)

func (p *passerelle) installWindows(args []string) int {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		p.refus(127, "powershell.exe not found: Windows interoperability disabled?")
	}
	switch argGH(args, 0) {
	case "--search":
		mots := strings.Join(args[1:], " ")
		if !regexp.MustCompile(`^[A-Za-z0-9 ._+-]+$`).MatchString(mots) {
			p.refus(2, "Search words: letters, digits, spaces, . _ + -")
		}
		return p.lancer("timeout", "180", "powershell.exe", "-NoProfile", "-Command", "winget search --accept-source-agreements --disable-interactivity '"+mots+"'")
	case "--file":
		return p.pcExec(append([]string{"--admin"}, args[1:]...))
	}
	id := argGH(args, 0)
	if !regexp.MustCompile(`^[A-Za-z0-9._+-]+$`).MatchString(id) {
		p.refus(2, "Usage: install-windows WINGET_ID   (to find the ID: install-windows --search NAME)")
	}
	var sortie strings.Builder
	essai := func(portee string) int {
		c := exec.Command("timeout", "1200", "powershell.exe", "-NoProfile", "-Command",
			"winget install --id '"+id+"' --exact --silent "+portee+" --accept-package-agreements --accept-source-agreements --disable-interactivity; exit $LASTEXITCODE")
		tuyau, _ := c.StdoutPipe()
		c.Stderr = c.Stdout
		code := 1
		if c.Start() == nil {
			r := bufio.NewReader(tuyau)
			for {
				b, err := r.ReadBytes('\n')
				if len(b) > 0 {
					l := strings.ReplaceAll(string(b), "\r", "")
					io.WriteString(p.sortie, l)
					sortie.WriteString(l)
				}
				if err != nil {
					break
				}
			}
			code = codeCommande(c.Wait(), c)
		}
		if reDejaInstalle.MatchString(sortie.String()) {
			code = 0
		}
		return code
	}
	const tache = "Athanor - installations"
	code := essai("--scope user")
	if code != 0 && dansC("schtasks.exe", "/query", "/tn", tache).Run() == nil {
		// installation with administrator rights, allowed once by the user: the scheduled task installs it
		// without a window
		fmt.Fprintf(p.sortie, "(installing with administrator rights via the '%s' task)\n", tache)
		dossier := filepath.Join(p.kit, "tools", "windows", "admin-requests")
		_ = os.MkdirAll(dossier, 0o755)
		n := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())
		_ = os.WriteFile(filepath.Join(dossier, "demande-"+n+".txt"), []byte(id), 0o644)
		_ = dansC("schtasks.exe", "/run", "/tn", tache).Run()
		res := filepath.Join(dossier, "resultat-"+n+".txt")
		for i := 0; i < 240 && !existeP(res); i++ {
			time.Sleep(attenteAdmin)
		}
		if b, err := os.ReadFile(res); err == nil {
			t := strings.ReplaceAll(string(b), "\r", "")
			io.WriteString(p.sortie, t)
			sortie.WriteString(t)
			code = 1
			for _, l := range strings.Split(t, "\n") {
				if strings.HasPrefix(l, "code=") {
					if c, err := strconv.Atoi(strings.TrimPrefix(l, "code=")); err == nil {
						code = c // as written (the exit status keeps its last byte)
					}
					break
				}
			}
			os.Remove(res)
		} else {
			fmt.Fprintln(p.erreur, "No answer from the administrator task after 20 min.")
			os.Remove(filepath.Join(dossier, "demande-"+n+".txt"))
			code = 124
		}
	} else if code != 0 {
		// never a UAC prompt (nobody is there to click it): without the authorised task, stop here
		fmt.Fprintln(p.erreur, "Administrator rights not allowed yet: in the app, Setup & health -> Administrator rights -> Repair (one UAC prompt, once).")
	}
	journal := filepath.Join(p.kit, "tools", "INSTALLS.md")
	b, _ := os.ReadFile(journal)
	f, err := os.OpenFile(journal, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err == nil {
		if !regexp.MustCompile(`(?m)^## On Windows`).Match(b) {
			io.WriteString(f, "\n## On Windows (winget, logged automatically)\n\n| Date | Software (winget ID) | Result | Uninstall |\n|---|---|---|---|\n")
		}
		res := "installed"
		if code != 0 {
			res = fmt.Sprintf("failed (code %d)", code)
		}
		fmt.Fprintf(f, "| %s | `%s` | %s | `winget uninstall --id %s` |\n", time.Now().Format("2006-01-02 15:04"), id, res, id)
		f.Close()
	}
	if code != 0 {
		fmt.Fprintf(p.erreur, "FAILED to install %s. Administrator rights are probably needed: note it in TODO_FOR_YOU.md (at the project root) and continue with an alternative (portable version in $TOOLS/windows, another tool).\n", id)
	}
	return code
}

var attenteAdmin = 5 * time.Second

// ---------------------------------------------------------------------------- entry

var passerellesGo = map[string]bool{"adb": true, "blender": true, "install-windows": true, "pc-exec": true, "roblox-studio": true,
	"tool": true, "unreal-cmd": true, "unreal-editor": true, "unreal-uat": true, "windows-var": true}

func lancerPasserelle(args []string) (code int) {
	if len(args) < 1 || !passerellesGo[args[0]] {
		fmt.Fprintln(os.Stderr, "usage: athanor passerelle adb|blender|install-windows|pc-exec|roblox-studio|tool|unreal-cmd|unreal-editor|unreal-uat|windows-var ARGUMENTS...")
		return 2
	}
	kit := os.Getenv("KIT")
	if kit == "" {
		if e, err := os.Executable(); err == nil {
			kit = filepath.Dir(e)
		}
	}
	projet := os.Getenv("PROJECT")
	if projet == "" {
		projet = kit + "/projet"
	}
	p := &passerelle{kit: kit, projet: projet, sortie: os.Stdout, erreur: os.Stderr}
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(sortiePasserelle); ok {
				code = int(s)
				return
			}
			panic(r)
		}
	}()
	outil, reste := args[0], args[1:]
	switch outil {
	case "adb":
		return p.adb(reste)
	case "blender":
		return p.blender(reste)
	case "install-windows":
		return p.installWindows(reste)
	case "pc-exec":
		return p.pcExec(reste)
	case "roblox-studio":
		return p.robloxStudio(reste)
	case "tool":
		return p.tool(reste)
	case "windows-var":
		return p.windowsVar(reste)
	}
	return p.unreal(outil, reste)
}
