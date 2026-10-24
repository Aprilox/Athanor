//go:build windows

package main

// Claude's isolated desktop: a Windows desktop of its own (like the one of the UAC prompt), in the
// user's session. The programs the pilots start there (Unreal, Blender, a game under test) and all
// the programs they start in turn never show on the user's screen and can never take the focus:
// a full-screen game keeps running undisturbed. The user can look at it at any time ("View Claude's
// desktop"), with a bar at the top of the screen to come back.
//
// The gateways of the kit start these programs through "Athanor.exe --isole [--detache] -- PROGRAM
// ARGUMENTS", which works even when the application window is closed.

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const nomBureauIsole = "AthanorClaude"

var (
	pCreateDesktopW          = user32.NewProc("CreateDesktopW")
	pOpenDesktopW            = user32.NewProc("OpenDesktopW")
	pOpenInputDesktop        = user32.NewProc("OpenInputDesktop")
	pSwitchDesktop           = user32.NewProc("SwitchDesktop")
	pSetThreadDesktop        = user32.NewProc("SetThreadDesktop")
	pCloseDesktop            = user32.NewProc("CloseDesktop")
	pGetUserObjectInfoW      = user32.NewProc("GetUserObjectInformationW")
	pRegisterClassExW        = user32.NewProc("RegisterClassExW")
	pCreateWindowExW         = user32.NewProc("CreateWindowExW")
	pDefWindowProcW          = user32.NewProc("DefWindowProcW")
	pTranslateMessage        = user32.NewProc("TranslateMessage")
	pDispatchMessageW        = user32.NewProc("DispatchMessageW")
	pBeginPaint              = user32.NewProc("BeginPaint")
	pEndPaint                = user32.NewProc("EndPaint")
	pFillRect                = user32.NewProc("FillRect")
	pDrawTextW               = user32.NewProc("DrawTextW")
	pGetClientRect           = user32.NewProc("GetClientRect")
	pGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	pLoadCursorW             = user32.NewProc("LoadCursorW")
	pRegisterHotKey          = user32.NewProc("RegisterHotKey")
	gdi32                    = syscall.NewLazyDLL("gdi32.dll")
	pCreateSolidBrush        = gdi32.NewProc("CreateSolidBrush")
	pSetTextColor            = gdi32.NewProc("SetTextColor")
	pSetBkMode               = gdi32.NewProc("SetBkMode")
	pCreateFontW             = gdi32.NewProc("CreateFontW")
	pSelectObject            = gdi32.NewProc("SelectObject")
	pCreateJobObjectW        = kernel32.NewProc("CreateJobObjectW")
	pSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
	pAssignProcessToJob      = kernel32.NewProc("AssignProcessToJobObject")
	pResumeThread            = kernel32.NewProc("ResumeThread")
	pWaitForInputIdle        = user32.NewProc("WaitForInputIdle")
	pSetTimer                = user32.NewProc("SetTimer")
	pSetWindowPos            = user32.NewProc("SetWindowPos")
)

const accesBureau = 0x10000000 // GENERIC_ALL

