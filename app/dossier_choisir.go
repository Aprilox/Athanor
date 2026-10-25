package main

// The system's own folder dialog, for the folder of a project (New project, Join a project, Manage):
// Windows' (IFileOpenDialog), macOS' (choose folder), zenity's or kdialog's on Linux. Without one (or
// from a paired device), the page shows Athanor's own folder picker instead.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var dialogueDossierMu sync.Mutex

// apiChoisirDossier: POST {depart, titre}: the dialog, then {chemin, vide} | {annule} | {indisponible}.
func (s *Serveur) apiChoisirDossier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Depart string `json:"depart"`
		Titre  string `json:"titre"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	if appareilDe(r) != "" { // a phone: the dialog would open on the computer, far from it
		s.ecrireJSON(w, map[string]any{"indisponible": true})
		return
	}
	if !dialogueDossierMu.TryLock() {
		montrerFenetre()
		s.erreur(w, http.StatusConflict, "A folder dialog is already open.")
		return
	}
	defer dialogueDossierMu.Unlock()
	depart := strings.TrimSpace(c.Depart)
	for depart != "" && !estDossierGH(depart) { // the nearest folder that exists
		p := filepath.Dir(depart)
		if p == depart {
			depart = ""
			break
		}
		depart = p
	}
	if depart == "" {
		depart, _ = os.UserHomeDir()
	}
	titre := strings.TrimSpace(c.Titre)
	if len([]rune(titre)) > 120 {
		titre = string([]rune(titre)[:120])
	}
	chemin, ok, dispo := choisirDossierSysteme(depart, titre)
	switch {
	case !dispo:
		s.ecrireJSON(w, map[string]any{"indisponible": true})
	case !ok:
		s.ecrireJSON(w, map[string]any{"annule": true})
	default:
		chemin = filepath.Clean(chemin)
		vide := true
		if e, err := os.ReadDir(chemin); err == nil {
			for _, x := range e {
				vide = vide && x.Name() == ".athanor"
			}
		}
		s.ecrireJSON(w, map[string]any{"chemin": chemin, "vide": vide})
	}
}
