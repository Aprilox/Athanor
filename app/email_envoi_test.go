package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fauxSMTP: a tiny SMTP server on 127.0.0.1, with STARTTLS or implicit TLS, AUTH PLAIN and LOGIN.
type fauxSMTP struct {
	ln        net.Listener
	conf      *tls.Config
	implicite bool // TLS from the first byte (port 465)
	starttls  bool // offers STARTTLS
	user, mdp string
	mu        sync.Mutex
	recus     []string
}

func (f *fauxSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fauxSMTP) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.recus...)
}

func (f *fauxSMTP) servir(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if f.implicite {
		c = tls.Server(c, f.conf)
	}
	tp := textproto.NewConn(c)
	_ = tp.PrintfLine("220 faux ESMTP")
	chiffre := f.implicite
	for {
		l, err := tp.ReadLine()
		if err != nil {
			return
		}
		champs := strings.Fields(l)
		if len(champs) == 0 {
			continue
		}
		switch strings.ToUpper(champs[0]) {
		case "EHLO", "HELO":
			lignes := []string{"faux"}
			if f.starttls && !chiffre {
				lignes = append(lignes, "STARTTLS")
			}
			lignes = append(lignes, "AUTH PLAIN LOGIN")
			for i, x := range lignes {
				sep := "-"
				if i == len(lignes)-1 {
					sep = " "
				}
				_ = tp.PrintfLine("250%s%s", sep, x)
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 go ahead")
			tc := tls.Server(c, f.conf)
			if tc.Handshake() != nil {
				return
			}
			c, tp, chiffre = tc, textproto.NewConn(tc), true
		case "AUTH":
			b := []byte{}
			if len(champs) > 2 {
				b, _ = base64.StdEncoding.DecodeString(champs[2])
			}
			if string(b) == "\x00"+f.user+"\x00"+f.mdp {
				_ = tp.PrintfLine("235 ok")
			} else {
				_ = tp.PrintfLine("535 5.7.8 Authentication failed")
			}
		case "DATA":
			_ = tp.PrintfLine("354 go")
			lignes, err := tp.ReadDotLines()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.recus = append(f.recus, strings.Join(lignes, "\n"))
			f.mu.Unlock()
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default: // MAIL, RCPT, RSET, NOOP
			_ = tp.PrintfLine("250 ok")
		}
	}
}

// demarrerFauxSMTP: a fake server whose certificate (127.0.0.1) the module trusts for the test's duration.
func demarrerFauxSMTP(t *testing.T, implicite, starttls bool, user, mdp string) *fauxSMTP {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	modele := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, modele, modele, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	if racinesEmail != nil {
		pool = racinesEmail.Clone() // the servers started before stay trusted
	}
	pool.AddCert(cert)
	avant := racinesEmail
	racinesEmail = pool
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fauxSMTP{ln: ln, conf: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}},
		implicite: implicite, starttls: starttls, user: user, mdp: mdp}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.servir(c)
		}
	}()
	t.Cleanup(func() { ln.Close(); racinesEmail = avant })
	return f
}

