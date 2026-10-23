//go:build windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func verrouPilote(chemin string) (*os.File, bool) {
	f, err := os.OpenFile(chemin, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	var ol windows.Overlapped
	if windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol) != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

func preparerSessionClaude(c *exec.Cmd) {
	cacherFenetre(c)
}

func arreterSessionClaude(kill bool, fSid, fWin, journalArret string) {
	if fWin != "" {
		if w := pidSurPilote(lireTexteCourt(fWin)); w > 1 {
			tl := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(w), "/NH")
			cacherFenetre(tl)
			if out, err := tl.Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "athanor") {
				tk := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(w))
				cacherFenetre(tk)
				sortie, _ := tk.CombinedOutput()
				if journalArret != "" {
					f, err := os.OpenFile(journalArret, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
					if err == nil {
						_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + "\n" + string(sortie))
						f.Close()
					}
				}
			}
		}
	}
	if sid := pidSurPilote(lireTexteCourt(fSid)); sid > 1 {
		tk := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(sid))
		cacherFenetre(tk)
		_ = tk.Run()
		if p, err := os.FindProcess(sid); err == nil {
			_ = p.Kill()
		}
	}
	_ = kill
}

func ignorerSIGPIPE() {}

func signauxArretPilote() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func processusVivantPilote(pid int) bool {
	if pid <= 1 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
