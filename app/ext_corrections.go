package main

// What "Fix with Claude" found, kept on this computer and written for everyone:
//   - the fix itself goes into this computer's library (appliquerCorrectionMCP), with what it replaced:
//     Undo puts it back;
//   - every fix, and every fault of Athanor's own integration that Claude names, is written as a report in
//     logs/corrections/: the verified catalog and Athanor are corrected from them, for every user.
//
//   athanor extensions corrections KIT                 the reports waiting (JSON)
//   athanor extensions appliquer-corrections KIT CODE  the catalog's fixes written into CODE/app/catalogue.json
//                                                     (a copy of Athanor's code: commit and push it), the
//                                                     reports applied moved to logs/corrections/appliquees/

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RapportCorrection: one report of logs/corrections/.
type RapportCorrection struct {
	Fichier     string            `json:"fichier,omitempty"` // its name (set when read)
	Genre       string            `json:"genre"`             // "mcp", "skill"
	ID          string            `json:"id"`
	Nom         string            `json:"nom"`
	Catalogue   string            `json:"catalogue,omitempty"` // the verified catalog's entry it fixes
	Plugin      string            `json:"plugin,omitempty"`
	Source      string            `json:"source,omitempty"`
	Systeme     string            `json:"systeme"` // windows, macos, linux
	OS          string            `json:"os"`
	Version     string            `json:"version"`
	Le          string            `json:"le"`
	Erreur      string            `json:"erreur"`
	Details     []string          `json:"details,omitempty"`
	Avant       *ConfigMCP        `json:"avant,omitempty"`
	Apres       *ConfigMCP        `json:"apres,omitempty"` // nil: no fix found
	Reglages    map[string]string `json:"reglages,omitempty"`
	Explication string            `json:"explication,omitempty"`
	Manque      string            `json:"manque,omitempty"`
	Athanor     string            `json:"athanor,omitempty"`
	Fichiers    []string          `json:"fichiers,omitempty"` // a skill: the files changed
}

func dossierCorrections(kit string) string { return filepath.Join(kit, "logs", "corrections") }

// ecrireRapportCorrection: the report of a server's fix (or of why there is none).
func ecrireRapportCorrection(kit, sid string, res ResultatCorrection) error {
	if res.Deja || res.Avant == nil || (!res.OK && res.Athanor == "" && res.Manque == "" && len(res.Essais) == 0) {
		return nil
	}
	b := lireBiblio(Kit{Racine: kit})
	e := b.MCP[sid]
	r := RapportCorrection{Genre: "mcp", ID: sid, Nom: e.Nom, Catalogue: e.Catalogue, Plugin: e.Plugin, Source: e.Source, Systeme: res.Systeme,
		OS: descriptionSysteme(), Version: versionAthanor, Le: time.Now().Format("2006-01-02 15:04"), Erreur: res.Initiale, Details: res.Details,
		Avant: res.Avant, Explication: res.Explication, Manque: res.Manque, Athanor: res.Athanor}
	if res.OK {
		r.Apres, r.Reglages = res.Config, res.Reglages
	}
	return ecrireRapport(kit, r)
}

func ecrireRapport(kit string, r RapportCorrection) error {
	d := dossierCorrections(kit)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	r.Fichier = ""
	nom := time.Now().Format("20060102-150405") + "-" + r.Genre + "-" + r.ID + "-" + r.Systeme + ".json"
	return ecrireJSONFichier(filepath.Join(d, nom), r, 0o644)
}

