//go:build windows

package main

import (
	"fmt"
	"os"
)

// lancerPont: native Windows has no sandbox, the gateways run directly (PONT_DIRECT): no bridge.
func lancerPont(args []string) int {
	fmt.Fprintln(os.Stderr, "athanor pont: no bridge in native Windows (no sandbox: the gateways run directly)")
	return 2
}
