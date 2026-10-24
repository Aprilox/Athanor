//go:build windows

package main

// Windows part: WSL commands without a window, application window (Edge), start with
// Windows, and the "Claude" virtual desktop (VirtualDesktop11 utility by Markus Scholtes, MIT,
// compiled on first launch with the C# compiler shipped with Windows).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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





// demarrerFenetre opens a visible console of its own (CreateProcess, CREATE_NEW_CONSOLE), without
// handing it any of the application's standard handles: started through "cmd /c start" from the
// application (which has no console), the window received an empty input, so every question, and
// the final "Press Enter", returned at once and the window closed straight away. No cmd in between:
// a "&" in a path (D:\Dev\R&D) is just a character of an argument.
func demarrerFenetre(titre string, args ...string) error {
	ligne := make([]string, len(args))
	for i, a := range args {
		ligne[i] = syscall.EscapeArg(a)
	}
	cl, err := syscall.UTF16PtrFromString(strings.Join(ligne, " "))
	if err != nil {
		return err
	}
	t, _ := syscall.UTF16PtrFromString(titre)
	si := &syscall.StartupInfo{Title: t}
	si.Cb = uint32(unsafe.Sizeof(*si))
	var pi syscall.ProcessInformation
	const createNewConsole, createUnicodeEnvironment = 0x00000010, 0x00000400
	if err := syscall.CreateProcess(nil, cl, nil, nil, false, createNewConsole|createUnicodeEnvironment, nil, nil, si, &pi); err != nil {
		return err
	}
	syscall.CloseHandle(pi.Thread)
	syscall.CloseHandle(pi.Process)
	return nil
}


// Ouvrir shows a folder in Explorer, or opens a file with its associated program.
func Ouvrir(chemin string) error {
	if _, err := os.Stat(chemin); err != nil {
		return fmt.Errorf("Not created yet: %s", chemin)
	}
	return commande("explorer.exe", chemin).Start()
}

// FenetreApp opens the interface in a real application window (WebView2, shipped with
// Windows 11). Blocking: call it from the main thread. Returns false when WebView2 is missing.
func FenetreApp(url string) bool {
	runtime.LockOSThread()
	preparerCadre()
	webview2.WndProcHook = messageFenetre
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
	installerCadre(w)
	installerFermeture(w)
	w.Navigate(url)
	fenetreHWND.Store(uintptr(w.Window()))
	fenetreOuverte.Store(true)
	w.Run()
	fenetreOuverte.Store(false)
	fenetreHWND.Store(0)
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
			c := commande(exe, "--app="+url, "--user-data-dir="+profil, "--window-size=1240,820", "--no-first-run", "--no-default-browser-check")
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
		c = commande("reg.exe", "add", cleRun, "/v", nomRun, "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\"", exe), "/f")
	} else {
		c = commande("reg.exe", "delete", cleRun, "/v", nomRun, "/f")
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
	q := commande("reg.exe", "query", cleRun, "/v", "ClaudeAutopilote")
	cacherFenetre(q)
	if q.Run() == nil {
		d := commande("reg.exe", "delete", cleRun, "/v", "ClaudeAutopilote", "/f")
		cacherFenetre(d)
		_ = d.Run()
		_ = DemarrageWindows(true)
	}
}

func DemarrageWindowsActif() bool {
	c := commande("reg.exe", "query", cleRun, "/v", nomRun)
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
	pEnumDesktopWindows         = user32.NewProc("EnumDesktopWindows")
)

type fenetre struct {
	hwnd  uintptr
	titre string
	exe   string
	pid   uint32
}

// One callback for the whole program: Go allows only a limited number of callbacks (about 2000) and
// never frees them, so a closure created at every scan would end the application after a while.
var (
	enumMu    sync.Mutex
	enumListe []fenetre
	enumMoi   uintptr
	enumCB    = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		ex, _, _ := pGetWindowLongW.Call(hwnd, uintptr(0xFFFFFFEC)) // GWL_EXSTYLE = -20
		if uint32(ex)&0x80 != 0 {                                   // WS_EX_TOOLWINDOW
			return 1
		}
		var pid uint32 // checked first: asking our own window for its title could wait on our own threads
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || uintptr(pid) == enumMoi {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		if n == 0 {
			return 1
		}
		enumListe = append(enumListe, fenetre{hwnd, syscall.UTF16ToString(buf[:n]), exeDuPid(pid), pid})
		return 1
	})
)

