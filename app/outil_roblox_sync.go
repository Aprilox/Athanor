package main

// athanor roblox-sync serve PROJECT PIDFILE | payload PROJECT: Roblox Studio, live (replaces
// system/roblox-sync.py, same answers): the scripts of a Rojo project reach the open Studio without
// restarting it. "serve" answers on 127.0.0.1 (a free port, a random key) and writes
// PROJECT/.athanor/studio/SYNC.luau, the Luau code Claude runs in Studio to fetch them; it stops when
// PIDFILE (the pilot's) disappears or changes. The tree follows Rojo's rules
// (https://rojo.space/docs/v7/sync-details/); only scripts are sent, the other files changed since the
// previous request are listed (they need roblox-studio --rebuild). Only reads the project.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxScriptRoblox = 4 << 20

// fichierProjetRojo: default.project.json, else the first *.project.json (2 levels down at most).
func fichierProjetRojo(projet string) string {
	d := filepath.Join(projet, "default.project.json")
	if estFichier(d) {
		return d
	}
	var trouve string
	var parcourir func(racine string, profondeur int) bool
	parcourir = func(racine string, profondeur int) bool {
		es, err := os.ReadDir(racine)
		if err != nil {
			return false
		}
		var dossiers, fichiers []string
		for _, e := range es {
			n := e.Name()
			p := filepath.Join(racine, n)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dossiers = append(dossiers, n)
			} else {
				fichiers = append(fichiers, n)
			}
		}
		sort.Strings(fichiers)
		for _, f := range fichiers {
			if strings.HasSuffix(f, ".project.json") {
				trouve = filepath.Join(racine, f)
				return true
			}
		}
		if profondeur >= 2 {
			return false
		}
		sort.Strings(dossiers)
		for _, n := range dossiers {
			if strings.HasPrefix(n, ".") || n == "node_modules" || n == "build" || n == "Packages" || estLien(filepath.Join(racine, n)) {
				continue
			}
			if parcourir(filepath.Join(racine, n), profondeur+1) {
				return true
			}
		}
		return false
	}
	parcourir(projet, 0)
	return trouve
}

type genreScript struct{ classe, contexte string }

// genreDeScript: the instance name and class of a script file; ok false for anything else.
func genreDeScript(nom string, legacy bool) (string, genreScript, bool) {
	bas := strings.ToLower(nom)
	for _, ext := range []string{".luau", ".lua"} {
		if !strings.HasSuffix(bas, ext) {
			continue
		}
		r := []rune(nom)
		base := string(r[:len(r)-len(ext)])
		b := strings.ToLower(base)
		rb := []rune(base)
		switch {
		case strings.HasSuffix(b, ".server"):
			if legacy {
				return string(rb[:len(rb)-7]), genreScript{"Script", ""}, true
			}
			return string(rb[:len(rb)-7]), genreScript{"Script", "Server"}, true
		case strings.HasSuffix(b, ".client"):
			if legacy {
				return string(rb[:len(rb)-7]), genreScript{"LocalScript", ""}, true
			}
			return string(rb[:len(rb)-7]), genreScript{"Script", "Client"}, true
		case strings.HasSuffix(b, ".module"):
			return string(rb[:len(rb)-7]), genreScript{"ModuleScript", ""}, true
		case strings.Contains(base, "."): // NAME.something.luau Rojo does not know: left alone
			return "", genreScript{}, false
		}
		return base, genreScript{"ModuleScript", ""}, true
	}
	return "", genreScript{}, false
}

