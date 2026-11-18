package main

// "Test" on the Vault page: Athanor's Go code checks a key with its provider (Cloudflare, GitHub) or reads
// it (SSH: key type, optional banner of a server) and proposes a note for the AI built from the result,
// never from the value. Messages are {cle, args}: English keys translated by the page (i18n.js).

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// the providers' base URLs: replaced only by the tests (httptest), never pointed at a real account there
var (
	urlCloudflare = baseCloudflare()
	urlGitHub     = "https://api.github.com"
)

// baseCloudflare: the Cloudflare API, or the test harness's local stand-in ATHANOR_CLOUDFLARE_API: only under
// ATHANOR_TEST=1 and only on this machine (a token never leaves for another host), never in a real launch.
func baseCloudflare() string {
	if u := os.Getenv("ATHANOR_CLOUDFLARE_API"); os.Getenv("ATHANOR_TEST") == "1" && strings.HasPrefix(u, "http://127.0.0.1:") {
		return u
	}
	return "https://api.cloudflare.com"
}

const (
	delaiTesteur    = 10 * time.Second
	delaiTesteurSSH = 5 * time.Second
)

// MessageTest: a sentence for the page, an English key and its arguments.
type MessageTest struct {
	Cle  string   `json:"cle"`
	Args []string `json:"args"`
}

// ResultatTest: what a test found; Note is the proposal for the AI (nil when nothing useful was learnt).
type ResultatTest struct {
	Type     string        `json:"type"`
	OK       bool          `json:"ok"`
	Resultat []MessageTest `json:"resultat"`
	Note     *MessageTest  `json:"note,omitempty"`
}

// clesTesteur: every key a test can answer, checked in the 5 languages by TestCoffreTesterTraduction.
var clesTesteur = []string{
	"Athanor cannot test this type yet.",
	"Could not reach {0}.",
	"{0} answered with an unexpected error ({1}).",
	"Cloudflare refused this token.",
	"Cloudflare token {0}.",
	"Cloudflare token active; zones: {0}",
	"Cloudflare token active; no zone visible.",
	"Cloudflare API token; zones: {0}",
	"Cloudflare API token (no zone visible)",
	"GitHub refused this token.",
	"GitHub account {0}; scopes: {1}",
	"GitHub account {0}; fine-grained token (its permissions are set on GitHub).",
	"GitHub token of account {0}; scopes: {1}",
	"GitHub token of account {0} (fine-grained)",
	"No SSH key was recognised in this value.",
	"SSH key {0}.",
	"SSH key {0}, protected by a passphrase.",
	"SSH key {0} for {1}",
	"{0} answers: {1}",
	"Could not connect to {0}.",
	"{0} does not answer as an SSH server.",
	"Logging in with this key is not tested yet: it comes with the Servers module.",
}

func messageTest(cle string, args ...string) MessageTest {
	if args == nil {
		args = []string{}
	}
	return MessageTest{Cle: cle, Args: args}
}

// testerValeur: the test of a value by its type; hote ("host[:port]") is only used by SSH.
func testerValeur(typ, valeur, hote string) ResultatTest {
	switch typ {
	case "cloudflare":
		return testerCloudflare(strings.TrimSpace(valeur))
	case "github":
		return testerGitHub(strings.TrimSpace(valeur))
	case "ssh":
		return testerSSH(valeur, hote)
	}
	return ResultatTest{Type: typ, Resultat: []MessageTest{messageTest("Athanor cannot test this type yet.")}}
}

// appelFournisseur: GET base+chemin with the token, the JSON decoded into v; code is the HTTP status, 0 if unreachable.
func appelFournisseur(base, chemin, jeton string, v any) (int, http.Header) {
	req, err := http.NewRequest("GET", base+chemin, nil)
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Authorization", "Bearer "+jeton)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Athanor")
	resp, err := (&http.Client{Timeout: delaiTesteur}).Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
	return resp.StatusCode, resp.Header
}

