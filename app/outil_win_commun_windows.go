//go:build windows

package main

// What the Windows side's steps share (installer-windows, installer-natif, autoriser-admin, admin-tache,
// reseau-unreal, relais-unreal, wsl): the window's colours and questions, wsl.exe's answers, running
// Athanor.exe again with administrator rights (one Windows consent), the step's log.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// etapeWin: the output of a step (the window, and its log file: the former transcripts) and its questions.
type etapeWin struct {
	sortie  io.Writer
	journal *os.File
	lecteur *bufio.Reader
	couleur bool
}

// nouvelleEtapeWin: a step writing to the console and appending to logs\NAME (when given).
func nouvelleEtapeWin(kit, nomJournal string) *etapeWin {
	e := &etapeWin{sortie: os.Stdout, couleur: activerCouleursConsole()}
	if kit != "" && nomJournal != "" {
		_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
		if f, err := os.OpenFile(filepath.Join(kit, "logs", nomJournal), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			e.journal = f
			fmt.Fprintf(f, "\r\n===== %s\r\n", time.Now().Format("2006-01-02 15:04:05"))
		}
	}
	return e
}

func (e *etapeWin) fermer() {
	if e.journal != nil {
		e.journal.Close()
	}
}

// ecrire: a line in a colour (ANSI codes, the console understands them once activated), and in the log.
func (e *etapeWin) ecrire(code, texte string) {
	if e.couleur && code != "" {
		fmt.Fprint(e.sortie, "\x1b["+code+"m"+texte+"\x1b[0m\r\n")
	} else {
		fmt.Fprint(e.sortie, texte+"\r\n")
	}
	if e.journal != nil {
		fmt.Fprint(e.journal, texte+"\r\n")
	}
}

func (e *etapeWin) ligne(t string)  { e.ecrire("", t) }
func (e *etapeWin) titre(t string)  { e.ligne(""); e.ecrire("97", "== "+t) }
func (e *etapeWin) ok(t string)     { e.ecrire("92", "OK  "+t) }
func (e *etapeWin) alerte(t string) { e.ecrire("93", "!   "+t) }
func (e *etapeWin) info(t string)   { e.ecrire("96", t) }
func (e *etapeWin) gris(t string)   { e.ecrire("90", t) }
func (e *etapeWin) vert(t string)   { e.ecrire("92", t) }

// lire: a line typed by the user ("" at the end of the input).
func (e *etapeWin) lire(question string) string {
	fmt.Fprint(e.sortie, question+": ")
	if e.lecteur == nil {
		e.lecteur = bufio.NewReader(os.Stdin)
	}
	r, _ := e.lecteur.ReadString('\n')
	r = strings.TrimRight(r, "\r\n")
	if e.journal != nil {
		fmt.Fprint(e.journal, question+": "+r+"\r\n")
	}
	return r
}

// ouiNon: yes or no, Enter giving the default.
func (e *etapeWin) ouiNon(q string, defOui bool) bool {
	inv := "[y/N]"
	if defOui {
		inv = "[Y/n]"
	}
	r := strings.TrimSpace(e.lire(q + " " + inv))
	if r == "" {
		return defOui
	}
	return strings.ContainsAny(r[:1], "oOyY")
}

// activerCouleursConsole: the console interprets the ANSI colour codes (false when it cannot).
func activerCouleursConsole() bool {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

// sansCaracteresNuls: wsl.exe's own messages are UTF-16 even with WSL_UTF8 for some of them.
func sansCaracteresNuls(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// wslSortie: what wsl.exe ARGS prints (both streams), its errors included.
func wslSortie(args ...string) string {
	c := commande("wsl.exe", args...)
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	out, _ := c.CombinedOutput()
	return sansCaracteresNuls(string(out))
}

// wslVisible: wsl.exe ARGS in this console (its questions and output), its exit code.
func wslVisible(args ...string) int {
	c := commande("wsl.exe", args...)
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if c.ProcessState != nil {
			return c.ProcessState.ExitCode()
		}
		return 1
	}
	return 0
}

// wslArret: wsl --shutdown, then the few seconds WSL needs.
func wslArret() {
	_ = commande("wsl.exe", "--shutdown").Run()
	time.Sleep(4 * time.Second)
}

// lireDistroKit: system\distro.txt, written by installer-windows ("" when missing).
func lireDistroKit(kit string) string {
	b, err := os.ReadFile(filepath.Join(kit, "system", "distro.txt"))
	if err != nil {
		return ""
	}
	return strings.Trim(string(b), "\ufeff \r\n\t")
}

// kitWindows: the tool folder: --kit DIR, else Athanor.exe's own folder.
func kitWindows(args []string) (string, []string) {
	kit := ""
	reste := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--kit" && i+1 < len(args) {
			kit = args[i+1]
			i++
			continue
		}
		reste = append(reste, args[i])
	}
	if kit == "" {
		if exe, err := os.Executable(); err == nil {
			kit = filepath.Dir(exe)
		}
	}
	if a, err := filepath.Abs(kit); err == nil {
		kit = a
	}
	return kit, reste
}

