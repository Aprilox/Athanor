package main

// Tasks for the user (page "For you"): what Claude cannot do itself (an account, a payment, a code
// received by SMS, a key to generate...). Claude writes them with `athanor todo` (system/taches.py) in
// .athanor/for-you/tasks.json; the user answers here, round after round: done (with a text, files,
// secret values), or skip (Claude finds another way). Claude checks each answer on its next cycle and
// says whether it works or what is still missing. The secret values go to .athanor/secrets.env, never
// into the answer's text.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	verrouReponses = sync.RWMutex{} // replies.json and secrets.env, written by the application alone
	reIDTache      = regexp.MustCompile(`^T[0-9]{1,5}$`)
	reNomSecret    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,59}$`)
	reLigneSecret  = regexp.MustCompile(`^([A-Z][A-Z0-9_]{1,59})='(?:[^']|'\\'')*'$`)
	reEntier       = regexp.MustCompile(`^[0-9]+$`)
	// names a value could not take (secrets.env is sourced with set -a): same lists as system/taches.py
	secretsInterdits = map[string]bool{"PATH": true, "HOME": true, "SHELL": true, "ENV": true, "IFS": true, "PS1": true, "PS2": true, "PS3": true, "PS4": true,
		"PROMPT_COMMAND": true, "SHELLOPTS": true, "CDPATH": true, "GLOBIGNORE": true, "TMPDIR": true, "PWD": true, "OLDPWD": true, "USER": true,
		"LOGNAME": true, "TERM": true, "LANG": true, "TOOLS": true, "PROJECT": true, "SSH_AUTH_SOCK": true, "HTTP_PROXY": true, "HTTPS_PROXY": true,
		"ALL_PROXY": true, "NO_PROXY": true, "EDITOR": true, "VISUAL": true, "PAGER": true}
	prefixesInterdits = []string{"LD_", "DYLD_", "BASH", "GIT_", "NPM_CONFIG_", "PYTHON", "NODE_", "PERL", "RUBY", "LC_", "ATHANOR", "CLAUDE", "ANTHROPIC", "XDG_", "SUDO"}
	reCibleTache      = regexp.MustCompile(`^todo-(T[0-9]{1,5})$`)
	reLienExterne     = regexp.MustCompile("^https?://[^\\s\"'<>\\\\^`{}|]+$")
)

const tailleTachesMax = 2 << 20

// TacheUtilisateur: a task as Claude wrote it (system/taches.py).
type TacheUtilisateur struct {
	ID         string          `json:"id"`
	Titre      string          `json:"titre"`
	Details    string          `json:"details"`
	Attendu    string          `json:"attendu"`
	Secrets    []string        `json:"secrets"`
	Jalons     []string        `json:"jalons"`
	Statut     string          `json:"statut"` // ouverte | manque | verifiee | resolue
	Tour       int             `json:"tour"`   // the user's round Claude last judged
	Note       string          `json:"note"`
	Cree       string          `json:"cree"`
	Maj        string          `json:"maj"`
	Historique []JugementTache `json:"historique"`
}

type JugementTache struct {
	Tour   int    `json:"tour"`
	Statut string `json:"statut"`
	Note   string `json:"note"`
	Le     string `json:"le"`
}

// ReponseTache: one answer of the user.
type ReponseTache struct {
	Tour     int      `json:"tour"`
	Action   string   `json:"action"` // fait | passer
	Texte    string   `json:"texte"`
	Fichiers []string `json:"fichiers"`
	Secrets  []string `json:"secrets"` // names only: the values are in .athanor/secrets.env
	Le       string   `json:"le"`
}

// TacheUtilVue: a task with its answers and its state, for the interface.
type TacheUtilVue struct {
	TacheUtilisateur
	Etat     string          `json:"etat"` // pour_vous | a_verifier | a_contourner | close
	Reponses []ReponseTache  `json:"reponses"`
	Donnes   map[string]bool `json:"donnes"` // secrets already given
}

func etatTache(t TacheUtilisateur, rep []ReponseTache) string {
	n := len(rep)
	if (t.Statut == "verifiee" || t.Statut == "resolue") && t.Tour >= n {
		return "close"
	}
	if n > t.Tour {
		if rep[n-1].Action == "passer" {
			return "a_contourner"
		}
		return "a_verifier"
	}
	return "pour_vous"
}

