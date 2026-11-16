package main

// Interactive SSH terminal of the Servers module: Athanor's Go opens the SSH session and its PTY with the
// vault's key and the confirmed host fingerprint; the browser only exchanges bytes through three local routes
// guarded like every API route (X-Athanor header, access cookie, session) plus a same-origin check. Keys and
// passwords never leave the Go. The audit log notes who connected to which server, when, for how long and why
// it closed, never what was typed nor shown. From a paired device these routes are refused unless the owner
// allowed it on this computer (DistanceConfig.Terminal, gestionDistance).

import (
	"bytes"
	"context"
	"errors"
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
	entreeTerminalMax     = 64 << 10 // one input message (a paste included)
	sessionsTerminalMax   = 8        // open terminals at once, every server together
	tentativesReconnexion = 10       // a lost connection: attempts before giving up (about 3 min: a reboot gets through)
	reconnexionsMax       = 3        // reconnections within fenetreReconnexions; one more loss gives up (a link that keeps falling)
)

var (
	delaiInactiviteTerminal = 15 * time.Minute // no keystroke, file work, assistant line or page opened: closed (tests shorten it)
	delaiLecteurTerminal    = time.Minute      // nobody reads its output (page closed): closed
	delaiKeepalive          = 15 * time.Second // a keepalive this often; unanswered this long: the connection is lost
	delaiReconnexion        = time.Second      // first wait before reconnecting, doubled each attempt (1, 2, 4, 8, 16, 30… s)
	plafondReconnexion      = 30 * time.Second
	fenetreReconnexions     = 5 * time.Minute
)

const ecranTerminalMax = 64 << 10

// Why a terminal closed (the interface translates them).
const (
	finTerminalDeconnecte  = "disconnected"     // Disconnect, or the owner closed it
	finTerminalTermine     = "ended"            // the shell ended (exit, logout, connection lost)
	finTerminalInactif     = "idle"             // delaiInactiviteTerminal reached
	finTerminalAbandonne   = "abandoned"        // no reader for delaiLecteurTerminal
	finTerminalReconnexion = "reconnect_failed" // the connection was lost and tentativesReconnexion attempts failed
	finTerminalInstable    = "unstable"         // lost again after reconnexionsMax reconnections within fenetreReconnexions
	// the connection was lost and the server answers, but no attempt can succeed: given up at once
	finTerminalMdpRefuse  = "password_refused"    // the password changed (retrying would trip fail2ban)
	finTerminalCleRefusee = "key_refused"         // the key was taken out of authorized_keys, or the vault's key is unusable
	finTerminalEmpreinte  = "fingerprint_changed" // possibly someone in the middle
	finTerminalRetire     = "server_removed"      // the server left the inventory
)

// finDefinitive: the end reason for an error that no new attempt can fix ("" when a new attempt may succeed).
func finDefinitive(err error) string {
	switch {
	case errors.Is(err, errServeurMdpRefuse):
		return finTerminalMdpRefuse
	case errors.Is(err, errServeurCleRefusee), errors.Is(err, errServeurPhrase), errors.Is(err, errServeurPasCle):
		return finTerminalCleRefusee
	case errors.Is(err, errEmpreinteChangee):
		return finTerminalEmpreinte
	case errors.Is(err, errServeurInconnu):
		return finTerminalRetire
	}
	return ""
}

const errTerminalReconnexion = "The connection to the server was lost: Athanor is reconnecting"

type sessionTerminal struct {
	s            *Serveur
	id, serveur  string
	de           string
	debut        time.Time
	client       *ssh.Client
	sess         *ssh.Session
	stdin        io.WriteCloser
	sortie       chan []byte // output chunks waiting for the reader
	fini         chan struct{}
	activite     atomic.Int64 // unix nano of the last keystroke, file work, assistant line or page opened (not output)
	mu           sync.Mutex
	cols, lignes int           // the PTY's size, for a reconnection
	tentative    int           // 0: connected; n: reconnecting, attempt n
	perdue       chan struct{} // closed when the connection is lost: the reader leaves, the page follows the reconnection
	lecteur      bool          // a GET reads the output now
	vuLecteur    time.Time     // when the last reader left (or the opening)
	ecran        []byte        // the last ecranTerminalMax bytes sent, replayed to the next reader (a reloaded page)
	muSFTP       sync.Mutex
	sftp         *sftp.Client // the files panel's channel on the same connection (serveurs_sftp.go), closed with it
	reprises     []time.Time  // when the last reconnections succeeded (reconnexionsMax within fenetreReconnexions)
	// the delays read once at the opening (a test changing them never races a terminal still running)
	inactivite, lecteurMax, keepalive, reconnexion, plafond time.Duration
}

