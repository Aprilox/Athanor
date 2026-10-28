package main

// An MCP server of the library edited in the app (Skills & MCP › View / edit): its configuration, its
// settings (name → default; the value set is in the field below the server) and the program the pilot
// opens for it before each cycle ("ouvrir", outil_ouvrir.go), as one JSON text. A server of the catalog
// keeps the catalog's opening until the user changes it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type docMCP struct {
	Config   ConfigMCP         `json:"config"`
	Reglages map[string]string `json:"reglages,omitempty"`
	Ouvrir   *OuvrirMCP        `json:"ouvrir,omitempty"`
}

func ouvrirCatalogue(cat Catalogue, id string) *OuvrirMCP {
	if id == "" {
		return nil
	}
	for _, m := range cat.MCP {
		if m.ID == id {
			return m.Ouvrir
		}
	}
	return nil
}

// ouvrirEffectif: the library's opening, else the catalog's.
func ouvrirEffectif(x MCPInstalle, cat Catalogue) *OuvrirMCP {
	if x.Ouvrir != nil {
		if x.Ouvrir.Programme == "" {
			return nil // none, chosen by the user
		}
		return x.Ouvrir
	}
	return ouvrirCatalogue(cat, x.Catalogue)
}

func jsonLisible(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

func texteMCP(x MCPInstalle, cat Catalogue) string {
	return jsonLisible(docMCP{x.Config, x.Reglages, ouvrirEffectif(x, cat)})
}

// champsMCP: the fields shown below a server: its keys and settings, those of its opening included, and
// every setting it has even when nothing uses it yet.
func champsMCP(x MCPInstalle, cat Catalogue) []string {
	vus := map[string]bool{}
	for _, n := range secretsRequis(x.Config) {
		vus[n] = true
	}
	for n := range x.Reglages {
		vus[n] = true
	}
	if o := ouvrirEffectif(x, cat); o != nil {
		for _, t := range append([]string{o.Programme, o.Processus, o.Port}, o.Args...) {
			for _, m := range reVarSecret.FindAllStringSubmatch(t, -1) {
				if !variablesPlugin[m[1]] {
					vus[m[1]] = true
				}
			}
		}
	}
	out := make([]string, 0, len(vus))
	for n := range vus {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

var reNomReglage = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)

// lireDocMCP: the edited text, checked; the secret values pasted in it moved out (separerSecrets).
func lireDocMCP(texte string, avant MCPInstalle) (docMCP, map[string]string, error) {
	var d docMCP
	dec := json.NewDecoder(strings.NewReader(texte))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, nil, errors.New("Invalid JSON: " + err.Error())
	}
	c := &d.Config
	c.Type = strings.TrimSpace(c.Type)
	if err := verifierConfig(*c); err != nil {
		return d, nil, err
	}
	if c.URL != "" {
		if c.Type == "" {
			c.Type = "http"
		}
		if c.Type != "http" && c.Type != "sse" {
			return d, nil, errors.New(`A server with a "url" has the type "http" or "sse".`)
		}
	} else {
		if c.Type == "" {
			c.Type = "stdio"
		}
		// "hote" (outside the cage): the verified catalog's own, kept as it is, never one written here
		hote := c.Type == "hote" && avant.Catalogue != "" && avant.Config.Type == "hote" && c.Command == avant.Config.Command &&
			strings.Join(c.Args, "\x00") == strings.Join(avant.Config.Args, "\x00") && len(c.Env) == 0
		if c.Type == "hote" && !hote {
			return d, nil, errors.New(`A program outside the cage ("hote") comes from the verified catalog only, as it is: its command and arguments cannot change.`)
		}
		if c.Type != "stdio" && c.Type != "windows" && !hote {
			return d, nil, errors.New(`A server started by a command has the type "stdio" (in a cage) or "windows" (a Windows program).`)
		}
		if c.Type == "stdio" && avant.Config.Type != "windows" && estProgrammeWindows(*c) {
			return d, nil, errors.New(`This server is a Windows program: it would run on Windows, outside the cage, with your rights. Set "type": "windows" to keep it.`)
		}
	}
	for n, v := range d.Reglages {
		if !reNomReglage.MatchString(n) || strings.ContainsAny(v, "\n\r") {
			return d, nil, errors.New("Invalid setting: " + n)
		}
		if estNomSecret(n) {
			return d, nil, errors.New("A key is not a setting (write ${NAME} in the configuration and fill in its field below the server): " + n)
		}
	}
	if len(d.Reglages) == 0 {
		d.Reglages = nil
	}
	if err := verifierOuvrir(d.Ouvrir); err != nil {
		return d, nil, err
	}
	return d, separerSecrets(c), nil
}

func (s *Serveur) apiExtMCPModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID    string `json:"id"`
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil || !reIDExt.MatchString(c.ID) {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b, secrets := lireBiblio(k), lireSecrets(k)
	x, ok := b.MCP[c.ID]
	if !ok {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	if x.Plugin != "" {
		s.erreur(w, http.StatusBadRequest, "This server comes with a plugin: it is set by the plugin.")
		return
	}
	d, valeurs, err := lireDocMCP(c.Texte, x)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	x.Config, x.Reglages, x.Ouvrir = d.Config, d.Reglages, d.Ouvrir
	if duCat := ouvrirCatalogue(lireCatalogue(), x.Catalogue); duCat != nil {
		if d.Ouvrir == nil {
			x.Ouvrir = &OuvrirMCP{} // removed by the user: an empty opening says "none", the catalog's is not used
		} else if jsonLisible(d.Ouvrir) == jsonLisible(duCat) {
			x.Ouvrir = nil // the catalog's, kept up to date with it
		}
	}
	if len(valeurs) > 0 {
		if secrets[c.ID] == nil {
			secrets[c.ID] = map[string]string{}
		}
		for n, v := range valeurs {
			secrets[c.ID][n] = v
		}
		if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	b.MCP[c.ID] = x
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Server saved. It applies from the next cycle.")
}
