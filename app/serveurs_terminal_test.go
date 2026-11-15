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
				if string(b[:n]) == "top\r" { // a program that keeps writing (C1-8: not activity)
					go func() {
						for {
							if _, err := canal.Write([]byte("top-tick\r\n")); err != nil {
								return
							}
							time.Sleep(20 * time.Millisecond)
						}
					}()
				}
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
	agir(id, map[string]any{"entree": "top\r"})
	l.attendre(t, "top-tick")
	l.attendreFin(t) // output alone does not keep it open (C1-8)
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
	// C1-7: once Athanor has a password, allowing needs it retyped
	sl.config.MotDePasseHash, sl.session = hacherMotDePasse("mdp-test"), "session"
	if c, _ := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": true, "confirme": true, "mot_de_passe": "faux"}); c != http.StatusUnauthorized || sl.shellDistanceAutorise() {
		t.Fatalf("allowed with a wrong password: %d", c)
	}
	if c, _ := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": true, "confirme": true, "mot_de_passe": "mdp-test"}); c != http.StatusOK || !sl.shellDistanceAutorise() {
		t.Fatalf("allow with the password: %d", c)
	}
	if c, _ := appel("/api/serveurs/terminal-distance", map[string]any{"autoriser": false}); c != http.StatusOK {
		t.Fatalf("forbid without the password: %d", c)
	}
	if !strings.Contains(journalAuditTexte(t), `"remote terminal"`) {
		t.Error("setting change not in the audit log")
	}
}

