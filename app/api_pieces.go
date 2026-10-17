package main

// Files attached to a request (a specification, images, a PDF, data...). They go into the workshop,
// .athanor/attachments/<request>/, so they never reach the project's git; the request gets an
// "Attached files" section listing them, which Claude reads with the request (CLAUDE.md).

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

var taillePieceMax int64 = 50 << 20 // 50 MB per file (a variable for the tests)

var (
	reNomPiece         = regexp.MustCompile(`[^\p{L}\p{N}._() -]+`)
	nomsReserves       = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
	verrouDemandes     sync.Mutex // the request files, written by the attachments
	enteteFichiers     = map[string]string{"en": "## Attached files", "fr": "## Fichiers joints", "es": "## Archivos adjuntos", "de": "## Angehängte Dateien", "it": "## File allegati", "pt": "## Ficheiros anexos"}
	prefixeLignePJ     = "- `.athanor/attachments/"
	errLienAtelier     = errors.New("The workshop holds a link where a folder is expected: nothing was written.")
	errPieceTropGrosse = errors.New("File too large (50 MB at most).")
)

// nomPiece: a file name that is safe on Windows and Linux and readable in Markdown.
func nomPiece(n string) string {
	n = filepath.Base(strings.ReplaceAll(n, `\`, "/"))
	n = strings.TrimSpace(reNomPiece.ReplaceAllString(n, "_"))
	n = strings.Trim(n, ". ")
	if utf8.RuneCountInString(n) > 120 {
		ext := filepath.Ext(n)
		if utf8.RuneCountInString(ext) > 12 {
			ext = ""
		}
		r := []rune(strings.TrimSuffix(n, ext))
		n = strings.TrimRight(string(r[:120-utf8.RuneCountInString(ext)]), ". ") + ext
	}
	if n == "" || n == "/" {
		n = "file"
	}
	if nomsReserves.MatchString(n) {
		n = "_" + n
	}
	return n
}

func tailleLisible(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+1023)>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// dossierSur creates base/parts..., refusing any symbolic link on the way (Claude can write the workshop).
func dossierSur(base string, parties ...string) (string, error) {
	d := base
	for _, x := range parties {
		d = filepath.Join(d, x)
		fi, err := os.Lstat(d)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", errLienAtelier
		}
		if err == nil && !fi.IsDir() {
			return "", errLienAtelier
		}
		if os.IsNotExist(err) {
			if err := os.Mkdir(d, 0o755); err != nil && !os.IsExist(err) {
				return "", err
			}
		} else if err != nil {
			return "", err
		}
	}
	return d, nil
}

// ajouterLignePiece lists an attached file in the request: after the last listed one, or in a new
// "Attached files" section at the end.
func ajouterLignePiece(fichier, ligne, entete string) error {
	verrouDemandes.Lock()
	defer verrouDemandes.Unlock()
	b, err := os.ReadFile(fichier)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	texte := strings.TrimRight(string(b), "\n")
	lignes := strings.Split(texte, "\n")
	derniere := -1
	for i, l := range lignes {
		if strings.HasPrefix(l, prefixeLignePJ) {
			derniere = i
		}
	}
	if derniere >= 0 {
		lignes = append(lignes[:derniere+1], append([]string{ligne}, lignes[derniere+1:]...)...)
		texte = strings.Join(lignes, "\n")
	} else if strings.TrimSpace(texte) == "" {
		texte = entete + "\n\n" + ligne
	} else {
		texte += "\n\n" + entete + "\n\n" + ligne
	}
	return ecrireSur(fichier, []byte(texte+"\n"))
}

func (s *Serveur) apiPiece(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	cible := r.URL.Query().Get("cible")
	var fichier, sous string
	switch {
	case cible == "demande":
		fichier, sous = p.Demande(), "request"
	case reCibleTache.MatchString(cible): // a file for an answer to a task (api_pourvous.go): listed in the answer
		sous = "for-you-" + reCibleTache.FindStringSubmatch(cible)[1]
	case nomFichierSur(cible):
		if _, err := dossierSur(p.Dossier, ".athanor", "queue"); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		fichier, sous = filepath.Join(p.FileAttente(), cible), strings.TrimSuffix(cible, ".md")
		if _, err := os.Stat(fichier); err != nil {
			s.erreur(w, http.StatusNotFound, "This request is no longer in the queue.")
			return
		}
	default:
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	brut, _ := url.QueryUnescape(r.Header.Get("X-Nom-Fichier"))
	nom := nomPiece(brut)
	if _, err := dossierSur(p.Dossier, ".athanor"); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	dossier, err := dossierSur(p.Atelier(), "attachments", sous)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	// the file arrives in a temporary file, then takes a name that is not taken yet
	t, err := os.CreateTemp(dossier, ".envoi-*.tmp")
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := io.Copy(t, io.LimitReader(r.Body, taillePieceMax+1))
	if e := t.Close(); err == nil {
		err = e
	}
	if err == nil && n > taillePieceMax {
		err = errPieceTropGrosse
	}
	if err != nil {
		os.Remove(t.Name())
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	ext := filepath.Ext(nom)
	base := strings.TrimSuffix(nom, ext)
	final := nom
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(dossier, final)); os.IsNotExist(err) {
			break
		}
		final = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	if err := os.Rename(t.Name(), filepath.Join(dossier, final)); err != nil {
		os.Remove(t.Name())
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	chemin := ".athanor/attachments/" + sous + "/" + final
	genre := strings.ToUpper(strings.TrimPrefix(ext, "."))
	if genre == "" {
		genre = "file"
	}
	entete := enteteFichiers[s.langue()]
	if entete == "" {
		entete = enteteFichiers["en"]
	}
	if fichier == "" {
		s.ecrireJSON(w, map[string]any{"nom": final, "chemin": chemin, "taille": n})
		return
	}
	if err := ajouterLignePiece(fichier, "- `"+chemin+"` ("+genre+", "+tailleLisible(n)+")", entete); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]any{"nom": final, "chemin": chemin, "taille": n})
}
