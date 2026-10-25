package main

// Remote access (Settings › Remote): Athanor from a phone or another computer, anywhere, through a
// Cloudflare tunnel (distance_tunnel.go).
//
// A second server listens on 127.0.0.1 only; cloudflared alone reaches it and carries it to a public
// https address. Nothing goes through without a trusted device's key:
//   - pairing: Athanor's window shows a link and its QR code holding a single-use code (random, 2 minutes,
//     renewed by itself). The device that opens it gets its own key (a cookie); the configuration only keeps
//     a hash of it.
//   - every request: the key of a device still in the list (✕ in Settings removes it: its next request is
//     refused), the address of the tunnel in Host, and the interface's secret header as in the window.
//   - from a device: everything the window does, but the secrets, the remote access itself, the password
//     and the folders opened on the computer. With a password, each device unlocks on its own.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
)

const (
	cookieAppareil  = "athanor_appareil"
	dureeCodeDist   = 2 * time.Minute
	enLigneDist     = 20 * time.Second // a device's page asks for the state every 1.5 s
	echecsMaxDist   = 20               // wrong pairing codes in 10 minutes before pairing pauses
	routeAppairer   = "/appairer"
	nomCleAppareil  = "appareil"
	ecritureVuDist  = time.Minute // a device's "last seen" is saved at most this often
	longueurCleDist = 32
)

// Appareil: a trusted device (its key is never saved, only its hash).
type Appareil struct {
	ID     string `json:"id"`
	Nom    string `json:"nom"`
	Hash   string `json:"hash"`
	Ajoute string `json:"ajoute"`
	VuLe   string `json:"vu_le,omitempty"`
	Lieu   string `json:"lieu,omitempty"` // the address it was last seen from (Cloudflare's CF-Connecting-IP)
}

type DistanceConfig struct {
	Actif     bool       `json:"actif,omitempty"`
	Port      int        `json:"port,omitempty"` // the local port cloudflared reaches (kept: the tunnel survives restarts)
	Appareils []Appareil `json:"appareils,omitempty"`
}

type cleContexte string

// distance: the remote access's live state (its own lock, never held with s.mu).
type distance struct {
	mu            sync.Mutex
	code          string
	codeFin       time.Time
	vu            map[string]time.Time
	lieu          map[string]string
	deverrouilles map[string]bool // devices unlocked with the password
	echecs        []time.Time
	ecritVu       time.Time
	ecoute        net.Listener
	srv           *http.Server
	tunnel        etatTunnel
	arretTunnel   chan struct{}
}

func (s *Serveur) dist() *distance {
	s.distOnce.Do(func() {
		s.distEtat = &distance{vu: map[string]time.Time{}, lieu: map[string]string{}, deverrouilles: map[string]bool{}}
	})
	return s.distEtat
}

func hacherCle(cle string) string {
	h := sha256.Sum256([]byte(cle))
	return hex.EncodeToString(h[:])
}

// appareilDe: the device a request comes from (its id, "" for Athanor's own window).
func appareilDe(r *http.Request) string {
	id, _ := r.Context().Value(cleContexte(nomCleAppareil)).(string)
	return id
}

// appareilValide: the device whose key the cookie holds, still trusted ("" otherwise).
func (s *Serveur) appareilValide(r *http.Request) string {
	c, err := r.Cookie(cookieAppareil)
	if err != nil {
		return ""
	}
	id, cle, ok := strings.Cut(c.Value, ".")
	if !ok || id == "" || len(cle) != 2*longueurCleDist {
		return ""
	}
	h := hacherCle(cle)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.config.Distance.Appareils {
		if a.ID == id && egal(a.Hash, h) {
			return id
		}
	}
	return ""
}

// codeAppairage: the current single-use pairing code and its end (a new one once used or past 2 minutes).
func (s *Serveur) codeAppairage() (string, time.Time) {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.code == "" || time.Now().After(d.codeFin) {
		d.code, d.codeFin = aleatoire(24), time.Now().Add(dureeCodeDist)
	}
	return d.code, d.codeFin
}

