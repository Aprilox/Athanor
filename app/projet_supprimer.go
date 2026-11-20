package main

// A project's folder sent to the recycle bin: what may go, and the recycle bin the tests replace.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

var corbeilleProjet = mettreALaCorbeille // a test sees what goes, without the real recycle bin

// errCorbeilleRefus: Windows would not move the folder (a file of it is open).
var errCorbeilleRefus = erreurEntree("Windows refused to move the folder to the recycle bin: a program is using one of its files. Close it, then delete again")

// ProgrammeBloquant: a program holding a file of a folder Windows refused to move (Restart Manager).
type ProgrammeBloquant struct {
	PID       int    `json:"pid"`
	Nom       string `json:"nom"`
	Arretable bool   `json:"arretable"` // not Explorer, a service nor a part of Windows
}

var (
	programmesBloquants = programmesDuDossier   // a test names its own
	tuerProgramme       = func(pid int) error { // a test stops nothing for real
		p, err := os.FindProcess(pid)
		if err == nil {
			err = p.Kill()
		}
		return err
	}
)

// arreterBloquants stops, among the pids the owner confirmed, those still holding a file of dossier and stoppable:
// the list is read again here, so a pid sent by the page alone stops nothing. Their names, for the audit log.
func arreterBloquants(dossier string, pids []int) []string {
	var noms []string
	for _, b := range programmesBloquants(dossier) {
		if b.Arretable && slices.Contains(pids, b.PID) && tuerProgramme(b.PID) == nil {
			noms = append(noms, fmt.Sprintf("%s (%d)", b.Nom, b.PID))
		}
	}
	return noms
}

// erreurBloquants: the refusal names the programs, and the page offers to stop the stoppable ones.
func (s *Serveur) erreurBloquants(w http.ResponseWriter, l []ProgrammeBloquant) {
	noms := make([]string, len(l))
	for i, b := range l {
		noms[i] = fmt.Sprintf("%s (%d)", b.Nom, b.PID)
	}
	cle, args := "These programs are using the project's files: {0}. Close them, or stop them from here, then delete again", []string{strings.Join(noms, ", ")}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": cle, "args": args, "erreur": erreurEntreeArgs{cle, args}.Error(), "bloquants": l})
}

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
// arreter: the programs holding its files the owner chose to stop first (after a refusal that named them).
func (s *Serveur) supprimerProjet(w http.ResponseWriter, p Projet, mode string, arreter []int) {
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
			if noms := arreterBloquants(cible, arreter); len(noms) > 0 {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: "propriétaire", Vers: "hub", Projet: p.Nom, Action: "programmes arrêtés avant suppression", Resultat: strings.Join(noms, ", ")})
			}
			err := corbeilleProjet(cible)
			for i := 0; err != nil && len(arreter) > 0 && i < 10; i++ { // a stopped program releases its files a moment later
				time.Sleep(pauseCorbeille)
				err = corbeilleProjet(cible)
			}
			if err != nil {
				if l := programmesBloquants(cible); len(l) > 0 {
					s.erreurBloquants(w, l)
				} else if errors.Is(err, errCorbeilleRefus) {
					s.erreur(w, http.StatusInternalServerError, errCorbeilleRefus.Error())
				} else {
					s.erreurArgs(w, http.StatusInternalServerError, "Cannot send it to the recycle bin: {0}", err.Error())
				}
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
