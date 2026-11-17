package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestMessageMoteur (B79): the English text unchanged, nested messages kept through JSON, errors.Is through
// the first error, a text the engine did not word is its own key, secrets masked in every argument.
func TestMessageMoteur(t *testing.T) {
	base := errors.New("dial tcp: refused")
	e := errMoteur("{0}: {1}; nothing was switched", "web-1", errMoteur("the commit has no folder \"{0}\" to publish", "absent"))
	if e.Error() != `web-1: the commit has no folder "absent" to publish; nothing was switched` {
		t.Errorf("text: %q", e.Error())
	}
	if w := errMoteur("sending the archive: {0}", base); !errors.Is(w, base) || messageDe(w).Args[0].(*MessageMoteur).Cle != "dial tcp: refused" {
		t.Errorf("cause or plain error: %#v", w)
	}
	if m := messageDe(fmt.Errorf("deploy: %w", e)); m.Cle != "deploy: "+e.Error() { // wrapped by another text: its own key, never a wrong part
		t.Errorf("wrapped: %#v", m)
	}
	if m := (&MessageMoteur{Cle: "{0} {1}", Args: []any{"{1}", "x"}}); m.Error() != "{1} x" {
		t.Errorf("one pass: %q", m.Error())
	}
	b, _ := json.Marshal(suite(messageDe(e), errMoteur("went back automatically to {0}", "v2")))
	var lu MessageMoteur
	if err := json.Unmarshal(b, &lu); err != nil || lu.Error() != e.Error()+"; went back automatically to v2" || lu.Args[0].(*MessageMoteur).Args[1].(*MessageMoteur).Cle != "the commit has no folder \"{0}\" to publish" {
		t.Errorf("JSON: %v %s", err, b)
	}
	cache := lu.masquer(func(s string) string { return strings.ReplaceAll(s, "absent", "•••") })
	if strings.Contains(cache.Error(), "absent") || !strings.Contains(lu.Error(), "absent") {
		t.Errorf("masked: %q (original %q)", cache.Error(), lu.Error())
	}
	v := messageVerification(&VerifDeploiement{Problemes: []string{"injoignable", "tls"}, HTTP: ResultatHTTP{Erreur: "timeout"}})
	if v.Error() != "the external verification failed: the site does not answer, the certificate is not valid: timeout" {
		t.Errorf("verification: %q", v.Error())
	}
}

// clesMoteur: every template the engine words (errMoteur, Cle literals, the check's problems) and the fixed lines
// its scripts print, so that TestWebTraduction asks i18n.js for each in the five languages.
func clesMoteur(t *testing.T) []string {
	fichiers, _ := filepath.Glob("deploiements*.go")
	re := regexp.MustCompile(`(?:errMoteur\(|Cle: )("(?:[^"\\]|\\.)*")`)
	vide := regexp.MustCompile(`^[{}\d:;, ]*$`) // "{0}: {1}", "{0}; {1}": nothing to translate
	var cles []string
	for _, f := range fichiers {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if c, err := strconv.Unquote(m[1]); err == nil && !vide.MatchString(c) {
				cles = append(cles, c)
			}
		}
	}
	for _, c := range problemesVerif {
		cles = append(cles, c)
	}
	return append(cles, "the external verification failed: {0}", "the external verification failed: {0}: {1}",
		"Caddy is not installed on this server ("+caddyfileServeur+" missing): install it first", "the archive changed on the way: its SHA-256 differs on the server",
		"the archive could not be extracted", "archive refused: a link of the release points out of it once extracted", "the site file cannot be written",
		"the vault is not available", "the build command needs Git for Windows' bash on this PC", errK3sUnServeur.Error(),
		"the git source fetches from GitHub: the project's origin is not a GitHub repository (use the artefact source)", "Harden this server: {0}")
}
