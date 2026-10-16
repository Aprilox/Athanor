package main

// Incremental reading of the logs and translation of Claude Code's stream-json flow into
// readable events (text, tool, result, sub-agent, end of cycle, quota…).

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
)

type Evenement struct {
	N         int64  `json:"n"`
	Genre     string `json:"genre"` // pilote, texte, outil, resultat, erreur, sous_agent, fin, quota, system
	Titre     string `json:"titre"`
	Detail    string `json:"detail"`
	SousAgent bool   `json:"sous_agent"`
	Brut      string `json:"brut"`
	Contexte  int64  `json:"contexte"`
	Heure     string `json:"heure"`
	OutilID   string `json:"-"`     // id of the tool_use (sub-agents) or of the tool_result
	ParentID  string `json:"-"`     // parent_tool_use_id
	Agent     string `json:"agent"` // name of the agent speaking (lead or sub-agent)
}

// Suiveur reads a file as it grows (only what was appended).
type Suiveur struct {
	Chemin   string
	position int64
	reste    string
}

func (s *Suiveur) NouvellesLignes() []string {
	f, err := os.Open(s.Chemin)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	if st.Size() < s.position {
		s.position, s.reste = 0, ""
	}
	if st.Size() == s.position {
		return nil
	}
	if _, err := f.Seek(s.position, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	s.position = st.Size()
	texte := s.reste + string(b)
	lignes := strings.Split(texte, "\n")
	if !strings.HasSuffix(texte, "\n") {
		s.reste = lignes[len(lignes)-1]
		lignes = lignes[:len(lignes)-1]
	} else {
		s.reste = ""
		lignes = lignes[:len(lignes)-1]
	}
	var out []string
	for _, l := range lignes {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// cheminLisible shortens an absolute file path for display: the part inside the workshop
// (".athanor/PLAN.md"), else the last two components ("…/docs/REQUIREMENTS.md"). The full path stays in the detail.
func cheminLisible(p string) string {
	q := strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(q, "/.athanor/"); i >= 0 {
		return q[i+1:]
	}
	if !strings.HasPrefix(q, "/") && !(len(q) > 2 && q[1] == ':') {
		return p // already relative to the project
	}
	parts := strings.Split(strings.Trim(q, "/"), "/")
	if len(parts) <= 2 {
		return q
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

func court(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func joli(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// Traduire converts a stream-json line into events (without number or time: the journal adds them).
func Traduire(ligne string) []Evenement {
	var v map[string]any
	if json.Unmarshal([]byte(ligne), &v) != nil {
		return nil
	}
	sousAgent := v["parent_tool_use_id"] != nil
	parent := str(v["parent_tool_use_id"])
	var out []Evenement
	outilID := ""
	ev := func(genre, titre, detail string, contexte int64) {
		out = append(out, Evenement{Genre: genre, Titre: titre, Detail: detail, SousAgent: sousAgent, Brut: ligne, Contexte: contexte, OutilID: outilID, ParentID: parent})
	}
	msg, _ := v["message"].(map[string]any)
	switch str(v["type"]) {
	case "system":
		if str(v["subtype"]) == "init" {
			ev("system", "Session opened, model "+str(v["model"]), "", 0)
		}
	case "assistant":
		var contexte int64
		if u, ok := msg["usage"].(map[string]any); ok && !sousAgent {
			for _, k := range []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
				if f, ok := u[k].(float64); ok {
					contexte += int64(f)
				}
			}
		}
		contenu, _ := msg["content"].([]any)
		for _, c := range contenu {
			m, _ := c.(map[string]any)
			switch str(m["type"]) {
			case "text":
				t := str(m["text"])
				if strings.TrimSpace(t) != "" {
					ev("texte", court(t, 180), t, contexte)
				}
			case "tool_use":
				nom := str(m["name"])
				outilID = str(m["id"])
				input, _ := m["input"].(map[string]any)
				chemin := str(input["file_path"])
				switch nom {
				case "Bash":
					cmd := str(input["command"])
					desc := str(input["description"])
					if desc == "" {
						desc = cmd
					}
					ev("outil", "Runs: "+court(desc, 150), cmd, contexte)
				case "Read":
					ev("outil", "Reads "+court(cheminLisible(chemin), 120), joli(input), contexte)
				case "Edit", "Write", "MultiEdit", "NotebookEdit":
					verbe := map[string]string{"Edit": "Edits", "Write": "Writes", "MultiEdit": "Edits", "NotebookEdit": "Edits"}[nom]
					ev("outil", verbe+" "+court(cheminLisible(chemin), 120), joli(input), contexte)
				case "Glob", "Grep":
					ev("outil", "Searches "+court(str(input["pattern"]), 100), joli(input), contexte)
				case "Agent", "Task":
					ev("sous_agent", "Sub-agent "+str(input["subagent_type"])+": "+court(str(input["description"]), 120), str(input["prompt"]), contexte)
				default:
					ev("outil", nom+" "+court(joli(input), 110), joli(input), contexte)
				}
			}
		}
	case "user":
		contenu, _ := msg["content"].([]any)
		for _, c := range contenu {
			m, _ := c.(map[string]any)
			if str(m["type"]) != "tool_result" {
				continue
			}
			outilID = str(m["tool_use_id"])
			erreur, _ := m["is_error"].(bool)
			texte := ""
			switch x := m["content"].(type) {
			case string:
				texte = x
			case []any:
				var parts []string
				for _, p := range x {
					if pm, ok := p.(map[string]any); ok {
						parts = append(parts, str(pm["text"]))
					}
				}
				texte = strings.Join(parts, "\n")
			}
			if strings.TrimSpace(texte) == "" && !erreur {
				continue
			}
			genre := "resultat"
			if erreur {
				genre = "erreur"
			}
			ev(genre, court(texte, 150), texte, 0)
		}
	case "result":
		r := str(v["result"])
		cout, _ := v["total_cost_usd"].(float64)
		tours, _ := v["num_turns"].(float64)
		duree, _ := v["duration_ms"].(float64)
		ev("fin", "End of cycle: "+itoa(int64(tours))+" turns, "+itoa(int64(duree/60000))+" min, "+ftoa(cout)+" $ — "+court(r, 120), r, 0)
	case "rate_limit_event":
		if info, ok := v["rate_limit_info"].(map[string]any); ok && str(info["status"]) != "allowed" {
			ev("quota", "Usage limit: "+str(info["status"]), ligne, 0)
		}
	}
	return out
}

func LignePilote(l string) Evenement {
	genre := "pilote"
	switch {
	case strings.Contains(strings.ToLower(l), "quota"):
		genre = "quota"
	case strings.Contains(l, "Error"), strings.Contains(l, "ERROR"), strings.Contains(l, "NO CONNECTION"), strings.Contains(l, "ALERT"), strings.Contains(l, "Warning"):
		genre = "erreur"
	case strings.Contains(l, "succeeded"), strings.Contains(l, "DONE"):
		genre = "fin"
	}
	return Evenement{Genre: genre, Titre: l, Brut: l}
}

// Agent: the project lead or a sub-agent, with what it is doing right now.
type Agent struct {
	ID       string `json:"id"`
	Nom      string `json:"nom"`
	Mission  string `json:"mission"`
	Statut   string `json:"statut"` // travaille, termine, attend
	Activite string `json:"activite"`
	Bulle    string `json:"bulle"`
	Tours    int    `json:"tours"`
	Depuis   string `json:"depuis"`
	Fin      string `json:"fin"`
	Contexte int64  `json:"contexte"`
}

// Journal keeps the latest events in memory and serves them to the interface.
type Journal struct {
	mu            sync.Mutex
	kit           Projet
	evenements    []Evenement
	compteur      int64
	suiveurPilote *Suiveur
	suiveurs      map[string]*Suiveur // logs of the current cycle (lead + workers), by path
	prefixeCycle  string
	Contexte      int64
	max           int
	agents        map[string]*Agent
	ordreAgents   []string
}

func NouveauJournal(k Projet) *Journal {
	j := &Journal{kit: k, max: 6000}
	j.Reinitialiser(k)
	return j
}

func (j *Journal) Reinitialiser(k Projet) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.kit = k
	j.evenements = nil
	j.Contexte = 0
	j.nouveauCycle()
	j.suiveurPilote = &Suiveur{Chemin: k.PiloteLog()}
	for _, l := range QueueFichier(k.PiloteLog(), 150) {
		j.pousser(LignePilote(l))
	}
	j.suiveurPilote.NouvellesLignes()
	j.suiveurs = map[string]*Suiveur{}
	j.prefixeCycle = ""
	j.lireCycle()
}

// lireCycle follows the logs of the current cycle; a new cycle resets the team.
func (j *Journal) lireCycle() string {
	fichiers := j.kit.JournauxCycle()
	if len(fichiers) == 0 {
		return ""
	}
	// Same cycle as long as the prefix does not change: in parallel mode, the lead's log only appears at
	// merge time, after the workers' logs (already read, not to be read again).
	pref := strings.TrimSuffix(fichiers[0], ".jsonl")
	if o := OuvrierDe(fichiers[0]); o != "" {
		pref = strings.TrimSuffix(pref, "-"+o)
	}
	if pref != j.prefixeCycle {
		j.prefixeCycle = pref
		j.suiveurs = map[string]*Suiveur{}
		j.Contexte = 0
		j.nouveauCycle()
	}
	for _, f := range fichiers {
		s := j.suiveurs[f]
		if s == nil {
			s = &Suiveur{Chemin: f}
			j.suiveurs[f] = s
		}
		ouvrier := OuvrierDe(f)
		for _, l := range s.NouvellesLignes() {
			j.pousserCycle(l, ouvrier)
		}
	}
	return fichiers[0]
}

func (j *Journal) pousser(e Evenement) {
	j.compteur++
	e.N = j.compteur
	e.Heure = heureCourte()
	j.evenements = append(j.evenements, e)
	if len(j.evenements) > j.max {
		j.evenements = j.evenements[len(j.evenements)-j.max+500:]
	}
}

func (j *Journal) nouveauCycle() {
	j.agents = map[string]*Agent{"chef": {ID: "chef", Nom: "Project lead", Mission: "Leads the current milestone: reads the plan, codes, checks, commits.", Statut: "attend", Depuis: heureCourte()}}
	j.ordreAgents = []string{"chef"}
}

// pousserCycle translates a line of a cycle log; ouvrier = "" for the lead, otherwise the worker's
// milestone (parallel mode), which has its own card in the team.
func (j *Journal) pousserCycle(l string, ouvrier string) {
	for _, e := range Traduire(l) {
		if e.Contexte > 0 && ouvrier == "" {
			j.Contexte = e.Contexte
		}
		// who is speaking?
		id := "chef"
		if ouvrier != "" {
			id = "ouvrier-" + ouvrier
		}
		if e.SousAgent && e.ParentID != "" {
			id = e.ParentID
		}
		a := j.agents[id]
		if a == nil {
			a = &Agent{ID: id, Nom: "Sub-agent", Statut: "travaille", Depuis: heureCourte()}
			if ouvrier != "" && !e.SousAgent {
				a.Nom, a.Mission = "Worker "+ouvrier, "Builds milestone "+ouvrier
			}
			j.agents[id] = a
			j.ordreAgents = append(j.ordreAgents, id)
		}
		e.Agent = a.Nom
		if ouvrier != "" {
			e.Titre = "[" + ouvrier + "] " + e.Titre
		}
		switch e.Genre {
		case "sous_agent":
			nom := strings.TrimPrefix(e.Titre, "Sub-agent ")
			mission := ""
			if i := strings.Index(nom, ": "); i > 0 {
				mission = nom[i+2:]
				nom = nom[:i]
			}
			j.agents[e.OutilID] = &Agent{ID: e.OutilID, Nom: strings.Title(nom), Mission: mission, Statut: "travaille", Depuis: heureCourte()}
			j.ordreAgents = append(j.ordreAgents, e.OutilID)
			a.Activite = "Waits for sub-agent " + nom
			a.Statut = "attend"
		case "texte":
			a.Bulle = e.Detail
			a.Statut = "travaille"
			a.Tours++
		case "outil":
			a.Activite = e.Titre
			a.Statut = "travaille"
			a.Tours++
		case "resultat", "erreur":
			if sa, ok := j.agents[e.OutilID]; ok && sa.ID != "chef" {
				sa.Statut, sa.Fin = "termine", heureCourte()
				if e.Genre == "erreur" {
					sa.Activite = "Finished with an error"
				} else {
					sa.Activite = "Report handed to the project lead"
				}
				if chef := j.agents["chef"]; chef != nil {
					chef.Statut = "travaille"
					if strings.HasPrefix(chef.Activite, "Waits for sub-agent ") {
						chef.Activite = "Reviews the report of " + strings.ToLower(sa.Nom)
					}
				}
			}
		case "fin":
			if ouvrier != "" {
				a.Statut, a.Fin, a.Activite = "termine", heureCourte(), "Report handed to the project lead"
			} else {
				a.Statut, a.Fin, a.Activite = "termine", heureCourte(), "Cycle finished"
			}
		}
		if !e.SousAgent {
			a.Contexte = e.Contexte
		}
		j.pousser(e)
	}
}

// Agents returns the team of the current cycle, in order of appearance.
func (j *Journal) Agents() []Agent {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Agent
	for _, id := range j.ordreAgents {
		if a := j.agents[id]; a != nil {
			out = append(out, *a)
		}
	}
	return out
}

// Lire re-reads the files and returns the events after the given number.
func (j *Journal) Lire(apres int64) ([]Evenement, int64, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, l := range j.suiveurPilote.NouvellesLignes() {
		j.pousser(LignePilote(l))
	}
	fichier := j.lireCycle()
	var out []Evenement
	for i := len(j.evenements) - 1; i >= 0; i-- {
		if j.evenements[i].N <= apres {
			break
		}
		out = append(out, j.evenements[i])
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out, j.Contexte, fichier
}
