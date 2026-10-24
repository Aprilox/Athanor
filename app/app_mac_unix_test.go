//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestAgentLancementMacBundle(t *testing.T) {
	p := agentLancementMac("/usr/bin/open", "/Applications/Athanor & Co.app")
	if !strings.Contains(p, "<string>/usr/bin/open</string><string>/Applications/Athanor &amp; Co.app</string>") {
		t.Fatal(p)
	}
}
