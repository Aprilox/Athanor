package main

// The hub's MCP route (ARCHITECTURE.md, stage 4): POST /api/mcp, JSON-RPC (initialize, tools/list,
// tools/call), outside garde. Only "Authorization: Bearer <session token>" opens it, and that token opens
// nothing else; every call checks the token again (tool, scope, expiry) and goes to the audit log.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OutilHub: a tool of the hub. Faire answers the text the agent reads; an error is a tool error (isError).
type OutilHub struct {
	Description string
	Schema      map[string]any // JSON Schema of the arguments
	Faire       func(s *Serveur, x SessionHub, args json.RawMessage) (string, error)
}

// corpsMaxMCP: a request's body; room for fichier_ecrire's 1 MB once JSON has escaped it.
const corpsMaxMCP = 4 * tailleMaxFichier

// outilsHub: every tool by name (filled by the modules' init, hub_outils_dev.go); a session sees only those of its token.
var outilsHub = map[string]OutilHub{}

// versionsMCP: the protocol versions understood, newest first.
var versionsMCP = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

type requeteMCP struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type erreurMCP struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Serveur) apiMCP(w http.ResponseWriter, r *http.Request) {
	jeton, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	x, ok := SessionHub{}, false
	if bearer {
		x, ok = s.verifierJeton(strings.TrimSpace(jeton))
	}
	if !ok {
		s.auditRefusMCP()
		w.Header().Set("WWW-Authenticate", "Bearer")
		repondreMCP(w, http.StatusUnauthorized, nil, nil, &erreurMCP{-32001, "invalid, expired or revoked session token"})
		return
	}
	var q requeteMCP
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, corpsMaxMCP)).Decode(&q); err != nil {
		if _, trop := errors.AsType[*http.MaxBytesError](err); trop {
			repondreMCP(w, http.StatusOK, nil, nil, &erreurMCP{-32700, fmt.Sprintf("request larger than %d bytes: send large files with sftp_envoyer", corpsMaxMCP)})
			return
		}
		repondreMCP(w, http.StatusOK, nil, nil, &erreurMCP{-32700, "parse error"})
		return
	}
	notification := len(q.ID) == 0 || string(q.ID) == "null"
	if q.JSONRPC != "2.0" || q.Method == "" {
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32600, "invalid request"})
		return
	}
	if notification { // notifications/initialized and the like: nothing to answer
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch q.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(q.Params, &p)
		v := versionsMCP[0]
		for _, c := range versionsMCP {
			if c == p.ProtocolVersion {
				v = c
			}
		}
		repondreMCP(w, http.StatusOK, q.ID, map[string]any{"protocolVersion": v,
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": "athanor-hub", "version": versionAthanor}}, nil)
	case "ping":
		repondreMCP(w, http.StatusOK, q.ID, map[string]any{}, nil)
	case "tools/list":
		outils := []map[string]any{}
		for _, nom := range x.Outils {
			if o, ok := outilsHub[nom]; ok {
				outils = append(outils, map[string]any{"name": nom, "description": o.Description, "inputSchema": o.Schema})
			}
		}
		repondreMCP(w, http.StatusOK, q.ID, map[string]any{"tools": outils}, nil)
	case "tools/call":
		s.appelOutilMCP(w, x, q)
	default:
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32601, "method not found: " + q.Method})
	}
}

// refusMCP (C30): any local process can call /api/mcp; its refusals are counted and logged at most once a
// minute, so that a loop cannot fill the audit log.
var refusMCP struct {
	sync.Mutex
	derniere time.Time
	n        int
}

func (s *Serveur) auditRefusMCP() {
	refusMCP.Lock()
	refusMCP.n++
	if time.Since(refusMCP.derniere) < time.Minute {
		refusMCP.Unlock()
		return
	}
	n := refusMCP.n
	refusMCP.derniere, refusMCP.n = time.Now(), 0
	refusMCP.Unlock()
	r := "jeton refusé"
	if n > 1 {
		r = fmt.Sprintf("jeton refusé (%d fois depuis la ligne précédente)", n)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "inconnu", Vers: "hub", Action: "mcp", Resultat: r})
}

// appelOutilMCP: a tool outside the token's list is answered as unknown (it does not exist for this session).
func (s *Serveur) appelOutilMCP(w http.ResponseWriter, x SessionHub, q requeteMCP) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(q.Params, &p); err != nil || p.Name == "" {
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32602, "invalid params"})
		return
	}
	p.Name = couper(p.Name, 64) // C30: an agent's tool name goes to the audit log and back
	ligne := LigneAudit{Type: "demande", De: x.Agent, Vers: "hub", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent, Action: "mcp " + p.Name}
	o, connu := outilsHub[p.Name]
	permis := false
	for _, n := range x.Outils {
		permis = permis || n == p.Name
	}
	if !connu || !permis {
		ligne.Resultat = "refusé : outil hors du jeton"
		s.ecrireAudit(ligne)
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32602, "unknown tool: " + p.Name})
		return
	}
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage("{}")
	}
	texte, err := o.Faire(s, x, p.Arguments)
	ligne.Resultat = "ok"
	if err != nil {
		texte, ligne.Resultat = err.Error(), "erreur : "+err.Error()
	}
	s.ecrireAudit(ligne)
	repondreMCP(w, http.StatusOK, q.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": texte}}, "isError": err != nil}, nil)
}

func repondreMCP(w http.ResponseWriter, code int, id json.RawMessage, resultat any, e *erreurMCP) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	rep := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		rep["error"] = e
	} else {
		rep["result"] = resultat
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(rep)
}
