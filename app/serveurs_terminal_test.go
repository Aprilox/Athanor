package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// terminal: the in-memory server's PTY side: the asked sizes go to s.tailles, the shell echoes what it is
// sent, answers Ctrl+C with "^C" and ends on "exit".
func (s *serveurSSHMemoire) terminal(canal ssh.Channel, r *ssh.Request) bool {
	taille := func(p []byte) string {
		if len(p) < 8 {
			return "?"
		}
		return fmt.Sprintf("%dx%d", binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:]))
	}
	noter := func(x string) {
		select {
		case s.tailles <- x:
		default:
		}
	}
	switch r.Type {
	case "pty-req":
		n := int(binary.BigEndian.Uint32(r.Payload))
		noter("pty " + taille(r.Payload[4+n:]))
	case "window-change":
		noter("resize " + taille(r.Payload))
	case "shell":
		go func() {
			invite := "$ "
			if s.invite != "" {
				invite = s.invite
			}
			canal.Write([]byte(invite))
			var tout []byte
			b := make([]byte, 4096)
			for {
				n, err := canal.Read(b)
				if err != nil {
					return
				}
				tout = append(tout, b[:n]...)
				if bytes.Contains(b[:n], []byte{3}) {
					canal.Write([]byte("^C\r\n$ "))
					continue
				}
				if string(b[:n]) == "\r" { // an Enter on an empty line: a shell prints its prompt again
					canal.Write([]byte("\r\n$ "))
					continue
				}
				canal.Write(b[:n])
				if bytes.Contains(tout, []byte("exit\r")) {
					canal.Write([]byte("\r\nlogout\r\n"))
					canal.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, 0))
					canal.Close()
					return
				}
			}
		}()
	default:
		return false
	}
	r.Reply(true, nil)
	return true
}

// lecteurTerminal: the output stream read in the background, as the page reads it.
type lecteurTerminal struct {
	mu    sync.Mutex
	b     bytes.Buffer
	fini  chan struct{}
	corps io.Closer
}

