package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestServeursAjoutTroisModes (M244e): the password alone (kept in the vault, used at every connection, no
// key made, cutting password logins refused), an imported key with and without its passphrase (missing or
// wrong passphrase, invalid key and a key the server refuses: refused, nothing kept), a generated key, each
// mode readable on the server; the password, the passphrase and the private keys in no response nor audit line.
func TestServeursAjoutTroisModes(t *testing.T) {
	s, appel, reponses := serveursAjoutTest(t)
	motDePasseCoffreTest(t, s)
	mdp, phrase := "temoin-mdp-"+aleatoire(8), "temoin-phrase-"+aleatoire(8) // built at run time
	pemCle := func(protegee bool) (string, ed25519.PublicKey) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		bloc, err := ssh.MarshalPrivateKey(priv, "")
		if protegee {
			bloc, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(phrase))
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(bloc)), pub
	}
	pemA, pubA := pemCle(false)
	pemB, pubB := pemCle(true)
	pemC, _ := pemCle(false) // not let in by the server
	srv := lancerSSHMemoire(t, nil, mdp, func(string, []byte) (string, int) { return "", 0 })
	for _, p := range []ed25519.PublicKey{pubA, pubB} {
		k, _ := ssh.NewPublicKey(p)
		srv.autoriser(k)
	}
	corps := func(nom string, m map[string]any) map[string]any {
		c := map[string]any{"nom": nom, "hote": srv.hote, "port": srv.port, "utilisateur": "athanor", "empreinte": srv.empreinte}
		for k, v := range m {
			c[k] = v
		}
		return c
	}
	entrees := len(s.coffre.Liste())

	// 1) refused entries: nothing kept
	for _, c := range []struct {
		nom  string
		m    map[string]any
		code int
		err  error
	}{
		{"password refused", map[string]any{"mode": "mot_de_passe", "mot_de_passe_serveur": "mauvais"}, http.StatusConflict, errServeurMdpRefuse},
		{"no key pasted", map[string]any{"mode": "importee"}, http.StatusBadRequest, errServeurCleImport},
		{"a public key", map[string]any{"mode": "importee", "cle_importee": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGq athanor"}, http.StatusBadRequest, errServeurCleImport},
		{"passphrase missing", map[string]any{"mode": "importee", "cle_importee": pemB}, http.StatusBadRequest, errServeurPhraseVide},
		{"wrong passphrase", map[string]any{"mode": "importee", "cle_importee": pemB, "passphrase": "mauvaise"}, http.StatusBadRequest, errServeurPhraseFaux},
		{"key refused by the server", map[string]any{"mode": "importee", "cle_importee": pemC}, http.StatusConflict, errServeurCleRefusee},
		{"unknown mode", map[string]any{"mode": "autre"}, http.StatusBadRequest, errServeurMode},
	} {
		if code, v := appel("/api/serveurs/ajouter", corps("X", c.m)); code != c.code || v["erreur"] != c.err.Error() {
			t.Errorf("%s: %d %v", c.nom, code, v)
		}
	}
	if l, _ := lireServeurs(); len(l) != 0 || len(s.coffre.Liste()) != entrees {
		t.Fatalf("a refused addition left something: %d servers, %d vault entries", len(l), len(s.coffre.Liste()))
	}

	ajouter := func(nom, auth string, m map[string]any) ServeurInventaire {
		t.Helper()
		code, v := appel("/api/serveurs/ajouter", corps(nom, m))
		if code != 200 {
			t.Fatalf("%s: %d %v", nom, code, v)
		}
		l, _ := lireServeurs()
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.Nom == nom })
		if i < 0 || l[i].Auth != auth || v["serveur"].(map[string]any)["auth"] != auth {
			t.Fatalf("%s: mode %v in the response, %+v in the inventory", nom, v["serveur"], l)
		}
		if auth != "cle_generee" && l[i].Etat != "ok" {
			t.Errorf("%s: state %q", nom, l[i].Etat)
		}
		return l[i]
	}
	entree := func(x ServeurInventaire) (EntreeCoffre, string) {
		t.Helper()
		i := slices.IndexFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == x.Cle })
		if i < 0 {
			t.Fatalf("%s: no vault entry %q", x.Nom, x.Cle)
		}
		e := s.coffre.Liste()[i]
		if e.Portee != "infra" || e.Source != "serveurs:"+x.ID {
			t.Errorf("%s: vault entry %+v", x.Nom, e)
		}
		v, err := s.coffre.Valeur(e.ID)
		if err != nil {
			t.Fatal(err)
		}
		return e, v
	}
	cleOuvre := func(nom, v string, pub ed25519.PublicKey) {
		t.Helper()
		k, err := ssh.ParsePrivateKey([]byte(v)) // kept without passphrase: every connection opens it alone
		if p, _ := ssh.NewPublicKey(pub); err != nil || string(k.PublicKey().Marshal()) != string(p.Marshal()) {
			t.Errorf("%s: the vault holds another key (%v)", nom, err)
		}
	}

	// 2) the password alone: kept in the vault, no key made nor put, every connection with it, no cut
	for len(srv.commandes) > 0 {
		<-srv.commandes
	}
	m := ajouter("Mdp", "mot_de_passe", map[string]any{"mode": "mot_de_passe", "mot_de_passe_serveur": mdp})
	if e, v := entree(m); e.Type != "mot_de_passe" || v != mdp {
		t.Errorf("password entry: %+v", e)
	}
	if n := len(s.coffre.Liste()); n != entrees+1 {
		t.Errorf("password alone: %d new vault entries, want 1 (no key)", n-entrees)
	}
	for len(srv.commandes) > 0 {
		if c := <-srv.commandes; strings.Contains(c, "authorized_keys") {
			t.Errorf("password alone put a key: %q", c)
		}
	}
	if c, err := s.connecterServeur(context.Background(), m); err != nil {
		t.Errorf("next connection with the password: %v", err)
	} else {
		c.Close()
	}
	if code, v := appel("/api/serveurs/"+m.ID, map[string]any{"couper_mot_de_passe": true}); code != http.StatusConflict || v["erreur"] != errCoupureMotDePasse.Error() {
		t.Errorf("cut on a password server: %d %v", code, v)
	}
	if _, err := s.clePubliqueServeur(m); err != errServeurSansCle {
		t.Errorf("hardening's key of a password server: %v", err)
	}

	// 3) imported keys: without then with a passphrase, kept opened in the vault, then used alone
	a := ajouter("A", "cle_importee", map[string]any{"mode": "importee", "cle_importee": pemA})
	_, va := entree(a)
	cleOuvre("A", va, pubA)
	b := ajouter("B", "cle_importee", map[string]any{"mode": "importee", "cle_importee": "\n" + pemB + "\n", "passphrase": phrase})
	_, vb := entree(b)
	cleOuvre("B", vb, pubB)
	if c, err := s.connecterServeur(context.Background(), b); err != nil {
		t.Errorf("next connection with the imported key: %v", err)
	} else {
		c.Close()
	}

	// 4) a generated key: its mode too
	ajouter("G", "cle_generee", map[string]any{"mode": "generee"})

	// 5) no secret in a response nor an audit line
	var audit strings.Builder
	_ = filepath.WalkDir(dossierJournalAudit(), func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			b, _ := os.ReadFile(p)
			audit.Write(b)
		}
		return nil
	})
	corpsCle := func(p string) string { return strings.Split(p, "\n")[2] } // a line of the key's base64
	for nom, v := range map[string]string{"password": mdp, "passphrase": phrase, "key A": corpsCle(pemA), "key B": corpsCle(pemB), "kept key B": corpsCle(vb)} {
		if strings.Contains(reponses.String(), v) {
			t.Errorf("the %s is in a response", nom)
		}
		if strings.Contains(audit.String(), v) {
			t.Errorf("the %s is in the audit log", nom)
		}
	}
	for _, x := range []string{"(mot_de_passe)", "(importee)", "ajout de serveur refusé"} {
		if !strings.Contains(audit.String(), x) {
			t.Errorf("audit log without %q", x)
		}
	}
}
