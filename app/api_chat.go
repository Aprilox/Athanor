package main

// Talking with Claude about a project, from the application: a conversation of short Claude Code
// sessions (claude -p, continued with --resume) run in the project's folder.
//   - By default Claude only reads (Read, Glob, Grep) and answers: a change asked for becomes a note for
//     the pilot, which the page sends with one click.
//   - Changes: only when the user ticks "change the files" (mode modif). With the pilot stopped, Claude may
//     then edit the project's files (no commands), and Athanor commits what changed; while it runs, the
//     pilot stops at the end of its cycle, Claude changes the files, and the pilot starts again with a note
//     of it (attendreFinCycle).
//   - Files attached to a message go into the workshop (.athanor/attachments/chat/, api_pieces.go): Claude
//     reads them, and a note sent to the pilot names them.
//   - An MCP server Claude found or wrote (<mcp>…</mcp>) goes into the library with one click (apiChatMCP).
// File tools stay inside the project (system/guard.py as a hook, as for the pilot). The conversation is
// kept in the kit's logs (logs/chat/), never in the project.

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type MessageChat struct {
	Role   string   `json:"role"` // vous, claude, erreur
	Texte  string   `json:"texte"`
	Outils []string `json:"outils,omitempty"` // what Claude did ("Reads src/x.luau", "Edits …")
	Mode   string   `json:"mode,omitempty"`   // question, modif
	Modele string   `json:"modele,omitempty"`
	Quand  string   `json:"quand"`
	Cout   float64  `json:"cout,omitempty"`   // what this turn cost
	Commit string   `json:"commit,omitempty"` // the commit of the changes (modif)
	// what Claude Code said the whole session cost so far: a resumed session (--resume) reports its total,
	// this turn's cost is the difference with the turn before (coutTourChat)
	CoutSession float64 `json:"cout_session,omitempty"`
	Note        bool    `json:"note,omitempty"`  // the note was sent to the pilot
	Agent       string  `json:"agent,omitempty"` // the sub-agent it proposed, added to the team under this name
	// Internet: the user let Claude search and read the web for this message (when the request needs it)
	Internet bool     `json:"internet,omitempty"`
	Pieces   []string `json:"pieces,omitempty"` // the files attached (.athanor/attachments/chat/…)
	MCP      string   `json:"mcp,omitempty"`    // the MCP server it proposed, added to the library under this id
	// Pause: the pilot was running, it stopped at the end of its cycle for this change; Pilote (the answer):
	// "relance" when it started again after it, else why not
	Pause  bool   `json:"pause,omitempty"`
	Pilote string `json:"pilote,omitempty"`
}

type conversationChat struct {
	Session  string        `json:"session"`
	Messages []MessageChat `json:"messages"`
}

type chatEnCours struct {
	cmd     *exec.Cmd
	journal string
	mode    string
	modele  string
	arrete  bool
	occupe  bool // the project held (s.occuper) while Claude changes the files
	// attente: waits for the end of the pilot's cycle before changing the files; pause: the pilot was
	// running when the change was asked (it starts again after it, unless the user stopped it meanwhile)
	attente, pause, auto bool
	stopAvant            bool // the user had already asked the pilot to stop after its cycle: it stays stopped
	sujet                string
}

var (
	chatMu      sync.Mutex
	chatsActifs = map[string]*chatEnCours{} // project folder → session under way
)

var modelesChat = map[string]bool{"": true, "sonnet": true, "opus": true, "haiku": true, "fable": true,
	"claude-sonnet-5-5": true, "claude-opus-5-5": true, "claude-haiku-5-5": true}

func dossierChat(k Kit) string { return filepath.Join(k.Journaux(), "chat") }

func idChat(p Projet) string {
	h := sha1.Sum([]byte(strings.ToLower(p.Dossier)))
	return hex.EncodeToString(h[:6])
}

