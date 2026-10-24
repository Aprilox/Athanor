//go:build !linux && !windows

package main

import (
	"fmt"
	"os"
)

// The gateways (system/bin) exist in WSL and in native Windows: macOS runs the applications directly.
func lancerPasserelle(args []string) int {
	fmt.Fprintln(os.Stderr, "athanor passerelle: WSL and native Windows only")
	return 2
}
