package main

// The Overview's savings, from stats.tsv: only what is measured. The cache's share at list price (reads
// paid a tenth), what RTK spared (its own measures), and the cycles run with and without Ponytail or the
// eco mode side by side on this project (a trend: the milestones differ, it is no controlled test).

import (
	"math"
	"strings"
)

// ComparaisonVue: the successful cycles with an option against those without, on this project.
type ComparaisonVue struct {
	Option     string  `json:"option"` // "ponytail", "eco"
	AvecN      int     `json:"avec_n"`
	SansN      int     `json:"sans_n"`
	AvecCout   float64 `json:"avec_cout"`   // average cost of a successful cycle, list price
	SansCout   float64 `json:"sans_cout"`   //
	AvecSortie int64   `json:"avec_sortie"` // average tokens written by Claude per successful cycle
	SansSortie int64   `json:"sans_sortie"` //
	Ecart      int     `json:"ecart"`       // the cost with against without, in % (negative: cheaper)
}

type GainsVue struct {
	CachePct        int              `json:"cache_pct"` // the cost of what Claude reads saved by the cache, in %
	RTKEntree       int64            `json:"rtk_entree"`
	RTKEconomise    int64            `json:"rtk_economise"`
	RTKPct          int              `json:"rtk_pct"`
	Ponytail        bool             `json:"ponytail"`
	VersionPonytail string           `json:"version_ponytail"`
	Comparaisons    []ComparaisonVue `json:"comparaisons,omitempty"`
	// the successful cycles counted for Ponytail's comparison, with it and without it (told while there are
	// fewer than minComparaison on a side: why no gain is shown yet)
	PonytailAvec int `json:"ponytail_avec"`
	PonytailSans int `json:"ponytail_sans"`
}

// minComparaison: cycles needed on each side before a comparison is shown.
const minComparaison = 3

func calculerGains(stats []LigneStat, ponytail bool) *GainsVue {
	g := &GainsVue{Ponytail: ponytail, VersionPonytail: versionPonytail}
	var entree, ecrit, lu float64
	for _, s := range stats {
		entree += float64(s.EntreeTokens)
		ecrit += float64(s.CacheEcrit)
		lu += float64(s.CacheLu)
		g.RTKEntree += s.RTKEntree
		g.RTKEconomise += s.RTKEconomise
	}
	// list prices: a cache write 1.25 times an input token, a cache read a tenth
	if sans := entree + ecrit + lu; sans > 0 {
		g.CachePct = int(math.Round((1 - (entree+1.25*ecrit+0.1*lu)/sans) * 100))
	}
	if g.RTKEntree > 0 {
		g.RTKPct = int(math.Round(float64(g.RTKEconomise) / float64(g.RTKEntree) * 100))
	}
	for _, opt := range []string{"ponytail", "eco"} {
		var c ComparaisonVue
		c.Option = opt
		var coutA, coutS float64
		var sortA, sortS int64
		for _, s := range stats {
			// the milestones built by the lead: no framing, no worker line, a commit
			if s.Etat != "ok" || s.Commit == "" || strings.Contains(s.Cycle, "·") || aOption(s.Options, "cadrage") {
				continue
			}
			if aOption(s.Options, opt) {
				c.AvecN++
				coutA += s.CoutUSD
				sortA += s.SortieTokens
			} else {
				c.SansN++
				coutS += s.CoutUSD
				sortS += s.SortieTokens
			}
		}
		if opt == "ponytail" {
			g.PonytailAvec, g.PonytailSans = c.AvecN, c.SansN
		}
		if c.AvecN < minComparaison || c.SansN < minComparaison {
			continue
		}
		c.AvecCout, c.SansCout = coutA/float64(c.AvecN), coutS/float64(c.SansN)
		c.AvecSortie, c.SansSortie = sortA/int64(c.AvecN), sortS/int64(c.SansN)
		if c.SansCout > 0 {
			c.Ecart = int(math.Round((c.AvecCout/c.SansCout - 1) * 100))
		}
		g.Comparaisons = append(g.Comparaisons, c)
	}
	return g
}

func aOption(options, o string) bool {
	for _, x := range strings.Split(options, ",") {
		if strings.TrimSpace(x) == o {
			return true
		}
	}
	return false
}
