package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// a server with its remote access on, its tunnel ready (no cloudflared: the address is given)
func serveurDistance(t *testing.T) (*Serveur, http.Handler, *http.ServeMux) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.pageAcces(w, r) {
			_, _ = w.Write([]byte("la page"))
		}
	})
	s.mux = mux
	s.config.Distance.Actif = true
	d := s.dist()
	d.tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
	return s, s.gestionDistance(mux), mux
}

func appelDistance(h http.Handler, methode, chemin, cookie string, corps string, entetes ...string) *httptest.ResponseRecorder {
	var r *http.Request
	if corps != "" {
		r = httptest.NewRequest(methode, "https://abc-def.trycloudflare.com"+chemin, strings.NewReader(corps))
	} else {
		r = httptest.NewRequest(methode, "https://abc-def.trycloudflare.com"+chemin, nil)
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1")
	r.Header.Set("Cf-Connecting-Ip", "203.0.113.7")
	for i := 0; i+1 < len(entetes); i += 2 {
		r.Header.Set(entetes[i], entetes[i+1])
	}
	if nom, val, ok := strings.Cut(cookie, "="); ok {
		r.AddCookie(&http.Cookie{Name: nom, Value: val})
	} else if cookie != "" {
		r.AddCookie(&http.Cookie{Name: cookieAppareil, Value: cookie})
	}
	if methode == "POST" && strings.HasPrefix(chemin, routeAppairer) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func valeurCookie(w *httptest.ResponseRecorder, nom string) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == nom && c.MaxAge >= 0 {
			return c.Value
		}
	}
	return ""
}

// ouvrirDemandeTest: the device scans the QR code: the page that asks for the computer's digits
func ouvrirDemandeTest(t *testing.T, s *Serveur, h http.Handler) string {
	t.Helper()
	w := appelDistance(h, "GET", "/appairer?c="+codeDuLien(t, s.vueDistance().Lien), "", "")
	dem := valeurCookie(w, cookieAppairage)
	if w.Code != http.StatusOK || dem == "" || !strings.Contains(w.Body.String(), `name="chiffres"`) || valeurCookie(w, cookieAppareil) != "" {
		t.Fatalf("scan: %d %q", w.Code, w.Header().Values("Set-Cookie"))
	}
	return cookieAppairage + "=" + dem
}

// appairerTest: scan, then the digits the window shows: the device's key
func appairerTest(t *testing.T, s *Serveur, h http.Handler) string {
	t.Helper()
	dem := ouvrirDemandeTest(t, s, h)
	w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+url.QueryEscape(chiffresLisiblesTest(s.vueDistance().Chiffres)), "Origin", "https://abc-def.trycloudflare.com")
	cle := valeurCookie(w, cookieAppareil)
	if w.Code != http.StatusSeeOther || cle == "" {
		t.Fatalf("digits: %d %s", w.Code, w.Body.String())
	}
	return cle
}

func chiffresLisiblesTest(c string) string { return c[:3] + " " + c[3:] } // typed with the space the window shows

func codeDuLien(t *testing.T, lien string) string {
	u, err := url.Parse(lien)
	if err != nil || u.Path != routeAppairer {
		t.Fatalf("pairing link: %q", lien)
	}
	return u.Query().Get("c")
}

