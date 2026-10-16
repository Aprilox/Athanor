//go:build windows

package main

// Windows part: WSL commands without a window, application window (Edge), start with
// Windows, and the "Claude" virtual desktop (VirtualDesktop11 utility by Markus Scholtes, MIT,
// compiled on first launch with the C# compiler shipped with Windows).

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const (
	createNoWindow         = 0x08000000
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

func cacherFenetre(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// fichierScript writes the script into logs/ and returns its path relative to the kit: passing the
// script through a file avoids any escaping problem between Windows, wsl.exe and bash
// (quotes, $, stars), and keeps a trace of what was run.
func fichierScript(k Kit, script string) (string, error) {
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(script))
	nom := "logs/.cmd-" + hex.EncodeToString(h[:5]) + ".sh"
	return nom, os.WriteFile(filepath.Join(k.Racine, filepath.FromSlash(nom)), []byte("#!/usr/bin/env bash\n"+script+"\n"), 0o644)
}

// DemarrerDetache runs a WSL script in a hidden wsl.exe, detached from the application (it survives it);
// the output goes into the script itself (start.sh writes the project's output.log).
func DemarrerDetache(k Kit, script string) error {
	nom, err := fichierScript(k, script)
	if err != nil {
		return err
	}
	c := exec.Command("wsl.exe", "-d", k.Distro(), "--cd", k.Racine, "--", "bash", "-l", "./"+nom)
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	// Outside the application's "job" if possible: closing the application must not take the pilot with it.
	// CREATE_NO_WINDOW only, never with DETACHED_PROCESS: Windows ignores CREATE_NO_WINDOW when both are
	// given, and wsl.exe (a console program) then opens a console window of its own.
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob}
	if err := c.Start(); err != nil {
		c = exec.Command("wsl.exe", "-d", k.Distro(), "--cd", k.Racine, "--", "bash", "-l", "./"+nom)
		c.Env = append(os.Environ(), "WSL_UTF8=1")
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup}
		if err := c.Start(); err != nil {
			return err
		}
	}
	go func() { _ = c.Wait() }()
	return nil
}

func commandeWSL(k Kit, script string) *exec.Cmd {
	nom, err := fichierScript(k, script)
	if err != nil {
		return exec.Command("cmd.exe", "/c", "echo Cannot write the script in logs\\: "+err.Error()+" & exit 1")
	}
	c := exec.Command("wsl.exe", "-d", k.Distro(), "--cd", k.Racine, "--", "bash", "-l", "./"+nom)
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	return c
}

// Visible console window for an interactive step (sudo, token).
func FenetreWSL(k Kit, titre, script string) error {
	nom, err := fichierScript(k, script+"\necho\nread -p 'Press Enter to close' _\n")
	if err != nil {
		return err
	}
	return demarrerFenetre(titre, "wsl.exe", "-d", k.Distro(), "--cd", k.Racine, "--", "bash", "-l", "./"+nom)
}

// demarrerFenetre opens a visible console with "cmd /c start". The command line is written by
// hand, each argument in quotes: otherwise a "&" in a path (D:\Dev\R&D) would cut the
// command and cmd would run what follows.
func demarrerFenetre(titre string, args ...string) error {
	q := func(a string) string { return `"` + strings.ReplaceAll(a, `"`, "") + `"` }
	ligne := `cmd.exe /c start ` + q(titre)
	for _, a := range args {
		ligne += " " + q(a)
	}
	c := exec.Command("cmd.exe")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow, CmdLine: ligne}
	return c.Start()
}

func FenetrePowerShell(k Kit, scriptRelatif string) error {
	chemin := filepath.Join(k.Racine, scriptRelatif)
	if strings.HasSuffix(strings.ToLower(chemin), ".cmd") {
		return demarrerFenetre("Installation", "cmd.exe", "/k", chemin)
	}
	return demarrerFenetre("Installation", "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-NoExit", "-File", chemin)
}

