package main

// Search on the Skills & MCP page: community skills (SkillsMP, a public directory of skills published on
// GitHub) and MCP servers (the official MCP Registry). A skill found is installed through its GitHub link
// (same checks as a pasted link); a server found becomes a configuration built from what the registry
// declares (an npm or PyPI package started in the cage, or a remote address), its keys left to fill in.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	skillsmpBase = "https://skillsmp.com"
	registreMCP  = "https://registry.modelcontextprotocol.io"
)

func init() {
	if v := os.Getenv("ATHANOR_SKILLSMP"); v != "" { // tests only
		skillsmpBase = v
	}
	if v := os.Getenv("ATHANOR_MCP_REGISTRY"); v != "" {
		registreMCP = v
	}
}

type ResultatSkill struct {
	Nom         string `json:"nom"`
	Auteur      string `json:"auteur"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Etoiles     int    `json:"etoiles"`
	Installe    bool   `json:"installe"`
}

// chercherSkills asks SkillsMP (free, about 50 searches a day without a key).
func chercherSkills(q string) ([]ResultatSkill, error) {
	r, err := clientGitHub.Get(skillsmpBase + "/api/v1/skills/search?q=" + url.QueryEscape(q) + "&limit=40&sortBy=stars")
	if err != nil {
		return nil, errors.New("SkillsMP cannot be reached: check the Internet connection")
	}
	defer r.Body.Close()
	if r.StatusCode == http.StatusTooManyRequests {
		return nil, errors.New("Too many searches on SkillsMP today (about 50 a day): try again tomorrow, or paste a GitHub link")
	}
	if r.StatusCode != 200 {
		return nil, errors.New("SkillsMP answered with an error: try again later")
	}
	var brut map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&brut); err != nil {
		return nil, errors.New("SkillsMP answered with an error: try again later")
	}
	// the list sits in data.skills (sometimes data.data.skills, or skills)
	var liste []struct {
		Name        string `json:"name"`
		Author      string `json:"author"`
		Description string `json:"description"`
		GithubURL   string `json:"githubUrl"`
		Stars       int    `json:"stars"`
	}
	var trouver func(m map[string]json.RawMessage, profondeur int) bool
	trouver = func(m map[string]json.RawMessage, profondeur int) bool {
		if v, ok := m["skills"]; ok && json.Unmarshal(v, &liste) == nil {
			return true
		}
		if v, ok := m["data"]; ok && profondeur < 3 {
			var sous map[string]json.RawMessage
			if json.Unmarshal(v, &sous) == nil {
				return trouver(sous, profondeur+1)
			}
		}
		return false
	}
	trouver(brut, 0)
	out := []ResultatSkill{}
	for _, x := range liste {
		if _, err := lireLienGitHub(x.GithubURL); err != nil || strings.TrimSpace(x.Name) == "" {
			continue // only skills published on GitHub can be installed
		}
		out = append(out, ResultatSkill{Nom: x.Name, Auteur: x.Author, Description: court(x.Description, 400), URL: x.GithubURL, Etoiles: x.Stars})
	}
	return out, nil
}

// ----------------------------------------------------------------------------- MCP registry

type regVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     string `json:"default"`
	Value       string `json:"value"`
	IsRequired  bool   `json:"isRequired"`
	IsSecret    bool   `json:"isSecret"`
}

type regArgument struct {
	Type       string `json:"type"` // positional, named
	Name       string `json:"name"`
	Value      string `json:"value"`
	ValueHint  string `json:"valueHint"`
	Default    string `json:"default"`
	IsRequired bool   `json:"isRequired"`
}

type regServeur struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Repository  struct {
		URL string `json:"url"`
	} `json:"repository"`
	WebsiteURL string `json:"websiteUrl"`
	Packages   []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
		RuntimeHint  string `json:"runtimeHint"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
		EnvironmentVariables []regVariable `json:"environmentVariables"`
		PackageArguments     []regArgument `json:"packageArguments"`
	} `json:"packages"`
	Remotes []struct {
		Type    string        `json:"type"`
		URL     string        `json:"url"`
		Headers []regVariable `json:"headers"`
	} `json:"remotes"`
}

type ResultatMCP struct {
	Nom         string   `json:"nom"`   // registry name (io.github.owner/server)
	Titre       string   `json:"titre"` // what is shown
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Depot       string   `json:"depot"`
	Genre       string   `json:"genre"` // npm, pypi, remote
	Installable bool     `json:"installable"`
	Raison      string   `json:"raison,omitempty"`
	Cles        []string `json:"cles,omitempty"`
	Installe    bool     `json:"installe"`
}

var reNomVariable = regexp.MustCompile(`[^A-Za-z0-9]+`)

func nomVariable(s string) string {
	return strings.Trim(strings.ToUpper(reNomVariable.ReplaceAllString(s, "_")), "_")
}

