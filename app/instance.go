package main

// One Athanor at a time. The running instance writes its port and a secret into a file of the user's
// (instance.json, mode 0600); a second launch (double-click, start menu) asks it to show its window
// and stops. Without that, the instance started with Windows (in the notification area) and a
// second one would both watch the same projects.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type infoInstance struct {
	Port  int    `json:"port"`
	Jeton string `json:"jeton"`
	PID   int    `json:"pid"`
}

func fichierInstance() string {
	dir, err := os.UserCacheDir() // %LocalAppData% on Windows, ~/.cache on Linux
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "Athanor", "instance.json")
}

// instanceExistante asks the running instance, if any, to show its window ("montrer") or only
// checks that it answers ("ping"). True: it answered, this launch has nothing more to do.
func instanceExistante(action string) bool {
	b, err := os.ReadFile(fichierInstance())
	if err != nil {
		return false
	}
	var i infoInstance
	if json.Unmarshal(b, &i) != nil || i.Port <= 0 || i.Jeton == "" {
		return false
	}
	permettrePremierPlan() // Windows: the running instance may bring its window to the front
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(i.Port)+"/api/instance/"+action, nil)
	req.Header.Set("X-Athanor-Instance", i.Jeton)
	c := &http.Client{Timeout: 3 * time.Second}
	r, err := c.Do(req)
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == http.StatusOK
}

// The running instance's side.
var (
	jetonInstance   = aleatoire(24)
	demandesFenetre = make(chan struct{}, 1)
	fenetreOuverte  atomic.Bool
)

func ecrireInstance(port int) {
	f := fichierInstance()
	_ = os.MkdirAll(filepath.Dir(f), 0o700)
	b, _ := json.Marshal(infoInstance{port, jetonInstance, os.Getpid()})
	_ = os.WriteFile(f, b, 0o600)
}

func effacerInstance() {
	b, err := os.ReadFile(fichierInstance())
	var i infoInstance
	if err == nil && json.Unmarshal(b, &i) == nil && i.PID == os.Getpid() {
		_ = os.Remove(fichierInstance())
	}
}

// demanderFenetre: show the window (bring it to the front if it is already open).
func demanderFenetre() {
	if fenetreOuverte.Load() {
		montrerFenetre()
		return
	}
	select {
	case demandesFenetre <- struct{}{}:
	default:
	}
}

func (s *Serveur) apiInstance(w http.ResponseWriter, r *http.Request) {
	if !egal(r.Header.Get("X-Athanor-Instance"), jetonInstance) {
		s.erreur(w, http.StatusForbidden, "Request refused (origin)")
		return
	}
	switch r.PathValue("action") {
	case "montrer":
		demanderFenetre()
	case "terminal":
		// athanor tui while Athanor runs: a single-use address for the terminal (the secret of instance.json,
		// a file of the user's own, mode 0600, out of reach of Claude's sandbox: as for "montrer")
		s.ecrireJSON(w, map[string]string{"lien": lienApp()})
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

// lienInstanceTerminal: the running instance's single-use address for a terminal; existe: an Athanor
// answers (lien "": a version too old to give one).
func lienInstanceTerminal() (lien string, existe bool) {
	b, err := os.ReadFile(fichierInstance())
	if err != nil {
		return "", false
	}
	var i infoInstance
	if json.Unmarshal(b, &i) != nil || i.Port <= 0 || i.Jeton == "" {
		return "", false
	}
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(i.Port)+"/api/instance/terminal", nil)
	req.Header.Set("X-Athanor-Instance", i.Jeton)
	r, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return "", false
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "", true
	}
	var rep struct {
		Lien string `json:"lien"`
	}
	_ = json.NewDecoder(r.Body).Decode(&rep)
	if !strings.HasPrefix(rep.Lien, "http://127.0.0.1:") {
		return "", true
	}
	return rep.Lien, true
}

// apiFenetre: the terminal interface asks for Athanor's window too (on this computer's screen).
func (s *Serveur) apiFenetre(w http.ResponseWriter, r *http.Request) {
	demanderFenetre()
	s.msg(w, "Athanor's window opens (on this computer's screen).")
}

// avantSortie: what to do before Athanor stops on its own (an update): the terminal interface gives the
// terminal back.
var avantSortie func()
