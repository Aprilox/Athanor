//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// The guard for real: a window of a console's class shown by a program a child of this process started
// (here this test program again, through cmd) is hidden at once and written down; the program goes on.
func TestGardeFenetresWindows(t *testing.T) {
	if os.Getenv("ATHANOR_AIDE_FENETRE") != "" {
		return // the helper below runs instead
	}
	notes := make(chan string, 8)
	go gardeFenetres(uint32(os.Getpid()), func(s string) { notes <- s })
	time.Sleep(500 * time.Millisecond)
	exe, _ := os.Executable()
	c := exec.Command("cmd.exe", "/c", exe, "-test.run=TestAideFenetreConsole")
	c.Env = append(os.Environ(), "ATHANOR_AIDE_FENETRE=1")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Process.Kill(); _ = c.Wait() }()
	select {
	case n := <-notes:
		t.Log(n)
		if !strings.Contains(strings.ToLower(n), "a window of dist") && !strings.Contains(strings.ToLower(n), ".exe (started by cmd.exe)") {
			t.Fatal(n)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("no window hidden")
	}
	time.Sleep(500 * time.Millisecond)
	vue := false
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if titreFenetre(h) == "athanor-essai" {
			v, _, _ := pIsWindowVisible.Call(h)
			vue = vue || v != 0
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	if vue {
		t.Fatal("the window is still shown")
	}
	_ = c.Process.Kill()
	_ = c.Wait()
	time.Sleep(time.Second)

	// started by this process itself (as the application opens its installation window): left alone
	d := exec.Command(exe, "-test.run=TestAideFenetreConsole")
	d.Env = append(os.Environ(), "ATHANOR_AIDE_FENETRE=1")
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Process.Kill(); _ = d.Wait() }()
	time.Sleep(4 * time.Second)
	select {
	case n := <-notes:
		t.Fatal("hidden:", n)
	default:
	}
	vue = false
	pEnumWindows.Call(cb, 0)
	if !vue {
		t.Fatal("Athanor's own window was hidden")
	}
}

// TestAideFenetreConsole: shows a window of a console's class for a few seconds (run by the test above).
func TestAideFenetreConsole(t *testing.T) {
	if os.Getenv("ATHANOR_AIDE_FENETRE") == "" {
		t.Skip("helper")
	}
	classe, _ := syscall.UTF16PtrFromString(classeConsoleGarde)
	titre, _ := syscall.UTF16PtrFromString("athanor-essai")
	proc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		r, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
		return r
	})
	type wndclassex struct {
		cbSize, style                uint32
		proc                         uintptr
		clsExtra, wndExtra           int32
		instance, icon, cursor, fond uintptr
		menu, classe                 *uint16
		iconSm                       uintptr
	}
	wc := wndclassex{proc: proc, classe: classe}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	h, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(classe)), uintptr(unsafe.Pointer(titre)), 0x00CF0000|0x10000000, 10, 10, 300, 200, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal(err)
	}
	fin := time.Now().Add(8 * time.Second)
	var msg [48]byte
	pPeek := user32.NewProc("PeekMessageW")
	for time.Now().Before(fin) {
		for {
			if r, _, _ := pPeek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1); r == 0 {
				break
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