// lireRapports: the reports waiting, newest first.
func lireRapports(kit string) []RapportCorrection {
	l, _ := filepath.Glob(filepath.Join(dossierCorrections(kit), "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(l)))
	out := []RapportCorrection{}
	for _, f := range l {
		var r RapportCorrection
		lireJSONFichier(f, &r)
		if r.ID == "" {
			continue
		}
		r.Fichier = filepath.Base(f)
		out = append(out, r)
	}
	return out
}

// appliquerCorrectionMCP: the fix into this computer's library (what it replaced kept for Undo).
func appliquerCorrectionMCP(k Kit, sid string, res ResultatCorrection) error {
	if !res.OK || res.Config == nil {
		return errors.New("no fix")
	}
	b := lireBiblio(k)
	e, ok := b.MCP[sid]
	if !ok {
		return errors.New("unknown server")
	}
	if e.Correction == nil { // the configuration before the first fix: what Undo puts back
		e.Correction = &CorrectionMCP{ConfigAvant: e.Config, ReglagesAvant: copieTextes(e.Reglages)}
	}
	e.Correction.Le, e.Correction.Explication, e.Correction.Systeme = maintenantISO(), res.Explication, res.Systeme
	e.Config = *res.Config
	if len(res.Reglages) > 0 {
		if e.Reglages == nil {
			e.Reglages = map[string]string{}
		}
		sec := lireSecrets(k)
		for n, v := range res.Reglages {
			e.Reglages[n] = v
			if x, ok := sec[sid][n]; ok { // the value given before: the fix's default is what was tested
				if e.Correction.ReglagesRetablis == nil {
					e.Correction.ReglagesRetablis = map[string]string{}
				}
				if _, deja := e.Correction.ReglagesRetablis[n]; !deja {
					e.Correction.ReglagesRetablis[n] = x
				}
				delete(sec[sid], n)
			}
		}
		if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), sec, 0o600); err != nil {
			return err
		}
	}
	b.MCP[sid] = e
	return ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
}

// annulerCorrectionMCP: Undo: the configuration and the settings as they were before the first fix.
func annulerCorrectionMCP(k Kit, sid string) error {
	b := lireBiblio(k)
	e, ok := b.MCP[sid]
	if !ok || e.Correction == nil {
		return errors.New("nothing to undo")
	}
	c := e.Correction
	e.Config, e.Reglages, e.Correction = c.ConfigAvant, c.ReglagesAvant, nil
	if len(c.ReglagesRetablis) > 0 {
		sec := lireSecrets(k)
		if sec[sid] == nil {
			sec[sid] = map[string]string{}
		}
		for n, v := range c.ReglagesRetablis {
			sec[sid][n] = v
		}
		if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), sec, 0o600); err != nil {
			return err
		}
	}
	b.MCP[sid] = e
	return ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644)
}

func copieTextes(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// ----------------------------------------------------------------------------- into Athanor's code

// jsonStyleCatalogue: compact JSON written the catalog's way ("a": 1, "b": [2, 3]), letters as they are.
func jsonStyleCatalogue(v any) (string, error) {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return "", err
	}
	src := bytes.TrimSpace(buf.Bytes())
	var out strings.Builder
	dans, echap := false, false
	for _, c := range src {
		out.WriteByte(c)
		switch {
		case echap:
			echap = false
		case dans && c == '\\':
			echap = true
		case c == '"':
			dans = !dans
		case !dans && (c == ':' || c == ','):
			out.WriteByte(' ')
		}
	}
	return out.String(), nil
}

// finObjet: where the JSON object starting at i ends (its closing brace's index + 1).
func finObjet(s string, i int) int {
	prof, dans, echap := 0, false, false
	for j := i; j < len(s); j++ {
		c := s[j]
		switch {
		case echap:
			echap = false
		case dans && c == '\\':
			echap = true
		case c == '"':
			dans = !dans
		case dans:
		case c == '{' || c == '[':
			prof++
		case c == '}' || c == ']':
			prof--
			if prof == 0 {
				return j + 1
			}
		}
	}
	return -1
}

