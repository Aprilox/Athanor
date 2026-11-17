package main

// Actions of the home conversation (M197): the assistant never acts; it proposes one action per answer between
// <action> and </action>. The Go computes its card (what, on what, with which accesses, why it cannot run as it
// is), the user launches, changes or refuses it, and every decision goes to the audit log (who, what, when, result).

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ActionChat: what the assistant proposes (and the user may change on the card before launching it).
type ActionChat struct {
	Type    string `json:"type"`              // projet (a new one, or an existing one's deployment) | pilote | demande | remarque | mission
	Projet  string `json:"projet,omitempty"`  // the project it acts on (projet: the new project's name)
	Dossier string `json:"dossier,omitempty"` // projet: its folder (empty: next to the latest project)
	Titre   string `json:"titre,omitempty"`   // demande: its title
	Texte   string `json:"texte,omitempty"`   // the request, the remark, the mission's sentence
	// M198: agent | mcp (a <agent> or <mcp> card of api_chat.go, added to the library), attribuer (switched on in projects)
	Projets      []string `json:"projets,omitempty"` // agent, mcp, attribuer: the projects (names) it is switched on in
	Nom          string   `json:"nom,omitempty"`     // agent, mcp: its name; attribuer: the sub-agent's name or the server's id
	Element      string   `json:"element,omitempty"` // attribuer: agent | mcp
	Description  string   `json:"description,omitempty"`
	Consignes    string   `json:"consignes,omitempty"` // agent: its instructions
	Modele       string   `json:"modele,omitempty"`    // agent: "" | eco | fort
	LectureSeule bool     `json:"lecture_seule,omitempty"`
	Source       string   `json:"source,omitempty"` // mcp: the page it comes from
	Config       string   `json:"config,omitempty"` // mcp: its configuration's JSON, as the card shows it
	Windows      bool     `json:"windows,omitempty"`
	// mcp: the user's separate confirmation that a Windows server runs outside the cage with their rights
	ConfirmeWindows bool     `json:"confirme_windows,omitempty"`
	Fichiers        []string `json:"fichiers,omitempty"` // mcp: the files Claude wrote (names; their content comes from the message)
	// M295, projet: its one deployment (servers and vault entries by id), only when the request uses one, and its
	// own permission mode ("": the one of Settings). With the name of an existing project: only that deployment.
	Deploiement *DeploiementDemande `json:"deploiement,omitempty"`
	Permission  string              `json:"permission,omitempty"`
}

