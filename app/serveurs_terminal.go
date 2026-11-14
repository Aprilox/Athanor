package main

// Interactive SSH terminal of the Servers module: Athanor's Go opens the SSH session and its PTY with the
// vault's key and the confirmed host fingerprint; the browser only exchanges bytes through three local routes
// guarded like every API route (X-Athanor header, access cookie, session) plus a same-origin check. Keys and
// passwords never leave the Go. The audit log notes who connected to which server, when, for how long and why
// it closed, never what was typed nor shown. From a paired device these routes are refused unless the owner
// allowed it on this computer (DistanceConfig.Terminal, gestionDistance).

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	entreeTerminalMax   = 64 << 10 // one input message (a paste included)
	sessionsTerminalMax = 8        // open terminals at once, every server together
)

var (
	delaiInactiviteTerminal = 15 * time.Minute // no keystroke and no output: closed (tests shorten it)
	delaiLecteurTerminal    = time.Minute      // nobody reads its output (page closed): closed
)

const ecranTerminalMax = 64 << 10

// Why a terminal closed (the interface translates them).
const (
	finTerminalDeconnecte = "disconnected" // Disconnect, or the owner closed it
	finTerminalTermine    = "ended"        // the shell ended (exit, logout, connection lost)
	finTerminalInactif    = "idle"         // delaiInactiviteTerminal reached
	finTerminalAbandonne  = "abandoned"    // no reader for delaiLecteurTerminal
)

type sessionTerminal struct {
	s           *Serveur
	id, serveur string
	de          string
	debut       time.Time
	client      *ssh.Client
	sess        *ssh.Session
	stdin       io.WriteCloser
	sortie      chan []byte // output chunks waiting for the reader
	fini        chan struct{}
	activite    atomic.Int64 // unix nano of the last keystroke or output
	mu          sync.Mutex
	lecteur     bool      // a GET reads the output now
	vuLecteur   time.Time // when the last reader left (or the opening)
	ecran       []byte    // the last ecranTerminalMax bytes sent, replayed to the next reader (a reloaded page)
	muSFTP      sync.Mutex
	sftp        *sftp.Client // the files panel's channel on the same connection (serveurs_sftp.go), closed with it
}

var terminaux = struct {
	sync.Mutex
	m    map[string]*sessionTerminal
	fins map[string]string // closed session -> why, for the interface's last question (bounded)
}{m: map[string]*sessionTerminal{}, fins: map[string]string{}}

func (t *sessionTerminal) toucher() { t.activite.Store(time.Now().UnixNano()) }

// Write: the shell's output (stdout and stderr, merged by the PTY) to the reader; blocks while the reader is
// behind, which slows the SSH channel instead of growing memory.
func (t *sessionTerminal) Write(p []byte) (int, error) {
	select {
	case t.sortie <- bytes.Clone(p):
		t.toucher()
		return len(p), nil
	case <-t.fini:
		return 0, io.ErrClosedPipe
	}
}

// afficher: text shown on the owner's screen, never sent to the shell (the assistant's live mode, M231f); dropped
// after 2 s when nobody reads, so that the assistant never waits on a closed page.
func (t *sessionTerminal) afficher(p []byte) {
	select {
	case t.sortie <- p:
		t.toucher()
	case <-t.fini:
	case <-time.After(2 * time.Second):
	}
}

// fermer: once; the SSH session and client closed, the reason kept a moment, the audit line written.
func (t *sessionTerminal) fermer(raison string) {
	terminaux.Lock()
	if _, ok := terminaux.m[t.id]; !ok {
		terminaux.Unlock()
		return
	}
	delete(terminaux.m, t.id)
	if len(terminaux.fins) >= 64 {
		clear(terminaux.fins)
	}
	terminaux.fins[t.id] = raison
	terminaux.Unlock()
	close(t.fini)
	t.sess.Close()
	t.client.Close()
	t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.close",
		Resultat: fmt.Sprintf("%s after %s", raison, time.Since(t.debut).Round(time.Second))})
}

// surveiller: closes the terminal when idle or abandoned.
func (t *sessionTerminal) surveiller() {
	tic := time.NewTicker(max(min(delaiInactiviteTerminal, delaiLecteurTerminal)/4, 10*time.Millisecond))
	defer tic.Stop()
	for {
		select {
		case <-t.fini:
			return
		case <-tic.C:
			t.mu.Lock()
			abandonne := !t.lecteur && time.Since(t.vuLecteur) > delaiLecteurTerminal
			t.mu.Unlock()
			switch {
			case abandonne:
				t.fermer(finTerminalAbandonne)
			case time.Since(time.Unix(0, t.activite.Load())) > delaiInactiviteTerminal:
				t.fermer(finTerminalInactif)
			}
		}
	}
}