// corrigerEntreeCatalogue: the catalog's text with one MCP entry fixed for one system (Windows: the entry's
// own configuration; macOS, Linux: its variant under "systemes"); only that entry's text changes.
func corrigerEntreeCatalogue(texte string, r RapportCorrection) (string, error) {
	debutMCP := strings.Index(texte, `"mcp": [`)
	if debutMCP < 0 {
		return "", errors.New(`no "mcp" list in the catalog`)
	}
	cle, _ := json.Marshal(r.Catalogue)
	i := strings.Index(texte[debutMCP:], `{"id": `+string(cle))
	if i < 0 {
		return "", errors.New("the catalog has no server " + r.Catalogue)
	}
	i += debutMCP
	j := finObjet(texte, i)
	if j < 0 {
		return "", errors.New("the catalog's entry " + r.Catalogue + " does not end")
	}
	var e objetJSON
	if err := json.Unmarshal([]byte(texte[i:j]), &e); err != nil {
		return "", err
	}
	// the settings' defaults: [{"nom": …, "defaut": …}]
	reglages := func(o objetJSON) []map[string]string {
		var l []map[string]string
		_ = json.Unmarshal(o.valeur["reglages"], &l)
		for n, v := range r.Reglages {
			trouve := false
			for _, x := range l {
				if x["nom"] == n {
					x["defaut"], trouve = v, true
				}
			}
			if !trouve {
				l = append(l, map[string]string{"nom": n, "defaut": v})
			}
		}
		return l
	}
	if r.Systeme == "windows" {
		e.mettre("config", r.Apres)
		if len(r.Reglages) > 0 {
			e.mettre("reglages", reglages(e))
		}
	} else {
		sys := e.objet("systemes")
		v := sys.objet(r.Systeme)
		v.mettre("config", r.Apres)
		if len(r.Reglages) > 0 {
			if !v.a("reglages") { // the system's settings start from the entry's own
				v.valeur = mapAvec(v.valeur, "reglages", e.valeur["reglages"])
				v.cles = append(v.cles, "reglages")
			}
			v.mettre("reglages", reglages(v))
		}
		sys.mettre(r.Systeme, v)
		e.mettre("systemes", sys)
	}
	// only the changed key's text is rewritten: the entry keeps its lines
	entree := texte[i:j]
	for _, k := range []string{"config", "reglages", "systemes"} {
		if !e.a(k) {
			continue
		}
		v, err := jsonStyleCatalogue(json.RawMessage(e.valeur[k]))
		if err != nil {
			return "", err
		}
		if entree, err = remplacerCleNiveau1(entree, k, v); err != nil {
			return "", err
		}
	}
	nouveau := texte[:i] + entree + texte[j:]
	var c Catalogue
	if err := json.Unmarshal([]byte(nouveau), &c); err != nil {
		return "", errors.New("the catalog would no longer read: " + err.Error())
	}
	return nouveau, nil
}

// remplacerCleNiveau1: in the text of one JSON object, the value of its key k replaced (or the key added
// at its end, on a line of its own like the others), the rest of the text as it was.
func remplacerCleNiveau1(obj, k, valeur string) (string, error) {
	pos := 1
	saute := func() {
		for pos < len(obj) && strings.ContainsRune(" \t\r\n", rune(obj[pos])) {
			pos++
		}
	}
	finChaine := func(p int) int { // p on the opening quote
		for q := p + 1; q < len(obj); q++ {
			if obj[q] == '\\' {
				q++
			} else if obj[q] == '"' {
				return q + 1
			}
		}
		return -1
	}
	marge := " "
	for {
		saute()
		if pos >= len(obj) || obj[pos] == '}' {
			break
		}
		if obj[pos] == ',' {
			pos++
			continue
		}
		if obj[pos] != '"' {
			return "", errors.New("unexpected text in the entry")
		}
		debutCle := pos
		fc := finChaine(pos)
		if fc < 0 {
			return "", errors.New("unterminated key")
		}
		var nom string
		_ = json.Unmarshal([]byte(obj[pos:fc]), &nom)
		if l := strings.LastIndex(obj[:debutCle], "\n"); l >= 0 && strings.TrimSpace(obj[l+1:debutCle]) == "" {
			marge = "\n" + obj[l+1:debutCle]
		}
		pos = fc
		saute()
		if pos >= len(obj) || obj[pos] != ':' {
			return "", errors.New("no colon")
		}
		pos++
		saute()
		dv := pos
		var fv int
		switch obj[pos] {
		case '{', '[':
			fv = finObjet(obj, pos)
		case '"':
			fv = finChaine(pos)
		default:
			fv = pos
			for fv < len(obj) && !strings.ContainsRune(",}", rune(obj[fv])) {
				fv++
			}
			for fv > dv && strings.ContainsRune(" \t\r\n", rune(obj[fv-1])) {
				fv--
			}
		}
		if fv < 0 {
			return "", errors.New("unterminated value")
		}
		if nom == k {
			return obj[:dv] + valeur + obj[fv:], nil
		}
		pos = fv
	}
	fin := strings.LastIndex(obj, "}")
	cle, _ := json.Marshal(k)
	return obj[:fin] + "," + marge + string(cle) + ": " + valeur + obj[fin:], nil
}

