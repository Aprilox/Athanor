//go:build !windows && !linux && !darwin

package main

import (
	"runtime"
	"time"
)

func systemeMachine() string { return runtime.GOOS }

// lireMachine: no reader for this system: the processors' count only.
func lireMachine() lectureMachine { return lectureMachine{t: time.Now(), coeurs: runtime.NumCPU()} }
