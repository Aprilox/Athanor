package main

import (
	"net"
	"testing"
)

// TestAdressePublique: the external check reaches a site through whatever is in front of its server (a public
// address), never a machine of the owner's network that is not one of the servers.
func TestAdressePublique(t *testing.T) {
	for ip, ok := range map[string]bool{"104.21.36.232": true, "8.8.8.8": true, "2606:4700::1": true, "2a01:4f8::1": true,
		"192.168.0.135": false, "10.0.0.1": false, "172.16.5.4": false, "127.0.0.1": false, "::1": false, "100.64.1.1": false,
		"169.254.1.1": false, "fe80::1": false, "fd00::1": false, "0.0.0.0": false, "224.0.0.1": false} {
		if adressePublique(net.ParseIP(ip)) != ok {
			t.Errorf("%s: want %v", ip, ok)
		}
	}
}
