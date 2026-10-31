package main

// athanor extensions preparer | mcp | tester | connexion: the skills, plugins and MCP servers chosen in the
// app for one Claude session (replaces system/extensions.py, same files and outputs).
//   preparer KIT PROJECT PRIVATE_DIR [--mcp FILE] [--dossier-mcp DIR] [--principal DIR]
//       PRIVATE_DIR/plugin (the project's skills), PRIVATE_DIR/plugins/ (the plugins switched on) and the
//       MCP file (default PRIVATE_DIR/mcp.json); one summary line
//   mcp KIT PROJECT FILE [--dossier-mcp DIR] [--principal DIR]   only the MCP file (a parallel worker)
//   tester KIT PROJECT ID      starts the server in its cage, lists its tools (JSON: the app's Test button)
//   connexion KIT ID FILE      FILE: the remote server ID alone, as the pilot gives it (the sign-in window)
// Local servers (stdio) run through athanor mcp-cage (their cage), Windows programs through athanor
// mcp-windows (WSL interop); in native Windows, both run as they are.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// lireJSONOrdonne: a JSON object file (a UTF-8 BOM allowed), its keys in order; empty when unreadable.
func lireJSONOrdonne(chemin string) objetJSON {
	var o objetJSON
	b, err := os.ReadFile(chemin)
	if err != nil {
		return o
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &o) != nil {
		return objetJSON{}
	}
	return o
}

func (o objetJSON) objet(k string) objetJSON {
	var x objetJSON
	if v, ok := o.valeur[k]; ok {
		_ = json.Unmarshal(v, &x)
	}
	return x
}

func (o objetJSON) brut(k string) any {
	v, ok := o.valeur[k]
	if !ok {
		return nil
	}
	var x any
	d := json.NewDecoder(bytes.NewReader(v))
	d.UseNumber()
	_ = d.Decode(&x)
	return x
}

func (o objetJSON) a(k string) bool { _, ok := o.valeur[k]; return ok }

var reVariable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// remplacerExt: ${VARIABLE} -> the server's secret or setting, {PROJECT} -> the folder it works on.
func remplacerExt(valeur string, secrets map[string]string, dossier string, manquants map[string]bool) string {
	return reVariable.ReplaceAllStringFunc(strings.ReplaceAll(valeur, "{PROJECT}", dossier), func(m string) string {
		n := m[2 : len(m)-1]
		if v, ok := secrets[n]; ok {
			return v
		}
		manquants[n] = true
		return ""
	})
}

var reVariableSure = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func trier(m map[string]bool) []string {
	l := make([]string, 0, len(m))
	for k := range m {
		l = append(l, k)
	}
	sort.Strings(l)
	return l
}