// memeOrigine: a browser request names its page's origin; it must be this host (the tunnel's or the local one).
func memeOrigine(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // not sent by a browser page (fetch on GET from the same origin may omit it)
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// tailleTerminal: columns and rows the browser measured, kept in sane bounds (0: the default).
func tailleTerminal(cols, lignes int) (int, int, bool) {
	if cols == 0 && lignes == 0 {
		return 80, 24, true
	}
	return cols, lignes, cols >= 2 && cols <= 1000 && lignes >= 2 && lignes <= 500
}

// apiTerminalOuvrir: POST /api/serveurs/{id}/terminal {cols, lignes}: a shell in a PTY, its session id.
func (s *Serveur) apiTerminalOuvrir(w http.ResponseWriter, r *http.Request) {
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	var c struct {
		Cols   int `json:"cols"`
		Lignes int `json:"lignes"`
	}
	if r.ContentLength != 0 && lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	cols, lignes, ok := tailleTerminal(c.Cols, c.Lignes)
	if !ok {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == r.PathValue("id") })
	if i < 0 || !idServeurValide.MatchString(liste[i].ID) {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	srv := liste[i]
	terminaux.Lock()
	plein := len(terminaux.m) >= sessionsTerminalMax
	terminaux.Unlock()
	if plein {
		s.erreur(w, http.StatusTooManyRequests, "Too many terminals are open: close one first")
		return
	}
	de := deQui(r)
	client, err := s.connecterServeur(r.Context(), srv)
	if err != nil {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "terminal.open", Resultat: "error: " + err.Error()})
		s.erreur(w, statutAjout(err), err.Error())
		return
	}
	t := &sessionTerminal{s: s, id: aleatoire(16), serveur: srv.ID, de: de, debut: time.Now(), client: client,
		sortie: make(chan []byte, 256), fini: make(chan struct{}), vuLecteur: time.Now()}
	t.toucher()
	if err := t.demarrer(cols, lignes); err != nil {
		client.Close()
		s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "terminal.open", Resultat: "error: " + err.Error()})
		s.erreur(w, http.StatusBadGateway, "The server refused to open a terminal")
		return
	}
	terminaux.Lock()
	terminaux.m[t.id] = t
	terminaux.Unlock()
	go func() { _ = t.sess.Wait(); t.fermer(finTerminalTermine) }()
	go t.surveiller()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "terminal.open", Resultat: "ok"})
	s.ecrireJSON(w, map[string]any{"session": t.id, "serveur": srv.ID, "nom": srv.Nom})
}

