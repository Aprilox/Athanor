package main

// SFTP page, the server's side beyond the files panel's first routes (serveurs_sftp.go): permissions in octal,
// what a deletion removes, and a small text file viewed and edited in the page. Same terminal connection, same
// audit (path and size, never the content).

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"unicode/utf8"

	"github.com/pkg/sftp"
)

// modeOctal: "755", "0644" or "4755" as permission bits (never more than the 12 of a file's mode).
func modeOctal(v string) (fs.FileMode, bool) {
	if len(v) < 3 || len(v) > 4 {
		return 0, false
	}
	var m uint32
	for _, ch := range v {
		if ch < '0' || ch > '7' {
			return 0, false
		}
		m = m*8 + uint32(ch-'0')
	}
	f := fs.FileMode(m & 0o777)
	if m&0o4000 != 0 {
		f |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		f |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		f |= fs.ModeSticky
	}
	return f, true
}

const inventaireCompteMax = 100000 // entries counted before a deletion's summary stops ("more than")

// inventaire: what a deletion removes, shown in its confirmation (the first paths, the counts, the size).
type inventaire struct {
	Chemins  []string `json:"chemins"`
	Fichiers int      `json:"fichiers"`
	Dossiers int      `json:"dossiers"`
	Taille   int64    `json:"taille"`
	Tronque  bool     `json:"tronque"`
}

func (v *inventaire) ajouter(chemin string, dossier bool, taille int64) {
	if dossier {
		v.Dossiers++
	} else {
		v.Fichiers++
		v.Taille += taille
	}
	if len(v.Chemins) < inventaireMax {
		v.Chemins = append(v.Chemins, chemin)
	} else {
		v.Tronque = true
	}
}

// inventaireSFTP: everything under chemin on the server (a link counts as one file, never followed).
func inventaireSFTP(c *sftp.Client, chemin string) (inventaire, error) {
	v := inventaire{Chemins: []string{}}
	st, err := c.Lstat(chemin)
	if err != nil {
		return v, err
	}
	if !st.IsDir() {
		v.ajouter(chemin, false, st.Size())
		return v, nil
	}
	m := c.Walk(chemin)
	for m.Step() {
		if m.Err() != nil {
			return v, m.Err()
		}
		v.ajouter(m.Path(), m.Stat().IsDir(), m.Stat().Size())
		if v.Fichiers+v.Dossiers >= inventaireCompteMax {
			v.Tronque = true
			break
		}
	}
	return v, nil
}

// texteLisible: at most 1 MB of UTF-8 without NUL bytes (a binary file is downloaded, never shown).
func texteLisible(b []byte) bool {
	return len(b) <= texteSFTPMax && utf8.Valid(b) && !slices.Contains(b, 0)
}

// apiSFTPTexte: GET /api/serveurs/{id}/sftp/{session}/texte?chemin=/abs: a UTF-8 file of 1 MB or less, to view
// and edit in the page ({contenu, modifie}); anything else is downloaded instead.
func (s *Serveur) apiSFTPTexte(w http.ResponseWriter, r *http.Request) {
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	chemin := cheminSFTP(r.URL.Query().Get("chemin"))
	if chemin == "" || chemin == "/" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	st, err := c.Stat(chemin)
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	if st.IsDir() || st.Size() > texteSFTPMax {
		s.erreur(w, http.StatusUnprocessableEntity, errSFTPTexte)
		return
	}
	f, err := c.Open(chemin)
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, texteSFTPMax+1))
	if err != nil {
		s.erreurSFTP(w, err)
		return
	}
	if !texteLisible(b) {
		s.erreur(w, http.StatusUnprocessableEntity, errSFTPTexte)
		return
	}
	t.auditSFTP("sftp.open", fmt.Sprintf("%s (%d bytes)", chemin, len(b)))
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "contenu": string(b), "modifie": st.ModTime().Unix(), "taille": len(b)})
}

// apiSFTPEcrireTexte: POST /api/serveurs/{id}/sftp/{session}/texte {chemin, contenu, modifie}: saves an edited
// text file in place (beside it then renamed, its mode kept), refused when it changed on the server since it
// was opened (modifie). The page asks for confirmation first.
func (s *Serveur) apiSFTPEcrireTexte(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Chemin  string `json:"chemin"`
		Contenu string `json:"contenu"`
		Modifie int64  `json:"modifie"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*texteSFTPMax)).Decode(&a) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	t, c, ok := s.sftpDe(w, r)
	if !ok {
		return
	}
	chemin := cheminSFTP(a.Chemin)
	if chemin == "" || chemin == "/" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	if !texteLisible([]byte(a.Contenu)) {
		s.erreur(w, http.StatusUnprocessableEntity, errSFTPTexte)
		return
	}
	avant, err := c.Lstat(chemin)
	switch {
	case err != nil:
		s.erreurSFTP(w, err)
		return
	case avant.Mode()&fs.ModeSymlink != 0:
		s.erreur(w, http.StatusConflict, errSFTPLien)
		return
	case avant.IsDir():
		s.erreur(w, http.StatusBadRequest, errSFTPDossier)
		return
	case a.Modifie != 0 && avant.ModTime().Unix() != a.Modifie:
		s.erreur(w, http.StatusConflict, errSFTPChange)
		return
	}
	part := chemin + ".athanor-part"
	f, err := c.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err == nil {
		_, err = io.WriteString(f, a.Contenu)
		if e := f.Close(); err == nil {
			err = e
		}
		if err == nil {
			err = c.Chmod(part, avant.Mode().Perm())
		}
		if err == nil {
			err = remplacerSFTP(c, part, chemin)
		}
		if err != nil {
			_ = c.Remove(part)
		}
	}
	if err != nil {
		t.auditSFTP("sftp.edit", chemin+": refused")
		s.erreurSFTP(w, err)
		return
	}
	t.auditSFTP("sftp.edit", fmt.Sprintf("%s (%d bytes)", chemin, len(a.Contenu)))
	modifie := int64(0)
	if st, err := c.Stat(chemin); err == nil {
		modifie = st.ModTime().Unix()
	}
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "modifie": modifie})
}