var terminaux = struct {
	sync.Mutex
	m    map[string]*sessionTerminal
	fins map[string]string // closed session -> why, for the interface's last question (bounded)
	// every group closing (emergency stop, remote terminal off): a terminal still connecting meanwhile checks them
	coupures []func(*sessionTerminal) bool
}{m: map[string]*sessionTerminal{}, fins: map[string]string{}}

func (t *sessionTerminal) toucher() { t.activite.Store(time.Now().UnixNano()) }

// Write: the shell's output (stdout and stderr, merged by the PTY) to the reader; blocks while the reader is
// behind, which slows the SSH channel instead of growing memory.
func (t *sessionTerminal) Write(p []byte) (int, error) {
	select {
	case t.sortie <- bytes.Clone(p): // C1-8: output is not activity (htop or tail -f would keep it open forever)
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
	t.mu.Lock()
	sess, client := t.sess, t.client
	t.mu.Unlock()
	sess.Close()
	client.Close()
	t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.close",
		Resultat: fmt.Sprintf("%s after %s", raison, time.Since(t.debut).Round(time.Second))})
}

// surveiller: closes the terminal when idle or abandoned.
func (t *sessionTerminal) surveiller() {
	tic := time.NewTicker(max(min(t.inactivite, t.lecteurMax)/4, 10*time.Millisecond))
	defer tic.Stop()
	for {
		select {
		case <-t.fini:
			return
		case <-tic.C:
			t.mu.Lock()
			abandonne := !t.lecteur && time.Since(t.vuLecteur) > t.lecteurMax
			t.mu.Unlock()
			switch {
			case abandonne:
				t.fermer(finTerminalAbandonne)
			case time.Since(time.Unix(0, t.activite.Load())) > t.inactivite:
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
	plein, vues := len(terminaux.m) >= sessionsTerminalMax, len(terminaux.coupures)
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
		sortie: make(chan []byte, 256), fini: make(chan struct{}), vuLecteur: time.Now(), cols: cols, lignes: lignes, perdue: make(chan struct{}),
		inactivite: delaiInactiviteTerminal, lecteurMax: delaiLecteurTerminal, keepalive: delaiKeepalive, reconnexion: delaiReconnexion, plafond: plafondReconnexion}
	t.toucher()
	if t.sess, t.stdin, err = t.demarrer(client, cols, lignes); err != nil {
		client.Close()
		s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "terminal.open", Resultat: "error: " + err.Error()})
		s.erreur(w, http.StatusBadGateway, "The server refused to open a terminal")
		return
	}
	if statut, msg := enregistrerTerminal(t, vues); statut != 0 {
		t.sess.Close()
		client.Close()
		s.erreur(w, statut, msg)
		return
	}
	go t.vivre()
	go t.garderEnVie(client)
	go t.surveiller()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "terminal.open", Resultat: "ok"})
	s.ecrireJSON(w, map[string]any{"session": t.id, "serveur": srv.ID, "nom": srv.Nom})
}

// demarrer: the PTY (xterm-256color, the browser's size) and the login shell on client.
func (t *sessionTerminal) demarrer(client *ssh.Client, cols, lignes int) (*ssh.Session, io.WriteCloser, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	err = sess.RequestPty("xterm-256color", lignes, cols, modes)
	var stdin io.WriteCloser
	if err == nil {
		stdin, err = sess.StdinPipe()
	}
	if err == nil {
		sess.Stdout, sess.Stderr = t, t
		err = sess.Shell()
	}
	if err != nil {
		sess.Close()
		return nil, nil, err
	}
	return sess, stdin, nil
}

func (t *sessionTerminal) ferme() bool {
	select {
	case <-t.fini:
		return true
	default:
		return false
	}
}

// vivre: waits for the shell; a shell that ended (exit, logout, a signal) closes the terminal, a connection
// lost (no exit status: the server stopped answering, the network fell) is reconnected.
func (t *sessionTerminal) vivre() {
	for {
		t.mu.Lock()
		sess := t.sess
		t.mu.Unlock()
		err := sess.Wait()
		var fin *ssh.ExitError
		if err == nil || errors.As(err, &fin) || t.ferme() {
			t.fermer(finTerminalTermine)
			return
		}
		if !t.reconnecter() {
			return
		}
	}
}

