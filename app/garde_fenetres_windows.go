//go:build windows

package main

// The window guard's Windows side (garde_fenetres.go has its decisions): an out-of-context event hook, on a
// thread of its own with its message loop, sees every top-level window created or shown on this desktop.
// A console or a Windows Terminal of a program Athanor started is made transparent at once, then hidden;
// the program goes on running without it.

import (
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pGetClassNameW   = user32.NewProc("GetClassNameW")
	pShowWindowAsync = user32.NewProc("ShowWindowAsync")
	gardeLancee      sync.Once
)

const (
	evFenetreCreee    = 0x8000                        // EVENT_OBJECT_CREATE
	evFenetreDetruite = 0x8001                        // EVENT_OBJECT_DESTROY
	evFenetreAffichee = 0x8002                        // EVENT_OBJECT_SHOW
	recentGarde       = int64(20 * time.Second / 100) // 20 s in Windows' 100 ns units
)

// demarrerGardeFenetres: the guard of the programs started, directly or not, by the process racine (this
// one: a pilot, the application), once per process; noter receives the line of each hidden window.
func demarrerGardeFenetres(racine uint32, noter func(string)) {
	gardeLancee.Do(func() { go gardeFenetres(racine, noter) })
}

func classeFenetre(h uintptr) string {
	b := make([]uint16, 128)
	n, _, _ := pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b[:n])
}

func titreFenetre(h uintptr) string {
	b := make([]uint16, 512)
	n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b[:n])
}

// processusMachine: every process, its parent, its program and when it started.
func processusMachine() []processusGarde {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []processusGarde
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		p := processusGarde{pid: e.ProcessID, ppid: e.ParentProcessID, exe: windows.UTF16ToString(e.ExeFile[:])}
		if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID); err == nil {
			var cree, fin, k, u windows.Filetime
			if windows.GetProcessTimes(h, &cree, &fin, &k, &u) == nil {
				p.cree = int64(cree.HighDateTime)<<32 | int64(cree.LowDateTime)
			}
			b := make([]uint16, 1024)
			n := uint32(len(b))
			if windows.QueryFullProcessImageName(h, 0, &b[0], &n) == nil {
				p.exe = windows.UTF16ToString(b[:n])
			}
			windows.CloseHandle(h)
		}
		out = append(out, p)
	}
	return out
}

func maintenantFiletime() int64 {
	var f windows.Filetime
	windows.GetSystemTimeAsFileTime(&f)
	return int64(f.HighDateTime)<<32 | int64(f.LowDateTime)
}

func gardeFenetres(racine uint32, noter func(string)) {
	runtime.LockOSThread()               // for good: the hook's events come to this thread's message loop
	notes := map[uintptr]bool{}          // windows hidden and written down
	terminaux := map[uintptr]time.Time{} // Windows Terminal windows just created
	cb := syscall.NewCallback(func(hook, ev, hwnd, objet, enfant, thread, temps uintptr) uintptr {
		if int32(objet) != 0 || int32(enfant) != 0 || hwnd == 0 { // OBJID_WINDOW, CHILDID_SELF
			return 0
		}
		if ev == evFenetreDetruite {
			delete(notes, hwnd)
			delete(terminaux, hwnd)
			return 0
		}
		if r, _, _ := pGetAncestor.Call(hwnd, 2); r != hwnd { // GA_ROOT: top-level windows only
			return 0
		}
		classe := classeFenetre(hwnd)
		if classe != classeConsoleGarde && classe != classeTerminalGarde {
			return 0
		}
		if classe == classeTerminalGarde {
			// only a new window, shown within seconds: a tab added to the user's own terminal never counts
			if ev == evFenetreCreee {
				terminaux[hwnd] = time.Now()
				return 0
			}
			if t, ok := terminaux[hwnd]; !ok || time.Since(t) > 20*time.Second {
				return 0
			}
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		nous := descendantsGarde(processusMachine(), racine)
		p, ok := fenetreAMasquerGarde(racine, classe, pid, titreFenetre(hwnd), nous, maintenantFiletime(), recentGarde)
		if !ok {
			return 0
		}
		if ev == evFenetreCreee {
			rendreInvisible(hwnd) // a console: transparent before it is ever drawn
		}
		pShowWindowAsync.Call(hwnd, 0) // SW_HIDE
		if !notes[hwnd] {
			notes[hwnd] = true
			if noter != nil {
				go noter(texteFenetreGarde(p, nous)) // (files written: never on the hook's thread)
			}
		}
		return 0
	})
	// WINEVENT_OUTOFCONTEXT | WINEVENT_SKIPOWNPROCESS
	if h, _, _ := pSetWinEventHook.Call(evFenetreCreee, evFenetreAffichee, 0, cb, 0, 0, 0x0000|0x0002); h == 0 {
		return
	}
	var msg [48]byte
	for {
		if r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
			return
		}
	}
}
