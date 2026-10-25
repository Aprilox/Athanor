//go:build !windows

package main

import "os"

// quitterPourMaj: the application stops for the update (its pilots go on).
func quitterPourMaj() {
	if avantSortie != nil {
		avantSortie() // the terminal interface: the terminal given back as it was
	}
	effacerInstance()
	remettreBureau()
	os.Exit(0)
}
