package main

// "Paste" on the Vault page: a pasted text (a .env block, an MCP config, a bare token, a PEM key or
// certificate) becomes proposals (name, family, type, scope, hint), never the value. Saving sends the
// pasted text back: the server analyses it again and stores the proposals the user kept.

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// PropositionCollee: one entry found in a pasted text; the value stays on the server side.
type PropositionCollee struct {
	Nom     string `json:"nom"`
	Famille string `json:"famille"`
	Type    string `json:"type"`
	Portee  string `json:"portee"`
	Indice  string `json:"indice"`
	valeur  string
}

// prefixesJeton: the token formats recognised by their prefix, the type and the name proposed.
var prefixesJeton = []struct {
	re       *regexp.Regexp
	typ, nom string
}{
	{regexp.MustCompile(`^(ghp_|gho_|github_pat_)[A-Za-z0-9_]{16,}$`), "github", "GITHUB_TOKEN"},
	{regexp.MustCompile(`^(sk-ant-)[A-Za-z0-9_-]{16,}$`), "anthropic", "ANTHROPIC_API_KEY"},
	{regexp.MustCompile(`^(sk-)[A-Za-z0-9_-]{16,}$`), "openai", "OPENAI_API_KEY"},
	{regexp.MustCompile(`^((?:sk|rk)_(?:test|live)_)[A-Za-z0-9]{8,}$`), "stripe", "STRIPE_SECRET_KEY"},
	{regexp.MustCompile(`^(pk_(?:test|live)_)[A-Za-z0-9]{8,}$`), "stripe", "STRIPE_PUBLISHABLE_KEY"},
	{regexp.MustCompile(`^(AKIA)[0-9A-Z]{16}$`), "aws", "AWS_ACCESS_KEY_ID"},
}

// typesParNom: the type read from a variable's name when the value has no known prefix.
var typesParNom = []struct{ mot, typ string }{
	{"CLOUDFLARE", "cloudflare"}, {"CF", "cloudflare"}, {"GITHUB", "github"}, {"GH", "github"}, {"AWS", "aws"},
	{"ANTHROPIC", "anthropic"}, {"CLAUDE", "anthropic"}, {"OPENAI", "openai"}, {"STRIPE", "stripe"}, {"SSH", "ssh"},
	{"PASSWORD", "mot_de_passe"}, {"PASSWD", "mot_de_passe"}, {"PWD", "mot_de_passe"},
}

var (
	reLigneEnv    = regexp.MustCompile(`^(?:export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$`)
	rePEM         = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]+)-----`)
	reURLIdentite = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://(?:[^/@\s]*:[^/@\s]+@|\S*[?&](?i:password|passwd|pass|pwd|secret|token)=[^&\s])`) // postgres://user:pass@host, redis://:pass@host, …?password=
	reMotsNom     = regexp.MustCompile(`[A-Za-z0-9]+`)
)

// typeJeton: the type and proposed name of a value by its prefix, or "" when unknown.
func typeJeton(v string) (typ, nom, prefixe string) {
	for _, p := range prefixesJeton {
		if m := p.re.FindStringSubmatch(v); m != nil {
			return p.typ, p.nom, m[1]
		}
	}
	return "", "", ""
}

// indiceValeur: what a proposal shows of a secret, its known prefix and its last 2 characters.
func indiceValeur(v, prefixe string) string {
	if len(v) < 12 {
		return "…"
	}
	return prefixe + "…" + v[len(v)-2:]
}

// proposerNomValeur: the proposal for NAME=value; nil for a placeholder.
func proposerNomValeur(nom, v string) *PropositionCollee {
	if reFactice.MatchString(strings.TrimSpace(v)) {
		return nil
	}
	typ, _, prefixe := typeJeton(v)
	// a value with a known token prefix or credentials in a URL is a secret whatever its name
	if !estNomSecret(nom) && typ == "" && !reURLIdentite.MatchString(v) {
		return &PropositionCollee{Nom: nom, Famille: "variable", Type: "autre", Portee: "code", Indice: tronquerRunes(v, 60), valeur: v}
	}
	if typ == "" {
		typ = "jeton"
		mots := reMotsNom.FindAllString(strings.ToUpper(nom), -1)
	nom:
		for _, t := range typesParNom {
			for _, m := range mots {
				if m == t.mot {
					typ = t.typ
					break nom
				}
			}
		}
		if reURLIdentite.MatchString(v) && typ == "jeton" {
			typ = "mot_de_passe"
		}
	}
	return &PropositionCollee{Nom: nom, Famille: "secret", Type: typ, Portee: typesCoffre[typ], Indice: indiceValeur(v, prefixe), valeur: v}
}

