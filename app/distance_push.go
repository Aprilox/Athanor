package main

// Notifications on the paired devices (Web Push): a phone that switched them on in Athanor (Remote, on the
// phone) gets a notification even with Athanor closed on it, through its browser's push service (Google's,
// Apple's, Mozilla's, Microsoft's). The message is encrypted for that device alone (RFC 8291, aes128gcm):
// the push service only relays bytes it cannot read. Athanor signs its requests with a key of its own
// (VAPID, RFC 8292), made once and kept in its settings.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AbonnementPush: where a device's browser receives its notifications (PushSubscription), and its keys.
type AbonnementPush struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // the device's public key (base64url, 65 bytes)
	Auth     string `json:"auth"`   // its authentication secret (base64url, 16 bytes)
}

var b64 = base64.RawURLEncoding

// hotesPush: the push services browsers use. A device's endpoint elsewhere is refused: Athanor never
// posts to an address a device would choose (a computer of the local network, for instance).
var hotesPush = []string{"fcm.googleapis.com", "android.googleapis.com", "push.services.mozilla.com", "push.apple.com", "notify.windows.com"}

func endpointPushValide(e string) bool {
	u, err := url.Parse(e)
	if err != nil || u.Scheme != "https" || u.User != nil || len(e) > 1024 || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, x := range hotesPush {
		if h == x || strings.HasSuffix(h, "."+x) {
			return true
		}
	}
	return false
}

func (a AbonnementPush) valide() bool {
	p, err1 := b64.DecodeString(strings.TrimRight(a.P256dh, "="))
	k, err2 := b64.DecodeString(strings.TrimRight(a.Auth, "="))
	return endpointPushValide(a.Endpoint) && err1 == nil && len(p) == 65 && p[0] == 4 && err2 == nil && len(k) == 16
}

// hkdfPush: HKDF-SHA-256 (RFC 5869), extract then expand to n ≤ 32 bytes.
func hkdfPush(sel, ikm, info []byte, n int) []byte {
	m := hmac.New(sha256.New, sel)
	m.Write(ikm)
	prk := m.Sum(nil)
	m = hmac.New(sha256.New, prk)
	m.Write(info)
	m.Write([]byte{1})
	return m.Sum(nil)[:n]
}

