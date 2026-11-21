package main

// The folders the user points the home conversation to (M196), and its attached files. A folder is checked
// here, on the Go side, when it is pointed and again at each turn: a full path, no "..", no symbolic link nor
// junction on the way, never Athanor's folders (the kit, the vault), a secrets file or a system folder. The
// session reads them (--add-dir) behind the files guard: read only, or read and write when the user said so
// and switched Writing on (outil_chat_session.go, outil_gardes.go).

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type DossierChat struct {
	Chemin   string `json:"chemin"`
	Ecriture bool   `json:"ecriture"` // read and write; else read only
}

const dossiersChatMax = 10

var (
	errDossierAbsolu  = errors.New("Give the folder's full path.")
	errDossierPoints  = errors.New("A path with “..” is refused: give the folder itself.")
	errDossierAbsent  = errors.New("This folder does not exist.")
	errDossierLien    = errors.New("A symbolic link or a junction is refused: point the real folder.")
	errDossierSecret  = errors.New("A secrets file (.env, secrets.env) is never given to the assistant.")
	errDossierAthanor = errors.New("Athanor's own folders (the tools, the vault) are never given to the assistant.")
	errDossierSysteme = errors.New("A system folder, a drive or your whole home folder is never given to the assistant: point a narrower folder.")
	errDossiersTrop   = errors.New("10 folders at most.")
)

// sousChemin: c is r or inside it (case-insensitive on Windows).
func sousChemin(c, r string) bool {
	if runtime.GOOS == "windows" {
		c, r = strings.ToLower(c), strings.ToLower(r)
	}
	if c == r {
		return true
	}
	return strings.HasPrefix(c, strings.TrimRight(r, `\/`)+string(filepath.Separator))
}

// aliasMac: on macOS /var, /tmp and /etc are the system's own links to /private/…, never one the owner made.
func aliasMac(d string) bool {
	return runtime.GOOS == "darwin" && (d == "/var" || d == "/tmp" || d == "/etc")
}

// sansAliasMac: c as macOS opens it once those links are followed ("/var/x" → "/private/var/x").
func sansAliasMac(c string) string {
	for _, a := range []string{"/var", "/tmp", "/etc"} {
		if aliasMac(a) && (c == a || strings.HasPrefix(c, a+"/")) {
			return "/private" + c
		}
	}
	return c
}

// verifierDossierChat: the folder, cleaned, or why it cannot be given to the assistant.
func verifierDossierChat(k Kit, brut string) (string, error) {
	brut = strings.TrimSpace(brut)
	if brut == "" || !filepath.IsAbs(brut) {
		return "", errDossierAbsolu
	}
	if slices.Contains(strings.FieldsFunc(brut, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
		return "", errDossierPoints
	}
	c := filepath.Clean(brut)
	if runtime.GOOS == "windows" && strings.HasPrefix(c, `\\`) {
		return "", errDossierAbsolu // UNC, \\?\, \\.\: a drive letter only
	}
	// each step of the path: no symbolic link, no junction (Windows: a mount point that Readlink reads)
	for d := c; ; d = filepath.Dir(d) {
		fi, err := os.Lstat(d)
		if err == nil && aliasMac(d) {
			fi, err = os.Stat(d) // macOS's own link (/etc → /private/etc): the folder it leads to
		}
		if err != nil {
			return "", errDossierAbsent
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errDossierLien
		}
		if fi.Mode()&os.ModeIrregular != 0 {
			if _, err := os.Readlink(d); err == nil {
				return "", errDossierLien
			}
		}
		if d == c && !fi.IsDir() {
			return "", errDossierAbsent
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	// the path as typed and as the system opens it (an 8.3 short name leads to the same folder)
	reel := cheminReel(c)
	if runtime.GOOS == "windows" && strings.HasPrefix(reel, `\\`) {
		return "", errDossierAbsolu
	}
	for _, x := range []string{c, reel} {
		if err := interditChat(k, x); err != nil {
			return "", err
		}
	}
	return c, nil
}

// interditChat: why the folder c (cleaned) can never be given to the assistant, or nil.
func interditChat(k Kit, c string) error {
	for _, x := range strings.FieldsFunc(c, func(r rune) bool { return r == '/' || r == '\\' }) {
		if n := strings.ToLower(x); n == ".env" || n == "secrets.env" {
			return errDossierSecret
		}
	}
	if filepath.Dir(c) == c {
		return errDossierSysteme // a drive, the root
	}
	for _, d := range dossiersSysteme() { // the same as for a project's folder (api_projets.go)
		if d != "" && (sousChemin(c, filepath.Clean(d)) || sousChemin(c, cheminReel(d))) {
			return errDossierSysteme
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, h := range []string{filepath.Clean(home), cheminReel(home)} {
			if sousChemin(h, c) { // the home folder or a parent of it
				return errDossierSysteme
			}
			for _, x := range []string{".ssh", ".gnupg", ".aws", ".claude", ".config", ".docker", ".kube"} {
				if sousChemin(c, filepath.Join(h, x)) {
					return errDossierSysteme
				}
			}
		}
	}
	// Athanor's folders: neither inside them, nor around them (a parent would hold them)
	for _, a := range []string{k.Racine, dossierAthanor(), filepath.Dir(cheminCoffre())} {
		if a == "" {
			continue
		}
		for _, b := range []string{filepath.Clean(a), cheminReel(a)} {
			if sousChemin(c, b) || sousChemin(b, c) {
				return errDossierAthanor
			}
		}
	}
	return nil
}

// verifierDossiersChat: the list checked, without doubles.
func verifierDossiersChat(k Kit, l []DossierChat) ([]DossierChat, error) {
	if len(l) > dossiersChatMax {
		return nil, errDossiersTrop
	}
	var out []DossierChat
	for _, d := range l {
		c, err := verifierDossierChat(k, d.Chemin)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(out, func(x DossierChat) bool { return sousChemin(c, x.Chemin) && sousChemin(x.Chemin, c) }) {
			out = append(out, DossierChat{Chemin: c, Ecriture: d.Ecriture})
		}
	}
	return out, nil
}

// texteDossiersChat: the folders as the system prompt gives them to the assistant.
func texteDossiersChat(l []DossierChat, ecriture bool) string {
	if len(l) == 0 {
		return ""
	}
	t := "\n\nFolders the user pointed you to (read them with Read, Glob and Grep; nothing else of the computer):"
	for _, d := range l {
		acces := "read only"
		if d.Ecriture && ecriture {
			acces = "read and write"
		}
		t += "\n- `" + d.Chemin + "` (" + acces + ")"
	}
	return t
}

// Attached files of the home conversation: images, PDF and text only, 20 MB at most each (Claude reads them).
const taillePieceAccueil = 20 << 20

var typesPieceAccueil = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".pdf": true,
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true, ".yaml": true, ".yml": true, ".xml": true, ".toml": true,
	".ini": true, ".log": true, ".html": true, ".css": true, ".js": true, ".ts": true, ".go": true, ".py": true, ".sql": true}

// apiHubChatPiece: a file attached to a message of the home conversation, into its own attachments folder.
func (s *Serveur) apiHubChatPiece(w http.ResponseWriter, r *http.Request) {
	p := projetAccueil(s.kitCourant())
	if err := os.MkdirAll(p.Dossier, 0o700); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !typesPieceAccueil[strings.ToLower(filepath.Ext(nomPieceEnvoyee(r)))] {
		s.erreur(w, http.StatusBadRequest, "This type of file is not accepted: images, PDF and text files only.")
		return
	}
	s.recevoirPiece(w, r, p, "", "chat", taillePieceAccueil)
}