// erreurFournisseur: the message for an answer that is neither a success nor a refusal.
func erreurFournisseur(typ, nom string, code int) ResultatTest {
	if code == 0 {
		return ResultatTest{Type: typ, Resultat: []MessageTest{messageTest("Could not reach {0}.", nom)}}
	}
	return ResultatTest{Type: typ, Resultat: []MessageTest{messageTest("{0} answered with an unexpected error ({1}).", nom, strconv.Itoa(code))}}
}

func testerCloudflare(jeton string) ResultatTest {
	r := ResultatTest{Type: "cloudflare"}
	var verif struct {
		Success bool `json:"success"`
		Result  struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	code, _ := appelFournisseur(urlCloudflare, "/client/v4/user/tokens/verify", jeton, &verif)
	switch {
	case code == 401 || code == 403 || code == 400 || (code == 200 && !verif.Success):
		r.Resultat = []MessageTest{messageTest("Cloudflare refused this token.")}
		return r
	case code != 200:
		return erreurFournisseur("cloudflare", "Cloudflare", code)
	case verif.Result.Status != "active":
		r.Resultat = []MessageTest{messageTest("Cloudflare token {0}.", texteSur(verif.Result.Status, 30))}
		return r
	}
	var zones struct {
		Result []struct {
			Name string `json:"name"`
		} `json:"result"`
	}
	r.OK = true
	if code, _ := appelFournisseur(urlCloudflare, "/client/v4/zones?per_page=50", jeton, &zones); code == 200 && len(zones.Result) > 0 {
		noms := make([]string, 0, len(zones.Result))
		for _, z := range zones.Result {
			noms = append(noms, texteSur(z.Name, 100))
		}
		liste := strings.Join(noms, ", ")
		r.Resultat = []MessageTest{messageTest("Cloudflare token active; zones: {0}", liste)}
		n := messageTest("Cloudflare API token; zones: {0}", liste)
		r.Note = &n
		return r
	}
	r.Resultat = []MessageTest{messageTest("Cloudflare token active; no zone visible.")}
	n := messageTest("Cloudflare API token (no zone visible)")
	r.Note = &n
	return r
}

func testerGitHub(jeton string) ResultatTest {
	r := ResultatTest{Type: "github"}
	var u struct {
		Login string `json:"login"`
	}
	code, entetes := appelFournisseur(urlGitHub, "/user", jeton, &u)
	switch {
	case code == 401 || code == 403:
		r.Resultat = []MessageTest{messageTest("GitHub refused this token.")}
		return r
	case code != 200:
		return erreurFournisseur("github", "GitHub", code)
	}
	r.OK = true
	compte := texteSur(u.Login, 60)
	var n MessageTest
	if _, classique := entetes["X-Oauth-Scopes"]; classique { // only classic tokens send their scopes
		droits := texteSur(entetes.Get("X-OAuth-Scopes"), 300)
		if droits == "" {
			droits = "—"
		}
		r.Resultat = []MessageTest{messageTest("GitHub account {0}; scopes: {1}", compte, droits)}
		n = messageTest("GitHub token of account {0}; scopes: {1}", compte, droits)
	} else {
		r.Resultat = []MessageTest{messageTest("GitHub account {0}; fine-grained token (its permissions are set on GitHub).", compte)}
		n = messageTest("GitHub token of account {0} (fine-grained)", compte)
	}
	r.Note = &n
	return r
}

// texteSur: an answer from outside made printable (no control character, no markup surprise) and bounded.
func texteSur(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	return tronquerRunes(s, max)
}

func testerSSH(valeur, hote string) ResultatTest {
	r := ResultatTest{Type: "ssh"}
	cle, chiffree := typeCleSSH(valeur)
	if cle == "" {
		r.Resultat = []MessageTest{messageTest("No SSH key was recognised in this value.")}
		return r
	}
	r.OK = true
	if chiffree {
		r.Resultat = append(r.Resultat, messageTest("SSH key {0}, protected by a passphrase.", cle))
	} else {
		r.Resultat = append(r.Resultat, messageTest("SSH key {0}.", cle))
	}
	n := messageTest("SSH key {0}.", cle)
	if hote = strings.TrimSpace(hote); hote != "" {
		if _, _, err := net.SplitHostPort(hote); err != nil {
			hote = net.JoinHostPort(strings.Trim(hote, "[]"), "22")
		}
		hote = tronquerRunes(hote, 255)
		banniere, err := banniereSSH(hote)
		switch {
		case errors.Is(err, errPasSSH):
			r.OK = false
			r.Resultat = append(r.Resultat, messageTest("{0} does not answer as an SSH server.", hote))
		case err != nil:
			r.OK = false
			r.Resultat = append(r.Resultat, messageTest("Could not connect to {0}.", hote))
		default:
			r.Resultat = append(r.Resultat, messageTest("{0} answers: {1}", hote, banniere))
			n = messageTest("SSH key {0} for {1}", cle, hote)
		}
	}
	r.Resultat = append(r.Resultat, messageTest("Logging in with this key is not tested yet: it comes with the Servers module."))
	r.Note = &n
	return r
}

var errPasSSH = errors.New("not an SSH server")

// banniereSSH: the "SSH-2.0-…" line a server sends first (RFC 4253 allows a few lines before it).
func banniereSSH(hote string) (string, error) {
	c, err := net.DialTimeout("tcp", hote, delaiTesteurSSH)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(delaiTesteurSSH))
	lecteur := bufio.NewReaderSize(c, 512)
	for range 5 {
		ligne, err := lecteur.ReadSlice('\n')
		if strings.HasPrefix(string(ligne), "SSH-") {
			return texteSur(string(ligne), 100), nil
		}
		if err != nil {
			break
		}
	}
	return "", errPasSSH
}

