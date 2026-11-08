package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func serveurChatEssai(t *testing.T) (*Serveur, Projet, string, func(meth, chemin string, corps any) (int, map[string]any)) {
	t.Helper()
	kit, _ := filepath.EvalSymlinks(t.TempDir())
	d, _ := filepath.EvalSymlinks(t.TempDir())
	p := Projet{Nom: "a", Dossier: d}
	_ = os.MkdirAll(p.Atelier(), 0o755)
	s := nouveauServeur(Config{Kit: kit, Projets: []Projet{p}, ProjetActif: "a", Langue: "fr"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(meth, chemin string, corps any) (int, map[string]any) {
		var b []byte
		if corps != nil {
			b, _ = json.Marshal(corps)
		}
		r := httptest.NewRequest(meth, chemin, bytes.NewReader(b))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	return s, p, kit, appel
}

// Files attached to a chat message: into the chat's attachments, listed for Claude; a note sent to the pilot
// names them.
func TestChatPieces(t *testing.T) {
	s, p, _, appel := serveurChatEssai(t)
	r := httptest.NewRequest("POST", "/api/pieces?p=a&cible=chat", strings.NewReader("png"))
	r.Header.Set("X-Athanor", "jeton")
	r.Header.Set("X-Nom-Fichier", url.QueryEscape("capture écran.png"))
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.routes(mux)
	mux.ServeHTTP(w, r)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	chemin, _ := v["chemin"].(string)
	if w.Code != 200 || chemin != ".athanor/attachments/chat/capture écran.png" {
		t.Fatalf("upload: %d %v", w.Code, v)
	}
	if _, err := os.Stat(p.Demande()); err == nil {
		t.Fatal("a chat file went into the request")
	}
	if l, err := piecesChat(p, []string{chemin}); err != nil || len(l) != 1 {
		t.Fatalf("valid: %v %v", l, err)
	}
	for _, x := range []string{".athanor/attachments/chat/../../REQUEST.md", ".athanor/attachments/chat/absent.png", ".athanor/attachments/request/x.png", "/etc/passwd", ".athanor/attachments/chat/"} {
		if _, err := piecesChat(p, []string{x}); err == nil {
			t.Errorf("accepted: %q", x)
		}
	}
	// a message with files only is accepted (the pilot stopped: Claude may change files, nothing is run here
	// since the project is held by another action)
	s.occuper("a")
	if c, _ := appel("POST", "/api/chat?p=a", map[string]any{"message": "", "pieces": []string{chemin}}); c != http.StatusConflict {
		t.Errorf("files only: %d", c)
	}
	s.liberer("a")
	if c, v := appel("POST", "/api/chat?p=a", map[string]any{"message": "x", "pieces": []string{"../x"}}); c != 400 || v["erreur_key"] != "Unknown attached file" {
		t.Errorf("bad file: %d %v", c, v)
	}
	// the note names the files of the message it answers, once
	avant := []MessageChat{{Role: "vous", Pieces: []string{"a/old.png"}}, {Role: "claude"}, {Role: "vous", Pieces: []string{chemin, ".athanor/attachments/chat/b.pdf"}}}
	if n := noteAvecPieces("Corriger le bouton (voir `.athanor/attachments/chat/b.pdf`)", avant, "fr"); n != "Corriger le bouton (voir `.athanor/attachments/chat/b.pdf`) (Fichiers joints: `"+chemin+"`)" {
		t.Errorf("note: %q", n)
	}
	if n := noteAvecPieces("x", avant[:2], "en"); n != "x (Attached files: `a/old.png`)" {
		t.Errorf("note: %q", n)
	}
	if n := noteAvecPieces("x", nil, "en"); n != "x" {
		t.Errorf("note: %q", n)
	}
}

// An MCP server Claude wrote in the chat: its files into tools/mcp-ecrits/<id>/, the server into the library,
// switched on in the project, the message marked; the pilot's configuration finds its folder.
func TestChatMCP(t *testing.T) {
	_, p, kit, appel := serveurChatEssai(t)
	k := Kit{Racine: kit}
	prop := "```json\n" + `{"nom":"Mon Logiciel","description":"Pilote le logiciel","fichiers":{"server.py":"print('mcp')"},"config":{"command":"uv","args":["run","--script","${ATHANOR_MCP_DIR}/server.py"]}}` + "\n```"
	ecrireConversation(k, p, conversationChat{Messages: []MessageChat{{Role: "vous", Texte: "un MCP"}, {Role: "claude", Texte: "Voici.\n<mcp>" + prop + "</mcp>"}}})
	if c, v := appel("POST", "/api/chat/mcp?p=a", map[string]any{"index": 0, "nom": "x", "config": "{}"}); c != 400 || v["erreur_key"] != "This message proposes no MCP server" {
		t.Fatalf("no proposal: %d %v", c, v)
	}
	if c, _ := appel("POST", "/api/chat/mcp?p=a", map[string]any{"index": 1, "nom": "Mon Logiciel", "config": `{"a":{"command":"x"},"b":{"command":"y"}}`}); c != 400 {
		t.Fatalf("two servers: %d", c)
	}
	cfg := `{"command":"uv","args":["run","--script","${ATHANOR_MCP_DIR}/server.py"],"env":{"API_KEY":"sk-123"}}`
	if c, v := appel("POST", "/api/chat/mcp?p=a", map[string]any{"index": 1, "nom": "Mon Logiciel", "description": "Pilote le logiciel", "config": cfg}); c != 200 {
		t.Fatalf("add: %d %v", c, v)
	}
	if b, err := os.ReadFile(filepath.Join(dossierMCPEcrit(kit, "mon-logiciel"), "server.py")); err != nil || string(b) != "print('mcp')" {
		t.Fatalf("server file: %q %v", b, err)
	}
	b := lireBiblio(k)
	x, ok := b.MCP["mon-logiciel"]
	if !ok || x.Description != "Pilote le logiciel" || x.Config.Env["API_KEY"] != "${API_KEY}" || lireSecrets(k)["mon-logiciel"]["API_KEY"] != "sk-123" {
		t.Fatalf("library: %+v", x)
	}
	if req := secretsRequis(x.Config); len(req) != 1 || req[0] != "API_KEY" {
		t.Errorf("keys asked: %v", req)
	}
	if !contient(lireChoix(k)[cleProjet(p)].MCP, "mon-logiciel") {
		t.Error("not switched on in the project")
	}
	if conv := lireConversation(k, p); conv.Messages[1].MCP != "mon-logiciel" {
		t.Errorf("message not marked: %+v", conv.Messages[1])
	}
	var def objetJSON
	_ = json.Unmarshal([]byte(`{"config":{"type":"stdio","command":"uv","args":["run","--script","${ATHANOR_MCP_DIR}/server.py"]}}`), &def)
	srv, raison := serveurMCP(kit, "mon-logiciel", def, objetJSON{}, p.Dossier, "")
	if srv == nil {
		t.Fatalf("config: %s", raison)
	}
	j, _ := json.Marshal(srv)
	if want, _ := json.Marshal(filepath.Join(kit, "tools", "mcp-ecrits", "mon-logiciel") + "/server.py"); !bytes.Contains(j, bytes.Trim(want, `"`)) {
		t.Errorf("the server's folder: %s", j)
	}
	// hostile files in the proposal: nothing written
	ecrireConversation(k, p, conversationChat{Messages: []MessageChat{{Role: "claude", Texte: `<mcp>{"fichiers":{"../evil.py":"x"},"config":{"command":"uv"}}</mcp>`}}})
	if c, _ := appel("POST", "/api/chat/mcp?p=a", map[string]any{"index": 0, "nom": "evil", "config": `{"command":"uv"}`}); c != 400 {
		t.Errorf("bad file name: %d", c)
	}
	if _, ok := lireBiblio(k).MCP["evil"]; ok {
		t.Error("installed despite a bad file")
	}
	if verifierFichiersMCP(map[string]string{"a.py": strings.Repeat("x", 1<<20+1)}) == nil || verifierFichiersMCP(map[string]string{".env": "x"}) == nil || verifierFichiersMCP(map[string]string{"a.py": "x", "README.md": "y"}) != nil {
		t.Error("files' checks")
	}
}

// A change asked while the pilot runs: the pilot stops at the end of its cycle, Claude changes the files,
// the pilot starts again with a note; Stop or the stop cancelled in the app give the change up.
func TestChatPausePilote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kit is a shell script")
	}
	s, p, kit, appel := serveurChatEssai(t)
	k := Kit{Racine: kit}
	trace := filepath.Join(kit, "trace")
	// the kit: "arreter --apres-cycle P" writes STOP, "chat session KIT P ID …" answers and commits, "demarrer … P"
	// starts the pilot (and removes STOP, as the launcher)
	ecrire(t, filepath.Join(kit, "athanor"), `#!/bin/sh
shift
echo "$*" >> `+trace+`
case "$1" in
arreter) touch "$3/.athanor/STOP" ;;
chat) echo '{"type":"result","result":"Fait.","total_cost_usd":0.01,"session_id":"s1"}' > "$3/logs/chat/$5.jsonl"; echo abc1234 > "$3/logs/chat/$5.commit" ;;
demarrer) for a in "$@"; do d="$a"; done; rm -f "$d/.athanor/STOP"; touch "$d/.athanor/pilot.pid" ;;
esac
`)
	_ = os.Chmod(filepath.Join(kit, "athanor"), 0o755)
	t.Setenv("CLAUDE_BIN", "/bin/true") // demarrerPilote, as root in a container
	attenteCycleChat = 20 * time.Millisecond
	defer func() { attenteCycleChat = 2 * time.Second }()
	pid := filepath.Join(p.Atelier(), "pilot.pid")
	stop := filepath.Join(p.Atelier(), "STOP")
	_ = os.WriteFile(pid, nil, 0o644)
	marqueAuto(p, true)
	attendre := func(n int) conversationChat {
		t.Helper()
		for i := 0; i < 250; i++ {
			if c := lireConversation(k, p); len(c.Messages) >= n && !chatAttendCycle(p) {
				chatMu.Lock()
				fini := chatsActifs[p.Dossier] == nil
				chatMu.Unlock()
				if fini {
					return c
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no answer: %+v", lireConversation(k, p).Messages)
		return conversationChat{}
	}

	// 1. the change waits for the end of the cycle, then is made, and the pilot starts again
	if c, v := appel("POST", "/api/chat?p=a", map[string]any{"message": "Change le titre", "mode": "modif"}); c != 200 || v["mode"] != "attente" {
		t.Fatalf("send: %d %v", c, v)
	}
	if !fichierExiste(stop) || !chatAttendCycle(p) {
		t.Fatal("the pilot was not asked to stop after its cycle")
	}
	if _, v := appel("GET", "/api/chat?p=a", nil); v["partiel"].(map[string]any)["mode"] != "attente" {
		t.Fatalf("waiting: %v", v)
	}
	time.Sleep(100 * time.Millisecond)
	if b, _ := os.ReadFile(trace); strings.Contains(string(b), "chat session") {
		t.Fatal("Claude changed the files while the pilot ran")
	}
	_ = os.Remove(pid) // the cycle ends
	conv := attendre(2)
	m := conv.Messages[1]
	if m.Role != "claude" || m.Commit != "abc1234" || m.Pilote != "relance" || !conv.Messages[0].Pause || conv.Messages[0].Mode != "modif" {
		t.Fatalf("answer: %+v / %+v", conv.Messages[0], m)
	}
	for i := 0; i < 100 && !fichierExiste(pid); i++ { // (started in the background)
		time.Sleep(20 * time.Millisecond)
	}
	if !fichierExiste(pid) || fichierExiste(stop) || s.estOccupe("a") {
		t.Fatal("the pilot did not start again, or the project stays held")
	}
	if b, _ := os.ReadFile(p.Demande()); !strings.Contains(string(b), "abc1234") || !strings.Contains(string(b), "Change le titre") {
		t.Fatalf("no note for the pilot: %s", b)
	}
	if b, _ := os.ReadFile(trace); !strings.Contains(string(b), "chat session "+kit+" "+p.Dossier) || !strings.Contains(string(b), " modif ") {
		t.Fatalf("trace: %s", b)
	}

	// 2. Stop while waiting: the stop is withdrawn, the pilot goes on, nothing is changed
	_ = os.Remove(trace)
	appel("POST", "/api/chat?p=a", map[string]any{"message": "Autre", "mode": "modif"})
	if c, _ := appel("POST", "/api/chat/arreter?p=a", nil); c != 200 || fichierExiste(stop) {
		t.Fatalf("stop: %d, STOP %v", c, fichierExiste(stop))
	}
	conv = attendre(4)
	if m := conv.Messages[3]; m.Role != "erreur" || m.Texte != "Stopped." || m.Pilote != "" {
		t.Fatalf("stopped: %+v", m)
	}

	// 3. the stop cancelled from the app: the change is given up
	appel("POST", "/api/chat?p=a", map[string]any{"message": "Encore", "mode": "modif"})
	_ = retirerStop(p)
	conv = attendre(6)
	if m := conv.Messages[5]; m.Role != "erreur" || !strings.Contains(m.Texte, "stop was cancelled") {
		t.Fatalf("cancelled: %+v", m)
	}
	if b, _ := os.ReadFile(trace); strings.Contains(string(b), "chat session") {
		t.Fatalf("a change was made: %s", b)
	}

	// 4. without "modif", the running pilot gets a question turn, never a pause
	_ = os.WriteFile(pid, nil, 0o644)
	if _, v := appel("POST", "/api/chat?p=a", map[string]any{"message": "Pourquoi ?"}); v["mode"] != "question" || fichierExiste(stop) {
		t.Fatalf("question: %v", v)
	}
	conv = attendre(8)

	// 5. the pilot stopped: the change at once, and the pilot stays stopped
	_ = os.Remove(pid)
	if _, v := appel("POST", "/api/chat?p=a", map[string]any{"message": "Change", "mode": "modif"}); v["mode"] != "modif" {
		t.Fatalf("stopped pilot: %v", v)
	}
	conv = attendre(10)
	if m := conv.Messages[9]; m.Commit != "abc1234" || m.Pilote != "" || conv.Messages[8].Pause || fichierExiste(pid) {
		t.Fatalf("stopped pilot: %+v", m)
	}
}
