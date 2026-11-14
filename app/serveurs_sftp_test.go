package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestTerminalSFTP: the files panel on the terminal's connection against the in-memory server: list, upload
// (refused over an existing file unless asked, then replaced), download, rename, delete a whole folder, bad
// paths, origin, a closed terminal; the audit log names each change by its path, never the content.
func TestTerminalSFTP(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	m := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, m.hote, m.port, m.empreinte, prive)
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	requete := func(methode, chemin, corps string, entetes ...string) (int, []byte, http.Header) {
		t.Helper()
		r, _ := http.NewRequest(methode, ts.URL+chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		for i := 0; i+1 < len(entetes); i += 2 {
			r.Header.Set(entetes[i], entetes[i+1])
		}
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer rep.Body.Close()
		b, _ := io.ReadAll(rep.Body)
		return rep.StatusCode, b, rep.Header
	}
	c, b, _ := requete("POST", "/api/serveurs/"+srv.ID+"/terminal", `{}`)
	var o struct{ Session string }
	if json.Unmarshal(b, &o); c != http.StatusOK || o.Session == "" {
		t.Fatalf("open: %d %s", c, b)
	}
	base := "/api/serveurs/" + srv.ID + "/sftp/" + o.Session
	type liste struct {
		Chemin  string
		Entrees []entreeSFTP
	}
	lister := func(chemin string) liste {
		t.Helper()
		c, b, _ := requete("GET", base+"?chemin="+url.QueryEscape(chemin), "")
		var l liste
		if json.Unmarshal(b, &l); c != http.StatusOK {
			t.Fatalf("list %q: %d %s", chemin, c, b)
		}
		return l
	}
	envoyer := func(chemin, corps string, remplacer bool) int {
		t.Helper()
		q := "?chemin=" + url.QueryEscape(chemin)
		if remplacer {
			q += "&remplacer=1"
		}
		c, _, _ := requete("POST", base+"/fichier"+q, corps, "Content-Type", "application/octet-stream")
		return c
	}
	agir := func(corps string) (int, string) {
		t.Helper()
		c, b, _ := requete("POST", base, corps)
		return c, string(b)
	}

	if l := lister(""); l.Chemin != "/" || len(l.Entrees) != 0 {
		t.Fatalf("home: %+v", l)
	}
	if c := envoyer("/notes.txt", "bonjour-envoye", false); c != http.StatusOK {
		t.Fatalf("upload: %d", c)
	}
	if c := envoyer("/notes.txt", "autre", false); c != http.StatusConflict {
		t.Fatalf("upload over an existing file without asking: %d", c)
	}
	if c := envoyer("/notes.txt", "salut-remplace", true); c != http.StatusOK {
		t.Fatalf("replace: %d", c)
	}
	c, b, hd := requete("GET", base+"/fichier?chemin=/notes.txt", "")
	if c != http.StatusOK || string(b) != "salut-remplace" || !strings.Contains(hd.Get("Content-Disposition"), "notes.txt") {
		t.Fatalf("download: %d %q %v", c, b, hd)
	}
	terminaux.Lock()
	tr := terminaux.m[o.Session]
	terminaux.Unlock()
	cs, err := tr.clientSFTP()
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.Mkdir("/dossier"); err != nil {
		t.Fatal(err)
	}
	if c := envoyer("/dossier/a.txt", "a", false); c != http.StatusOK {
		t.Fatalf("upload in a folder: %d", c)
	}
	if l := lister("/"); len(l.Entrees) != 2 || l.Entrees[0].Nom != "dossier" || !l.Entrees[0].Dossier || l.Entrees[1].Nom != "notes.txt" || l.Entrees[1].Taille != 14 {
		t.Fatalf("listing: %+v", l)
	}
	if c := envoyer("/dossier", "x", true); c != http.StatusConflict {
		t.Fatalf("a folder replaced by a file: %d", c)
	}
	for _, cas := range []struct {
		corps string
		code  int
	}{
		{`{"renommer":"/notes.txt","nom":"a/b"}`, http.StatusBadRequest},
		{`{"renommer":"/notes.txt","nom":".."}`, http.StatusBadRequest},
		{`{"renommer":"/notes.txt","nom":"dossier"}`, http.StatusConflict},
		{`{"renommer":"/absent","nom":"x"}`, http.StatusNotFound},
		{`{"renommer":"notes.txt","nom":"x"}`, http.StatusBadRequest},
		{`{"supprimer":"/"}`, http.StatusBadRequest},
		{`{"supprimer":"/../"}`, http.StatusBadRequest},
		{`{}`, http.StatusBadRequest},
		{`{"renommer":"/notes.txt","nom":"lu.txt"}`, http.StatusOK},
		{`{"supprimer":"/dossier"}`, http.StatusOK},
	} {
		if c, b := agir(cas.corps); c != cas.code {
			t.Errorf("%s: %d %s, want %d", cas.corps, c, b, cas.code)
		}
	}
	if l := lister("/"); len(l.Entrees) != 1 || l.Entrees[0].Nom != "lu.txt" {
		t.Fatalf("after rename and delete: %+v", l)
	}
	if c, _, _ := requete("GET", base+"/fichier?chemin=/notes.txt", ""); c != http.StatusNotFound {
		t.Errorf("download of a missing file: %d", c)
	}
	if c, _, _ := requete("GET", base+"?chemin=relatif", ""); c != http.StatusBadRequest {
		t.Errorf("relative path: %d", c)
	}
	if c, _, _ := requete("GET", base, "", "Origin", "http://ailleurs.example"); c != http.StatusForbidden {
		t.Errorf("other origin: %d", c)
	}
	if c, _, _ := requete("GET", "/api/serveurs/"+srv.ID+"/sftp/inconnue", ""); c != http.StatusGone {
		t.Errorf("unknown session: %d", c)
	}
	if c, _, _ := requete("POST", "/api/serveurs/"+srv.ID+"/terminal/"+o.Session, `{"fermer":true}`); c != http.StatusOK {
		t.Fatalf("disconnect: %d", c)
	}
	if c, _, _ := requete("GET", base, ""); c != http.StatusGone {
		t.Errorf("files of a closed terminal: %d", c)
	}
	journal := journalAuditTexte(t)
	for _, x := range []string{"sftp.upload", "/notes.txt (14 bytes)", "sftp.download", "sftp.rename", "/notes.txt -\\u003e /lu.txt", "sftp.delete"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	for _, x := range []string{"bonjour-envoye", "salut-remplace", string(prive)} {
		if strings.Contains(journal, x) {
			t.Errorf("audit log holds %q", x)
		}
	}
}