// fnmatchPython: fnmatch.fnmatch on a POSIX system (* also crosses "/").
func fnmatchPython(nom, motif string) bool {
	var b strings.Builder
	b.WriteString(`(?s)^`)
	r := []rune(motif)
	for i := 0; i < len(r); i++ {
		switch c := r[i]; c {
		case '*':
			if !strings.HasSuffix(b.String(), ".*") {
				b.WriteString(".*")
			}
		case '?':
			b.WriteString(".")
		case '[':
			j := i + 1
			if j < len(r) && r[j] == '!' {
				j++
			}
			if j < len(r) && r[j] == ']' {
				j++
			}
			for j < len(r) && r[j] != ']' {
				j++
			}
			if j >= len(r) {
				b.WriteString(`\[`)
				continue
			}
			contenu := string(r[i+1 : j])
			i = j
			contenu = strings.ReplaceAll(contenu, `\`, `\\`)
			switch {
			case strings.HasPrefix(contenu, "!"):
				contenu = "^" + contenu[1:]
			case strings.HasPrefix(contenu, "^") || strings.HasPrefix(contenu, "["):
				contenu = `\` + contenu
			}
			b.WriteString("[" + contenu + "]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(nom)
}

type noeudRoblox struct {
	chemin []string
	classe string
}

type scriptRoblox struct {
	chemin                   []string
	classe, contexte, source string
}

type arbreRojo struct {
	projet, fichier string
	legacy          bool
	motifs          []string
	noeuds          []noeudRoblox
	scripts         []scriptRoblox
	geres           [][]string
	autres          []string
}

func copieChemin(c []string, plus ...string) []string {
	return append(append([]string{}, c...), plus...)
}

func (a *arbreRojo) ignore(chemin string) bool {
	rel, err := filepath.Rel(filepath.Dir(a.fichier), chemin)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, m := range a.motifs {
		if fnmatchPython(rel, m) {
			return true
		}
	}
	return false
}

func (a *arbreRojo) lire() error {
	b, err := os.ReadFile(a.fichier)
	if err != nil {
		return err
	}
	var p objetJSON
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &p); err != nil {
		return err
	}
	a.legacy = true
	if v, ok := p.brut("emitLegacyScripts").(bool); ok && !v {
		a.legacy = false
	}
	for _, m := range liste(p.brut("globIgnorePaths")) {
		if s, ok := m.(string); ok {
			a.motifs = append(a.motifs, s)
		}
	}
	if vraiPython(p.brut("tree")) {
		a.noeud(json.RawMessage(p.valeur["tree"]), nil)
	}
	a.autres = append(a.autres, a.fichier)
	return nil
}

func (a *arbreRojo) noeud(brut json.RawMessage, chemin []string) {
	if t := bytes.TrimSpace(brut); len(t) == 0 || t[0] != '{' {
		return
	}
	var n objetJSON
	if json.Unmarshal(brut, &n) != nil {
		return
	}
	if len(chemin) > 0 {
		classe, _ := n.brut("$className").(string)
		a.noeuds = append(a.noeuds, noeudRoblox{chemin, classe})
	}
	if p, ok := n.brut("$path").(string); ok && len(chemin) > 0 {
		cible := p
		if !filepath.IsAbs(p) {
			cible = filepath.Dir(a.fichier) + string(filepath.Separator) + p
		}
		if runtimeWindows() {
			cible = filepath.Clean(cible)
		} else {
			cible = normpathPosix(cible)
		}
		if estDossier(cible) {
			a.dossier(cible, chemin)
		} else if estFichier(cible) {
			if _, g, ok := genreDeScript(filepath.Base(cible), a.legacy); ok {
				a.noeuds = a.noeuds[:len(a.noeuds)-1]
				a.script(cible, chemin, g)
			} else {
				a.autres = append(a.autres, cible)
			}
		}
	}
	for _, k := range n.cles {
		if !strings.HasPrefix(k, "$") {
			a.noeud(n.valeur[k], copieChemin(chemin, k))
		}
	}
}

func (a *arbreRojo) dossier(d string, chemin []string) {
	es, err := os.ReadDir(d)
	if err != nil {
		return
	}
	var entrees []string
	for _, e := range es {
		entrees = append(entrees, e.Name())
	}
	sort.Strings(entrees)
	var init string
	var genreInit genreScript
	for _, e := range entrees {
		if nom, g, ok := genreDeScript(e, a.legacy); ok && strings.ToLower(nom) == "init" && estFichier(filepath.Join(d, e)) {
			init, genreInit = filepath.Join(d, e), g
		}
	}
	if init != "" { // the folder is that script
		if n := len(a.noeuds); n > 0 && strings.Join(a.noeuds[n-1].chemin, "\x00") == strings.Join(chemin, "\x00") && len(a.noeuds[n-1].chemin) == len(chemin) {
			a.noeuds = a.noeuds[:n-1]
		}
		a.script(init, chemin, genreInit)
	}
	a.geres = append(a.geres, chemin)
	for _, e := range entrees {
		f := filepath.Join(d, e)
		if strings.HasPrefix(e, ".") || a.ignore(f) || (init != "" && f == init) {
			continue
		}
		if estDossier(f) {
			a.noeuds = append(a.noeuds, noeudRoblox{copieChemin(chemin, e), ""})
			a.dossier(f, copieChemin(chemin, e))
			continue
		}
		if nom, g, ok := genreDeScript(e, a.legacy); ok {
			a.script(f, copieChemin(chemin, nom), g)
		} else {
			a.autres = append(a.autres, f)
		}
	}
}

func (a *arbreRojo) script(f string, chemin []string, g genreScript) {
	st, err := os.Stat(f)
	if err != nil || st.Size() > maxScriptRoblox || !st.Mode().IsRegular() {
		return
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return
	}
	a.scripts = append(a.scripts, scriptRoblox{chemin, g.classe, g.contexte, decoderUTF8Python(b)})
}

// etatRoblox: the other files' dates at the previous request (those changed since are announced).
type etatRoblox struct {
	mu    sync.Mutex
	dates map[string]time.Time
	vu    bool
}

func (e *etatRoblox) changes(autres []string, projet string) []string {
	dates := map[string]time.Time{}
	for _, f := range autres {
		if st, err := os.Stat(f); err == nil {
			dates[f] = st.ModTime()
		}
	}
	e.mu.Lock()
	avant, vu := e.dates, e.vu
	e.dates, e.vu = dates, true
	e.mu.Unlock()
	out := []string{}
	if !vu {
		return out
	}
	for f, t := range dates {
		if a, ok := avant[f]; !ok || !a.Equal(t) {
			r, err := filepath.Rel(projet, f)
			if err != nil {
				r = f
			}
			out = append(out, filepath.ToSlash(r))
		}
	}
	sort.Strings(out)
	return out
}

func chargeRoblox(projet string, etat *etatRoblox) objetJSON {
	var r objetJSON
	a := &arbreRojo{projet: projet, fichier: fichierProjetRojo(projet)}
	if a.fichier == "" {
		r.mettre("erreur", "no Rojo project file (default.project.json) in the project")
		return r
	}
	if err := a.lire(); err != nil {
		r.mettre("erreur", "the Rojo project file cannot be read: "+err.Error())
		return r
	}
	// only the containers on the way to the scripts: an instance of the project file itself (a part and
	// its properties) is the rebuild's job, never created bare here
	utiles := map[string]bool{}
	tous := append([][]string{}, a.geres...)
	for _, s := range a.scripts {
		tous = append(tous, s.chemin)
	}
	for _, c := range tous {
		for i := 1; i <= len(c); i++ {
			utiles[strings.Join(c[:i], "\x00")] = true
		}
	}
	var noeuds []noeudRoblox
	for _, n := range a.noeuds {
		if utiles[strings.Join(n.chemin, "\x00")] {
			noeuds = append(noeuds, n)
		}
	}
	sort.SliceStable(noeuds, func(i, j int) bool { return len(noeuds[i].chemin) < len(noeuds[j].chemin) })
	ln := []objetJSON{}
	for _, n := range noeuds {
		var o objetJSON
		o.mettre("chemin", n.chemin)
		o.mettre("classe", n.classe)
		ln = append(ln, o)
	}
	ls := []objetJSON{}
	for _, s := range a.scripts {
		var o objetJSON
		o.mettre("chemin", s.chemin)
		o.mettre("classe", s.classe)
		o.mettre("contexte", s.contexte)
		o.mettre("source", s.source)
		ls = append(ls, o)
	}
	geres := [][]string{}
	geres = append(geres, a.geres...)
	r.mettre("noeuds", ln)
	r.mettre("scripts", ls)
	r.mettre("geres", geres)
	r.mettre("autres", etat.changes(a.autres, projet))
	return r
}

func cleAleatoire() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func servirRoblox(projet, fichierPid string) int {
	cle := cleAleatoire()
	etat := &etatRoblox{}
	chargeRoblox(projet, etat) // the dates of reference
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	port := l.Addr().(*net.TCPAddr).Port
	dossier := filepath.Join(projet, ".athanor", "studio")
	_ = os.MkdirAll(dossier, 0o777)
	cible := filepath.Join(dossier, "SYNC.luau")
	if err := os.WriteFile(cible+".tmp", []byte(strings.NewReplacer("%PORT%", strconv.Itoa(port), "%CLE%", cle).Replace(luauSync)), 0o666); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.Rename(cible+".tmp", cible)
	fmt.Printf("Roblox sync server on 127.0.0.1:%d\n", port)
	go func() { // the pilot stopped (its pid file is gone, or another pilot took over): stop too
		lirePid := func() (string, bool) {
			b, err := os.ReadFile(fichierPid)
			return stripPython(string(b)), err == nil
		}
		pid, _ := lirePid()
		for {
			time.Sleep(5 * time.Second)
			if p, ok := lirePid(); !ok || p != pid {
				break
			}
		}
		os.Remove(cible)
		os.Exit(0)
	}()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/"+cle {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		corps := jsonPython(chargeRoblox(projet, etat), "", false)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(corps)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(corps)
	})
	_ = (&http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}).Serve(l)
	return 0
}

func lancerRobloxSync(args []string) int {
	args = cheminsOutil(args)
	switch {
	case len(args) == 3 && args[0] == "serve":
		return servirRoblox(abspathOS(args[1]), args[2])
	case len(args) == 2 && args[0] == "payload":
		fmt.Println(string(jsonPython(chargeRoblox(abspathOS(args[1]), &etatRoblox{}), " ", false)))
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: athanor roblox-sync serve PROJECT PIDFILE | payload PROJECT")
	return 1
}
