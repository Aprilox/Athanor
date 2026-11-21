package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// fauxFournisseurs: a fake Cloudflare and a fake GitHub (never a real account) that accept only the
// tokens given; GitHub sends scopes only for the classic one, like the real API.
func fauxFournisseurs(t *testing.T, cloudflare, classique, finGrain string) {
	t.Helper()
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cloudflare {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"success":false}`))
			return
		}
		switch r.URL.Path {
		case "/client/v4/user/tokens/verify":
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"x","status":"active"}}`))
		case "/client/v4/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"name":"martin.fr"},{"name":"exemple.org"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/user":
			w.WriteHeader(404)
		case r.Header.Get("Authorization") == "Bearer "+classique:
			w.Header().Set("X-OAuth-Scopes", "repo, workflow")
			_, _ = w.Write([]byte(`{"login":"martin-dev"}`))
		case r.Header.Get("Authorization") == "Bearer "+finGrain:
			_, _ = w.Write([]byte(`{"login":"martin-dev"}`))
		default:
			w.WriteHeader(401)
		}
	}))
	t.Cleanup(cf.Close)
	t.Cleanup(gh.Close)
	a, b := urlCloudflare, urlGitHub
	urlCloudflare, urlGitHub = cf.URL, gh.URL
	t.Cleanup(func() { urlCloudflare, urlGitHub = a, b })
}

// sansValeur: the JSON of a result must not hold the value (nor in its note).
func sansValeur(t *testing.T, r ResultatTest, valeur string) string {
	t.Helper()
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), valeur) {
		t.Fatalf("the value is in the result: %s", b)
	}
	return string(b)
}

func TestCoffreTesterFournisseurs(t *testing.T) {
	cf, classique, fin := "TEMOIN-"+aleatoire(16), "ghp_"+aleatoire(36), "github_pat_"+aleatoire(40)
	fauxFournisseurs(t, cf, classique, fin)

	r := testerValeur("cloudflare", cf, "")
	j := sansValeur(t, r, cf)
	if !r.OK || r.Resultat[0].Cle != "Cloudflare token active; zones: {0}" || r.Resultat[0].Args[0] != "martin.fr, exemple.org" ||
		r.Note == nil || r.Note.Args[0] != "martin.fr, exemple.org" {
		t.Fatalf("cloudflare: %s", j)
	}
	if r := testerValeur("cloudflare", "TEMOIN-autre", ""); r.OK || r.Resultat[0].Cle != "Cloudflare refused this token." || r.Note != nil {
		t.Fatalf("cloudflare refused: %+v", r)
	}

	r = testerValeur("github", classique, "")
	j = sansValeur(t, r, classique)
	if !r.OK || r.Resultat[0].Cle != "GitHub account {0}; scopes: {1}" || r.Note.Args[0] != "martin-dev" || r.Note.Args[1] != "repo, workflow" {
		t.Fatalf("github: %s", j)
	}
	r = testerValeur("github", fin, "")
	if j = sansValeur(t, r, fin); !r.OK || r.Note.Cle != "GitHub token of account {0} (fine-grained)" {
		t.Fatalf("github fine-grained: %s", j)
	}
	if r := testerValeur("github", "ghp_"+aleatoire(36), ""); r.OK || r.Resultat[0].Cle != "GitHub refused this token." {
		t.Fatalf("github refused: %+v", r)
	}
	if r := testerValeur("stripe", "sk_test_x", ""); r.OK || r.Resultat[0].Cle != "Athanor cannot test this type yet." {
		t.Fatalf("stripe: %+v", r)
	}

	// an unreachable provider: a clear message, no hang
	urlGitHub = "http://127.0.0.1:1"
	if r := testerValeur("github", classique, ""); r.OK || r.Resultat[0].Cle != "Could not reach {0}." {
		t.Fatalf("unreachable: %+v", r)
	}
}

// clePEMTest: an Ed25519 private key in PKCS#8 PEM, made at run time.
func clePEMTest() string {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	b, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
}

// chaine: an SSH wire-format string.
func chaine(b []byte) []byte { return binary.BigEndian.AppendUint32(nil, uint32(len(b))) }

func blobSSH(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(append(out, chaine(p)...), p...)
	}
	return out
}

// cleOpenSSH: an OpenSSH private key file whose public part is pub (the private part is not read).
func cleOpenSSH(chiffre string, pub []byte) string {
	b := append([]byte("openssh-key-v1\x00"), blobSSH([]byte(chiffre), []byte("none"), nil)...)
	b = append(b, 0, 0, 0, 1)
	b = append(b, blobSSH(pub, []byte(aleatoire(40)))...)
	return string(pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: b}))
}

