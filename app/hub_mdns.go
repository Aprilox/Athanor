package main

// mDNS discovery (ARCHITECTURE.md §Ouverture, stage 11): the "mdns" setting is OFF by default and only the
// owner turns it on (Settings › Remote Athanors, written to the audit log). On: Athanor answers the queries
// for _athanor._tcp.local with its name and its remote access tunnel URL only (nothing at all without a
// tunnel: no address, no port, the API is never served outside 127.0.0.1) and looks for the others; an
// instance found is only a proposal (its URL, then the 6-digit code of Remote Athanors › Add). The only
// socket outside the loopback of the whole application is this multicast one, and only while it is on.
// A minimal DNS encoder/decoder of our own rather than a dependency (DECISIONS.md, M186).

import (
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	serviceMDNS = "_athanor._tcp.local."
	typePTR     = 12
	typeTXT     = 16
	typeANY     = 255
	ttlMDNS     = 120
	maxTrouves  = 32
)

var groupeMDNS = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// --- the DNS packet (RFC 1035 / 6762), only what Athanor needs: questions, PTR and TXT records ---

type questionDNS struct {
	Nom  string
	Type uint16
}

type rrMDNS struct {
	Nom    string
	Type   uint16
	TTL    uint32
	Cible  string   // PTR
	Textes []string // TXT
}

type paquetDNS struct {
	ID         uint16
	Reponse    bool
	Questions  []questionDNS
	Reponses   []rrMDNS // answers and additional records together
	Additionel int
}

func ajouterNom(b []byte, nom string) ([]byte, error) {
	for _, l := range strings.Split(strings.TrimSuffix(nom, "."), ".") {
		if len(l) == 0 || len(l) > 63 {
			return nil, errors.New("dns: bad label")
		}
		b = append(append(b, byte(len(l))), l...)
	}
	return append(b, 0), nil
}

func encoderDNS(p paquetDNS) ([]byte, error) {
	var drapeaux uint16
	if p.Reponse {
		drapeaux = 0x8400 // response, authoritative
	}
	b := binary.BigEndian.AppendUint16(nil, p.ID)
	b = binary.BigEndian.AppendUint16(b, drapeaux)
	b = binary.BigEndian.AppendUint16(b, uint16(len(p.Questions)))
	b = binary.BigEndian.AppendUint16(b, uint16(len(p.Reponses)-p.Additionel))
	b = binary.BigEndian.AppendUint16(b, 0)
	b = binary.BigEndian.AppendUint16(b, uint16(p.Additionel))
	var err error
	for _, q := range p.Questions {
		if b, err = ajouterNom(b, q.Nom); err != nil {
			return nil, err
		}
		b = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(b, q.Type), 1)
	}
	for _, r := range p.Reponses {
		if b, err = ajouterNom(b, r.Nom); err != nil {
			return nil, err
		}
		b = binary.BigEndian.AppendUint16(b, r.Type)
		b = binary.BigEndian.AppendUint16(b, 0x8001) // IN, cache flush
		b = binary.BigEndian.AppendUint32(b, r.TTL)
		var donnees []byte
		switch r.Type {
		case typePTR:
			if donnees, err = ajouterNom(nil, r.Cible); err != nil {
				return nil, err
			}
		case typeTXT:
			for _, t := range r.Textes {
				if len(t) > 255 {
					return nil, errors.New("dns: TXT string too long")
				}
				donnees = append(append(donnees, byte(len(t))), t...)
			}
		}
		b = append(binary.BigEndian.AppendUint16(b, uint16(len(donnees))), donnees...)
	}
	if len(b) > 9000 {
		return nil, errors.New("dns: packet too large")
	}
	return b, nil
}

var errDNS = errors.New("dns: malformed packet")

// lireNom: a name at off (compression pointers followed, at most 20 jumps: no loop), and the offset after it.
func lireNom(m []byte, off int) (string, int, error) {
	var labels []string
	fin, sauts := -1, 0
	for {
		if off >= len(m) {
			return "", 0, errDNS
		}
		n := int(m[off])
		switch {
		case n == 0:
			if fin < 0 {
				fin = off + 1
			}
			return strings.Join(labels, ".") + ".", fin, nil
		case n&0xC0 == 0xC0:
			if off+1 >= len(m) || sauts > 20 {
				return "", 0, errDNS
			}
			if fin < 0 {
				fin = off + 2
			}
			off = int(binary.BigEndian.Uint16(m[off:]) & 0x3FFF)
			sauts++
		case n > 63:
			return "", 0, errDNS
		default:
			if off+1+n > len(m) || len(labels) > 127 {
				return "", 0, errDNS
			}
			labels = append(labels, string(m[off+1:off+1+n]))
			off += 1 + n
		}
	}
}

