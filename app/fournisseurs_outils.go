package main

// The Cloudflare tools of a Servers agent (M116, ARCHITECTURE.md §Périmètre DNS). A task launched for an
// environment carries the host names of its URL (SessionHub.Hotes): the tools act only on those names, in the
// zone of the environment's provider that holds them, its token read from the infra vault into memory only.
// Any other name, or a name no zone of the provider holds, gets the unknown name's answer. Reads run at once;
// a change or a destructive call runs only as a step of a plan the owner accepted (the plan names one of the
// task's servers; prod → an accord every time).

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode"
)

// outilsCloudflare: the Cloudflare tools, in the order the agent's list shows them.
var outilsCloudflare = []string{"fournisseurs", "dns_lister", "dns_ecrire", "dns_supprimer", "ssl_lire", "ssl_regler", "certificat_origine", "access_proteger", "access_retirer"}

var (
	errNomInconnu  = errors.New("unknown name: only your environment's host names, in a zone of its Cloudflare provider")
	typesDNS       = []string{"A", "AAAA", "CNAME", "TXT"}
	modesSSL       = []string{"off", "flexible", "full", "strict"}
	errSansFournis = errors.New("this environment has no Cloudflare provider: the owner chooses one in the project's Environments panel")
)

// argsCloudflare: the arguments of every Cloudflare tool (each one reads its own: argumentsDe), canonical
// once read (a plan's step is matched on them).
type argsCloudflare struct {
	Nom     string   `json:"nom,omitempty"`
	Type    string   `json:"type,omitempty"`
	Contenu string   `json:"contenu,omitempty"`
	Proxied *bool    `json:"proxied,omitempty"`
	TTL     int      `json:"ttl,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	Emails  []string `json:"emails,omitempty"`

	serveur string // certificat_origine: the accepted plan's server
}

func init() {
	parmi := func(d string, l ...string) map[string]any {
		return map[string]any{"type": "string", "description": d, "enum": append([]string{}, l...)}
	}
	nom := schemaTexte("A host name of your environment (see fournisseurs)")
	classe := parmi("Your own class of this action; the code's class is kept when higher", "lecture", "changement", "destructrice")
	type def struct {
		nom, description string
		props            map[string]any
		requis           []string
	}
	for _, d := range []def{
		{"fournisseurs", "Your environment's host names and, for its Cloudflare provider, the zones holding them and the rights its token has there.",
			map[string]any{}, nil},
		{"dns_lister", "The DNS records of your environment's host names (one name, or all of them).",
			map[string]any{"nom": schemaTexte("A host name of your environment; all of them when empty")}, nil},
		{"dns_ecrire", "Create or update a DNS record of a host name of your environment (A, AAAA and CNAME: the record of that type is replaced; TXT: added). A change: only as a step of an accepted plan.",
			map[string]any{"nom": nom, "type": parmi("Record type", typesDNS...), "contenu": schemaTexte("IPv4 (A), IPv6 (AAAA), target host (CNAME) or text (TXT)"),
				"proxied": map[string]any{"type": "boolean", "description": "Through Cloudflare's proxy (A, AAAA, CNAME; false by default, always true when the environment is protected by Access)"},
				"ttl":     map[string]any{"type": "integer", "description": "Seconds, 60 to 86400; automatic when empty"}}, []string{"nom", "type", "contenu"}},
		{"dns_supprimer", "Delete the DNS records of a host name of your environment of one type (only the one holding contenu when given). Destructive: only as a step of an accepted plan.",
			map[string]any{"nom": nom, "type": parmi("Record type", typesDNS...), "contenu": schemaTexte("Only the record holding exactly this content")}, []string{"nom", "type"}},
		{"ssl_lire", "The SSL/TLS encryption mode of the zone holding a host name of your environment.",
			map[string]any{"nom": nom}, []string{"nom"}},
		{"ssl_regler", "Set the SSL/TLS encryption mode of the zone holding a host name of your environment (off is destructive). Only as a step of an accepted plan.",
			map[string]any{"nom": nom, "mode": parmi("The mode (strict: the origin's certificate is checked)", modesSSL...)}, []string{"nom", "mode"}},
		{"certificat_origine", "Have a Cloudflare origin certificate made for a host name of your environment and installed by Athanor on the plan's server, in " + dossierTLS + "/<name>.crt and .key (the key 0600 root, never shown to you). A change: only as a step of an accepted plan on that server.",
			map[string]any{"nom": nom}, []string{"nom"}},
		{"access_proteger", "Protect a host name of your environment with Cloudflare Access: a self-hosted application whose only policy allows the given e-mail addresses (the owner's choice). A change: only as a step of an accepted plan.",
			map[string]any{"nom": nom, "emails": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The e-mail addresses allowed in (1 to 50)"}}, []string{"nom", "emails"}},
		{"access_retirer", "Remove the Cloudflare Access application of a host name of your environment: the site is open to everyone again. Destructive: only as a step of an accepted plan.",
			map[string]any{"nom": nom}, []string{"nom"}},
	} {
		d.props["classe"] = classe
		outilsHub[d.nom] = OutilHub{Description: d.description, Schema: schemaObjet(d.props, append([]string{}, d.requis...)...), Faire: faireOutilCloudflare(d.nom)}
	}
}

// classerCloudflare (pure): a Cloudflare tool's arguments read, checked and made canonical, and its class.
func classerCloudflare(outil string, brut []byte) (Classe, string, argsCloudflare, error) {
	var a argsCloudflare
	if err := lireArguments(brut, &a); err != nil {
		return 0, "", a, err
	}
	a.Nom = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(a.Nom)), ".")
	if a.Nom != "" && (len(a.Nom) > 253 || !nomHoteValide.MatchString(a.Nom)) || a.Nom == "" && outil != "fournisseurs" && outil != "dns_lister" {
		return 0, "", a, errors.New("nom: a host name of your environment")
	}
	switch outil {
	case "fournisseurs", "dns_lister", "ssl_lire":
		return ClasseLecture, "reads Cloudflare", a, nil
	case "certificat_origine":
		return ClasseChangement, "has an origin certificate made for " + a.Nom + " and installed on the plan's server", argsCloudflare{Nom: a.Nom}, nil
	case "access_proteger":
		return classerAccess(a)
	case "access_retirer":
		return ClasseDestructrice, "removes the Cloudflare Access protection of " + a.Nom + ": the site opens to everyone", argsCloudflare{Nom: a.Nom}, nil
	case "ssl_regler":
		if !slices.Contains(modesSSL, a.Mode) {
			return 0, "", a, errors.New("mode: off, flexible, full or strict")
		}
		if a.Mode == "off" {
			return ClasseDestructrice, "SSL off: visitors' traffic is no longer encrypted", a, nil
		}
		return ClasseChangement, "sets the zone's SSL mode to " + a.Mode, a, nil
	}
	if !slices.Contains(typesDNS, a.Type) {
		return 0, "", a, errors.New("type: A, AAAA, CNAME or TXT")
	}
	if outil == "dns_supprimer" {
		a.Proxied, a.TTL = nil, 0
		return ClasseDestructrice, "deletes " + a.Type + " records of " + a.Nom, a, nil
	}
	contenu := strings.TrimSpace(a.Contenu)
	switch a.Type {
	case "A", "AAAA":
		ip, err := netip.ParseAddr(contenu)
		if err != nil || ip.Zone() != "" || ip.Is4() != (a.Type == "A") || ip.Is4In6() {
			return 0, "", a, fmt.Errorf("contenu: an IPv%s address", map[string]string{"A": "4", "AAAA": "6"}[a.Type])
		}
		contenu = ip.String()
	case "CNAME":
		contenu = strings.TrimSuffix(strings.ToLower(contenu), ".")
		if len(contenu) > 253 || !nomHoteValide.MatchString(contenu) || !strings.Contains(contenu, ".") {
			return 0, "", a, errors.New("contenu: the target host name")
		}
	case "TXT":
		contenu = a.Contenu
		if contenu == "" || len(contenu) > 2048 || strings.IndexFunc(contenu, unicode.IsControl) >= 0 {
			return 0, "", a, errors.New("contenu: a text of 1 to 2048 characters, on one line")
		}
		if a.Proxied != nil && *a.Proxied {
			return 0, "", a, errors.New("proxied: a TXT record is never proxied")
		}
		a.Proxied = nil
	}
	a.Contenu = contenu
	if a.Type != "TXT" && a.Proxied == nil {
		a.Proxied = new(bool)
	}
	if a.TTL == 0 {
		a.TTL = 1 // Cloudflare's automatic
	} else if a.TTL < 60 || a.TTL > 86400 {
		return 0, "", a, errors.New("ttl: 60 to 86400 seconds")
	}
	return ClasseChangement, "writes a " + a.Type + " record of " + a.Nom, a, nil
}

// nomPerimetre: nom is one of x's host names (M286: or a name inside one, the zones of a task given API); otherwise
// the unknown name's answer, the refusal audited.
func (s *Serveur) nomPerimetre(x SessionHub, outil, nom string) error {
	if nom == "" || slices.Contains(x.Hotes, nom) || len(x.APIs) > 0 && slices.ContainsFunc(x.Hotes, func(z string) bool { return strings.HasSuffix(nom, "."+z) }) {
		return nil
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "cloudflare", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
		Action: "serveurs." + outil, Resultat: "refusé : nom hors du périmètre (" + couper(nom, 80) + ")"})
	return errNomInconnu
}

// faireOutilCloudflare: the MCP side of a Cloudflare tool: perimeter, class, plan, then the calls.
func faireOutilCloudflare(nom string) func(*Serveur, SessionHub, json.RawMessage) (string, error) {
	return func(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
		var a map[string]json.RawMessage
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("invalid arguments: %v", err)
		}
		if err := argumentsDe(nom, a); err != nil {
			return "", err
		}
		if err := s.accesServeursAgent(x); err != nil {
			return "", err
		}
		e, err := classerEtape(nom, a)
		var c argsCloudflare
		if err == nil {
			_ = json.Unmarshal(e.Arguments, &c)
			err = s.nomPerimetre(x, nom, c.Nom)
		}
		// M286: a task on API alone follows its permission mode as a server's assistant does (decisionMode); any other
		// session as before: reads run, a change is a step of an accepted plan
		m := ""
		if apiSeule(x) {
			m = modeAutorisation(x, serveurAPI)
		}
		switch {
		case err != nil:
		case m != "" && decisionMode(m, e.Classe, false) == decisionLibre:
			if e.Classe > ClasseLecture {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "cloudflare", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
					Action: "serveurs." + nom, Resultat: "accordé : mode « " + m + " » de la tâche (" + e.Classe.String() + ")"})
			}
		case m != "" || e.Classe > ClasseLecture:
			err = s.etapeCloudflare(x, &e)
		}
		if err == nil && nom == "certificat_origine" {
			var p PlanServeur
			p, err = s.lirePlan(e.Plan)
			c.serveur = p.Serveur
		}
		if err != nil {
			if !errors.Is(err, errNomInconnu) {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "cloudflare", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
					Action: "serveurs." + nom, Resultat: "refusé : " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)})
			}
			return "", err
		}
		r, err := s.executerCloudflare(x, nom, c)
		if e.Plan != "" {
			s.noterEtape(e.Plan, e.Num, err, nil)
		}
		if c.serveur != "" {
			s.noterRunbook(c.serveur, e, ResultatOutil{}, err)
		}
		res := "ok"
		if err != nil {
			res = "échec : " + err.Error()
		}
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "cloudflare", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent,
			Action: "serveurs." + nom, Resultat: couper(s.masquerSecrets(res), auditMaxOutil)})
		if err != nil {
			return "", errors.New(strings.ReplaceAll(s.masquerSecrets(err.Error()), "<", "&lt;"))
		}
		return enDonnees("cloudflare-output", fmt.Sprintf(` tool="%s"`, nom), s.masquerSecrets(enJSON(r))), nil
	}
}

// etapeCloudflare: a Cloudflare change runs only as a step of an accepted plan on one of x's servers.
func (s *Serveur) etapeCloudflare(x SessionHub, e *Etape) error {
	if projetBloque("servers", x.Mission) != "" { // R2: a paused project deploys nothing and leaves the DNS alone
		return errors.New("the project of this task is paused: nothing changes until the owner resumes it; wait, or end the task")
	}
	info, serveurs, ou := "", x.Serveurs, "one of your servers"
	if apiSeule(x) {
		serveurs, ou = []string{serveurAPI}, "\""+serveurAPI+"\""
	}
	for _, id := range serveurs {
		p, n, pourquoi, err := s.etapeAccordee(x, id, *e)
		if err != nil {
			return err
		}
		if p.ID != "" {
			e.Plan, e.Num = p.ID, n
			s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "cloudflare", Projet: x.Projet, Mission: x.Mission, Env: p.Env,
				Agent: x.Agent, Action: "serveurs." + e.Outil, Resultat: fmt.Sprintf("accordé : plan %s, étape %d (%s)", p.ID, n, e.Classe)})
			return nil
		}
		info = cmp.Or(info, pourquoi)
	}
	if info != "" {
		info = "; " + info
	}
	return fmt.Errorf("plan required: this is a %s action (%s); a change runs only as a step of a plan the owner accepted, with exactly its arguments: propose a plan (proposer_plan, serveur = %s)%s", e.Classe, e.Raison, ou, info)
}

// apiSeule (M286): x is the session of a task on the owner's API alone, with no server.
func apiSeule(x SessionHub) bool { return len(x.Serveurs) == 0 && len(x.APIs) > 0 }

// envSession: the environment x's task was launched for (zero when none).
func (s *Serveur) envSession(x SessionHub) Environnement {
	projet, env, _ := strings.Cut(x.Env, "/")
	if s.acces == nil || projet == "" {
		return Environnement{}
	}
	return s.acces.Environnements(projet)[env]
}

// fournisseurDe: the provider of x's environment, without its token (M283: the one whose token is among the
// deployment's API, else the older Fournisseur). M286: for a task given API, the sheet of one of them, the one
// holding nom's zone first.
func (s *Serveur) fournisseurDe(x SessionHub, nom string) (Fournisseur, error) {
	e := s.envSession(x)
	if len(x.APIs) > 0 {
		e = Environnement{APIs: x.APIs, URL: "https://" + nom}
	}
	if e.Fournisseur == "" && len(e.APIs) == 0 {
		return Fournisseur{}, errSansFournis
	}
	l, err := lireFournisseurs()
	if err != nil {
		return Fournisseur{}, err
	}
	f, ok := fournisseurDeploiement(e, l)
	if !ok {
		return Fournisseur{}, errSansFournis
	}
	return f, nil
}

// fournisseurEnv: the provider of x's environment and its token (read from the vault, kept in memory only).
func (s *Serveur) fournisseurEnv(x SessionHub, nom string) (Fournisseur, string, error) {
	f, err := s.fournisseurDe(x, nom)
	if err != nil {
		return Fournisseur{}, "", err
	}
	var jeton string
	if _, err = s.cleFournisseur(f.Cle); err == nil {
		jeton, err = s.coffre.Valeur(f.Cle)
	}
	if err != nil {
		return Fournisseur{}, "", fmt.Errorf("the provider's token cannot be read (%v): the owner opens the vault or checks the provider", err)
	}
	return f, jeton, nil
}

// prodTouchee: the prod environments ("project/prod (host)", every project) a Cloudflare step on nom reaches:
// the one whose host is nom, and for a zone-wide setting (ssl_regler) every prod host of the zone (C56).
func (s *Serveur) prodTouchee(x SessionHub, outil, nom string) (zone string, envs []string) {
	if s.acces == nil {
		return "", nil
	}
	if nom == "" && len(x.Hotes) > 0 {
		nom = x.Hotes[0] // the name the tool falls back to
	}
	prods := s.acces.HotesProd()
	if outil != "ssl_regler" {
		if e, ok := prods[nom]; ok {
			envs = []string{e + " (" + nom + ")"}
		}
		return "", envs
	}
	zone = nom
	if l := strings.Split(nom, "."); len(l) > 2 {
		zone = strings.Join(l[len(l)-2:], ".") // no provider zone known: its last two labels, the widest guess
	}
	if f, err := s.fournisseurDe(x, nom); err == nil {
		if z, ok := zoneDe(f, nom); ok {
			zone = z.Nom
		}
	}
	for h, e := range prods {
		if h == zone || strings.HasSuffix(h, "."+zone) {
			envs = append(envs, e+" ("+h+")")
		}
	}
	slices.Sort(envs)
	return zone, envs
}

// zoneDe: the zone of f holding nom (the longest zone name that is nom or a parent of it).
func zoneDe(f Fournisseur, nom string) (ZoneFournisseur, bool) {
	var z ZoneFournisseur
	for _, c := range f.Zones {
		if (nom == c.Nom || strings.HasSuffix(nom, "."+c.Nom)) && len(c.Nom) > len(z.Nom) {
			z = c
		}
	}
	return z, z.ID != ""
}

// enregistrementDNS: a DNS record as Cloudflare gives it (the id stays with Athanor).
type enregistrementDNS struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

func vueDNS(l []enregistrementDNS) []map[string]any {
	out := []map[string]any{}
	for _, r := range l {
		out = append(out, map[string]any{"type": r.Type, "nom": r.Name, "contenu": r.Content, "proxied": r.Proxied, "ttl": r.TTL})
	}
	return out
}

// cloudflare: one call, its answer's result decoded into v; a refusal says Cloudflare's messages.
func (s *Serveur) cloudflare(jeton, methode, chemin string, corps, v any) error {
	var r struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	code := s.appelCloudflare(jeton, methode, chemin, corps, &r)
	if code == 0 {
		return errors.New("Cloudflare could not be reached")
	}
	if code/100 != 2 || !r.Success {
		m := []string{}
		for _, e := range r.Errors {
			m = append(m, couper(e.Message, 200))
		}
		return fmt.Errorf("Cloudflare refused (%d): %s", code, strings.Join(m, "; "))
	}
	if v != nil {
		return json.Unmarshal(r.Result, v)
	}
	return nil
}

// executerCloudflare: the tool's calls for x (its arguments already checked and in its perimeter).
func (s *Serveur) executerCloudflare(x SessionHub, outil string, a argsCloudflare) (any, error) {
	if len(x.APIs) > 0 && outil == "fournisseurs" { // M286: every sheet of the task's API, whole zones
		l, err := lireFournisseurs()
		fiches := []map[string]any{}
		for _, f := range l {
			if slices.Contains(x.APIs, f.Cle) {
				fiches = append(fiches, map[string]any{"nom": f.Nom, "type": f.Type, "etat": f.Etat, "zones": f.Zones})
			}
		}
		return map[string]any{"hotes": x.Hotes, "fournisseurs": fiches}, err
	}
	if len(x.APIs) > 0 && a.Nom == "" && len(x.Hotes) > 1 {
		return nil, errors.New("give nom: one of your zones (" + strings.Join(x.Hotes, ", ") + ") or a name inside one")
	}
	f, jeton, err := s.fournisseurEnv(x, cmp.Or(a.Nom, strings.Join(x.Hotes[:min(1, len(x.Hotes))], "")))
	if err != nil {
		return nil, err
	}
	if outil == "fournisseurs" {
		zones := []map[string]any{}
		for _, z := range f.Zones {
			if slices.ContainsFunc(x.Hotes, func(h string) bool { y, _ := zoneDe(f, h); return y.ID == z.ID }) {
				zones = append(zones, map[string]any{"zone": z.Nom, "droits": z.Droits})
			}
		}
		return map[string]any{"hotes": x.Hotes, "fournisseur": map[string]any{"nom": f.Nom, "type": f.Type, "etat": f.Etat, "zones": zones}}, nil
	}
	noms := []string{a.Nom}
	if a.Nom == "" {
		noms = x.Hotes
	}
	if len(noms) == 0 { // C66
		return nil, errors.New("the environment has no URL: the owner sets it in the project's Environments panel")
	}
	lister := func(z ZoneFournisseur, nom, typ string) ([]enregistrementDNS, error) {
		var l []enregistrementDNS
		q := url.Values{"per_page": {"100"}}
		if nom != "" {
			q.Set("name", nom)
		}
		if typ != "" {
			q.Set("type", typ)
		}
		err := s.cloudflare(jeton, "GET", "/zones/"+url.PathEscape(z.ID)+"/dns_records?"+q.Encode(), nil, &l)
		return l, err
	}
	z, ok := zoneDe(f, noms[0])
	if len(noms) == 1 && !ok {
		return nil, errNomInconnu
	}
	dns := "/zones/" + url.PathEscape(z.ID) + "/dns_records"
	switch outil {
	case "certificat_origine":
		return s.certificatOrigine(x, jeton, a.Nom, a.serveur)
	case "access_proteger", "access_retirer":
		if outil == "access_proteger" { // Access only sees what goes through Cloudflare's proxy (C58)
			l, err := lister(z, a.Nom, "")
			if err != nil {
				return nil, err
			}
			l = slices.DeleteFunc(l, func(r enregistrementDNS) bool { return r.Type == "TXT" })
			if len(l) == 0 || slices.ContainsFunc(l, func(r enregistrementDNS) bool { return !r.Proxied }) {
				return nil, errors.New(a.Nom + " does not go through Cloudflare's proxy, so Access would not protect it: write its A, AAAA or CNAME record with proxied=true first (dns_ecrire in a plan)")
			}
		}
		return s.access(f, jeton, a)
	case "dns_lister":
		tous := []enregistrementDNS{}
		for _, n := range noms {
			if z, ok := zoneDe(f, n); ok {
				if len(x.APIs) > 0 && n == z.Nom {
					n = "" // M286: a zone's own name reads its whole DNS. shortcut: the first 100 records, page when a zone holds more
				}
				l, err := lister(z, n, "")
				if err != nil {
					return nil, err
				}
				tous = append(tous, l...)
			}
		}
		return map[string]any{"enregistrements": vueDNS(tous)}, nil
	case "dns_ecrire":
		force := a.Type != "TXT" && !*a.Proxied && s.envSession(x).Protection == "access"
		if force { // an Access site is never reached around the proxy (C58)
			a.Proxied = &force
		}
		l, err := lister(z, a.Nom, a.Type)
		if err != nil {
			return nil, err
		}
		if len(l) > 1 && a.Type != "TXT" { // which one to replace is not ours to guess (C67)
			return nil, fmt.Errorf("%d %s records hold %s: delete them first (dns_supprimer), then write the one you want", len(l), a.Type, a.Nom)
		}
		corps := map[string]any{"type": a.Type, "name": a.Nom, "content": a.Contenu, "ttl": a.TTL}
		if a.Proxied != nil {
			corps["proxied"] = *a.Proxied
		}
		pareil := func(r enregistrementDNS) bool {
			return r.Content == a.Contenu && (a.Type == "TXT" || r.Proxied == *a.Proxied && r.TTL == a.TTL)
		}
		var r enregistrementDNS
		res := map[string]any{"resultat": "written"}
		if force {
			res["proxied"] = "forced to true: the environment is protected by Cloudflare Access"
		}
		switch {
		case slices.ContainsFunc(l, pareil):
			res["resultat"] = "unchanged: the record already holds this"
			return res, nil
		case len(l) > 0 && a.Type != "TXT":
			err = s.cloudflare(jeton, "PUT", dns+"/"+url.PathEscape(l[0].ID), corps, &r)
		default:
			err = s.cloudflare(jeton, "POST", dns, corps, &r)
		}
		if err != nil {
			return nil, err
		}
		res["enregistrement"] = vueDNS([]enregistrementDNS{r})[0]
		return res, nil
	case "dns_supprimer":
		l, err := lister(z, a.Nom, a.Type)
		if err != nil {
			return nil, err
		}
		l = slices.DeleteFunc(l, func(r enregistrementDNS) bool { return a.Contenu != "" && r.Content != a.Contenu })
		if len(l) == 0 {
			return nil, errors.New("no record of this name and type matches: nothing deleted")
		}
		for _, r := range l {
			if err := s.cloudflare(jeton, "DELETE", dns+"/"+url.PathEscape(r.ID), nil, nil); err != nil {
				return nil, err
			}
		}
		// what was deleted, to put it back by a plan if needed
		return map[string]any{"resultat": "deleted", "supprimes": vueDNS(l)}, nil
	case "ssl_lire", "ssl_regler":
		var r struct {
			Value string `json:"value"`
		}
		ssl := "/zones/" + url.PathEscape(z.ID) + "/settings/ssl"
		if outil == "ssl_lire" {
			err = s.cloudflare(jeton, "GET", ssl, nil, &r)
		} else {
			err = s.cloudflare(jeton, "PATCH", ssl, map[string]string{"value": a.Mode}, &r)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"zone": z.Nom, "ssl": r.Value}, nil
	}
	return nil, errors.New("unknown tool")
}