func TestDistanceAppairage(t *testing.T) {
	s, h, mux := serveurDistance(t)
	api := []string{"X-Athanor", "jeton"}

	// another host name than the tunnel's: refused before anything
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/etat", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("other host: %d", w.Code)
	}
	// no key: neither the API nor the page
	if w := appelDistance(h, "GET", "/api/etat", "", "", api...); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Not paired") {
		t.Fatalf("API without a key: %d %s", w.Code, w.Body.String())
	}
	if w := appelDistance(h, "GET", "/", "", ""); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "la page") {
		t.Fatalf("page without a key: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/app.js", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("interface's code without a key: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/", "faux.0123", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("page with a made-up key: %d", w.Code)
	}

	// the window shows a link and its QR code
	v := s.vueDistance()
	if v.Etat != "pret" || !strings.HasPrefix(v.Lien, "https://abc-def.trycloudflare.com/appairer?c=") || !strings.HasPrefix(v.QR, "data:image/svg+xml;base64,") || v.Expire < 100 {
		t.Fatalf("view: %+v", v)
	}
	code := codeDuLien(t, v.Lien)
	if len(code) < 40 {
		t.Fatalf("code too short: %q", code)
	}
	if code2 := codeDuLien(t, s.vueDistance().Lien); code2 != code {
		t.Fatal("the code changed before it was used or expired")
	}
	if w := appelDistance(h, "GET", "/appairer?c=mauvais", "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("wrong code: %d", w.Code)
	}
	// the scan alone gives no key: the device is asked for the computer's 6 digits
	dem := ouvrirDemandeTest(t, s, h)
	if w := appelDistance(h, "GET", "/appairer?c="+code, "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("code used twice: %d", w.Code)
	}
	if codeDuLien(t, s.vueDistance().Lien) == code {
		t.Fatal("no new code once used")
	}
	if w := appelDistance(h, "GET", "/", dem, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("interface with a scan only: %d", w.Code)
	}
	v = s.vueDistance()
	if len(v.Chiffres) != 6 || v.ChiffresExpire <= 0 || v.ChiffresExpire > 20000 || strings.Contains(v.Lien, v.Chiffres) || strings.Contains(v.QR, v.Chiffres) {
		t.Fatalf("digits: %+v", v)
	}
	w = appelDistance(h, "POST", routeAppairer, dem, "chiffres="+v.Chiffres, "Origin", "https://abc-def.trycloudflare.com")
	ck := strings.Join(w.Header().Values("Set-Cookie"), " ; ")
	cle := valeurCookie(w, cookieAppareil)
	if w.Code != http.StatusSeeOther || cle == "" || !strings.Contains(ck, "HttpOnly") || !strings.Contains(ck, "Secure") || !strings.Contains(ck, "SameSite=Lax") {
		t.Fatalf("pairing: %d %q", w.Code, ck)
	}
	// the request is over: its cookie gives nothing more
	if w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+s.vueDistance().Chiffres); w.Code != http.StatusForbidden {
		t.Fatalf("request used twice: %d", w.Code)
	}
	// only a hash of the key is kept
	b, _ := json.Marshal(s.config)
	if strings.Contains(string(b), strings.Split(cle, ".")[1]) {
		t.Fatal("the key is saved in the configuration")
	}

	// the device: the page, the API (with the interface's header), seen as connected
	if w := appelDistance(h, "GET", "/", cle, ""); w.Code != http.StatusOK || w.Body.String() != "la page" {
		t.Fatalf("page with the key: %d %s", w.Code, w.Body.String())
	}
	if w := appelDistance(h, "GET", "/api/etat", cle, ""); w.Code != http.StatusForbidden {
		t.Fatalf("API without the interface's header: %d", w.Code)
	}
	w = appelDistance(h, "GET", "/api/etat", cle, "", api...)
	var e EtatVue
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if w.Code != http.StatusOK || !e.Distant || !e.DistanceActive || e.Connectes != 1 {
		t.Fatalf("API with the key: %d %+v", w.Code, e)
	}
	v = s.vueDistance()
	if len(v.Appareils) != 1 || !v.Appareils[0].EnLigne || v.Appareils[0].Nom != "iPhone · Safari" || v.Appareils[0].Lieu != "203.0.113.7" {
		t.Fatalf("devices: %+v", v.Appareils)
	}

	// the Remote page from a device: the state and the trusted devices, never what pairs another device
	w = appelDistance(h, "GET", "/api/distance", cle, "", api...)
	var dv DistanceVue
	_ = json.Unmarshal(w.Body.Bytes(), &dv)
	if w.Code != http.StatusOK || !dv.Actif || len(dv.Appareils) != 1 || !dv.Appareils[0].Moi {
		t.Fatalf("Remote page from a device: %d %s", w.Code, w.Body.String())
	}
	if dv.Lien != "" || dv.QR != "" || dv.Chiffres != "" || dv.Expire != 0 || dv.DomaineLien != "" ||
		strings.Contains(w.Body.String(), routeAppairer) {
		t.Fatalf("the pairing reaches a device: %s", w.Body.String())
	}
	if fv := s.vueDistance(); fv.Lien == "" || fv.Chiffres == "" || fv.Appareils[0].Moi {
		t.Fatalf("the window lost its pairing: %+v", fv)
	}
	// a device renames: the answer has no pairing either
	w = appelDistance(h, "POST", "/api/distance/appareils/"+v.Appareils[0].ID, cle, `{"nom":"iPhone"}`, api...)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), routeAppairer) || strings.Contains(w.Body.String(), `"chiffres"`) {
		t.Fatalf("rename from a device: %d %s", w.Code, w.Body.String())
	}

	// what a device never does
	for _, c := range []struct{ m, p string }{
		{"POST", "/api/distance"}, {"POST", "/api/distance/domaine"},
		{"POST", "/api/mot-de-passe"}, {"POST", "/api/ouvrir"}, {"GET", "/api/env?secrets=1"}, {"POST", "/api/instance/montrer"},
	} {
		if w := appelDistance(h, c.m, c.p, cle, "{}", api...); w.Code != http.StatusForbidden {
			t.Fatalf("%s %s from a device: %d", c.m, c.p, w.Code)
		}
	}

	// the window renames it, then removes it: its next request is refused
	fenetre := func(methode, chemin, corps string) int {
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	id := v.Appareils[0].ID
	if c := fenetre("POST", "/api/distance/appareils/"+id, `{"nom":"Mon téléphone"}`); c != http.StatusOK || s.vueDistance().Appareils[0].Nom != "Mon téléphone" {
		t.Fatalf("rename: %d", c)
	}
	if c := fenetre("DELETE", "/api/distance/appareils/"+id, ""); c != http.StatusOK || len(s.vueDistance().Appareils) != 0 {
		t.Fatalf("remove: %d", c)
	}
	if w := appelDistance(h, "GET", "/api/etat", cle, "", api...); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Not paired") {
		t.Fatalf("removed device: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/", cle, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("removed device, page: %d", w.Code)
	}
}

