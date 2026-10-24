package main

// Claude's commands are the kit's executable under other names (the former scripts of system/bin): the
// pilot links them into system/bin, which is on Claude's PATH (a symbolic link on Linux and macOS, a hard
// link of Athanor.exe, NAME.exe, in native Windows). Called by one of these names, the executable is that
// gateway:
//   - adb, blender, install-windows, pc-exec, roblox-studio, tool, unreal-cmd, unreal-editor, unreal-uat,
//     windows-var: the PC side (athanor passerelle) when it runs outside Claude's sandbox (PONT_DIRECT=1,
//     native Windows), else the request handed to the pilot's bridge (athanor pont), whose output is shown
//     live and whose exit code is returned;
//   - athanor: Claude's AI-free tools only (screenshots, map, archive, todo).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// nomsPasserelles: the gateways of system/bin (also the bridge's list, passerellesPont).
var nomsPasserelles = []string{"adb", "blender", "install-windows", "pc-exec", "roblox-studio", "tool", "unreal-cmd", "unreal-editor", "unreal-uat", "windows-var"}

// commandesClaude: what "athanor" offers Claude (system/bin/athanor).
var commandesClaude = map[string]bool{"screenshots": true, "map": true, "archive": true, "todo": true}

// nomAppel: the name the executable was called by (no folder, no .exe, lower case).
func nomAppel() string {
	n := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.TrimSuffix(n, ".exe")
}

