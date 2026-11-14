package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// M198: a sub-agent or an MCP server the home conversation proposes (<agent>, <mcp>) is a card: nothing is in the
// library before the user launches it, then it goes on the projects ticked; a Windows server asks its own
// confirmation, a key in clear is refused; "put it on my-shop" is an attribuer card.
func TestHubChatEquipe(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	s, p, _, appel := serveurChatEssai(t)
	k := s.kitCourant()
	h := projetAccueil(k)
	_ = os.MkdirAll(h.Dossier, 0o700)
	ecrireConversation(k, h, conversationChat{Messages: []MessageChat{
		{Role: "vous", Texte: "Crée-moi un traducteur et mets-le sur a"},
		{Role: "claude", Texte: "Voici.\n<agent>{\"nom\":\"traducteur\",\"description\":\"Traduit les textes.\",\"consignes\":\"Traduis fidèlement.\",\"modele\":\"eco\",\"lecture_seule\":true,\"projets\":[\"A\"]}</agent>"},
		{Role: "claude", Texte: "<mcp>{\"nom\":\"Blender\",\"description\":\"Pilote Blender\",\"config\":{\"command\":\"blender.exe\",\"args\":[]},\"windows\":true}</mcp>"},
		{Role: "claude", Texte: "<mcp>{\"nom\":\"meteo\",\"config\":{\"url\":\"https://x.example/mcp\",\"headers\":{\"Authorization\":\"Bearer sk-vraie-cle-123456789abc\"}}}</mcp>"},
		{Role: "claude", Texte: "<mcp>{\"nom\":\"notes\",\"description\":\"Notes\",\"config\":{\"command\":\"uv\",\"args\":[\"run\",\"--script\",\"${ATHANOR_MCP_DIR}/server.py\"]},\"fichiers\":{\"server.py\":\"print(1)\"}}</mcp>"},
		{Role: "claude", Texte: "<action>{\"type\":\"attribuer\",\"element\":\"mcp\",\"nom\":\"notes\",\"projets\":[\"a\"]}</action>"},
		{Role: "claude", Texte: "<action>{\"type\":\"attribuer\",\"element\":\"agent\",\"nom\":\"traducteur\",\"projets\":[\"inconnu\"]}</action>"},
	}})
	_, v := appel("GET", "/api/hub/chat", nil)
	cartes, _ := v["actions"].(map[string]any)
	carte := func(i string) map[string]any { c, _ := cartes[i].(map[string]any); return c }
	if len(cartes) != 6 {
		t.Fatalf("cards: %v", v["actions"])
	}
	if c := carte("1"); c["type"] != "agent" || c["sur"] != "traducteur → a" || c["erreur"] != nil || c["modele"] != "eco" {
		t.Fatalf("agent card: %v", c)
	}
	if c := carte("2"); c["erreur"] != "Confirm that this server may run outside the cage, with your rights." || !strings.Contains(c["config"].(string), "blender.exe") {
		t.Fatalf("windows card: %v", c)
	}
	if e := carte("3")["erreur"]; e == nil || !strings.Contains(e.(string), "${NAME}") {
		t.Fatalf("a key in clear can be launched: %v", e)
	}
	if c := carte("4"); c["erreur"] != nil || len(c["fichiers"].([]any)) != 1 {
		t.Fatalf("written server card: %v", c)
	}
	if e := carte("5")["erreur"]; e != "Not in your library: add it first." {
		t.Fatalf("attribuer before adding: %v", e)
	}
	if e := carte("6")["erreur"]; e != "Unknown project: choose one of yours." {
		t.Fatalf("unknown project: %v", e)
	}
	// nothing in the library before a card is launched
	if f := lireAgents(k); len(f.Agents) != 0 || len(lireBiblio(k).MCP) != 0 {
		t.Fatalf("created before the card: %+v %+v", f.Agents, lireBiblio(k).MCP)
	}
	lancer := func(i int, a map[string]any) (int, map[string]any) {
		return appel("POST", "/api/hub/chat/action", map[string]any{"index": i, "decision": "lancer", "action": a})
	}
	// the agent, as the card shows it, on the project ticked
	if c, v := lancer(1, map[string]any{"nom": "traducteur", "description": "Traduit les textes.", "consignes": "Traduis fidèlement.", "modele": "eco", "lecture_seule": true, "projets": []string{"a"}}); c != 200 {
		t.Fatalf("agent: %d %v", c, v)
	}
	if f := lireAgents(k); len(f.Agents) != 1 || f.Agents[0].Modele != "eco" || !f.Agents[0].LectureSeule || !slices.Equal(f.Agents[0].Projets, []string{p.Dossier}) {
		t.Fatalf("agent in the library: %+v", f.Agents)
	}
	// a Windows server: refused without its own confirmation, then outside the cage and on no project
	windows := map[string]any{"nom": "Blender", "description": "Pilote Blender", "config": `{"command":"blender.exe","args":[]}`, "windows": true}
	if c, _ := lancer(2, windows); c != 400 {
		t.Fatalf("windows server without confirmation: %d", c)
	}
	windows["confirme_windows"] = true
	if c, v := lancer(2, windows); c != 200 {
		t.Fatalf("windows server: %d %v", c, v)
	}
	if m := lireBiblio(k).MCP["blender"]; m.Config.Type != "windows" || slices.Contains(lireChoix(k)[cleProjet(p)].MCP, "blender") {
		t.Fatalf("windows server: %+v %+v", m, lireChoix(k))
	}
	// a key in clear: refused, kept nowhere
	if c, _ := lancer(3, map[string]any{"nom": "meteo", "config": `{"url":"https://x.example/mcp","headers":{"Authorization":"Bearer sk-vraie-cle-123456789abc"}}`}); c != 400 {
		t.Fatalf("key in clear launched: %d", c)
	}
	if b, _ := os.ReadFile(filepath.Join(dossierExt(k), "secrets.json")); strings.Contains(string(b), "sk-vraie") {
		t.Fatal("the key in clear was kept")
	}
	// a server Claude wrote: its files from the message only (never from the request), in no project yet
	if c, v := lancer(4, map[string]any{"nom": "notes", "description": "Notes", "config": `{"command":"uv","args":["run","--script","${ATHANOR_MCP_DIR}/server.py"]}`, "fichiers": []string{"pirate.py"}}); c != 200 {
		t.Fatalf("written server: %d %v", c, v)
	}
	if b, err := os.ReadFile(filepath.Join(k.Racine, "tools", "mcp-ecrits", "notes", "server.py")); err != nil || string(b) != "print(1)" {
		t.Fatalf("server's file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(k.Racine, "tools", "mcp-ecrits", "notes", "pirate.py")); err == nil {
		t.Fatal("a file named by the request was written")
	}
	if slices.Contains(lireChoix(k)[cleProjet(p)].MCP, "notes") {
		t.Fatal("switched on in a project nobody ticked")
	}
	// "put it on a": the attribuer card, now launchable
	_, v = appel("GET", "/api/hub/chat", nil)
	if e := v["actions"].(map[string]any)["5"].(map[string]any)["erreur"]; e != nil {
		t.Fatalf("attribuer card: %v", e)
	}
	if c, v := lancer(5, map[string]any{"element": "mcp", "nom": "notes", "projets": []string{"a"}}); c != 200 {
		t.Fatalf("attribuer: %d %v", c, v)
	}
	if !slices.Contains(lireChoix(k)[cleProjet(p)].MCP, "notes") {
		t.Fatalf("not switched on in a: %+v", lireChoix(k))
	}
	// the library is in what the assistant is told (names and projects, never a configuration)
	if e := s.texteEtatHub(); !strings.Contains(e, "sub-agent traducteur: on a") || !strings.Contains(e, "MCP server notes (notes): on a") || strings.Contains(e, "blender.exe") {
		t.Fatalf("state for the assistant:\n%s", e)
	}
}