// serveurMCP: Claude Code's configuration of one server, or the reason it is left out.
func serveurMCP(kit, sid string, def objetJSON, secretsServeur objetJSON, dossier, principal string) (*objetJSON, string) {
	// installed with its Windows configuration, on macOS or Linux: this system's variant (mcp_systemes.go)
	def, secretsServeur, raisonSysteme := adapterAuSysteme(kit, def, secretsServeur)
	if raisonSysteme != "" {
		return nil, raisonSysteme
	}
	c := def.objet("config")
	if def.valeur == nil || c.valeur == nil || !strings.HasPrefix(strings.TrimSpace(string(def.valeur["config"])), "{") {
		return nil, "no configuration"
	}
	secrets := map[string]string{}
	if strings.HasPrefix(strings.TrimSpace(string(def.valeur["reglages"])), "{") {
		r := def.objet("reglages")
		for _, k := range r.cles {
			secrets[k] = textePython(r.brut(k))
		}
	}
	for _, k := range secretsServeur.cles {
		v := secretsServeur.brut(k)
		if s, ok := v.(string); !ok || s != "" {
			secrets[k] = textePython(v)
		}
	}
	if s, _ := def.brut("catalogue").(string); s == "unreal" {
		secrets["UNREAL_MCP_URL"] = adresseUnreal(kit, secrets["UNREAL_MCP_URL"])
	}
	manquants := map[string]bool{}
	optionnels := map[string]bool{}
	for _, x := range liste(def.brut("optionnels")) {
		if s, ok := x.(string); ok {
			optionnels[s] = true
		}
	}
	facultatif := func(cles objetJSON) objetJSON {
		var garde objetJSON
		for _, k := range cles.cles {
			m := map[string]bool{}
			val := remplacerExt(textePython(cles.brut(k)), secrets, dossier, m)
			tous := len(m) > 0
			for n := range m {
				tous = tous && optionnels[n]
			}
			if tous {
				continue
			}
			for n := range m {
				manquants[n] = true
			}
			garde.mettre(k, val)
		}
		return garde
	}
	pid, _ := def.brut("plugin").(string)
	if !reIdSur.MatchString(pid) {
		pid = ""
	}
	racinePlugin := ""
	if pid != "" {
		racinePlugin = filepath.Join(kit, "extensions", "plugins", pid)
		donnees := filepath.Join(kit, "tools", "plugin-data", pid)
		_ = os.MkdirAll(donnees, 0o755)
		secrets["CLAUDE_PLUGIN_ROOT"], secrets["CLAUDE_PLUGIN_DATA"], secrets["CLAUDE_PROJECT_DIR"] = racinePlugin, donnees, dossier
	}
	t, _ := c.brut("type").(string)
	if !vraiPython(c.brut("type")) {
		t = "stdio"
		if vraiPython(c.brut("url")) {
			t = "http"
		}
	}
	argsDe := func(d string) []string {
		var out []string
		for _, a := range liste(c.brut("args")) {
			out = append(out, remplacerExt(textePython(a), secrets, d, manquants))
		}
		return out
	}
	var s objetJSON
	switch t {
	case "http", "sse":
		u, _ := c.brut("url").(string)
		url := remplacerExt(u, secrets, dossier, manquants)
		if !regexp.MustCompile(`^https?://`).MatchString(url) {
			return nil, "invalid URL"
		}
		s.mettre("type", t)
		s.mettre("url", url)
		if h := facultatif(c.objet("headers")); len(h.cles) > 0 {
			s.mettre("headers", h)
		}
	case "stdio":
		cmd, ok := c.brut("command").(string)
		if !ok || strings.TrimSpace(cmd) == "" {
			return nil, "no command"
		}
		args := argsDe(dossier)
		env := facultatif(c.objet("env"))
		if runtime.GOOS == "windows" {
			s.mettre("type", "stdio")
			ajouterCommandeWindows(&s, remplacerExt(cmd, secrets, dossier, manquants), args)
			if len(env.cles) > 0 {
				s.mettre("env", env)
			}
			if len(manquants) > 0 {
				return nil, "missing key " + strings.Join(trier(manquants), ", ")
			}
			return &s, ""
		}
		cage := []string{sid, filepath.Join(kit, "tools"), "--rw", dossier}
		if principal != "" && realpathPython(principal) != realpathPython(dossier) {
			cage = []string{sid, filepath.Join(kit, "tools"), "--ro", principal, "--rw", dossier}
			if st, err := os.Stat(filepath.Join(principal, ".git")); err == nil && st.IsDir() {
				cage = append(cage, "--rw", filepath.Join(principal, ".git"))
			}
		}
		if racinePlugin != "" {
			cage = append(cage, "--ro", racinePlugin)
		}
		cles := append([]string(nil), env.cles...)
		sort.Strings(cles)
		for _, k := range cles {
			var v string
			_ = json.Unmarshal(env.valeur[k], &v)
			if !reVariableSure.MatchString(k) || strings.Contains(v, "\x00") {
				return nil, "invalid variable " + tronquerRunes(k, 40)
			}
			cage = append(cage, "--env", k, v)
		}
		s.mettre("type", "stdio")
		s.mettre("command", filepath.Join(kit, "athanor"))
		s.mettre("args", append(append(append([]string{"mcp-cage"}, cage...), "--", remplacerExt(cmd, secrets, dossier, manquants)), args...))
	case "hote":
		// a program of this computer, outside the cage: the verified catalog's only (mcp_systemes.go)
		if id, _ := def.brut("catalogue").(string); id == "" || entreeCatalogueSysteme(kit, id) == nil {
			return nil, "a program outside the cage: only from the verified catalog"
		}
		cmd, ok := c.brut("command").(string)
		if !ok || strings.TrimSpace(cmd) == "" {
			return nil, "no command"
		}
		athanor := filepath.Join(kit, "athanor")
		if runtime.GOOS == "windows" {
			athanor = filepath.Join(kit, "Athanor.exe")
		}
		var args []string
		for _, a := range argsDe(dossier) {
			args = append(args, commandeHote(a, athanor))
		}
		s.mettre("type", "stdio")
		s.mettre("command", commandeHote(remplacerExt(cmd, secrets, dossier, manquants), athanor))
		s.mettre("args", args)
		if env := facultatif(c.objet("env")); len(env.cles) > 0 {
			s.mettre("env", env)
		}
	case "windows":
		cmd, ok := c.brut("command").(string)
		if !ok || strings.TrimSpace(cmd) == "" {
			return nil, "no command"
		}
		if runtime.GOOS == "windows" {
			args := argsDe(dossier)
			s.mettre("type", "stdio")
			ajouterCommandeWindows(&s, remplacerExt(cmd, secrets, dossier, manquants), args)
			if env := facultatif(c.objet("env")); len(env.cles) > 0 {
				s.mettre("env", env)
			}
			if len(manquants) > 0 {
				return nil, "missing key " + strings.Join(trier(manquants), ", ")
			}
			return &s, ""
		}
		if !interopWindows() {
			return nil, "Windows program: Windows is not reachable from here"
		}
		dossierW := cheminWindowsWSL(dossier)
		args := argsDe(dossierW)
		env := facultatif(c.objet("env"))
		cles := append([]string(nil), env.cles...)
		sort.Strings(cles)
		var lancement []string
		for _, k := range cles {
			var v string
			_ = json.Unmarshal(env.valeur[k], &v)
			if !reVariableSure.MatchString(k) || strings.Contains(v, "\x00") {
				return nil, "invalid variable " + tronquerRunes(k, 40)
			}
			lancement = append(lancement, "--env", k, v)
		}
		s.mettre("type", "stdio")
		s.mettre("command", filepath.Join(kit, "athanor"))
		s.mettre("args", append(append(append([]string{"mcp-windows"}, lancement...), "--", remplacerExt(cmd, secrets, dossierW, manquants)), args...))
	default:
		return nil, "unknown type " + textePython(c.brut("type"))
	}
	if len(manquants) > 0 {
		return nil, "missing key " + strings.Join(trier(manquants), ", ")
	}
	return &s, ""
}