// listerFenetres: the visible, titled windows of other programs on the current desktop.
func listerFenetres() []fenetre {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumListe = nil
	enumMoi, _, _ = pGetCurrentProcessId.Call()
	pEnumWindows.Call(enumCB, 0)
	return enumListe
}

// fenetresDuBureau: the same, on another desktop (Claude's isolated desktop).
func fenetresDuBureau(bureau uintptr) []fenetre {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumListe = nil
	enumMoi, _, _ = pGetCurrentProcessId.Call()
	pEnumDesktopWindows.Call(bureau, enumCB, 0)
	return enumListe
}

// exeDuPid returns the full path of a process's executable ("" when unknown); kept in a small cache
// because the window hook asks for it on every new window.
// Entries live 10 s: a process number can be given again to another program.
type exeConnu struct {
	exe   string
	quand time.Time
}

var (
	cacheExeMu sync.Mutex
	cacheExe   = map[uint32]exeConnu{}
)

func exeDuPid(pid uint32) string {
	cacheExeMu.Lock()
	defer cacheExeMu.Unlock()
	if e, ok := cacheExe[pid]; ok && time.Since(e.quand) < 10*time.Second {
		return e.exe
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
	if len(cacheExe) > 512 {
		cacheExe = map[uint32]exeConnu{}
	}
	if exe != "" {
		cacheExe[pid] = exeConnu{exe, time.Now()}
	}
	return exe
}

var (
	pSetWinEventHook            = user32.NewProc("SetWinEventHook")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pGetAncestor                = user32.NewProc("GetAncestor")
	pIsWindow                   = user32.NewProc("IsWindow")
	pSetWindowLongW             = user32.NewProc("SetWindowLongW")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
)

const (
	gwlExStyle     = uintptr(0xFFFFFFEC) // GWL_EXSTYLE = -20 (an int: the upper half is ignored)
	wsExToolwindow = 0x80
	wsExLayered    = 0x80000
	evObjetAffiche = 0x8002 // EVENT_OBJECT_SHOW
)

// rendreInvisible makes a window fully transparent while it is sent to the "Claude" desktop, so it
// never shows on the user's screen. It returns the original extended style to put back, or false
// when the window cannot be made transparent (already layered: it is then only moved).
func rendreInvisible(h uintptr) (uint32, bool) {
	ex, _, _ := pGetWindowLongW.Call(h, gwlExStyle)
	if uint32(ex)&wsExLayered != 0 {
		return 0, false
	}
	pSetWindowLongW.Call(h, gwlExStyle, uintptr(uint32(ex)|wsExLayered))
	if r, _, _ := pSetLayeredWindowAttributes.Call(h, 0, 0, 2); r == 0 { // LWA_ALPHA, alpha 0
		pSetWindowLongW.Call(h, gwlExStyle, uintptr(uint32(ex)))
		return 0, false
	}
	return uint32(ex), true
}

func rendreVisible(h uintptr, ex uint32) { pSetWindowLongW.Call(h, gwlExStyle, uintptr(ex)) }

// pasMasquee: a window being handled whose style was not changed (nothing to put back).
const pasMasquee = ^uint32(0)

type Bureau struct {
	mu            sync.Mutex // settings, state shown in the app, desktop switches
	mf            sync.Mutex // the window maps below (short critical sections only)
	mh            sync.Mutex // one call to the utility at a time
	kit           Kit
	helper        string
	disponible    bool
	message       string
	numClaude     int // number of the "Claude" desktop (-1 = unknown)
	verifie       time.Time
	deplacees     map[uintptr]bool
	exclues       map[uintptr]bool   // windows the user brought back: never moved again
	masquees      map[uintptr]uint32 // windows made transparent, on their way (original extended style)
	aDeplacer     chan uintptr
	regles        atomic.Value // *reglesBureau: what the window hook needs, without waiting for mu
	PilotesActifs func() bool  // set by the server: at least one pilot running
	vues          []string
	nbDeplacees   int
	retour        int
	dossiers      []string
	Programmes    []string
	Actif         bool
	isole         uintptr // Claude's isolated desktop (0 = not available: virtual desktop instead)
	restantes     int     // windows open on it
	barre         barreRetour
	Langue        func() string
}

// reglesBureau is a snapshot read by the window hook: it must answer at once, even while the
// loop or a desktop switch holds mu.
type reglesBureau struct {
	actif      bool // option on, desktop ready, at least one pilot running
	numClaude  int
	programmes []string
	dossiers   []string
}

func NouveauBureau(k Kit) *Bureau {
	b := &Bureau{kit: k, numClaude: -1, retour: 0, deplacees: map[uintptr]bool{}, exclues: map[uintptr]bool{},
		masquees: map[uintptr]uint32{}, aDeplacer: make(chan uintptr, 64), Actif: true}
	b.regles.Store(&reglesBureau{numClaude: -1})
	go b.boucle()
	go b.ecouter()
	go b.deplaceur()
	return b
}

func (b *Bureau) DefinirKit(k Kit) {
	b.mu.Lock()
	if b.kit.Racine != "" && b.kit.Racine != k.Racine { // the previous tool folder's gateways stop isolating
		os.Remove(filepath.Join(b.kit.Racine, "logs", "bureau-isole"))
	}
	b.kit = k
	b.mu.Unlock()
}
func (b *Bureau) DefinirDossiers(d []string) { b.mu.Lock(); b.dossiers = d; b.mu.Unlock() }

// DefinirReglages changes the activation and the list of programs (under lock: the loop reads them).
func (b *Bureau) DefinirReglages(actif bool, programmes []string) {
	b.mu.Lock()
	b.Actif, b.Programmes = actif, programmes
	if !actif {
		b.regles.Store(&reglesBureau{numClaude: -1})
	}
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
	c := commande(csc, "/nologo", "/target:exe", "/out:"+exe, src)
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
	b.mh.Lock()
	defer b.mh.Unlock()
	c := commande(b.helper, append([]string{"/Quiet"}, args...)...)
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

func appartient(r *reglesBureau, exe string) bool {
	exe = strings.ToLower(exe)
	if exe == "" {
		return false
	}
	base := exe[strings.LastIndex(exe, "\\")+1:]
	for _, p := range r.programmes {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && (base == p || strings.HasPrefix(base, strings.TrimSuffix(p, ".exe"))) {
			return true
		}
	}
	for _, d := range r.dossiers {
		if d != "" && strings.HasPrefix(exe, strings.ToLower(d)) {
			return true
		}
	}
	// (No rule on the title: the application itself and its installation windows must stay here.)
	return false
}

// ecouter asks Windows to report every window that appears (out-of-context event hook, on a
// thread of its own with its message loop): a pilot's window is caught the moment it shows,
// made transparent and handed to the mover, instead of waiting for the next scan.
func (b *Bureau) ecouter() {
	runtime.LockOSThread()
	cb := syscall.NewCallback(func(hook, ev, hwnd, objet, enfant, thread, temps uintptr) uintptr {
		if int32(objet) != 0 || int32(enfant) != 0 || hwnd == 0 { // OBJID_WINDOW, CHILDID_SELF
			return 0
		}
		if racine, _, _ := pGetAncestor.Call(hwnd, 2); racine != hwnd { // GA_ROOT: top-level windows only
			return 0
		}
		b.nouvelleFenetre(hwnd)
		return 0
	})
	// WINEVENT_OUTOFCONTEXT | WINEVENT_SKIPOWNPROCESS
	if h, _, _ := pSetWinEventHook.Call(evObjetAffiche, evObjetAffiche, 0, cb, 0, 0, 0x0000|0x0002); h == 0 {
		return // the scan of the loop still does the job
	}
	var msg [48]byte
	for {
		if r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
			return
		}
	}
}

func (b *Bureau) nouvelleFenetre(h uintptr) {
	r, _ := b.regles.Load().(*reglesBureau)
	if r == nil || !r.actif || r.numClaude < 0 {
		return
	}
	if ex, _, _ := pGetWindowLongW.Call(h, gwlExStyle); uint32(ex)&wsExToolwindow != 0 {
		return // tooltips, menus, floating palettes: they follow their owner
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || !appartient(r, exeDuPid(pid)) {
		return
	}
	b.mf.Lock()
	_, deja := b.masquees[h]
	if b.deplacees[h] || b.exclues[h] {
		b.mf.Unlock()
		return
	}
	if !deja {
		b.masquees[h] = pasMasquee // reserved: the style change below is done outside the lock
	}
	b.mf.Unlock()
	if !deja {
		// (changing the style of another program's window waits for it: never under mf)
		if ex, ok := rendreInvisible(h); ok {
			b.mf.Lock()
			b.masquees[h] = ex
			b.mf.Unlock()
		}
	}
	select {
	case b.aDeplacer <- h:
	default: // queue full: the loop's scan catches it
	}
}

// deplaceur sends the caught windows to the "Claude" desktop, then makes them visible again there.
// A window that has just appeared is not always movable yet: a few tries, 120 ms apart.
func (b *Bureau) deplaceur() {
	for h := range b.aDeplacer {
		r, _ := b.regles.Load().(*reglesBureau)
		ok := false
		for essai := 0; r != nil && r.numClaude >= 0 && essai < 6 && !ok; essai++ {
			if essai > 0 {
				time.Sleep(120 * time.Millisecond)
			}
			if v, _, _ := pIsWindow.Call(h); v == 0 {
				break
			}
			if n, _, err := b.appel("/GetDesktop:"+strconv.Itoa(r.numClaude), "/MoveWindowHandle:"+strconv.FormatUint(uint64(h), 10)); err == nil && n >= 0 {
				ok = true
			}
		}
		b.mf.Lock()
		ex, masquee := b.masquees[h]
		delete(b.masquees, h)
		if ok && !b.deplacees[h] {
			b.deplacees[h] = true
			b.nbDeplacees++
		}
		b.mf.Unlock()
		if masquee && ex != pasMasquee {
			rendreVisible(h, ex) // on the "Claude" desktop, or back here if it could not be moved
		}
	}
}

func (b *Bureau) boucle() {
	for {
		time.Sleep(1500 * time.Millisecond)
		// Read before taking mu: PilotesActifs takes the server's lock, and the server calls the
		// Definir* setters (which take mu) while holding it.
		actifs := b.PilotesActifs == nil || b.PilotesActifs()
		b.mu.Lock()
		if !b.Actif {
			b.message = "Virtual desktop disabled (Options)."
			b.marquerIsole(false)
			b.regles.Store(&reglesBureau{numClaude: -1})
			// Programs started while the option was on stay on Claude's desktop until they close: it stays
			// reachable ("View Claude's desktop") as long as windows remain there, to go and close them.
			if b.isole == 0 {
				b.isole = ouvrirBureauIsoleExistant()
			}
			b.restantes = 0
			if b.isole != 0 {
				b.restantes = len(fenetresDuBureau(b.isole))
			}
			b.disponible = b.restantes > 0
			b.mu.Unlock()
			continue
		}
		// Claude's isolated desktop: the gateways start the programs there (logs/bureau-isole tells them to),
		// so nothing has to be moved. The virtual desktop below is only the fallback.
		if b.isole == 0 {
			if h, err := ouvrirBureauIsole(); err == nil {
				b.isole = h // kept open while the application runs
			}
		}
		if b.isole != 0 {
			b.marquerIsole(true)
			b.restantes = len(fenetresDuBureau(b.isole))
			b.disponible = true
			b.message = "“Claude” desktop ready: the programs Claude starts open there, never on your screen."
			b.regles.Store(&reglesBureau{numClaude: -1})
			b.mu.Unlock()
			continue
		}
		if err := b.preparerHelper(); err != nil {
			b.disponible, b.message = false, err.Error()
			b.mu.Unlock()
			continue
		}
		// The desktop is checked every 30 s (the user may remove it), not at every scan.
		if b.numClaude < 0 || time.Since(b.verifie) > 30*time.Second {
			if _, err := b.assurerBureau(); err != nil {
				b.disponible, b.message = false, err.Error()
				b.regles.Store(&reglesBureau{numClaude: -1})
				b.mu.Unlock()
				continue
			}
			b.verifie = time.Now()
		}
		n := b.numClaude
		b.disponible = true
		b.message = "“Claude” desktop ready: the pilots' windows are sent there automatically."
		// Only while a pilot is running (actifs): the user's own use of the same programs is never touched
		// when nothing is working, and a window brought back by the user stays where the user put it.
		r := &reglesBureau{actif: actifs, numClaude: n, programmes: append([]string{}, b.Programmes...),
			dossiers: append([]string{filepath.Join(b.kit.Racine, "tools")}, b.dossiers...)}
		b.regles.Store(r)
		b.mu.Unlock()

		// Scan: windows the hook missed (opened before the app, or while the queue was full).
		var vues []string
		var aBouger []uintptr
		for _, f := range listerFenetres() {
			if !appartient(r, f.exe) {
				continue
			}
			vues = append(vues, f.titre+" — "+f.exe[strings.LastIndex(f.exe, "\\")+1:])
			b.mf.Lock()
			if !actifs {
				b.exclues[f.hwnd] = true // open while nothing runs: the user's own window
			} else if _, enCours := b.masquees[f.hwnd]; !b.deplacees[f.hwnd] && !b.exclues[f.hwnd] && !enCours {
				aBouger = append(aBouger, f.hwnd)
			}
			b.mf.Unlock()
		}
		for _, h := range aBouger {
			if r2, _, err := b.appel("/GetDesktop:"+strconv.Itoa(n), "/MoveWindowHandle:"+strconv.FormatUint(uint64(h), 10)); err == nil && r2 >= 0 {
				b.mf.Lock()
				b.deplacees[h] = true
				b.nbDeplacees++
				b.mf.Unlock()
			}
		}
		b.mu.Lock()
		b.vues = vues
		b.mu.Unlock()
	}
}

// marquerIsole writes or removes logs/bureau-isole in the tool folder: the gateways read it to start
// the programs on Claude's desktop (Athanor.exe --isole), even when the application is closed.
func (b *Bureau) marquerIsole(oui bool) {
	if b.kit.Racine == "" {
		return
	}
	f := filepath.Join(b.kit.Racine, "logs", "bureau-isole")
	if !oui {
		os.Remove(f)
		return
	}
	if _, err := os.Stat(f); err != nil {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(nomBureauIsole+"\n"), 0o644)
	}
}

type EtatBureau struct {
	Disponible bool     `json:"disponible"`
	Message    string   `json:"message"`
	Deplacees  int      `json:"deplacees"`
	Vues       []string `json:"vues"`
	Isole      bool     `json:"isole"`
	Restantes  int      `json:"restantes"` // windows still open on Claude's isolated desktop
	Ecran      bool     `json:"ecran"`     // Linux: "View" shows Claude's screen (always false here)
}

func (b *Bureau) Etat() EtatBureau {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mf.Lock()
	nb := b.nbDeplacees
	b.mf.Unlock()
	return EtatBureau{b.disponible, b.message, nb, append([]string{}, b.vues...), b.isole != 0, b.restantes, false}
}

func (b *Bureau) Voir() error {
	langue := "" // read before taking mu (Langue takes the server's lock)
	if b.Langue != nil {
		langue = b.Langue()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.disponible {
		return errors.New(b.message)
	}
	if b.isole != 0 {
		// Never switch without the way back: the bar must exist on Claude's desktop first, and a
		// watchdog brings the user back if the application disappears meanwhile.
		if b.barre.texte == "" {
			b.barre.texte, b.barre.revenir = texteBarre(langue), func() { basculer("Default") }
		}
		if err := b.barre.demarrer(b.isole); err != nil {
			return fmt.Errorf("Cannot show Claude's desktop: %v", err)
		}
		lancerGardien()
		return basculer(nomBureauIsole)
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
	if b.isole != 0 { // always possible, even once nothing is left there
		return basculer("Default")
	}
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
	b.mf.Lock()
	var liste []uintptr
	for h := range b.deplacees {
		liste = append(liste, h)
		b.exclues[h] = true
	}
	b.deplacees = map[uintptr]bool{}
	b.mf.Unlock()
	for _, h := range liste {
		b.appel("/GetDesktop:"+strconv.Itoa(b.retour), "/MoveWindowHandle:"+strconv.FormatUint(uint64(h), 10))
	}
	return nil
}

// arreterArbre stops a command and everything it started (taskkill /T: the Windows process tree).
func arreterArbre(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	k := commande("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid))
	cacherFenetre(k)
	if k.Run() != nil {
		_ = c.Process.Kill()
	}
}