// demarrer: the PTY (xterm-256color, the browser's size) and the login shell.
func (t *sessionTerminal) demarrer(cols, lignes int) error {
	var err error
	if t.sess, err = t.client.NewSession(); err != nil {
		return err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if err = t.sess.RequestPty("xterm-256color", lignes, cols, modes); err != nil {
		return err
	}
	if t.stdin, err = t.sess.StdinPipe(); err != nil {
		return err
	}
	t.sess.Stdout, t.sess.Stderr = t, t
	return t.sess.Shell()
}

// terminalDe: the open session named by the path, on the server named by the path.
func terminalDe(r *http.Request) (*sessionTerminal, string) {
	terminaux.Lock()
	defer terminaux.Unlock()
	t := terminaux.m[r.PathValue("session")]
	if t == nil || t.serveur != r.PathValue("id") {
		return nil, terminaux.fins[r.PathValue("session")]
	}
	return t, ""
}

func (s *Serveur) terminalFerme(w http.ResponseWriter, raison string) {
	if raison == "" {
		raison = finTerminalTermine
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	_, _ = fmt.Fprintf(w, `{"erreur_key":"This terminal is closed","erreur":"This terminal is closed","fin":%q}`+"\n", raison)
}

// apiTerminalFlux: GET /api/serveurs/{id}/terminal/{session}: the shell's output as a byte stream, one reader
// at a time (a reloaded page reads on where the last one stopped); ends when the terminal closes.
func (s *Serveur) apiTerminalFlux(w http.ResponseWriter, r *http.Request) {
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	t, raison := terminalDe(r)
	if t == nil {
		s.terminalFerme(w, raison)
		return
	}
	t.mu.Lock()
	occupe := t.lecteur
	t.lecteur = true
	t.mu.Unlock()
	if occupe {
		s.erreur(w, http.StatusConflict, "This terminal is already open in another window")
		return
	}
	defer func() {
		t.mu.Lock()
		t.lecteur, t.vuLecteur = false, time.Now()
		t.mu.Unlock()
	}()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	ecrire := func(b []byte) bool {
		if _, err := w.Write(b); err != nil {
			return false
		}
		t.mu.Lock()
		t.ecran = append(t.ecran, b...)
		if n := len(t.ecran) - ecranTerminalMax; n > 0 { // shortcut: cut after a line end, a full-screen program redraws on the size sent next
			coupe := t.ecran[n:]
			if i := bytes.IndexByte(coupe, '\n'); i >= 0 {
				coupe = coupe[i+1:]
			}
			t.ecran = bytes.Clone(coupe)
		}
		t.mu.Unlock()
		return true
	}
	envoyer := func(b []byte) bool {
		if !ecrire(b) {
			return false
		}
		for len(t.sortie) > 0 { // whatever else is ready, in the same flush
			if !ecrire(<-t.sortie) {
				return false
			}
		}
		return rc.Flush() == nil
	}
	t.mu.Lock()
	deja := t.ecran
	t.mu.Unlock()
	if len(deja) > 0 { // the screen so far, for a page that comes back
		if _, err := w.Write(deja); err != nil || rc.Flush() != nil {
			return
		}
	}
	for {
		select {
		case b := <-t.sortie:
			if !envoyer(b) {
				return
			}
		case <-t.fini:
			for len(t.sortie) > 0 { // the last words (logout…)
				if !envoyer(<-t.sortie) {
					return
				}
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}

// apiTerminalAgir: POST /api/serveurs/{id}/terminal/{session} {entree, cols, lignes, fermer}: keystrokes,
// a new size, or Disconnect.
func (s *Serveur) apiTerminalAgir(w http.ResponseWriter, r *http.Request) {
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	var c struct {
		Entree string `json:"entree"`
		Cols   int    `json:"cols"`
		Lignes int    `json:"lignes"`
		Fermer bool   `json:"fermer"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if len(c.Entree) > entreeTerminalMax {
		s.erreur(w, http.StatusRequestEntityTooLarge, "Too much text at once")
		return
	}
	cols, lignes, ok := tailleTerminal(c.Cols, c.Lignes)
	if !ok {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	t, raison := terminalDe(r)
	if t == nil {
		s.terminalFerme(w, raison)
		return
	}
	if c.Fermer {
		t.fermer(finTerminalDeconnecte)
		s.ecrireJSON(w, map[string]any{"fin": finTerminalDeconnecte})
		return
	}
	if c.Cols != 0 || c.Lignes != 0 {
		if err := t.sess.WindowChange(lignes, cols); err != nil {
			s.terminalFerme(w, finTerminalTermine)
			return
		}
	}
	if c.Entree != "" && tacheDirecte(t.id) != nil { // two authors never type at once (M231f)
		s.erreur(w, http.StatusLocked, "The assistant is working in this terminal: stop it or take over first")
		return
	}
	if c.Entree != "" {
		if _, err := io.WriteString(t.stdin, c.Entree); err != nil {
			s.terminalFerme(w, finTerminalTermine)
			return
		}
		t.toucher()
	}
	s.ecrireJSON(w, map[string]any{"ok": true})
}

// routeShellDistance: a route that gives a shell or the server's files (terminal, SFTP): refused from a paired
// device unless the owner allowed it on this computer.
func routeShellDistance(chemin string) bool {
	reste, ok := strings.CutPrefix(chemin, "/api/serveurs/")
	if !ok {
		return false
	}
	p := strings.Split(reste, "/")
	return len(p) >= 2 && p[0] != "" && (p[1] == "terminal" || p[1] == "sftp")
}

func (s *Serveur) shellDistanceAutorise() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.Distance.Terminal
}

// apiTerminalDistance: POST /api/serveurs/terminal-distance {autoriser, confirme}: the terminal and SFTP from a
// paired device. Allowing needs confirme (the interface asks first); refused from a device (not in the allowlist).
func (s *Serveur) apiTerminalDistance(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Autoriser bool `json:"autoriser"`
		Confirme  bool `json:"confirme"`
	}
	if lireCorps(r, &c) != nil || c.Autoriser && !c.Confirme {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	s.mu.Lock()
	s.config.Distance.Terminal = c.Autoriser
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	resultat := "off"
	if c.Autoriser {
		resultat = "on"
	}
	if err != nil {
		resultat += " (config: " + err.Error() + ")"
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "remote terminal", Resultat: resultat})
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"terminal_distance": c.Autoriser})
}
