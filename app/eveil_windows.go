//go:build windows

package main

// Athanor.exe --eveil FILE: keeps Windows awake while a pilot runs (native Windows and WSL, the application
// window closed or not). The pilot touches FILE every 20 s and writes in it what must stay on: "system"
// (no sleep) or "screen" (no sleep, and the screen stays on, so the session is not locked by the screen
// timeout: Roblox Studio and Unreal only draw on an unlocked session). Ends once FILE is gone or has not
// been touched for 3 minutes (pilot stopped or killed); Windows lifts the request when the process ends.

import (
	"os"
	"runtime"
	"strings"
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
	if strings.TrimSpace(string(b)) == "screen" {
		return "screen"
	}
	return "system"
}

func GarderEveille(args []string) int {
	if len(args) != 1 || args[0] == "" {
		os.Stderr.WriteString("Usage: Athanor.exe --eveil FILE\n")
		return 2
	}
	runtime.LockOSThread() // the request belongs to the thread that makes it
	actuel := ""
	for {
		m := modeEveil(args[0])
		if m == "" {
			pSetThreadExecutionState.Call(esContinuous)
			return 0
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
