package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type allerRetour func(*http.Request) (*http.Response, error)

func (f allerRetour) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestApprobationTelephone (P7): an agent asks an access (twice): the paired phone is told once; through
// the tunnel, the paired phone reads and accepts it (native mode: no password needed from a device), a
// client of the tunnel that is not paired is refused, and the hub's MCP is never reached from there.
func TestApprobationTelephone(t *testing.T) {
	s := serveurCoffreTest(t)
	natif(t, true) // after nouveauServeur, which sets it from the config
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	demo := Projet{Nom: "demo", Dossier: filepath.Join(t.TempDir(), "demo")}
	if err := os.MkdirAll(filepath.Join(demo.Atelier(), "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.config.Projets, s.config.Langue = []Projet{demo}, "fr"
	temoin := "temoin-tel-" + aleatoire(6)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Stripe test", Famille: "secret", Type: "stripe"}, temoin)
	if err != nil {
		t.Fatal(err)
	}

	// the tunnel and a paired phone with notifications on (as serveurDistance)
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	s.mux = mux
	s.config.Distance.Actif = true
	s.dist().tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
	h := s.gestionDistance(mux)
	cle := appairerTest(t, s, h)
	api := []string{"X-Athanor", "jeton"}
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	s.mu.Lock()
	s.config.Distance.Appareils[0].Push = &AbonnementPush{Endpoint: "https://fcm.googleapis.com/fcm/send/tel",
		P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
	idTel := s.config.Distance.Appareils[0].ID
	s.mu.Unlock()

	var mu sync.Mutex
	var recus []MessagePush
	ancien := clientPush
	clientPush = &http.Client{Transport: allerRetour(func(r *http.Request) (*http.Response, error) {
		var m MessagePush
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(dechiffrerPushEssai(t, ua, auth, b), &m)
		mu.Lock()
		recus = append(recus, m)
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody, Request: r}, nil
	})}
	t.Cleanup(func() { clientPush = ancien })
	nRecus := func() []MessagePush {
		mu.Lock()
		defer mu.Unlock()
		return append([]MessagePush{}, recus...)
	}

	// the pilot asks, then asks again at its next cycle: one approval, one notification
	sess := SessionHub{Projet: "demo", Agent: "pilote"}
	arg := json.RawMessage(`{"nom":"STRIPE_SECRET_KEY","raison":"paiements de test"}`)
	for i := 0; i < 2; i++ {
		if _, err := outilDemanderAcces(s, sess, arg); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := s.approbations.Liste()
	if len(l) != 1 {
		t.Fatalf("%d approvals", len(l))
	}
	id := l[0].ID
	for i := 0; i < 100 && len(nRecus()) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // a second notification would have come by now
	m := nRecus()
	if len(m) != 1 {
		t.Fatalf("%d notifications, want 1", len(m))
	}
	if m[0].Titre != "Studio, demo : accès demandé" || m[0].URL != "/#pourvous" || m[0].Tag != "appr-"+id ||
		!strings.Contains(m[0].Texte, "STRIPE_SECRET_KEY") || strings.Contains(m[0].Texte, temoin) {
		t.Fatalf("notification: %+v", m[0])
	}

	corps, _ := json.Marshal(map[string]string{"id": id, "decision": "accepter", "entree": e.ID})
	// not paired (no key, or an unknown one): refused, nothing decided
	for _, c := range []string{"", "inconnue"} {
		if w := appelDistance(h, "POST", "/api/hub/approbations", c, string(corps), api...); w.Code != http.StatusUnauthorized {
			t.Errorf("unpaired client %q: %d", c, w.Code)
		}
		if w := appelDistance(h, "GET", "/api/hub/approbations", c, "", api...); w.Code != http.StatusUnauthorized {
			t.Errorf("unpaired client %q reads: %d", c, w.Code)
		}
	}
	if s.approbations.PourAgent(id) != "attente" {
		t.Fatal("decided by an unpaired client")
	}
	// the hub's MCP and the other hub routes: never from the phone
	for _, r := range [][2]string{{"POST", "/api/mcp"}, {"POST", "/api/hub/alertes"}, {"POST", "/api/coffre"}, {"POST", "/api/acces"}} {
		if w := appelDistance(h, r[0], r[1], cle, "{}", api...); w.Code != http.StatusForbidden {
			t.Errorf("%s %s from the phone: %d", r[0], r[1], w.Code)
		}
	}
	// the paired phone reads, then accepts (native mode: a device needs no password)
	w := appelDistance(h, "GET", "/api/hub/approbations", cle, "", api...)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id) {
		t.Fatalf("phone reads: %d %s", w.Code, w.Body)
	}
	w = appelDistance(h, "POST", "/api/hub/approbations", cle, string(corps), api...)
	if w.Code != 200 || s.approbations.PourAgent(id) != "acceptee" {
		t.Fatalf("phone accepts: %d %s", w.Code, w.Body)
	}
	if x, _ := s.approbations.Lire(id); x.Par != "appareil:"+idTel {
		t.Errorf("decided by %q", x.Par)
	}
	b, _ := os.ReadFile(filepath.Join(demo.Atelier(), "secrets.env"))
	if !strings.Contains(string(b), "STRIPE_SECRET_KEY='"+temoin+"'") {
		t.Errorf("secrets.env without the value: %q", b)
	}
	if strings.Contains(w.Body.String(), temoin) || strings.Contains(auditTexte(t), temoin) {
		t.Error("value in the answer or the audit log")
	}
	if len(nRecus()) != 1 {
		t.Error("a decision sent a notification")
	}
}
