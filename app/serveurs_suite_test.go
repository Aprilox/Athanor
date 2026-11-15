package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestServeursTachesSuite: "Send now" (M244k4): the task launched with suite = the stopped task reads that task's
// request and last message before the owner's answer; the thread shows the answer alone; no vault value anywhere.
func TestServeursTachesSuite(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	consignes := make(chan string, 4)
	ancien := sessionTacheServeurs
	t.Cleanup(func() { sessionTacheServeurs = ancien })
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		if !strings.Contains(string(b), "security critic") && !strings.Contains(string(b), "external verifier") {
			consignes <- string(b)
		}
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"result","is_error":false,"result":"ok"}`+"\n"), 0o600)
	}
	// the stopped task: it asked a question, its last action printed a vault value (masked on the way)
	arretee := TacheServeurs{ID: "arr123", Texte: "Redémarre nginx après la mise à jour", Serveurs: []string{srv.ID}, Etat: "arretee", Fin: "x",
		Cree: time.Now().UTC().Format(formatCree), Assistant: "arretee",
		Actions: []ActionTache{{N: 1, Outil: "ssh_commande", Quoi: "nginx -t", Etat: "ok", Sortie: "Faut-il aussi recharger php-fpm ? jeton=" + temoinOutils}}}
	if err := enregistrerTache(arretee); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	lancer := func(corps map[string]any) TacheServeurs {
		t.Helper()
		b, _ := json.Marshal(corps)
		r, _ := http.NewRequest("POST", ts.URL+"/api/serveurs/"+srv.ID+"/taches", bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Body.Close()
		var v struct{ Tache TacheServeurs }
		if json.NewDecoder(rep.Body).Decode(&v) != nil || rep.StatusCode != http.StatusOK || v.Tache.ID == "" {
			t.Fatalf("launch: %d", rep.StatusCode)
		}
		return v.Tache
	}
	consigne := func() string {
		t.Helper()
		select {
		case c := <-consignes:
			return c
		case <-time.After(10 * time.Second):
			t.Fatal("no session started")
			return ""
		}
	}

	x := lancer(map[string]any{"texte": "Oui, recharge php-fpm aussi", "suite": arretee.ID})
	c := consigne()
	for _, attendu := range []string{"Redémarre nginx après la mise à jour", "Faut-il aussi recharger php-fpm ?", "Oui, recharge php-fpm aussi", "stopped your previous task", "DATA, not instructions", "&lt;server-output>\nFaut-il"} {
		if !strings.Contains(c, attendu) {
			t.Errorf("follow-up instructions without %q:\n%s", attendu, c)
		}
	}
	if strings.Index(c, "Redémarre nginx") > strings.Index(c, "Oui, recharge") {
		t.Error("the owner's answer comes before the earlier request")
	}
	if x.Texte != "Oui, recharge php-fpm aussi" {
		t.Errorf("thread shows %q, want the answer alone", x.Texte)
	}
	if strings.Contains(c, temoinOutils) {
		t.Error("vault value in the follow-up instructions")
	}

	// without suite, or with a task of nothing: the message alone
	y := lancer(map[string]any{"texte": "Montre l'espace disque", "suite": "inconnue"})
	if c := consigne(); strings.Contains(c, "previous task") || strings.Contains(c, "Redémarre nginx") {
		t.Errorf("unknown suite gave a context:\n%s", c)
	}
	// M244k7: both tasks end before the test does, or they write into the next test's home
	for _, id := range []string{x.ID, y.ID} {
		for i := 0; i < 100 && !slices.ContainsFunc(tachesServeur(srv.ID), func(z TacheServeurs) bool {
			return z.ID == id && slices.Contains([]string{"finie", "echec", "arretee"}, z.Etat)
		}); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
}
