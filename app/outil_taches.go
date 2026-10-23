package main

// athanor todo ...: the tasks for the user, what Claude cannot do itself (replaces system/taches.py, same
// files, same messages); athanor etat-taches PROJECT: their count for the pilot (taches.py etat).
// .athanor/for-you/tasks.json is Claude's (written here only), replies.json and secrets.env the app's.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTitre, maxDetails, maxAttendu, maxNote = 150, 8000, 1000, 2000
	maxOuvertes, maxTotal, maxFichierTaches   = 30, 200, 2 << 20
)

// reJalon: a milestone's identifier as in the plan (the names and secrets rules are api_pourvous.go's,
// the same as taches.py's).
var reJalon = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,19}$`)

func maintenantTache() string { return time.Now().Format("2006-01-02T15:04:05") }

type erreurTache string

func (e erreurTache) Error() string { return string(e) }

func estLien(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

// dossierAtelierSur: PROJECT/.athanor/... never through a link (the workshop is Claude's).
func dossierAtelierSur(projet string, parties ...string) (string, error) {
	d := filepath.Join(projet, ".athanor")
	if st, err := os.Stat(d); estLien(d) || err != nil || !st.IsDir() {
		return "", erreurTache("no workshop (.athanor/) in " + projet)
	}
	for _, p := range parties {
		d = filepath.Join(d, p)
		if estLien(d) {
			return "", erreurTache(d + " is a link: nothing written")
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			if err := os.Mkdir(d, 0o777); err != nil {
				return "", err
			}
		}
	}
	return d, nil
}

// lireOctets: a regular file only, never through a link, never blocking on a pipe put in its place.
func lireOctets(chemin string, max int) ([]byte, error) {
	f, err := ouvrirSansLien(chemin, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errors.New("file too large")
	}
	return b, nil
}

// lireDictJSON: a JSON object file; empty when missing or not an object; read again while it is being
// rewritten (3 times).
func lireDictJSON(chemin string) map[string]any {
	for essai := 0; essai < 3; essai++ {
		b, err := lireOctets(chemin, maxFichierTaches)
		if err != nil {
			return map[string]any{}
		}
		b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
		var v any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if utf8.Valid(b) && d.Decode(&v) == nil && !d.More() {
			if m, ok := v.(map[string]any); ok {
				return m
			}
			return map[string]any{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return map[string]any{}
}

func ecrireJSONTaches(chemin string, v any) error {
	t := fmt.Sprintf("%s.%d.tmp", chemin, os.Getpid())
	f, err := ouvrirSansLien(t, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(jsonPython(v, "  ", false))
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(t, chemin)
}

func texteJSON(v any) string { s, _ := v.(string); return s }

func entierTache(v any) int64 {
	if n, ok := entierJSON(v); ok && n >= 0 {
		return n
	}
	return 0
}

func textesJSON(v any) []string {
	out := []string{}
	for _, x := range liste(v) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

type jugement struct {
	tour             int64
	statut, note, le string
}

type tacheUtil struct {
	id, titre, details, attendu string
	secrets, jalons             []string
	statut                      string
	tour                        int64
	note, cree, maj             string
	historique                  []jugement
	sansHistorique              bool // a task just added has no "historique" yet (as taches.py writes it)
}

func (t *tacheUtil) objet() objetJSON {
	var o objetJSON
	o.mettre("id", t.id)
	o.mettre("titre", t.titre)
	o.mettre("details", t.details)
	o.mettre("attendu", t.attendu)
	o.mettre("secrets", t.secrets)
	o.mettre("jalons", t.jalons)
	o.mettre("statut", t.statut)
	o.mettre("tour", t.tour)
	o.mettre("note", t.note)
	o.mettre("cree", t.cree)
	o.mettre("maj", t.maj)
	if !t.sansHistorique {
		h := []objetJSON{}
		for _, j := range t.historique {
			var x objetJSON
			x.mettre("tour", j.tour)
			x.mettre("statut", j.statut)
			x.mettre("note", j.note)
			x.mettre("le", j.le)
			h = append(h, x)
		}
		o.mettre("historique", h)
	}
	return o
}

type fichierTaches struct {
	suivant int64
	taches  []*tacheUtil
}

func (f *fichierTaches) objet() objetJSON {
	var o objetJSON
	o.mettre("suivant", f.suivant)
	l := []objetJSON{}
	for _, t := range f.taches {
		l = append(l, t.objet())
	}
	o.mettre("taches", l)
	return o
}

func lireTachesUtil(projet string) *fichierTaches {
	d := lireDictJSON(filepath.Join(projet, ".athanor", "for-you", "tasks.json"))
	f := &fichierTaches{}
	vus := map[string]bool{}
	for _, x := range liste(d["taches"]) {
		t := dict(x)
		id, ok := t["id"].(string)
		if t == nil || !ok || !reIDTache.MatchString(id) || vus[id] {
			continue
		}
		vus[id] = true
		n := &tacheUtil{id: id, titre: texteJSON(t["titre"]), details: texteJSON(t["details"]), attendu: texteJSON(t["attendu"]),
			secrets: textesJSON(t["secrets"]), jalons: textesJSON(t["jalons"]), statut: texteJSON(t["statut"]), tour: entierTache(t["tour"]),
			note: texteJSON(t["note"]), cree: texteJSON(t["cree"]), maj: texteJSON(t["maj"]), historique: []jugement{}}
		for _, y := range liste(t["historique"]) {
			if j := dict(y); j != nil {
				n.historique = append(n.historique, jugement{entierTache(j["tour"]), texteJSON(j["statut"]), texteJSON(j["note"]), texteJSON(j["le"])})
			}
		}
		f.taches = append(f.taches, n)
	}
	f.suivant = entierTache(d["suivant"])
	if f.suivant == 0 {
		f.suivant = int64(len(f.taches) + 1)
	}
	return f
}

type reponseUtil struct {
	action, texte     string
	fichiers, secrets []string
	le                string
}

func lireReponsesUtil(projet string) map[string][]reponseUtil {
	d := lireDictJSON(filepath.Join(projet, ".athanor", "for-you", "replies.json"))
	out := map[string][]reponseUtil{}
	for k, v := range d {
		l, ok := v.([]any)
		if !ok {
			continue
		}
		r := []reponseUtil{}
		for _, x := range l {
			if m := dict(x); m != nil {
				r = append(r, reponseUtil{texteJSON(m["action"]), texteJSON(m["texte"]), textesJSON(m["fichiers"]), textesJSON(m["secrets"]), texteJSON(m["le"])})
			}
		}
		out[k] = r
	}
	return out
}

func etatTacheUtil(t *tacheUtil, r []reponseUtil) string {
	n := int64(len(r))
	if (t.statut == "verifiee" || t.statut == "resolue") && t.tour >= n {
		return "close"
	}
	if n > t.tour {
		if r[len(r)-1].action == "passer" {
			return "a_contourner"
		}
		return "a_verifier"
	}
	return "pour_vous"
}

func secretsPresents(projet string) map[string]bool {
	noms := map[string]bool{}
	b, err := lireOctets(filepath.Join(projet, ".athanor", "secrets.env"), maxFichierTaches)
	if err != nil {
		return noms
	}
	for _, l := range strings.Split(strings.ToValidUTF8(string(b), "\uFFFD"), "\n") {
		if m := reLigneSecret.FindStringSubmatch(l); m != nil {
			noms[m[1]] = true
		}
	}
	return noms
}

func reponsesVues(projet string) map[string]int64 {
	out := map[string]int64{}
	for k, v := range lireDictJSON(filepath.Join(projet, ".athanor", "for-you", "seen.json")) {
		out[k] = entierTache(v)
	}
	return out
}

// courtPython: s stripped, cut to n characters (the last one an ellipsis).
func courtPython(s string, n int) string {
	s = stripPython(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------------------- Claude's commands

func verrouTaches(projet string) (string, func(), error) {
	d, err := dossierAtelierSur(projet, "for-you")
	if err != nil {
		return "", nil, err
	}
	f, err := ouvrirSansLien(filepath.Join(d, ".lock"), os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return "", nil, err
	}
	if err := verrouillerFd(f); err != nil {
		f.Close()
		return "", nil, err
	}
	return d, func() { f.Close() }, nil
}

func lireEntreeTache() string {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(os.Stdin, int64(maxDetails+1)*4))
	s := strings.ToValidUTF8(string(b), "\uFFFD")
	if r := []rune(s); len(r) > maxDetails+1 {
		s = string(r[:maxDetails+1])
	}
	return s
}

type optionsTache struct {
	besoin          *string
	secrets, jalons []string
}

func optionsTodo(args []string) ([]string, optionsTache) {
	var pos []string
	o := optionsTache{secrets: []string{}, jalons: []string{}}
	for i := 0; i < len(args); {
		a := args[i]
		if (a == "--need" || a == "--secret" || a == "--milestone") && i+1 < len(args) {
			switch a {
			case "--need":
				v := args[i+1]
				o.besoin = &v
			default:
				for _, x := range strings.Split(args[i+1], ",") {
					if x = stripPython(x); x != "" {
						if a == "--secret" {
							o.secrets = append(o.secrets, x)
						} else {
							o.jalons = append(o.jalons, x)
						}
					}
				}
			}
			i += 2
		} else {
			pos = append(pos, a)
			i++
		}
	}
	return pos, o
}

func validerChamps(o optionsTache) error {
	for _, s := range o.secrets {
		if !nomSecretValide(s) {
			return erreurTache("secret name " + reprPython(s) + ": capital letters, digits and _ (e.g. SHOPIFY_ADMIN_TOKEN), not a name the shell or a tool already uses (PATH, LD_…, GIT_…)")
		}
	}
	for _, j := range o.jalons {
		if !reJalon.MatchString(j) {
			return erreurTache("milestone " + reprPython(j) + ": its identifier as in the plan (e.g. M3)")
		}
	}
	if o.besoin != nil && utf8.RuneCountInString(*o.besoin) > maxAttendu {
		return erreurTache(fmt.Sprintf("--need: %d characters at most", maxAttendu))
	}
	return nil
}

func triSansDoublon(l []string) []string {
	vu := map[string]bool{}
	out := []string{}
	for _, x := range l {
		if !vu[x] {
			vu[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func todoAjouter(projet string, args []string) (string, error) {
	pos, o := optionsTodo(args)
	titre := stripPython(strings.Join(pos, " "))
	if titre == "" || utf8.RuneCountInString(titre) > maxTitre || strings.Contains(titre, "\n") {
		return "", erreurTache(fmt.Sprintf("a title on one line, %d characters at most", maxTitre))
	}
	if err := validerChamps(o); err != nil {
		return "", err
	}
	details := lireEntreeTache()
	if utf8.RuneCountInString(details) > maxDetails {
		return "", erreurTache(fmt.Sprintf("details: %d characters at most", maxDetails))
	}
	d, liberer, err := verrouTaches(projet)
	if err != nil {
		return "", err
	}
	defer liberer()
	f, rep := lireTachesUtil(projet), lireReponsesUtil(projet)
	for _, t := range f.taches {
		if e := etatTacheUtil(t, rep[t.id]); strings.ToLower(stripPython(t.titre)) == strings.ToLower(titre) && e != "close" {
			return fmt.Sprintf("%s already asks this (%s): use `athanor todo edit %s` to change it.", t.id, e, t.id), nil
		}
	}
	ouvertes := 0
	for _, t := range f.taches {
		if etatTacheUtil(t, rep[t.id]) != "close" {
			ouvertes++
		}
	}
	if ouvertes >= maxOuvertes {
		return "", erreurTache(fmt.Sprintf("already %d tasks open for the user: close or merge some first", maxOuvertes))
	}
	n := max(f.suivant, 1)
	pris := map[string]bool{}
	for _, t := range f.taches {
		pris[t.id] = true
	}
	for k := range rep {
		pris[k] = true
	}
	for pris["T"+strconv.FormatInt(n, 10)] {
		n++
	}
	if n > 99999 {
		return "", erreurTache("too many tasks in this project")
	}
	besoin := ""
	if o.besoin != nil {
		besoin = *o.besoin
	}
	m := maintenantTache()
	t := &tacheUtil{id: "T" + strconv.FormatInt(n, 10), titre: titre, details: stripPython(details), attendu: stripPython(besoin),
		secrets: triSansDoublon(o.secrets), jalons: o.jalons, statut: "ouverte", cree: m, maj: m, sansHistorique: true}
	f.taches = append(f.taches, t)
	f.suivant = n + 1
	for len(f.taches) > maxTotal { // the oldest closed tasks go when the list is too long
		i := -1
		for k, x := range f.taches {
			if etatTacheUtil(x, rep[x.id]) == "close" {
				i = k
				break
			}
		}
		if i < 0 {
			break
		}
		f.taches = append(f.taches[:i], f.taches[i+1:]...)
	}
	if err := ecrireJSONTaches(filepath.Join(d, "tasks.json"), f.objet()); err != nil {
		return "", err
	}
	return t.id + " created: the user sees it in the application (page For you). Go on with what does not need it; if nothing else can be done, run `athanor todo wait`.", nil
}

func trouverTache(f *fichierTaches, id string) (*tacheUtil, error) {
	for _, t := range f.taches {
		if t.id == strings.ToUpper(id) {
			return t, nil
		}
	}
	return nil, erreurTache("no task " + id + " (athanor todo list --all)")
}

func todoModifier(projet string, args []string) (string, error) {
	pos, o := optionsTodo(args)
	if len(pos) != 1 {
		return "", erreurTache("usage: athanor todo edit ID [--need ...] [--secret NAME]... < new details")
	}
	if err := validerChamps(o); err != nil {
		return "", err
	}
	details := lireEntreeTache()
	if utf8.RuneCountInString(details) > maxDetails {
		return "", erreurTache(fmt.Sprintf("details: %d characters at most", maxDetails))
	}
	d, liberer, err := verrouTaches(projet)
	if err != nil {
		return "", err
	}
	defer liberer()
	f := lireTachesUtil(projet)
	t, err := trouverTache(f, pos[0])
	if err != nil {
		return "", err
	}
	if s := stripPython(details); s != "" {
		t.details = s
	}
	if o.besoin != nil {
		t.attendu = stripPython(*o.besoin)
	}
	if len(o.secrets) > 0 {
		t.secrets = triSansDoublon(append(append([]string(nil), t.secrets...), o.secrets...))
	}
	if len(o.jalons) > 0 {
		t.jalons = o.jalons
	}
	t.maj = maintenantTache()
	if err := ecrireJSONTaches(filepath.Join(d, "tasks.json"), f.objet()); err != nil {
		return "", err
	}
	return t.id + " updated.", nil
}

func todoJuger(projet string, args []string, statut string) (string, error) {
	nom := map[string]string{"verifiee": "ok", "manque": "missing", "resolue": "resolved"}[statut]
	if len(args) < 2 || stripPython(strings.Join(args[1:], " ")) == "" {
		return "", erreurTache(`usage: athanor todo ` + nom + ` ID "message for the user"`)
	}
	note := stripPython(strings.Join(args[1:], " "))
	if utf8.RuneCountInString(note) > maxNote {
		return "", erreurTache(fmt.Sprintf("message: %d characters at most", maxNote))
	}
	d, liberer, err := verrouTaches(projet)
	if err != nil {
		return "", err
	}
	defer liberer()
	f := lireTachesUtil(projet)
	t, err := trouverTache(f, args[0])
	if err != nil {
		return "", err
	}
	// the rounds Claude has read (athanor todo list): an answer sent since then stays to be handled
	tour := min(int64(len(lireReponsesUtil(projet)[t.id])), reponsesVues(projet)[t.id])
	if statut == "verifiee" && tour == 0 {
		statut = "resolue" // nothing answered by the user: closed all the same
	}
	m := maintenantTache()
	t.statut, t.tour, t.note, t.maj = statut, tour, note, m
	t.historique = append(t.historique, jugement{tour, statut, note, maintenantTache()})
	if len(t.historique) > 20 {
		t.historique = t.historique[len(t.historique)-20:]
	}
	if err := ecrireJSONTaches(filepath.Join(d, "tasks.json"), f.objet()); err != nil {
		return "", err
	}
	return fmt.Sprintf(map[string]string{"verifiee": "%s closed as verified: the user is told.", "resolue": "%s closed: the user is told.",
		"manque": "%s back with the user, with your message."}[statut], t.id), nil
}

func todoAttendre(projet string) (string, error) {
	f, rep := lireTachesUtil(projet), lireReponsesUtil(projet)
	pour := false
	for _, t := range f.taches {
		switch etatTacheUtil(t, rep[t.id]) {
		case "a_verifier", "a_contourner":
			return "", erreurTache("the user answered tasks: handle them first (athanor todo list)")
		case "pour_vous":
			pour = true
		}
	}
	if !pour {
		return "", erreurTache("no task waits for the user: carry on with the plan")
	}
	d, err := dossierAtelierSur(projet)
	if err != nil {
		return "", err
	}
	t := filepath.Join(d, fmt.Sprintf(".WAITING_FOR_YOU.%d", os.Getpid()))
	if err := os.WriteFile(t, []byte(maintenantTache()+"\n"), 0o666); err != nil {
		return "", err
	}
	if err := os.Rename(t, filepath.Join(d, "WAITING_FOR_YOU")); err != nil {
		return "", err
	}
	return "The pilot pauses after this cycle and resumes as soon as the user answers. End the cycle now (commit what is done, note it in .athanor/LOG.md).", nil
}

var etiquettesTaches = map[string]string{"pour_vous": "waits for the user", "a_verifier": "THE USER SAYS IT IS DONE: check it now",
	"a_contourner": "THE USER SKIPPED IT: find another way without it", "close": "closed"}

func todoLister(projet string, args []string) string {
	f, rep := lireTachesUtil(projet), lireReponsesUtil(projet)
	if d, liberer, err := verrouTaches(projet); err == nil { // what Claude has now read (reponsesVues)
		var vus objetJSON
		vus.valeur = map[string]json.RawMessage{}
		for _, t := range f.taches {
			vus.mettre(t.id, len(rep[t.id]))
		}
		_ = ecrireJSONTaches(filepath.Join(d, "seen.json"), vus)
		liberer()
	}
	tout := false
	for _, a := range args {
		tout = tout || a == "--all"
	}
	presents := secretsPresents(projet)
	var lignes []string
	for _, t := range f.taches {
		r := rep[t.id]
		e := etatTacheUtil(t, r)
		if e == "close" && !tout {
			continue
		}
		lignes = append(lignes, fmt.Sprintf("## %s · %s [%s]", t.id, t.titre, etiquettesTaches[e]))
		if len(t.jalons) > 0 {
			lignes = append(lignes, "Blocks: "+strings.Join(t.jalons, ", "))
		}
		if t.details != "" && e != "close" {
			lignes = append(lignes, "Steps you gave: "+strings.ReplaceAll(courtPython(t.details, 600), "\n", " / "))
		}
		if t.attendu != "" {
			lignes = append(lignes, "You asked for: "+t.attendu)
		}
		if len(t.secrets) > 0 {
			var s []string
			for _, n := range t.secrets {
				etat := "not given yet"
				if presents[n] {
					etat = "given, in .athanor/secrets.env"
				}
				s = append(s, n+" ("+etat+")")
			}
			lignes = append(lignes, "Secrets: "+strings.Join(s, ", "))
		}
		if t.note != "" && e != "a_verifier" {
			lignes = append(lignes, "Your last message: "+t.note)
		}
		if len(r) > 0 {
			x := r[len(r)-1]
			action := "done"
			if x.action == "passer" {
				action = "skip, find another way"
			}
			txt := x.texte
			if txt == "" {
				txt = "(no text)"
			}
			lignes = append(lignes, fmt.Sprintf("User's answer (round %d, %s, %s): %s", len(r), action, x.le, courtPython(txt, 4000)))
			if len(x.fichiers) > 0 {
				var l []string
				for _, p := range x.fichiers {
					l = append(l, "`"+p+"`")
				}
				lignes = append(lignes, "Files: "+tronquerRunes(strings.Join(l, ", "), 3000))
			}
			if len(x.secrets) > 0 {
				lignes = append(lignes, "Secrets given: "+strings.Join(x.secrets, ", ")+" (in .athanor/secrets.env)")
			}
		}
		lignes = append(lignes, "")
	}
	if len(lignes) == 0 {
		if tout {
			return "No task for the user."
		}
		return "No task for the user open."
	}
	return rstripPython(strings.Join(lignes, "\n"))
}

// etatTachesJSON: what the pilot needs, as taches.py etat printed it.
func etatTachesJSON(projet string) string {
	f, rep := lireTachesUtil(projet), lireReponsesUtil(projet)
	n := map[string]int{}
	for _, t := range f.taches {
		n[etatTacheUtil(t, rep[t.id])]++
	}
	attente := 0
	if st, err := os.Stat(filepath.Join(projet, ".athanor", "WAITING_FOR_YOU")); err == nil && st.Mode().IsRegular() {
		attente = 1
	}
	var o objetJSON
	o.mettre("pour_vous", n["pour_vous"])
	o.mettre("a_traiter", n["a_verifier"]+n["a_contourner"])
	o.mettre("attente", attente)
	return string(jsonPython(o, "", true))
}

// projetAtelier: PROJECT as athanor.py read it (pathlib's form; native Windows: /c/x is C:/x).
func projetAtelier() string {
	p, ok := os.LookupEnv("PROJECT")
	if !ok {
		p = "/projet"
	}
	if m := regexp.MustCompile(`^/([A-Za-z])(/|$)`).FindStringSubmatch(p); m != nil && runtimeWindows() {
		p = m[1] + ":/" + p[min(3, len(p)):]
	}
	return cheminPur(p)
}

func lancerTodo(args []string) int {
	projet := projetAtelier()
	if len(args) == 0 {
		fmt.Print(docTaches + "\n")
		return 2
	}
	var sortie string
	var err error
	switch c, reste := args[0], args[1:]; c {
	case "add":
		sortie, err = todoAjouter(projet, reste)
	case "edit":
		sortie, err = todoModifier(projet, reste)
	case "list":
		sortie = todoLister(projet, reste)
	case "ok", "missing", "resolved":
		sortie, err = todoJuger(projet, reste, map[string]string{"ok": "verifiee", "missing": "manque", "resolved": "resolue"}[c])
	case "wait":
		sortie, err = todoAttendre(projet)
	default:
		fmt.Print(docTaches + "\n")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "athanor todo: "+err.Error())
		return 1
	}
	fmt.Println(sortie)
	return 0
}

func lancerEtatTaches(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: athanor etat-taches PROJECT")
		return 2
	}
	fmt.Println(etatTachesJSON(abspathOS(args[0])))
	return 0
}
