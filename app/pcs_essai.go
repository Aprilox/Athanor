//go:build essai

package main

import (
	"os"
	"time"
)

// Only in the test benches' builds (go build -tags essai), never in Athanor's: GitHub replaced by a local
// bare repository, so that two real pilots can build a project together on one machine.
func init() {
	if d := os.Getenv("ATHANOR_DEPOT_ESSAI"); d != "" {
		depotEssaiGH = d
		attentePrisePC = time.Second
	}
}
