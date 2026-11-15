package main

import (
	"sync"
	"testing"
	"time"
)

// M240l2: password checks of this PC run one at a time, each wrong one lengthens the pause, a right one resets it.
func TestMdpEssaisSerialises(t *testing.T) {
	if pauseMdp(0) != 400*time.Millisecond || pauseMdp(1) != 800*time.Millisecond || pauseMdp(9) != 5*time.Second {
		t.Fatalf("pauses: %v %v %v", pauseMdp(0), pauseMdp(1), pauseMdp(9))
	}
	s := &Serveur{}
	hash := hacherMotDePasse("le-bon-mot-de-passe")
	debut := time.Now()
	var g sync.WaitGroup
	for range 3 {
		g.Add(1)
		go func() { defer g.Done(); s.verifierMdpLent("", "faux", hash, true) }()
	}
	// M240l5: a fourth attempt while three wait or run is refused at once (429), the queue stays bounded
	time.Sleep(100 * time.Millisecond)
	if t0 := time.Now(); func() bool { _, _, plein := s.verifierMdpLent("", "faux", hash, true); return plein }() == false || time.Since(t0) > 50*time.Millisecond {
		t.Fatal("a fourth attempt in the queue was not refused at once")
	}
	g.Wait()
	// in parallel they still pay 0.4 + 0.8 + 1.6 s, one after the other
	if d := time.Since(debut); d < 2800*time.Millisecond || s.echecsMdp != 3 {
		t.Fatalf("3 parallel wrong attempts: %v, %d failures", d, s.echecsMdp)
	}
	s.verifierMdpLent("", "faux", hash, false) // a remote device has its own count
	if s.echecsMdp != 3 {
		t.Fatalf("remote attempt counted: %d", s.echecsMdp)
	}
	if ok, _, plein := s.verifierMdpLent("", "le-bon-mot-de-passe", hash, true); !ok || plein || s.echecsMdp != 0 {
		t.Fatalf("right password: %v, %d failures", ok, s.echecsMdp)
	}
}
