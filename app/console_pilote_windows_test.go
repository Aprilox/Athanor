package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const temoinConsole = "témoin-console-7c1e"

// fenetresVisibles: the visible top-level windows of this desktop and of the user's ("Default", where
// Windows Terminal opens a new console's tab), by class name.
func fenetresVisibles() map[uintptr]string {
	m := map[uintptr]string{}
	pClasse := user32.NewProc("GetClassNameW")
	rappel := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(h); v != 0 {
			b := make([]uint16, 128)
			pClasse.Call(h, uintptr(unsafe.Pointer(&b[0])), 128)
			m[h] = syscall.UTF16ToString(b)
		}
		return 1
	})
	user32.NewProc("EnumWindows").Call(rappel, 0)
	nom, _ := syscall.UTF16PtrFromString("Default")
	if d, _, _ := user32.NewProc("OpenDesktopW").Call(uintptr(unsafe.Pointer(nom)), 0, 0, 0x0041); d != 0 {
		user32.NewProc("EnumDesktopWindows").Call(d, rappel, 0)
		user32.NewProc("CloseDesktop").Call(d)
	}
	return m
}

// TestConsoleCacheePilote: a pilot started without a console (Athanor.exe from the application) gets a
// hidden one, which the console programs it starts share: no console window, no Windows Terminal tab.
func TestConsoleCacheePilote(t *testing.T) {
	switch os.Getenv("ATHANOR_TEST_CONSOLE") {
	case "enfant":
		h, _, _ := pGetConsoleWindow.Call()
		fmt.Printf("%x", h)
		time.Sleep(700 * time.Millisecond) // long enough for a window opened for it to be seen
		os.Exit(0)
	case "pilote":
		avant, _, _ := pGetConsoleWindow.Call()
		ok := consoleCachee(false)
		consolePartagee = ok
		h, _, _ := pGetConsoleWindow.Call()
		visible, _, _ := pIsWindowVisible.Call(h)
		c := commande(os.Args[0], "-test.run=^TestConsoleCacheePilote$")
		c.Env = append(os.Environ(), "ATHANOR_TEST_CONSOLE=enfant")
		sansDrapeau := c.SysProcAttr == nil
		sortie, err := c.Output()
		rapport := fmt.Sprintf("%s avant=%v ok=%v visible=%v sansdrapeau=%v console=%x enfant=%s err=%v",
			temoinConsole, avant != 0, ok, visible != 0, sansDrapeau, h, sortie, err)
		_ = os.WriteFile(os.Getenv("ATHANOR_TEST_RAPPORT"), []byte(rapport), 0o644)
		os.Exit(0)
	}

	rapport := filepath.Join(t.TempDir(), "rapport.txt")
	base := fenetresVisibles()
	c := exec.Command(os.Args[0], "-test.run=^TestConsoleCacheePilote$")
	c.Env = append(os.Environ(), "ATHANOR_TEST_CONSOLE=pilote", "ATHANOR_TEST_RAPPORT="+rapport)
	// no console at all, hidden: how a Windows-subsystem Athanor.exe starts
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008} // DETACHED_PROCESS
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	fini := make(chan error, 1)
	go func() { fini <- c.Wait() }()
	var nouvelles []string
	limite := time.After(60 * time.Second)
attente:
	for {
		select {
		case err := <-fini:
			if err != nil {
				t.Fatalf("pilot process: %v", err)
			}
			break attente
		case <-limite:
			_ = c.Process.Kill()
			t.Fatal("pilot process still running after 60 s")
		case <-time.After(100 * time.Millisecond):
			for h, classe := range fenetresVisibles() {
				if _, vu := base[h]; !vu && (classe == "ConsoleWindowClass" || classe == "CASCADIA_HOSTING_WINDOW_CLASS") {
					nouvelles = append(nouvelles, classe)
					base[h] = classe
				}
			}
		}
	}
	b, err := os.ReadFile(rapport)
	if err != nil {
		t.Fatal(err)
	}
	r := string(b)
	if !strings.HasPrefix(r, temoinConsole) {
		t.Fatalf("report not written by the pilot process: %q", r)
	}
	for _, attendu := range []string{"avant=false", "ok=true", "visible=false", "sansdrapeau=true", "err=<nil>"} {
		if !strings.Contains(r, " "+attendu) {
			t.Errorf("want %s: %s", attendu, r)
		}
	}
	// the child shares the pilot's console: same console window
	var console, enfant string
	for _, champ := range strings.Fields(r) {
		if v, ok := strings.CutPrefix(champ, "console="); ok {
			console = v
		} else if v, ok := strings.CutPrefix(champ, "enfant="); ok {
			enfant = v
		}
	}
	if console == "" || console == "0" || enfant != console {
		t.Errorf("the child does not share the pilot's console: %s", r)
	}
	if len(nouvelles) > 0 {
		t.Errorf("console windows opened: %v", nouvelles)
	}
}

// TestDrapeauxConsole: the creation flags of a program started by Athanor: its own hidden console
// (CREATE_NO_WINDOW) when Athanor has none to share, none at all when it has one (the child inherits it).
func TestDrapeauxConsole(t *testing.T) {
	garde := consolePartagee
	defer func() { consolePartagee = garde }()
	consolePartagee = false
	c := commande("git", "--version")
	if c.SysProcAttr == nil || !c.SysProcAttr.HideWindow || c.SysProcAttr.CreationFlags != createNoWindow {
		t.Fatalf("without a console to share: %+v", c.SysProcAttr)
	}
	consolePartagee = true
	if c := commande("git", "--version"); c.SysProcAttr != nil {
		t.Fatalf("with a shared console: %+v", c.SysProcAttr)
	}
	if swConsolePilote(false) != 0 || swConsolePilote(true) != 7 {
		t.Fatal("hidden = SW_HIDE, shown = SW_SHOWMINNOACTIVE (never activated)")
	}
}