func TestDistanceMotDePasse(t *testing.T) {
	s, h, _ := serveurDistance(t)
	api := []string{"X-Athanor", "jeton"}
	cle := appairerTest(t, s, h)
	s.config.Sel = "sel"
	s.config.MotDePasseHash = hacher("sel", "secret")
	if w := appelDistance(h, "GET", "/api/journal", cle, "", api...); w.Code != http.StatusUnauthorized {
		t.Fatalf("locked device: %d", w.Code)
	}
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"faux"}`, api...); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"secret"}`, api...); w.Code != http.StatusOK {
		t.Fatalf("unlock: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/api/journal", cle, "", api...); w.Code == http.StatusUnauthorized {
		t.Fatal("still locked after unlocking")
	}
	// the computer stays locked: the device unlocked itself only
	r := httptest.NewRequest("GET", "/api/journal", nil)
	if !s.verrouillePour(r) {
		t.Fatal("the window unlocked by a device")
	}
	// the device locks itself again
	_ = appelDistance(h, "POST", "/api/verrouiller", cle, "{}", api...)
	if w := appelDistance(h, "GET", "/api/journal", cle, "", api...); w.Code != http.StatusUnauthorized {
		t.Fatalf("locked again: %d", w.Code)
	}
	// a stolen phone trying passwords: 5 wrong ones and it is no longer trusted (the count starts again
	// after a right one: the wrong one above is forgotten)
	for i := 1; i < essaisMdpDistance; i++ {
		w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"faux"}`, api...)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"`+strconv.Itoa(essaisMdpDistance-i)+`"`) {
			t.Fatalf("wrong password %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"faux"}`, api...); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Not paired") {
		t.Fatalf("5th wrong password: %d %s", w.Code, w.Body.String())
	}
	if len(s.vueDistance().Appareils) != 0 {
		t.Fatal("device kept after 5 wrong passwords")
	}
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"secret"}`, api...); w.Code != http.StatusUnauthorized {
		t.Fatalf("right password after removal: %d", w.Code)
	}
	// the computer's own window is never removed: wrong passwords there only wait
	r = httptest.NewRequest("POST", "/api/deverrouiller", strings.NewReader(`{"mot_de_passe":"faux"}`))
	w := httptest.NewRecorder()
	s.apiDeverrouiller(w, r)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Wrong password") {
		t.Fatalf("window: %d %s", w.Code, w.Body.String())
	}
}

func TestDistanceEssaisLimites(t *testing.T) {
	s, h, _ := serveurDistance(t)
	code := codeDuLien(t, s.vueDistance().Lien)
	for i := 0; i < echecsMaxDist; i++ {
		_ = s.consommerCode("faux")
	}
	if w := appelDistance(h, "GET", "/appairer?c="+code, "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("pairing after %d wrong codes: %d", echecsMaxDist, w.Code)
	}
}

func TestDistanceSansTunnel(t *testing.T) {
	s, h, _ := serveurDistance(t)
	s.dist().tunnel = etatTunnel{etat: "demarrage"}
	if v := s.vueDistance(); v.Lien != "" || v.QR != "" {
		t.Fatalf("a link without a tunnel: %+v", v)
	}
	if w := appelDistance(h, "GET", "/", "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("no tunnel: %d", w.Code)
	}
	s.config.Distance.Actif = false
	if v := s.vueDistance(); v.Etat != "arret" {
		t.Fatalf("off: %+v", v)
	}
}

func TestNomAppareil(t *testing.T) {
	for ua, nom := range map[string]string{
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36":         "Android · Chrome",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0":     "Windows · Edge",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15":        "Mac · Safari",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148": "iPhone · Chrome",
		"curl/8": "Device",
	} {
		if n := nomAppareil(ua); n != nom {
			t.Errorf("%q: %q, not %q", ua, n, nom)
		}
	}
}

func TestCloudflaredRelease(t *testing.T) {
	notes := "## SHA256 Checksums:\n```\ncloudflared-darwin-arm64.tgz: F35C50089cd25f77a4cb5a2152036bc26db15aa31fbe11f7995d2e42a4ed6257\ncloudflared-linux-amd64: 9d71c677db00134c1bd4144b7783486b654ad281b1ea62b4972098d19f770f17\ncloudflared-linux-amd64.deb: 049777d30f9bf93da6df8bbe31383460eb2aa51a832c6551824d56f9fcc55974\n```"
	if sommeDansNotes(notes, "cloudflared-linux-amd64") != "9d71c677db00134c1bd4144b7783486b654ad281b1ea62b4972098d19f770f17" {
		t.Fatal("linux checksum")
	}
	if sommeDansNotes(notes, "cloudflared-darwin-arm64.tgz") != "f35c50089cd25f77a4cb5a2152036bc26db15aa31fbe11f7995d2e42a4ed6257" {
		t.Fatal("mac checksum")
	}
	if sommeDansNotes(notes, "cloudflared-windows-amd64.exe") != "" {
		t.Fatal("a checksum for a file not listed")
	}
	if n := nomAssetCloudflared(); !strings.HasPrefix(n, "cloudflared-") {
		t.Fatal(n)
	}
	var arch bytes.Buffer
	gz := gzip.NewWriter(&arch)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "cloudflared", Mode: 0o755, Size: 5, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("prog!"))
	tw.Close()
	gz.Close()
	if b, err := extraireTgzCloudflared(arch.Bytes()); err != nil || string(b) != "prog!" {
		t.Fatalf("mac archive: %q %v", b, err)
	}
	if !reAdresseTunnel.MatchString("2026-10-25T10:00:00Z INF |  https://quiet-river-1234.trycloudflare.com  |") {
		t.Fatal("tunnel address")
	}
}

func TestDistanceFichiersPublics(t *testing.T) {
	_, h, mux := serveurDistance(t)
	sous, _ := fs.Sub(statique, "static")
	mux.Handle("/manifest.webmanifest", http.FileServer(http.FS(sous)))
	mux.Handle("/icone-180.png", http.FileServer(http.FS(sous)))
	w := appelDistance(h, "GET", "/manifest.webmanifest", "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "manifest+json") || !strings.Contains(w.Body.String(), `"standalone"`) {
		t.Fatalf("manifest without a key: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := appelDistance(h, "GET", "/icone-180.png", "", ""); w.Code != http.StatusOK {
		t.Fatalf("icon: %d", w.Code)
	}
}

func TestDistanceChiffres(t *testing.T) {
	s, h, _ := serveurDistance(t)
	origine := []string{"Origin", "https://abc-def.trycloudflare.com"}
	// three wrong codes: the request ends, the scan must be done again
	dem := ouvrirDemandeTest(t, s, h)
	bon := s.vueDistance().Chiffres
	faux := "000000"
	if bon == faux {
		faux = "111111"
	}
	for i, attendu := range []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusForbidden} {
		w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+faux, origine...)
		if w.Code != attendu || valeurCookie(w, cookieAppareil) != "" {
			t.Fatalf("wrong code %d: %d", i+1, w.Code)
		}
		if i == 0 && !strings.Contains(w.Body.String(), "2") {
			t.Fatalf("attempts left not said: %s", w.Body.String())
		}
	}
	if w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+bon, origine...); w.Code != http.StatusForbidden {
		t.Fatalf("right code after three wrong ones: %d", w.Code)
	}
	// a form sent from another site: refused
	dem = ouvrirDemandeTest(t, s, h)
	if w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+s.vueDistance().Chiffres, "Origin", "https://ailleurs.example"); w.Code != http.StatusForbidden {
		t.Fatalf("other origin: %d", w.Code)
	}
	if w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+s.vueDistance().Chiffres, "Origin", "null"); w.Code != http.StatusForbidden {
		t.Fatalf("hidden origin: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/appairer?c="+codeDuLien(t, s.vueDistance().Lien), "", ""); w.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("pairing page's referrer policy: %q", w.Header().Get("Referrer-Policy"))
	}
	// no request (only the digits): refused
	if w := appelDistance(h, "POST", routeAppairer, "", "chiffres="+s.vueDistance().Chiffres, origine...); w.Code != http.StatusForbidden {
		t.Fatalf("digits without a scan: %d", w.Code)
	}
	// the digits that just changed are still accepted a few seconds, older ones not
	d := s.dist()
	d.mu.Lock()
	ancien := d.chiffres
	d.chiffresFin = time.Now().Add(-time.Second)
	d.mu.Unlock()
	if nouveau, _ := s.codeChiffres(); nouveau == ancien && ancien != "" {
		d.mu.Lock()
		d.chiffres = "999999"
		d.mu.Unlock()
	}
	d.mu.Lock()
	ok := d.chiffresValides(ancien)
	d.chiffresAvantFin = time.Now().Add(-graceChiffres - time.Second)
	perime := d.chiffresValides(ancien)
	d.mu.Unlock()
	if !ok || perime {
		t.Fatalf("previous digits: just changed %v, long ago %v", ok, perime)
	}
	// an expired request
	dem = ouvrirDemandeTest(t, s, h)
	d.mu.Lock()
	for _, x := range d.demandes {
		x.fin = time.Now().Add(-time.Second)
	}
	d.mu.Unlock()
	if w := appelDistance(h, "POST", routeAppairer, dem, "chiffres="+s.vueDistance().Chiffres, origine...); w.Code != http.StatusForbidden {
		t.Fatalf("expired request: %d", w.Code)
	}
	// and the right way works
	if cle := appairerTest(t, s, h); cle == "" {
		t.Fatal("no key")
	}
}

func TestDistanceLimiteSansCle(t *testing.T) {
	s, h, _ := serveurDistance(t)
	cle := appairerTest(t, s, h)
	s.dist().sansCle = map[string]*compteSansCle{} // the pairing's own two requests are not part of the count below
	// 60 requests a minute without a key from one address go through, the 61st and the next ones not
	for i := 0; i < requetesSansCleMax; i++ {
		if w := appelDistance(h, "GET", "/", "", ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	w := appelDistance(h, "GET", "/", "", "")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("61st: %d", w.Code)
	}
	if w := appelDistance(h, "GET", "/appairer?c="+codeDuLien(t, s.vueDistance().Lien), "", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("pairing from a refused address: %d", w.Code)
	}
	// another address is not refused, a trusted device never
	if w := appelDistance(h, "GET", "/", "", "", "Cf-Connecting-Ip", "198.51.100.9"); w.Code != http.StatusUnauthorized {
		t.Fatalf("other address: %d", w.Code)
	}
	for i := 0; i < 2*requetesSansCleMax; i++ {
		if w := appelDistance(h, "GET", "/api/etat", cle, "", "X-Athanor", "jeton"); w.Code != http.StatusOK {
			t.Fatalf("trusted device, request %d: %d", i+1, w.Code)
		}
	}
	// 10 minutes later the address is let in again
	d := s.dist()
	d.mu.Lock()
	for _, c := range d.sansCle {
		c.bloqueJusqua = time.Now().Add(-time.Second)
		c.debut = time.Now().Add(-2 * fenetreSansCle)
	}
	d.mu.Unlock()
	if w := appelDistance(h, "GET", "/", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("after the pause: %d", w.Code)
	}
}