func (l *lecteurTerminal) attendre(t *testing.T, texte string) {
	t.Helper()
	for range 300 {
		l.mu.Lock()
		ok := strings.Contains(l.b.String(), texte)
		l.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t.Fatalf("output %q never showed %q", l.b.String(), texte)
}

func (l *lecteurTerminal) attendreFin(t *testing.T) {
	t.Helper()
	select {
	case <-l.fini:
	case <-time.After(5 * time.Second):
		t.Fatal("the output stream did not end")
	}
}

// TestTerminalSSH: a shell in a PTY opened by the Go against the in-memory server: size, keystrokes, Ctrl+C,
// resize, one reader at a time, origin and header checks, exit, Disconnect, idle and abandoned terminals,
// changed fingerprint; the audit log names each connection without what was typed, nor the key.
func TestTerminalSSH(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	m := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, m.hote, m.port, m.empreinte, prive)
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	var reponses strings.Builder
	requete := func(methode, chemin, corps string, entetes ...string) (*http.Response, map[string]any) {
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
		if methode == "GET" && rep.StatusCode == http.StatusOK {
			return rep, nil
		}
		b, _ := io.ReadAll(rep.Body)
		rep.Body.Close()
		reponses.Write(b)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		return rep, v
	}
	ouvrir := func(corps string) string {
		t.Helper()
		rep, v := requete("POST", "/api/serveurs/"+srv.ID+"/terminal", corps)
		id, _ := v["session"].(string)
		if rep.StatusCode != http.StatusOK || id == "" {
			t.Fatalf("open: %d %v", rep.StatusCode, v)
		}
		return id
	}
	lire := func(id string) *lecteurTerminal {
		t.Helper()
		rep, _ := requete("GET", "/api/serveurs/"+srv.ID+"/terminal/"+id, "")
		if rep.StatusCode != http.StatusOK {
			t.Fatalf("stream: %d", rep.StatusCode)
		}
		l := &lecteurTerminal{fini: make(chan struct{}), corps: rep.Body}
		go func() {
			defer close(l.fini)
			defer rep.Body.Close()
			b := make([]byte, 4096)
			for {
				n, err := rep.Body.Read(b)
				l.mu.Lock()
				l.b.Write(b[:n])
				l.mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		return l
	}
	agir := func(id string, corps map[string]any, entetes ...string) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(corps)
		rep, v := requete("POST", "/api/serveurs/"+srv.ID+"/terminal/"+id, string(b), entetes...)
		return rep.StatusCode, v
	}
	taille := func(attendu string) {
		t.Helper()
		select {
		case x := <-m.tailles:
			if x != attendu {
				t.Fatalf("size %q, want %q", x, attendu)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("size %q never asked", attendu)
		}
	}

	id := ouvrir(`{"cols":100,"lignes":30}`)
	taille("pty 100x30")
	l := lire(id)
	l.attendre(t, "$ ")
	if c, _ := agir(id, map[string]any{"entree": "echo bonjour-tape"}); c != http.StatusOK {
		t.Fatalf("input: %d", c)
	}
	l.attendre(t, "echo bonjour-tape")
	if c, _ := agir(id, map[string]any{"cols": 120, "lignes": 40}); c != http.StatusOK {
		t.Fatalf("resize: %d", c)
	}
	taille("resize 120x40")
	agir(id, map[string]any{"entree": "\x03"})
	l.attendre(t, "^C")
	if rep, _ := requete("GET", "/api/serveurs/"+srv.ID+"/terminal/"+id, ""); rep.StatusCode != http.StatusConflict {
		t.Fatalf("second reader: %d, want 409", rep.StatusCode)
	}
	// a reloaded page: the next reader gets the screen so far first
	l.corps.Close()
	l.attendreFin(t)
	for i := 0; ; i++ { // the server notices the reader left
		terminaux.Lock()
		st := terminaux.m[id]
		terminaux.Unlock()
		st.mu.Lock()
		parti := !st.lecteur
		st.mu.Unlock()
		if parti {
			break
		}
		if i == 50 {
			t.Fatal("the first reader never left")
		}
		time.Sleep(40 * time.Millisecond)
	}
	l = lire(id)
	l.attendre(t, "echo bonjour-tape")
	if c, _ := agir(id, map[string]any{"entree": "x"}, "Origin", "https://evil.example"); c != http.StatusForbidden {
		t.Fatalf("other origin: %d, want 403", c)
	}
	if c, _ := agir(id, map[string]any{"entree": "x"}, "X-Athanor", "faux"); c != http.StatusForbidden {
		t.Fatalf("wrong header: %d, want 403", c)
	}
	if c, _ := agir(id, map[string]any{"cols": 5000, "lignes": 40}); c != http.StatusBadRequest {
		t.Fatalf("absurd size: %d, want 400", c)
	}
	if c, _ := agir(id, map[string]any{"entree": strings.Repeat("a", entreeTerminalMax+1)}); c != http.StatusRequestEntityTooLarge {
		t.Fatalf("huge input: %d, want 413", c)
	}
	if c, _ := agir("autre", map[string]any{"entree": "x"}); c != http.StatusGone {
		t.Fatalf("unknown session: %d, want 410", c)
	}
	agir(id, map[string]any{"entree": "exit\r"})
	l.attendreFin(t)
	if !strings.Contains(l.b.String(), "logout") {
		t.Fatalf("last words lost: %q", l.b.String())
	}
	if c, v := agir(id, map[string]any{"entree": "x"}); c != http.StatusGone || v["fin"] != finTerminalTermine {
		t.Fatalf("after exit: %d %v", c, v)
	}

	// Disconnect
	id = ouvrir("")
	taille("pty 80x24")
	l = lire(id)
	if c, v := agir(id, map[string]any{"fermer": true}); c != http.StatusOK || v["fin"] != finTerminalDeconnecte {
		t.Fatalf("disconnect: %d %v", c, v)
	}
	l.attendreFin(t)

	// idle, then abandoned (no reader)
	inact, lect := delaiInactiviteTerminal, delaiLecteurTerminal
	t.Cleanup(func() { delaiInactiviteTerminal, delaiLecteurTerminal = inact, lect })
	delaiInactiviteTerminal = 300 * time.Millisecond
	id = ouvrir("")
	l = lire(id)
	l.attendreFin(t)
	if c, v := agir(id, map[string]any{}); c != http.StatusGone || v["fin"] != finTerminalInactif {
		t.Fatalf("idle: %d %v", c, v)
	}
	delaiInactiviteTerminal, delaiLecteurTerminal = time.Minute, 200*time.Millisecond
	id = ouvrir("")
	time.Sleep(600 * time.Millisecond)
	if c, v := agir(id, map[string]any{}); c != http.StatusGone || v["fin"] != finTerminalAbandonne {
		t.Fatalf("abandoned: %d %v", c, v)
	}

	// a changed host fingerprint: refused, in the owner's words
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		l[0].EmpreinteHote = "SHA256:autre"
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
	if rep, v := requete("POST", "/api/serveurs/"+srv.ID+"/terminal", ""); rep.StatusCode != http.StatusConflict || v["erreur"] != errEmpreinteChangee.Error() {
		t.Fatalf("changed fingerprint: %d %v", rep.StatusCode, v)
	}
	if rep, _ := requete("POST", "/api/serveurs/inconnu/terminal", ""); rep.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown server: %d", rep.StatusCode)
	}

	journal := journalAuditTexte(t)
	for _, x := range []string{`"terminal.open"`, `"terminal.close"`, finTerminalTermine + " after", finTerminalDeconnecte + " after", finTerminalInactif + " after", finTerminalAbandonne + " after"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	cle := strings.Split(strings.TrimSpace(string(prive)), "\n")[1] // a line of the private key
	for _, x := range []string{"bonjour-tape", cle, temoinOutils} {
		if strings.Contains(journal, x) || strings.Contains(reponses.String(), x) {
			t.Errorf("%q in the audit log or an API answer", x)
		}
	}
	terminaux.Lock()
	ouverts := len(terminaux.m)
	terminaux.Unlock()
	if ouverts != 0 {
		t.Errorf("%d terminals left open", ouverts)
	}
}

// TestTerminalDistance: the terminal (and SFTP) routes are refused from a paired device until the owner allows
// them on this computer, with a confirmation; the setting itself is never changed from a device.
func TestTerminalDistance(t *testing.T) {
	s, h, _ := serveurDistance(t)
	cle := appairerTest(t, s, h)
	refuse := func(methode, chemin, corps string) bool {
		w := appelDistance(h, methode, chemin, cle, corps, "X-Athanor", "jeton")
		return w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "Not available from a remote device")
	}
	routes := [][2]string{{"POST", "/api/serveurs/ab12/terminal"}, {"GET", "/api/serveurs/ab12/terminal/x"}, {"POST", "/api/serveurs/ab12/terminal/x"}, {"GET", "/api/serveurs/ab12/sftp/x"}, {"POST", "/api/serveurs/ab12/sftp/x"}, {"POST", "/api/serveurs/ab12/sftp/x/fichier"}, {"GET", "/api/serveurs/ab12/sftp/x/fichier"}}
	for _, r := range routes {
		if !refuse(r[0], r[1], `{}`) {
			t.Errorf("%s %s reached from a device by default", r[0], r[1])
		}
	}
	s.mu.Lock()
	s.config.Distance.Terminal = true
	s.mu.Unlock()
	for _, r := range routes {
		if refuse(r[0], r[1], `{}`) {
			t.Errorf("%s %s refused once allowed", r[0], r[1])
		}
	}
	for _, c := range []string{"/api/serveurs/terminal-distance", "/api/serveurs/ab12/taches/x/terminal", "/api/serveurs//terminal"} {
		if !refuse("POST", c, `{"autoriser":false}`) {
			t.Errorf("POST %s reached from a device", c)
		}
	}

	// on this computer: allowing needs the confirmation, the choice is kept
	sl, appel, _ := serveursAjoutTest(t)
	if c, _ := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": true}); c != http.StatusBadRequest {
		t.Fatalf("allowed without confirmation: %d", c)
	}
	if c, v := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": true, "confirme": true}); c != http.StatusOK || v["terminal_distance"] != true || !sl.shellDistanceAutorise() {
		t.Fatalf("allow: %d %v", c, v)
	}
	if c, _ := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": false}); c != http.StatusOK || sl.shellDistanceAutorise() {
		t.Fatalf("forbid again: %d", c)
	}
	if !strings.Contains(journalAuditTexte(t), `"remote terminal"`) {
		t.Error("setting change not in the audit log")
	}
}