var reIndicationDossier = regexp.MustCompile(`(?i)(dir|path|folder|root|workspace|project)`)

// configDepuisRegistre builds the Claude Code configuration of a server from its registry entry:
// a remote address when there is a usable one, else an npm (npx) or PyPI (uvx) package run in the cage.
// Keys become ${NAME} to fill in on the page; optional ones are listed apart.
func configDepuisRegistre(s regServeur) (ConfigMCP, []string, string, error) {
	var optionnels []string
	valeurVar := func(v regVariable, nom string) (string, bool) {
		switch {
		case v.Value != "" && !strings.Contains(v.Value, "{"):
			return v.Value, true
		case v.IsSecret || (v.IsRequired && v.Default == ""):
			if !v.IsRequired {
				optionnels = append(optionnels, nom)
			}
			if v.Value != "" { // "Bearer {token}" → "Bearer ${NAME}"
				return regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(v.Value, "${"+nom+"}"), true
			}
			if strings.EqualFold(v.Name, "authorization") {
				return "Bearer ${" + nom + "}", true
			}
			return "${" + nom + "}", true
		case v.Default != "":
			return v.Default, true
		}
		return "", false
	}
	for _, rm := range s.Remotes {
		if (rm.Type != "streamable-http" && rm.Type != "sse" && rm.Type != "http") || strings.Contains(rm.URL, "{") || !strings.HasPrefix(rm.URL, "https://") {
			continue
		}
		c := ConfigMCP{Type: "http", URL: rm.URL}
		if rm.Type == "sse" {
			c.Type = "sse"
		}
		for _, h := range rm.Headers {
			nom := nomVariable(h.Name)
			if strings.EqualFold(h.Name, "authorization") {
				nom = "AUTHORIZATION_TOKEN"
			}
			if v, ok := valeurVar(h, nom); ok {
				if c.Headers == nil {
					c.Headers = map[string]string{}
				}
				c.Headers[h.Name] = v
			}
		}
		return c, optionnels, "remote", nil
	}
	raison := "Started another way (Docker, .NET…): add it by hand from its README"
	for _, p := range s.Packages {
		if p.Transport.Type != "" && p.Transport.Type != "stdio" {
			continue
		}
		c := ConfigMCP{Type: "stdio"}
		switch p.RegistryType {
		case "npm":
			id := p.Identifier
			if p.Version != "" && p.Version != "latest" {
				id += "@" + p.Version
			}
			c.Command, c.Args = "npx", []string{"-y", id}
		case "pypi":
			id := p.Identifier
			if p.Version != "" && p.Version != "latest" {
				id += "==" + p.Version
			}
			c.Command, c.Args = "uvx", []string{id}
		default:
			continue
		}
		ok := true
		for _, a := range p.PackageArguments {
			v := a.Value
			if v == "" {
				v = a.Default
			}
			if v == "" && a.IsRequired {
				if reIndicationDossier.MatchString(a.ValueHint + " " + a.Name) {
					v = "{PROJECT}" // the folder the server works on: the project (a worker's worktree)
				} else {
					ok, raison = false, "Needs a setting the registry does not give, add it by hand from its README: "+firstNonEmpty(a.ValueHint, a.Name)
					break
				}
			}
			if v == "" {
				continue
			}
			if a.Type == "named" && a.Name != "" {
				c.Args = append(c.Args, a.Name, v)
			} else {
				c.Args = append(c.Args, v)
			}
		}
		if !ok {
			continue
		}
		for _, e := range p.EnvironmentVariables {
			if v, ok := valeurVar(e, nomVariable(e.Name)); ok {
				if c.Env == nil {
					c.Env = map[string]string{}
				}
				c.Env[e.Name] = v
			}
		}
		return c, optionnels, p.RegistryType, nil
	}
	return ConfigMCP{}, nil, "", errors.New(raison)
}

