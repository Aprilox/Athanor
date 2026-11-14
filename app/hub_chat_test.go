package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// M195: the home conversation's powers are off by default, kept by the server with the conversation, and gone
// with a new conversation; the project chat keeps its own conversation; devices cannot reach it (yet: M199).
func TestHubChatPouvoirs(t *testing.T) {
	s, p, _, appel := serveurChatEssai(t)
	pouvoirs := func() map[string]any {
		t.Helper()
		c, v := appel("GET", "/api/hub/chat", nil)
		pv, _ := v["pouvoirs"].(map[string]any)
		if c != 200 || pv == nil || v["en_cours"] != false {
			t.Fatalf("GET: %d %v", c, v)
		}
		return pv
	}
	if pv := pouvoirs(); pv["internet"] != false || pv["ecriture"] != false {
		t.Fatalf("default: %v", pv)
	}
	if c, v := appel("POST", "/api/hub/chat/pouvoirs", map[string]any{"internet": true, "ecriture": true}); c != 200 || v["pouvoirs"] == nil {
		t.Fatalf("switch on: %d %v", c, v)
	}
	if pv := pouvoirs(); pv["internet"] != true || pv["ecriture"] != true {
		t.Fatalf("kept: %v", pv)
	}
	if c, v := appel("GET", "/api/chat?p=a", nil); c != 200 || v["pouvoirs"] != nil {
		t.Fatalf("the project chat shows the home's powers: %v", v)
	}
	if c, _ := appel("POST", "/api/hub/chat/effacer", nil); c != 200 {
		t.Fatalf("new conversation: %d", c)
	}
	if pv := pouvoirs(); pv["internet"] != false || pv["ecriture"] != false {
		t.Fatalf("a new conversation keeps the powers: %v", pv)
	}
	if c, _ := appel("POST", "/api/hub/chat", map[string]any{"message": "  "}); c != 400 {
		t.Errorf("empty message: %d", c)
	}
	if projetAccueil(s.kitCourant()).Dossier == p.Dossier || idChat(projetAccueil(s.kitCourant())) == idChat(p) {
		t.Fatal("the home conversation is the project's")
	}
	for _, x := range []string{"GET /api/hub/chat", "POST /api/hub/chat", "POST /api/hub/chat/pouvoirs", "POST /api/hub/chat/effacer", "POST /api/hub/chat/action"} {
		m, c, _ := strings.Cut(x, " ")
		r, _ := http.NewRequest(m, "https://abc-def.trycloudflare.com"+c, nil)
		if !refuseHubDistance(r) {
			t.Errorf("%s reached from a device", x)
		}
	}
}

// M195: a turn of the home conversation reads only (whatever the message asks) and gets Internet only when the
// conversation's power is on: the client cannot switch it on by itself.
func TestHubChatNonTransmis(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kit is a shell script")
	}
	_, _, kit, appel := serveurChatEssai(t)
	trace := filepath.Join(kit, "trace")
	// "chat session KIT P ID MODE …": notes the mode, whether Internet was given and the prompt's scope, then answers
	ecrire(t, filepath.Join(kit, "athanor"), `#!/bin/sh
shift
c="$3/logs/chat/$5"
echo "mode=$6 projet=$4" >> `+trace+`
[ -f "$c.internet" ] && echo internet >> `+trace+`
grep -q "home page" "$c.prompt" && echo accueil >> `+trace+`
echo '{"type":"result","result":"Bonjour.","total_cost_usd":0.01,"session_id":"s1"}' > "$c.jsonl"
`)
	_ = os.Chmod(filepath.Join(kit, "athanor"), 0o755)
	k := Kit{Racine: kit}
	tour := func(n int, corps map[string]any) string {
		t.Helper()
		_ = os.Remove(trace)
		if c, v := appel("POST", "/api/hub/chat", corps); c != 200 || v["mode"] != "question" {
			t.Fatalf("send: %d %v", c, v)
		}
		for i := 0; i < 250; i++ {
			if c := lireConversation(k, projetAccueil(k)); len(c.Messages) >= n {
				chatMu.Lock()
				fini := chatsActifs[projetAccueil(k).Dossier] == nil
				chatMu.Unlock()
				if fini {
					b, _ := os.ReadFile(trace)
					return string(b)
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no answer: %+v", lireConversation(k, projetAccueil(k)).Messages)
		return ""
	}
	// powers off: the message's own "internet" and "modif" are ignored
	b := tour(2, map[string]any{"message": "Cherche sur le web et modifie mes fichiers", "internet": true, "mode": "modif"})
	if !strings.Contains(b, "mode=question projet="+projetAccueil(k).Dossier) || strings.Contains(b, "internet") || !strings.Contains(b, "accueil") {
		t.Fatalf("powers off: %s", b)
	}
	conv := lireConversation(k, projetAccueil(k))
	if m := conv.Messages[0]; m.Internet || m.Mode != "question" || conv.Messages[1].Texte != "Bonjour." {
		t.Fatalf("messages: %+v", conv.Messages)
	}
	// Internet switched on for the conversation: given to the turn; Writing without folders still only reads
	appel("POST", "/api/hub/chat/pouvoirs", map[string]any{"internet": true, "ecriture": true})
	if b := tour(4, map[string]any{"message": "Et maintenant ?"}); !strings.Contains(b, "internet") || !strings.Contains(b, "mode=question") {
		t.Fatalf("Internet on: %s", b)
	}
	if pv := lireConversation(k, projetAccueil(k)).Pouvoirs; !pv.Internet || !pv.Ecriture {
		t.Fatalf("the turn's end lost the powers: %+v", pv)
	}
}