func TestCoffreTesterSSH(t *testing.T) {
	pubEd, privEd, _ := ed25519.GenerateKey(rand.Reader)
	blobEd := blobSSH([]byte("ssh-ed25519"), pubEd)
	rsaK, _ := rsa.GenerateKey(rand.Reader, 2048)
	pkcs8, _ := x509.MarshalPKCS8PrivateKey(privEd)
	cas := []struct{ valeur, type_ string }{
		{cleOpenSSH("none", blobEd), "Ed25519"},
		{cleOpenSSH("aes256-ctr", blobEd), "Ed25519"},
		{string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaK)})), "RSA 2048"},
		{string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})), "Ed25519"},
		{"ssh-ed25519 " + base64.StdEncoding.EncodeToString(blobEd) + " martin@pc", "Ed25519"},
	}
	for i, c := range cas {
		r := testerValeur("ssh", c.valeur, "")
		sansValeur(t, r, strings.TrimSpace(strings.Split(c.valeur, "\n")[1%len(strings.Split(c.valeur, "\n"))]))
		if !r.OK || r.Resultat[0].Args[0] != c.type_ || r.Note.Args[0] != c.type_ {
			t.Errorf("key %d: %+v", i, r)
		}
		if (i == 1) != (r.Resultat[0].Cle == "SSH key {0}, protected by a passphrase.") {
			t.Errorf("key %d passphrase: %+v", i, r)
		}
	}
	if r := testerValeur("ssh", "pas une clé", ""); r.OK || r.Resultat[0].Cle != "No SSH key was recognised in this value." {
		t.Fatalf("not a key: %+v", r)
	}

	// a server that greets like SSH, one that does not, and a closed port
	serveur := func(accueil string) string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				_, _ = c.Write([]byte(accueil))
				c.Close()
			}
		}()
		return l.Addr().String()
	}
	cle := cas[0].valeur
	hote := serveur("SSH-2.0-FauxServeur_1.0\r\n")
	r := testerValeur("ssh", cle, hote)
	if !r.OK || r.Resultat[1].Cle != "{0} answers: {1}" || r.Resultat[1].Args[1] != "SSH-2.0-FauxServeur_1.0" || r.Note.Cle != "SSH key {0} for {1}" || r.Note.Args[1] != hote {
		t.Fatalf("banner: %+v", r)
	}
	if r := testerValeur("ssh", cle, serveur("HTTP/1.1 400 Bad Request\r\n\r\n")); r.OK || r.Resultat[1].Cle != "{0} does not answer as an SSH server." {
		t.Fatalf("not ssh: %+v", r)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	ferme := l.Addr().String()
	l.Close()
	if r := testerValeur("ssh", cle, ferme); r.OK || r.Resultat[1].Cle != "Could not connect to {0}." {
		t.Fatalf("closed port: %+v", r)
	}
}

// TestCoffreTesterAPI: a saved entry and a pasted text are tested through POST /api/coffre/tester; the
// value is in no answer.
func TestCoffreTesterAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	jeton := "ghp_" + aleatoire(36)
	fauxFournisseurs(t, "x", jeton, "y")
	var reponses strings.Builder
	appel := func(corps any, chemin string) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest("POST", chemin, strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	code, e := appel(map[string]string{"nom": "GITHUB_TOKEN", "famille": "secret", "type": "github", "portee": "code", "valeur": jeton}, "/api/coffre")
	if code != 200 {
		t.Fatalf("add: %d %v", code, e)
	}
	code, v := appel(map[string]string{"id": e["id"].(string)}, "/api/coffre/tester")
	if code != 200 || v["ok"] != true || v["note"].(map[string]any)["cle"] != "GitHub token of account {0}; scopes: {1}" {
		t.Fatalf("saved entry: %d %v", code, v)
	}
	// C15: the type of the form does not send a saved entry to another provider
	if code, v = appel(map[string]string{"id": e["id"].(string), "type": "cloudflare"}, "/api/coffre/tester"); code != 200 || v["type"] != "github" {
		t.Fatalf("saved entry tested as the form type: %d %v", code, v)
	}
	code, v = appel(map[string]any{"texte": "export GITHUB_TOKEN=" + jeton, "i": 0}, "/api/coffre/tester")
	if code != 200 || v["ok"] != true || v["type"] != "github" {
		t.Fatalf("pasted text: %d %v", code, v)
	}
	if code, _ = appel(map[string]any{"texte": "GITHUB_TOKEN=" + jeton, "i": 3}, "/api/coffre/tester"); code != 400 {
		t.Fatalf("bad index: %d", code)
	}
	if code, _ = appel(map[string]string{"id": "inconnu"}, "/api/coffre/tester"); code != 404 {
		t.Fatalf("unknown id: %d", code)
	}
	if strings.Contains(reponses.String(), jeton) || strings.Contains(reponses.String(), jeton[4:]) {
		t.Fatal("the value is in an answer")
	}
}

// TestCoffreTesterTraduction: every message of coffre_tester.go is listed in clesTesteur and translated
// in the 5 languages of the page.
func TestCoffreTesterTraduction(t *testing.T) {
	src, err := os.ReadFile("coffre_tester.go")
	if err != nil {
		t.Fatal(err)
	}
	liste := map[string]bool{}
	for _, c := range clesTesteur {
		liste[c] = true
	}
	trouves := regexp.MustCompile(`messageTest\("([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(trouves) < 20 {
		t.Fatalf("%d messages found in coffre_tester.go", len(trouves))
	}
	for _, m := range trouves {
		if !liste[m[1]] {
			t.Errorf("not in clesTesteur: %q", m[1])
		}
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range clesTesteur {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}

// TestBaseCloudflare: the stand-in address is taken only under ATHANOR_TEST=1 and only on this machine.
func TestBaseCloudflare(t *testing.T) {
	const vraie, locale = "https://api.cloudflare.com", "http://127.0.0.1:4567"
	for _, c := range []struct{ test, api, attendu string }{
		{"", "", vraie},
		{"", locale, vraie},
		{"1", "", vraie},
		{"1", locale, locale},
		{"1", "https://ailleurs.example", vraie},
		{"1", "http://127.0.0.1.ailleurs.example:80", vraie},
	} {
		t.Setenv("ATHANOR_TEST", c.test)
		t.Setenv("ATHANOR_CLOUDFLARE_API", c.api)
		if u := baseCloudflare(); u != c.attendu {
			t.Errorf("ATHANOR_TEST=%q ATHANOR_CLOUDFLARE_API=%q: %s, expected %s", c.test, c.api, u, c.attendu)
		}
	}
}
