package main

// Files panel next to the terminal: browse, upload, download, rename and delete the server's files through the
// open terminal's own SSH connection (same key, same host fingerprint, same lifetime: it closes with the terminal).
// The owner already has a shell on this server, so paths are not restricted as the agents' are (serveurs_outils.go);
// every download and change is written to the audit log with its path, never the content. Refused from a paired
// device like the terminal (routeShellDistance: /api/serveurs/{id}/sftp/…).

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

const (
	envoiSFTPMax     = 4 << 30 // one upload
	entreesSFTPMax   = 5000    // one folder's listing
	errSFTPAbsent    = "This server does not offer file transfer (SFTP)"
	errSFTPExiste    = "A file or folder with this name already exists"
	errSFTPInconnu   = "This file or folder no longer exists"
	errSFTPRefuse    = "The server refused: permission denied"
	errSFTPDossier   = "This is a folder"
	errSFTPChemin    = "Invalid path"
	errSFTPMemeNom   = "Choose another name"
	errSFTPTropGros  = "This file is too large to send from here"
	prefixeSFTPEchec = "The server could not do it: check its free disk space and this folder's rights, or try the same in the terminal. Its answer: "
	errSFTPLien      = "This name is a link: open the folder of its target to replace that file"
)

// clientSFTP: the terminal's SFTP channel, opened at the first use.
func (t *sessionTerminal) clientSFTP() (*sftp.Client, error) {
	t.muSFTP.Lock()
	defer t.muSFTP.Unlock()
	if t.sftp == nil {
		c, err := sftp.NewClient(t.client)
		if err != nil {
			return nil, err
		}
		t.sftp = c
	}
	return t.sftp, nil
}

// cheminSFTP: an absolute path on the server, cleaned ("" when invalid).
func cheminSFTP(p string) string {
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return ""
	}
	return path.Clean(p)
}

// nomSFTP: a file name alone (no folder, not "." nor "..").
func nomSFTP(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\x00") && len(n) <= 255
}

// sftpDe: the open terminal named by the path and its SFTP client; answers the error itself.
func (s *Serveur) sftpDe(w http.ResponseWriter, r *http.Request) (*sessionTerminal, *sftp.Client, bool) {
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return nil, nil, false
	}
	t, raison := terminalDe(r)
	if t == nil {
		s.terminalFerme(w, raison)
		return nil, nil, false
	}
	if r.Method == http.MethodPost && tacheDirecte(t.id) != nil { // live mode: the files are locked like the keyboard
		s.erreur(w, http.StatusLocked, "The assistant is working in this terminal: stop it or take over first")
		return nil, nil, false
	}
	c, err := t.clientSFTP()
	if err != nil {
		s.erreur(w, http.StatusBadGateway, errSFTPAbsent)
		return nil, nil, false
	}
	t.toucher() // working on the files keeps the terminal alive
	return t, c, true
}

// erreurSFTP: the server's refusal in plain words.
func (s *Serveur) erreurSFTP(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.erreur(w, http.StatusNotFound, errSFTPInconnu)
	case errors.Is(err, fs.ErrPermission):
		s.erreur(w, http.StatusForbidden, errSFTPRefuse)
	default: // C1-15: plain words and what to try first, the server's own answer after them
		s.erreur(w, http.StatusBadGateway, prefixeSFTPEchec+err.Error())
	}
}

func (t *sessionTerminal) auditSFTP(action, resultat string) {
	t.s.ecrireAudit(LigneAudit{Type: "evenement", De: t.de, Vers: "serveur:" + t.serveur, Action: action, Resultat: resultat})
}

type entreeSFTP struct {
	Nom     string `json:"nom"`
	Dossier bool   `json:"dossier"`
	Lien    bool   `json:"lien,omitempty"`
	Taille  int64  `json:"taille"`
	Modifie int64  `json:"modifie"` // unix seconds
	Mode    string `json:"mode"`
}