// Ouvrir shows a folder in Explorer, or opens a file with its associated program.
func Ouvrir(chemin string) error {
	if _, err := os.Stat(chemin); err != nil {
		return fmt.Errorf("Not created yet: %s", chemin)
	}
	return exec.Command("explorer.exe", chemin).Start()
}

// FenetreApp opens the interface in a real application window (WebView2, shipped with
// Windows 11). Blocking: call it from the main thread. Returns false when WebView2 is missing.
func FenetreApp(url string) bool {
	runtime.LockOSThread()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus:     true,
		DataPath:      filepath.Join(os.Getenv("LocalAppData"), "Athanor", "webview2"),
		WindowOptions: webview2.WindowOptions{Title: "Athanor", Width: 1240, Height: 820, Center: true, IconId: 1},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	w.SetSize(1240, 820, webview2.HintMin)
	w.Navigate(url)
	w.Run()
	return true
}

// OuvrirFenetreApp (fallback): Edge or Chrome application window without address bar, else the browser.
func OuvrirFenetreApp(url string) {
	candidats := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
	}
	profil := filepath.Join(os.Getenv("LocalAppData"), "Athanor", "navigateur")
	for _, exe := range candidats {
		if _, err := os.Stat(exe); err == nil {
			c := exec.Command(exe, "--app="+url, "--user-data-dir="+profil, "--window-size=1240,820", "--no-first-run", "--no-default-browser-check")
			cacherFenetre(c)
			if c.Start() == nil {
				return
			}
		}
	}
	_ = Ouvrir(url)
}

const cleRun = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const nomRun = "Athanor"