// consommerCode: the code is right (once: the next pairing needs the new one).
func (s *Serveur) consommerCode(c string) bool {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	maintenant := time.Now()
	recents := d.echecs[:0]
	for _, t := range d.echecs {
		if maintenant.Sub(t) < 10*time.Minute {
			recents = append(recents, t)
		}
	}
	d.echecs = recents
	if len(d.echecs) >= echecsMaxDist {
		return false
	}
	if d.code != "" && maintenant.Before(d.codeFin) && c != "" && egal(c, d.code) {
		d.code = ""
		return true
	}
	d.echecs = append(d.echecs, maintenant)
	return false
}

// nomAppareil: "iPhone · Safari" from the browser's description.
func nomAppareil(ua string) string {
	sys := "Device"
	for _, x := range []struct{ motif, nom string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"CrOS", "Chromebook"},
		{"Windows", "Windows"}, {"Macintosh", "Mac"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, x.motif) {
			sys = x.nom
			break
		}
	}
	nav := ""
	for _, x := range []struct{ motif, nom string }{
		{"Edg", "Edge"}, {"OPR/", "Opera"}, {"SamsungBrowser", "Samsung Internet"}, {"Firefox", "Firefox"},
		{"FxiOS", "Firefox"}, {"CriOS", "Chrome"}, {"Chrome", "Chrome"}, {"Safari", "Safari"},
	} {
		if strings.Contains(ua, x.motif) {
			nav = x.nom
			break
		}
	}
	if nav == "" {
		return sys
	}
	return sys + " · " + nav
}

// appairer: a device opened the pairing link: its key, then the interface.
func (s *Serveur) appairer(w http.ResponseWriter, r *http.Request) {
	if !s.consommerCode(r.URL.Query().Get("c")) {
		time.Sleep(400 * time.Millisecond)
		s.pageDistance(w, http.StatusForbidden, "expire")
		return
	}
	cle := aleatoire(longueurCleDist)
	a := Appareil{ID: aleatoire(6), Nom: nomAppareil(r.UserAgent()), Hash: hacherCle(cle), Ajoute: time.Now().Format(time.RFC3339), VuLe: time.Now().Format(time.RFC3339), Lieu: lieuRequete(r)}
	s.mu.Lock()
	s.config.Distance.Appareils = append(s.config.Distance.Appareils, a)
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d := s.dist()
	d.mu.Lock()
	d.vu[a.ID], d.lieu[a.ID] = time.Now(), a.Lieu
	d.mu.Unlock()
	// Lax: the first visit comes from the camera or another app (a Strict cookie would not follow it)
	http.SetCookie(w, &http.Cookie{Name: cookieAppareil, Value: a.ID + "." + cle, Path: "/", MaxAge: 400 * 24 * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func lieuRequete(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" && len(ip) <= 64 {
		return ip
	}
	return ""
}

// vuDistance: the device asked something now (shown as connected; saved at most once a minute).
func (s *Serveur) vuDistance(id string, r *http.Request) {
	d := s.dist()
	d.mu.Lock()
	d.vu[id] = time.Now()
	if l := lieuRequete(r); l != "" {
		d.lieu[id] = l
	}
	ecrire := time.Since(d.ecritVu) >= ecritureVuDist
	if ecrire {
		d.ecritVu = time.Now()
	}
	vu, lieu := map[string]time.Time{}, map[string]string{}
	for k, v := range d.vu {
		vu[k] = v
	}
	for k, v := range d.lieu {
		lieu[k] = v
	}
	d.mu.Unlock()
	if ecrire {
		s.mu.Lock()
		for i, a := range s.config.Distance.Appareils {
			if t, ok := vu[a.ID]; ok {
				s.config.Distance.Appareils[i].VuLe = t.Format(time.RFC3339)
			}
			if l := lieu[a.ID]; l != "" {
				s.config.Distance.Appareils[i].Lieu = l
			}
		}
		_ = ecrireConfig(s.config)
		s.mu.Unlock()
	}
}

func init() { _ = mime.AddExtensionType(".webmanifest", "application/manifest+json") }

// fichiersPublicsDistance: what a browser fetches without its cookies (adding Athanor to the home screen)
var fichiersPublicsDistance = map[string]bool{"/manifest.webmanifest": true, "/icone-180.png": true, "/icone-512.png": true, "/logo.svg": true}

// interditDistance: what a device never does (Athanor's window only).
func interditDistance(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/distance"), strings.HasPrefix(p, "/api/instance"),
		p == "/api/mot-de-passe", p == "/api/ouvrir":
		return true
	case p == "/api/env" && r.URL.Query().Get("secrets") != "":
		return true
	}
	return false
}

// gestionDistance: the server the tunnel reaches (the same interface and API, for trusted devices only).
func (s *Serveur) gestionDistance(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hote := s.hoteTunnel()
		if hote == "" || !strings.EqualFold(r.Host, hote) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		if r.URL.Path == routeAppairer {
			s.appairer(w, r)
			return
		}
		id := s.appareilValide(r)
		if id == "" && fichiersPublicsDistance[r.URL.Path] {
			mux.ServeHTTP(w, r) // the icon and the description of the app to add to the home screen: no secret
			return
		}
		if id == "" {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				s.erreur(w, http.StatusUnauthorized, "Not paired")
				return
			}
			s.pageDistance(w, http.StatusUnauthorized, "inconnu")
			return
		}
		if interditDistance(r) {
			s.erreur(w, http.StatusForbidden, "Not available from a remote device")
			return
		}
		s.vuDistance(id, r)
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cleContexte(nomCleAppareil), id)))
	})
}

