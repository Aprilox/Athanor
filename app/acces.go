package main

// Who may use the interface: only the windows Athanor opens itself. Every window is opened on a
// single-use address (/?ouvrir=…) that gives it a cookie; the page (and the secret header it carries)
// and the API refuse anything without that cookie. A program of the computer that finds the port, for
// instance a command Claude runs, cannot drive the application.

import (
	"net/http"
	"time"
)

const cookieAcces = "athanor_acces"

// lienOuverture: a single-use address for a new window (valid 2 minutes).
func (s *Serveur) lienOuverture(base string) string {
	j := aleatoire(24)
	s.mu.Lock()
	if s.ouvertures == nil {
		s.ouvertures = map[string]time.Time{}
	}
	for k, t := range s.ouvertures {
		if time.Now().After(t) {
			delete(s.ouvertures, k)
		}
	}
	s.ouvertures[j] = time.Now().Add(2 * time.Minute)
	s.mu.Unlock()
	return base + "?ouvrir=" + j
}

func (s *Serveur) consommerOuverture(j string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.ouvertures {
		if egal(k, j) {
			delete(s.ouvertures, k)
			return time.Now().Before(t)
		}
	}
	return false
}

// accesValide: the request comes from a window Athanor opened (no access key = tests, check off).
func (s *Serveur) accesValide(r *http.Request) bool {
	if appareilDe(r) != "" {
		return true // a trusted device, through the tunnel's server (distance.go)
	}
	if s.cleAcces == "" {
		return true
	}
	c, err := r.Cookie(cookieAcces)
	return err == nil && egal(c.Value, s.cleAcces)
}

// pageAcces serves the interface: a single-use address gives the cookie, then the page.
func (s *Serveur) pageAcces(w http.ResponseWriter, r *http.Request) bool {
	if j := r.URL.Query().Get("ouvrir"); j != "" {
		if s.consommerOuverture(j) {
			http.SetCookie(w, &http.Cookie{Name: cookieAcces, Value: s.cleAcces, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return false
		}
	}
	if s.accesValide(r) {
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Athanor</title><body style="font-family:sans-serif;background:#111;color:#ddd;padding:40px">
<p>Open Athanor from its shortcut (Athanor.exe, or athanor on Linux): this page only works in the window Athanor opens.</p>
<p>Ouvrez Athanor depuis son raccourci (Athanor.exe, ou athanor sous Linux) : cette page ne fonctionne que dans la fenêtre qu'Athanor ouvre.</p>`))
	return false
}