// chiffrerPush: the body of a push message for one device (RFC 8291, aes128gcm, one record).
func chiffrerPush(uaPub, auth, message []byte, asPriv *ecdh.PrivateKey, sel []byte) ([]byte, error) {
	cleUA, err := ecdh.P256().NewPublicKey(uaPub)
	if err != nil {
		return nil, err
	}
	secret, err := asPriv.ECDH(cleUA)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes()
	info := append(append([]byte("WebPush: info\x00"), uaPub...), asPub...)
	ikm := hkdfPush(auth, secret, info, 32)
	cek := hkdfPush(sel, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfPush(sel, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	bloc, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(bloc)
	if err != nil {
		return nil, err
	}
	chiffre := gcm.Seal(nil, nonce, append(append([]byte{}, message...), 2), nil) // 2: the last record
	var tete bytes.Buffer
	tete.Write(sel)
	_ = binary.Write(&tete, binary.BigEndian, uint32(4096))
	tete.WriteByte(byte(len(asPub)))
	tete.Write(asPub)
	return append(tete.Bytes(), chiffre...), nil
}

// cleVapid: Athanor's signing key for the push services (made once, kept in the settings).
func (s *Serveur) cleVapid() (*ecdsa.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, err := b64.DecodeString(s.config.Distance.Vapid); err == nil && len(d) == 32 {
		return cleECDSA(d), nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	d := k.D.FillBytes(make([]byte, 32))
	s.config.Distance.Vapid = b64.EncodeToString(d)
	_ = ecrireConfig(s.config)
	return k, nil
}

func cleECDSA(d []byte) *ecdsa.PrivateKey {
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	k.X, k.Y = k.Curve.ScalarBaseMult(d)
	return k
}

func publiqueVapid(k *ecdsa.PrivateKey) []byte {
	b := make([]byte, 65)
	b[0] = 4
	k.X.FillBytes(b[1:33])
	k.Y.FillBytes(b[33:])
	return b
}

// jetonVapid: the signed token (JWT, ES256) that says to the push service who sends (RFC 8292).
func jetonVapid(k *ecdsa.PrivateKey, endpoint string, expire time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	tete := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	corps, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": expire.Unix(), "sub": "mailto:athanor@users.noreply.github.com"})
	signe := tete + "." + b64.EncodeToString(corps)
	h := sha256.Sum256([]byte(signe))
	r, ss, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return signe + "." + b64.EncodeToString(sig), nil
}

// MessagePush: what the device shows (its service worker reads it, static/sw.js).
type MessagePush struct {
	Titre string `json:"titre"`
	Texte string `json:"texte"`
	URL   string `json:"url,omitempty"` // the page opened by a tap ("/#pourvous")
	Tag   string `json:"tag,omitempty"` // a newer message with the same tag replaces the older one
}

// clientPush: never follows a redirect (only the push service the device named is ever reached).
var clientPush = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// errPushPerime: the push service forgot this device (the browser unsubscribed, the app was removed).
var errPushPerime = errors.New("subscription gone")

func (s *Serveur) envoyerPush(a AbonnementPush, m MessagePush) error {
	if !a.valide() {
		return errors.New("invalid subscription")
	}
	k, err := s.cleVapid()
	if err != nil {
		return err
	}
	ua, _ := b64.DecodeString(strings.TrimRight(a.P256dh, "="))
	auth, _ := b64.DecodeString(strings.TrimRight(a.Auth, "="))
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	sel := make([]byte, 16)
	if _, err := rand.Read(sel); err != nil {
		return err
	}
	// one record of 4096 bytes holds it all (RFC 8291): the texts cut well below
	m.Titre, m.Texte, m.Tag = coupeValide(m.Titre, 200), coupeValide(m.Texte, 1500), coupeValide(m.Tag, 100)
	if len(m.URL) > 200 || !strings.HasPrefix(m.URL, "/") || strings.HasPrefix(m.URL, "//") {
		m.URL = "/"
	}
	texte, _ := json.Marshal(m)
	corps, err := chiffrerPush(ua, auth, texte, eph, sel)
	if err != nil {
		return err
	}
	jeton, err := jetonVapid(k, a.Endpoint, time.Now().Add(12*time.Hour))
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, a.Endpoint, bytes.NewReader(corps))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "high")
	if m.Tag != "" {
		// a topic is at most 32 base64url characters (RFC 8030): the tag's hash
		h := sha256.Sum256([]byte(m.Tag))
		req.Header.Set("Topic", b64.EncodeToString(h[:])[:32])
	}
	req.Header.Set("Authorization", "vapid t="+jeton+", k="+b64.EncodeToString(publiqueVapid(k)))
	rep, err := clientPush.Do(req)
	if err != nil {
		return err
	}
	defer rep.Body.Close()
	switch {
	case rep.StatusCode == http.StatusNotFound || rep.StatusCode == http.StatusGone || rep.StatusCode == http.StatusForbidden:
		// forgotten, or subscribed with another key of Athanor's: the device switches them on again
		return errPushPerime
	case rep.StatusCode >= 300:
		return fmt.Errorf("push service: %s", rep.Status)
	}
	return nil
}

var pushMu sync.Mutex // one round of notifications at a time

// notifierAppareils: a notification on every paired device that switched them on (best effort, in the
// background; a device the push service forgot loses its subscription).
func (s *Serveur) notifierAppareils(m MessagePush) {
	s.mu.Lock()
	var cibles []Appareil
	for _, a := range s.config.Distance.Appareils {
		if a.Push != nil {
			cibles = append(cibles, a)
		}
	}
	s.mu.Unlock()
	if len(cibles) == 0 {
		return
	}
	go func() {
		pushMu.Lock()
		defer pushMu.Unlock()
		for _, a := range cibles {
			if err := s.envoyerPush(*a.Push, m); errors.Is(err, errPushPerime) {
				s.oublierPushAppareil(a.ID, a.Push.Endpoint)
			}
		}
	}()
}

// oublierPushAppareil: the subscription goes, if it is still that one (the device may have subscribed again).
func (s *Serveur) oublierPushAppareil(id, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Distance.Appareils {
		if a := s.config.Distance.Appareils[i]; a.ID == id && a.Push != nil && a.Push.Endpoint == endpoint {
			s.config.Distance.Appareils[i].Push = nil
			_ = ecrireConfig(s.config)
		}
	}
}

func (s *Serveur) majPushAppareil(id string, ab *AbonnementPush) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Distance.Appareils {
		if s.config.Distance.Appareils[i].ID == id {
			s.config.Distance.Appareils[i].Push = ab
			_ = ecrireConfig(s.config)
			return true
		}
	}
	return false
}