// CarteActionChat: the card the page shows, computed here (Quoi and Acces are i18n keys, Sur is data).
type CarteActionChat struct {
	ActionChat
	Quoi   string   `json:"quoi"`
	Sur    string   `json:"sur"`
	Acces  []string `json:"acces"`
	Erreur string   `json:"erreur,omitempty"` // why it cannot be launched as it is (i18n key)
	Etat   string   `json:"etat,omitempty"`   // "": waits for the user; lancee | refusee
	// what launching it gave (an i18n key and its arguments)
	Resultat string            `json:"resultat,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Noms     map[string]string `json:"noms,omitempty"` // projet: the names of the card's servers and APIs, by id
	// projet: the card as it is shown; Launch sends it back (an older view launches nothing)
	Empreinte string `json:"empreinte,omitempty"`
}

var reAction = regexp.MustCompile(`(?s)<action>(.*?)</action>`)

// verrouActionsChat: a double click launches a card once.
var verrouActionsChat sync.Mutex

const maxTexteAction = 20000

const consigneActionsAccueil = `

Actions: you never act yourself. When the user wants one of these done, propose it as a card: write it once, at
the end of your answer, between <action> and </action>, as JSON, and say in one sentence that they launch, change
or refuse it on the card (Athanor shows what it does, on what and with which accesses; nothing runs before the
user launches it):
- {"type": "projet", "projet": "the new project's name (letters, digits, spaces, dashes)", "texte": "its complete request, in the user's words", "dossier": "its full folder path, or empty: Athanor picks one next to the other projects", "deploiement": {"serveurs": ["the servers the user named, by their name"], "apis": ["the API keys of the vault the user named, by their name"], "url": "the site's address, or empty", "plan": "how to deploy it, in the user's words, or empty", "moment": "jalon" (after each milestone) or "livraison" (at delivery, the default)}, "permission": "manuel", "auto", "modifications" or "ignorer"}: create a project and start its pilot. "deploiement" only when the request names a server, an API key or an address (a project built on this PC only has none), and never a server or a key the user did not name; "permission" only when the user asks for a mode. With the name of an existing project and "deploiement" ("deploy @site on @server"): set that project's deployment and nothing else, without "texte" nor "dossier";
- {"type": "pilote", "projet": "an existing project"}: start the pilot of a project;
- {"type": "demande", "projet": "an existing project", "titre": "a short title", "texte": "the request or the task, self-contained"}: add a request (a task) to a project's queue, its pilot takes it after the current one;
- {"type": "remarque", "projet": "an existing project", "texte": "the remark, short and self-contained"}: leave a remark its pilot reads at its next cycle;
- {"type": "mission", "texte": "the mission in one sentence, naming its projects and servers"}: prepare a mission (servers, deployments, several projects); Athanor's orchestrator then prepares its own card.
- {"type": "attribuer", "element": "agent" or "mcp", "nom": "the sub-agent's name or the MCP server's id, as in your library", "projets": ["exact project names"]}: switch a sub-agent or an MCP server of the library on in projects ("put it on my-shop").
One action per answer, with the exact names of the projects below; a question gets an answer and no card.

Sub-agents: when the user wants a specialist sub-agent for their pilots, explain what it would do, ask few
questions (what it does, when the lead calls it, what it must never do, cheap or strong model, whether it may
change files, its name), then propose it once, at the end of your answer, between <agent> and </agent>, as JSON:
{"nom": "lowercase letters, digits and dashes, 2 to 31", "description": "when the lead should call it, 400
characters at most, in the user's language", "consignes": "its complete instructions, 6000 characters at most",
"modele": "" or "eco" (cheap) or "fort" (strongest), "lecture_seule": true when it only reads and reports,
"projets": ["the projects to switch it on in, when the user named them"]}. Names taken: architect, creator,
developer, tester, critic, executor, scout.

MCP servers: when the user wants a pilot to drive a program or a service through MCP, find an existing server
(with Internet: the official MCP Registry, the program's documentation, GitHub) or write one (a single Python
file run by uv, dependencies inline, PEP 723), then propose it once, at the end of your answer, between <mcp> and
</mcp>, as JSON: {"nom": "a short name", "description": "what it lets the pilot do", "source": "its page or
empty", "config": {"command": "…", "args": […], "env": {…}} or {"url": "…", "headers": {…}}, "windows": true only
when it must run as a Windows program (outside the cage, with the user's rights), "fichiers": {"server.py": "the
complete file"} only for a server you wrote (its config then {"command": "uv", "args": ["run", "--script",
"${ATHANOR_MCP_DIR}/server.py"]}), "projets": ["the projects to switch it on in, when the user named them"]}.
Never a key or a password in it: "${NAME}" in its place, the user fills it in on the Skills & MCP page.
You never write team files or MCP configurations yourself: only the card the user validates changes them.