// apiSFTPLister: GET /api/serveurs/{id}/sftp/{session}?chemin=/abs (empty: the user's home folder).
func (s *Serveur) apiSFTPLister(w http.ResponseWriter, r *http.Request) {
	_, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	chemin := r.URL.Query().Get("chemin")
	if chemin == "" {
		var err error
		if chemin, err = c.Getwd(); err != nil || cheminSFTP(chemin) == "" {
			chemin = "/"
		}
	}
	if chemin = cheminSFTP(chemin); chemin == "" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	infos, err := c.ReadDir(chemin)
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	tronque := len(infos) > entreesSFTPMax
	entrees := make([]entreeSFTP, 0, min(len(infos), entreesSFTPMax))
	for _, fi := range infos[:min(len(infos), entreesSFTPMax)] {
		e := entreeSFTP{Nom: fi.Name(), Dossier: fi.IsDir(), Taille: fi.Size(), Modifie: fi.ModTime().Unix(), Mode: fi.Mode().String()}
		if fi.Mode()&fs.ModeSymlink != 0 { // a link to a folder opens like a folder
			e.Lien = true
			if cible, err := c.Stat(path.Join(chemin, fi.Name())); err == nil {
				e.Dossier = cible.IsDir()
			}
		}
		entrees = append(entrees, e)
	}
	slices.SortFunc(entrees, func(a, b entreeSFTP) int {
		if a.Dossier != b.Dossier {
			if a.Dossier {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Nom), strings.ToLower(b.Nom))
	})
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "entrees": entrees, "tronque": tronque})
}

// apiSFTPTelecharger: GET /api/serveurs/{id}/sftp/{session}/fichier?chemin=/abs: the file's bytes.
func (s *Serveur) apiSFTPTelecharger(w http.ResponseWriter, r *http.Request) {
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	chemin := cheminSFTP(r.URL.Query().Get("chemin"))
	if chemin == "" || chemin == "/" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	f, err := c.Open(chemin)
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	if st.IsDir() {
		s.erreur(w, http.StatusBadRequest, errSFTPDossier)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(path.Base(chemin)))
	w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	n, err := io.Copy(w, f)
	resultat := fmt.Sprintf("%s (%d bytes)", chemin, n)
	if err != nil {
		resultat += " interrupted"
	}
	t.auditSFTP("sftp.download", resultat)
}

// C1-14: a download streams to the browser's own download (an <a download> navigation, never a whole blob in the
// page). A navigation cannot send the anti-CSRF header, so the page first asks a ticket with it: single use,
// 1 minute, bound to its terminal and path; the GET with that ticket still needs the access cookie and session.
var ticketsSFTP = struct {
	sync.Mutex
	m map[string]ticketSFTP
}{m: map[string]ticketSFTP{}}

type ticketSFTP struct {
	serveur, session, chemin string
	fin                      time.Time
}

// apiSFTPTicket: POST /api/serveurs/{id}/sftp/{session}/ticket {chemin}: {ticket} for apiSFTPTicketLire.
func (s *Serveur) apiSFTPTicket(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Chemin string `json:"chemin"`
	}
	if lireCorps(r, &c) != nil || cheminSFTP(c.Chemin) == "" || cheminSFTP(c.Chemin) == "/" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	if !memeOrigine(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	if t, raison := terminalDe(r); t == nil { // not sftpDe: reading stays allowed in live mode (C1-6)
		s.terminalFerme(w, raison)
		return
	}
	jeton := aleatoire(24)
	ticketsSFTP.Lock()
	for k, x := range ticketsSFTP.m {
		if time.Now().After(x.fin) {
			delete(ticketsSFTP.m, k)
		}
	}
	ticketsSFTP.m[jeton] = ticketSFTP{r.PathValue("id"), r.PathValue("session"), cheminSFTP(c.Chemin), time.Now().Add(time.Minute)}
	ticketsSFTP.Unlock()
	s.ecrireJSON(w, map[string]any{"ticket": jeton})
}

// apiSFTPTicketLire: GET /api/serveurs/{id}/sftp/{session}/ticket/{ticket}: the file, as apiSFTPTelecharger.
// Not behind garde (no header on a navigation): the ticket stands for it, the access and the session are checked.
func (s *Serveur) apiSFTPTicketLire(w http.ResponseWriter, r *http.Request) {
	if !s.accesValide(r) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	if s.verrouillePour(r) {
		s.erreur(w, http.StatusUnauthorized, "Locked")
		return
	}
	ticketsSFTP.Lock()
	x, ok := ticketsSFTP.m[r.PathValue("ticket")]
	delete(ticketsSFTP.m, r.PathValue("ticket"))
	ticketsSFTP.Unlock()
	if !ok || time.Now().After(x.fin) || x.serveur != r.PathValue("id") || x.session != r.PathValue("session") {
		s.erreur(w, http.StatusGone, "This download link has expired: click Download again")
		return
	}
	r.URL.RawQuery = url.Values{"chemin": {x.chemin}}.Encode()
	s.apiSFTPTelecharger(w, r)
}