func cheminPourVous(p Projet, nom string) string { return filepath.Join(p.Atelier(), "for-you", nom) }
func cheminSecrets(p Projet) string              { return filepath.Join(p.Atelier(), "secrets.env") }

// ----------------------------------------------------------------------------- the workshop's files
// Claude writes in the workshop at the same time as the application: every access goes through the
// workshop opened as a root (os.Root: no path inside it leads out, links included), files are read only
// if regular (never a link, never a pipe that would block), and written to a temporary file then renamed
// (a folder swapped for a link meanwhile makes the rename fail: nothing lands outside).

func ouvrirAtelier(p Projet) (*os.Root, error) {
	d := p.Atelier()
	fi, err := os.Lstat(d)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errLienAtelier
	}
	r, err := os.OpenRoot(d)
	if err != nil {
		return nil, err
	}
	if ri, err := r.Stat("."); err != nil || !os.SameFile(fi, ri) {
		r.Close()
		return nil, errLienAtelier
	}
	return r, nil
}

// lireAtelier reads .athanor/<rel> (rel with "/").
func lireAtelier(p Projet, rel string, max int64) ([]byte, error) {
	r, err := ouvrirAtelier(p)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if fi, err := r.Lstat(rel); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, errLienAtelier
	}
	f, err := r.OpenFile(rel, os.O_RDONLY|oSansLien|oNonBloquant, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, errLienAtelier
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		err = errors.New("file too large")
	}
	return b, err
}

// ecrireAtelier writes .athanor/<dossier>/<nom> (dossier "" or one level, created if needed).
func ecrireAtelier(p Projet, dossier, nom string, donnees []byte) error {
	r, err := ouvrirAtelier(p)
	if err != nil {
		return err
	}
	defer r.Close()
	rel := nom
	if dossier != "" {
		if err := r.Mkdir(dossier, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		if fi, err := r.Lstat(dossier); err != nil || !fi.IsDir() {
			return errLienAtelier
		}
		rel = dossier + "/" + nom
	}
	tmp := rel + "." + aleatoire(6) + ".tmp"
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|oSansLien, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(donnees)
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		base := p.Atelier()
		err = os.Rename(filepath.Join(base, filepath.FromSlash(tmp)), filepath.Join(base, filepath.FromSlash(rel)))
	}
	if err != nil {
		_ = r.Remove(tmp)
	}
	return err
}