func decoderDNS(m []byte) (paquetDNS, error) {
	var p paquetDNS
	if len(m) < 12 {
		return p, errDNS
	}
	p.ID = binary.BigEndian.Uint16(m)
	p.Reponse = m[2]&0x80 != 0
	nq := int(binary.BigEndian.Uint16(m[4:]))
	nr := int(binary.BigEndian.Uint16(m[6:])) + int(binary.BigEndian.Uint16(m[8:])) + int(binary.BigEndian.Uint16(m[10:]))
	off := 12
	for i := 0; i < nq; i++ {
		nom, o, err := lireNom(m, off)
		if err != nil || o+4 > len(m) {
			return p, errDNS
		}
		p.Questions = append(p.Questions, questionDNS{nom, binary.BigEndian.Uint16(m[o:])})
		off = o + 4
	}
	for i := 0; i < nr; i++ {
		nom, o, err := lireNom(m, off)
		if err != nil || o+10 > len(m) {
			return p, errDNS
		}
		r := rrMDNS{Nom: nom, Type: binary.BigEndian.Uint16(m[o:]), TTL: binary.BigEndian.Uint32(m[o+4:])}
		l := int(binary.BigEndian.Uint16(m[o+8:]))
		debut := o + 10
		if debut+l > len(m) {
			return p, errDNS
		}
		switch r.Type {
		case typePTR:
			if r.Cible, _, err = lireNom(m, debut); err != nil {
				return p, errDNS
			}
		case typeTXT:
			for d := debut; d < debut+l; {
				n := int(m[d])
				if d+1+n > debut+l {
					return p, errDNS
				}
				r.Textes = append(r.Textes, string(m[d+1:d+1+n]))
				d += 1 + n
			}
		}
		p.Reponses = append(p.Reponses, r)
		off = debut + l
	}
	return p, nil
}

// --- what Athanor says and what it finds ---

// InstanceReseau: another Athanor found on the local network (a proposal; strings from the network, bounded).
type InstanceReseau struct {
	ID  string    `json:"id"`
	Nom string    `json:"nom"`
	URL string    `json:"url"`
	Vu  time.Time `json:"vu"`
}

type etatMDNS struct {
	mu      sync.Mutex
	conn    *net.UDPConn
	trouves map[string]InstanceReseau
}

// reponseMDNS: our answer to a query for the service (nil without a tunnel: nothing is said at all).
func reponseMDNS(q paquetDNS, id, nom, adresse string) []byte {
	if q.Reponse || adresse == "" {
		return nil
	}
	for _, x := range q.Questions {
		if strings.EqualFold(x.Nom, serviceMDNS) && (x.Type == typePTR || x.Type == typeANY) {
			inst := id + "." + serviceMDNS
			b, err := encoderDNS(paquetDNS{ID: q.ID, Reponse: true, Reponses: []rrMDNS{
				{Nom: serviceMDNS, Type: typePTR, TTL: ttlMDNS, Cible: inst},
				{Nom: inst, Type: typeTXT, TTL: ttlMDNS, Textes: []string{"nom=" + bornerMDNS(nom, 63), "url=" + adresse}},
			}, Additionel: 1})
			if err != nil {
				return nil
			}
			return b
		}
	}
	return nil
}

func bornerMDNS(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	for len(s) > n {
		_, l := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-l]
	}
	return s
}

// instancesDeReponse: the Athanors announced in a response (id from the PTR, nom and url from its TXT;
// only an https URL with a host is kept, our own id is skipped).
func instancesDeReponse(p paquetDNS, moi string) []InstanceReseau {
	if !p.Reponse {
		return nil
	}
	var res []InstanceReseau
	for _, ptr := range p.Reponses {
		if ptr.Type != typePTR || !strings.EqualFold(ptr.Nom, serviceMDNS) || !strings.HasSuffix(strings.ToLower(ptr.Cible), "."+serviceMDNS) {
			continue
		}
		id := strings.TrimSuffix(ptr.Cible[:len(ptr.Cible)-len(serviceMDNS)], ".")
		if id == "" || id == moi || len(id) > 32 || strings.Trim(strings.ToLower(id), "0123456789abcdef") != "" {
			continue
		}
		i := InstanceReseau{ID: id}
		for _, txt := range p.Reponses {
			if txt.Type != typeTXT || !strings.EqualFold(txt.Nom, ptr.Cible) {
				continue
			}
			for _, t := range txt.Textes {
				if v, ok := strings.CutPrefix(t, "nom="); ok {
					i.Nom = bornerMDNS(v, 63)
				} else if v, ok := strings.CutPrefix(t, "url="); ok {
					if u, err := url.Parse(v); err == nil && u.Scheme == "https" && u.Host != "" && len(v) <= 200 && u.User == nil {
						i.URL = u.Scheme + "://" + u.Host
					}
				}
			}
		}
		if i.URL != "" {
			res = append(res, i)
		}
	}
	return res
}

