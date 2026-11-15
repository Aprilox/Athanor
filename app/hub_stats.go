package main

// The hub's live figures: the milestones done today. Each milestone that becomes done in a PLAN.md (the
// detection of the push notifications, observerPlan) is dated once in dossierAthanor()/hub/jalons.json;
// no git log (a "(in progress)" commit or several commits for one milestone would skew the count).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JalonFait struct {
	Projet string    `json:"projet"` // the project's folder
	Jalon  string    `json:"jalon"`  // its title in PLAN.md
	Date   time.Time `json:"date"`
}

var jalonsMu sync.Mutex

func fichierJalonsFaits() string { return filepath.Join(dossierAthanor(), "hub", "jalons.json") }

func lireJalonsFaits(fichier string) []JalonFait {
	var v []JalonFait
	if b, err := os.ReadFile(fichier); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

// noterJalonFait: dates a milestone that became done; a milestone already noted is not noted again,
// entries older than 30 days are dropped.
func noterJalonFait(fichier, projet, jalon string, quand time.Time) error {
	jalonsMu.Lock()
	defer jalonsMu.Unlock()
	v := []JalonFait{}
	for _, j := range lireJalonsFaits(fichier) {
		if j.Projet == projet && j.Jalon == jalon {
			return nil
		}
		if quand.Sub(j.Date) < 30*24*time.Hour {
			v = append(v, j)
		}
	}
	return ecrireJSONFichier(fichier, append(v, JalonFait{projet, jalon, quand}), 0o600)
}

// jalonsDuJour: the milestones done on the local day of maintenant.
func jalonsDuJour(fichier string, maintenant time.Time) int {
	jalonsMu.Lock()
	defer jalonsMu.Unlock()
	a, m, j := maintenant.Date()
	n := 0
	for _, x := range lireJalonsFaits(fichier) {
		if xa, xm, xj := x.Date.In(maintenant.Location()).Date(); xa == a && xm == m && xj == j {
			n++
		}
	}
	return n
}

// observerPlan: the milestones of p that became done since the last look; faits keeps what was seen,
// per project folder, and plans_vus.json (next to fichier) keeps it across restarts, so a milestone
// ticked while Athanor was stopped is still noted. A project never seen (nothing on disk either) only
// learns what is already there. Each new one is dated in fichier.
func observerPlan(p Projet, faits map[string]map[string]bool, fichier string, quand time.Time) []Jalon {
	b, err := os.ReadFile(p.Plan())
	if err != nil {
		return nil
	}
	vus := filepath.Join(filepath.Dir(fichier), "plans_vus.json")
	avant, connu := faits[p.Dossier]
	if !connu {
		avant, connu = lirePlansVus(vus)[p.Dossier]
	}
	maintenant := map[string]bool{}
	var nouveaux []Jalon
	for _, j := range analyserPlan(string(b)) {
		if j.Fait {
			maintenant[j.Titre] = true
			if connu && !avant[j.Titre] {
				nouveaux = append(nouveaux, j)
				_ = noterJalonFait(fichier, p.Dossier, j.Titre, quand)
			}
		}
	}
	if !connu || len(maintenant) != len(avant) || len(nouveaux) > 0 {
		jalonsMu.Lock()
		v := lirePlansVus(vus)
		v[p.Dossier] = maintenant
		_ = ecrireJSONFichier(vus, v, 0o600)
		jalonsMu.Unlock()
	}
	faits[p.Dossier] = maintenant
	return nouveaux
}

func lirePlansVus(fichier string) map[string]map[string]bool {
	v := map[string]map[string]bool{}
	if b, err := os.ReadFile(fichier); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v
}
