package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// sondeServeur: when a server's SSH port was last asked, and whether a login was already tried since it
// answers again.
type sondeServeur struct {
	quand  time.Time
	essaye bool
}

const delaiSonde = 30 * time.Second

var (
	sondeAuto  = !testing.Testing() // the tests' invented servers are never asked over the network
	sonderHote = empreinteHoteSSH
	sondesMu   sync.Mutex
	sondes     = map[string]sondeServeur{}
)

// sonderServeurs: the Servers page is shown (GET /api/serveurs): each added server's SSH port is asked for
// its host key (no login, no vault), in the background and at most every 30 s per server, so that the list
// tells an outage and a return by itself (M313b4).
func (s *Serveur) sonderServeurs(liste []ServeurInventaire) {
	for _, srv := range liste {
		if srv.Vu.IsZero() || (srv.Etat != "ok" && srv.Etat != "injoignable") {
			continue // not added yet, or a changed fingerprint: the owner settles those
		}
		sondesMu.Lock()
		p := sondes[srv.ID]
		recent := time.Since(p.quand) < delaiSonde
		if !recent {
			p.quand = time.Now()
			sondes[srv.ID] = p
		}
		sondesMu.Unlock()
		if !recent {
			go s.sonderServeur(srv, p.essaye)
		}
	}
}

// sonderServeur: no answer: unreachable. An answer from a server noted unreachable: one real connection,
// which notes the state itself; a refused key or password is not tried again while the server keeps
// answering (repeated refusals can get this computer banned).
func (s *Serveur) sonderServeur(srv ServeurInventaire, essaye bool) {
	ctx, fin := context.WithTimeout(context.Background(), delaiSonde)
	defer fin()
	_, _, err := sonderHote(ctx, srv.Hote, srv.Port)
	switch {
	case err != nil:
		noterJoignable(srv.ID, errServeurInjoignable, true)
		essaye = false
	case srv.Etat == "ok":
		essaye = false
	case !essaye && !s.fenetreVerrouillee(): // a locked window would leave a waiting step in the bell
		c, e := s.connecterServeur(ctx, srv)
		if e == nil {
			c.Close()
		}
		essaye = errors.Is(e, errServeurCleRefusee) || errors.Is(e, errServeurMdpRefuse)
	}
	sondesMu.Lock()
	p := sondes[srv.ID]
	p.essaye = essaye
	sondes[srv.ID] = p
	sondesMu.Unlock()
}
