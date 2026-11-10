package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// appelCoffre calls a handler directly (garde skipped), from the window or from a remote device.
func appelCoffre(t *testing.T, h http.HandlerFunc, appareil, corps string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/", strings.NewReader(corps))
	if appareil != "" {
		r = r.WithContext(context.WithValue(r.Context(), cleContexte(nomCleAppareil), appareil))
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// lisible: nil when the entry's value reads back as attendu.
func lisible(c *Coffre, id, attendu string) error {
	v, err := c.Valeur(id)
	if err == nil && v != attendu {
		err = errors.New("value changed")
	}
	return err
}

func TestCoffreServeur(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	sys := nouveauFauxSysteme()
	ancien := systemeCoffre
	systemeCoffre = func() enveloppeSysteme { return sys }
	t.Cleanup(func() { systemeCoffre = ancien })
	// the config is written next to the test binary, which would then become Athanor's folder
	_, err := os.Stat(cheminConfig())
	configNeuf := errors.Is(err, os.ErrNotExist)
	if configNeuf {
		t.Cleanup(func() { os.Remove(cheminConfig()) })
	}

	s := nouveauServeur(Config{Kit: filepath.Join(d, "kit")})
	if s.coffre == nil || s.coffreErr != nil {
		t.Fatal("vault not loaded:", s.coffreErr)
	}
	chemin := s.coffre.chemin
	// a config left by an earlier test makes the test binary's folder Athanor's: drop the vault written there
	t.Cleanup(func() { _ = os.Remove(chemin) })
	absent := func(quand string) {
		if _, err := os.Stat(chemin); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("vault file written", quand)
		}
	}
	absent("at start-up")
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"nouveau":"alpha-1"}`, nil); w.Code != 200 {
		t.Fatal("set password:", w.Code, w.Body.String())
	}
	absent("when the password was set")
	if s.coffre.cleInfra == nil {
		t.Fatal("infra key not open after setting the password")
	}

	valeurs := map[string]string{}
	for _, e := range []struct{ nom, famille, typ, val string }{
		{"ANTHROPIC_API_KEY", "secret", "anthropic", "temoin-code-5512"},
		{"SSH_PROD", "secret", "ssh", "temoin-infra-8830"},
		{"REGION", "variable", "autre", "eu-west"},
	} {
		n, err := s.coffre.Ajouter(EntreeCoffre{Nom: e.nom, Famille: e.famille, Type: e.typ}, e.val)
		if err != nil {
			t.Fatal(e.nom, err)
		}
		valeurs[n.ID] = e.val
	}
	idInfra := ""
	for _, e := range s.coffre.Liste() {
		if e.Portee == "infra" {
			idInfra = e.ID
		}
	}
	tout := func(c *Coffre, quand string) {
		for id, v := range valeurs {
			if err := lisible(c, id, v); err != nil {
				t.Fatal(quand, id, err)
			}
		}
	}

	// two changes: keys rewrapped, every entry still readable
	for _, ch := range [][2]string{{"alpha-1", "beta-2"}, {"beta-2", "gamma-3"}} {
		if w := appelCoffre(t, s.apiMotDePasse, "", `{"ancien":"`+ch[0]+`","nouveau":"`+ch[1]+`"}`, nil); w.Code != 200 {
			t.Fatal("change password:", w.Code, w.Body.String())
		}
		tout(s.coffre, "after "+ch[1])
	}
	if w := appelCoffre(t, s.apiMotDePasse, "", `{"ancien":"faux","nouveau":"delta-4"}`, nil); w.Code != 401 {
		t.Fatal("wrong old password accepted:", w.Code)
	}
	hash := s.config.MotDePasseHash
	w := appelCoffre(t, s.apiMotDePasse, "", `{"ancien":"gamma-3","nouveau":""}`, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "delete them first") || s.config.MotDePasseHash != hash {
		t.Fatal("password removed with an infra entry:", w.Code, w.Body.String())
	}
	tout(s.coffre, "after the refused removal")

	// locking closes the infra key only; unlocking (window or device) reopens it
	fermee := func(quand string) {
		t.Helper()
		if err := lisible(s.coffre, idInfra, valeurs[idInfra]); !errors.Is(err, errCoffreVerrouille) {
			t.Fatal("infra key not closed", quand, err)
		}
	}
	ouverte := func(quand string) {
		t.Helper()
		if err := lisible(s.coffre, idInfra, valeurs[idInfra]); err != nil {
			t.Fatal("infra key closed", quand, err)
		}
	}
	appelCoffre(t, s.apiVerrouiller, "", "", nil)
	fermee("after the window locked")
	tout2 := func() {
		for id, v := range valeurs {
			if id != idInfra {
				if err := lisible(s.coffre, id, v); err != nil {
					t.Fatal("code entry closed by the lock:", err)
				}
			}
		}
	}
	tout2()
	if w := appelCoffre(t, s.apiDeverrouiller, "tel", `{"mot_de_passe":"faux"}`, nil); w.Code != 401 {
		t.Fatal("wrong password from the device:", w.Code)
	}
	fermee("after a wrong password")
	appelCoffre(t, s.apiDeverrouiller, "tel", `{"mot_de_passe":"gamma-3"}`, nil)
	ouverte("after the device unlocked")
	appelCoffre(t, s.apiVerrouiller, "tel", "", nil)
	fermee("after the device locked itself")

	w = appelCoffre(t, s.apiDeverrouiller, "", `{"mot_de_passe":"gamma-3"}`, nil)
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "athanor_session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	appelCoffre(t, s.apiDeverrouiller, "tel", `{"mot_de_passe":"gamma-3"}`, nil)
	appelCoffre(t, s.apiVerrouiller, "tel", "", nil)
	ouverte("while the window stays unlocked")
	appelCoffre(t, s.apiVerrouiller, "", "", session)
	fermee("after the computer locked")
	appelCoffre(t, s.apiDeverrouiller, "tel", `{"mot_de_passe":"gamma-3"}`, nil)
	if _, err := s.retirerAppareilDistance("tel", ""); err != nil {
		t.Fatal(err)
	}
	fermee("after the device was removed")
	appelCoffre(t, s.apiDeverrouiller, "tel", `{"mot_de_passe":"gamma-3"}`, nil)
	s.fermerEcouteDistance()
	fermee("after remote access was switched off")

	// restart: code entries reopen through the keystore, infra waits for the password
	cfg := lireConfig()
	if configNeuf {
		os.Remove(cheminConfig())
	}
	s2 := nouveauServeur(cfg)
	if s2.coffre == nil || s2.coffre.chemin != chemin {
		t.Fatal("restarted server reads another vault:", s2.coffreErr)
	}
	for id, v := range valeurs {
		err := lisible(s2.coffre, id, v)
		if id == idInfra && !errors.Is(err, errCoffreVerrouille) {
			t.Fatal("infra entry open after a restart:", err)
		}
		if id != idInfra && err != nil {
			t.Fatal("code entry closed after a restart:", err)
		}
	}
	appelCoffre(t, s2.apiDeverrouiller, "", `{"mot_de_passe":"gamma-3"}`, nil)
	tout(s2.coffre, "after unlocking the restarted server")

	// without infra entries the password can go; code secrets stay readable through the keystore
	if err := s2.coffre.Supprimer(idInfra); err != nil {
		t.Fatal(err)
	}
	delete(valeurs, idInfra)
	if w := appelCoffre(t, s2.apiMotDePasse, "", `{"ancien":"gamma-3","nouveau":""}`, nil); w.Code != 200 || s2.config.MotDePasseHash != "" {
		t.Fatal("password not removed:", w.Code, w.Body.String())
	}
	tout(rouvrir(t, chemin, sys), "after removing the password")
}