// pageDistance: what a device sees without its key (link expired, device removed), in Athanor's language.
func (s *Serveur) pageDistance(w http.ResponseWriter, code int, quoi string) {
	s.mu.Lock()
	langue := s.config.Langue
	s.mu.Unlock()
	textes := map[string][2]string{
		"en": {"This link has expired or was already used. On your computer, open Athanor › Remote and scan the QR code again.", "This device is not paired with Athanor (or no longer is). On your computer, open Athanor › Remote and scan the QR code."},
		"fr": {"Ce lien a expiré ou a déjà servi. Sur votre ordinateur, ouvrez Athanor › À distance et scannez à nouveau le QR code.", "Cet appareil n'est pas (ou plus) appairé avec Athanor. Sur votre ordinateur, ouvrez Athanor › À distance et scannez le QR code."},
		"es": {"Este enlace ha caducado o ya se usó. En su ordenador, abra Athanor › A distancia y vuelva a escanear el código QR.", "Este dispositivo no está (o ya no está) vinculado con Athanor. En su ordenador, abra Athanor › A distancia y escanee el código QR."},
		"de": {"Dieser Link ist abgelaufen oder wurde schon benutzt. Öffnen Sie auf Ihrem Computer Athanor › Fernzugriff und scannen Sie den QR-Code erneut.", "Dieses Gerät ist nicht (mehr) mit Athanor gekoppelt. Öffnen Sie auf Ihrem Computer Athanor › Fernzugriff und scannen Sie den QR-Code."},
		"it": {"Questo link è scaduto o è già stato usato. Sul computer apri Athanor › A distanza e scansiona di nuovo il codice QR.", "Questo dispositivo non è (più) associato ad Athanor. Sul computer apri Athanor › A distanza e scansiona il codice QR."},
		"pt": {"Esta ligação expirou ou já foi usada. No computador, abra o Athanor › À distância e volte a ler o código QR.", "Este dispositivo não está (ou já não está) associado ao Athanor. No computador, abra o Athanor › À distância e leia o código QR."},
	}
	t, ok := textes[langue]
	if !ok {
		t = textes["en"]
	}
	texte := t[1]
	if quoi == "expire" {
		texte = t[0]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Athanor</title>
<body style="margin:0;font:17px/1.5 system-ui,sans-serif;background:#14181f;color:#e6e8eb;display:flex;min-height:100vh;align-items:center;justify-content:center">
<div style="max-width:420px;padding:28px"><div style="font-size:40px">🔒</div><h1 style="font-size:22px;margin:8px 0 12px">Athanor</h1><p>%s</p></div>`, html.EscapeString(langue), html.EscapeString(texte))
}

// ----------------------------------------------------------------------------- the local server

// ouvrirEcouteDistance: the port cloudflared reaches (the saved one when free: a tunnel kept since the
// previous start of Athanor still points to it).
func (s *Serveur) ouvrirEcouteDistance(mux http.Handler) error {
	d := s.dist()
	d.mu.Lock()
	deja := d.ecoute != nil
	d.mu.Unlock()
	if deja {
		return nil
	}
	s.mu.Lock()
	port := s.config.Distance.Port
	s.mu.Unlock()
	var e net.Listener
	var err error
	// right after an update the previous Athanor may still hold it a moment: the tunnel kept since then points there
	for i := 0; port > 0 && i < 20; i++ {
		if e, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if e == nil {
		if e, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	nouveau := e.Addr().(*net.TCPAddr).Port
	if nouveau != port {
		s.mu.Lock()
		s.config.Distance.Port = nouveau
		_ = ecrireConfig(s.config)
		s.mu.Unlock()
	}
	srv := &http.Server{Handler: s.gestionDistance(mux), ReadHeaderTimeout: 10 * time.Second}
	d.mu.Lock()
	d.ecoute, d.srv = e, srv
	d.mu.Unlock()
	go func() { _ = srv.Serve(e) }()
	return nil
}

func (s *Serveur) fermerEcouteDistance() {
	d := s.dist()
	d.mu.Lock()
	srv := d.srv
	d.ecoute, d.srv = nil, nil
	d.deverrouilles = map[string]bool{}
	d.code = ""
	d.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// demarrerDistance: at Athanor's start, the remote access as it was left.
func (s *Serveur) demarrerDistance(mux http.Handler) {
	s.mux = mux
	s.mu.Lock()
	actif := s.config.Distance.Actif
	s.mu.Unlock()
	if !actif {
		s.arreterTunnelLaisse() // a tunnel left by a previous start while remote access is off
		return
	}
	go func() { // its port may take a few seconds to free (an update): the window does not wait for it
		if err := s.ouvrirEcouteDistance(mux); err == nil {
			eveilDistance(true)
			s.lancerTunnel()
		}
	}()
}

// ----------------------------------------------------------------------------- API (Athanor's window only)

type AppareilVue struct {
	ID      string `json:"id"`
	Nom     string `json:"nom"`
	Ajoute  string `json:"ajoute"`
	VuLe    string `json:"vu_le,omitempty"`
	EnLigne bool   `json:"en_ligne"`
	Lieu    string `json:"lieu,omitempty"`
}

type DistanceVue struct {
	Actif     bool          `json:"actif"`
	Etat      string        `json:"etat"` // arret, installation, demarrage, pret, erreur
	Message   string        `json:"message,omitempty"`
	Adresse   string        `json:"adresse,omitempty"`
	Lien      string        `json:"lien,omitempty"`
	QR        string        `json:"qr,omitempty"` // an SVG image (data: address)
	Expire    int           `json:"expire,omitempty"`
	Appareils []AppareilVue `json:"appareils"`
}

func (s *Serveur) vueDistance() DistanceVue {
	s.mu.Lock()
	v := DistanceVue{Actif: s.config.Distance.Actif, Appareils: []AppareilVue{}}
	appareils := append([]Appareil{}, s.config.Distance.Appareils...)
	s.mu.Unlock()
	d := s.dist()
	d.mu.Lock()
	t := d.tunnel
	vu, lieu := map[string]time.Time{}, map[string]string{}
	for k, x := range d.vu {
		vu[k] = x
	}
	for k, x := range d.lieu {
		lieu[k] = x
	}
	d.mu.Unlock()
	v.Etat, v.Message = t.etat, t.message
	if !v.Actif {
		v.Etat = "arret"
	} else if v.Etat == "" {
		v.Etat = "demarrage"
	}
	if v.Actif && v.Etat == "pret" && t.adresse != "" {
		v.Adresse = t.adresse
		code, fin := s.codeAppairage()
		v.Lien = t.adresse + routeAppairer + "?c=" + code
		v.QR = imageQR(v.Lien)
		v.Expire = max(1, int(time.Until(fin).Seconds()))
	}
	for _, a := range appareils {
		av := AppareilVue{ID: a.ID, Nom: a.Nom, Ajoute: a.Ajoute, VuLe: a.VuLe, Lieu: a.Lieu}
		if x, ok := vu[a.ID]; ok {
			av.VuLe = x.Format(time.RFC3339)
			av.EnLigne = v.Actif && time.Since(x) < enLigneDist
		}
		if l := lieu[a.ID]; l != "" {
			av.Lieu = l
		}
		v.Appareils = append(v.Appareils, av)
	}
	sort.SliceStable(v.Appareils, func(i, j int) bool { return v.Appareils[i].VuLe > v.Appareils[j].VuLe })
	return v
}

func (s *Serveur) apiDistance(w http.ResponseWriter, r *http.Request) {
	s.ecrireJSON(w, s.vueDistance())
}

// apiDistanceActif: the switch (on: the local server, then the tunnel; off: both stop, every device with them).
func (s *Serveur) apiDistanceActif(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Actif bool `json:"actif"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	s.config.Distance.Actif = c.Actif
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Actif {
		if err := s.ouvrirEcouteDistance(s.mux); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		eveilDistance(true)
		go s.lancerTunnel()
	} else {
		s.arreterTunnel()
		s.fermerEcouteDistance()
		eveilDistance(false)
	}
	s.ecrireJSON(w, s.vueDistance())
}

// apiDistanceAppareil: rename a device (POST {nom}) or remove it from the trusted ones (DELETE).
func (s *Serveur) apiDistanceAppareil(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var c struct {
		Nom string `json:"nom"`
	}
	if r.Method == http.MethodPost {
		if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Nom) == "" {
			s.erreur(w, http.StatusBadRequest, "A name")
			return
		}
		c.Nom = strings.TrimSpace(c.Nom)
		if len([]rune(c.Nom)) > 40 {
			c.Nom = string([]rune(c.Nom)[:40])
		}
	}
	s.mu.Lock()
	l := s.config.Distance.Appareils
	trouve := false
	for i, a := range l {
		if a.ID != id {
			continue
		}
		trouve = true
		if r.Method == http.MethodDelete {
			s.config.Distance.Appareils = append(append([]Appareil{}, l[:i]...), l[i+1:]...)
		} else {
			s.config.Distance.Appareils[i].Nom = c.Nom
		}
		break
	}
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if !trouve {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.Method == http.MethodDelete {
		d := s.dist()
		d.mu.Lock()
		delete(d.vu, id)
		delete(d.lieu, id)
		delete(d.deverrouilles, id)
		d.mu.Unlock()
	}
	s.ecrireJSON(w, s.vueDistance())
}

// imageQR: the QR code of a text, as an SVG image (a data: address the page shows as is).
func imageQR(texte string) string {
	c, err := qr.Encode(texte, qr.M)
	if err != nil {
		return ""
	}
	const marge = 4
	n := c.Size + 2*marge
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+marge, y+marge)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String()))
}

// connectesDistance: remote access on, and the trusted devices connected now.
func (s *Serveur) connectesDistance() (bool, int) {
	s.mu.Lock()
	actif := s.config.Distance.Actif
	ids := map[string]bool{}
	for _, a := range s.config.Distance.Appareils {
		ids[a.ID] = true
	}
	s.mu.Unlock()
	if !actif {
		return false, 0
	}
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for id, t := range d.vu {
		if ids[id] && time.Since(t) < enLigneDist {
			n++
		}
	}
	return true, n
}