// ajouterCommandeWindows: native Windows, a .cmd or .bat launcher (npx, uvx…) goes through cmd /c.
func ajouterCommandeWindows(s *objetJSON, cmd string, args []string) {
	w, _ := exec.LookPath(cmd)
	b := strings.ToLower(filepath.Base(cmd))
	if l := strings.ToLower(w); (strings.HasSuffix(l, ".cmd") || strings.HasSuffix(l, ".bat")) && b != "cmd" && b != "cmd.exe" {
		s.mettre("command", "cmd")
		s.mettre("args", append([]string{"/c", cmd}, args...))
		return
	}
	if args == nil {
		args = []string{}
	}
	s.mettre("command", cmd)
	s.mettre("args", args)
}

func interopWindows() bool {
	for _, n := range []string{"WSLInterop", "WSLInterop-late"} {
		if _, err := os.Stat("/proc/sys/fs/binfmt_misc/" + n); err == nil {
			return true
		}
	}
	return os.Getenv("WSL_INTEROP") != ""
}

func cheminWindowsWSL(dossier string) string {
	b, err := commande("wslpath", "-w", dossier).Output()
	if err != nil {
		return dossier
	}
	if w := strings.TrimSpace(string(b)); w != "" {
		return w
	}
	return dossier
}

var (
	reURLLocale  = regexp.MustCompile(`^https?://(127\.0\.0\.1|localhost)(:\d+)?(/|$)`)
	reHoteURL    = regexp.MustCompile(`^(https?://)[^/]+`)
	reNonChiffre = regexp.MustCompile(`\D`)
)

