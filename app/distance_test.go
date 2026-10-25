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
	"strings"
	"testing"
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
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: cookieAppareil, Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

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
	w = appelDistance(h, "GET", "/appairer?c="+code, "", "")
	ck := w.Header().Get("Set-Cookie")
	if w.Code != http.StatusSeeOther || !strings.Contains(ck, cookieAppareil+"=") || !strings.Contains(ck, "HttpOnly") || !strings.Contains(ck, "Secure") || !strings.Contains(ck, "SameSite=Lax") {
		t.Fatalf("pairing: %d %q", w.Code, ck)
	}
	cle := strings.TrimPrefix(strings.Split(ck, ";")[0], cookieAppareil+"=")
	if w := appelDistance(h, "GET", "/appairer?c="+code, "", ""); w.Code != http.StatusForbidden {
		t.Fatalf("code used twice: %d", w.Code)
	}
	if codeDuLien(t, s.vueDistance().Lien) == code {
		t.Fatal("no new code once used")
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

	// what a device never does
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/distance"}, {"POST", "/api/distance"}, {"DELETE", "/api/distance/appareils/" + v.Appareils[0].ID},
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
	w := appelDistance(h, "GET", "/appairer?c="+codeDuLien(t, s.vueDistance().Lien), "", "")
	cle := strings.TrimPrefix(strings.Split(w.Header().Get("Set-Cookie"), ";")[0], cookieAppareil+"=")
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
