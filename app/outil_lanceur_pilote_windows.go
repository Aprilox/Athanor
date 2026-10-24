//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

const estWindows = true

// ligneDeCommande: the program of a process (its full path: Windows gives no other command line cheaply).
func ligneDeCommande(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	// the pilot of Athanor.exe: "…\Athanor.exe" (counted as a pilot by piloteVivant through "pilot")
	return windows.UTF16ToString(buf[:n]) + " pilote"
}

// detacherProcessus: out of the caller's process group and job (closing the application or its console
// does not stop the pilot), hidden.
func detacherProcessus(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob}
}

// signalerArret: Windows has no signal to send to a program without a console: the pilot reads the
// "arreter" file written just before.
func signalerArret(pid int) {}

func terminerProcessus(pid int) {
	tk := commande("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cacherFenetre(tk)
	_ = tk.Run()
}