// adresseUnreal: the Unreal MCP as seen from WSL, through the Windows relay when "mirrored" networking is
// unavailable (its port in system/unreal_relay.txt, the Windows host = WSL's default gateway).
func adresseUnreal(kit, url string) string {
	if url == "" {
		url = "http://127.0.0.1:8000/mcp"
	}
	if runtime.GOOS == "windows" {
		return url
	}
	relais := filepath.Join(kit, "system", "unreal_relay.txt")
	if st, err := os.Stat(relais); err != nil || !st.Mode().IsRegular() || !reURLLocale.MatchString(url) {
		return url
	}
	b, err := os.ReadFile(relais)
	if err != nil {
		return url
	}
	port := reNonChiffre.ReplaceAllString(string(b), "")
	route, err := commande("ip", "route", "show", "default").Output()
	if err != nil {
		return url
	}
	mots := strings.Fields(string(route))
	passerelle := ""
	for i, m := range mots {
		if m == "via" && i+1 < len(mots) {
			passerelle = mots[i+1]
			break
		}
	}
	if port == "" || passerelle == "" {
		return url
	}
	loc := reHoteURL.FindStringSubmatchIndex(url)
	return url[:loc[2]] + url[loc[2]:loc[3]] + passerelle + ":" + port + url[loc[1]:]
}

func catalogueKit(kit string) objetJSON {
	if b, err := os.ReadFile(filepath.Join(kit, "local", "app", "catalogue.json")); err == nil {
		var o objetJSON
		if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &o) == nil {
			return o
		}
	}
	if b, err := os.ReadFile(filepath.Join(kit, "app", "catalogue.json")); err == nil {
		var o objetJSON
		if json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &o) == nil {
			return o
		}
		return objetJSON{}
	}
	var o objetJSON
	_ = json.Unmarshal(jsonRemplacable("app/catalogue.json", catalogueJSON), &o)
	return o
}

func construireMCP(kit string, choix map[string][]string, fichier, dossier, principal string) ([]string, []string) {
	var serveurs objetJSON
	var notes []string
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
	secrets := lireJSONOrdonne(filepath.Join(kit, "extensions", "secrets.json"))
	var ids []string
	for _, x := range choix["mcp"] {
		if !vraiPython(dict(biblio.brut(x))["plugin"]) {
			ids = append(ids, x)
		}
	}
	plugins := map[string]bool{}
	for _, p := range choix["plugins"] {
		plugins[p] = true
	}
	var duPlugin []string
	for _, x := range biblio.cles {
		d := dict(biblio.brut(x))
		if p, ok := d["plugin"].(string); ok && plugins[p] && reIdSur.MatchString(x) {
			duPlugin = append(duPlugin, x)
		}
	}
	sort.Strings(duPlugin)
	ids = append(ids, duPlugin...)
	var aides []string
	for _, sid := range ids {
		if !biblio.a(sid) {
			continue
		}
		// Blender in the background (blender_apercu.go): its MCP server left out of the session
		if estEntreeBlender(sid, biblio.objet(sid)) {
			if m := modeBlenderDe(biblio.objet(sid)); m != modeBlenderDirect {
				aides = append(aides, "- **"+sid+"**: "+consigneModeBlender(m))
				notes = append(notes, sid+map[bool]string{true: " in the background, shown in a preview window", false: " in the background"}[m == modeBlenderApercu])
				continue
			}
		}
		s, raison := serveurMCP(kit, sid, biblio.objet(sid), secrets.objet(sid), dossier, principal)
		if s != nil {
			serveurs.mettre(sid, *s)
		} else {
			notes = append(notes, sid+" skipped ("+raison+")")
		}
	}
	catalogue := map[string]map[string]any{}
	for _, m := range liste(catalogueKit(kit).brut("mcp")) {
		if d := dict(m); d != nil {
			if id, ok := d["id"].(string); ok {
				catalogue[id] = d
			}
		}
	}
	for _, sid := range serveurs.cles {
		d := dict(biblio.brut(sid))
		a, _ := d["sauvegarde"].(string)
		if !vraiPython(d["sauvegarde"]) {
			cle, ok := d["catalogue"].(string)
			if !vraiPython(d["catalogue"]) {
				cle, ok = sid, true
			}
			if ok {
				a, _ = catalogue[cle]["sauvegarde"].(string)
				if e := entreeCatalogueSysteme(kit, cle); e != nil { // this system's words, when they differ
					a, _ = e["sauvegarde"].(string)
				}
			}
		}
		if strings.TrimSpace(a) != "" {
			aides = append(aides, fmt.Sprintf("- **%s**: %s", sid, tronquerRunes(strings.TrimSpace(a), 1500)))
		}
	}
	if len(aides) > 0 {
		_ = os.WriteFile(fichier+".sauvegarde.md", []byte("## Saving the work done through your MCP servers\n\nThese programs keep what you do in them until it is saved. Before you tick a milestone, save it into the project folder so that the commit holds it:\n"+strings.Join(aides, "\n")+"\n"), 0o644)
	} else {
		os.Remove(fichier + ".sauvegarde.md")
	}
	if len(serveurs.cles) > 0 {
		neuf := fichier + ".neuf"
		os.Remove(neuf)
		var r objetJSON
		r.mettre("mcpServers", serveurs)
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.SetIndent("", "  ")
		if e.Encode(r) == nil && os.WriteFile(neuf, bytes.TrimRight(b.Bytes(), "\n"), 0o600) == nil {
			_ = os.Rename(neuf, fichier)
		}
	} else {
		os.Remove(fichier)
	}
	return serveurs.cles, notes
}

