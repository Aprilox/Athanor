//go:build windows

package main

import "os"

// signalerFin: Windows has no TERM: the process is ended (as Python's terminate() does there).
func signalerFin(p *os.Process) error { return p.Kill() }
