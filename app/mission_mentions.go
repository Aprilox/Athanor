package main

// Mentions of the home bar (@project, @server, @key): what the owner can name, by readable name, never a value.
// A chip carries a ref the Go checks again (project name, server id, vault entry id: names are not unique).

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

// MentionTrouvee: one suggestion of the @ list (Detail: a server's host, an entry's type; never a value).
type MentionTrouvee struct {
	Type   string `json:"type"` // projet | serveur | cle | dossier (a folder pointed to the home conversation)
	Ref    string `json:"ref"`
	Nom    string `json:"nom"`
	Detail string `json:"detail,omitempty"`
}

// MentionDemandee: one chip the bar sends (type nouveau: "New project", no ref).
type MentionDemandee struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
}

const maxMentions = 20

var errMentionInconnue = errors.New("unknown mention")

// candidatsMentions: every project (the active one first, then the newest), server and vault entry.
func (s *Serveur) candidatsMentions() []MentionTrouvee {
	s.mu.Lock()
	projets, actif := append([]Projet{}, s.config.Projets...), s.config.ProjetActif
	s.mu.Unlock()
	var l []MentionTrouvee
	slices.Reverse(projets)
	slices.SortStableFunc(projets, func(a, b Projet) int { return boolInt(b.Nom == actif) - boolInt(a.Nom == actif) })
	for _, p := range projets {
		if !p.Archive {
			l = append(l, MentionTrouvee{Type: "projet", Ref: p.Nom, Nom: p.Nom})
		}
	}
	for _, x := range s.devDistants() { // a remote Athanor's project: nom@instance, its ref names the remote by id
		for _, p := range x.projets {
			l = append(l, MentionTrouvee{Type: "projet", Ref: p.Nom + "@" + x.d.ID, Nom: p.Nom + "@" + x.d.Nom, Detail: x.d.Nom})
		}
	}
	serveurs, _ := lireServeurs() // an unreadable inventory: no server to mention
	for _, sv := range serveurs {
		l = append(l, MentionTrouvee{Type: "serveur", Ref: sv.ID, Nom: sv.Nom, Detail: sv.Hote})
	}
	if s.coffre != nil && s.coffreErr == nil {
		for _, e := range s.coffre.Liste() {
			l = append(l, MentionTrouvee{Type: "cle", Ref: e.ID, Nom: e.Nom, Detail: e.Type})
		}
	}
	for _, d := range lireConversation(s.kitCourant(), projetAccueil(s.kitCourant())).Pouvoirs.Dossiers {
		l = append(l, MentionTrouvee{Type: "dossier", Ref: d.Chemin, Nom: filepath.Base(d.Chemin), Detail: d.Chemin})
	}
	return l
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// chercherMentions: the candidates whose name (or detail) contains q, case-insensitive, at most 20, shared
// in turn between the types (30 projects never hide the servers and keys), kept grouped by type.
func (s *Serveur) chercherMentions(q string) []MentionTrouvee {
	q = strings.ToLower(strings.TrimSpace(q))
	parType := map[string][]MentionTrouvee{}
	for _, m := range s.candidatsMentions() {
		if q == "" || strings.Contains(strings.ToLower(m.Nom), q) || strings.Contains(strings.ToLower(m.Detail), q) {
			parType[m.Type] = append(parType[m.Type], m)
		}
	}
	types, pris := []string{"projet", "serveur", "cle", "dossier"}, map[string]int{}
	for n, avance := 0, true; n < maxMentions && avance; {
		avance = false
		for _, ty := range types {
			if n < maxMentions && pris[ty] < len(parType[ty]) {
				pris[ty]++
				n++
				avance = true
			}
		}
	}
	l := []MentionTrouvee{}
	for _, ty := range types {
		l = append(l, parType[ty][:pris[ty]]...)
	}
	return l
}

// apiMentions: GET /api/mentions?q=, the @ list of the home bar.
func (s *Serveur) apiMentions(w http.ResponseWriter, r *http.Request) {
	s.ecrireJSON(w, map[string]any{"mentions": s.chercherMentions(r.URL.Query().Get("q"))})
}

// resoudreMentions: the chips checked against what exists now (a removed server, a hostile ref → errMentionInconnue,
// a 400 for the API); duplicates are dropped. Projets = names, Serveurs = ids, Cles = vault entry ids.
func (s *Serveur) resoudreMentions(l []MentionDemandee) (MentionsMission, error) {
	if len(l) > maxMentions {
		return MentionsMission{}, errMentionInconnue
	}
	connus := map[string]bool{}
	for _, c := range s.candidatsMentions() {
		connus[c.Type+"\x00"+c.Ref] = true
	}
	m := MentionsMission{Projets: []string{}, Serveurs: []string{}, Cles: []string{}}
	for _, d := range l {
		if d.Type == "nouveau" {
			m.Nouveau = true
			continue
		}
		if d.Type == "dossier" { // the home conversation's: a mission gets no folder this way
			continue
		}
		if !connus[d.Type+"\x00"+d.Ref] {
			return MentionsMission{}, errMentionInconnue
		}
		cible := map[string]*[]string{"projet": &m.Projets, "serveur": &m.Serveurs, "cle": &m.Cles}[d.Type]
		if !slices.Contains(*cible, d.Ref) {
			*cible = append(*cible, d.Ref)
		}
	}
	return m, nil
}