// TestEmailEnvoi: a test send reaches the fake server (STARTTLS and implicit TLS); a wrong password, a server
// without STARTTLS, a TLS mismatch, a closed port and a silent server each give their plain-words error; the
// password is never in an answer, comptes.json nor the audit log; 5 tests a minute; a failure rings the bell.
func TestEmailEnvoi(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "témoin-" + aleatoire(12) // built at run time: no fake secret in the repository
	var reponses strings.Builder
	appel := func(methode, chemin string, corps any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest(methode, chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	compte := func(nom string, port int, securite, mdp string) string {
		t.Helper()
		code, v := appel("POST", "/api/email/comptes", map[string]any{"nom": nom, "hote": "127.0.0.1", "port": port, "securite": securite,
			"utilisateur": "contact@exemple.fr", "expediteur": "Boutique <contact@exemple.fr>", "mot_de_passe": mdp})
		if code != 200 {
			t.Fatalf("add %s: %d %v", nom, code, v)
		}
		return v["id"].(string)
	}
	tester := func(id string) (int, map[string]any) {
		t.Helper()
		testsEmail = nil // the per-minute limit is checked on its own below
		return appel("POST", "/api/email/comptes/"+id+"/test", map[string]any{"destinataire": "client@exemple.fr"})
	}

	starttls := demarrerFauxSMTP(t, false, true, "contact@exemple.fr", temoin)
	code, v := tester(compte("Starttls", starttls.port(), "starttls", temoin))
	if code != 200 || v["ok"] != true || v["message"] != msgEnvoiOK {
		t.Fatalf("STARTTLS send: %d %v", code, v)
	}
	if m := starttls.messages(); len(m) != 1 || !strings.Contains(m[0], "To: <client@exemple.fr>") || !strings.Contains(m[0], "test e-mail") {
		t.Fatalf("message received: %q", m)
	}
	implicite := demarrerFauxSMTP(t, true, false, "contact@exemple.fr", temoin)
	if code, v := tester(compte("Implicite", implicite.port(), "tls", temoin)); code != 200 || v["ok"] != true || len(implicite.messages()) != 1 {
		t.Fatalf("implicit TLS send: %d %v", code, v)
	}

	// each failure in plain words, nothing received
	sansTLS := demarrerFauxSMTP(t, false, false, "contact@exemple.fr", temoin)
	ferme, _ := net.Listen("tcp", "127.0.0.1:0")
	portFerme := ferme.Addr().(*net.TCPAddr).Port
	ferme.Close()
	muet, _ := net.Listen("tcp", "127.0.0.1:0") // accepts, never speaks
	defer muet.Close()
	go func() {
		for {
			c, err := muet.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	idMauvais := compte("Mauvais", starttls.port(), "starttls", "pas-le-bon-"+aleatoire(6))
	for _, c := range []struct {
		id   string
		veut erreurEnvoi
	}{
		{idMauvais, errEnvoiIdentifiants},
		{compte("SansTLS", sansTLS.port(), "starttls", temoin), errEnvoiTLSAbsent},
		{compte("Melange", sansTLS.port(), "tls", temoin), errEnvoiTLS},
		{compte("Ferme", portFerme, "starttls", temoin), errEnvoiConnexion},
	} {
		if code, v := tester(c.id); code != 200 || v["ok"] != false || v["message"] != c.veut.Error() {
			t.Errorf("%s: %d %v, want %q", c.id, code, v, c.veut)
		}
	}
	avant := delaiEnvoiEmail
	delaiEnvoiEmail = 300 * time.Millisecond
	code, v = tester(compte("Muet", muet.Addr().(*net.TCPAddr).Port, "starttls", temoin))
	delaiEnvoiEmail = avant
	if v["message"] != errEnvoiDelai.Error() {
		t.Errorf("silent server: %d %v", code, v)
	}
	if n := len(starttls.messages()) + len(sansTLS.messages()); n != 1 {
		t.Errorf("%d messages received after the failures, want 1", n)
	}

	// the last result on the card; a failed one rings the bell
	liste, _ := lireComptesEmail()
	for _, c := range liste {
		if c.DernierTest == nil || c.DernierTest.Destinataire != "client@exemple.fr" || c.DernierTest.T == "" {
			t.Errorf("%s: last test %+v", c.Nom, c.DernierTest)
		}
	}
	cloche := map[string]bool{}
	for _, e := range (moduleEmail{}).Elements(s) {
		cloche[e.Titre] = e.Attente != nil && e.Attente.Raison == "The test e-mail of {0} failed: {1}"
	}
	if !cloche["Mauvais"] || !cloche["Muet"] || len(cloche) != 5 {
		t.Errorf("bell: %v", cloche)
	}
	// a changed account forgets its failed test (and leaves the bell)
	appel("POST", "/api/email/comptes/"+idMauvais, map[string]any{"nom": "Mauvais", "hote": "127.0.0.1", "port": starttls.port(), "securite": "starttls",
		"utilisateur": "contact@exemple.fr", "expediteur": "Boutique <contact@exemple.fr>", "mot_de_passe": temoin})
	if code, v := tester(idMauvais); v["ok"] != true {
		t.Errorf("after the password fix: %d %v", code, v)
	}

	// refusals: a bad recipient, an unknown account, 5 tests a minute, refused from a device
	if code, v := appel("POST", "/api/email/comptes/"+idMauvais+"/test", map[string]any{"destinataire": "pas une adresse\r\nBcc: x@y.z"}); code != 400 || v["erreur"] != errEnvoiDest.Error() {
		t.Errorf("bad recipient: %d %v", code, v)
	}
	if code, _ := tester("inconnu"); code != 404 {
		t.Errorf("unknown account: %d", code)
	}
	maintenant := time.Now()
	testsEmail = nil
	for i := range 5 {
		if !testEmailPermis(maintenant) {
			t.Fatalf("test %d refused", i+1)
		}
	}
	if testEmailPermis(maintenant) || !testEmailPermis(maintenant.Add(61*time.Second)) {
		t.Error("the limit of 5 tests a minute is not kept")
	}
	testsEmail = []time.Time{maintenant, maintenant, maintenant, maintenant, maintenant}
	if code, _ := appel("POST", "/api/email/comptes/"+idMauvais+"/test", map[string]any{"destinataire": "client@exemple.fr"}); code != http.StatusTooManyRequests {
		t.Errorf("6th test in a minute: %d", code)
	}
	testsEmail = nil
	if !refuseHubDistance(httptest.NewRequest("POST", "/api/email/comptes/"+idMauvais+"/test", nil)) {
		t.Error("test send allowed from a device")
	}

	// the password: never in an answer, comptes.json nor the audit log, which has the test's lines
	if strings.Contains(reponses.String(), temoin) {
		t.Error("the password is in an API answer")
	}
	if b, _ := os.ReadFile(fichierComptesEmail()); strings.Contains(string(b), temoin) || !strings.Contains(string(b), "dernier_test") {
		t.Errorf("comptes.json: %s", b)
	}
	journal := ""
	fichiers, _ := filepath.Glob(filepath.Join(dossierJournalAudit(), "*.jsonl"))
	for _, f := range fichiers {
		b, _ := os.ReadFile(f)
		journal += string(b)
	}
	if strings.Contains(journal, temoin) || strings.Count(journal, "test d'envoi e-mail") < 7 ||
		!strings.Contains(journal, "Mauvais → client@exemple.fr : "+errEnvoiIdentifiants.Error()) {
		t.Errorf("audit log (%d files): %s", len(fichiers), journal)
	}
}
