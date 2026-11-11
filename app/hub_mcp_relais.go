package main

// athanor mcp-hub TOKEN_FILE: the MCP stdio server Claude starts for the hub's tools (ARCHITECTURE.md,
// stage 4). Each line read on stdin goes to POST /api/mcp of the running Athanor (port read in
// instance.json, the session token read in TOKEN_FILE, both at every request: Athanor may restart); its
// answer goes back on stdout. Athanor closed or the token refused: a clear error, never a wait without end.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const nomMCPHub = "athanor-hub"

func init() {
	enregistrerCommande("mcp-hub", "mcp-hub TOKEN_FILE: the hub's tools for the pilot's session, MCP stdio relayed to the running Athanor", func(args []string) int {
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: athanor mcp-hub TOKEN_FILE")
			return 2
		}
		relaisMCPHub(os.Stdin, os.Stdout, args[0], fichierInstance(), 60*time.Second)
		return 0
	})
}

// relaisMCPHub: one request at a time until stdin ends.
// shortcut: requests are relayed in turn (a slow tool holds the next ones), concurrent calls if a tool ever needs it.
func relaisMCPHub(in io.Reader, out io.Writer, fichierJeton, fichierInst string, delai time.Duration) {
	client := &http.Client{Timeout: delai}
	lecteur := bufio.NewReader(in)
	for {
		ligne, err := lecteur.ReadBytes('\n')
		if l := bytes.TrimSpace(ligne); len(l) > 0 {
			if rep := relaisUneRequete(client, l, fichierJeton, fichierInst); rep != nil {
				_, _ = out.Write(append(rep, '\n'))
			}
		}
		if err != nil {
			return
		}
	}
}

// relaisUneRequete: the line to write back (nil for a notification or a client's answer).
func relaisUneRequete(client *http.Client, ligne []byte, fichierJeton, fichierInst string) []byte {
	var q requeteMCP
	if json.Unmarshal(ligne, &q) != nil {
		return reponseRelais(nil, nil, &erreurMCP{-32700, "parse error"})
	}
	if len(q.ID) == 0 || string(q.ID) == "null" || q.Method == "" {
		return nil // notifications (the hub has nothing to do with them) and answers
	}
	corps, panne := envoyerHub(client, ligne, fichierJeton, fichierInst)
	if panne == "" {
		return corps
	}
	switch q.Method {
	case "initialize": // the server stays connected; its tools answer the error until Athanor is back
		return reponseRelais(q.ID, map[string]any{"protocolVersion": versionsMCP[0],
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": nomMCPHub, "version": versionAthanor}}, nil)
	case "ping":
		return reponseRelais(q.ID, map[string]any{}, nil)
	case "tools/list":
		return reponseRelais(q.ID, map[string]any{"tools": []any{}}, nil)
	case "tools/call":
		return reponseRelais(q.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": panne}}, "isError": true}, nil)
	}
	return reponseRelais(q.ID, nil, &erreurMCP{-32000, panne})
}

// envoyerHub: the hub's answer, or why there is none (the text the agent reads).
func envoyerHub(client *http.Client, ligne []byte, fichierJeton, fichierInst string) ([]byte, string) {
	const ferme = "Athanor is not open on this computer: the hub's tools are unavailable. Carry on without them; note in .athanor/BLOCKERS.md what needed them."
	b, err := os.ReadFile(fichierInst)
	var i infoInstance
	if err != nil || json.Unmarshal(b, &i) != nil || i.Port <= 0 {
		return nil, ferme
	}
	jeton, err := os.ReadFile(fichierJeton)
	if err != nil || len(bytes.TrimSpace(jeton)) == 0 {
		return nil, "No hub session token: this pilot was not started by Athanor (or was stopped), so the hub's tools are unavailable."
	}
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(i.Port)+"/api/mcp", bytes.NewReader(ligne))
	req.Header.Set("Authorization", "Bearer "+string(bytes.TrimSpace(jeton)))
	req.Header.Set("Content-Type", "application/json")
	r, err := client.Do(req)
	if err != nil {
		return nil, ferme
	}
	defer r.Body.Close()
	corps, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	switch {
	case r.StatusCode == http.StatusUnauthorized:
		return nil, "The hub refused this session's token (expired or revoked: the pilot was stopped or restarted). The hub's tools are unavailable until the next start."
	case err != nil || r.StatusCode != http.StatusOK || !json.Valid(corps):
		return nil, "The hub did not answer properly (HTTP " + strconv.Itoa(r.StatusCode) + "). Try again later."
	}
	return bytes.TrimSpace(corps), ""
}

func reponseRelais(id json.RawMessage, resultat any, e *erreurMCP) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	rep := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		rep["error"] = e
	} else {
		rep["result"] = resultat
	}
	b, _ := json.Marshal(rep)
	return b
}

// systemeMCPHub: "windows" (native), "wsl" (the hub's loopback is Windows', the relay runs there) or "unix".
var systemeMCPHub = func() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	if interopWindows() {
		return "wsl"
	}
	return "unix"
}

// serveurMCPHub: the athanor-hub entry of a pilot's mcp.json, nil when Athanor gave it no token
// (hub-jeton next to the file: the pilot's private folder).
func serveurMCPHub(kit, fichier string) *objetJSON {
	jeton := filepath.Join(filepath.Dir(fichier), "hub-jeton")
	if st, err := os.Stat(jeton); err != nil || !st.Mode().IsRegular() {
		return nil
	}
	var s objetJSON
	s.mettre("type", "stdio")
	switch systemeMCPHub() {
	case "windows":
		s.mettre("command", filepath.Join(kit, "Athanor.exe"))
		s.mettre("args", []string{"mcp-hub", jeton})
	case "wsl":
		s.mettre("command", filepath.Join(kit, "athanor"))
		s.mettre("args", []string{"mcp-windows", "--", filepath.Join(kit, "Athanor.exe"), "mcp-hub", cheminWindowsWSL(jeton)})
	default:
		s.mettre("command", filepath.Join(kit, "athanor"))
		s.mettre("args", []string{"mcp-hub", jeton})
	}
	return &s
}

// mcpSansHub: the merge session's MCP file, the pilot's without athanor-hub (only the main pilot asks the
// hub); "" when nothing is left.
func mcpSansHub(fichier string) string {
	serveurs := lireJSONOrdonne(fichier).objet("mcpServers")
	if !serveurs.a(nomMCPHub) {
		return fichier
	}
	var reste objetJSON
	for _, k := range serveurs.cles {
		if k != nomMCPHub {
			reste.mettre(k, serveurs.valeur[k])
		}
	}
	f := strings.TrimSuffix(fichier, ".json") + "-fusion.json"
	if len(reste.cles) == 0 || ecrireMCP(f, reste) != nil {
		os.Remove(f)
		return ""
	}
	return f
}
