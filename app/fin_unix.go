//go:build !windows

package main

import (
	"os"
	"syscall"
)

// signalerFin: asks a process to end (TERM), as Python's terminate().
func signalerFin(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
