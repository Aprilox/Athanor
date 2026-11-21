package main

// SFTP page, the Local panel: this computer's files, read and changed by Athanor's Go at the page's request
// (list, new folder, rename, delete). Local routes behind garde (access cookie, anti-CSRF header, origin) and
// refused from a paired device like the terminal (routeShellDistance: /api/serveurs/local/sftp) unless the owner
// allowed it with a confirmation. The assistant has no tool on them: it never sees this disk. Athanor's data
// folder (vault, keys, settings) is never changed nor sent from here (protegeLocal).

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const (
	errLocalProtege    = "This is Athanor's data folder (vault, keys, settings): it is never changed nor sent from this panel"
	errLocalPasDossier = "This is not a folder"
	errLocalRacine     = "A disk or your personal folder cannot be deleted from here"
)

// cheminLocal: an absolute path of this computer, cleaned ("" when invalid). Network and device paths (\\server,
// \\?\) are refused: opening one would send this PC's Windows identity to another machine.
func cheminLocal(p string) string {
	if p == "" || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return ""
	}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
			return ""
		}
		if len(p) == 2 && p[1] == ':' { // "C:" alone means the current folder of that disk
			p += `\`
		}
	}
	if !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

// nomLocal: a file name alone, valid on this system.
func nomLocal(n string) bool {
	if !nomSFTP(n) {
		return false
	}
	return runtime.GOOS != "windows" || !strings.ContainsAny(n, `\:*?"<>|`) && strings.TrimRight(n, ". ") == n
}

// dossiersProteges: Athanor's data folders on this computer.
func dossiersProteges() []string {
	var l []string
	for _, d := range []string{dossierAthanor(), dossierAthanorDefaut(), dossierDonneesApp()} {
		if d = cheminLocal(d); d != "" && !slices.Contains(l, d) {
			l = append(l, d)
		}
	}
	return l
}

// dansProtege: p is Athanor's data folder or inside it.
func dansProtege(p string, proteges []string) bool {
	return slices.ContainsFunc(proteges, func(d string) bool { return dansDossier(d, p) })
}

// protegeLocal: p touches Athanor's data folder: is it, is inside it, or holds it (sending or deleting a parent
// would take the vault with it).
func protegeLocal(p string, proteges []string) bool {
	for _, d := range proteges {
		if dansDossier(d, p) || dansDossier(p, d) {
			return true
		}
	}
	return false
}

