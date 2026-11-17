package main

// A project's folder sent to the recycle bin: what may go, and the recycle bin the tests replace.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var corbeilleProjet = mettreALaCorbeille // a test sees what goes, without the real recycle bin

// controlerDossierSupprime: "" when d may go to the recycle bin, else why not.
func controlerDossierSupprime(d, kit string) string {
	if !filepath.IsAbs(d) {
		return "The project's folder path is not a full path"
	}
	if cle := dossierInterdit(d, kit); cle != "" {
		return cle
	}
	fi, err := os.Lstat(d)
	if errors.Is(err, os.ErrNotExist) {
		return "" // already gone: the project is still removed from Studio
	}
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "The project's folder is a link or not a folder: it is never deleted"
	}
	reel := cheminReel(d)
	if runtime.GOOS == "windows" && !strings.EqualFold(reel, filepath.Clean(d)) || runtime.GOOS != "windows" && reel != filepath.Clean(d) {
		return "The project's folder leads elsewhere (a link or a junction): it is never deleted"
	}
	return ""
}

// supprimerProjet: the project leaves Studio with its deployment, its relay and its own settings (retirerProjet);
// mode "atelier" or "dossier" first sends that to the recycle bin. Nothing is ever removed from the servers.
func (s *Serveur) supprimerProjet(w http.ResponseWriter, p Projet, mode string) {
	var cible string
	switch mode {
	case "liste":
	case "atelier":
		cible = p.Atelier()
	case "dossier":
		cible = p.Dossier
		// checked again now: never a system or personal folder, never a link, never another project's files
		if cle := controlerDossierSupprime(p.Dossier, s.kitCourant().Racine); cle != "" {
			s.erreur(w, http.StatusBadRequest, cle)
			return
		}
		if err := s.conflitProjets(p.Dossier, p.Nom); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown action")
		return
	}
	if cible != "" {
		if _, err := os.Lstat(cible); err == nil {
			if err := corbeilleProjet(cible); err != nil {
				s.erreur(w, http.StatusInternalServerError, "Cannot send it to the recycle bin: "+err.Error())
				return
			}
		} else if !os.IsNotExist(err) {
			s.erreur(w, http.StatusInternalServerError, "Cannot send it to the recycle bin: "+err.Error())
			return
		}
	}
	if err := s.retirerProjet(p); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mode != "liste" {
		s.oublierGitHub(p) // its GitHub key goes with its Athanor data (kept when only removed from the list)
	}
	switch mode {
	case "liste":
		s.msg(w, "Project removed from the list. Its folder is intact; delete its .athanor subfolder to clean it.")
	case "atelier":
		s.msg(w, "Project removed; its workshop (.athanor) is in the recycle bin.")
	default:
		s.msg(w, "Project removed; its folder is in the recycle bin.")
	}
}