// The files are read the same way here and by Claude's command (system/taches.py): a field of the wrong
// type takes its empty value, an entry that is not an object is skipped, the first task of an identifier wins.
func champTexte(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func champEntier(m map[string]any, k string) int {
	if n, ok := m[k].(json.Number); ok && reEntier.MatchString(string(n)) && len(n) < 10 {
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func champTextes(m map[string]any, k string) []string {
	l := []string{}
	if v, ok := m[k].([]any); ok {
		for _, x := range v {
			if s, ok := x.(string); ok {
				l = append(l, s)
			}
		}
	}
	return l
}

func decoderJSON(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(strings.TrimPrefix(string(b), "\xef\xbb\xbf")))
	d.UseNumber()
	return d.Decode(v)
}

func lireTaches(p Projet) []TacheUtilisateur {
	b, err := lireAtelier(p, "for-you/tasks.json", tailleTachesMax)
	if err != nil {
		return nil
	}
	var f map[string]any
	if decoderJSON(b, &f) != nil {
		return nil
	}
	brut, _ := f["taches"].([]any)
	l, vus := []TacheUtilisateur{}, map[string]bool{}
	for _, x := range brut {
		m, ok := x.(map[string]any)
		id, _ := m["id"].(string)
		if !ok || !reIDTache.MatchString(id) || vus[id] {
			continue
		}
		vus[id] = true
		t := TacheUtilisateur{ID: id, Titre: champTexte(m, "titre"), Details: champTexte(m, "details"), Attendu: champTexte(m, "attendu"),
			Secrets: champTextes(m, "secrets"), Jalons: champTextes(m, "jalons"), Statut: champTexte(m, "statut"), Tour: champEntier(m, "tour"),
			Note: champTexte(m, "note"), Cree: champTexte(m, "cree"), Maj: champTexte(m, "maj"), Historique: []JugementTache{}}
		h, _ := m["historique"].([]any)
		for _, y := range h {
			if j, ok := y.(map[string]any); ok {
				t.Historique = append(t.Historique, JugementTache{Tour: champEntier(j, "tour"), Statut: champTexte(j, "statut"), Note: champTexte(j, "note"), Le: champTexte(j, "le")})
			}
		}
		l = append(l, t)
	}
	return l
}

func lireReponses(p Projet) map[string][]ReponseTache {
	verrouReponses.RLock()
	defer verrouReponses.RUnlock()
	return lireReponsesSansVerrou(p)
}

func lireReponsesSansVerrou(p Projet) map[string][]ReponseTache {
	r := map[string][]ReponseTache{}
	b, err := lireAtelier(p, "for-you/replies.json", tailleTachesMax)
	if err != nil {
		return r
	}
	var f map[string]any
	if decoderJSON(b, &f) != nil {
		return r
	}
	for id, v := range f {
		l, ok := v.([]any)
		if !ok {
			continue
		}
		rep := []ReponseTache{}
		for _, x := range l {
			if m, ok := x.(map[string]any); ok {
				rep = append(rep, ReponseTache{Tour: len(rep) + 1, Action: champTexte(m, "action"), Texte: champTexte(m, "texte"),
					Fichiers: champTextes(m, "fichiers"), Secrets: champTextes(m, "secrets"), Le: champTexte(m, "le")})
			}
		}
		r[id] = rep
	}
	return r
}

// nomSecretValide: what a value may be called (secrets.env is sourced with `set -a`: never a name that
// changes how commands run). Same rule as system/taches.py.
func nomSecretValide(n string) bool {
	if !reNomSecret.MatchString(n) || secretsInterdits[n] {
		return false
	}
	for _, x := range prefixesInterdits {
		if strings.HasPrefix(n, x) {
			return false
		}
	}
	return true
}

// secretsDonnes: the names present in .athanor/secrets.env (never the values).
func secretsDonnes(p Projet) map[string]bool {
	d := map[string]bool{}
	b, err := lireAtelier(p, "secrets.env", tailleTachesMax)
	if err != nil {
		return d
	}
	for _, l := range strings.Split(string(b), "\n") {
		if m := reLigneSecret.FindStringSubmatch(l); m != nil {
			d[m[1]] = true
		}
	}
	return d
}

func (p Projet) TachesVues() []TacheUtilVue {
	rep := lireReponses(p)
	donnes := secretsDonnes(p)
	l := []TacheUtilVue{}
	for _, t := range lireTaches(p) {
		r := rep[t.ID]
		if r == nil {
			r = []ReponseTache{}
		}
		d := map[string]bool{}
		for _, n := range t.Secrets {
			d[n] = donnes[n]
		}
		l = append(l, TacheUtilVue{TacheUtilisateur: t, Etat: etatTache(t, r), Reponses: r, Donnes: d})
	}
	// waiting for the user first, then waiting for Claude, then closed (most recent first)
	rang := map[string]int{"pour_vous": 0, "a_verifier": 1, "a_contourner": 1, "close": 2}
	sort.SliceStable(l, func(i, j int) bool {
		if rang[l[i].Etat] != rang[l[j].Etat] {
			return rang[l[i].Etat] < rang[l[j].Etat]
		}
		if l[i].Etat == "close" {
			return l[i].Maj > l[j].Maj
		}
		return false
	})
	return l
}

// PourVous: the number of tasks waiting for the user (badge of the menu).
func (p Projet) PourVous() int {
	rep := lireReponses(p)
	n := 0
	for _, t := range lireTaches(p) {
		if etatTache(t, rep[t.ID]) == "pour_vous" {
			n++
		}
	}
	return n
}

func (s *Serveur) apiPourVousVoir(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	s.ecrireJSON(w, map[string]any{"taches": p.TachesVues()})
}

// ecrireSecrets sets NAME='value' lines in .athanor/secrets.env (shell-quoted: the file is sourced); only
// well-formed lines are kept. Returns the previous content, to put it back if the answer cannot be saved.
func ecrireSecrets(p Projet, valeurs map[string]string) ([]byte, error) {
	if len(valeurs) == 0 {
		return nil, nil
	}
	avant, err := lireAtelier(p, "secrets.env", tailleTachesMax)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lignes := []string{"# Values you gave Claude on the For you page (sourced by its commands, never committed)."}
	for _, l := range strings.Split(string(avant), "\n") {
		if m := reLigneSecret.FindStringSubmatch(l); m != nil {
			if _, remplace := valeurs[m[1]]; !remplace {
				lignes = append(lignes, l)
			}
		}
	}
	noms := make([]string, 0, len(valeurs))
	for n := range valeurs {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	for _, n := range noms {
		lignes = append(lignes, n+"="+citerShell(valeurs[n]))
	}
	return avant, ecrireAtelier(p, "", "secrets.env", []byte(strings.Join(lignes, "\n")+"\n"))
}

func (s *Serveur) apiPourVous(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	var c struct {
		ID       string            `json:"id"`
		Action   string            `json:"action"` // fait | passer
		Texte    string            `json:"texte"`
		Fichiers []string          `json:"fichiers"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDTache.MatchString(c.ID) || (c.Action != "fait" && c.Action != "passer") {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	c.Texte = strings.TrimSpace(strings.ReplaceAll(c.Texte, "\r\n", "\n"))
	if utf8.RuneCountInString(c.Texte) > 4000 {
		s.erreur(w, http.StatusBadRequest, "Your answer is too long (4,000 characters at most).")
		return
	}
	verrouReponses.Lock()
	defer verrouReponses.Unlock()
	var tache *TacheUtilisateur
	for _, t := range lireTaches(p) {
		if t.ID == c.ID && tache == nil {
			t := t
			tache = &t
		}
	}
	rep := lireReponsesSansVerrou(p)
	if tache == nil || etatTache(*tache, rep[c.ID]) != "pour_vous" {
		s.erreur(w, http.StatusConflict, "This task is no longer waiting for you: refresh.")
		return
	}
	// secret values: only the names the task asks for, one line each
	valeurs := map[string]string{}
	demandes := map[string]bool{}
	for _, n := range tache.Secrets {
		demandes[n] = true
	}
	for n, v := range c.Secrets {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !demandes[n] || !nomSecretValide(n) || strings.ContainsAny(v, "\r\n\x00") || utf8.RuneCountInString(v) > 4000 {
			s.erreur(w, http.StatusBadRequest, "Invalid secret value.")
			return
		}
		valeurs[n] = v
	}
	if c.Action == "passer" {
		valeurs = map[string]string{}
	}
	// files: only those uploaded for this task (api_pieces.go, target todo-ID)
	prefixe := ".athanor/attachments/for-you-" + c.ID + "/"
	fichiers := []string{}
	for _, f := range c.Fichiers {
		nom := strings.TrimPrefix(f, prefixe)
		if !strings.HasPrefix(f, prefixe) || nom != nomPiece(nom) {
			s.erreur(w, http.StatusBadRequest, "Invalid request")
			return
		}
		if fi, err := os.Lstat(filepath.Join(p.Atelier(), "attachments", "for-you-"+c.ID, nom)); err != nil || !fi.Mode().IsRegular() {
			s.erreur(w, http.StatusBadRequest, "An attached file is missing: add it again.")
			return
		}
		fichiers = append(fichiers, f)
	}
	secretsAvant, err := ecrireSecrets(p, valeurs)
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	noms := []string{}
	for n := range valeurs {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	rep[c.ID] = append(rep[c.ID], ReponseTache{Tour: len(rep[c.ID]) + 1, Action: c.Action, Texte: c.Texte, Fichiers: fichiers, Secrets: noms, Le: time.Now().Format("2006-01-02T15:04:05")})
	b, _ := json.MarshalIndent(rep, "", "  ")
	if err := ecrireAtelier(p, "for-you", "replies.json", b); err != nil {
		if len(valeurs) > 0 { // the answer is not saved: the values it brought go too
			if len(secretsAvant) > 0 {
				_ = ecrireAtelier(p, "", "secrets.env", secretsAvant)
			} else if r, e := ouvrirAtelier(p); e == nil {
				_ = r.Remove("secrets.env")
				r.Close()
			}
		}
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Action == "passer" {
		s.msg(w, "Skipped: Claude looks for another way at the start of its next cycle.")
	} else {
		s.msg(w, "Sent: Claude checks it at the start of its next cycle and tells you here.")
	}
}

// apiLien opens a link of Claude's texts (a sign-up page, a dashboard...) in the default browser.
func (s *Serveur) apiLien(w http.ResponseWriter, r *http.Request) {
	var c struct {
		URL string `json:"url"`
	}
	if err := lireCorps(r, &c); err != nil || len(c.URL) > 2000 || !reLienExterne.MatchString(c.URL) {
		s.erreur(w, http.StatusBadRequest, "Invalid link.")
		return
	}
	if err := ouvrirLien(c.URL); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]bool{"ok": true})
}

// ----------------------------------------------------------------------------- notifications
// The tasks are watched in the background: a new task for the user (or one Claude sends back) and a
// task Claude verified show a system notification, even when Athanor's window is closed.

var texteNotif = map[string]map[string]string{
	"en": {"besoin": "Claude needs you", "encore": "Claude still needs you", "verifie": "Task verified ✓"},
	"fr": {"besoin": "Claude a besoin de vous", "encore": "Claude a encore besoin de vous", "verifie": "Tâche vérifiée ✓"},
	"es": {"besoin": "Claude le necesita", "encore": "Claude todavía le necesita", "verifie": "Tarea verificada ✓"},
	"de": {"besoin": "Claude braucht Sie", "encore": "Claude braucht Sie noch", "verifie": "Aufgabe geprüft ✓"},
	"it": {"besoin": "Claude ha bisogno di te", "encore": "Claude ha ancora bisogno di te", "verifie": "Compito verificato ✓"},
	"pt": {"besoin": "O Claude precisa de si", "encore": "O Claude ainda precisa de si", "verifie": "Tarefa verificada ✓"},
}

func (s *Serveur) surveillerTaches() {
	vus := map[string]bool{}
	premier := true
	for {
		s.mu.Lock()
		projets := append([]Projet{}, s.config.Projets...)
		s.mu.Unlock()
		for _, p := range projets {
			if p.Archive {
				continue
			}
			for _, t := range p.TachesVues() {
				var titre string
				switch {
				case t.Etat == "pour_vous" && t.Statut == "manque":
					titre = "encore"
				case t.Etat == "pour_vous":
					titre = "besoin"
				case t.Etat == "close" && t.Statut == "verifiee":
					titre = "verifie"
				default:
					continue
				}
				cle := fmt.Sprintf("%s|%s|%s|%d", p.Dossier, t.ID, t.Statut, t.Tour)
				if vus[cle] {
					continue
				}
				vus[cle] = true
				if !premier {
					l := texteNotif[s.langue()]
					if l == nil {
						l = texteNotif["en"]
					}
					notifier(l[titre]+" · "+p.Nom, t.Titre)
				}
			}
		}
		premier = false
		time.Sleep(15 * time.Second)
	}
}

// notifier shows a system notification (best effort, nothing if it fails).
func notifier(titre, texte string) {
	switch runtime.GOOS {
	case "windows":
		// a toast through PowerShell, under PowerShell's own application identity (always registered)
		x := func(s string) string {
			return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
		}
		xml := `<toast><visual><binding template="ToastGeneric"><text>` + x(titre) + `</text><text>` + x(texte) + `</text></binding></visual></toast>`
		script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null
$d = New-Object Windows.Data.Xml.Dom.XmlDocument
$d.LoadXml($env:ATHANOR_TOAST)
$n = [Windows.UI.Notifications.ToastNotification]::new($d)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show($n)`
		c := commande("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
		c.Env = append(os.Environ(), "ATHANOR_TOAST="+xml)
		cacherFenetre(c)
		_ = c.Start()
		go func() { _ = c.Wait() }()
	case "darwin":
		c := commande("osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", titre, texte)
		_ = c.Start()
		go func() { _ = c.Wait() }()
	default:
		if chemin, err := exec.LookPath("notify-send"); err == nil {
			c := commande(chemin, "-a", "Athanor", titre, texte)
			_ = c.Start()
			go func() { _ = c.Wait() }()
		}
	}
}