func lireConversation(k Kit, p Projet) conversationChat {
	var c conversationChat
	b, err := os.ReadFile(filepath.Join(dossierChat(k), idChat(p)+".json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func ecrireConversation(k Kit, p Projet, c conversationChat) {
	if len(c.Messages) > 200 {
		c.Messages = c.Messages[len(c.Messages)-200:]
	}
	_ = os.MkdirAll(dossierChat(k), 0o700)
	b, _ := json.MarshalIndent(c, "", " ")
	f := filepath.Join(dossierChat(k), idChat(p)+".json")
	_ = os.WriteFile(f+".tmp", b, 0o600)
	_ = os.Rename(f+".tmp", f)
}

// lireTour: what a session wrote so far (stream-json): its texts, its actions, the final answer, its id.
func lireTour(journal string) (textes []string, outils []string, final string, session string, cout float64, erreur string) {
	f, err := os.Open(journal)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		l := sc.Text()
		var v struct {
			Session string  `json:"session_id"`
			Type    string  `json:"type"`
			Cout    float64 `json:"total_cost_usd"`
			Erreur  bool    `json:"is_error"`
			Result  string  `json:"result"`
		}
		if json.Unmarshal([]byte(l), &v) != nil {
			continue
		}
		if v.Session != "" {
			session = v.Session
		}
		if v.Type == "result" {
			final, cout = v.Result, v.Cout
			if v.Erreur {
				erreur = v.Result
			}
		}
		for _, e := range Traduire(l) {
			if e.SousAgent {
				continue
			}
			switch e.Genre {
			case "texte":
				textes = append(textes, e.Detail)
			case "outil":
				if len(outils) < 60 {
					outils = append(outils, e.Titre)
				}
			}
		}
	}
	return
}

const consigneChat = `You are Claude, talking with the owner of this project inside Athanor, the application that runs
an autonomous pilot on it (its plan, log and notes are in .athanor/: PLAN.md, LOG.md, DECISIONS.md, BUGS.md).
Answer in the language the user writes in, briefly and concretely; read the project's files when it helps,
and name the files you mean.

Sub-agents: .athanor/TEAM.md lists the project's team (Athanor's own sub-agents and the user's). When the user
wants a new specialist sub-agent, or to change one of theirs, shape it with them: ask what you need, a few
questions at a time (what it does, when the lead should call it, what it must never do, a cheap or a strong
model, whether it may change files, its name), then propose it. Once the
user agrees, write it once, at the end of your answer, between <agent> and </agent>, as JSON:
{"nom": "lowercase letters, digits and dashes, 2 to 31", "ancien": "its current name when changing one, else empty",
"description": "when the lead should call it, 400 characters at most, in the user's language",
"consignes": "its complete instructions (role, method, always and never, what it hands back), 6000 characters at most",
"modele": "" (like the other sub-agents) or "eco" (cheap, mechanical work) or "fort" (strongest, hard reasoning),
"lecture_seule": true when it only reads and reports}.
Athanor shows it as a card the user can still edit, and adds it with one click to the user's library of
sub-agents, switched on in this project (the user switches it on in other projects on their Team page):
never write the team's files yourself. Names taken by Athanor: architect, creator, developer, tester, critic, executor, scout.

Attached files: a message may list files the user attached (.athanor/attachments/chat/…): read them (an image
too) before you answer.

MCP servers: when the user wants the pilot to drive a program or a service through MCP, get them one. First
look for an existing server (with Internet: the official MCP Registry, registry.modelcontextprotocol.io, the
program's documentation, GitHub); without Internet, say what you know and suggest switching Internet on. When
none exists or none fits, write one: a single Python file run by uv, its dependencies declared inline (PEP 723,
e.g. the official "mcp" package and its FastMCP), talking to the program through what it offers (its command
line, its local HTTP API, its files…). Once the user agrees, write it once, at the end of your answer, between
<mcp> and </mcp>, as JSON: {"nom": "a short name", "description": "what it lets the pilot do, in the user's
language", "source": "the page it comes from, or empty",
"config": {"command": "…", "args": […], "env": {…}} for a local server or {"url": "…", "headers": {…}} for a
remote one, "windows": true only when it must run as a Windows program (outside the cage, with the user's rights),
"fichiers": {"server.py": "the complete file"} only for a server you wrote, its config then being
{"command": "uv", "args": ["run", "--script", "${ATHANOR_MCP_DIR}/server.py"]} (Athanor keeps the files in that folder)}.
Never write a key or a password in it: "${NAME}" in its place, the user fills it in on the Skills & MCP page.
Athanor shows it as a card the user adds with one click to the library, switched on in this project: never
write MCP configurations or servers in the project yourself.
%s`

const consigneQuestion = `In this turn you can only read: no change, no command. Never change a file yourself, even when asked
(the user did not tick "change the files"). When the user asks for a change, explain briefly what should
change, then write the instruction for the pilot, on its own, between <note> and </note> (short, precise,
self-contained, in the user's language, naming the attached files it needs by their path): Athanor shows it
with a button that sends it to the pilot, which reads it at its next cycle (when it starts again, if it is
stopped). A mere question gets an answer, no note. (The user can also tick "change the files" to have you
make the change yourself.)`

const consigneModif = `The pilot is stopped and the user ticked "change the files": you may change the project's files (Edit,
Write), but only to make a change the user explicitly asks for in this message. A question, an idea or a
remark gets an answer, and no file changes; when unsure whether they want the change made, ask first. You
cannot run commands. Keep the change small and focused, never touch .athanor/ (the pilot's workshop) unless
asked, and end with the list of the files you changed. Athanor commits your changes for you.`

var (
	reNote  = regexp.MustCompile(`(?s)<note>(.*?)</note>`)
	reAgent = regexp.MustCompile(`(?s)<agent>(.*?)</agent>`)
	reMCP   = regexp.MustCompile(`(?s)<mcp>(.*?)</mcp>`)
)

const prefixePieceChat = ".athanor/attachments/chat/"

// piecesChat: the files attached to a message, as the upload gave them (api_pieces.go, cible "chat"): each
// one in the chat's attachments folder, and there.
func piecesChat(p Projet, l []string) ([]string, error) {
	if len(l) > 20 {
		return nil, errors.New("20 files at most per message")
	}
	var out []string
	for _, x := range l {
		nom := strings.TrimPrefix(x, prefixePieceChat)
		if nom == x || nom == "" || nom == "." || nom == ".." || strings.ContainsAny(nom, `/\`) {
			return nil, errors.New("Unknown attached file")
		}
		if fi, err := os.Lstat(filepath.Join(p.Atelier(), "attachments", "chat", nom)); err != nil || !fi.Mode().IsRegular() {
			return nil, errors.New("Unknown attached file")
		}
		out = append(out, x)
	}
	return out, nil
}

// apiChat: the conversation of the current project, with the turn under way.
func (s *Serveur) apiChat(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	k := s.kitCourant()
	c := lireConversation(k, p)
	chatMu.Lock()
	t := chatsActifs[p.Dossier]
	chatMu.Unlock()
	if c.Messages == nil {
		c.Messages = []MessageChat{}
	}
	rep := map[string]any{"messages": c.Messages, "pilote_actif": p.PiloteActif(), "en_cours": t != nil}
	if t != nil {
		textes, outils, _, _, _, _ := lireTour(t.journal)
		mode := t.mode
		chatMu.Lock()
		if t.attente {
			mode = "attente"
		}
		chatMu.Unlock()
		rep["partiel"] = map[string]any{"textes": textes, "outils": outils, "mode": mode}
	}
	s.ecrireJSON(w, rep)
}

func (s *Serveur) apiChatEnvoyer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Message string `json:"message"`
		Modele  string `json:"modele"`
		// "modif": Claude may change the files (with the pilot running, it stops at the end of its cycle first,
		// then starts again); anything else: a read-only turn, a change becomes a note for the pilot
		Mode string `json:"mode"`
		// Internet: Claude may search and read the web for this message, only when the request needs it
		Internet bool     `json:"internet"`
		Pieces   []string `json:"pieces"` // files attached, uploaded first (api_pieces.go, cible "chat")
	}
	if err := lireCorps(r, &c); err != nil || (strings.TrimSpace(c.Message) == "" && len(c.Pieces) == 0) {
		s.erreur(w, http.StatusBadRequest, "Write your message first")
		return
	}
	if utf8.RuneCountInString(c.Message) > 20000 {
		s.erreur(w, http.StatusBadRequest, "Message too long (20,000 characters at most)")
		return
	}
	if c.Modele != "" && !reModele.MatchString(c.Modele) && !modelesChat[c.Modele] {
		s.erreur(w, http.StatusBadRequest, "Unknown model")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	pieces, err := piecesChat(p, c.Pieces)
	if err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	k := s.kitCourant()
	actif := p.PiloteActif()
	mode, pause := "question", false
	if c.Mode == "modif" {
		mode, pause = "modif", actif
	}
	chatMu.Lock()
	if chatsActifs[p.Dossier] != nil {
		chatMu.Unlock()
		s.erreur(w, http.StatusConflict, "Claude is still answering: wait for it, or stop it.")
		return
	}
	if mode == "modif" && !pause && !s.occuper(p.Nom) { // no pilot start while Claude changes the files
		chatMu.Unlock()
		s.erreur(w, http.StatusConflict, "Another action is under way on this project.")
		return
	}
	id := idChat(p) + "-" + time.Now().Format("150405")
	d := dossierChat(k)
	en := &chatEnCours{journal: filepath.Join(d, id+".jsonl"), mode: mode, modele: c.Modele, occupe: mode == "modif" && !pause, attente: pause, pause: pause, auto: lancerAuDemarrage(p),
		stopAvant: pause && fichierExiste(filepath.Join(p.Atelier(), "STOP"))}
	if err := os.MkdirAll(d, 0o700); err != nil {
		chatMu.Unlock()
		if en.occupe {
			s.liberer(p.Nom)
		}
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	chatsActifs[p.Dossier] = en
	chatMu.Unlock()

	texte := strings.TrimSpace(c.Message)
	conv := lireConversation(k, p)
	conv.Messages = append(conv.Messages, MessageChat{Role: "vous", Texte: texte, Mode: mode, Modele: c.Modele, Quand: time.Now().Format(time.RFC3339), Internet: c.Internet, Pieces: pieces, Pause: pause})
	ecrireConversation(k, p, conv)

	consigne := consigneQuestion
	if mode == "modif" {
		consigne = consigneModif
	}
	sujet := strings.Join(strings.Fields(texte), " ")
	if utf8.RuneCountInString(sujet) > 60 {
		sujet = string([]rune(sujet)[:59]) + "…"
	}
	if sujet == "" {
		sujet = strings.TrimPrefix(pieces[0], prefixePieceChat)
	}
	en.sujet = sujet
	if len(pieces) > 0 {
		texte += "\n\nAttached files (read them):"
		for _, x := range pieces {
			texte += "\n- `" + x + "`"
		}
	}
	fichiers := map[string]string{
		id + ".msg":     strings.TrimSpace(texte),
		id + ".prompt":  strings.Replace(consigneChat, "%s", consigne, 1),
		id + ".subject": "Chat: " + sujet,
	}
	if c.Internet {
		fichiers[id+".internet"] = "1" // outil_chat_session.go: WebSearch and WebFetch allowed for this turn
	}
	for nom, contenu := range fichiers {
		if err := os.WriteFile(filepath.Join(d, nom), []byte(contenu), 0o600); err != nil {
			s.finChat(k, p, en, "", err.Error())
			nettoyerTourChat(d, id)
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if pause {
		if _, err := s.arreterApresCycle(p); err != nil {
			s.finChat(k, p, en, "", err.Error())
			nettoyerTourChat(d, id)
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		go s.attendreFinCycle(k, p, en, id)
		s.ecrireJSON(w, map[string]string{"mode": "attente"})
		return
	}
	if err := s.lancerTourChat(k, p, en, id); err != nil {
		s.finChat(k, p, en, "", err.Error())
		nettoyerTourChat(d, id)
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireJSON(w, map[string]string{"mode": mode})
}

func nettoyerTourChat(d, id string) {
	for _, ext := range []string{".msg", ".prompt", ".subject", ".commit", ".settings.json", ".err", ".internet"} {
		os.Remove(filepath.Join(d, id+ext))
	}
}

var errChatArrete = errors.New("Stopped.")

// lancerTourChat: Claude's session for the turn (its files written in logs/chat/), its answer into the
// conversation when it ends. Nothing starts once the user stopped the turn.
func (s *Serveur) lancerTourChat(k Kit, p Projet, en *chatEnCours, id string) error {
	d := dossierChat(k)
	cmd := commandeKit(k, "chat", "session", k.Racine, p.Dossier, id, en.mode, en.modele, lireConversation(k, p).Session)
	cacherFenetre(cmd)
	chatMu.Lock()
	if en.arrete {
		chatMu.Unlock()
		return errChatArrete
	}
	en.cmd, en.attente = cmd, false
	err := cmd.Start()
	chatMu.Unlock()
	if err != nil {
		return err
	}
	go func() {
		err := cmd.Wait()
		detail := ""
		if err != nil {
			b, _ := os.ReadFile(filepath.Join(d, id+".err"))
			detail = strings.TrimSpace(string(b))
			if i := strings.LastIndex(detail, "\n"); i >= 0 {
				detail = detail[i+1:]
			}
			if detail == "" {
				detail = err.Error()
			}
		}
		commit, _ := os.ReadFile(filepath.Join(d, id+".commit"))
		s.finChat(k, p, en, strings.TrimSpace(string(commit)), detail)
		nettoyerTourChat(d, id)
	}()
	return nil
}

// attendreFinCycle: a change asked while the pilot runs. The pilot was asked to stop at the end of its cycle
// (the app shows it); once it has stopped, Claude changes the files, then the pilot starts again (finChat).
// The user's Stop, or the pilot's stop cancelled from the app, gives the change up: the pilot goes on.
func (s *Serveur) attendreFinCycle(k Kit, p Projet, en *chatEnCours, id string) {
	d := dossierChat(k)
	abandon := func(detail string) {
		s.finChat(k, p, en, "", detail)
		nettoyerTourChat(d, id)
	}
	for {
		time.Sleep(attenteCycleChat)
		chatMu.Lock()
		arrete := en.arrete
		chatMu.Unlock()
		if arrete {
			abandon("")
			return
		}
		if p.PiloteActif() {
			if !fichierExiste(filepath.Join(p.Atelier(), "STOP")) { // "Cancel the stop" in the app
				abandon("The pilot's stop was cancelled: the change was not made.")
				return
			}
			continue
		}
		if !s.occuper(p.Nom) {
			continue // another action on the project: its turn first
		}
		if p.PiloteActif() { // started again meanwhile
			s.liberer(p.Nom)
			continue
		}
		chatMu.Lock()
		en.occupe = true
		chatMu.Unlock()
		if err := s.lancerTourChat(k, p, en, id); err != nil {
			abandon(err.Error())
		}
		return
	}
}

var attenteCycleChat = 2 * time.Second // (shorter in the tests)

// chatAttendCycle: a change of the chat waits for the end of the pilot's cycle (it then starts again).
func chatAttendCycle(p Projet) bool {
	chatMu.Lock()
	defer chatMu.Unlock()
	en := chatsActifs[p.Dossier]
	return en != nil && en.attente && !en.stopAvant
}

// coutTourChat: what this turn cost. Claude Code gives the total of the session: a resumed one (--resume)
// carries on the total of the turns before, so the turn's cost is the difference with the last total known
// (a message written before 2.49.4 kept that total as its cost); a total lower than it is a session that
// started its count again.
func coutTourChat(avant []MessageChat, repris bool, total float64) float64 {
	if !repris || total <= 0 {
		return total
	}
	for i := len(avant) - 1; i >= 0; i-- {
		m := avant[i]
		precedent := m.CoutSession
		if precedent == 0 {
			precedent = m.Cout
		}
		if m.Role == "vous" || precedent <= 0 {
			continue
		}
		if total >= precedent {
			return math.Round((total-precedent)*1e4) / 1e4
		}
		return total
	}
	return total
}

// finChat: the turn's answer goes into the conversation.
func (s *Serveur) finChat(k Kit, p Projet, en *chatEnCours, commit, detail string) {
	textes, outils, final, session, cout, erreur := lireTour(en.journal)
	conv := lireConversation(k, p)
	repris := conv.Session != "" // this turn resumed the conversation's session (--resume)
	if session != "" {
		conv.Session = session
	}
	total := cout
	cout = coutTourChat(conv.Messages, repris, total)
	noterDepense(k.Racine, lireConnexionClaude(k.Racine, nil).Mode, cout, p.Nom, "chat") // API mode: counted once
	texte := strings.TrimSpace(final)
	if texte == "" && len(textes) > 0 {
		texte = strings.TrimSpace(textes[len(textes)-1])
	}
	m := MessageChat{Role: "claude", Texte: texte, Outils: outils, Mode: en.mode, Modele: en.modele, Quand: time.Now().Format(time.RFC3339), Cout: cout, CoutSession: total, Commit: commit}
	switch {
	case en.arrete:
		m.Role, m.Texte = "erreur", "Stopped."
	case erreur != "" || (texte == "" && detail != ""):
		m.Role = "erreur"
		m.Texte = court(firstNonEmpty(erreur, firstNonEmpty(detail, "No answer.")), 300)
	case texte == "":
		m.Role, m.Texte = "erreur", "No answer."
	}
	chatMu.Lock()
	occupe := en.occupe
	en.occupe = false
	chatMu.Unlock()
	if occupe {
		s.liberer(p.Nom)
	}
	// stopped for this change: started again, unless the user stopped it meanwhile (its "auto" mark removed)
	if en.pause && !en.stopAvant && !p.PiloteActif() && (lancerAuDemarrage(p) || !en.auto) {
		m.Pilote = s.relancerApresChat(p, commit, en.sujet)
	}
	conv.Messages = append(conv.Messages, m)
	ecrireConversation(k, p, conv)
	chatMu.Lock()
	delete(chatsActifs, p.Dossier)
	chatMu.Unlock()
	os.Remove(en.journal)
}

// relancerApresChat: the pilot starts again after the change made during its pause, with a note of it.
func (s *Serveur) relancerApresChat(p Projet, commit, sujet string) string {
	if commit != "" {
		_ = p.AjouterRemarque(fmt.Sprintf(texteChangeChat(s.langue()), commit, sujet), libelle(s.langue(), "remarques"), libelle(s.langue(), "remarques_aide"))
	}
	if _, err := s.demarrerPilote(p); err != nil {
		return err.Error()
	}
	return "relance"
}

func texteChangeChat(langue string) string {
	t := map[string]string{
		"en": "Changed by the user from the chat while the pilot was paused (commit %s): “%s”. Keep this change and build on it.",
		"fr": "Modifié par l'utilisateur depuis Discuter pendant la pause du pilote (commit %s) : « %s ». Garder ce changement et en tenir compte.",
		"es": "Cambiado por el usuario desde Conversar durante la pausa del piloto (commit %s): «%s». Conservar este cambio y tenerlo en cuenta.",
		"de": "Vom Benutzer im Chat geändert, während der Pilot pausierte (Commit %s): „%s“. Diese Änderung beibehalten und berücksichtigen.",
		"it": "Modificato dall'utente dalla chat durante la pausa del pilota (commit %s): «%s». Mantenere questa modifica e tenerne conto.",
		"pt": "Alterado pelo utilizador a partir de Conversar durante a pausa do piloto (commit %s): «%s». Manter esta alteração e tê-la em conta.",
	}
	if v, ok := t[langue]; ok {
		return v
	}
	return t["en"]
}

func (s *Serveur) apiChatArreter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	chatMu.Lock()
	en := chatsActifs[p.Dossier]
	attente := false
	if en != nil {
		en.arrete = true
		attente = en.attente && !en.stopAvant
		if en.cmd != nil && en.cmd.Process != nil {
			arreterArbre(en.cmd)
		}
	}
	chatMu.Unlock()
	if attente && p.PiloteActif() { // the stop asked for this change is withdrawn: the pilot goes on
		if err := retirerStop(p); err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.msg(w, "Stopped.")
}

func (s *Serveur) apiChatEffacer(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	chatMu.Lock()
	enCours := chatsActifs[p.Dossier] != nil
	chatMu.Unlock()
	if enCours {
		s.erreur(w, http.StatusConflict, "Claude is still answering: wait for it, or stop it.")
		return
	}
	ecrireConversation(s.kitCourant(), p, conversationChat{})
	s.msg(w, "New conversation.")
}

// apiChatNote: Claude's <note> of a message goes to the pilot (the project's notes along the way).
func (s *Serveur) apiChatNote(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Index int `json:"index"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	k := s.kitCourant()
	conv := lireConversation(k, p)
	if c.Index < 0 || c.Index >= len(conv.Messages) || conv.Messages[c.Index].Role != "claude" {
		s.erreur(w, http.StatusBadRequest, "Unknown message")
		return
	}
	m := reNote.FindStringSubmatch(conv.Messages[c.Index].Texte)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		s.erreur(w, http.StatusBadRequest, "This message has no note for the pilot")
		return
	}
	if err := p.AjouterRemarque(noteAvecPieces(strings.TrimSpace(m[1]), conv.Messages[:c.Index], s.langue()), libelle(s.langue(), "remarques"), libelle(s.langue(), "remarques_aide")); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	conv.Messages[c.Index].Note = true
	ecrireConversation(k, p, conv)
	s.msg(w, "Note sent: the pilot reads it at its next cycle.")
}

// noteAvecPieces: the note names the files attached to the user's message it answers (the pilot then reads
// them too), those Claude did not name already.
func noteAvecPieces(note string, avant []MessageChat, langue string) string {
	var manque []string
	for i := len(avant) - 1; i >= 0; i-- {
		if avant[i].Role != "vous" {
			continue
		}
		for _, x := range avant[i].Pieces {
			if !strings.Contains(note, x) {
				manque = append(manque, "`"+x+"`")
			}
		}
		break
	}
	if len(manque) == 0 {
		return note
	}
	entete := strings.TrimPrefix(firstNonEmpty(enteteFichiers[langue], enteteFichiers["en"]), "## ")
	return note + " (" + entete + ": " + strings.Join(manque, ", ") + ")"
}

// apiChatAgent: the sub-agent a message of Claude proposes, as the user checked it on its card, added to
// the team (or changing one of the user's), the message marked.
func (s *Serveur) apiChatAgent(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Index  int        `json:"index"`
		Ancien string     `json:"ancien"`
		Agent  AgentPerso `json:"agent"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	k := s.kitCourant()
	conv := lireConversation(k, p)
	if c.Index < 0 || c.Index >= len(conv.Messages) || conv.Messages[c.Index].Role != "claude" || !reAgent.MatchString(conv.Messages[c.Index].Texte) {
		s.erreur(w, http.StatusBadRequest, "This message proposes no sub-agent")
		return
	}
	verrouAgents.Lock()
	if !agentsLisibles(k) {
		verrouAgents.Unlock()
		s.erreur(w, http.StatusConflict, "extensions/agents.json is unreadable: fix or remove it, nothing was changed.")
		return
	}
	f := lireAgents(k)
	cle, code, err := enregistrerAgentPerso(k, &f, p, strings.TrimSpace(c.Ancien), c.Agent)
	verrouAgents.Unlock()
	if err != nil {
		s.erreur(w, code, err.Error())
		return
	}
	conv = lireConversation(k, p)
	if c.Index < len(conv.Messages) {
		conv.Messages[c.Index].Agent = strings.ToLower(strings.TrimSpace(c.Agent.Nom))
		ecrireConversation(k, p, conv)
	}
	s.msg(w, cle)
}

// apiChatMCP: the MCP server a message of Claude proposes, as the user checked it on its card: the files
// of a server Claude wrote into tools/mcp-ecrits/<id>/ (${ATHANOR_MCP_DIR}), the server into the library
// (as a pasted configuration, api_extensions.go) and switched on in this project, the message marked.
func (s *Serveur) apiChatMCP(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Index       int    `json:"index"`
		Nom         string `json:"nom"`
		Description string `json:"description"`
		Source      string `json:"source"`
		Config      string `json:"config"` // the JSON of the server, as the card shows it
		Windows     bool   `json:"windows"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	k := s.kitCourant()
	conv := lireConversation(k, p)
	if c.Index < 0 || c.Index >= len(conv.Messages) || conv.Messages[c.Index].Role != "claude" || !reMCP.MatchString(conv.Messages[c.Index].Texte) {
		s.erreur(w, http.StatusBadRequest, "This message proposes no MCP server")
		return
	}
	id := identExt(c.Nom)
	if !reIDExt.MatchString(id) || id == "unreal-mcp" {
		s.erreur(w, http.StatusBadRequest, "Invalid name")
		return
	}
	if l, err := lireConfigCollee(c.Config, id); err != nil || len(l) != 1 {
		s.erreur(w, http.StatusBadRequest, "The configuration must hold one server")
		return
	}
	// the files come from Claude's message itself, never from the request
	var prop struct {
		Fichiers map[string]string `json:"fichiers"`
	}
	brut := strings.TrimSpace(reMCP.FindStringSubmatch(conv.Messages[c.Index].Texte)[1])
	brut = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(brut, "```json"), "```"), "```"))
	_ = json.Unmarshal([]byte(brut), &prop)
	if err := verifierFichiersMCP(prop.Fichiers); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	extMu.Lock()
	defer extMu.Unlock()
	if len(prop.Fichiers) > 0 {
		outils := filepath.Join(k.Racine, "tools")
		_ = os.MkdirAll(outils, 0o755)
		d, err := dossierSur(outils, "mcp-ecrits", id)
		if err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		for nom, contenu := range prop.Fichiers {
			if err := ecrireSur(filepath.Join(d, nom), []byte(contenu)); err != nil {
				s.erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	ids, _, code, err := installerMCPPerso(k, c.Nom, c.Config, c.Source, c.Description, c.Windows)
	if err != nil {
		s.erreur(w, code, err.Error())
		return
	}
	choix := lireChoix(k)
	ch := choix[cleProjet(p)]
	if !contient(ch.MCP, ids[0]) {
		ch.MCP = append(ch.MCP, ids[0])
	}
	if ch.Skills == nil {
		ch.Skills = []string{}
	}
	choix[cleProjet(p)] = ch
	if err := ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	conv = lireConversation(k, p)
	if c.Index < len(conv.Messages) {
		conv.Messages[c.Index].MCP = ids[0]
		ecrireConversation(k, p, conv)
	}
	s.msg(w, "“{0}” added to the library and switched on in this project.", strings.TrimSpace(c.Nom))
}

// verifierFichiersMCP: the files of a server Claude wrote: plain names, 20 at most, 1 MB each, 4 MB in all.
func verifierFichiersMCP(f map[string]string) error {
	if len(f) > 20 {
		return errors.New("Too many files for one server (20 at most)")
	}
	total := 0
	for nom, contenu := range f {
		if nom == "" || nomPiece(nom) != nom || strings.HasPrefix(nom, ".") {
			return errors.New("A file of the server has an invalid name")
		}
		if len(contenu) > 1<<20 {
			return errors.New("A file of the server is too large (1 MB at most)")
		}
		total += len(contenu)
	}
	if total > 4<<20 {
		return errors.New("The files of the server are too large (4 MB in all)")
	}
	return nil
}