The state of Athanor now (projects, modules and your library, from Athanor itself):
`

// texteEtatHub: the projects and the modules' elements, one line each, for the assistant (no value of the vault).
func (s *Serveur) texteEtatHub() string {
	_, el := s.vueHub()
	var b strings.Builder
	for _, e := range el {
		fmt.Fprintf(&b, "- [%s] %s: %s", e.Module, e.Titre, e.Etat)
		if a := e.Avancement; a != nil && a.Total > 0 {
			fmt.Fprintf(&b, ", %d/%d", a.Fait, a.Total)
		}
		if e.Activite != "" {
			b.WriteString(", " + e.Activite)
		}
		if e.Attente != nil {
			b.WriteString(", waits for the owner")
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		b.WriteString("(nothing yet: no project, no server)\n")
	}
	for _, m := range modulesHub() {
		if c, ok := m.(moduleContexteChat); ok {
			b.WriteString(c.ContexteChat(s))
		}
	}
	// the library (M198): names and the projects they are on, never a configuration nor a key
	k := s.kitCourant()
	sur := func(dossiers []string) string {
		var l []string
		for _, d := range dossiers {
			if p, ok := s.projetDuDossierChat(d); ok {
				l = append(l, p.Nom)
			}
		}
		if len(l) == 0 {
			return "on no project"
		}
		return "on " + strings.Join(l, ", ")
	}
	for _, a := range lireAgents(k).Agents {
		fmt.Fprintf(&b, "- [library] sub-agent %s: %s\n", a.Nom, sur(a.Projets))
	}
	choix, biblio := lireChoix(k), lireBiblio(k)
	ids := make([]string, 0, len(biblio.MCP))
	for id := range biblio.MCP {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var dossiers []string
		for cle, ch := range choix {
			if contient(ch.MCP, id) {
				dossiers = append(dossiers, cle)
			}
		}
		fmt.Fprintf(&b, "- [library] MCP server %s (%s): %s\n", id, biblio.MCP[id].Nom, sur(dossiers))
	}
	return s.masquerSecrets(b.String())
}

// lireActionChat: the action of a message of the assistant (false without one or when it cannot be read).
func lireActionChat(texte string) (ActionChat, bool) {
	brut := func(m []string) []byte {
		j := strings.TrimSpace(m[1])
		return []byte(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(j, "```json"), "```"), "```")))
	}
	if m := reAction.FindStringSubmatch(texte); m != nil {
		var a ActionChat
		if json.Unmarshal(brut(m), &a) != nil {
			return ActionChat{}, true // a card that says it cannot be read
		}
		return a, true
	}
	// M198: the <agent> and <mcp> cards of the project chat (api_chat.go), here added to the library and to projects
	if m := reAgent.FindStringSubmatch(texte); m != nil {
		var a ActionChat
		if json.Unmarshal(brut(m), &a) != nil {
			return ActionChat{}, true
		}
		a.Type = "agent"
		return a, true
	}
	if m := reMCP.FindStringSubmatch(texte); m != nil {
		var x struct {
			ActionChat
			Config   json.RawMessage   `json:"config"`
			Fichiers map[string]string `json:"fichiers"`
		}
		if json.Unmarshal(brut(m), &x) != nil {
			return ActionChat{}, true
		}
		a := x.ActionChat
		a.Type, a.Fichiers = "mcp", []string{}
		if len(x.Config) > 0 {
			var b bytes.Buffer
			if json.Indent(&b, x.Config, "", "  ") == nil {
				a.Config = b.String()
			}
		}
		for n := range x.Fichiers {
			a.Fichiers = append(a.Fichiers, n)
		}
		sort.Strings(a.Fichiers)
		return a, true
	}
	return ActionChat{}, false
}

// projetsChat: the projects named on a card (case ignored, each once): their names as Athanor knows them and their
// folders, or the first name unknown.
func (s *Serveur) projetsChat(noms []string) ([]string, []string, string) {
	l, dossiers := []string{}, []string{}
	for _, n := range noms {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		p, ok := s.projetLocalChat(n)
		if !ok {
			return nil, nil, n
		}
		if !slices.Contains(l, p.Nom) {
			l, dossiers = append(l, p.Nom), append(dossiers, p.Dossier)
		}
	}
	return l, dossiers, ""
}

// projetDuDossierChat: the project of a folder of the library (a sub-agent's projects, a key of projets.json).
func (s *Serveur) projetDuDossierChat(d string) (Projet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Projets {
		if !p.Archive && normaliserDossier(p.Dossier) == normaliserDossier(d) {
			return p, true
		}
	}
	return Projet{}, false
}

// cleEnClairMCP: true when the configuration holds a key or a password in clear (never: "${NAME}" in its place).
func cleEnClairMCP(config, id string) bool {
	l, err := lireConfigCollee(config, id)
	if err != nil {
		return false
	}
	for _, c := range l {
		if len(separerSecrets(&c)) > 0 {
			return true
		}
	}
	return false
}

func (s *Serveur) projetLocalChat(nom string) (Projet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Projets {
		if !p.Archive && strings.EqualFold(p.Nom, nom) {
			return p, true
		}
	}
	return Projet{}, false
}