func mapAvec(m map[string]json.RawMessage, k string, v json.RawMessage) map[string]json.RawMessage {
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	if len(v) == 0 {
		v = json.RawMessage("[]")
	}
	m[k] = v
	return m
}

// appliquerAuCode: the catalog's fixes waiting, written into a copy of Athanor's code; the others (a
// server of the user's own, a plugin's, a skill, a fault of Athanor's) are listed: they are corrected by hand.
func appliquerAuCode(kit, code string) (appliques, aVoir []string, err error) {
	cat := filepath.Join(code, "app", "catalogue.json")
	b, err := os.ReadFile(cat)
	if err != nil {
		return nil, nil, errors.New("not a copy of Athanor's code (no app/catalogue.json): " + code)
	}
	texte := string(b)
	fait := filepath.Join(dossierCorrections(kit), "appliquees")
	rapports := lireRapports(kit)
	sort.Slice(rapports, func(i, j int) bool { return rapports[i].Fichier < rapports[j].Fichier }) // the oldest first: the newest wins
	var deplacer []string
	for _, r := range rapports {
		if r.Genre != "mcp" || r.Catalogue == "" || r.Apres == nil || r.Plugin != "" {
			aVoir = append(aVoir, r.Fichier)
			continue
		}
		n, e := corrigerEntreeCatalogue(texte, r)
		if e != nil {
			aVoir = append(aVoir, r.Fichier+" ("+e.Error()+")")
			continue
		}
		texte = n
		appliques = append(appliques, r.Catalogue+" ("+r.Systeme+")")
		deplacer = append(deplacer, r.Fichier)
		if r.Athanor != "" {
			aVoir = append(aVoir, r.Fichier+" (Athanor's integration: "+court(r.Athanor, 200)+")")
		}
	}
	if len(appliques) > 0 {
		if err := ecrireAtomique(cat, []byte(texte), 0o644); err != nil {
			return nil, aVoir, err
		}
		_ = os.MkdirAll(fait, 0o755)
		for _, f := range deplacer {
			_ = os.Rename(filepath.Join(dossierCorrections(kit), f), filepath.Join(fait, f))
		}
	}
	return appliques, aVoir, nil
}

func lancerCorrectionsExt(action, kit string, args []string, w io.Writer) int {
	switch action {
	case "corrections":
		b, _ := json.Marshal(lireRapports(kit))
		fmt.Fprintln(w, string(b))
		return 0
	case "appliquer-corrections":
		if len(args) < 1 {
			fmt.Fprintln(w, "usage: athanor extensions appliquer-corrections KIT FOLDER_OF_ATHANORS_CODE")
			return 2
		}
		a, v, err := appliquerAuCode(kit, abspathOS(args[0]))
		if err != nil {
			fmt.Fprintln(w, err.Error())
			return 1
		}
		if len(a) == 0 {
			fmt.Fprintln(w, "No fix of the verified catalog waiting.")
		} else {
			fmt.Fprintln(w, "Written into app/catalogue.json: "+strings.Join(a, ", ")+". Commit and push it.")
		}
		for _, x := range v {
			fmt.Fprintln(w, "To look at by hand: logs/corrections/"+x)
		}
		return 0
	}
	return 2
}