// copierDossier: a copy of src in dst without .git and __pycache__; suivre: links followed (a broken one
// skipped), else every link left out.
func copierDossier(src, dst string, suivre bool) error { return copierDossierN(src, dst, suivre, 0) }

func copierDossierN(src, dst string, suivre bool, liens int) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		if d.Name() == ".git" || d.Name() == "__pycache__" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		cible := filepath.Join(dst, rel)
		if d.Type()&fs.ModeSymlink != 0 {
			if !suivre {
				return nil
			}
			st, err := os.Stat(p)
			if err != nil {
				return nil // dangling
			}
			if st.IsDir() {
				vrai, err := filepath.EvalSymlinks(p)
				parent, _ := filepath.EvalSymlinks(filepath.Dir(p))
				if err != nil || liens >= 16 || parent == vrai || strings.HasPrefix(parent, vrai+string(filepath.Separator)) {
					return nil // a loop of links
				}
				return copierDossierN(vrai, cible, true, liens+1)
			}
			return copierFichier(p, cible, st)
		}
		if d.IsDir() {
			st, _ := d.Info()
			return os.MkdirAll(cible, st.Mode().Perm()|0o700)
		}
		st, err := d.Info()
		if err != nil || !st.Mode().IsRegular() {
			return nil
		}
		return copierFichier(p, cible, st)
	})
}

func copierFichier(src, dst string, st os.FileInfo) error {
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	in, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	out.Close()
	_ = os.Chmod(dst, st.Mode().Perm())
	_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	return err
}

