package main

// The models the settings and the chat propose. Never a fixed list of versions: the aliases (which always
// mean the newest model of their family), then the full names this PC has really used (learned from the
// results of its cycles, logs/tarifs.json), so a new model shows up as soon as a cycle has run on it.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

var aliasModeles = []string{"opus", "sonnet", "haiku", "fable"}

var reModele = regexp.MustCompile(`^claude-[a-z0-9.-]{2,60}$`)

func modelesConnus(k Kit) []string {
	vus := map[string]bool{}
	var noms []string
	b, err := os.ReadFile(filepath.Join(k.Journaux(), "tarifs.json"))
	if err == nil && len(b) < 4<<20 {
		var t struct {
			Modeles map[string]json.RawMessage `json:"modeles"`
		}
		if json.Unmarshal(b, &t) == nil {
			for nom := range t.Modeles {
				if reModele.MatchString(nom) && !vus[nom] {
					vus[nom] = true
					noms = append(noms, nom)
				}
			}
		}
	}
	sort.Slice(noms, func(a, b int) bool { return noms[a] > noms[b] }) // newest version numbers first, roughly
	return append(append([]string{}, aliasModeles...), noms...)
}

func (s *Serveur) apiModeles(w http.ResponseWriter, r *http.Request) {
	s.ecrireJSON(w, map[string]any{"modeles": modelesConnus(s.kitCourant())})
}
