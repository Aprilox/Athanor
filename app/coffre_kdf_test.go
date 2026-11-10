package main

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMotDePasseFormat(t *testing.T) {
	debut := time.Now()
	h := hacherMotDePasse("témoin-mdp")
	if d := time.Since(debut); d > time.Second {
		t.Fatalf("derivation too slow: %v", d)
	}
	if !strings.HasPrefix(h, "pbkdf2-sha256$600000$") || len(strings.Split(h, "$")) != 4 || strings.Contains(h, "témoin") {
		t.Fatalf("format: %s", h)
	}
	if h == hacherMotDePasse("témoin-mdp") {
		t.Fatal("same salt twice")
	}
	if ok, rempl := verifierMotDePasse("", "témoin-mdp", h); !ok || rempl {
		t.Fatalf("right password: %v %v", ok, rempl)
	}
	for _, faux := range []string{"", "témoin-mdP", "témoin-mdp "} {
		if ok, _ := verifierMotDePasse("", faux, h); ok {
			t.Fatalf("wrong password accepted: %q", faux)
		}
	}
	for _, abime := range []string{"", "pbkdf2-sha256$", "pbkdf2-sha256$x$00$00", "pbkdf2-sha256$0$00$00", "pbkdf2-sha256$99999999999$00$00", h + "$x"} {
		if ok, _ := verifierMotDePasse("", "témoin-mdp", abime); ok {
			t.Fatalf("malformed hash accepted: %q", abime)
		}
	}
	faible := hacherMotDePasseIter("témoin-mdp", 1000)
	if ok, rempl := verifierMotDePasse("", "témoin-mdp", faible); !ok || !rempl {
		t.Fatalf("weaker parameters: %v %v", ok, rempl)
	}
}

// hacherMotDePasseIter: a hash with other parameters (as a future default change would leave).
func hacherMotDePasseIter(mdp string, iter int) string {
	sel := []byte("0123456789abcdef")
	return "pbkdf2-sha256$" + strconv.Itoa(iter) + "$" + hex.EncodeToString(sel) + "$" + hex.EncodeToString(deriverMdp(mdp, sel, iter))
}

func TestMotDePasseAncienRemplace(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.config.Sel = "ab12cd34"
	s.config.MotDePasseHash = ancienHashMdp("ab12cd34", "témoin-mdp")
	ancien := s.config.MotDePasseHash
	deverrouiller := func(mdp string) int {
		w := httptest.NewRecorder()
		s.apiDeverrouiller(w, httptest.NewRequest("POST", "/api/deverrouiller", strings.NewReader(`{"mot_de_passe":"`+mdp+`"}`)))
		return w.Code
	}
	if c := deverrouiller("faux"); c != http.StatusUnauthorized || s.config.MotDePasseHash != ancien {
		t.Fatalf("wrong password: %d, hash changed: %v", c, s.config.MotDePasseHash != ancien)
	}
	if c := deverrouiller("témoin-mdp"); c != http.StatusOK {
		t.Fatalf("old format refused: %d", c)
	}
	if !strings.HasPrefix(s.config.MotDePasseHash, "pbkdf2-sha256$") || s.config.Sel != "" {
		t.Fatalf("not upgraded: %q sel %q", s.config.MotDePasseHash, s.config.Sel)
	}
	if relu := lireConfig(); relu.MotDePasseHash != s.config.MotDePasseHash {
		t.Fatal("upgraded hash not saved")
	}
	if c := deverrouiller("témoin-mdp"); c != http.StatusOK {
		t.Fatalf("new format refused: %d", c)
	}
	if c := deverrouiller("faux"); c != http.StatusUnauthorized {
		t.Fatalf("wrong password after upgrade: %d", c)
	}

	// changing it: the old one is checked, the new one stored in the new format
	changer := func(ancien, nouveau string) int {
		w := httptest.NewRecorder()
		s.apiMotDePasse(w, httptest.NewRequest("POST", "/api/mot-de-passe", strings.NewReader(`{"ancien":"`+ancien+`","nouveau":"`+nouveau+`"}`)))
		return w.Code
	}
	if c := changer("faux", "autre-mdp"); c != http.StatusUnauthorized {
		t.Fatalf("change with wrong password: %d", c)
	}
	if c := changer("témoin-mdp", "autre-mdp"); c != http.StatusOK {
		t.Fatalf("change: %d", c)
	}
	if ok, _ := verifierMotDePasse(s.config.Sel, "autre-mdp", s.config.MotDePasseHash); !ok || !strings.HasPrefix(s.config.MotDePasseHash, "pbkdf2-sha256$") {
		t.Fatal("new password not stored in the new format")
	}
}