// carteActionChat: the card of a, its fields cleaned and completed (a new project's folder, a request's title).
func (s *Serveur) carteActionChat(a ActionChat) CarteActionChat {
	a.Type, a.Projet, a.Dossier, a.Titre, a.Texte = strings.ToLower(strings.TrimSpace(a.Type)), strings.TrimSpace(a.Projet),
		strings.TrimSpace(a.Dossier), strings.TrimSpace(a.Titre), strings.TrimSpace(a.Texte)
	c := CarteActionChat{Acces: []string{}}
	k := s.kitCourant()
	if a.Type != "projet" {
		a.Deploiement, a.Permission = nil, ""
	}
	sansTexte := false // projet: an existing project gets its deployment only, no request
	// agent, mcp, attribuer: the projects it goes on, those the user ticked on the card
	surProjets := func(nom string) string {
		noms, _, inconnu := s.projetsChat(a.Projets)
		if inconnu != "" && c.Erreur == "" {
			c.Erreur = "Unknown project: choose one of yours."
		}
		a.Projets = noms
		a.Projet, a.Dossier, a.Titre, a.Texte = "", "", "", ""
		if len(noms) == 0 {
			return nom
		}
		return nom + " → " + strings.Join(noms, ", ")
	}
	existant := func() {
		if p, ok := s.projetLocalChat(a.Projet); ok {
			a.Projet = p.Nom
		} else if c.Erreur == "" {
			c.Erreur = "Unknown project: choose one of yours."
		}
	}
	switch a.Type {
	case "projet":
		a.Permission = strings.TrimSpace(a.Permission)
		if d := a.Deploiement; d != nil && len(d.Serveurs)+len(d.APIs) == 0 && strings.TrimSpace(d.URL+d.Plan) == "" {
			a.Deploiement = nil // nothing in it: a project built on this PC only
		}
		if p, ok := s.projetLocalChat(a.Projet); ok && a.Deploiement != nil {
			sansTexte = true
			a.Projet, a.Dossier, a.Titre, a.Texte, a.Permission = p.Nom, "", "", "", ""
			c.Quoi, c.Sur = "Set the project's deployment", p.Nom
			c.Acces = []string{"Writes this project's deployment only: no project is created, no request is added."}
		} else {
			if a.Dossier == "" && a.Projet != "" {
				s.mu.Lock()
				a.Dossier = dossierProjetNeuf(a.Projet, s.config.Projets)
				s.mu.Unlock()
			}
			c.Quoi, c.Sur = "Create the project and start its pilot", a.Projet+" — "+a.Dossier
			c.Acces = []string{"Creates this folder and writes the project's first request in it.",
				"Starts the pilot: it works in this folder only, with the accesses you give this project."}
			if _, err := s.controlerProjetNeuf(a.Projet, a.Dossier); err != nil {
				c.Erreur = err.Error()
			}
			if a.Permission == "ignorer" {
				c.Acces = append(c.Acces, "Ignore permissions: this project acts on its servers without asking you, dangerous commands and production deployments included.")
			} else if a.Permission != "" {
				c.Acces = append(c.Acces, "Sets this project's own permission mode, instead of the one of Settings.")
			}
			if c.Erreur == "" && a.Permission != "" && !slices.Contains(modesAutorisation, a.Permission) {
				c.Erreur = "Unknown permission mode (manuel, auto, modifications or ignorer)"
			}
		}
		if a.Deploiement != nil {
			d := *a.Deploiement
			if d.Moment == "" {
				d.Moment = "livraison"
			}
			c.Noms = s.nommerDeploiement(&d)
			a.Deploiement = &d
			c.Acces = append(c.Acces, "Gives its pilot the servers and the API keys shown on this card, and nothing else of your vault.")
			if err := s.deploiementCarteErr(a.Projet, d); err != nil && c.Erreur == "" {
				c.Erreur = err.Error()
			}
		}
	case "pilote":
		a.Titre, a.Texte, a.Dossier = "", "", ""
		existant()
		c.Quoi, c.Sur = "Start the project's pilot", a.Projet
		c.Acces = []string{"Starts the pilot: it works in this project's folder, with the accesses you gave this project."}
	case "demande":
		if a.Titre == "" {
			a.Titre, _, _ = strings.Cut(a.Texte, "\n")
			if utf8.RuneCountInString(a.Titre) > 60 {
				a.Titre = string([]rune(a.Titre)[:59]) + "…"
			}
		}
		existant()
		c.Quoi, c.Sur = "Add a request to the project's queue", a.Projet
		c.Acces = []string{"Writes one file in the project's queue: its pilot takes it after the current request."}
	case "remarque":
		existant()
		c.Quoi, c.Sur = "Leave a remark to the project's pilot", a.Projet
		c.Acces = []string{"Adds the remark to the project's notes: its pilot reads it at its next cycle."}
	case "mission":
		a.Projet, a.Dossier, a.Titre = "", "", ""
		c.Quoi, c.Sur = "Prepare a mission", "Athanor"
		c.Acces = []string{"Athanor's orchestrator reads your projects and servers to prepare the mission's card.",
			"Nothing runs on a server before you launch that card."}
		if utf8.RuneCountInString(a.Texte) > maxPhraseMission {
			c.Erreur = "Say what you want in one sentence, 4000 characters at most"
		}
	case "agent":
		a.Nom, a.Description, a.Consignes, a.Modele = strings.ToLower(strings.TrimSpace(a.Nom)), strings.TrimSpace(a.Description), strings.TrimSpace(a.Consignes), strings.TrimSpace(a.Modele)
		a.Element, a.Source, a.Config, a.Windows, a.ConfirmeWindows, a.Fichiers = "", "", "", false, false, nil
		c.Quoi, c.Sur = "Add the sub-agent to your library", surProjets(a.Nom)
		c.Acces = []string{"Adds it to your library of sub-agents (Team page); no file of a project changes."}
		if len(a.Projets) > 0 {
			c.Acces = append(c.Acces, "Switches it on in these projects: their pilot can call it from its next cycle.")
		}
		if a.LectureSeule {
			c.Acces = append(c.Acces, "It only reads and reports: it changes no file.")
		} else {
			c.Acces = append(c.Acces, "It may change the files of the projects it is switched on in, like their pilot.")
		}
		if err := validerAgent(AgentPerso{Nom: a.Nom, Description: a.Description, Consignes: a.Consignes, Modele: a.Modele}); err != nil {
			c.Erreur = err.Error()
		} else if lireAgents(k).trouver(a.Nom) >= 0 {
			c.Erreur = "A sub-agent already has this name."
		}
	case "mcp":
		a.Nom, a.Description, a.Source, a.Config = strings.TrimSpace(a.Nom), strings.TrimSpace(a.Description), strings.TrimSpace(a.Source), strings.TrimSpace(a.Config)
		a.Element, a.Consignes, a.Modele, a.LectureSeule = "", "", "", false
		c.Quoi, c.Sur = "Add the MCP server to your library", surProjets(a.Nom)
		c.Acces = []string{"Adds it to your library (Skills & MCP page)."}
		if len(a.Fichiers) > 0 {
			c.Acces = append(c.Acces, "Saves the files of the server Claude wrote in Athanor's tools folder.")
		}
		if len(a.Projets) > 0 {
			c.Acces = append(c.Acces, "Switches it on in these projects: their pilot can use it from its next cycle.")
		}
		if a.Windows {
			c.Acces = append(c.Acces, "Runs as a Windows program, outside the cage, with your rights: it can read and change your files and run programs.")
		}
		id := identExt(a.Nom)
		_, homonyme := lireBiblio(k).MCP[id]
		switch l, err := lireConfigCollee(a.Config, firstNonEmpty(id, "mcp")); {
		case !reIDExt.MatchString(id) || id == "unreal-mcp":
			c.Erreur = "Invalid name"
		case err != nil || len(l) != 1:
			c.Erreur = "The configuration must hold one server"
		case cleEnClairMCP(a.Config, id):
			c.Erreur = "A key is written in clear: put ${NAME} in its place, you fill it in on the Skills & MCP page."
		case a.Windows && !a.ConfirmeWindows:
			c.Erreur = "Confirm that this server may run outside the cage, with your rights."
		case homonyme:
			c.Erreur = errMCPHomonyme.Error()
		}
	case "attribuer":
		a.Element, a.Nom = strings.ToLower(strings.TrimSpace(a.Element)), strings.TrimSpace(a.Nom)
		a.Description, a.Consignes, a.Modele, a.LectureSeule, a.Source, a.Config, a.Windows, a.ConfirmeWindows, a.Fichiers = "", "", "", false, "", "", false, false, nil
		connu := false
		if a.Element == "agent" {
			a.Nom = strings.ToLower(a.Nom)
			connu = lireAgents(k).trouver(a.Nom) >= 0
			c.Quoi = "Switch the sub-agent on in projects"
			c.Acces = []string{"Adds it to the team of these projects: their pilot can call it from its next cycle."}
		} else {
			a.Element = "mcp"
			for id, m := range lireBiblio(k).MCP {
				if id == identExt(a.Nom) || strings.EqualFold(m.Nom, a.Nom) {
					a.Nom, connu = id, true
					if m.Config.Type == "windows" {
						c.Acces = append(c.Acces, "Runs as a Windows program, outside the cage, with your rights: it can read and change your files and run programs.")
					}
					break
				}
			}
			c.Quoi = "Switch the MCP server on in projects"
			c.Acces = append([]string{"Switches it on in these projects: their pilot can use it from its next cycle."}, c.Acces...)
		}
		c.Sur = surProjets(a.Nom)
		if !connu && c.Erreur == "" {
			c.Erreur = "Not in your library: add it first."
		} else if len(a.Projets) == 0 && c.Erreur == "" {
			c.Erreur = "Choose at least one project."
		}
	default:
		c.Erreur = "This card cannot be read: ask the assistant to propose it again."
	}
	if c.Erreur == "" && slices.Contains([]string{"projet", "demande", "remarque", "mission"}, a.Type) && a.Texte == "" && !sansTexte {
		c.Erreur = "Write what it must do."
	}
	if c.Erreur == "" && utf8.RuneCountInString(a.Texte) > maxTexteAction {
		c.Erreur = "This text is too long (20,000 characters at most)."
	}
	if c.Erreur == "" && s.coffre != nil && s.coffre.Urgence() != "" {
		c.Erreur = "The emergency stop is on: resume Athanor first."
	}
	c.ActionChat = a
	if a.Type == "projet" {
		c.Empreinte = empreinteCarteChat(c)
	}
	return c
}

