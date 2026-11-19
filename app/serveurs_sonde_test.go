package main

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestSondeServeur: the probe of the Servers page tells an outage and a return by itself, asks a server at
// most once per delay, and tries a refused key only once (M313b4).
func TestSondeServeur(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(string, []byte) (string, int) { return "", 0 })
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		l[0].Vu = time.Now().Add(-time.Hour).UTC()
		return l, nil
	})
	etat := func() ServeurInventaire { l, _ := lireServeurs(); return l[0] }
	t.Cleanup(func() { sondesMu.Lock(); delete(sondes, srv.ID); sondesMu.Unlock() })
	essaye := func() bool { sondesMu.Lock(); defer sondesMu.Unlock(); return sondes[srv.ID].essaye }
	ecoute, _ := net.Listen("tcp", "127.0.0.1:0") // a port nobody listens on any more: the server is stopped
	arrete := etat()
	arrete.Port = ecoute.Addr().(*net.TCPAddr).Port
	ecoute.Close()

	s.sonderServeur(arrete, false)
	if e := etat(); e.Etat != "injoignable" {
		t.Fatalf("stopped server: %+v", e)
	}
	s.sonderServeur(etat(), false) // it answers again: one real connection settles it
	if e := etat(); e.Etat != "ok" || essaye() {
		t.Fatalf("back: %+v, tried %v", e, essaye())
	}

	// a server that answers but refuses the key: tried once, then left alone
	autre, _ := nouvelleCleSSH(t)
	refuse := lancerSSHMemoire(t, autre.PublicKey(), "", func(string, []byte) (string, int) { return "", 0 })
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		l[0].Port, l[0].EmpreinteHote, l[0].Etat = refuse.port, refuse.empreinte, "injoignable"
		return l, nil
	})
	s.sonderServeur(etat(), false)
	if e := etat(); e.Etat != "injoignable" || !essaye() {
		t.Fatalf("refused key: %+v, tried %v", e, essaye())
	}

	// the list asks each server once per delay, and never one that is not added yet
	avant := sonderHote
	t.Cleanup(func() { sonderHote = avant })
	vus := make(chan string, 4)
	sonderHote = func(_ context.Context, hote string, _ int) (string, string, error) {
		vus <- hote
		return "", "", nil
	}
	sondesMu.Lock()
	delete(sondes, srv.ID)
	sondesMu.Unlock()
	liste := []ServeurInventaire{etat(), {ID: "pasencore", Hote: "pas.encore", Etat: "inconnu"}}
	s.sonderServeurs(liste)
	s.sonderServeurs(liste)
	if h := <-vus; h != mem.hote {
		t.Fatalf("asked: %q", h)
	}
	select {
	case h := <-vus:
		t.Fatalf("asked twice within the delay, or a server not added yet: %q", h)
	case <-time.After(300 * time.Millisecond):
	}
}