func estDossierSansLien(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

func construirePlugin(kit string, choix map[string][]string, prive string) []string {
	source := filepath.Join(kit, "extensions", "skills")
	neuf, final := filepath.Join(prive, "plugin.neuf"), filepath.Join(prive, "plugin")
	os.RemoveAll(neuf)
	var copies []string
	for _, sid := range choix["skills"] {
		d := filepath.Join(source, sid)
		if st, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil && st.Mode().IsRegular() && estDossierSansLien(d) {
			if copierDossier(d, filepath.Join(neuf, "skills", sid), true) == nil {
				copies = append(copies, sid)
			}
		}
	}
	if len(copies) > 0 {
		_ = os.MkdirAll(filepath.Join(neuf, ".claude-plugin"), 0o755)
		_ = os.WriteFile(filepath.Join(neuf, ".claude-plugin", "plugin.json"), []byte(`{"name": "athanor", "description": "Skills chosen in Athanor for this project"}`), 0o644)
	}
	vieux := filepath.Join(prive, "plugin.vieux")
	os.RemoveAll(vieux)
	if st, err := os.Stat(final); err == nil && st.IsDir() {
		_ = os.Rename(final, vieux)
	}
	if len(copies) > 0 {
		_ = os.Rename(neuf, final)
	}
	os.RemoveAll(vieux)
	os.RemoveAll(neuf)
	return copies
}

func construirePlugins(kit string, choix map[string][]string, prive string) []string {
	source := filepath.Join(kit, "extensions", "plugins")
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("plugins")
	neuf, final, vieux := filepath.Join(prive, "plugins.neuf"), filepath.Join(prive, "plugins"), filepath.Join(prive, "plugins.vieux")
	os.RemoveAll(neuf)
	_ = os.MkdirAll(neuf, 0o755)
	var copies []string
	for _, pid := range choix["plugins"] {
		d := filepath.Join(source, pid)
		if biblio.a(pid) && estDossierSansLien(d) {
			c := filepath.Join(neuf, pid)
			_ = copierDossier(d, c, false)
			for _, f := range []string{"hooks/hooks.json", ".mcp.json", ".lsp.json", "settings.json"} { // never run outside the sandbox
				os.Remove(filepath.Join(c, filepath.FromSlash(f)))
			}
			for _, f := range []string{"monitors", "workflows"} {
				os.RemoveAll(filepath.Join(c, f))
			}
			copies = append(copies, pid)
		}
	}
	os.RemoveAll(vieux)
	if st, err := os.Stat(final); err == nil && st.IsDir() {
		_ = os.Rename(final, vieux)
	}
	_ = os.Rename(neuf, final)
	os.RemoveAll(vieux)
	return copies
}

func optionsExt(args []string) map[string]string {
	v := map[string]string{}
	for len(args) > 0 {
		cle := args[0]
		args = args[1:]
		if strings.HasPrefix(cle, "--") && len(args) > 0 {
			v[cle[2:]] = args[0]
			args = args[1:]
		}
	}
	return v
}

// ---------------------------------------------------------------- the Test button

type lecteurLignes struct {
	lignes chan []byte
}

// lireLignes: the complete lines of r (a last line without its end is left, as extensions.py does).
func lireLignes(r io.Reader) *lecteurLignes {
	l := &lecteurLignes{lignes: make(chan []byte, 64)}
	go func() {
		br := bufio.NewReaderSize(r, 64<<10)
		for {
			b, err := br.ReadBytes('\n')
			if err != nil {
				break
			}
			l.lignes <- b
		}
		close(l.lignes)
	}()
	return l
}

// attendreReponse: the JSON-RPC answer whose id is id, nil at the end of the output or of the delay.
func (l *lecteurLignes) attendreReponse(id int, fin time.Time) map[string]any {
	for {
		reste := time.Until(fin)
		if reste <= 0 {
			return nil
		}
		select {
		case b, ok := <-l.lignes:
			if !ok {
				return nil
			}
			var m map[string]any
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			if n, ok := m["id"].(float64); ok && n == float64(id) {
				return m
			}
		case <-time.After(reste):
			return nil
		}
	}
}

func testerServeur(kit, projet, sid string, delai time.Duration) map[string]any {
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
	if !biblio.a(sid) {
		return map[string]any{"ok": false, "erreur": "unknown server"}
	}
	return testerDefinition(kit, projet, sid, biblio.objet(sid), lireJSONOrdonne(filepath.Join(kit, "extensions", "secrets.json")).objet(sid), delai)
}

// testerDefinition: a server started as this definition says (the library's, or one proposed to fix it).
func testerDefinition(kit, projet, sid string, def, secretsServeur objetJSON, delai time.Duration) map[string]any {
	s, raison := serveurMCP(kit, sid, def, secretsServeur, projet, "")
	if s == nil {
		return map[string]any{"ok": false, "erreur": raison}
	}
	if t, _ := s.brut("type").(string); t != "stdio" {
		return map[string]any{"ok": true, "outils": []string{}, "note": "remote server: Claude Code connects to it at the next cycle"}
	}
	cmd, _ := s.brut("command").(string)
	var args []string
	for _, a := range liste(s.brut("args")) {
		args = append(args, textePython(a))
	}
	p := commande(cmd, args...)
	p.Env = os.Environ()
	env := s.objet("env")
	for _, k := range env.cles {
		var v string
		_ = json.Unmarshal(env.valeur[k], &v)
		p.Env = append(p.Env, k+"="+v)
	}
	if st, err := os.Stat(projet); err == nil && st.IsDir() {
		p.Dir = projet
	}
	entree, _ := p.StdinPipe()
	sortie, ecriture, err := os.Pipe() // read here, not by exec: a quick server's last answer is never lost
	if err != nil {
		return map[string]any{"ok": false, "erreur": err.Error()}
	}
	defer sortie.Close()
	p.Stdout = ecriture
	var erreurs bytes.Buffer
	p.Stderr = &limiteur{b: &erreurs, max: 1 << 20}
	p.WaitDelay = 3 * time.Second // a program that outlives its launcher keeps the pipes open
	err = p.Start()
	ecriture.Close()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			nom := filepath.Base(cmd)
			court := strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(nom), ".exe"), ".cmd")
			aide := map[string]string{"uvx": " (it comes with uv: winget install --id astral-sh.uv -e, or Setup & health -> Install everything)",
				"npx": " (it comes with Node.js: Setup & health -> Install everything)"}[court]
			return map[string]any{"ok": false, "erreur": "program not found on this PC: " + nom + aide}
		}
		return map[string]any{"ok": false, "erreur": err.Error()}
	}
	fini := make(chan struct{})
	go func() { _ = p.Wait(); close(fini) }()
	arreter := func() {
		select {
		case <-fini:
			return
		default:
		}
		entree.Close()
		if runtime.GOOS == "windows" { // the server and everything it started (node, python…)
			_ = commande("taskkill", "/T", "/F", "/PID", fmt.Sprint(p.Process.Pid)).Run()
		}
		_ = signalerFin(p.Process)
		select {
		case <-fini:
		case <-time.After(5 * time.Second):
			_ = p.Process.Kill()
			<-fini
		}
	}
	defer arreter()
	l := lireLignes(sortie)
	envoyer := func(m any) error {
		b, _ := json.Marshal(m)
		_, err := entree.Write(append(b, '\n'))
		return err
	}
	fin := time.Now().Add(delai)
	echec := func(e string) map[string]any {
		arreter()
		var details []string
		for _, x := range lignesPython(strings.TrimSpace(strings.ToValidUTF8(erreurs.String(), "�"))) {
			details = append(details, x)
		}
		if len(details) > 6 {
			details = details[len(details)-6:]
		}
		if details == nil {
			details = []string{}
		}
		return map[string]any{"ok": false, "erreur": e, "details": details}
	}
	casse := "[Errno 32] Broken pipe"
	// a server that already stopped: its end of the pipe may be closed or not yet (a race in
	// extensions.py too); either way it gives no answer, which is what is said
	_ = envoyer(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "athanor", "version": "1"}}})
	r := l.attendreReponse(1, fin)
	if r == nil || r["result"] == nil {
		return echec("no answer to initialize")
	}
	info := dict(r["result"])["serverInfo"]
	if !vraiPython(info) {
		info = nil
	}
	if envoyer(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}) != nil || envoyer(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}) != nil {
		return echec(casse)
	}
	r = l.attendreReponse(2, fin)
	outils := []string{}
	for _, t := range liste(dict(dict(r)["result"])["tools"]) {
		n, _ := dict(t)["name"].(string)
		outils = append(outils, n)
	}
	return map[string]any{"ok": true, "outils": outils, "serveur": info}
}