// empreinteCarteChat: the card as the page shows it (what it does, on what, with which accesses, its fields).
func empreinteCarteChat(c CarteActionChat) string {
	c.Empreinte, c.Etat, c.Resultat, c.Args = "", "", "", nil
	b, _ := json.Marshal(c)
	return fmt.Sprintf("%x", sha256.Sum256(b))[:24]
}

// nommerDeploiement (M295): the card's servers and APIs by id, whether the assistant wrote their id or their name
// (as the owner mentions them), and their names for the card. An unknown one stays as written: deploiementValide
// refuses it.
func (s *Serveur) nommerDeploiement(d *DeploiementDemande) map[string]string {
	noms := map[string]string{}
	propre := func(x string) string { return strings.TrimPrefix(strings.TrimSpace(x), "@") }
	inventaire, _ := lireServeurs()
	d.Serveurs = slices.Clone(d.Serveurs)
	for i, x := range d.Serveurs {
		d.Serveurs[i] = propre(x)
		for _, v := range inventaire {
			if v.ID == propre(x) || strings.EqualFold(v.Nom, propre(x)) {
				d.Serveurs[i], noms[v.ID] = v.ID, v.Nom
				break
			}
		}
	}
	d.APIs = slices.Clone(d.APIs)
	var coffre []EntreeCoffre
	if s.coffre != nil {
		coffre = s.coffre.Liste()
	}
	for i, x := range d.APIs {
		d.APIs[i] = propre(x)
		for _, v := range coffre {
			if v.ID == propre(x) || strings.EqualFold(v.Nom, propre(x)) {
				d.APIs[i], noms[v.ID] = v.ID, v.Nom
				break
			}
		}
	}
	return noms
}

