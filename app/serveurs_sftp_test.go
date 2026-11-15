package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pkg/sftp"
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
	// C1-14: a ticket, then a plain navigation (no header) streams the file once
	c, b, _ = requete("POST", base+"/ticket", `{"chemin":"/notes.txt"}`)
	var tk struct{ Ticket string }
	if json.Unmarshal(b, &tk); c != http.StatusOK || tk.Ticket == "" {
		t.Fatalf("ticket: %d %s", c, b)
	}
	if c, b, _ := requete("GET", "/api/serveurs/"+srv.ID+"/sftp/autre/ticket/"+tk.Ticket, "", "X-Athanor", ""); c != http.StatusGone {
		t.Fatalf("ticket on another terminal: %d %s", c, b) // and it is spent
	}
	c, b, _ = requete("POST", base+"/ticket", `{"chemin":"/notes.txt"}`)
	json.Unmarshal(b, &tk)
	if c, b, _ := requete("GET", base+"/ticket/"+tk.Ticket, "", "X-Athanor", ""); c != http.StatusOK || string(b) != "salut-remplace" {
		t.Fatalf("download by ticket: %d %q", c, b)
	}
	if c, _, _ := requete("GET", base+"/ticket/"+tk.Ticket, "", "X-Athanor", ""); c != http.StatusGone {
		t.Fatalf("ticket used twice: %d", c)
	}
	// C1-15: another refusal in plain words, the server's answer after them
	w := httptest.NewRecorder()
	s.erreurSFTP(w, errors.New("sftp: failure"))
	if !strings.Contains(w.Body.String(), prefixeSFTPEchec) || !strings.Contains(w.Body.String(), "sftp: failure") {
		t.Fatalf("plain refusal: %s", w.Body.String())
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
	if err := cs.Symlink("/dossier/a.txt", "/lien"); err != nil {
		t.Fatal(err)
	}
	if c := envoyer("/lien", "x", true); c != http.StatusConflict { // never turned into a plain file with the link's mode
		t.Fatalf("a link replaced by a file: %d", c)
	}
	if err := cs.Remove("/lien"); err != nil {
		t.Fatal(err)
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
	// the emergency stop and switching off the remote terminal close what is open (critic C1-4)
	ouvrir := func() string {
		t.Helper()
		c, b, _ := requete("POST", "/api/serveurs/"+srv.ID+"/terminal", `{}`)
		var o struct{ Session string }
		if json.Unmarshal(b, &o); c != http.StatusOK || o.Session == "" {
			t.Fatalf("open: %d %s", c, b)
		}
		return o.Session
	}
	appareil, fenetre := ouvrir(), ouvrir()
	terminaux.Lock()
	terminaux.m[appareil].de = "appareil:tel"
	terminaux.Unlock()
	if c, b, _ := requete("POST", "/api/serveurs/terminal-distance", `{"autoriser":false}`); c != http.StatusOK {
		t.Fatalf("remote terminal off: %d %s", c, b)
	}
	if c, _, _ := requete("GET", "/api/serveurs/"+srv.ID+"/sftp/"+appareil, ""); c != http.StatusGone {
		t.Errorf("a device's terminal left open after the remote terminal was switched off: %d", c)
	}
	if c, _, _ := requete("GET", "/api/serveurs/"+srv.ID+"/sftp/"+fenetre, ""); c != http.StatusOK {
		t.Errorf("the window's terminal closed by the remote switch: %d", c)
	}
	if n := fermerTerminaux(func(*sessionTerminal) bool { return true }); n != 1 {
		t.Errorf("terminals closed by the emergency stop's call: %d, want 1", n)
	}
	if c, _, _ := requete("GET", "/api/serveurs/"+srv.ID+"/sftp/"+fenetre, ""); c != http.StatusGone {
		t.Errorf("terminal left open after fermerTerminaux: %d", c)
	}
	// T2-1: a terminal still connecting during a group closing is refused at its registration if it would have closed
	terminaux.Lock()
	vues := len(terminaux.coupures)
	terminaux.Unlock()
	fermerTerminaux(func(t *sessionTerminal) bool { return strings.HasPrefix(t.de, "appareil:") })
	if c, _ := enregistrerTerminal(&sessionTerminal{id: "t21-appareil", de: "appareil:tel"}, vues); c != http.StatusConflict {
		t.Errorf("a device's terminal connecting during the remote cut registered: %d", c)
	}
	if c, _ := enregistrerTerminal(&sessionTerminal{id: "t21-fenetre"}, vues); c != 0 {
		t.Errorf("the window's terminal connecting during the remote cut refused: %d", c)
	}
	terminaux.Lock()
	delete(terminaux.m, "t21-fenetre")
	terminaux.Unlock()
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

// TestRemplacerSFTPSansPosixRename (T2-2): a server without OpenSSH's posix-rename (Dropbear, BusyBox) still gets
// the upload in place of the old file, and a new file too
func TestRemplacerSFTPSansPosixRename(t *testing.T) {
	if err := sftp.SetSFTPExtensions("hardlink@openssh.com", "statvfs@openssh.com"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com")
	})
	cs, cc := net.Pipe()
	srv := sftp.NewRequestServer(cs, sftp.InMemHandler())
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	c, err := sftp.NewClientPipe(cc, cc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
		t.Fatal("posix-rename still offered")
	}
	ecrire := func(chemin, texte string) {
		f, err := c.Create(chemin)
		if err == nil {
			_, err = f.Write([]byte(texte))
			_ = f.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, avant := range []bool{true, false} {
		if avant {
			ecrire("/a", "vieux")
		}
		ecrire("/a.part", "neuf")
		if err := remplacerSFTP(c, "/a.part", "/a"); err != nil {
			t.Fatalf("replace (old file %v): %v", avant, err)
		}
		f, err := c.Open("/a")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if _, err := c.Stat("/a.part"); string(b) != "neuf" || err == nil {
			t.Errorf("old file %v: content %q, part left: %v", avant, b, err == nil)
		}
		_ = c.Remove("/a")
	}
}