// TestTerminalSSHReconnexion (M244g): a connected terminal whose server drops or stops answering keepalives is
// reconnected with backoff, the state shown (503 to a reader, 200 with the attempt to the page, keystrokes 409),
// the size kept, nothing asked again; a server that never comes back closes it with reconnect_failed.
func TestTerminalSSHReconnexion(t *testing.T) {
	signer, prive := nouvelleCleSSH(t)
	m := lancerSSHMemoire(t, signer.PublicKey(), "", nil)
	s, srv := outilsServeursTest(t, m.hote, m.port, m.empreinte, prive)
	ka, dr, pr := delaiKeepalive, delaiReconnexion, plafondReconnexion
	t.Cleanup(func() { delaiKeepalive, delaiReconnexion, plafondReconnexion = ka, dr, pr })
	delaiKeepalive, delaiReconnexion, plafondReconnexion = 100*time.Millisecond, 20*time.Millisecond, 80*time.Millisecond
	ident := srv.ID
	mux := http.NewServeMux()
	s.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	appel := func(methode, chemin string, corps any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(corps)
		r, _ := http.NewRequest(methode, ts.URL+"/api/serveurs/"+ident+chemin, bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		rep, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	agir := func(id string, corps map[string]any) (int, map[string]any) {
		t.Helper()
		rep := appel("POST", "/terminal/"+id, corps)
		defer rep.Body.Close()
		var v map[string]any
		_ = json.NewDecoder(rep.Body).Decode(&v)
		return rep.StatusCode, v
	}
	lire := func(id string) *lecteurTerminal {
		t.Helper()
		rep := appel("GET", "/terminal/"+id, nil)
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
	attendreEtat := func(id string, ok func(int, map[string]any) bool, quoi string) map[string]any {
		t.Helper()
		for range 300 {
			if c, v := agir(id, map[string]any{}); ok(c, v) {
				return v
			}
			time.Sleep(20 * time.Millisecond)
		}
		c, v := agir(id, map[string]any{})
		t.Fatalf("%s never seen: %d %v", quoi, c, v)
		return nil
	}
	enReconnexion := func(c int, v map[string]any) bool {
		n, _ := v["reconnexion"].(float64)
		return c == http.StatusOK && n >= 1
	}
	connecte := func(c int, v map[string]any) bool {
		return c == http.StatusOK && v["ok"] == true && v["reconnexion"] == nil
	}
	vider := func() {
		for len(m.tailles) > 0 {
			<-m.tailles
		}
	}

	ouvrir := func() string {
		t.Helper()
		rep := appel("POST", "/terminal", map[string]any{"cols": 100, "lignes": 30})
		var o map[string]any
		_ = json.NewDecoder(rep.Body).Decode(&o)
		rep.Body.Close()
		id, _ := o["session"].(string)
		if id == "" {
			t.Fatalf("open: %d %v", rep.StatusCode, o)
		}
		return id
	}
	finie := func(fin string) func(int, map[string]any) bool {
		return func(c int, v map[string]any) bool {
			return c == http.StatusGone && v["fin"] == fin && v["tentatives"] == float64(tentativesReconnexion)
		}
	}

	id := ouvrir()
	l := lire(id)
	l.attendre(t, "$ ")
	agir(id, map[string]any{"entree": "avant-coupure"})
	l.attendre(t, "avant-coupure")

	// cut, then back up: the reader leaves, the page sees the attempt, the size changed meanwhile is the new PTY's
	m.couper(true)
	l.attendreFin(t)
	v := attendreEtat(id, enReconnexion, "reconnecting")
	if v["tentatives"] != float64(tentativesReconnexion) {
		t.Fatalf("attempts: %v", v)
	}
	if r := appel("GET", "/terminal/"+id, nil); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("reader while reconnecting: %d, want 503", r.StatusCode)
	} else {
		r.Body.Close()
	}
	if c, _ := agir(id, map[string]any{"entree": "perdu"}); c != http.StatusConflict {
		t.Fatalf("keystrokes while reconnecting: %d, want 409", c)
	}
	agir(id, map[string]any{"cols": 90, "lignes": 20})
	vider()
	m.couper(false)
	attendreEtat(id, connecte, "reconnected")
	select {
	case x := <-m.tailles:
		if x != "pty 90x20" {
			t.Fatalf("new PTY %q, want pty 90x20", x)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no new PTY")
	}
	l = lire(id)
	l.attendre(t, "avant-coupure") // the screen so far, replayed
	agir(id, map[string]any{"entree": "apres-reprise"})
	l.attendre(t, "apres-reprise")

	// a server that stops answering keepalives (no FIN): the connection is closed and reconnected
	m.mu.Lock()
	m.gele = true
	m.mu.Unlock()
	l.attendreFin(t)
	attendreEtat(id, enReconnexion, "reconnecting after silent keepalives")
	m.mu.Lock()
	m.gele = false
	m.mu.Unlock()
	attendreEtat(id, connecte, "reconnected after keepalives")

	// never back: given up after tentativesReconnexion attempts, with a reason the page explains
	l = lire(id)
	m.couper(true)
	l.attendreFin(t)
	attendreEtat(id, finie(finTerminalReconnexion), "reconnect_failed")
	m.couper(false)

	journal := journalAuditTexte(t)
	for i := 0; i < 50 && !strings.Contains(journal, finTerminalReconnexion+" after"); i++ { // written just after the 410
		time.Sleep(20 * time.Millisecond)
		journal = journalAuditTexte(t)
	}
	for _, x := range []string{`"terminal.lost"`, `"terminal.reconnect"`, "ok (attempt", finTerminalReconnexion + " after"} {
		if !strings.Contains(journal, x) {
			t.Errorf("audit log without %q", x)
		}
	}
	if strings.Contains(journal, "avant-coupure") || strings.Contains(journal, strings.Split(string(prive), "\n")[1]) {
		t.Error("typed text or the key in the audit log")
	}
	// a link that keeps falling: after reconnexionsMax reconnections within fenetreReconnexions, the next loss gives up
	id = ouvrir()
	for n := range reconnexionsMax {
		l = lire(id)
		l.attendre(t, "$ ")
		m.couper(true)
		l.attendreFin(t)
		m.couper(false)
		attendreEtat(id, connecte, fmt.Sprintf("reconnection %d", n+1))
	}
	l = lire(id)
	l.attendre(t, "$ ")
	m.mu.Lock()
	avant := m.connexions
	m.mu.Unlock()
	m.couper(true)
	l.attendreFin(t)
	m.couper(false)
	attendreEtat(id, finie(finTerminalInstable), "unstable")
	m.mu.Lock()
	if n := m.connexions - avant; n != 0 {
		t.Errorf("unstable link: %d new connections, want none", n)
	}
	m.mu.Unlock()

	// a refusal no attempt can fix: one attempt, then its own end; an authentication refusal gets one more try after
	// the cap (M244k7: a rebooting server refuses logins for a few seconds), two stay far below fail2ban's 5
	essai := func(mem *serveurSSHMemoire, fin string, voulu int, casser func()) {
		t.Helper()
		id := ouvrir()
		l := lire(id)
		l.attendre(t, "$ ")
		casser()
		mem.mu.Lock()
		avant := mem.connexions
		mem.mu.Unlock()
		mem.couper(true)
		l.attendreFin(t)
		mem.couper(false)
		attendreEtat(id, finie(fin), fin)
		time.Sleep(300 * time.Millisecond) // several backoffs: a retry would show here
		mem.mu.Lock()
		defer mem.mu.Unlock()
		if n := mem.connexions - avant; n != voulu {
			t.Errorf("%s: %d attempts, want %d", fin, n, voulu)
		}
		mem.refuse = false
	}
	refuser := func(mem *serveurSSHMemoire) func() {
		return func() { mem.mu.Lock(); mem.refuse = true; mem.mu.Unlock() }
	}
	// the key taken out of authorized_keys
	essai(m, finTerminalCleRefusee, 2, refuser(m))

	// the password changed on the server (fail2ban bans at 5 failures)
	m2 := lancerSSHMemoire(t, nil, "mdp-du-serveur", nil)
	mdp, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Mot de passe VPS 2", Famille: "secret", Type: "mot_de_passe", Portee: "infra"}, "mdp-du-serveur")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := ServeurInventaire{ID: "cd34", Nom: "VPS 2", Hote: m2.hote, Port: m2.port, Utilisateur: "athanor", Auth: "mot_de_passe", Cle: mdp.ID, EmpreinteHote: m2.empreinte, Etat: "ok"}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { return append(l, srv2), nil }); err != nil {
		t.Fatal(err)
	}
	ident = srv2.ID
	essai(m2, finTerminalMdpRefuse, 2, refuser(m2))

	// the host's fingerprint no longer the confirmed one (someone in the middle)
	essai(m2, finTerminalEmpreinte, 1, func() {
		_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
			for i := range l {
				if l[i].ID == srv2.ID {
					l[i].EmpreinteHote = m.empreinte
				}
			}
			return l, nil
		})
	})
}
