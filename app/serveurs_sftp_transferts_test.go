package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSFTPTransferts: the transfer queue on the terminal's connection: a file and a whole folder sent then
// fetched back, progress while running, Annuler leaving nothing half written, a name already taken never
// overwritten without an answer (garder les deux, passer for all, remplacer), automatic resumption after a
// network error in both directions from the bytes already written, Athanor's data folder neither sent nor
// written, bad requests refused, the assistant's transfers listed "par l'assistant"; the audit has paths and
// sizes, never the content.
func TestSFTPTransferts(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	m := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, m.hote, m.port, m.empreinte, prive)
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	pause := pauseRepriseTransfert
	pauseRepriseTransfert = 10 * time.Millisecond
	defer func() { pauseRepriseTransfert, fauteTransfert = pause, nil }()
	req := func(methode, chemin, corps string) (int, []byte) {
		t.Helper()
		return requeteSFTPTest(t, s, ts, methode, chemin, corps)
	}
	c, b := req("POST", "/api/serveurs/"+srv.ID+"/terminal", `{}`)
	var o struct{ Session string }
	if json.Unmarshal(b, &o); c != http.StatusOK || o.Session == "" {
		t.Fatalf("open: %d %s", c, b)
	}
	base := "/api/serveurs/" + srv.ID + "/sftp/" + o.Session
	defer req("POST", "/api/serveurs/"+srv.ID+"/terminal/"+o.Session, `{"fermer":true}`)
	if c, b := req("POST", base, `{"dossier":"/","nom":"dest"}`); c != http.StatusOK {
		t.Fatalf("mkdir: %d %s", c, b)
	}

	loc := t.TempDir()
	ecrire := func(p, contenu string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(contenu), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ecrire(filepath.Join(loc, "a.txt"), "témoin-contenu-a")
	ecrire(filepath.Join(loc, "dossier", "b.txt"), "témoin-contenu-b")
	ecrire(filepath.Join(loc, "dossier", "sous", "c.txt"), "témoin-contenu-c")
	gros := bytes.Repeat([]byte("0123456789abcdef"), 3<<16) // 3 MiB: three blocks
	ecrire(filepath.Join(loc, "gros.bin"), string(gros))
	distant := func(p string) string {
		t.Helper()
		c, b := req("GET", base+"/fichier?chemin="+url.QueryEscape(p), "")
		if c != http.StatusOK {
			return ""
		}
		return string(b)
	}
	lancer := func(sens string, sources []string, dossier string) []string {
		t.Helper()
		corps, _ := json.Marshal(map[string]any{"sens": sens, "sources": sources, "dossier": dossier})
		c, b := req("POST", base+"/transferts", string(corps))
		var r struct{ Transferts []string }
		if json.Unmarshal(b, &r); c != http.StatusOK || len(r.Transferts) != len(sources) {
			t.Fatalf("transfer %s %v: %d %s", sens, sources, c, b)
		}
		return r.Transferts
	}
	etats := func() map[string]etatTransfert {
		t.Helper()
		c, b := req("GET", base+"/transferts", "")
		var r struct{ Transferts []etatTransfert }
		if json.Unmarshal(b, &r); c != http.StatusOK {
			t.Fatalf("list: %d %s", c, b)
		}
		m := map[string]etatTransfert{}
		for _, x := range r.Transferts {
			m[x.ID] = x
		}
		return m
	}
	attendre := func(id string, ok func(etatTransfert) bool) etatTransfert {
		t.Helper()
		for fin := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			x := etats()[id]
			if ok(x) {
				return x
			}
			if time.Now().After(fin) {
				t.Fatalf("transfer %s stays %+v", id, x)
			}
		}
	}
	fini := func(id string) etatTransfert {
		t.Helper()
		return attendre(id, func(x etatTransfert) bool { return !actif(x.Etat) })
	}
	agir := func(id, corps string) int {
		t.Helper()
		c, _ := req("POST", base+"/transferts/"+id, corps)
		return c
	}

	// a file and a folder sent
	ids := lancer("envoi", []string{filepath.Join(loc, "a.txt"), filepath.Join(loc, "dossier")}, "/dest")
	if x := fini(ids[0]); x.Etat != "termine" || x.Cible != "/dest/a.txt" || x.Fait != x.Total || x.Total != int64(len("témoin-contenu-a")) || x.Par != "vous" {
		t.Errorf("file sent: %+v", x)
	}
	if x := fini(ids[1]); x.Etat != "termine" || !x.Dossier || x.FichiersTotal != 2 || x.Fichiers != 2 || x.Fait != x.Total {
		t.Errorf("folder sent: %+v", x)
	}
	if distant("/dest/dossier/sous/c.txt") != "témoin-contenu-c" || distant("/dest/a.txt") != "témoin-contenu-a" {
		t.Fatal("contents on the server differ")
	}

	// a name already taken: never overwritten without an answer
	ecrire(filepath.Join(loc, "a.txt"), "témoin-nouveau-a")
	id := lancer("envoi", []string{filepath.Join(loc, "a.txt")}, "/dest")[0]
	if x := attendre(id, func(x etatTransfert) bool { return x.Etat == "conflit" }); x.Conflit != "a.txt" {
		t.Errorf("conflict: %+v", x)
	}
	if distant("/dest/a.txt") != "témoin-contenu-a" {
		t.Error("overwritten before the answer")
	}
	if agir(id, `{"action":"retirer"}`) != http.StatusConflict || agir(id, `{"action":"n-importe"}`) != http.StatusConflict {
		t.Error("a running transfer removed or a bad action taken")
	}
	agir(id, `{"action":"garder"}`)
	if x := fini(id); x.Etat != "termine" || x.Cible != "/dest/a (1).txt" || distant("/dest/a (1).txt") != "témoin-nouveau-a" || distant("/dest/a.txt") != "témoin-contenu-a" {
		t.Errorf("keep both: %+v", x)
	}
	ids = lancer("envoi", []string{filepath.Join(loc, "a.txt"), filepath.Join(loc, "dossier")}, "/dest")
	attendre(ids[0], func(x etatTransfert) bool { return x.Etat == "conflit" })
	agir(ids[0], `{"action":"passer","tous":true}`)
	for _, id := range ids {
		if x := fini(id); x.Etat != "passe" {
			t.Errorf("skip for all: %+v", x)
		}
	}
	id = lancer("envoi", []string{filepath.Join(loc, "a.txt")}, "/dest")[0]
	attendre(id, func(x etatTransfert) bool { return x.Etat == "conflit" }) // "for all" ended with its queue
	agir(id, `{"action":"remplacer"}`)
	if x := fini(id); x.Etat != "termine" || distant("/dest/a.txt") != "témoin-nouveau-a" {
		t.Errorf("replace: %+v", x)
	}
	if agir(id, `{"action":"annuler"}`) != http.StatusConflict || agir(id, `{"action":"retirer"}`) != http.StatusOK {
		t.Error("finished transfer: cancel accepted or removal refused")
	}
	if _, ok := etats()[id]; ok {
		t.Error("removed transfer still listed")
	}

	// a network error in the middle: resumed from the bytes already written, in both directions
	var mu sync.Mutex
	coupures := 0
	fauteTransfert = func(tr *transfertSFTP) error {
		mu.Lock()
		defer mu.Unlock()
		transferts.Lock()
		fait := tr.Fait
		transferts.Unlock()
		if fait >= 1<<20 && coupures < 1 {
			coupures++
			return errors.New("connection reset by peer")
		}
		return nil
	}
	id = lancer("envoi", []string{filepath.Join(loc, "gros.bin")}, "/dest")[0]
	if x := fini(id); x.Etat != "termine" || x.Reprises != 1 || x.Fait != int64(len(gros)) || distant("/dest/gros.bin") != string(gros) {
		t.Errorf("upload resumed: %+v", x)
	}
	if distant("/dest/gros.bin.athanor-part") != "" {
		t.Error("partial file left on the server")
	}
	dl := filepath.Join(loc, "dl")
	_ = os.Mkdir(dl, 0o755)
	mu.Lock()
	coupures = 0
	mu.Unlock()
	id = lancer("telechargement", []string{"/dest/gros.bin", "/dest/dossier"}, dl)[0]
	if x := fini(id); x.Etat != "termine" || x.Reprises != 1 {
		t.Errorf("download resumed: %+v", x)
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "gros.bin")); !bytes.Equal(b, gros) {
		t.Errorf("downloaded file differs: %d bytes", len(b))
	}
	for fin := time.Now().Add(5 * time.Second); time.Now().Before(fin); time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(filepath.Join(dl, "dossier", "sous", "c.txt")); string(b) == "témoin-contenu-c" {
			break
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "dossier", "sous", "c.txt")); string(b) != "témoin-contenu-c" {
		t.Errorf("folder downloaded: %q", b)
	}

	// progress while running, then Annuler: nothing half written
	entre, libere := make(chan struct{}), make(chan struct{})
	var une sync.Once
	fauteTransfert = func(*transfertSFTP) error {
		une.Do(func() { close(entre); <-libere })
		return nil
	}
	id = lancer("envoi", []string{filepath.Join(loc, "gros.bin")}, "/")[0]
	<-entre
	if x := etats()[id]; x.Etat != "en_cours" || x.Total != int64(len(gros)) || x.FichiersTotal != 1 || x.Cible != "/gros.bin" {
		t.Errorf("progress: %+v", x)
	}
	if agir(id, `{"action":"annuler"}`) != http.StatusOK {
		t.Error("cancel refused")
	}
	close(libere)
	if x := fini(id); x.Etat != "annule" {
		t.Errorf("cancelled: %+v", x)
	}
	for fin := time.Now().Add(5 * time.Second); distant("/gros.bin.athanor-part") != "" && time.Now().Before(fin); {
		time.Sleep(10 * time.Millisecond)
	}
	if distant("/gros.bin.athanor-part") != "" || distant("/gros.bin") != "" {
		t.Error("a cancelled upload left a file")
	}
	fauteTransfert = nil

	// refused: Athanor's data folder, bad paths and directions, a destination that is not a folder
	donnees := dossierAthanor()
	_ = os.MkdirAll(donnees, 0o700)
	for _, x := range []struct {
		corps string
		code  int
	}{
		{`{"sens":"envoi","sources":[` + jsonTexte(donnees) + `],"dossier":"/dest"}`, http.StatusForbidden},
		{`{"sens":"envoi","sources":[` + jsonTexte(filepath.Dir(donnees)) + `],"dossier":"/dest"}`, http.StatusForbidden},
		{`{"sens":"telechargement","sources":["/dest/a.txt"],"dossier":` + jsonTexte(donnees) + `}`, http.StatusForbidden},
		{`{"sens":"envoi","sources":["relatif/a.txt"],"dossier":"/dest"}`, http.StatusBadRequest},
		{`{"sens":"envoi","sources":[` + jsonTexte(filepath.Join(loc, "a.txt")) + `],"dossier":"/dest/a.txt"}`, http.StatusBadRequest},
		{`{"sens":"telechargement","sources":["/"],"dossier":` + jsonTexte(dl) + `}`, http.StatusBadRequest},
		{`{"sens":"telechargement","sources":["/dest/../../etc"],"dossier":"relatif"}`, http.StatusBadRequest},
		{`{"sens":"ailleurs","sources":["/dest/a.txt"],"dossier":` + jsonTexte(dl) + `}`, http.StatusBadRequest},
		{`{"sens":"envoi","sources":[],"dossier":"/dest"}`, http.StatusBadRequest},
		{`{"sens":"telechargement","sources":["/dest/absent"],"dossier":` + jsonTexte(dl) + `}`, http.StatusNotFound},
	} {
		if c, b := req("POST", base+"/transferts", x.corps); c != x.code {
			t.Errorf("%s: %d %s, want %d", x.corps, c, b, x.code)
		}
	}
	if c, _ := req("GET", "/api/serveurs/"+srv.ID+"/sftp/inconnue/transferts", ""); c != http.StatusGone {
		t.Errorf("unknown session: %d", c)
	}

	// the assistant's transfer, through its own tool, in the same list (other tests may have left theirs)
	avant := etats()
	if _, err := s.appelerOutilServeur(context.Background(), "agent", srv.ID, "sftp_recuperer", []byte(`{"distant":"/dest/a.txt","local":"retour/a.txt"}`), Sauvegarde{}); err != nil {
		t.Fatal(err)
	}
	nouveau := ""
	for k, x := range etats() {
		if _, vu := avant[k]; !vu {
			nouveau = k
			if x.Par != "assistant" || x.Etat != "termine" || x.Sens != "telechargement" || x.Nom != "a.txt" || x.Fait != int64(len("témoin-nouveau-a")) {
				t.Errorf("assistant's transfer: %+v", x)
			}
		}
	}
	if nouveau == "" {
		t.Error("the assistant's transfer is not listed")
	} else if agir(nouveau, `{"action":"reprendre"}`) != http.StatusConflict || agir(nouveau, `{"action":"annuler"}`) != http.StatusConflict {
		t.Error("the owner resumed or cancelled the assistant's transfer")
	}

	journal := journalAuditTexte(t)
	for _, x := range []string{"sftp.upload", "sftp.download", "/dest/a (1).txt", "termine", "annule", "passe"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	for _, x := range []string{"témoin-contenu", "témoin-nouveau", "0123456789abcdef", string(prive)} {
		if strings.Contains(journal, x) {
			t.Errorf("audit log holds %q", x)
		}
	}
}
