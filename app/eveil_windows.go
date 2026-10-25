//go:build windows

package main

// Athanor.exe --eveil FILE: keeps Windows awake while a pilot runs (native Windows and WSL, the application
// window closed or not). The pilot touches FILE every 20 s and writes in it what must stay on: "system"
// (no sleep) or "screen" (no sleep, and the screen stays on, so the session is not locked by the screen
// timeout: Roblox Studio and Unreal only draw on an unlocked session). Ends once FILE is gone or has not
// been touched for 3 minutes (pilot stopped or killed); Windows lifts the request when the process ends.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var pSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")

const (
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000
)

// modeEveil: what FILE asks for, "" when the pilot is gone.
func modeEveil(fichier string) string {
	st, err := os.Stat(fichier)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64 || time.Since(st.ModTime()) > 3*time.Minute {
		return ""
	}
	b, err := os.ReadFile(fichier)
	if err != nil {
		return ""
	}
	switch strings.TrimSpace(string(b)) {
	case "screen":
		return "screen"
	case "aucun":
		return "aucun"
	}
	return "system"
}

func GarderEveille(args []string) int {
	if len(args) != 1 || args[0] == "" {
		os.Stderr.WriteString("Usage: Athanor.exe --eveil FILE\n")
		return 2
	}
	runtime.LockOSThread() // the request belongs to the thread that makes it
	// the mouse guard and Claude's quiet desktop run alongside, as long as the pilot
	// (garde_souris_windows.go, bureau_calme_windows.go); the kit is above FILE (KIT/logs/pilotes/ID/eveil)
	kit := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(args[0]))))
	fin := make(chan struct{})
	var wg sync.WaitGroup
	for _, f := range []func(string, <-chan struct{}){garderSouris, silencerBureau} {
		wg.Add(1)
		go func() { defer wg.Done(); f(kit, fin) }()
	}
	defer func() { // the sound is given back before the process ends (at most 5 s)
		close(fin)
		fini := make(chan struct{})
		go func() { wg.Wait(); close(fini) }()
		select {
		case <-fini:
		case <-time.After(5 * time.Second):
		}
	}()
	actuel := ""
	for {
		m := modeEveil(args[0])
		if m == "" {
			pSetThreadExecutionState.Call(esContinuous)
			return 0
		}
		if m == "aucun" { // KEEP_AWAKE=0: only the guard
			if actuel != m {
				pSetThreadExecutionState.Call(esContinuous)
				actuel = m
			}
			time.Sleep(20 * time.Second)
			continue
		}
		if m != actuel {
			drapeaux := uintptr(esContinuous | esSystemRequired)
			if m == "screen" {
				drapeaux |= esDisplayRequired
			}
			pSetThreadExecutionState.Call(drapeaux)
			actuel = m
		}
		time.Sleep(20 * time.Second)
	}
}