// apiSFTPEnvoyer: POST /api/serveurs/{id}/sftp/{session}/fichier?chemin=/abs/name[&remplacer=1], the bytes as
// the body. Written beside the target then renamed over it, so that a cut upload never leaves half a file;
// an existing file is only replaced when asked (the interface asks first), and keeps its mode.
func (s *Serveur) apiSFTPEnvoyer(w http.ResponseWriter, r *http.Request) {
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	chemin := cheminSFTP(r.URL.Query().Get("chemin"))
	if chemin == "" || !nomSFTP(path.Base(chemin)) {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	if r.ContentLength > envoiSFTPMax {
		s.erreur(w, http.StatusRequestEntityTooLarge, errSFTPTropGros)
		return
	}
	avant, err := c.Lstat(chemin)
	switch {
	case err == nil && avant.Mode()&fs.ModeSymlink != 0: // renaming over it would turn the link into a plain file with the link's 0777 mode
		s.erreur(w, http.StatusConflict, errSFTPLien)
		return
	case err == nil && (avant.IsDir() || r.URL.Query().Get("remplacer") != "1"):
		s.erreur(w, http.StatusConflict, errSFTPExiste)
		return
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		s.erreurSFTP(w, err)
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	part := chemin + ".athanor-part"
	f, err := c.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, envoiSFTPMax))
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil && avant != nil {
		err = c.Chmod(part, avant.Mode().Perm())
	}
	if err == nil {
		err = remplacerSFTP(c, part, chemin)
	}
	if err != nil {
		_ = c.Remove(part)
		t.auditSFTP("sftp.upload", fmt.Sprintf("%s: error after %d bytes", chemin, n))
		var trop *http.MaxBytesError
		if errors.As(err, &trop) {
			s.erreur(w, http.StatusRequestEntityTooLarge, errSFTPTropGros)
			return
		}
		s.erreurSFTP(w, err)
		return
	}
	t.auditSFTP("sftp.upload", fmt.Sprintf("%s (%d bytes)", chemin, n))
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "taille": n})
}

// remplacerSFTP: part takes cible's place, in one step where the server has OpenSSH's posix-rename; Dropbear,
// BusyBox and some NAS lack it (T2-2): the old file goes first, then a plain rename (a short moment without it)
func remplacerSFTP(c *sftp.Client, part, cible string) error {
	if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
		return c.PosixRename(part, cible)
	}
	if err := c.Remove(cible); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return c.Rename(part, cible)
}

// apiSFTPAgir: POST /api/serveurs/{id}/sftp/{session} {renommer, nom} or {supprimer}: rename an entry inside its
// folder, or delete a file or a whole folder (the interface asks for confirmation first).
func (s *Serveur) apiSFTPAgir(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Renommer  string `json:"renommer"`
		Nom       string `json:"nom"`
		Supprimer string `json:"supprimer"`
	}
	if lireCorps(r, &a) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	switch {
	case a.Renommer != "":
		de := cheminSFTP(a.Renommer)
		if de == "" || de == "/" || !nomSFTP(a.Nom) {
			s.erreur(w, http.StatusBadRequest, errSFTPChemin)
			return
		}
		vers := path.Join(path.Dir(de), a.Nom)
		if vers == de {
			s.erreur(w, http.StatusBadRequest, errSFTPMemeNom)
			return
		}
		if _, err := c.Lstat(vers); err == nil {
			s.erreur(w, http.StatusConflict, errSFTPExiste)
			return
		}
		if err := c.Rename(de, vers); err != nil {
			s.erreurSFTP(w, err)
			return
		}
		t.auditSFTP("sftp.rename", de+" -> "+vers)
		s.ecrireJSON(w, map[string]any{"chemin": vers})
	case a.Supprimer != "":
		chemin := cheminSFTP(a.Supprimer)
		if chemin == "" || chemin == "/" {
			s.erreur(w, http.StatusBadRequest, errSFTPChemin)
			return
		}
		st, err := c.Lstat(chemin)
		if err == nil {
			if st.IsDir() {
				err = c.RemoveAll(chemin)
			} else {
				err = c.Remove(chemin) // a link: the link only
			}
		}
		if err != nil {
			s.erreurSFTP(w, err)
			return
		}
		t.auditSFTP("sftp.delete", chemin)
		s.ecrireJSON(w, map[string]any{"ok": true})
	default:
		s.erreur(w, http.StatusBadRequest, "Invalid request")
	}
}
