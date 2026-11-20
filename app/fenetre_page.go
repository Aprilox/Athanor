package main

import (
	"net/http"
	"regexp"
)

// rePageFenetre: the pages a window of their own may open: a server's terminal, or its SFTP.
var rePageFenetre = regexp.MustCompile(`^terminal/[A-Za-z0-9_-]{1,64}(/sftp)?$`)

// ouvrirFenetrePage: opens url in a window of its own (an application window of Edge or Chrome, else the browser;
// Windows, Linux and macOS alike); a test replaces it.
var ouvrirFenetrePage = OuvrirFenetreApp

// apiFenetrePage: POST /api/fenetre/page {page}: a page of Athanor in a window of its own (detach a terminal or its SFTP),
// signed in by a single-use opening link, like the app's own window; never from a paired device (not in distance.go).
func (s *Serveur) apiFenetrePage(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Page string `json:"page"`
	}
	if lireCorps(r, &c) != nil || !rePageFenetre.MatchString(c.Page) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	lien := lienApp()
	if lien == "" {
		s.erreur(w, http.StatusConflict, "Athanor cannot open another window here")
		return
	}
	go ouvrirFenetrePage(lien + "#" + c.Page)
	s.ecrireJSON(w, map[string]any{"ok": true})
}