// apiPush: GET (the device's state, Athanor's public key); POST {action: "abonner", endpoint, p256dh, auth}
// | "desabonner" | "essai". Paired devices only: Athanor's window has the computer's own notifications.
func (s *Serveur) apiPush(w http.ResponseWriter, r *http.Request) {
	id := appareilDe(r)
	if id == "" {
		s.erreur(w, http.StatusForbidden, "Only from a paired device")
		return
	}
	k, err := s.cleVapid()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	abonne := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, a := range s.config.Distance.Appareils {
			if a.ID == id {
				return a.Push != nil
			}
		}
		return false
	}
	if r.Method == http.MethodGet {
		s.ecrireJSON(w, map[string]any{"cle": b64.EncodeToString(publiqueVapid(k)), "abonne": abonne()})
		return
	}
	var c struct {
		Action string `json:"action"`
		AbonnementPush
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	switch c.Action {
	case "abonner":
		if !c.AbonnementPush.valide() {
			s.erreur(w, http.StatusBadRequest, "This browser's push service is not one Athanor knows.")
			return
		}
		ab := c.AbonnementPush
		s.majPushAppareil(id, &ab)
		_ = s.envoyerPushEssai(ab)
	case "desabonner":
		s.majPushAppareil(id, nil)
	case "essai":
		s.mu.Lock()
		var ab *AbonnementPush
		for _, a := range s.config.Distance.Appareils {
			if a.ID == id {
				ab = a.Push
			}
		}
		s.mu.Unlock()
		if ab == nil {
			s.erreur(w, http.StatusBadRequest, "Notifications are off on this device.")
			return
		}
		if err := s.envoyerPushEssai(*ab); err != nil {
			s.erreur(w, http.StatusBadGateway, err.Error())
			return
		}
	default:
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	s.ecrireJSON(w, map[string]any{"abonne": abonne()})
}

// textesPush: the texts of the notifications, in Athanor's language.
var textesPush = map[string]map[string]string{
	"en": {"essai": "Notifications are on", "essai_t": "Athanor will tell this device when Claude needs you, a milestone is done or a computer is stuck.", "jalon": "Milestone done", "livre": "Project delivered", "livre_t": "The final report is ready.", "besoin": "Claude needs you"},
	"fr": {"essai": "Notifications activées", "essai_t": "Athanor préviendra cet appareil quand Claude a besoin de vous, qu'un jalon est fini ou qu'un ordinateur bloque.", "jalon": "Jalon terminé", "livre": "Projet livré", "livre_t": "Le rapport final est prêt.", "besoin": "Claude a besoin de vous"},
	"es": {"essai": "Notificaciones activadas", "essai_t": "Athanor avisará a este dispositivo cuando Claude le necesite, se termine un hito o un ordenador se bloquee.", "jalon": "Hito terminado", "livre": "Proyecto entregado", "livre_t": "El informe final está listo.", "besoin": "Claude le necesita"},
	"de": {"essai": "Benachrichtigungen an", "essai_t": "Athanor meldet sich auf diesem Gerät, wenn Claude Sie braucht, ein Meilenstein fertig ist oder ein Computer hängt.", "jalon": "Meilenstein fertig", "livre": "Projekt ausgeliefert", "livre_t": "Der Abschlussbericht ist fertig.", "besoin": "Claude braucht Sie"},
	"it": {"essai": "Notifiche attive", "essai_t": "Athanor avviserà questo dispositivo quando Claude ha bisogno di te, un traguardo è finito o un computer è bloccato.", "jalon": "Traguardo completato", "livre": "Progetto consegnato", "livre_t": "Il rapporto finale è pronto.", "besoin": "Claude ha bisogno di te"},
	"pt": {"essai": "Notificações ativadas", "essai_t": "O Athanor avisará este dispositivo quando o Claude precisar de si, um marco estiver terminado ou um computador estiver bloqueado.", "jalon": "Marco concluído", "livre": "Projeto entregue", "livre_t": "O relatório final está pronto.", "besoin": "O Claude precisa de si"},
}

func (s *Serveur) textePush(cle string) string {
	l, ok := textesPush[s.langue()]
	if !ok {
		l = textesPush["en"]
	}
	return l[cle]
}

func (s *Serveur) envoyerPushEssai(a AbonnementPush) error {
	return s.envoyerPush(a, MessagePush{Titre: s.textePush("essai"), Texte: s.textePush("essai_t"), URL: "/", Tag: "essai"})
}

// surveillerJalonsPush: a milestone ticked, a project delivered: said on the devices with notifications on
// (the first look at each project only learns what is already there).
func (s *Serveur) surveillerJalonsPush() {
	faits := map[string]map[string]bool{}
	livres := map[string]bool{}
	for {
		time.Sleep(20 * time.Second)
		s.mu.Lock()
		projets := append([]Projet{}, s.config.Projets...)
		avecPush := false
		for _, a := range s.config.Distance.Appareils {
			avecPush = avecPush || a.Push != nil
		}
		s.mu.Unlock()
		for _, p := range projets {
			if p.Archive || !p.Existe() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(p.Atelier(), "PLAN.md"))
			if err != nil {
				continue
			}
			avant, connu := faits[p.Dossier]
			maintenant := map[string]bool{}
			for _, j := range analyserPlan(string(b)) {
				if j.Fait {
					maintenant[j.Titre] = true
					if connu && !avant[j.Titre] && avecPush {
						s.notifierAppareils(MessagePush{Titre: s.textePush("jalon") + " · " + p.Nom, Texte: j.Titre, URL: "/#suivi", Tag: "jalon-" + p.Nom})
					}
				}
			}
			faits[p.Dossier] = maintenant
			r, _ := os.ReadFile(filepath.Join(p.Atelier(), "FINAL_REPORT.md"))
			livre := reStatusDonePilote.Match(r)
			if v, vu := livres[p.Dossier]; vu && livre && !v && avecPush {
				s.notifierAppareils(MessagePush{Titre: s.textePush("livre") + " · " + p.Nom, Texte: s.textePush("livre_t"), URL: "/#suivi", Tag: "livre-" + p.Nom})
			}
			livres[p.Dossier] = livre
		}
	}
}