// typeCleSSH: the kind of key in a PEM block or an OpenSSH public key line ("Ed25519", "RSA 4096"…),
// and whether it is protected by a passphrase; "" when nothing is recognised.
func typeCleSSH(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if champs := strings.Fields(v); len(champs) >= 2 && (strings.HasPrefix(champs[0], "ssh-") || strings.HasPrefix(champs[0], "ecdsa-") || strings.HasPrefix(champs[0], "sk-")) {
		if blob, err := base64.StdEncoding.DecodeString(champs[1]); err == nil {
			return typeBlobSSH(blob), false
		}
	}
	b, _ := pem.Decode([]byte(v))
	if b == nil {
		return "", false
	}
	chiffree := strings.Contains(b.Headers["Proc-Type"], "ENCRYPTED")
	switch b.Type {
	case "OPENSSH PRIVATE KEY":
		return lireCleOpenSSH(b.Bytes)
	case "ENCRYPTED PRIVATE KEY":
		return "PKCS#8", true
	case "RSA PRIVATE KEY":
		if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
			return "RSA " + strconv.Itoa(k.N.BitLen()), false
		}
		return "RSA", chiffree
	case "EC PRIVATE KEY":
		if k, err := x509.ParseECPrivateKey(b.Bytes); err == nil {
			return "ECDSA " + k.Curve.Params().Name, false
		}
		return "ECDSA", chiffree
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
		if err != nil {
			return "", false
		}
		switch k := k.(type) {
		case *rsa.PrivateKey:
			return "RSA " + strconv.Itoa(k.N.BitLen()), false
		case *ecdsa.PrivateKey:
			return "ECDSA " + k.Curve.Params().Name, false
		case ed25519.PrivateKey:
			return "Ed25519", false
		}
	}
	return "", false
}

