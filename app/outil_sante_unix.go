//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// groupeSante: the program in a process group of its own, so that it can be stopped with what it started.
func groupeSante(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func tuerGroupeSante(c *exec.Cmd) {
	if c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		_ = c.Process.Kill()
	}
}
