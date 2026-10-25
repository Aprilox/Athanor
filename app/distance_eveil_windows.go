//go:build windows

package main

import (
	"runtime"
	"sync"
)

// eveilDistance: while remote access is on, Windows does not go to sleep (the screen may turn off): a device
// could not reach a sleeping computer. The request belongs to the thread that makes it.
var (
	eveilDistMu  sync.Mutex
	eveilDistFin chan struct{}
)

func eveilDistance(actif bool) {
	eveilDistMu.Lock()
	defer eveilDistMu.Unlock()
	if actif == (eveilDistFin != nil) {
		return
	}
	if !actif {
		close(eveilDistFin)
		eveilDistFin = nil
		return
	}
	fin := make(chan struct{})
	eveilDistFin = fin
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		_, _, _ = pSetThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired))
		<-fin
		_, _, _ = pSetThreadExecutionState.Call(uintptr(esContinuous))
	}()
}