// deploiementCarteErr: why the deployment of a card cannot be written as it is (nil: it can).
func (s *Serveur) deploiementCarteErr(projet string, d DeploiementDemande) error {
	switch {
	case s.coffre == nil || s.coffreErr != nil:
		return errors.New("The vault file cannot be read")
	case s.acces == nil || s.accesErr != nil:
		return errors.New("The access file cannot be read")
	}
	_, _, err := s.deploiementValide(projet, d)
	return err
}

// cartesConversation: the card of every message of the assistant that proposes an action, by message index.
func (s *Serveur) cartesConversation(conv conversationChat) map[string]CarteActionChat {
	l := map[string]CarteActionChat{}
	for i, m := range conv.Messages {
		if m.Role != "claude" {
			continue
		}
		a, ok := lireActionChat(m.Texte)
		if !ok {
			continue
		}
		c := s.carteActionChat(a)
		if m.Action != nil { // decided: the card as it was launched or refused
			c = *m.Action
		}
		l[strconv.Itoa(i)] = c
	}
	return l
}

// executerActionChat: does what the card says (message: the assistant's message that proposed it); the result as an
// i18n key and its arguments.
func (s *Serveur) executerActionChat(c CarteActionChat, qui, message string) (string, []string, error) {
	a := c.ActionChat
	k := s.kitCourant()
	_, dossiers, _ := s.projetsChat(a.Projets)
	ajoute := func(nom string) (string, []string, error) {
		if len(a.Projets) == 0 {
			return "“{0}” added to your library: tell me on which projects to switch it on.", []string{nom}, nil
		}
		return "“{0}” added to your library and switched on in: {1}.", []string{nom, strings.Join(a.Projets, ", ")}, nil
	}
	switch a.Type {
	case "agent":
		verrouAgents.Lock()
		defer verrouAgents.Unlock()
		if !agentsLisibles(k) {
			return "", nil, errors.New("extensions/agents.json is unreadable: fix or remove it, nothing was changed.")
		}
		f := lireAgents(k)
		if _, _, err := enregistrerAgentPerso(k, &f, dossiers, "", AgentPerso{Nom: a.Nom, Description: a.Description, Consignes: a.Consignes, Modele: a.Modele, LectureSeule: a.LectureSeule}); err != nil {
			return "", nil, err
		}
		return ajoute(a.Nom)
	case "mcp":
		// the files come from the assistant's message itself, never from the request
		if _, _, err := ajouterMCPBiblio(k, a.Nom, a.Description, a.Source, a.Config, a.Windows, fichiersMCPMessage(message), dossiers); err != nil {
			return "", nil, err
		}
		return ajoute(a.Nom)
	case "attribuer":
		if a.Element == "agent" {
			verrouAgents.Lock()
			defer verrouAgents.Unlock()
			if !agentsLisibles(k) {
				return "", nil, errors.New("extensions/agents.json is unreadable: fix or remove it, nothing was changed.")
			}
			f := lireAgents(k)
			i := f.trouver(a.Nom)
			if i < 0 {
				return "", nil, errors.New("This sub-agent no longer exists.")
			}
			for _, d := range dossiers {
				if !f.Agents[i].duDossier(d) {
					f.Agents[i].Projets = append(f.Agents[i].Projets, d)
				}
				f.accepterAgent(d, a.Nom)
			}
			if err := ecrireAgents(k, f); err != nil {
				return "", nil, err
			}
		} else {
			extMu.Lock()
			err := activerMCPProjets(k, a.Nom, dossiers)
			extMu.Unlock()
			if err != nil {
				return "", nil, err
			}
		}
		return "“{0}” switched on in: {1}.", []string{a.Nom, strings.Join(a.Projets, ", ")}, nil
	case "projet":
		deployer := func(projet string) error {
			if _, err := s.accesPretErr(projet); err != nil {
				return err
			}
			return s.definirDeploiementProjet(projet, *a.Deploiement, qui)
		}
		if p, ok := s.projetLocalChat(a.Projet); ok && a.Deploiement != nil { // an existing project: its deployment only
			return "Deployment of “{0}” saved.", []string{p.Nom}, deployer(p.Nom)
		}
		p, _, err := s.ajouterProjet(a.Projet, a.Dossier, a.Texte, false)
		if err != nil {
			return "", nil, err
		}
		if a.Deploiement != nil {
			err = deployer(p.Nom)
		}
		if err == nil && a.Permission != "" {
			if _, err = definirReglageProjet(p.Nom, "permission", a.Permission); err == nil {
				s.ecrireAudit(LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: p.Nom, Action: "reglage du projet: permission", Resultat: "defaut → " + a.Permission})
			}
		}
		if err != nil { // never a pilot that would run without what the card said
			return "Project “{0}” added, but its deployment or its permission mode was not saved and its pilot was not started: {1}", []string{p.Nom, err.Error()}, nil
		}
		msg, err := demarrerPiloteMission(s, p)
		if err != nil {
			return "Project “{0}” added, but its pilot did not start: {1}", []string{p.Nom, err.Error()}, nil
		}
		return "Project “{0}” added. {1}", []string{p.Nom, msg}, nil
	case "pilote":
		p, _ := s.projetLocalChat(a.Projet)
		msg, err := demarrerPiloteMission(s, p)
		return msg, nil, err
	case "demande":
		p, _ := s.projetLocalChat(a.Projet)
		ou, _, err := s.ajouterDemande(p, a.Titre, a.Texte)
		if ou == "demande" {
			return "It became the project's request: start its pilot to build it.", nil, err
		}
		return "Request added to the queue of {0}.", []string{p.Nom}, err
	case "remarque":
		p, _ := s.projetLocalChat(a.Projet)
		return "Remark sent: the pilot reads it at its next cycle.", nil,
			p.AjouterRemarque(a.Texte, libelle(s.langue(), "remarques"), libelle(s.langue(), "remarques_aide"))
	case "mission":
		_, err := s.creerMission(a.Texte, MentionsMission{Projets: []string{}, Serveurs: []string{}, Cles: []string{}}, qui)
		return "Mission prepared: its card appears on the Missions page.", nil, err
	}
	return "", nil, errors.New("This card cannot be read: ask the assistant to propose it again.")
}

