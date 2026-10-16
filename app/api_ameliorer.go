package main

// "Improve with Claude": rewrites a request (new project or queued request) into a clearer, richer
// version, without changing what the user wants. The interface keeps the user's version so that
// they can go back to it.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const consigneAmeliorer = `You improve a project request written by a user for an autonomous AI developer (Claude Code, driven by a tool called Athanor, which plans the project into milestones and builds it alone).
Rewrite the request so that it is clear, complete and actionable, WITHOUT changing what the user wants:
- Keep every requirement, constraint, name, number and technology the user gave. Never contradict or drop them.
- Make the implicit explicit: the goal, who it is for, the platform(s), the main features (as a list), the content, the look and feel, the constraints, and what "done" looks like.
- When something important is unspecified, add a sensible default and mark it with "(suggestion)" so the user can spot it. Do not add scope the request does not call for.
- Do not choose technologies unless the user did: the developer chooses.
- Plain Markdown with short headings and bullet lists, about 400 words at most.
- Write in the same language as the request.
- Output ONLY the improved request: no preamble, no comment, no code fence around it.

The request:
<<<
%s
>>>`

// one rewrite at a time: each one is a Claude session on the user's quota
var ameliorationEnCours sync.Mutex

func (s *Serveur) apiAmeliorerDemande(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil || strings.TrimSpace(c.Texte) == "" {
		s.erreur(w, http.StatusBadRequest, "Write your request first")
		return
	}
	if utf8.RuneCountInString(c.Texte) > 20000 {
		s.erreur(w, http.StatusBadRequest, "Request too long to be improved (20,000 characters at most)")
		return
	}
	if !ameliorationEnCours.TryLock() {
		s.erreur(w, http.StatusConflict, "Claude is already improving a request")
		return
	}
	defer ameliorationEnCours.Unlock()

	k := s.kitCourant()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	nom := ".request-" + hex.EncodeToString(b) + ".txt"
	chemin := filepath.Join(k.Journaux(), nom)
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	consigne := strings.Replace(consigneAmeliorer, "%s", strings.TrimSpace(c.Texte), 1)
	if err := os.WriteFile(chemin, []byte(consigne), 0o600); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(chemin)

	// One turn, no project around it: an empty temporary folder as working directory, so that the
	// session has nothing to read or change. The request travels through a file (no quoting issue).
	script := chargerEnv + `f="$PWD/logs/` + nom + `"
d=$(mktemp -d) || exit 1
cd "$d" || exit 1
timeout 240 "${CLAUDE_BIN:-claude}" -p "$(cat "$f")" --max-turns 1 --output-format text ${MODEL:+--model "$MODEL"} < /dev/null
code=$?
cd / && rm -rf "$d"
exit $code`
	cmd := commandeWSL(k, script)
	cacherFenetre(cmd)
	var erreurs strings.Builder
	cmd.Stderr = &erreurs
	fini := make(chan struct{})
	var sortie []byte
	var err error
	go func() { sortie, err = cmd.Output(); close(fini) }()
	select {
	case <-fini:
	case <-time.After(260 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		s.erreur(w, http.StatusGatewayTimeout, "Claude took too long: try again in a moment")
		return
	}
	texte := strings.TrimSpace(strings.ReplaceAll(string(sortie), "\r\n", "\n"))
	if strings.HasPrefix(texte, "```") { // a code fence around the whole answer, despite the instruction
		if i := strings.Index(texte, "\n"); i >= 0 {
			texte = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(texte[i+1:]), "```"))
		}
	}
	if err != nil || texte == "" {
		detail := strings.TrimSpace(erreurs.String())
		if detail == "" {
			detail = texte
		}
		if i := strings.LastIndex(detail, "\n"); i >= 0 {
			detail = detail[i+1:]
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"erreur_key": "Claude could not improve the request: {0}", "args": []string{court(detail, 200)}})
		return
	}
	s.ecrireJSON(w, map[string]string{"texte": texte})
}
