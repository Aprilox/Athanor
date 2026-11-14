package main

// Actions of the home conversation (M197): the assistant never acts; it proposes one action per answer between
// <action> and </action>. The Go computes its card (what, on what, with which accesses, why it cannot run as it
// is), the user launches, changes or refuses it, and every decision goes to the audit log (who, what, when, result).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ActionChat: what the assistant proposes (and the user may change on the card before launching it).
type ActionChat struct {
	Type    string `json:"type"`              // projet | pilote | demande | remarque | mission
	Projet  string `json:"projet,omitempty"`  // the project it acts on (projet: the new project's name)
	Dossier string `json:"dossier,omitempty"` // projet: its folder (empty: next to the latest project)
	Titre   string `json:"titre,omitempty"`   // demande: its title
	Texte   string `json:"texte,omitempty"`   // the request, the remark, the mission's sentence
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
	Resultat string   `json:"resultat,omitempty"`
	Args     []string `json:"args,omitempty"`
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
- {"type": "projet", "projet": "the new project's name (letters, digits, spaces, dashes)", "texte": "its complete request, in the user's words", "dossier": "its full folder path, or empty: Athanor picks one next to the other projects"}: create a project and start its pilot;
- {"type": "pilote", "projet": "an existing project"}: start the pilot of a project;
- {"type": "demande", "projet": "an existing project", "titre": "a short title", "texte": "the request or the task, self-contained"}: add a request (a task) to a project's queue, its pilot takes it after the current one;
- {"type": "remarque", "projet": "an existing project", "texte": "the remark, short and self-contained"}: leave a remark its pilot reads at its next cycle;
- {"type": "mission", "texte": "the mission in one sentence, naming its projects and servers"}: prepare a mission (servers, deployments, several projects); Athanor's orchestrator then prepares its own card.
One action per answer, with the exact names of the projects below; a question gets an answer and no card.

The state of Athanor now (projects and modules, from Athanor itself):
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
		return "(nothing yet: no project, no server)\n"
	}
	return s.masquerSecrets(b.String())
}

// lireActionChat: the action of a message of the assistant (false without one or when it cannot be read).
func lireActionChat(texte string) (ActionChat, bool) {
	m := reAction.FindStringSubmatch(texte)
	if m == nil {
		return ActionChat{}, false
	}
	j := strings.TrimSpace(m[1])
	j = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(j, "```json"), "```"), "```")
	var a ActionChat
	if json.Unmarshal([]byte(j), &a) != nil {
		return ActionChat{}, true // a card that says it cannot be read
	}
	return a, true
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
	existant := func() {
		if p, ok := s.projetLocalChat(a.Projet); ok {
			a.Projet = p.Nom
		} else if c.Erreur == "" {
			c.Erreur = "Unknown project: choose one of yours."
		}
	}
	switch a.Type {
	case "projet":
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
	default:
		c.Erreur = "This card cannot be read: ask the assistant to propose it again."
	}
	if c.Erreur == "" && a.Type != "pilote" && a.Texte == "" {
		c.Erreur = "Write what it must do."
	}
	if c.Erreur == "" && utf8.RuneCountInString(a.Texte) > maxTexteAction {
		c.Erreur = "This text is too long (20,000 characters at most)."
	}
	if c.Erreur == "" && s.coffre != nil && s.coffre.Urgence() != "" {
		c.Erreur = "The emergency stop is on: resume Athanor first."
	}
	c.ActionChat = a
	return c
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

// executerActionChat: does what the card says; the result as an i18n key and its arguments.
func (s *Serveur) executerActionChat(c CarteActionChat, qui string) (string, []string, error) {
	a := c.ActionChat
	switch a.Type {
	case "projet":
		p, _, err := s.ajouterProjet(a.Projet, a.Dossier, a.Texte, false)
		if err != nil {
			return "", nil, err
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
	c.Action.Type = propose.Type
	carte := s.carteActionChat(c.Action)
	qui := deQui(r)
	audit := LigneAudit{Type: "evenement", De: qui, Vers: "hub", Projet: carte.Projet, Action: "chat d'accueil : " + carte.Quoi + " (" + carte.Sur + ")"}
	if c.Decision == "refuser" {
		carte.Etat, carte.Resultat = "refusee", "Card refused: nothing was done."
		audit.Resultat = "carte refusée"
	} else {
		if carte.Erreur != "" {
			s.erreur(w, http.StatusBadRequest, carte.Erreur)
			return
		}
		cle, args, err := s.executerActionChat(carte, qui)
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