// estAdministrateur: this process runs with administrator rights (elevated).
func estAdministrateur() bool { return windows.GetCurrentProcessToken().IsElevated() }

var pShellExecuteExW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteExW")

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// erreurConsentementRefuse: the user said no to Windows' consent (UAC).
var erreurConsentementRefuse = fmt.Errorf("administrator rights refused")

// executerEleve: PROGRAM ARGS with administrator rights (Windows asks the user once), waited for; its exit
// code. erreurConsentementRefuse when the user refuses. cache: no window (SW_HIDE).
func executerEleve(programme string, args []string, cache bool) (int, error) {
	ligne := make([]string, len(args))
	for i, a := range args {
		ligne[i] = syscall.EscapeArg(a)
	}
	verbe, _ := syscall.UTF16PtrFromString("runas")
	fichier, _ := syscall.UTF16PtrFromString(programme)
	params, _ := syscall.UTF16PtrFromString(strings.Join(ligne, " "))
	const seeMaskNoCloseProcess, seeMaskNoAsync = 0x40, 0x100
	montrer := int32(1) // SW_SHOWNORMAL
	if cache {
		montrer = 0 // SW_HIDE
	}
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess | seeMaskNoAsync, lpVerb: verbe, lpFile: fichier, lpParameters: params, nShow: montrer}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, err := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if en, ok := err.(syscall.Errno); ok && en == 1223 { // ERROR_CANCELLED
			return -1, erreurConsentementRefuse
		}
		return -1, err
	}
	if info.hProcess == 0 {
		return 0, nil
	}
	defer windows.CloseHandle(info.hProcess)
	_, _ = windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
	var code uint32
	_ = windows.GetExitCodeProcess(info.hProcess, &code)
	return int(code), nil
}

// relancerEleveJournal: this very command again, with administrator rights and without a window, its
// messages written into a file and shown here afterwards. Its exit code (-1 and an error when refused).
func relancerEleveJournal(e *etapeWin, args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, err
	}
	f, err := os.CreateTemp("", "athanor-eleve-*.txt")
	if err != nil {
		return -1, err
	}
	nom := f.Name()
	f.Close()
	defer os.Remove(nom)
	code, err := executerEleve(exe, append(append([]string{}, args...), "--journal", nom), true)
	if b, e2 := os.ReadFile(nom); e2 == nil {
		for _, l := range lignesFichier(string(b)) {
			e.ligne(l)
		}
	}
	return code, err
}

// sortieJournal: --journal FILE (an elevated run without a window writes its messages there), the other
// arguments; the step writes to that file, else to the console.
func sortieJournal(args []string) (*etapeWin, []string) {
	reste := []string{}
	journal := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--journal" && i+1 < len(args) {
			journal = args[i+1]
			i++
			continue
		}
		reste = append(reste, args[i])
	}
	e := &etapeWin{sortie: os.Stdout}
	if journal != "" {
		if f, err := os.OpenFile(journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			e.sortie = f
		} else {
			e.sortie = io.Discard
		}
	} else {
		e.couleur = activerCouleursConsole()
	}
	return e, reste
}

// sansFenetreWin: a program started without any window of its own (a step running without a console: the
// administrator task, an elevated run).
func sansFenetreWin(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c
}

// powershellCache: a short PowerShell command for what Windows only offers there (the firewall's rule
// groups and network adapters: the NetSecurity module), its values passed through the environment, never
// inside the command's text. Its output and error.
func powershellCache(script string, env ...string) (string, error) {
	return powershellCacheEntree(script, "", env...)
}

// powershellCacheEntree: powershellCache, with a text on its standard input ([Console]::In.ReadToEnd()):
// a list too long for an environment variable.
func powershellCacheEntree(script, entree string, env ...string) (string, error) {
	c := sansFenetreWin(commande("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
		"[Console]::OutputEncoding = [Text.Encoding]::UTF8; [Console]::InputEncoding = [Text.Encoding]::UTF8; "+script))
	c.Env = append(os.Environ(), env...)
	c.Stdin = strings.NewReader(entree)
	out, err := c.CombinedOutput()
	return strings.ReplaceAll(string(out), "\r", ""), err
}