func DemarrageWindows(actif bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var c *exec.Cmd
	if actif {
		c = exec.Command("reg.exe", "add", cleRun, "/v", nomRun, "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\" --reduit", exe), "/f")
	} else {
		c = exec.Command("reg.exe", "delete", cleRun, "/v", nomRun, "/f")
	}
	cacherFenetre(c)
	out, err := c.CombinedOutput()
	if err != nil && actif {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// migrerSysteme moves what the previous name (Claude Autopilote) left on Windows: the local data
// folder (WebView2 profile, compiled VirtualDesktop utility) and the "start with Windows" entry,
// which is re-created under the new name and pointing at this executable.
func migrerSysteme() {
	local := os.Getenv("LocalAppData")
	if local != "" {
		ancien, neuf := filepath.Join(local, "ClaudeAutopilote"), filepath.Join(local, "Athanor")
		if _, err := os.Stat(neuf); os.IsNotExist(err) {
			if _, err := os.Stat(ancien); err == nil {
				_ = os.Rename(ancien, neuf)
			}
		}
	}
	q := exec.Command("reg.exe", "query", cleRun, "/v", "ClaudeAutopilote")
	cacherFenetre(q)
	if q.Run() == nil {
		d := exec.Command("reg.exe", "delete", cleRun, "/v", "ClaudeAutopilote", "/f")
		cacherFenetre(d)
		_ = d.Run()
		_ = DemarrageWindows(true)
	}
}

func DemarrageWindowsActif() bool {
	c := exec.Command("reg.exe", "query", cleRun, "/v", nomRun)
	cacherFenetre(c)
	return c.Run() == nil
}

// ----------------------------------------------------------------------------- virtual desktop

var (
	user32                      = syscall.NewLazyDLL("user32.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	pEnumWindows                = user32.NewProc("EnumWindows")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pGetWindowTextW             = user32.NewProc("GetWindowTextW")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pGetWindowLongW             = user32.NewProc("GetWindowLongW")
	pOpenProcess                = kernel32.NewProc("OpenProcess")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pCloseHandle                = kernel32.NewProc("CloseHandle")
	pGetCurrentProcessId        = kernel32.NewProc("GetCurrentProcessId")
)

type fenetre struct {
	hwnd  uintptr
	titre string
	exe   string
	pid   uint32
}

func listerFenetres() []fenetre {
	var liste []fenetre
	moi, _, _ := pGetCurrentProcessId.Call()
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		ex, _, _ := pGetWindowLongW.Call(hwnd, uintptr(0xFFFFFFEC)) // GWL_EXSTYLE = -20
		if uint32(ex)&0x80 != 0 {                                   // WS_EX_TOOLWINDOW
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		if n == 0 {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || uintptr(pid) == moi {
			return 1
		}
		exe := ""
		h, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
		if h != 0 {
			b := make([]uint16, 1024)
			taille := uint32(len(b))
			if r, _, _ := pQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&taille))); r != 0 {
				exe = syscall.UTF16ToString(b[:taille])
			}
			pCloseHandle.Call(h)
		}
		liste = append(liste, fenetre{hwnd, syscall.UTF16ToString(buf[:n]), exe, pid})
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return liste
}

type Bureau struct {
	mu            sync.Mutex
	kit           Kit
	helper        string
	disponible    bool
	message       string
	numClaude     int // number of the "Claude" desktop (-1 = unknown)
	deplacees     map[uintptr]bool
	exclues       map[uintptr]bool // windows the user brought back: never moved again
	PilotesActifs func() bool      // set by the server: at least one pilot running
	vues          []string
	nbDeplacees   int
	retour        int
	dossiers      []string
	Programmes    []string
	Actif         bool
}

func NouveauBureau(k Kit) *Bureau {
	b := &Bureau{kit: k, numClaude: -1, retour: 0, deplacees: map[uintptr]bool{}, exclues: map[uintptr]bool{}, Actif: true}
	go b.boucle()
	return b
}

func (b *Bureau) DefinirKit(k Kit)           { b.mu.Lock(); b.kit = k; b.mu.Unlock() }
func (b *Bureau) DefinirDossiers(d []string) { b.mu.Lock(); b.dossiers = d; b.mu.Unlock() }

// DefinirReglages changes the activation and the list of programs (under lock: the loop reads them).
func (b *Bureau) DefinirReglages(actif bool, programmes []string) {
	b.mu.Lock()
	b.Actif, b.Programmes = actif, programmes
	b.mu.Unlock()
}

// Compiles the utility when first needed: %LocalAppData%\Athanor\VirtualDesktop11.exe
func (b *Bureau) preparerHelper() error {
	if b.helper != "" {
		return nil
	}
	dir := filepath.Join(os.Getenv("LocalAppData"), "Athanor")
	exe := filepath.Join(dir, "VirtualDesktop11.exe")
	if _, err := os.Stat(exe); err == nil {
		b.helper = exe
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := filepath.Join(dir, "VirtualDesktop11.cs")
	if err := os.WriteFile(src, sourceVirtualDesktop, 0o644); err != nil {
		return err
	}
	csc := filepath.Join(os.Getenv("WINDIR"), "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	if _, err := os.Stat(csc); err != nil {
		csc = filepath.Join(os.Getenv("WINDIR"), "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe")
	}
	c := exec.Command(csc, "/nologo", "/target:exe", "/out:"+exe, src)
	cacherFenetre(c)
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Cannot compile the virtual desktop helper: %s", strings.TrimSpace(string(out)))
	}
	b.helper = exe
	return nil
}

// appel runs the utility; the exit code is the desktop number of the "pipeline" (or -1).
func (b *Bureau) appel(args ...string) (int, string, error) {
	c := exec.Command(b.helper, append([]string{"/Quiet"}, args...)...)
	cacherFenetre(c)
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
			err = nil
		}
	}
	if code > 100000 || code < 0 { // -1 comes back as uint32
		code = -1
	}
	return code, strings.TrimSpace(string(out)), err
}

// Finds or creates the "Claude" desktop; returns its number.
func (b *Bureau) assurerBureau() (int, error) {
	if n, _, err := b.appel("/GetDesktop:Claude"); err == nil && n >= 0 {
		b.numClaude = n
		return n, nil
	}
	n, out, err := b.appel("/New", "/Name:Claude")
	if err != nil || n < 0 {
		return -1, fmt.Errorf("Cannot create the desktop: %s %v", out, err)
	}
	b.numClaude = n
	return n, nil
}