// sansGuillemets: a .env value without its quotes, or without its trailing " # comment".
func sansGuillemets(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// analyserColle: the proposals for a pasted text, in the order of the text.
func analyserColle(texte string) []PropositionCollee {
	texte = strings.TrimSpace(strings.ReplaceAll(texte, "\r\n", "\n"))
	var out []PropositionCollee
	ajouter := func(p *PropositionCollee) {
		if p != nil {
			out = append(out, *p)
		}
	}
	if texte == "" {
		return nil
	}
	// a PEM block: an SSH or TLS private key, or a certificate
	if m := rePEM.FindStringSubmatch(texte); m != nil {
		p := PropositionCollee{Nom: "SSH_PRIVATE_KEY", Famille: "secret", Type: "ssh", Indice: m[1], valeur: texte}
		switch {
		case strings.Contains(m[1], "CERTIFICATE"):
			p.Nom, p.Type = "TLS_CERTIFICATE", "certificat"
		case !strings.Contains(m[1], "PRIVATE KEY"):
			p.Nom, p.Type = "PEM", "certificat"
		}
		p.Portee = typesCoffre[p.Type]
		return []PropositionCollee{p}
	}
	// an MCP configuration: its keys (env, headers), as the Extensions page imports them
	if strings.HasPrefix(texte, "{") {
		if serveurs, err := lireConfigCollee(texte, "serveur"); err == nil {
			vus := map[string]bool{}
			for _, c := range serveurs {
				for nom, v := range separerSecrets(&c) {
					if !vus[nom] {
						vus[nom] = true
						ajouter(proposerNomValeur(nom, v)) // separerSecrets keeps only secret names
					}
				}
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Nom < out[j].Nom }) // out of a map: a stable order
			return out
		}
	}
	// a .env block or `export NAME=value` lines
	lignes := strings.Split(texte, "\n")
	env := 0
	for _, l := range lignes {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if m := reLigneEnv.FindStringSubmatch(l); m != nil && !strings.HasPrefix(m[2], "=") { // not a base64 "…=="
			env++
			ajouter(proposerNomValeur(m[1], sansGuillemets(m[2])))
		}
	}
	if env > 0 {
		return out
	}
	// a bare token, else the whole text as a token
	if reFactice.MatchString(texte) {
		return nil
	}
	typ, nom, prefixe := typeJeton(texte)
	if typ == "" {
		typ, nom = "jeton", "TOKEN"
	}
	return []PropositionCollee{{Nom: nom, Famille: "secret", Type: typ, Portee: typesCoffre[typ], Indice: indiceValeur(texte, prefixe), valeur: texte}}
}

// choixColle: one proposal kept by the user, with the metadata it may have changed.
type choixColle struct {
	I      int    `json:"i"`
	Nom    string `json:"nom"`
	Type   string `json:"type"`
	Portee string `json:"portee"`
	Note   string `json:"note"`
}

// apiCoffreColler: POST /api/coffre/coller {texte} → the proposals; {texte, enregistrer:[…]} → the entries
// added (the text is analysed again here, the page never holds a value apart from the pasted text).
func (s *Serveur) apiCoffreColler(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	var c struct {
		Texte       string       `json:"texte"`
		Enregistrer []choixColle `json:"enregistrer"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxValeurCoffre)
	if err := lireCorps(r, &c); err != nil {
		s.erreurCoffre(w, errCoffreRequete)
		return
	}
	if len(c.Texte) > maxValeurCoffre {
		s.erreurCoffre(w, errCoffreTaille)
		return
	}
	props := analyserColle(c.Texte)
	if !s.etatCoffre().MotDePasse { // infra needs a password: propose what can be saved
		for i := range props {
			if props[i].Portee == "infra" {
				props[i].Portee = "code"
			}
		}
	}
	if c.Enregistrer == nil {
		s.ecrireJSON(w, map[string]any{"propositions": props})
		return
	}
	// every choice is checked before the first entry is added
	for _, ch := range c.Enregistrer {
		var err error
		switch {
		case ch.I < 0 || ch.I >= len(props):
			err = errCoffreRequete
		case utf8.RuneCountInString(ch.Nom) > maxNomCoffre:
			err = errCoffreNomLong
		case utf8.RuneCountInString(ch.Note) > maxNoteCoffre:
			err = errCoffreNote
		default:
			p := props[ch.I]
			err = verifierEntree(EntreeCoffre{Nom: firstNonEmpty(strings.TrimSpace(ch.Nom), p.Nom), Famille: p.Famille,
				Type: firstNonEmpty(ch.Type, p.Type), Portee: firstNonEmpty(ch.Portee, p.Portee)})
		}
		if err != nil {
			s.erreurCoffre(w, err)
			return
		}
	}
	ajoutees := []EntreeCoffre{}
	for _, ch := range c.Enregistrer {
		p := props[ch.I]
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: firstNonEmpty(strings.TrimSpace(ch.Nom), p.Nom), Famille: p.Famille,
			Type: firstNonEmpty(ch.Type, p.Type), Note: strings.TrimSpace(ch.Note), Portee: firstNonEmpty(ch.Portee, p.Portee)}, p.valeur)
		if err != nil { // a closed key or a disk error: the entries added before stay, the page reloads the list
			s.erreurCoffre(w, err)
			return
		}
		ajoutees = append(ajoutees, e)
	}
	s.ecrireJSON(w, map[string]any{"entrees": ajoutees})
}