// chaineSSH: one length-prefixed string of the SSH wire format, and the rest.
func chaineSSH(b []byte) ([]byte, []byte, bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// lireCleOpenSSH: "openssh-key-v1\0", cipher, kdf, kdf options, key count, then the public key (readable
// even when the private part is encrypted).
func lireCleOpenSSH(b []byte) (string, bool) {
	const magie = "openssh-key-v1\x00"
	if !strings.HasPrefix(string(b), magie) {
		return "", false
	}
	b = b[len(magie):]
	chiffre, b, ok := chaineSSH(b)
	if !ok {
		return "", false
	}
	for range 2 { // kdf name and options
		if _, b, ok = chaineSSH(b); !ok {
			return "", false
		}
	}
	if len(b) < 4 {
		return "", false
	}
	pub, _, ok := chaineSSH(b[4:])
	if !ok {
		return "", false
	}
	t := typeBlobSSH(pub)
	return t, t != "" && string(chiffre) != "none"
}

// typeBlobSSH: the readable kind of an SSH public key blob.
func typeBlobSSH(blob []byte) string {
	nom, reste, ok := chaineSSH(blob)
	if !ok {
		return ""
	}
	switch string(nom) {
	case "ssh-ed25519":
		return "Ed25519"
	case "ssh-rsa":
		if _, reste, ok = chaineSSH(reste); ok { // e, then n
			if n, _, ok := chaineSSH(reste); ok {
				return "RSA " + strconv.Itoa(new(big.Int).SetBytes(n).BitLen())
			}
		}
		return "RSA"
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return "ECDSA P-" + strings.TrimPrefix(string(nom), "ecdsa-sha2-nistp")
	case "sk-ssh-ed25519@openssh.com":
		return "Ed25519-SK"
	}
	return ""
}

// apiCoffreTester: POST /api/coffre/tester {id} (a saved entry) or {texte, i} (proposal i of a pasted
// text, analysed again here), with optional {type, hote}. The answer never holds the value. A saved Cloudflare
// API of the infra vault gets the longer test (zones and rights), kept as its sheet (testerFiche).
func (s *Serveur) apiCoffreTester(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	var c struct {
		ID    string `json:"id"`
		Texte string `json:"texte"`
		I     int    `json:"i"`
		Type  string `json:"type"`
		Hote  string `json:"hote"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxValeurCoffre)
	if err := lireCorps(r, &c); err != nil || len(c.Hote) > 300 {
		s.erreurCoffre(w, errCoffreRequete)
		return
	}
	var typ, valeur string
	if c.ID != "" {
		for _, x := range s.coffre.Liste() {
			if x.ID == c.ID {
				typ = x.Type
			}
		}
		v, err := s.coffre.Valeur(c.ID)
		if err != nil {
			s.erreurCoffre(w, err)
			return
		}
		valeur = v
	} else {
		props := analyserColle(c.Texte)
		if c.I < 0 || c.I >= len(props) {
			s.erreurCoffre(w, errCoffreRequete)
			return
		}
		typ, valeur = props[c.I].Type, props[c.I].valeur
	}
	// C15: a saved entry is tested as its own type (a Stripe key never goes to GitHub because of the form)
	if _, connu := typesCoffre[c.Type]; connu && c.ID == "" {
		typ = c.Type
	}
	if e, err := s.cleFournisseur(c.ID); err == nil { // M289: a Cloudflare API of the infra vault keeps what Test learns
		f, err := s.testerFiche(e, strings.TrimSpace(valeur), deQui(r))
		if err != nil {
			s.erreurCoffre(w, err)
			return
		}
		res := ResultatTest{Type: typ, OK: f.Etat == "ok", Resultat: []MessageTest{*f.Bilan}}
		if res.OK {
			n := messageTest("Cloudflare API token; zones: {0}", f.Bilan.Args...)
			res.Note = &n
		}
		s.ecrireJSON(w, res)
		return
	}
	s.ecrireJSON(w, testerValeur(typ, valeur, c.Hote))
}