// chercherMCP asks the official MCP Registry; one entry per server (its latest version).
func chercherMCP(q string) ([]regServeur, error) {
	var corps struct {
		Servers []json.RawMessage `json:"servers"`
	}
	ok := false
	for _, chemin := range []string{"/v0.1/servers", "/v0/servers"} {
		r, err := clientGitHub.Get(registreMCP + chemin + "?search=" + url.QueryEscape(q) + "&limit=60")
		if err != nil {
			return nil, errors.New("The MCP Registry cannot be reached: check the Internet connection")
		}
		if r.StatusCode == 200 && json.NewDecoder(r.Body).Decode(&corps) == nil {
			r.Body.Close()
			ok = true
			break
		}
		r.Body.Close()
	}
	if !ok {
		return nil, errors.New("The MCP Registry answered with an error: try again later")
	}
	type entree struct {
		s       regServeur
		dernier bool
	}
	parNom := map[string]entree{}
	var ordre []string
	for _, brut := range corps.Servers {
		var e struct {
			Server *regServeur `json:"server"`
			Meta   map[string]struct {
				Status   string `json:"status"`
				IsLatest bool   `json:"isLatest"`
			} `json:"_meta"`
		}
		_ = json.Unmarshal(brut, &e)
		srv := e.Server
		if srv == nil { // older answers: the server itself
			srv = &regServeur{}
			if json.Unmarshal(brut, srv) != nil {
				continue
			}
		}
		if srv.Name == "" {
			continue
		}
		dernier, retire := false, false
		for _, m := range e.Meta {
			dernier = dernier || m.IsLatest
			retire = retire || m.Status == "deleted" || m.Status == "deprecated"
		}
		if retire {
			continue
		}
		if avant, vu := parNom[srv.Name]; !vu {
			ordre = append(ordre, srv.Name)
			parNom[srv.Name] = entree{*srv, dernier}
		} else if !avant.dernier {
			parNom[srv.Name] = entree{*srv, dernier}
		}
	}
	out := []regServeur{}
	for _, n := range ordre {
		out = append(out, parNom[n].s)
	}
	return out, nil
}

func titreRegistre(s regServeur) string {
	if strings.TrimSpace(s.Title) != "" {
		return s.Title
	}
	n := s.Name
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	return n
}

func (s *Serveur) apiExtRecherche(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Type string `json:"type"`
		Q    string `json:"q"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Q) == "" || len(c.Q) > 200 {
		s.erreur(w, http.StatusBadRequest, "Type what you are looking for, e.g. front-end")
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	b := lireBiblio(k)
	extMu.Unlock()
	if c.Type == "mcp" {
		serveurs, err := chercherMCP(strings.TrimSpace(c.Q))
		if err != nil {
			s.erreur(w, http.StatusBadGateway, err.Error())
			return
		}
		sources := map[string]bool{}
		for _, x := range b.MCP {
			sources[x.Source] = true
		}
		out := []ResultatMCP{}
		for _, sv := range serveurs {
			conf, _, genre, err := configDepuisRegistre(sv)
			res := ResultatMCP{Nom: sv.Name, Titre: titreRegistre(sv), Description: court(sv.Description, 400), Version: sv.Version,
				Depot: firstNonEmpty(sv.Repository.URL, sv.WebsiteURL), Genre: genre, Installable: err == nil, Installe: sources["registry:"+sv.Name]}
			if err != nil {
				res.Raison = err.Error()
			} else {
				res.Cles = secretsRequis(conf)
			}
			out = append(out, res)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Installable && !out[j].Installable })
		s.ecrireJSON(w, map[string]any{"resultats": out})
		return
	}
	skills, err := chercherSkills(strings.TrimSpace(c.Q))
	if err != nil {
		s.erreur(w, http.StatusBadGateway, err.Error())
		return
	}
	for i := range skills {
		_, skills[i].Installe = b.Skills[identExt(skills[i].Nom)]
	}
	s.ecrireJSON(w, map[string]any{"resultats": skills})
}

// apiExtRegistre installs a server found in the MCP Registry.
func (s *Serveur) apiExtRegistre(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Nom string `json:"nom"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Nom) == "" {
		s.erreur(w, http.StatusBadRequest, "Unknown")
		return
	}
	serveurs, err := chercherMCP(c.Nom)
	if err != nil {
		s.erreur(w, http.StatusBadGateway, err.Error())
		return
	}
	var sv *regServeur
	for i := range serveurs {
		if serveurs[i].Name == c.Nom {
			sv = &serveurs[i]
		}
	}
	if sv == nil {
		s.erreur(w, http.StatusNotFound, "Unknown")
		return
	}
	conf, optionnels, _, err := configDepuisRegistre(*sv)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	k := s.kitCourant()
	extMu.Lock()
	defer extMu.Unlock()
	b := lireBiblio(k)
	source := "registry:" + sv.Name
	id := identExt(titreRegistre(*sv))
	if id == "" || id == "unreal-mcp" {
		id = identExt(strings.ReplaceAll(sv.Name, "/", "-"))
	}
	if x, existe := b.MCP[id]; existe && x.Source != source {
		id = identExt(strings.ReplaceAll(sv.Name, "/", "-"))
	}
	if !reIDExt.MatchString(id) {
		s.erreur(w, http.StatusBadRequest, "Invalid name")
		return
	}
	reglages := separerReglages(&conf)
	b.MCP[id] = MCPInstalle{Nom: titreRegistre(*sv), Description: court(sv.Description, 400), Source: source, Config: conf, Optionnels: optionnels, Reglages: reglages, Ajoute: maintenantISO()}
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "“{0}” installed. Switch it on for the projects that need it.", titreRegistre(*sv))
}