func requeteMDNS() []byte {
	b, _ := encoderDNS(paquetDNS{Questions: []questionDNS{{serviceMDNS, typePTR}}})
	return b
}

// adresseTunnel: the remote access tunnel's URL while it is ready ("" otherwise).
func (s *Serveur) adresseTunnel() string {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tunnel.etat != "pret" {
		return ""
	}
	return d.tunnel.adresse
}

func (s *Serveur) traiterPaquetMDNS(m []byte, de *net.UDPAddr, envoyer func([]byte, *net.UDPAddr)) {
	p, err := decoderDNS(m)
	if err != nil {
		return
	}
	if !p.Reponse {
		nom, _ := os.Hostname()
		if r := reponseMDNS(p, idInstance(), nom, s.adresseTunnel()); r != nil {
			dest := groupeMDNS
			if de != nil && de.Port != 5353 {
				dest = de // a one-shot (legacy) query: answered to its sender
			}
			envoyer(r, dest)
		}
		return
	}
	trouves := instancesDeReponse(p, idInstance())
	if len(trouves) == 0 {
		return
	}
	s.mdns.mu.Lock()
	defer s.mdns.mu.Unlock()
	for _, i := range trouves {
		if _, deja := s.mdns.trouves[i.ID]; deja || len(s.mdns.trouves) < maxTrouves {
			i.Vu = time.Now()
			s.mdns.trouves[i.ID] = i
		}
	}
}

// demarrerMDNS / arreterMDNS: the multicast socket exists only while the setting is on.
func (s *Serveur) demarrerMDNS() error {
	s.mdns.mu.Lock()
	defer s.mdns.mu.Unlock()
	if s.mdns.conn != nil {
		return nil
	}
	c, err := net.ListenMulticastUDP("udp4", nil, groupeMDNS) // the one socket outside the loopback (TestI7Listeners)
	if err != nil {
		return err
	}
	s.mdns.conn = c
	s.mdns.trouves = map[string]InstanceReseau{}
	envoyer := func(b []byte, a *net.UDPAddr) { _, _ = c.WriteToUDP(b, a) }
	go func() {
		tampon := make([]byte, 9000)
		for {
			n, de, err := c.ReadFromUDP(tampon)
			if err != nil {
				return // closed by arreterMDNS
			}
			s.traiterPaquetMDNS(tampon[:n], de, envoyer)
		}
	}()
	go func() {
		for {
			s.mdns.mu.Lock()
			encore := s.mdns.conn == c
			s.mdns.mu.Unlock()
			if !encore {
				return
			}
			envoyer(requeteMDNS(), groupeMDNS)
			time.Sleep(30 * time.Second)
		}
	}()
	return nil
}

func (s *Serveur) arreterMDNS() {
	s.mdns.mu.Lock()
	defer s.mdns.mu.Unlock()
	if s.mdns.conn != nil {
		_ = s.mdns.conn.Close()
		s.mdns.conn = nil
	}
	s.mdns.trouves = nil
}

// instancesReseau: the Athanors seen in the last 2 minutes, by name.
func (s *Serveur) instancesReseau() []InstanceReseau {
	s.mdns.mu.Lock()
	defer s.mdns.mu.Unlock()
	res := []InstanceReseau{}
	for id, i := range s.mdns.trouves {
		if time.Since(i.Vu) > 2*time.Minute {
			delete(s.mdns.trouves, id)
			continue
		}
		res = append(res, i)
	}
	sort.Slice(res, func(a, b int) bool { return res[a].Nom+res[a].ID < res[b].Nom+res[b].ID })
	return res
}

// apiMDNS: POST /api/instances/mdns {actif}: the owner turns the discovery on or off (the computer only:
// interditDistance refuses /api/instance*), written to the audit log.
func (s *Serveur) apiMDNS(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Actif bool `json:"actif"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, 400, "Invalid request.")
		return
	}
	resultat := "ok"
	if c.Actif {
		if err := s.demarrerMDNS(); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: "proprietaire", Vers: "hub", Action: "mdns on", Resultat: "erreur: " + err.Error()})
			s.erreur(w, http.StatusInternalServerError, "Local network discovery could not start (this network refuses multicast).")
			return
		}
	} else {
		s.arreterMDNS()
	}
	s.mu.Lock()
	s.config.MDNS = c.Actif
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		resultat = "config: " + err.Error()
	}
	action := "mdns off"
	if c.Actif {
		action = "mdns on"
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "proprietaire", Vers: "hub", Action: action, Resultat: resultat})
	s.ecrireJSON(w, map[string]any{"mdns": c.Actif})
}
