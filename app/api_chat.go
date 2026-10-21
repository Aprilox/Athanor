package main

// Talking with Claude about a project, from the application: a conversation of short Claude Code
// sessions (claude -p, continued with --resume) run in the project's folder.
//   - Questions: always, with read-only tools (Read, Glob, Grep).
//   - Changes: only while the pilot is stopped (they would collide with its work): Claude may then edit
//     the project's files (no commands), and Athanor commits what changed. While the pilot runs, Claude
//     writes the change as a note for the pilot instead, which the page sends with one click.
// File tools stay inside the project (system/guard.py as a hook, as for the pilot). The conversation is
// kept in the kit's logs (logs/chat/), never in the project.

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
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
	Cout   float64  `json:"cout,omitempty"`
	Commit string   `json:"commit,omitempty"` // the commit of the changes (modif)
	Note   bool     `json:"note,omitempty"`   // the note was sent to the pilot
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
%s`

const consigneQuestion = `You can only read: no change, no command. The pilot is RUNNING on this project, so you must not change
anything yourself. When the user asks for a change, explain briefly what should change, then write the
instruction for the pilot, on its own, between <note> and </note> (short, precise, self-contained, in the
user's language): Athanor shows it with a button that sends it to the pilot, which reads it at its next cycle.`

const consigneQuestionArrete = `You can only read in this turn (the user asked a question).`

const consigneModif = `The pilot is stopped: you may change the project's files (Edit, Write) to do what the user asks; you
cannot run commands. Keep the change small and focused, never touch .athanor/ (the pilot's workshop) unless
asked, and end with the list of the files you changed. Athanor commits your changes for you.`

var reNote = regexp.MustCompile(`(?s)<note>(.*?)</note>`)

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
		rep["partiel"] = map[string]any{"textes": textes, "outils": outils, "mode": t.mode}
	}
	s.ecrireJSON(w, rep)
}

