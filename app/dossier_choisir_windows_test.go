//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// TestDialogueDossierWindows (ATHANOR_ESSAI_DIALOGUE=1, a desktop needed): the dialog opens on the start
// folder; OK chooses it (pressed by the test itself).
func TestDialogueDossierWindows(t *testing.T) {
	if os.Getenv("ATHANOR_ESSAI_DIALOGUE") != "1" {
		t.Skip("ATHANOR_ESSAI_DIALOGUE=1 to open a real dialog")
	}
	user := syscall.NewLazyDLL("user32.dll")
	findW, postM := user.NewProc("FindWindowW"), user.NewProc("PostMessageW")
	depart := t.TempDir()
	titre := "Athanor test dialog"
	go func() {
		n, _ := syscall.UTF16PtrFromString(titre)
		enumW, getT := user.NewProc("EnumWindows"), user.NewProc("GetWindowTextW")
		time.Sleep(3 * time.Second)
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			b := make([]uint16, 200)
			getT.Call(h, uintptr(unsafe.Pointer(&b[0])), 200)
			if s := syscall.UTF16ToString(b); s != "" {
				os.Stderr.WriteString("window: " + s + "\n")
			}
			return 1
		})
		enumW.Call(cb, 0)
		for i := 0; i < 100; i++ {
			time.Sleep(200 * time.Millisecond)
			if h, _, _ := findW.Call(0, uintptr(unsafe.Pointer(n))); h != 0 {
				time.Sleep(500 * time.Millisecond)
				if os.Getenv("ATHANOR_ESSAI_ANNULER") == "1" {
					postM.Call(h, 0x0010, 0, 0) // WM_CLOSE
				} else {
					// the file name field holds the start folder, then OK (Wine's dialog does not take an
					// empty name; Windows' takes the folder shown)
					findEx, sendM := user.NewProc("FindWindowExW"), user.NewProc("SendMessageW")
					cls, _ := syscall.UTF16PtrFromString("Edit")
					var e uintptr
					for _, parent := range []uintptr{h} {
						e, _, _ = findEx.Call(parent, 0, uintptr(unsafe.Pointer(cls)), 0)
						if e == 0 {
							cb, _ := syscall.UTF16PtrFromString("ComboBoxEx32")
							if c, _, _ := findEx.Call(parent, 0, uintptr(unsafe.Pointer(cb)), 0); c != 0 {
								cbb, _ := syscall.UTF16PtrFromString("ComboBox")
								c2, _, _ := findEx.Call(c, 0, uintptr(unsafe.Pointer(cbb)), 0)
								e, _, _ = findEx.Call(c2, 0, uintptr(unsafe.Pointer(cls)), 0)
							}
						}
					}
					if e != 0 {
						txt, _ := syscall.UTF16PtrFromString(depart)
						sendM.Call(e, 0x000C, 0, uintptr(unsafe.Pointer(txt))) // WM_SETTEXT
					}
					postM.Call(h, 0x0111, 1, 0) // WM_COMMAND IDOK
				}
				return
			}
		}
	}()
	chemin, ok, dispo := choisirDossierSysteme(depart, titre)
	if os.Getenv("ATHANOR_ESSAI_ANNULER") == "1" {
		if !dispo || ok {
			t.Fatalf("closed: %q %v %v", chemin, ok, dispo)
		}
		return
	}
	if !dispo || !ok || !strings.EqualFold(strings.TrimRight(chemin, `\`), strings.TrimRight(depart, `\`)) {
		t.Fatalf("dialog: %q %v %v (start %q)", chemin, ok, dispo, depart)
	}
}
