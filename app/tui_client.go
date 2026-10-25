package main

// The terminal interface talks to Athanor's server as its window does: a single-use address gives it the
// access cookie, the page gives it the secret header, then the same API (nothing of its own on the server's
// side, so the terminal and the window always show the same thing).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type clientTUI struct {
	base   string // http://127.0.0.1:port
	http   *http.Client
	csrf   string
	projet string // the project shown (?p=…), "" before the first answer
}

var reCSRF = regexp.MustCompile(`<meta name="csrf" content="([^"]+)">`)

// nouveauClientTUI: the access through a single-use address (lienOuverture).
func nouveauClientTUI(lien string) (*clientTUI, error) {
	u, err := url.Parse(lien)
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid address")
	}
	jar, _ := cookiejar.New(nil)
	c := &clientTUI{base: u.Scheme + "://" + u.Host, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	r, err := c.http.Get(lien) // the cookie, then the page (its secret header)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	m := reCSRF.FindSubmatch(b)
	if r.StatusCode != http.StatusOK || m == nil {
		return nil, fmt.Errorf("access refused (%d)", r.StatusCode)
	}
	c.csrf = string(m[1])
	return c, nil
}

// erreurAPI: what the server answered with an error (a message key, translated by the interface).
type erreurAPI struct {
	Cle    string
	Code   int
	Verrou bool // 401: the application is locked
}

func (e *erreurAPI) Error() string { return e.Cle }

// avecProjet: the path with ?p= the project shown.
func (c *clientTUI) avecProjet(chemin string) string {
	if c.projet == "" {
		return chemin
	}
	sep := "?"
	if strings.Contains(chemin, "?") {
		sep = "&"
	}
	return chemin + sep + "p=" + url.QueryEscape(c.projet)
}

// appel: one request of the API (corps as JSON), its answer decoded into v (nil: ignored).
func (c *clientTUI) appel(methode, chemin string, corps, v any) error {
	var lecteur io.Reader
	if corps != nil {
		b, err := json.Marshal(corps)
		if err != nil {
			return err
		}
		lecteur = bytes.NewReader(b)
	}
	req, err := http.NewRequest(methode, c.base+"/api/"+c.avecProjet(chemin), lecteur)
	if err != nil {
		return err
	}
	req.Header.Set("X-Athanor", c.csrf)
	if corps != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return err
	}
	if r.StatusCode != http.StatusOK {
		var e struct {
			Cle    string `json:"erreur_key"`
			Erreur string `json:"erreur"`
		}
		_ = json.Unmarshal(b, &e)
		return &erreurAPI{Cle: firstNonEmpty(firstNonEmpty(e.Cle, e.Erreur), http.StatusText(r.StatusCode)), Code: r.StatusCode, Verrou: r.StatusCode == http.StatusUnauthorized}
	}
	if v == nil {
		return nil
	}
	return json.Unmarshal(b, v)
}

func (c *clientTUI) lire(chemin string, v any) error { return c.appel("GET", chemin, nil, v) }

// messageAPI: an answer with a message for the user ({message_key, args}, or {erreur}).
type messageAPI struct {
	Cle  string   `json:"message_key"`
	Args []string `json:"args"`
}