func (s *Serveur) apiChatEnvoyer(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Message string `json:"message"`
		Modele  string `json:"modele"`
		Mode    string `json:"mode"` // "question" forces a read-only turn even with the pilot stopped
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Message) == "" {
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
	k := s.kitCourant()
	mode := "modif"
	if p.PiloteActif() || c.Mode == "question" {
		mode = "question"
	}
	chatMu.Lock()
	if chatsActifs[p.Dossier] != nil {
		chatMu.Unlock()
		s.erreur(w, http.StatusConflict, "Claude is still answering: wait for it, or stop it.")
		return
	}
	if mode == "modif" && !s.occuper(p.Nom) { // no pilot start while Claude changes the files
		chatMu.Unlock()
		s.erreur(w, http.StatusConflict, "Another action is under way on this project.")
		return
	}
	id := idChat(p) + "-" + time.Now().Format("150405")
	d := dossierChat(k)
	if err := os.MkdirAll(d, 0o700); err != nil {
		chatMu.Unlock()
		if mode == "modif" {
			s.liberer(p.Nom)
		}
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	en := &chatEnCours{journal: filepath.Join(d, id+".jsonl"), mode: mode, modele: c.Modele}
	chatsActifs[p.Dossier] = en
	chatMu.Unlock()

	conv := lireConversation(k, p)
	conv.Messages = append(conv.Messages, MessageChat{Role: "vous", Texte: strings.TrimSpace(c.Message), Mode: mode, Modele: c.Modele, Quand: time.Now().Format(time.RFC3339)})
	ecrireConversation(k, p, conv)

	consigne := consigneQuestion
	if mode == "modif" {
		consigne = consigneModif
	} else if !p.PiloteActif() {
		consigne = consigneQuestionArrete
	}
	sujet := strings.Join(strings.Fields(c.Message), " ")
	if utf8.RuneCountInString(sujet) > 60 {
		sujet = string([]rune(sujet)[:59]) + "…"
	}
	fichiers := map[string]string{
		id + ".msg":     strings.TrimSpace(c.Message),
		id + ".prompt":  strings.Replace(consigneChat, "%s", consigne, 1),
		id + ".subject": "Chat: " + sujet,
	}
	for nom, contenu := range fichiers {
		if err := os.WriteFile(filepath.Join(d, nom), []byte(contenu), 0o600); err != nil {
			s.finChat(k, p, en, "", err.Error())
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	cmd := commandeWSL(k, scriptChat(p, id, mode, c.Modele, conv.Session))
	cacherFenetre(cmd)
	en.cmd = cmd
	if err := cmd.Start(); err != nil {
		s.finChat(k, p, en, "", err.Error())
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
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
		for _, ext := range []string{".msg", ".prompt", ".subject", ".commit", ".settings.json", ".err"} {
			os.Remove(filepath.Join(d, id+ext))
		}
	}()
	s.ecrireJSON(w, map[string]string{"mode": mode})
}

// finChat: the turn's answer goes into the conversation.
func (s *Serveur) finChat(k Kit, p Projet, en *chatEnCours, commit, detail string) {
	textes, outils, final, session, cout, erreur := lireTour(en.journal)
	conv := lireConversation(k, p)
	if session != "" {
		conv.Session = session
	}
	texte := strings.TrimSpace(final)
	if texte == "" && len(textes) > 0 {
		texte = strings.TrimSpace(textes[len(textes)-1])
	}
	m := MessageChat{Role: "claude", Texte: texte, Outils: outils, Mode: en.mode, Modele: en.modele, Quand: time.Now().Format(time.RFC3339), Cout: cout, Commit: commit}
	switch {
	case en.arrete:
		m.Role, m.Texte = "erreur", "Stopped."
	case erreur != "" || (texte == "" && detail != ""):
		m.Role = "erreur"
		m.Texte = court(firstNonEmpty(erreur, firstNonEmpty(detail, "No answer.")), 300)
	case texte == "":
		m.Role, m.Texte = "erreur", "No answer."
	}
	conv.Messages = append(conv.Messages, m)
	ecrireConversation(k, p, conv)
	chatMu.Lock()
	delete(chatsActifs, p.Dossier)
	chatMu.Unlock()
	if en.mode == "modif" {
		s.liberer(p.Nom)
	}
	os.Remove(en.journal)
}

func (s *Serveur) apiChatArreter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	chatMu.Lock()
	en := chatsActifs[p.Dossier]
	if en != nil {
		en.arrete = true
		if en.cmd != nil && en.cmd.Process != nil {
			arreterArbre(en.cmd)
		}
	}
	chatMu.Unlock()
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
	if err := p.AjouterRemarque(strings.TrimSpace(m[1]), libelle(s.langue(), "remarques"), libelle(s.langue(), "remarques_aide")); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	conv.Messages[c.Index].Note = true
	ecrireConversation(k, p, conv)
	s.msg(w, "Note sent: the pilot reads it at its next cycle.")
}

// scriptChat: one turn, in the project's folder (WSL or Git Bash). The paths and the message go through
// files of logs/chat (no quoting issue); file tools are kept inside the project by guard.py.
func scriptChat(p Projet, id, mode, modele, session string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	outils := `--allowedTools Read Glob Grep --disallowedTools Bash Edit Write MultiEdit NotebookEdit WebFetch WebSearch Task`
	if mode == "modif" {
		outils = `--permission-mode acceptEdits --allowedTools Read Glob Grep Edit Write MultiEdit --disallowedTools Bash NotebookEdit WebFetch WebSearch Task`
	}
	return chargerEnv + `case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) export PATH="$PWD/system/natif:$PATH" ;; esac
export MSYS2_ARG_CONV_EXCL='*'   # native Windows: Claude's arguments as they are (the paths go through w)
K="$PWD"; C="$K/logs/chat/` + id + `"
w() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi; }
D=` + q(p.Dossier) + `
P=$(wslpath -u "$D" 2>/dev/null || cygpath -u "$D" 2>/dev/null || printf '%s' "$D")
cd "$P" || { echo "Project folder not found: $D" >&2; exit 2; }
python3 "$(w "$K/system/chat.py")" settings "$(w "$P")" "$(w "$C.settings.json")" > /dev/null || { echo "Cannot write the chat's settings" >&2; exit 2; }
args=(-p "$(cat "$C.msg")" --output-format stream-json --verbose --max-turns 40 --settings "$(w "$C.settings.json")" --append-system-prompt "$(cat "$C.prompt")" ` + outils + `)
M=` + q(modele) + `; [ -n "$M" ] && args+=(--model "$M")
S=` + q(session) + `; [ -n "$S" ] && args+=(--resume "$S")
timeout 1200 "${CLAUDE_BIN:-claude}" "${args[@]}" < /dev/null > "$C.jsonl" 2> "$C.err"
code=$?
if [ ` + q(mode) + ` = modif ] && [ -d .git ] && [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  git add -A && git commit -q -F "$(w "$C.subject")" && git rev-parse --short HEAD > "$C.commit"
fi
exit $code`
}