type limiteur struct {
	b   *bytes.Buffer
	max int
}

func (l *limiteur) Write(p []byte) (int, error) {
	if l.b.Len() < l.max {
		l.b.Write(p[:min(len(p), l.max-l.b.Len())])
	}
	return len(p), nil
}

func lancerExtensions(args []string) int {
	args = cheminsOutil(args)
	if len(args) >= 2 {
		switch args[0] {
		case "corrections", "appliquer-corrections": // ext_corrections.go
			return lancerCorrectionsExt(args[0], abspathOS(args[1]), args[2:], os.Stdout)
		case "tester-skill", "corriger-skill", "appliquer-skill", "annuler-skill": // ext_skill.go
			if len(args) < 3 || !reIdSur.MatchString(args[2]) {
				fmt.Fprintln(os.Stderr, "usage: athanor extensions "+args[0]+" KIT ID")
				return 2
			}
			return lancerSkillExt(args[0], abspathOS(args[1]), args[2], os.Stdout)
		case "mode-blender": // blender_apercu.go: athanor extensions mode-blender KIT ID apercu|arriere-plan|direct
			if len(args) < 4 || !reIdSur.MatchString(args[2]) || (args[3] != modeBlenderApercu && args[3] != modeBlenderArrierePlan && args[3] != modeBlenderDirect) {
				fmt.Fprintln(os.Stderr, "usage: athanor extensions mode-blender KIT ID apercu|arriere-plan|direct")
				return 2
			}
			if err := ecrireModeBlender(Kit{Racine: abspathOS(args[1])}, args[2], args[3]); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return 1
			}
			fmt.Println("Saved: it applies from the next cycle.")
			return 0
		case "annuler-correction":
			if len(args) < 3 || !reIdSur.MatchString(args[2]) {
				return 2
			}
			if err := annulerCorrectionMCP(Kit{Racine: abspathOS(args[1])}, args[2]); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return 1
			}
			fmt.Println("The server's configuration before Claude's fix is back.")
			return 0
		}
	}
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: athanor extensions preparer KIT PROJECT PRIVATE_DIR [--mcp FILE] [--dossier-mcp DIR] [--principal DIR] | mcp KIT PROJECT FILE [...] | tester KIT PROJECT ID | corriger KIT PROJECT ID [--appliquer] | annuler-correction KIT ID | corrections KIT | appliquer-corrections KIT CODE | tester-skill KIT ID | corriger-skill KIT ID | connexion KIT ID FILE")
		return 2
	}
	action, kit, projet := args[0], abspathOS(args[1]), args[2]
	switch action {
	case "corriger": // ext_corriger.go
		return lancerCorrigerExt(kit, projet, args[3:], os.Stdout)
	case "tester":
		sid := ""
		if len(args) > 3 {
			sid = args[3]
		}
		var r map[string]any
		if reIdSur.MatchString(sid) {
			r = testerServeur(kit, projet, sid, 150*time.Second)
		} else {
			r = map[string]any{"ok": false, "erreur": "invalid id"}
		}
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
		return 0
	case "connexion":
		sid, fichier := "", ""
		if len(args) > 3 {
			sid, fichier = args[2], args[3]
		}
		biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
		if !reIdSur.MatchString(sid) || !biblio.a(sid) {
			fmt.Fprintln(os.Stderr, "unknown server")
			return 1
		}
		wd, _ := os.Getwd()
		s, raison := serveurMCP(kit, sid, biblio.objet(sid), lireJSONOrdonne(filepath.Join(kit, "extensions", "secrets.json")).objet(sid), wd, "")
		t := ""
		if s != nil {
			t, _ = s.brut("type").(string)
		}
		if s == nil || (t != "http" && t != "sse") {
			fmt.Fprintln(os.Stderr, firstNonEmpty(raison, "not a remote server"))
			return 1
		}
		var r, m objetJSON
		m.mettre(sid, *s)
		r.mettre("mcpServers", m)
		b, _ := json.Marshal(r)
		if err := os.WriteFile(fichier, b, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	choix := choixDuProjetKit(kit, projet)
	switch action {
	case "preparer":
		if len(args) < 4 {
			return 2
		}
		prive, o := args[3], optionsExt(args[4:])
		_ = os.MkdirAll(prive, 0o755)
		skills := construirePlugin(kit, choix, prive)
		plugins := construirePlugins(kit, choix, prive)
		mcp, notes := construireMCP(kit, choix, firstNonEmpty(o["mcp"], filepath.Join(prive, "mcp.json")), firstNonEmpty(o["dossier-mcp"], projet), o["principal"])
		notes = append(notes, construireOuvertures(kit, mcp, filepath.Join(prive, "ouvrir.json"), firstNonEmpty(o["dossier-mcp"], projet))...)
		aucun := func(l []string) string {
			if len(l) == 0 {
				return "none"
			}
			return strings.Join(l, ", ")
		}
		ligne := "skills: " + aucun(skills) + " · plugins: " + aucun(plugins) + " · MCP: " + aucun(mcp)
		if len(notes) > 0 {
			ligne += " · " + strings.Join(notes, "; ")
		}
		fmt.Println(ligne)
		return 0
	case "mcp":
		if len(args) < 4 {
			return 2
		}
		o := optionsExt(args[4:])
		construireMCP(kit, choix, args[3], firstNonEmpty(o["dossier-mcp"], projet), o["principal"])
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown action "+action)
	return 2
}
