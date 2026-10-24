//go:build windows

package main

import (
	"os"
	"time"
)

// quitterPourMaj: the application closes for the update (native pilots go on, no question asked), as
// when its window is closed; without a window of its own, it stops.
func quitterPourMaj() {
	decisionFermeture.Store(1)
	fermetureApp.permise = true
	if h := fenetreHWND.Load(); h != 0 {
		cPostMessageW.Call(h, wmClose, 0, 0)
		time.Sleep(10 * time.Second) // main returns meanwhile; this is only a safety net
	}
	effacerInstance()
	remettreBureau()
	os.Exit(0)
}
