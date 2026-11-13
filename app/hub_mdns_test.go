package main

import (
	"net"
	"strings"
	"testing"
)

// TestMDNSPaquets: the DNS encoder/decoder of hub_mdns.go, our answer (nothing without a tunnel), what is
// read from another Athanor's answer, and hostile packets (truncated, pointer loops) that never panic.
func TestMDNSPaquets(t *testing.T) {
	// a query, encoded then decoded
	q, err := decoderDNS(requeteMDNS())
	if err != nil || q.Reponse || len(q.Questions) != 1 || q.Questions[0].Nom != serviceMDNS || q.Questions[0].Type != typePTR {
		t.Fatalf("query: %+v %v", q, err)
	}
	// no tunnel: not a word
	if r := reponseMDNS(q, "abc123", "pc", ""); r != nil {
		t.Fatal("an answer without a tunnel")
	}
	// another service: no answer
	autre, _ := encoderDNS(paquetDNS{Questions: []questionDNS{{"_http._tcp.local.", typePTR}}})
	if p, _ := decoderDNS(autre); reponseMDNS(p, "abc123", "pc", "https://a-b.trycloudflare.com") != nil {
		t.Fatal("an answer to another service")
	}
	// with a tunnel: the name and the URL, nothing else (no address, no port)
	brut := reponseMDNS(q, "abc123", "pc\x01-bureau", "https://a-b.trycloudflare.com")
	p, err := decoderDNS(brut)
	if err != nil || !p.Reponse || len(p.Reponses) != 2 {
		t.Fatalf("answer: %+v %v", p, err)
	}
	for _, r := range p.Reponses {
		if r.Type != typePTR && r.Type != typeTXT {
			t.Fatalf("a record of type %d (only PTR and TXT are said)", r.Type)
		}
	}
	trouves := instancesDeReponse(p, "fff000")
	if len(trouves) != 1 || trouves[0].ID != "abc123" || trouves[0].Nom != "pc-bureau" || trouves[0].URL != "https://a-b.trycloudflare.com" {
		t.Fatalf("found: %+v", trouves)
	}
	if len(instancesDeReponse(p, "abc123")) != 0 {
		t.Fatal("our own answer kept")
	}
	// an http URL, a URL with credentials or a bad id: not kept
	for _, cas := range [][2]string{{"abc123", "http://a.example"}, {"abc123", "https://u:p@a.example"}, {"zz!", "https://a.example"}, {"abc123", "javascript:alert(1)"}} {
		b, err := encoderDNS(paquetDNS{Reponse: true, Reponses: []rrMDNS{
			{Nom: serviceMDNS, Type: typePTR, Cible: cas[0] + "." + serviceMDNS},
			{Nom: cas[0] + "." + serviceMDNS, Type: typeTXT, Textes: []string{"nom=x", "url=" + cas[1]}},
		}})
		if err != nil {
			continue // a label the encoder refuses
		}
		pp, _ := decoderDNS(b)
		if n := instancesDeReponse(pp, "fff000"); len(n) != 0 {
			t.Fatalf("%v kept: %+v", cas, n)
		}
	}
	// name compression: the TXT's name is a pointer to the PTR's target
	comp := append([]byte{}, brut...)
	i := strings.Index(string(comp), "\x06abc123")
	j := strings.LastIndex(string(comp), "\x06abc123")
	if i < 0 || j <= i {
		t.Fatal("names not found in the packet")
	}
	fin := j + len("\x06abc123\x08_athanor\x04_tcp\x05local\x00")
	comp = append(append(append([]byte{}, comp[:j]...), 0xC0|byte(i>>8), byte(i)), comp[fin:]...)
	if pc, err := decoderDNS(comp); err != nil || len(instancesDeReponse(pc, "fff000")) != 1 {
		t.Fatalf("compressed answer: %v", err)
	}
	// hostile packets: every truncation, and a pointer to itself
	for n := 0; n < len(brut); n++ {
		_, _ = decoderDNS(brut[:n])
	}
	boucle := []byte{0, 0, 0x84, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0xC0, 12, 0, 12, 0, 1}
	if _, err := decoderDNS(boucle); err == nil {
		t.Fatal("a pointer loop decoded")
	}

	// traiterPaquetMDNS: a query is answered only with a tunnel; an answer is recorded, at most maxTrouves
	s := nouveauServeur(Config{})
	s.mdns.trouves = map[string]InstanceReseau{}
	var envoyes [][]byte
	var dests []*net.UDPAddr
	envoyer := func(b []byte, a *net.UDPAddr) { envoyes, dests = append(envoyes, b), append(dests, a) }
	s.traiterPaquetMDNS(requeteMDNS(), &net.UDPAddr{IP: net.IPv4(192, 168, 1, 9), Port: 5353}, envoyer)
	if len(envoyes) != 0 {
		t.Fatal("answered without a tunnel")
	}
	d := s.dist()
	d.mu.Lock()
	d.tunnel.etat, d.tunnel.adresse = "pret", "https://x-y.trycloudflare.com"
	d.mu.Unlock()
	s.traiterPaquetMDNS(requeteMDNS(), &net.UDPAddr{IP: net.IPv4(192, 168, 1, 9), Port: 5353}, envoyer)
	s.traiterPaquetMDNS(requeteMDNS(), &net.UDPAddr{IP: net.IPv4(192, 168, 1, 9), Port: 40000}, envoyer)
	if len(envoyes) != 2 || dests[0] != groupeMDNS || dests[1].Port != 40000 {
		t.Fatalf("answers: %d %v", len(envoyes), dests)
	}
	if rp, _ := decoderDNS(envoyes[0]); !strings.Contains(strings.Join(rp.Reponses[1].Textes, " "), "url=https://x-y.trycloudflare.com") {
		t.Fatalf("answer: %+v", rp)
	}
	s.traiterPaquetMDNS(brut, nil, envoyer)
	if l := s.instancesReseau(); len(l) != 1 || l[0].ID != "abc123" {
		t.Fatalf("instances: %+v", l)
	}
	for k := 0; k < 3*maxTrouves; k++ {
		id := strings.Repeat("a", 1+k%20) + string("0123456789abcdef"[k%16])
		b := reponseMDNS(q, id+strings.Repeat("b", k/20), "n", "https://z.example")
		s.traiterPaquetMDNS(b, nil, envoyer)
	}
	if n := len(s.instancesReseau()); n > maxTrouves {
		t.Fatalf("%d instances kept (at most %d)", n, maxTrouves)
	}
}

// verifierMDNSOffOn: verifies the multicast socket exists only while mDNS is on (part of TestI7Listeners).
func verifierMDNSOffOn(t *testing.T) {
	if (Config{}).MDNS {
		t.Fatal("mDNS on by default")
	}
	s := nouveauServeur(Config{})
	if s.mdns.conn != nil {
		t.Fatal("mDNS socket open while off")
	}
	if err := s.demarrerMDNS(); err != nil {
		t.Logf("mDNS on: multicast unavailable here (%v), the socket part is skipped", err)
		return
	}
	if a, ok := s.mdns.conn.LocalAddr().(*net.UDPAddr); !ok || a.Port != 5353 {
		t.Errorf("mDNS socket on %v", s.mdns.conn.LocalAddr())
	}
	s.arreterMDNS()
	if s.mdns.conn != nil {
		t.Fatal("mDNS socket still open after off")
	}
}