// kitDeLappel: the tool folder of a gateway: the executable's own folder, or two levels up from system/bin
// (a hard link of Athanor.exe in native Windows; Linux resolves the symbolic link to the kit's athanor).
func kitDeLappel() string {
	if k := os.Getenv("KIT"); k != "" {
		return cheminOutil(k)
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	d := filepath.Dir(exe)
	if strings.EqualFold(filepath.Base(d), "bin") && strings.EqualFold(filepath.Base(filepath.Dir(d)), "system") {
		return filepath.Dir(filepath.Dir(d))
	}
	return d
}

// lancerParNom: the exit code and true when the executable was called by the name of a gateway.
func lancerParNom() (int, bool) {
	nom := nomAppel()
	if n := os.Getenv("ATHANOR_APPEL"); n != "" { // the pilot's own calls (commandePasserelleKit)
		_ = os.Unsetenv("ATHANOR_APPEL")
		if n != "athanor" {
			nom = n
		}
	}
	if nom == "athanor" {
		// the kit's own executable is "athanor" too: only its link in system/bin (Claude's) is restricted
		d := filepath.Dir(os.Args[0])
		if lp, err := exec.LookPath(os.Args[0]); err == nil && !strings.ContainsAny(os.Args[0], `/\`) {
			d = filepath.Dir(lp) // called by its bare name, through the PATH
		}
		if !strings.EqualFold(filepath.Base(d), "bin") || !strings.EqualFold(filepath.Base(filepath.Dir(d)), "system") {
			return 0, false
		}
		if len(os.Args) < 2 || !commandesClaude[os.Args[1]] {
			fmt.Fprintln(os.Stderr, "Usage: athanor screenshots|map|archive|todo ...")
			return 2, true
		}
		code, _ := lancerSousCommande(os.Args[1:])
		return code, true
	}
	estPasserelle := false
	for _, n := range nomsPasserelles {
		if n == nom {
			estPasserelle = true
		}
	}
	if !estPasserelle {
		return 0, false
	}
	kit := kitDeLappel()
	_ = os.Setenv("KIT", kit)
	if os.Getenv("PROJECT") == "" {
		_ = os.Setenv("PROJECT", filepath.Join(kit, "projet"))
	} else {
		_ = os.Setenv("PROJECT", cheminOutil(os.Getenv("PROJECT")))
	}
	if t := os.Getenv("TOOLS"); t != "" {
		_ = os.Setenv("TOOLS", cheminOutil(t))
	}
	chargerPcTools(kit)
	if runtime.GOOS == "darwin" {
		fmt.Fprintf(os.Stderr, "%s: no gateway on macOS: the applications run directly (see .athanor/PC_TOOLS.md).\n", nom)
		return 127, true
	}
	if os.Getenv("PONT_DIRECT") == "1" || runtime.GOOS == "windows" {
		return lancerPasserelle(append([]string{nom}, os.Args[1:]...)), true // already outside the sandbox
	}
	return passerParLePont(nom, os.Args[1:]), true
}

// chargerPcTools: system/pc_tools.env (UE_DIR='…' lines, written by athanor detecter) into the environment.
func chargerPcTools(kit string) {
	b, err := os.ReadFile(filepath.Join(kit, "system", "pc_tools.env"))
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		cle, val, ok := strings.Cut(strings.TrimSpace(strings.TrimSuffix(l, "\r")), "=")
		if !ok || !reCleEnvPilote.MatchString(cle+"=") {
			continue
		}
		if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
			val = strings.ReplaceAll(val[1:len(val)-1], `'\''`, "'")
		}
		// over what the caller's environment says (as the former scripts sourced it): the PC's programs
		// are those athanor detecter found, never ones a command line names
		_ = os.Setenv(cle, cheminOutil(val))
	}
}

// passerParLePont: inside the sandbox a gateway can neither start a Windows program nor reach Docker: it
// hands the command to the pilot's bridge (outside the sandbox) and shows its output live.
func passerParLePont(outil string, args []string) int {
	dossier := os.Getenv("ATHANOR_PONT")
	if dossier == "" {
		dossier = filepath.Join(os.Getenv("PROJECT"), ".athanor", "pont")
	}
	if st, err := os.Stat(filepath.Join(dossier, "vivant")); err != nil || time.Since(st.ModTime()) > 10*time.Second {
		fmt.Fprintf(os.Stderr, "%s: the pilot's bridge is not responding (pilot stopped?). Restart the pilot.\n", outil)
		return 127
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid())
	demande := filepath.Join(dossier, "demande-"+id+".json")
	sortie := filepath.Join(dossier, "sortie-"+id+".log")
	codeF := filepath.Join(dossier, "code-"+id)
	cwd, _ := os.Getwd()
	env := map[string]string{}
	if u := os.Getenv("UNREAL_MCP_URL"); u != "" {
		env["UNREAL_MCP_URL"] = u
	}
	if args == nil {
		args = []string{}
	}
	b, _ := json.Marshal(map[string]any{"outil": outil, "cwd": cwd, "args": args, "env": env})
	if err := os.WriteFile(demande+".tmp", b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot write to the bridge: %v\n", outil, err)
		return 127
	}
	if err := os.Rename(demande+".tmp", demande); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot write to the bridge: %v\n", outil, err)
		return 127
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	var lu int64
	montrer := func() {
		f, err := os.Open(sortie)
		if err != nil {
			return
		}
		defer f.Close()
		if _, err := f.Seek(lu, io.SeekStart); err == nil {
			n, _ := io.Copy(os.Stdout, f)
			lu += n
		}
	}
	for {
		select {
		case <-sig:
			_ = os.WriteFile(filepath.Join(dossier, "annuler-"+id), nil, 0o644)
			return 130
		case <-time.After(300 * time.Millisecond):
		}
		montrer()
		if c, err := os.ReadFile(codeF); err == nil {
			montrer()
			_ = os.Remove(codeF)
			_ = os.Remove(sortie)
			n, err := strconv.Atoi(strings.TrimSpace(string(c)))
			if err != nil {
				return 1
			}
			return n & 0xff
		}
	}
}

// commandePasserelleKit: the gateway NAME run by the pilot or the app themselves (outside Claude's sandbox):
// the kit's executable called by that name, so it reads the PC tools and runs the PC side directly.
func commandePasserelleKit(kit, projet, nom string, args ...string) *exec.Cmd {
	exe := filepath.Join(kit, "athanor")
	if runtime.GOOS == "windows" {
		exe = filepath.Join(kit, "Athanor.exe")
	}
	if _, err := os.Stat(exe); err != nil {
		if e, err := os.Executable(); err == nil {
			exe = e
		}
	}
	// (test harness: stand-ins of the gateways, a folder of NAME scripts; never set by the application)
	if d := os.Getenv("ATHANOR_FAUSSES_PASSERELLES"); d != "" && nom != "athanor" {
		if f := filepath.Join(d, nom); fichierRegulierPilote(f) {
			c := commande(f, args...)
			c.Env = append(os.Environ(), "KIT="+kit, "PROJECT="+projet, "PONT_DIRECT=1")
			return c
		}
	}
	c := commande(exe, args...)
	if nom != "athanor" {
		c.Args = append([]string{nom}, args...) // the name it is called by (argv[0])
	}
	c.Env = append(os.Environ(), "KIT="+kit, "PROJECT="+projet, "PONT_DIRECT=1", "ATHANOR_APPEL="+nom)
	return c
}
