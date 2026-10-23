//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
)

func verrouPilote(chemin string) (*os.File, bool) {
	f, err := os.OpenFile(chemin, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

func preparerSessionClaude(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func arreterSessionClaude(kill bool, fSid, _, _ string) {
	sid := pidSurPilote(lireTexteCourt(fSid))
	if sid <= 1 {
		return
	}
	sig := syscall.SIGTERM
	nom := "-TERM"
	if kill {
		sig = syscall.SIGKILL
		nom = "-KILL"
	}
	_ = syscall.Kill(-sid, sig)
	_ = syscall.Kill(sid, sig)
	_ = exec.Command("pkill", nom, "-s", strconv.Itoa(sid)).Run()
}

func ignorerSIGPIPE() {
	signal.Ignore(syscall.SIGPIPE)
}

func signauxArretPilote() []os.Signal {
	return []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}
}

func processusVivantPilote(pid int) bool {
	return pid > 1 && syscall.Kill(pid, 0) == nil
}
