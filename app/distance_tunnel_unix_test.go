//go:build !windows

package main

import (
	"os"
	"testing"
	"time"
)

// a program refused for a few seconds after it was written (on Windows, the antivirus reading it): the
// tunnel starts once it is allowed, without an error left
func TestTunnelProgrammeRefuseUnInstant(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.config.Distance = DistanceConfig{Actif: true, Port: 45556}
	installerFauxCloudflared(t, s)
	if err := os.Chmod(s.cheminCloudflared(), 0o644); err != nil { // refused: "permission denied"
		t.Fatal(err)
	}
	go func() {
		time.Sleep(3 * time.Second)
		_ = os.Chmod(s.cheminCloudflared(), 0o755)
	}()
	go s.lancerTunnel()
	e := attendreTunnel(t, s, "")
	defer s.arreterTunnel()
	if e.pid == 0 || e.message != "" {
		t.Fatalf("tunnel: %+v", e)
	}
}