// ouvrirBureauIsole opens Claude's desktop, creating it the first time.
func ouvrirBureauIsole() (uintptr, error) {
	nom, _ := syscall.UTF16PtrFromString(nomBureauIsole)
	if h, _, _ := pOpenDesktopW.Call(uintptr(unsafe.Pointer(nom)), 0, 0, accesBureau); h != 0 {
		return h, nil
	}
	h, _, err := pCreateDesktopW.Call(uintptr(unsafe.Pointer(nom)), 0, 0, 0, accesBureau, 0)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

// ouvrirBureauIsoleExistant opens Claude's desktop only if it exists (programs still running there).
func ouvrirBureauIsoleExistant() uintptr {
	nom, _ := syscall.UTF16PtrFromString(nomBureauIsole)
	h, _, _ := pOpenDesktopW.Call(uintptr(unsafe.Pointer(nom)), 0, 0, accesBureau)
	return h
}

// nomBureauActif is the name of the desktop on the screen right now ("Default", "AthanorClaude"…).
func nomBureauActif() string {
	h, _, _ := pOpenInputDesktop.Call(0, 0, 0x0001) // DESKTOP_READOBJECTS
	if h == 0 {
		return ""
	}
	defer pCloseDesktop.Call(h)
	buf := make([]uint16, 256)
	var n uint32
	if r, _, _ := pGetUserObjectInfoW.Call(h, 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&n))); r == 0 { // UOI_NAME
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// -------------------------------------------------------------------- Athanor.exe --isole (gateways)

type limitesJob struct { // JOBOBJECT_EXTENDED_LIMIT_INFORMATION (64-bit layout)
	tempsProcessus, tempsJob int64
	drapeaux                 uint32
	minMemoire, maxMemoire   uintptr
	maxProcessus             uint32
	affinite                 uintptr
	priorite, ordonnancement uint32
	io                       [6]uint64
	memProcessus, memJob     uintptr
	picProcessus, picJob     uintptr
}

// LancerIsole runs a program on Claude's desktop and returns its exit code. Its output goes to the
// standard output of Athanor.exe (the gateway shows it live). Without --detache, Athanor.exe waits for
// the program, and stopping Athanor.exe (a cancelled command) stops the program and everything it started.
// If the desktop cannot be created, the program runs on the current desktop: the work goes on.
func LancerIsole(args []string) int {
	detache, ici, finTout, fichierPID := false, false, false, ""
	for len(args) > 0 && args[0] != "--" {
		switch {
		case args[0] == "--detache":
			detache = true
		case args[0] == "--ici": // on the current desktop (a Windows MCP server when Claude's desktop is off)
			ici = true
		case args[0] == "--fin-tout": // native Windows pilot: whatever the program left running stops with it
			finTout = true
		case args[0] == "--pid" && len(args) > 1: // the gateway stops this process tree (taskkill /T) on cancel
			fichierPID = args[1]
			args = args[1:]
		}
		args = args[1:]
	}
	if fichierPID != "" {
		_ = os.WriteFile(fichierPID, []byte(strconv.Itoa(os.Getpid())), 0o644)
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: Athanor.exe --isole [--detache] [--ici] [--fin-tout] [--pid FILE] -- PROGRAM [ARGUMENTS...]")
		return 2
	}
	if estScriptShellPilote(args[0]) {
		if b := bashNatif(); b != "" {
			args = append([]string{b, strings.ReplaceAll(args[0], `\`, "/")}, args[1:]...)
		}
	}
	var nomBureau *uint16 // nil: the current desktop
	if !ici {
		if h, err := ouvrirBureauIsole(); err == nil {
			defer pCloseDesktop.Call(h) // the desktop lives on as long as programs run on it
			nomBureau, _ = syscall.UTF16PtrFromString(`WinSta0\` + nomBureauIsole)
		} else {
			fmt.Fprintln(os.Stderr, "Athanor: Claude's desktop is not available (", err, "): the program opens on the current desktop.")
		}
	}
	parties := make([]string, len(args))
	for i, a := range args {
		parties[i] = syscall.EscapeArg(a)
	}
	texteLigne := strings.Join(parties, " ")
	// cmd.exe /c with several quoted arguments (a .bat in "Program Files", a path with spaces): cmd only
	// keeps the line intact between /s /c "…" (it removes the first and last quote, nothing else).
	if b := strings.ToLower(args[0]); len(args) > 2 && (b == "cmd.exe" || b == "cmd" || strings.HasSuffix(b, `\cmd.exe`)) && strings.EqualFold(args[1], "/c") {
		texteLigne = parties[0] + ` /s /c "` + strings.Join(entreGuillemetsCmd(args[2:], parties[2:]), " ") + `"`
	}
	ligne, err := syscall.UTF16PtrFromString(texteLigne)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor:", err)
		return 2
	}
	si := &syscall.StartupInfo{Desktop: nomBureau}
	si.Cb = uint32(unsafe.Sizeof(*si))
	heriter := false
	if !detache { // the output comes back to the gateway; a detached program (the editor) keeps nothing open
		for i, n := range []int{syscall.STD_INPUT_HANDLE, syscall.STD_OUTPUT_HANDLE, syscall.STD_ERROR_HANDLE} {
			h, e := syscall.GetStdHandle(n)
			if e != nil || h == syscall.InvalidHandle || h == 0 {
				continue
			}
			syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT)
			switch i {
			case 0:
				si.StdInput = h
			case 1:
				si.StdOutput = h
			case 2:
				si.StdErr = h
			}
			heriter = true
		}
		if heriter {
			si.Flags |= syscall.STARTF_USESTDHANDLES
		}
	}
	drapeaux := uint32(createNoWindow) // console programs: no console window (ignored for windowed programs)
	if !detache {
		drapeaux |= 0x4 // CREATE_SUSPENDED: placed in the job before it runs
	}
	var pi syscall.ProcessInformation
	if detache {
		// out of the job Athanor.exe runs in (a native Windows cycle's, see finTout): a detached program (an
		// editor) stays open after the cycle; refused by a job that does not allow it, it starts in it
		err := syscall.CreateProcess(nil, ligne, nil, nil, heriter, drapeaux|createBreakawayFromJob, nil, nil, si, &pi)
		if err != nil {
			err = syscall.CreateProcess(nil, ligne, nil, nil, heriter, drapeaux, nil, nil, si, &pi)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Athanor: cannot start", args[0], ":", err)
			return 127
		}
	} else if err := syscall.CreateProcess(nil, ligne, nil, nil, heriter, drapeaux, nil, nil, si, &pi); err != nil {
		fmt.Fprintln(os.Stderr, "Athanor: cannot start", args[0], ":", err)
		if err == syscall.Errno(740) { // ERROR_ELEVATION_REQUIRED: pc-exec retries through the administrator task
			return 740
		}
		return 127
	}
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	if detache {
		// A program connects to its desktop when its window code starts: until then, our handle keeps
		// Claude's desktop alive (with the application closed, nothing else does).
		pWaitForInputIdle.Call(uintptr(pi.Process), 30000)
		return 0
	}
	// Job "kill on close": when Athanor.exe is stopped (command cancelled), the program and its children
	// stop too. Lifted once the program ends normally: what it started on purpose keeps running.
	job, _, _ := pCreateJobObjectW.Call(0, 0)
	if job != 0 {
		l := limitesJob{drapeaux: 0x2000} // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if finTout {
			l.drapeaux |= 0x800 // JOB_OBJECT_LIMIT_BREAKAWAY_OK: only for what asks for it (a detached editor)
		}
		pSetInformationJobObject.Call(job, 9, uintptr(unsafe.Pointer(&l)), unsafe.Sizeof(l))
		pAssignProcessToJob.Call(job, uintptr(pi.Process))
	}
	pResumeThread.Call(uintptr(pi.Thread))
	syscall.WaitForSingleObject(pi.Process, syscall.INFINITE)
	if job != 0 && !finTout {
		l := limitesJob{}
		pSetInformationJobObject.Call(job, 9, uintptr(unsafe.Pointer(&l)), unsafe.Sizeof(l))
	}
	var code uint32
	syscall.GetExitCodeProcess(pi.Process, &code)
	return int(code)
}

// -------------------------------------------------------------------- looking at Claude's desktop

// barreRetour is the bar at the top of Claude's desktop: a click (or Ctrl + Alt + B) brings the user back.
// Created once, on Claude's desktop, by a thread of its own; it stays there while the application runs.
type barreRetour struct {
	once    sync.Once
	pret    chan struct{}
	err     error
	revenir func()
	texte   string
}

var classeBarre, _ = syscall.UTF16PtrFromString("AthanorRetour")

func (r *barreRetour) demarrer(bureau uintptr) error {
	r.once.Do(func() {
		r.pret = make(chan struct{})
		go r.boucle(bureau)
	})
	<-r.pret
	return r.err
}

func (r *barreRetour) boucle(bureau uintptr) {
	runtime.LockOSThread() // for good: this thread belongs to Claude's desktop now
	if ok, _, err := pSetThreadDesktop.Call(bureau); ok == 0 {
		r.err = fmt.Errorf("SetThreadDesktop: %v", err)
		close(r.pret)
		return
	}
	fond, _, _ := pCreateSolidBrush.Call(0x302318) // BGR: dark slate
	police, _ := syscall.UTF16PtrFromString("Segoe UI")
	fonte, _, _ := pCreateFontW.Call(uintptr(0xFFFFFFEA), 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(police))) // -22 px, semi-bold, ClearType
	texte, _ := syscall.UTF16FromString(r.texte)
	proc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		switch msg {
		case 0x0202, 0x0312: // WM_LBUTTONUP, WM_HOTKEY
			go r.revenir()
			return 0
		case 0x0113: // WM_TIMER: stays above a window that put itself "always on top" since
			pSetWindowPos.Call(h, ^uintptr(0), 0, 0, 0, 0, 0x1|0x2|0x10) // HWND_TOPMOST; SWP_NOSIZE | SWP_NOMOVE | SWP_NOACTIVATE
			return 0
		case 0x000F: // WM_PAINT
			var ps [72]byte
			dc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
			var rc [4]int32
			pGetClientRect.Call(h, uintptr(unsafe.Pointer(&rc[0])))
			pFillRect.Call(dc, uintptr(unsafe.Pointer(&rc[0])), fond)
			pSelectObject.Call(dc, fonte)
			pSetTextColor.Call(dc, 0xF2F2F2)
			pSetBkMode.Call(dc, 1)                                                                                                        // TRANSPARENT
			pDrawTextW.Call(dc, uintptr(unsafe.Pointer(&texte[0])), uintptr(len(texte)-1), uintptr(unsafe.Pointer(&rc[0])), 0x1|0x4|0x20) // centered, one line
			pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
			return 0
		}
		r0, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
		return r0
	})
	curseur, _, _ := pLoadCursorW.Call(0, 32649) // IDC_HAND
	type wndclassex struct {
		cbSize, style                uint32
		proc                         uintptr
		clsExtra, wndExtra           int32
		instance, icon, cursor, fond uintptr
		menu, classe                 *uint16
		iconSm                       uintptr
	}
	wc := wndclassex{proc: proc, cursor: curseur, fond: fond, classe: classeBarre}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	largeurEcran, _, _ := pGetSystemMetrics.Call(0) // SM_CXSCREEN
	l, haut := int32(640), int32(46)
	x := (int32(largeurEcran) - l) / 2
	titre, _ := syscall.UTF16PtrFromString("Athanor")
	h, _, err := pCreateWindowExW.Call(0x8|0x80, uintptr(unsafe.Pointer(classeBarre)), uintptr(unsafe.Pointer(titre)), // WS_EX_TOPMOST | WS_EX_TOOLWINDOW
		0x80000000|0x10000000, uintptr(x), 0, uintptr(l), uintptr(haut), 0, 0, 0, 0) // WS_POPUP | WS_VISIBLE
	if h == 0 {
		r.err = fmt.Errorf("CreateWindowEx: %v", err)
		close(r.pret)
		return
	}
	if ok, _, _ := pRegisterHotKey.Call(h, 1, 0x1|0x2|0x4000, 'B'); ok == 0 { // Ctrl + Alt + B (MOD_NOREPEAT)
		// shortcut taken by another program: the bar no longer mentions it (clicking it still works)
		texte, _ = syscall.UTF16FromString(strings.SplitN(r.texte, "   (", 2)[0])
	}
	pSetTimer.Call(h, 1, 1000, 0)
	close(r.pret)
	var msg [48]byte
	for {
		if v, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(v) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

// texteBarre: the bar's label, in the application's language.
func texteBarre(langue string) string {
	switch langue {
	case "fr":
		return "←  Revenir à mon bureau   (Ctrl + Alt + B)"
	case "es":
		return "←  Volver a mi escritorio   (Ctrl + Alt + B)"
	case "de":
		return "←  Zurück zu meinem Desktop   (Strg + Alt + B)"
	case "it":
		return "←  Torna al mio desktop   (Ctrl + Alt + B)"
	case "pt":
		return "←  Voltar ao meu ambiente de trabalho   (Ctrl + Alt + B)"
	}
	return "←  Back to my desktop   (Ctrl + Alt + B)"
}

// basculer shows a desktop; "Default" is the user's.
func basculer(nom string) error {
	n, _ := syscall.UTF16PtrFromString(nom)
	h, _, err := pOpenDesktopW.Call(uintptr(unsafe.Pointer(n)), 0, 0, 0x0100) // DESKTOP_SWITCHDESKTOP
	if h == 0 {
		return err
	}
	defer pCloseDesktop.Call(h)
	if ok, _, err := pSwitchDesktop.Call(h); ok == 0 {
		return err
	}
	return nil
}

// remettreBureau brings the user back to their desktop if Claude's is on the screen (application closing).
func remettreBureau() {
	if nomBureauActif() == nomBureauIsole {
		basculer("Default")
	}
}

// -------------------------------------------------------------------- watchdog

var gardienLance sync.Once

// lancerGardien starts, once, "Athanor.exe --retour PID": if the application disappears (crash, task
// manager) while Claude's desktop is on the screen, it brings the user back to their desktop.
func lancerGardien() {
	gardienLance.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		c := commande(exe, "--retour", strconv.Itoa(os.Getpid()))
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob}
		if c.Start() != nil {
			c = commande(exe, "--retour", strconv.Itoa(os.Getpid()))
			c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup}
			_ = c.Start()
		}
		if c.Process != nil {
			_ = c.Process.Release()
		}
	})
}

// GardienRetour (Athanor.exe --retour PID): waits for the application to end, then shows the user's
// desktop again if Claude's is still on the screen.
func GardienRetour(args []string) int {
	if len(args) == 0 {
		return 2
	}
	pid, err := strconv.Atoi(args[0])
	if err != nil {
		return 2
	}
	h, err := syscall.OpenProcess(0x00100000, false, uint32(pid)) // SYNCHRONIZE
	if err != nil {
		return 1
	}
	syscall.WaitForSingleObject(h, syscall.INFINITE)
	remettreBureau()
	return 0
}
