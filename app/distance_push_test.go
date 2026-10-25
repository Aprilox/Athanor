package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestChiffrerPush: the example of RFC 8291 (§5), byte for byte.
func TestChiffrerPush(t *testing.T) {
	d := func(s string) []byte {
		b, err := b64.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	as, err := ecdh.P256().NewPrivateKey(d("yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if b64.EncodeToString(as.PublicKey().Bytes()) != "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8" {
		t.Fatal("application server public key")
	}
	corps, err := chiffrerPush(d("BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"), d("BTBZMqHH6r4Tts7J_aSIgg"),
		[]byte("When I grow up, I want to be a watermelon"), as, d("DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	tete, chiffre := d("DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"),
		d("8pfeW0KbunFT06SuDKoJH9Ql87S1QUrdirN6GcG7sFz1y1sqLgVi1VhjVkHsUoEsbI_0LpXMuGvnzQ")
	if string(corps) != string(append(tete, chiffre...)) {
		t.Fatalf("body:\n%s\nwanted\n%s", b64.EncodeToString(corps), b64.EncodeToString(append(tete, chiffre...)))
	}
}

// TestJetonVapid: a token the push service accepts: ES256 over the header and claims, for its origin.
func TestJetonVapid(t *testing.T) {
	k := cleECDSA(append(make([]byte, 31), 7))
	j, err := jetonVapid(k, "https://fcm.googleapis.com/fcm/send/abc", time.Unix(2000000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Split(j, ".")
	if len(p) != 3 {
		t.Fatal(j)
	}
	var c map[string]any
	b, _ := b64.DecodeString(p[1])
	_ = json.Unmarshal(b, &c)
	if c["aud"] != "https://fcm.googleapis.com" || c["exp"] != float64(2000000000) || !strings.HasPrefix(c["sub"].(string), "mailto:") {
		t.Fatalf("claims: %v", c)
	}
	sig, _ := b64.DecodeString(p[2])
	h := sha256.Sum256([]byte(p[0] + "." + p[1]))
	if len(sig) != 64 || !ecdsa.Verify(&k.PublicKey, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature")
	}
	if len(publiqueVapid(k)) != 65 {
		t.Fatal("public key")
	}
}

func TestEndpointPush(t *testing.T) {
	for e, ok := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":              true,
		"https://updates.push.services.mozilla.com/wpush/v2/x": true,
		"https://web.push.apple.com/QH":                        true,
		"https://wns2-par02p.notify.windows.com/w/?token=x":    true,
		"http://fcm.googleapis.com/fcm/send/abc":               false,
		"https://fcm.googleapis.com.evil.example/x":            false,
		"https://192.168.1.1/x":                                false,
		"https://user@fcm.googleapis.com/x":                    false,
		"https://fcm.googleapis.com:8443/x":                    false,
		"https://evilfcm.googleapis.com.example/x":             false,
		"https://localhost/x":                                  false,
	} {
		if endpointPushValide(e) != ok {
			t.Errorf("%s: %v", e, !ok)
		}
	}
}

// dechiffrerPushEssai: what the device's browser does with a push message (RFC 8291), for the tests.
func dechiffrerPushEssai(t *testing.T, ua *ecdh.PrivateKey, auth, corps []byte) []byte {
	t.Helper()
	if len(corps) < 86 || corps[20] != 65 {
		t.Fatalf("header: %d bytes", len(corps))
	}
	sel, asPub, chiffre := corps[:16], corps[21:86], corps[86:]
	cle, err := ecdh.P256().NewPublicKey(asPub)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := ua.ECDH(cle)
	ikm := hkdfPush(auth, secret, append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPub...), 32)
	cek := hkdfPush(sel, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfPush(sel, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	bloc, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(bloc)
	clair, err := gcm.Open(nil, nonce, chiffre, nil)
	if err != nil || len(clair) == 0 || clair[len(clair)-1] != 2 {
		t.Fatalf("decrypt: %v", err)
	}
	return clair[:len(clair)-1]
}

// TestDistancePush: a paired device switches notifications on, gets the test one, then Athanor's; the push
// service forgets it: its subscription goes. Athanor's window cannot subscribe (it has the system's own).
func TestDistancePush(t *testing.T) {
	s, h, _ := serveurDistance(t)
	api := []string{"X-Athanor", "jeton"}
	cle := appairerTest(t, s, h)
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)

	var mu sync.Mutex
	var recus []MessagePush
	code := http.StatusCreated
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		if r.Host != "fcm.googleapis.com" || r.Header.Get("Content-Encoding") != "aes128gcm" || len(r.Header.Get("Topic")) > 32 || !strings.HasPrefix(a, "vapid t=") || !strings.Contains(a, ", k=") {
			http.Error(w, "bad request", 400)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var m MessagePush
		_ = json.Unmarshal(dechiffrerPushEssai(t, ua, auth, b), &m)
		mu.Lock()
		recus = append(recus, m)
		c := code
		mu.Unlock()
		w.WriteHeader(c)
	}))
	defer service.Close()
	ancien := clientPush
	clientPush = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", service.Listener.Addr().String())
		},
	}}
	defer func() { clientPush = ancien }()
	attendre := func(n int) []MessagePush {
		t.Helper()
		for i := 0; i < 100; i++ {
			mu.Lock()
			l := append([]MessagePush{}, recus...)
			mu.Unlock()
			if len(l) >= n {
				return l
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%d message(s) expected", n)
		return nil
	}

	if w := appelDistance(h, "GET", "/api/push", cle, "", api...); w.Code != 200 || !strings.Contains(w.Body.String(), `"abonne":false`) {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	mauvais := `{"action":"abonner","endpoint":"https://192.168.1.10/x","p256dh":"` + b64.EncodeToString(ua.PublicKey().Bytes()) + `","auth":"` + b64.EncodeToString(auth) + `"}`
	if w := appelDistance(h, "POST", "/api/push", cle, mauvais, api...); w.Code != http.StatusBadRequest {
		t.Fatalf("an endpoint on the local network accepted: %d", w.Code)
	}
	bon := strings.Replace(mauvais, "https://192.168.1.10/x", "https://fcm.googleapis.com/fcm/send/abc", 1)
	if w := appelDistance(h, "POST", "/api/push", cle, bon, api...); w.Code != 200 || !strings.Contains(w.Body.String(), `"abonne":true`) {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body)
	}
	if l := attendre(1); l[0].Tag != "essai" || l[0].Titre == "" {
		t.Fatalf("test notification: %+v", l[0])
	}
	s.notifierAppareils(MessagePush{Titre: "Claude needs you · demo", Texte: "Create the Stripe account", URL: "/#pourvous", Tag: "pv-T1"})
	if l := attendre(2); l[1].Texte != "Create the Stripe account" || l[1].URL != "/#pourvous" {
		t.Fatalf("notification: %+v", l[1])
	}
	// the push service forgot the device
	mu.Lock()
	code = http.StatusGone
	mu.Unlock()
	s.notifierAppareils(MessagePush{Titre: "x"})
	attendre(3)
	for i := 0; i < 100 && s.config.Distance.Appareils[0].Push != nil; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.Lock()
	reste := s.config.Distance.Appareils[0].Push
	s.mu.Unlock()
	if reste != nil {
		t.Fatal("subscription kept after 410 Gone")
	}
	// Athanor's own window: never subscribes
	r := httptest.NewRequest("GET", "/api/push", nil)
	r.Header.Set("X-Athanor", "jeton")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("the window subscribed")
	}
}

// TestRejoindreDossierVide: joining a project needs a new or empty folder (the team's files arrive in it).
func TestRejoindreDossierVide(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	plein := t.TempDir()
	ecrireEssai(t, plein+"/README.md", "mine\n")
	appel := func(dossier string) int {
		b, _ := json.Marshal(map[string]any{"nom": "equipe", "dossier": dossier, "demande": "x", "rejoindre": true})
		r := httptest.NewRequest("POST", "/api/projets", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.apiAjouterProjet(w, r)
		return w.Code
	}
	if c := appel(plein); c != http.StatusBadRequest {
		t.Fatalf("a folder with files: %d", c)
	}
	neuf := t.TempDir() + "/neuf"
	if c := appel(neuf); c != http.StatusOK {
		t.Fatalf("a new folder: %d", c)
	}
	if b, err := os.ReadFile(neuf + "/.athanor/REQUEST.md"); err == nil && strings.Contains(string(b), "x") {
		t.Fatal("a request written in a project that joins a team")
	}
}
