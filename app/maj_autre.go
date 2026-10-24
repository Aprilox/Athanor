//go:build !windows

package main

import "os"

// quitterPourMaj: the application stops for the update (its pilots go on).
func quitterPourMaj() {
	effacerInstance()
	remettreBureau()
	os.Exit(0)
}
