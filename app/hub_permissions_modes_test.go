package main

import (
	"strings"
	"testing"
)

// TestPermissionsModesHub (M174b, Athanor 2.52.0): the working modes (parallel, eco, fast) keep the rights of the
// mode route. A paired device: TestDistanceToutesRoutes. An agent: no hub tool reads or writes them (the owner's
// switches, in Manage).
func TestPermissionsModesHub(t *testing.T) {
	for nom, o := range outilsHub {
		if strings.Contains(nom, "mode") || strings.Contains(o.Description, "FAST") || strings.Contains(o.Description, "working mode") {
			t.Errorf("the agents' tool %q touches the working modes", nom)
		}
	}
}
