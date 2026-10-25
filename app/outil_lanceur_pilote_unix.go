//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const estWindows = false

// ligneDeCommande: the command line of a process ("" when it cannot be read).
func ligneDeCommande(pid int) string {
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err != nil {
			return ""
		}
		return strings.ReplaceAll(string(b), "\x00", " ")
	}
	out, err := commande("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detacherProcessus: a session of its own (nothing of the caller's terminal stops it).
func detacherProcessus(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// detacherDansLeJob: Windows only differs (its jobs); here the same as detacherProcessus.
func detacherDansLeJob(c *exec.Cmd) { detacherProcessus(c) }

func signalerArret(pid int)     { _ = syscall.Kill(pid, syscall.SIGTERM) }
func terminerProcessus(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
