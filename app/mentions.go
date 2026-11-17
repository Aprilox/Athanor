package main

// Mentions of the home bar (@project, @server, @API): what the owner can name, by readable name, never a value.
// A chip carries a ref the Go checks again (project name, server id, vault entry id: names are not unique).
// M287: one type "api" for every vault entry (the old "cle" and "fournisseur" chips are still read), found by
// its name or its category, and filtered by category.

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

// MentionTrouvee: one suggestion of the @ list (Detail: a server's host, an entry's type; never a value).
type MentionTrouvee struct {
	Type   string `json:"type"` // projet | serveur | api | dossier (a folder pointed to the home conversation)
	Ref    string `json:"ref"`
	Nom    string `json:"nom"`
	Detail string `json:"detail,omitempty"`
	// an API's category: its id (the filter) and its name (searched, shown)
	Categorie    string `json:"categorie,omitempty"`
	CategorieNom string `json:"categorie_nom,omitempty"`
}

// MentionDemandee: one chip the bar sends (type nouveau: "New project", no ref).
type MentionDemandee struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
}

const maxMentions = 20

const mentionsParGroupe = 5 // the @ list of the home: 4 types × 5 stays within maxMentions

var errMentionInconnue = errors.New("unknown mention")

// typesAccueil: the groups of the home bar's @ list.
var typesAccueil = []string{"projet", "serveur", "api", "dossier"}

// mentionAPI: an old chip read as the API it names: "cle" by the same vault entry, "fournisseur" by the entry
// of its token (an unknown provider is left as it is, so refused).
func mentionAPI(d MentionDemandee) MentionDemandee {
	if d.Type == "fournisseur" && d.Ref != "" {
		l, _ := lireFournisseurs()
		i := slices.IndexFunc(l, func(f Fournisseur) bool { return f.ID == d.Ref })
		if i < 0 {
			return d
		}
		d.Ref = l[i].Cle
	}
	if d.Type == "cle" || d.Type == "fournisseur" {
		d.Type = "api"
	}
	return d
}

// candidatsMentions: every project (the active one first, then the newest), server and API of the vault.
func (s *Serveur) candidatsMentions() []MentionTrouvee {
	s.mu.Lock()
	projets, actif := append([]Projet{}, s.config.Projets...), s.config.ProjetActif
	s.mu.Unlock()
	var l []MentionTrouvee
	slices.Reverse(projets)
	slices.SortStableFunc(projets, func(a, b Projet) int { return boolInt(b.Nom == actif) - boolInt(a.Nom == actif) })
	for _, p := range projets {
		if !p.Archive && p.Existe() { // a project whose folder is gone is not offered (Projects says so)
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
		categories := map[string]string{}
		for _, c := range s.coffre.Categories() {
			categories[c.ID] = c.Nom
		}
		for _, e := range s.coffre.Liste() {
			if e.Type == "ssh" && slices.ContainsFunc(serveurs, func(sv ServeurInventaire) bool { return sv.Cle == e.ID }) {
				continue // the ssh key a server is reached with is part of that server, not an API
			}
			l = append(l, MentionTrouvee{Type: "api", Ref: e.ID, Nom: e.Nom, Detail: e.Type, Categorie: e.Categorie, CategorieNom: categories[e.Categorie]})
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

// chercherMentions: the candidates whose name (or detail, or category name) contains q, case-insensitive, at most
// mentionsParGroupe of each type of the home (14 projects never hide the servers and API), kept grouped by type;
// typ: one type only, maxMentions of it (M252); categorie: only the API of that category (its id, or
// sansCategorie); autres = how many more per type; sauf: the chips already chosen ("type:ref"), left out before
// the cut (M244k7).
func (s *Serveur) chercherMentions(q, typ, categorie string, sauf ...string) (l []MentionTrouvee, autres map[string]int) {
	q, typ = strings.ToLower(strings.TrimSpace(q)), mentionAPI(MentionDemandee{Type: typ}).Type
	pris := map[string]bool{}
	for _, x := range sauf {
		ty, ref, _ := strings.Cut(x, ":")
		d := mentionAPI(MentionDemandee{Type: ty, Ref: ref})
		pris[d.Type+":"+d.Ref] = true
	}
	parType := map[string][]MentionTrouvee{}
	for _, m := range s.candidatsMentions() {
		if pris[m.Type+":"+m.Ref] {
			continue
		}
		if categorie != "" && (m.Type != "api" || m.Categorie != categorie && (categorie != sansCategorie || m.Categorie != "")) {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(m.Nom), q) || strings.Contains(strings.ToLower(m.Detail), q) || strings.Contains(strings.ToLower(m.CategorieNom), q) {
			parType[m.Type] = append(parType[m.Type], m)
		}
	}
	l, autres = []MentionTrouvee{}, map[string]int{}
	types, max := typesAccueil, mentionsParGroupe
	if typ != "" {
		types, max = []string{typ}, maxMentions
	}
	for _, ty := range types {
		g := parType[ty]
		if len(g) > max {
			autres[ty], g = len(g)-max, g[:max]
		}
		l = append(l, g...)
	}
	return l, autres
}

// apiMentions: GET /api/mentions?q=&type=&categorie=&sauf=, the @ list of the home bar (type: one kind only, M252).
func (s *Serveur) apiMentions(w http.ResponseWriter, r *http.Request) {
	l, autres := s.chercherMentions(r.URL.Query().Get("q"), r.URL.Query().Get("type"), r.URL.Query().Get("categorie"), r.URL.Query()["sauf"]...)
	s.ecrireJSON(w, map[string]any{"mentions": l, "autres": autres})
}

// Mentions: what the owner mentioned (@project, @server, @key), resolved by the Go (resoudreMentions):
// project names, server ids and vault entry ids (names are not unique); Nouveau: the "New project" chip.
type Mentions struct {
	Projets  []string `json:"projets"`
	Serveurs []string `json:"serveurs"`
	Cles     []string `json:"cles"`
	Nouveau  bool     `json:"nouveau,omitempty"`
}

// resoudreMentions: the chips checked against what exists now (a removed server, a hostile ref → errMentionInconnue,
// a 400 for the API); duplicates are dropped. Projets = names, Serveurs = ids, Cles = vault entry ids.
func (s *Serveur) resoudreMentions(l []MentionDemandee) (Mentions, error) {
	if len(l) > maxMentions {
		return Mentions{}, errMentionInconnue
	}
	connus := map[string]bool{}
	for _, c := range s.candidatsMentions() {
		connus[c.Type+"\x00"+c.Ref] = true
	}
	m := Mentions{Projets: []string{}, Serveurs: []string{}, Cles: []string{}}
	for _, d := range l {
		if d.Type == "nouveau" {
			m.Nouveau = true
			continue
		}
		if d.Type == "dossier" { // the home conversation's: a mission gets no folder this way
			continue
		}
		if d = mentionAPI(d); !connus[d.Type+"\x00"+d.Ref] {
			return Mentions{}, errMentionInconnue
		}
		cible := map[string]*[]string{"projet": &m.Projets, "serveur": &m.Serveurs, "api": &m.Cles}[d.Type]
		if !slices.Contains(*cible, d.Ref) {
			*cible = append(*cible, d.Ref)
		}
	}
	return m, nil
}