// racinesLocales: where the Local panel can start: the disks on Windows, "/" elsewhere.
func racinesLocales() []string {
	if runtime.GOOS != "windows" {
		return []string{"/"}
	}
	var l []string
	for c := 'C'; c <= 'Z'; c++ { // A: and B: are skipped: an empty floppy drive answers slowly
		if _, err := os.Stat(string(c) + `:\`); err == nil {
			l = append(l, string(c)+`:\`)
		}
	}
	return l
}

type entreeLocale struct {
	entreeSFTP
	Protege bool `json:"protege,omitempty"`
}

// erreurLocale: the system's refusal in plain words.
func (s *Serveur) erreurLocale(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.erreur(w, http.StatusNotFound, errSFTPInconnu)
	case errors.Is(err, fs.ErrPermission):
		s.erreur(w, http.StatusForbidden, errSFTPRefuse)
	case errors.Is(err, fs.ErrExist):
		s.erreur(w, http.StatusConflict, errSFTPExiste)
	default:
		s.erreur(w, http.StatusInternalServerError, err.Error())
	}
}

// apiLocalLister: GET /api/serveurs/local/sftp?chemin=abs (empty: the personal folder): a folder of this computer
// with {chemin, parent, separateur, maison, racines, entrees, tronque}.
func (s *Serveur) apiLocalLister(w http.ResponseWriter, r *http.Request) {
	maison, _ := os.UserHomeDir()
	chemin := r.URL.Query().Get("chemin")
	if chemin == "" {
		chemin = maison
	}
	if chemin = cheminLocal(chemin); chemin == "" {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin)
		return
	}
	if st, err := os.Stat(chemin); err != nil {
		s.erreurLocale(w, err)
		return
	} else if !st.IsDir() {
		s.erreur(w, http.StatusBadRequest, errLocalPasDossier)
		return
	}
	des, err := os.ReadDir(chemin)
	if err != nil && len(des) == 0 {
		s.erreurLocale(w, err)
		return
	}
	tronque := len(des) > entreesSFTPMax
	entrees := make([]entreeLocale, 0, min(len(des), entreesSFTPMax))
	proteges := dossiersProteges()
	for _, de := range des[:min(len(des), entreesSFTPMax)] {
		p := filepath.Join(chemin, de.Name())
		e := entreeLocale{entreeSFTP: entreeSFTP{Nom: de.Name(), Dossier: de.IsDir()}}
		if fi, err := de.Info(); err == nil {
			e.Taille, e.Modifie, e.Mode = fi.Size(), fi.ModTime().Unix(), fi.Mode().String()
		}
		if de.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 { // a link or a Windows junction: opens as its target
			e.Lien = de.Type()&fs.ModeSymlink != 0
			e.Cible, _ = os.Readlink(p)
			if st, err := os.Stat(p); err == nil {
				e.Dossier = st.IsDir()
			}
		}
		e.Protege = protegeLocal(p, proteges)
		entrees = append(entrees, e)
	}
	slices.SortFunc(entrees, func(a, b entreeLocale) int {
		if a.Dossier != b.Dossier {
			if a.Dossier {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Nom), strings.ToLower(b.Nom))
	})
	parent := filepath.Dir(chemin)
	if parent == chemin {
		parent = ""
	}
	s.ecrireJSON(w, map[string]any{"chemin": chemin, "parent": parent, "separateur": string(filepath.Separator),
		"maison": maison, "racines": racinesLocales(), "entrees": entrees, "tronque": tronque})
}

func (s *Serveur) auditLocal(r *http.Request, action, resultat string) {
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "local", Action: action, Resultat: resultat})
}

// inventaireLocal: everything under chemin on this computer (a link counts as one file, never followed).
func inventaireLocal(chemin string) (inventaire, error) {
	v := inventaire{Chemins: []string{}}
	err := filepath.WalkDir(chemin, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		var taille int64
		if fi, e := d.Info(); e == nil {
			taille = fi.Size()
		}
		v.ajouter(p, d.IsDir(), taille)
		if v.Fichiers+v.Dossiers >= inventaireCompteMax {
			v.Tronque = true
			return fs.SkipAll
		}
		return nil
	})
	return v, err
}

// apiLocalAgir: POST /api/serveurs/local/sftp {dossier, nom} (new folder), {renommer, nom}, {supprimer} or
// {inventaire} (what deleting would remove, for the confirmation). Athanor's data folder is refused.
func (s *Serveur) apiLocalAgir(w http.ResponseWriter, r *http.Request) {
	var a struct {
		Renommer   string `json:"renommer"`
		Nom        string `json:"nom"`
		Supprimer  string `json:"supprimer"`
		Dossier    string `json:"dossier"`
		Inventaire string `json:"inventaire"`
	}
	if lireCorps(r, &a) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	chemin := cheminLocal(a.Renommer + a.Supprimer + a.Dossier + a.Inventaire)
	if chemin == "" || len(a.Renommer+a.Supprimer+a.Dossier+a.Inventaire) != max(len(a.Renommer), len(a.Supprimer), len(a.Dossier), len(a.Inventaire)) {
		s.erreur(w, http.StatusBadRequest, errSFTPChemin) // none, or more than one action
		return
	}
	if maison, _ := os.UserHomeDir(); a.Supprimer != "" && (filepath.Dir(chemin) == chemin || maison != "" && strings.EqualFold(chemin, filepath.Clean(maison))) {
		s.erreur(w, http.StatusBadRequest, errLocalRacine)
		return
	}
	// a new folder may go beside the data folder (in its parent), never inside it; the rest never touches it
	if proteges := dossiersProteges(); a.Dossier != "" && dansProtege(chemin, proteges) || a.Dossier == "" && protegeLocal(chemin, proteges) {
		s.erreur(w, http.StatusForbidden, errLocalProtege)
		return
	}
	switch {
	case a.Dossier != "":
		if !nomLocal(a.Nom) {
			s.erreur(w, http.StatusBadRequest, errSFTPChemin)
			return
		}
		cible := filepath.Join(chemin, a.Nom)
		if err := os.Mkdir(cible, 0o755); err != nil {
			s.erreurLocale(w, err)
			return
		}
		s.auditLocal(r, "sftp.local.mkdir", cible)
		s.ecrireJSON(w, map[string]any{"chemin": cible})
	case a.Renommer != "":
		if !nomLocal(a.Nom) || filepath.Dir(chemin) == chemin {
			s.erreur(w, http.StatusBadRequest, errSFTPChemin)
			return
		}
		vers := filepath.Join(filepath.Dir(chemin), a.Nom)
		if vers == chemin {
			s.erreur(w, http.StatusBadRequest, errSFTPMemeNom)
			return
		}
		if _, err := os.Lstat(vers); err == nil && (runtime.GOOS == "linux" || !strings.EqualFold(vers, chemin)) { // a case change is allowed where names ignore case
			s.erreur(w, http.StatusConflict, errSFTPExiste)
			return
		}
		if err := os.Rename(chemin, vers); err != nil {
			s.erreurLocale(w, err)
			return
		}
		s.auditLocal(r, "sftp.local.rename", chemin+" -> "+vers)
		s.ecrireJSON(w, map[string]any{"chemin": vers})
	case a.Supprimer != "":
		st, err := os.Lstat(chemin)
		if err == nil {
			if st.IsDir() {
				err = os.RemoveAll(chemin)
			} else {
				err = os.Remove(chemin) // a link or a junction: the link only
			}
		}
		if err != nil {
			s.erreurLocale(w, err)
			return
		}
		s.auditLocal(r, "sftp.local.delete", chemin)
		s.ecrireJSON(w, map[string]any{"ok": true})
	default:
		inv, err := inventaireLocal(chemin)
		if err != nil {
			s.erreurLocale(w, err)
			return
		}
		s.ecrireJSON(w, inv)
	}
}
