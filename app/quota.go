package main

// The subscription's quota as Claude Code last reported it: the "rate_limit_event" lines of the cycle
// logs (stream-json) carry, for each window (the 5-hour session, the week, a model's own week such as
// Fable's), how much is used and when it starts again. The quota is the account's, shared by every
// project: the newest line of any project wins.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FenetreQuota struct {
	Nom         string   `json:"nom"`                   // five_hour, seven_day, seven_day_opus, seven_day_fable…
	Utilisation *float64 `json:"utilisation,omitempty"` // 0..1, when Claude Code gives it
	Reinit      int64    `json:"reinit"`                // when it starts again (Unix seconds)
}

type EtatQuota struct {
	Fenetres []FenetreQuota `json:"fenetres"`
	Statut   string         `json:"statut"` // allowed, allowed_warning, rejected
	Lu       int64          `json:"lu"`     // date of the log that said it (Unix seconds)
}

var (
	quotaMu    sync.Mutex
	quotaCache *EtatQuota
	quotaQuand time.Time
)

// quotaActuel: the latest quota report of these projects' logs (cached a few seconds: the page asks
// every 1.5 s).
func quotaActuel(projets []Projet) *EtatQuota {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	if time.Since(quotaQuand) < 8*time.Second {
		return quotaCache
	}
	quotaQuand = time.Now()
	type journal struct {
		chemin string
		date   time.Time
	}
	var js []journal
	for _, p := range projets {
		entrees, err := os.ReadDir(p.LogsDir())
		if err != nil {
			continue
		}
		for _, e := range entrees {
			if !strings.HasPrefix(e.Name(), "cycle-") || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if i, err := e.Info(); err == nil && i.Mode().IsRegular() {
				js = append(js, journal{filepath.Join(p.LogsDir(), e.Name()), i.ModTime()})
			}
		}
	}
	sort.Slice(js, func(a, b int) bool { return js[a].date.After(js[b].date) })
	for i, j := range js {
		if i >= 6 { // the newest logs only: a cycle that said nothing about the quota is rare
			break
		}
		if q := quotaDuJournal(j.chemin); q != nil {
			q.Lu = j.date.Unix()
			quotaCache = q
			return q
		}
	}
	return quotaCache
}

// quotaDuJournal: the last rate_limit_event of a log (read from its end), nil when there is none.
func quotaDuJournal(chemin string) *EtatQuota {
	f, err := os.Open(chemin)
	if err != nil {
		return nil
	}
	defer f.Close()
	const fin = 2 << 20
	if st, err := f.Stat(); err == nil && st.Size() > fin {
		_, _ = f.Seek(st.Size()-fin, io.SeekStart)
	}
	var derniere []byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		l := sc.Bytes()
		if bytes.Contains(l, []byte(`"rate_limit_event"`)) {
			derniere = append(derniere[:0], l...)
		}
	}
	if derniere == nil {
		return nil
	}
	return analyserQuota(derniere)
}

func analyserQuota(ligne []byte) *EtatQuota {
	var v struct {
		Type string `json:"type"`
		Info struct {
			Status   string   `json:"status"`
			ResetsAt int64    `json:"resetsAt"`
			Genre    string   `json:"rateLimitType"`
			Util     *float64 `json:"utilization"`
			Fenetres map[string]struct {
				Util     *float64 `json:"utilization"`
				ResetsAt int64    `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(ligne, &v) != nil || v.Type != "rate_limit_event" {
		return nil
	}
	q := &EtatQuota{Statut: v.Info.Status}
	for nom, f := range v.Info.Fenetres {
		if len(nom) > 40 {
			continue
		}
		q.Fenetres = append(q.Fenetres, FenetreQuota{Nom: nom, Utilisation: borner(f.Util), Reinit: f.ResetsAt})
	}
	if len(q.Fenetres) == 0 && v.Info.Genre != "" && len(v.Info.Genre) <= 40 {
		q.Fenetres = append(q.Fenetres, FenetreQuota{Nom: v.Info.Genre, Utilisation: borner(v.Info.Util), Reinit: v.Info.ResetsAt})
	}
	if len(q.Fenetres) == 0 {
		return nil
	}
	rang := func(n string) int {
		switch n {
		case "five_hour":
			return 0
		case "seven_day":
			return 1
		}
		return 2
	}
	sort.Slice(q.Fenetres, func(a, b int) bool {
		ra, rb := rang(q.Fenetres[a].Nom), rang(q.Fenetres[b].Nom)
		if ra != rb {
			return ra < rb
		}
		return q.Fenetres[a].Nom < q.Fenetres[b].Nom
	})
	return q
}

func borner(u *float64) *float64 {
	if u == nil {
		return nil
	}
	x := *u
	if x < 0 {
		x = 0
	}
	if x > 1.5 {
		x = 1.5
	}
	return &x
}

// ----------------------------------------------------------------------------- automatic stops
// The rules of the automatic stops (Overview -> Subscription quota): read by every pilot at each check
// (system/quota.py, from the kit's logs/quota-arret.json). "modele" is every model's own week.

type RegleQuota struct {
	Seuil  int    `json:"seuil"`  // percent of the window used
	Action string `json:"action"` // apres (after the cycle), maintenant (now), pause (until the reset)
}

var fenetresRegles = map[string]bool{"five_hour": true, "seven_day": true, "modele": true}
var actionsRegles = map[string]bool{"apres": true, "maintenant": true, "pause": true}

func fichierReglesQuota(k Kit) string { return filepath.Join(k.Journaux(), "quota-arret.json") }

func lireReglesQuota(k Kit) map[string]RegleQuota {
	out := map[string]RegleQuota{}
	b, err := os.ReadFile(fichierReglesQuota(k))
	if err != nil || len(b) > 64<<10 {
		return out
	}
	var d map[string]RegleQuota
	if json.Unmarshal(b, &d) != nil {
		return out
	}
	for f, r := range d {
		if fenetresRegles[f] && actionsRegles[r.Action] && r.Seuil >= 1 && r.Seuil <= 100 {
			out[f] = r
		}
	}
	return out
}

var reglesQuotaMu sync.Mutex

func (s *Serveur) apiReglesQuota(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Fenetre string `json:"fenetre"`
		Seuil   int    `json:"seuil"` // 0: no automatic stop for this window
		Action  string `json:"action"`
	}
	if err := lireCorps(r, &c); err != nil || !fenetresRegles[c.Fenetre] || c.Seuil < 0 || c.Seuil > 100 || (c.Seuil > 0 && !actionsRegles[c.Action]) {
		s.erreur(w, http.StatusBadRequest, "Invalid setting")
		return
	}
	k := s.kitCourant()
	reglesQuotaMu.Lock()
	defer reglesQuotaMu.Unlock()
	d := lireReglesQuota(k)
	if c.Seuil == 0 {
		delete(d, c.Fenetre)
	} else {
		d[c.Fenetre] = RegleQuota{Seuil: c.Seuil, Action: c.Action}
	}
	b, _ := json.MarshalIndent(d, "", " ")
	_ = os.MkdirAll(k.Journaux(), 0o755)
	f := fichierReglesQuota(k)
	if err := os.WriteFile(f+".tmp", b, 0o644); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(f+".tmp", f); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Automatic stop saved: the running pilots apply it at once.")
}
