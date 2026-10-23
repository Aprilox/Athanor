//go:build !linux

package main

import (
	"fmt"
	"os"
)

// The gateways' PC side in Go is for WSL: native Windows (and the Mac) keep the scripts of system/bin.
func lancerPasserelle(args []string) int {
	fmt.Fprintln(os.Stderr, "athanor passerelle: WSL only (native Windows keeps the scripts of system/bin)")
	return 2
}