func (b *Bureau) appartient(f fenetre) bool {
	exe := strings.ToLower(f.exe)
	base := exe[strings.LastIndex(exe, "\\")+1:]
	for _, p := range b.Programmes {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && (base == p || strings.HasPrefix(base, strings.TrimSuffix(p, ".exe"))) {
			return true
		}
	}
	for _, d := range append([]string{filepath.Join(b.kit.Racine, "tools")}, b.dossiers...) {
		if d != "" && strings.HasPrefix(exe, strings.ToLower(d)) {
			return true
		}
	}
	// (No rule on the title: the application itself and its installation windows must stay here.)
	return false
}

func (b *Bureau) boucle() {
	for {
		time.Sleep(1500 * time.Millisecond)
		b.mu.Lock()
		if !b.Actif {
			b.message = "Virtual desktop disabled (Options)."
			b.mu.Unlock()
			continue
		}
		if err := b.preparerHelper(); err != nil {
			b.disponible, b.message = false, err.Error()
			b.mu.Unlock()
			continue
		}
		n, err := b.assurerBureau()
		if err != nil {
			b.disponible, b.message = false, err.Error()
			b.mu.Unlock()
			continue
		}
		b.disponible = true
		b.message = "“Claude” desktop ready: the pilots' windows are sent there automatically."
		// Only while a pilot is running: the user's own use of the same programs is never touched when
		// nothing is working, and a window brought back by the user stays where the user put it.
		actifs := b.PilotesActifs == nil || b.PilotesActifs()
		var vues []string
		for _, f := range listerFenetres() {
			if !b.appartient(f) {
				continue
			}
			vues = append(vues, f.titre+" — "+f.exe[strings.LastIndex(f.exe, "\\")+1:])
			if !actifs {
				b.exclues[f.hwnd] = true // open while nothing runs: the user's own window
				continue
			}
			if b.deplacees[f.hwnd] || b.exclues[f.hwnd] {
				continue
			}
			if r, _, err := b.appel("/GetDesktop:"+strconv.Itoa(n), "/MoveWindowHandle:"+strconv.FormatUint(uint64(f.hwnd), 10)); err == nil && r >= 0 {
				b.deplacees[f.hwnd] = true
				b.nbDeplacees++
			}
		}
		b.vues = vues
		b.mu.Unlock()
	}
}

type EtatBureau struct {
	Disponible bool     `json:"disponible"`
	Message    string   `json:"message"`
	Deplacees  int      `json:"deplacees"`
	Vues       []string `json:"vues"`
}

func (b *Bureau) Etat() EtatBureau {
	b.mu.Lock()
	defer b.mu.Unlock()
	return EtatBureau{b.disponible, b.message, b.nbDeplacees, append([]string{}, b.vues...)}
}

func (b *Bureau) Voir() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.disponible {
		return errors.New(b.message)
	}
	if n, _, err := b.appel("/GetCurrentDesktop"); err == nil && n >= 0 && n != b.numClaude {
		b.retour = n
	}
	_, _, err := b.appel("/Switch:" + strconv.Itoa(b.numClaude))
	return err
}

func (b *Bureau) Revenir() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.disponible {
		return errors.New(b.message)
	}
	_, _, err := b.appel("/Switch:" + strconv.Itoa(b.retour))
	return err
}

func (b *Bureau) RamenerTout() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.disponible {
		return errors.New(b.message)
	}
	for h := range b.deplacees {
		b.appel("/GetDesktop:"+strconv.Itoa(b.retour), "/MoveWindowHandle:"+strconv.FormatUint(uint64(h), 10))
		b.exclues[h] = true
	}
	b.deplacees = map[uintptr]bool{}
	return nil
}