// garderEnVie: a keepalive every t.keepalive; a server that does not answer within t.keepalive (network
// gone without a word) gets its connection closed, which vivre sees as lost.
func (t *sessionTerminal) garderEnVie(c *ssh.Client) {
	for {
		select {
		case <-t.fini:
			return
		case <-time.After(t.keepalive):
		}
		rep := make(chan error, 1)
		go func() { _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); rep <- err }()
		select {
		case err := <-rep:
			if err != nil {
				return // already closed: vivre knows
			}
		case <-time.After(t.keepalive):
			c.Close()
			return
		case <-t.fini:
			return
		}
	}
}

// reconnecter: up to tentativesReconnexion attempts, waiting 1, 2, 4, 8, 16, then 30 s, with the vault's secrets
// as at the opening (nothing asked again); a refusal no attempt can fix (password, key, fingerprint, server
// removed) or a link lost again after reconnexionsMax reconnections within fenetreReconnexions gives up at once.
// True when a new shell runs, false when the terminal closed (given up, or closed meanwhile).
func (t *sessionTerminal) reconnecter() bool {
	t.mu.Lock()
	t.reprises = slices.DeleteFunc(t.reprises, func(x time.Time) bool { return time.Since(x) > fenetreReconnexions })
	if len(t.reprises) >= reconnexionsMax {
		t.mu.Unlock()
		t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.lost",
			Resultat: fmt.Sprintf("given up: lost again after %d reconnections in %s", reconnexionsMax, fenetreReconnexions)})
		t.fermer(finTerminalInstable)
		return false
	}
	t.tentative = 1
	close(t.perdue)
	t.mu.Unlock()
	t.muSFTP.Lock() // the files panel's channel died with the connection
	if t.sftp != nil {
		t.sftp.Close()
		t.sftp = nil
	}
	t.muSFTP.Unlock()
	t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.lost", Resultat: "reconnecting"})
	attente, refus := t.reconnexion, false
	var err error
	for n := 1; n <= tentativesReconnexion; n++ {
		t.mu.Lock()
		t.tentative = n
		t.mu.Unlock()
		select {
		case <-t.fini:
			return false
		case <-time.After(attente):
		}
		attente = min(attente*2, t.plafond)
		if err = t.rebrancher(); err == nil {
			if t.ferme() {
				return false
			}
			t.mu.Lock()
			t.reprises = append(t.reprises, time.Now())
			t.mu.Unlock()
			t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.reconnect", Resultat: fmt.Sprintf("ok (attempt %d)", n)})
			return true
		}
		if fin := finDefinitive(err); fin != "" {
			// M244k7: a rebooting server may refuse the login for a few seconds (/run/nologin): one more try after the cap
			if !refus && (errors.Is(err, errServeurMdpRefuse) || errors.Is(err, errServeurCleRefusee)) {
				refus, attente = true, t.plafond
				continue
			}
			t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.reconnect", Resultat: fmt.Sprintf("given up (attempt %d): %s", n, err)})
			t.fermer(fin)
			return false
		}
	}
	t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: "terminal.reconnect", Resultat: "error: " + err.Error()})
	t.fermer(finTerminalReconnexion)
	return false
}

// rebrancher: one attempt: the server read again from the inventory, a new connection and shell swapped in.
func (t *sessionTerminal) rebrancher() error {
	liste, err := lireServeurs()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == t.serveur })
	if i < 0 {
		return errServeurInconnu
	}
	ctx, annuler := context.WithCancel(context.Background())
	defer annuler()
	go func() {
		select {
		case <-t.fini:
			annuler()
		case <-ctx.Done():
		}
	}()
	client, err := t.s.connecterServeur(ctx, liste[i])
	if err != nil {
		return err
	}
	t.mu.Lock()
	cols, lignes := t.cols, t.lignes
	t.mu.Unlock()
	sess, stdin, err := t.demarrer(client, cols, lignes)
	if err != nil {
		client.Close()
		return err
	}
	t.mu.Lock()
	if t.ferme() { // closed meanwhile: fermer may have read the old client already
		t.mu.Unlock()
		sess.Close()
		client.Close()
		return nil
	}
	if n := len(t.ecran); n > 0 && t.ecran[n-1] != '\n' { // M275: the new shell's output starts on its own line, never after the old prompt
		t.ecran = append(t.ecran, '\r', '\n')
	}
	vieux := t.client
	t.client, t.sess, t.stdin = client, sess, stdin
	t.tentative, t.perdue = 0, make(chan struct{})
	t.mu.Unlock()
	vieux.Close()
	go t.garderEnVie(client)
	return nil
}

// reconnexion: the answer for a terminal reconnecting (the page shows the attempt, then reads again).
func (s *Serveur) reconnexion(w http.ResponseWriter, statut, n int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statut)
	_, _ = fmt.Fprintf(w, `{"ok":true,"reconnexion":%d,"tentatives":%d}`+"\n", n, tentativesReconnexion)
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