// apiHubChatAction: POST /api/hub/chat/action {index, decision: lancer|refuser, action} — the user's decision on
// the card of a message; action: the card's fields as the user left them (its type stays the proposed one).
func (s *Serveur) apiHubChatAction(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Index    int        `json:"index"`
		Decision string     `json:"decision"`
		Action   ActionChat `json:"action"`
		// projet: the card the owner read; Ignore permissions as everywhere (choixIgnorer)
		Empreinte  string `json:"empreinte"`
		Confirmer  bool   `json:"confirmer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if err := lireCorps(r, &c); err != nil || (c.Decision != "lancer" && c.Decision != "refuser") {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	verrouActionsChat.Lock()
	defer verrouActionsChat.Unlock()
	k := s.kitCourant()
	p := projetAccueil(k)
	conv := lireConversation(k, p)
	if c.Index < 0 || c.Index >= len(conv.Messages) || conv.Messages[c.Index].Role != "claude" {
		s.erreur(w, http.StatusBadRequest, "Unknown message")
		return
	}
	propose, ok := lireActionChat(conv.Messages[c.Index].Texte)
	if !ok {
		s.erreur(w, http.StatusBadRequest, "This message proposes no action")
		return
	}
	if conv.Messages[c.Index].Action != nil {
		s.erreur(w, http.StatusConflict, "This card was already decided.")
		return
	}
	c.Action.Type, c.Action.Fichiers = propose.Type, propose.Fichiers
	carte := s.carteActionChat(c.Action)
	qui := deQui(r)
	audit := LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: carte.Projet, Action: "chat d'accueil : " + carte.Quoi + " (" + carte.Sur + ")"}
	if c.Decision == "refuser" {
		carte.Etat, carte.Resultat = "refusee", "Card refused: nothing was done."
		audit.Resultat = "carte refusée"
	} else {
		// R3: Launch from an older view (a project created or a server removed since) launches nothing, and a
		// card changed into another action (a new project renamed into an existing one) neither
		if vue := s.carteActionChat(propose); propose.Type == "projet" && (c.Empreinte != vue.Empreinte || carte.Quoi != vue.Quoi) {
			s.erreur(w, http.StatusConflict, "This card changed since it was shown: read it again, then launch it.")
			return
		}
		if carte.Erreur != "" {
			s.erreur(w, http.StatusBadRequest, carte.Erreur)
			return
		}
		if carte.Permission != "" {
			if code, err := s.choixIgnorer(r, carte.Permission, c.Confirmer, c.MotDePasse); err != nil {
				audit.Resultat = "refusée : " + err.Error()
				s.ecrireAudit(audit)
				s.erreur(w, code, err.Error())
				return
			}
		}
		cle, args, err := s.executerActionChat(carte, qui, conv.Messages[c.Index].Texte)
		if err != nil {
			audit.Resultat = "échec : " + err.Error()
			s.ecrireAudit(audit)
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		carte.Etat, carte.Resultat, carte.Args = "lancee", cle, args
		audit.Resultat = "lancée : " + strings.Join(append([]string{cle}, args...), " | ")
	}
	s.ecrireAudit(audit)
	conv = lireConversation(k, p) // a turn may have ended meanwhile
	if c.Index < len(conv.Messages) {
		conv.Messages[c.Index].Action = &carte
		ecrireConversation(k, p, conv)
	}
	s.ecrireJSON(w, map[string]any{"carte": carte, "message_key": carte.Resultat, "args": carte.Args})
}
