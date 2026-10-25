package main

// Requests without a trusted device's key (distance.go), counted per address (Cloudflare's
// CF-Connecting-IP): past 60 in a minute, that address is refused for 10 minutes. The tunnel's address is
// public; someone who finds it can only see the "not paired" page, and now cannot ask for it in a loop
// either. Trusted devices are never counted.

import (
	"net/http"
	"strconv"
	"time"
)

const (
	requetesSansCleMax = 60
	fenetreSansCle     = time.Minute
	blocageSansCle     = 10 * time.Minute
	adressesSuiviesMax = 10000
)

type compteSansCle struct {
	debut, bloqueJusqua time.Time
	n                   int
}

// autoriserSansCle: one more request without a key from this address; false while it is refused.
func (s *Serveur) autoriserSansCle(r *http.Request) (bool, time.Duration) {
	ip := lieuRequete(r)
	if ip == "" {
		ip = "-" // no Cloudflare header: a program of the computer itself
	}
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	maintenant := time.Now()
	if len(d.sansCle) >= adressesSuiviesMax { // the oldest counts go (a flood of addresses)
		for k, c := range d.sansCle {
			if maintenant.Sub(c.debut) > fenetreSansCle && maintenant.After(c.bloqueJusqua) {
				delete(d.sansCle, k)
			}
		}
		if len(d.sansCle) >= adressesSuiviesMax {
			d.sansCle = map[string]*compteSansCle{}
		}
	}
	c := d.sansCle[ip]
	if c == nil {
		c = &compteSansCle{debut: maintenant}
		d.sansCle[ip] = c
	}
	if maintenant.Before(c.bloqueJusqua) {
		return false, c.bloqueJusqua.Sub(maintenant)
	}
	if maintenant.Sub(c.debut) >= fenetreSansCle {
		c.debut, c.n = maintenant, 0
	}
	c.n++
	if c.n > requetesSansCleMax {
		c.bloqueJusqua = maintenant.Add(blocageSansCle)
		return false, blocageSansCle
	}
	return true, 0
}

func refuserTrop(w http.ResponseWriter, attente time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(attente.Seconds())+1))
	http.Error(w, "Too many requests: try again in a few minutes.", http.StatusTooManyRequests)
}