// raisonFin: why this closed terminal ended.
func (t *sessionTerminal) raisonFin() string {
	terminaux.Lock()
	defer terminaux.Unlock()
	return terminaux.fins[t.id]
}

func (s *Serveur) terminalFerme(w http.ResponseWriter, raison string) {
	if raison == "" {
		raison = finTerminalTermine
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	_, _ = fmt.Fprintf(w, `{"erreur_key":"This terminal is closed","erreur":"This terminal is closed","fin":%q,"tentatives":%d}`+"\n", raison, tentativesReconnexion)
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
	occupe, n, perdue := t.lecteur, t.tentative, t.perdue
	t.lecteur = t.lecteur || n == 0
	t.mu.Unlock()
	if n > 0 {
		s.reconnexion(w, http.StatusServiceUnavailable, n)
		return
	}
	if !occupe {
		t.toucher() // the page (re)opened counts as the owner being there
	}
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
		case <-perdue: // the connection is lost: the page asks the state and follows the reconnection
			for len(t.sortie) > 0 {
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
	t.mu.Lock()
	if c.Cols != 0 || c.Lignes != 0 {
		t.cols, t.lignes = cols, lignes
	}
	n, sess, stdin := t.tentative, t.sess, t.stdin
	t.mu.Unlock()
	if n > 0 {
		if c.Entree != "" {
			s.erreur(w, http.StatusConflict, errTerminalReconnexion)
			return
		}
		s.reconnexion(w, http.StatusOK, n) // the new shell gets the size kept
		return
	}
	if c.Cols != 0 || c.Lignes != 0 {
		if err := sess.WindowChange(lignes, cols); err != nil && t.ferme() {
			s.terminalFerme(w, t.raisonFin())
			return
		}
	}
	if c.Entree != "" && tacheDirecte(t.id) != nil { // two authors never type at once (M231f)
		s.erreur(w, http.StatusLocked, "The assistant is working in this terminal: stop it or take over first")
		return
	}
	if c.Entree != "" {
		if _, err := io.WriteString(stdin, c.Entree); err != nil {
			if t.ferme() {
				s.terminalFerme(w, t.raisonFin())
			} else { // the connection fell, vivre is about to notice
				s.erreur(w, http.StatusConflict, errTerminalReconnexion)
			}
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

// fermerTerminaux: closes the open terminals (and their files panel) for which fermer is true; how many.
// enregistrerTerminal: the session joins the open ones, unless a group closing since its opening began (vues =
// len(coupures) then) would have closed it, or the limit was reached meanwhile; 0 when registered
func enregistrerTerminal(t *sessionTerminal, vues int) (int, string) {
	terminaux.Lock()
	defer terminaux.Unlock()
	if slices.ContainsFunc(terminaux.coupures[vues:], func(f func(*sessionTerminal) bool) bool { return f(t) }) {
		return http.StatusConflict, "Terminals were just closed by an emergency stop or by switching off the remote terminal: open it again if you need it"
	}
	if len(terminaux.m) >= sessionsTerminalMax { // checked again: openings at the same time all passed the first check
		return http.StatusTooManyRequests, "Too many terminals are open: close one first"
	}
	terminaux.m[t.id] = t
	return 0, ""
}

func fermerTerminaux(fermer func(*sessionTerminal) bool) int {
	terminaux.Lock()
	terminaux.coupures = append(terminaux.coupures, fermer)
	var l []*sessionTerminal
	for _, t := range terminaux.m {
		if fermer(t) {
			l = append(l, t)
		}
	}
	terminaux.Unlock()
	for _, t := range l {
		t.fermer(finTerminalDeconnecte)
	}
	return len(l)
}

func (s *Serveur) shellDistanceAutorise() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.Distance.Terminal
}

// apiTerminalDistance: POST /api/serveurs/terminal-distance {autoriser, confirme}: the terminal and SFTP from a
// paired device. Allowing needs confirme (the interface asks first) and, once Athanor has a password, that
// password retyped (C1-7: as the other strong rights); refused from a device (not in the allowlist).
func (s *Serveur) apiTerminalDistance(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Autoriser  bool   `json:"autoriser"`
		Confirme   bool   `json:"confirme"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil || c.Autoriser && !c.Confirme {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if c.Autoriser {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil && err != errPreuveSansMdp {
			s.erreur(w, code, err.Error())
			return
		}
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
	if !c.Autoriser { // a device's terminal already open closes too
		fermerTerminaux(func(t *sessionTerminal) bool { return strings.HasPrefix(t.de, "appareil:") })
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "remote terminal", Resultat: resultat})
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"terminal_distance": c.Autoriser})
}
