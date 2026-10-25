//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

// eveilDistance: while remote access is on, the computer does not go to sleep (the screen may turn off):
// caffeinate on the Mac, systemd-inhibit on Linux when there is one. Both end with Athanor.
var (
	eveilDistMu  sync.Mutex
	eveilDistCmd *exec.Cmd
)

func eveilDistance(actif bool) {
	eveilDistMu.Lock()
	defer eveilDistMu.Unlock()
	if actif == (eveilDistCmd != nil) {
		return
	}
	if !actif {
		if eveilDistCmd.Process != nil {
			_ = eveilDistCmd.Process.Kill()
		}
		eveilDistCmd = nil
		return
	}
	var c *exec.Cmd
	pid := strconv.Itoa(os.Getpid())
	if runtime.GOOS == "darwin" {
		c = exec.Command("caffeinate", "-i", "-w", pid)
	} else if p, err := exec.LookPath("systemd-inhibit"); err == nil {
		c = exec.Command(p, "--what=idle:sleep", "--who=Athanor", "--why=Remote access is on", "--mode=block", "tail", "--pid="+pid, "-f", "/dev/null")
	} else {
		return
	}
	if c.Start() != nil {
		return
	}
	go func() { _ = c.Wait() }()
	eveilDistCmd = c
}
