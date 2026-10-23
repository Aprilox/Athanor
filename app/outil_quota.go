package main

// athanor quota verifier RULES LOG…: the automatic stops on the subscription's quota (Overview ->
// Subscription quota -> Auto stop; replaces system/quota.py, same output). Prints "action<TAB>reset<TAB>
// message" for the first rule that the latest rate_limit_event of these logs (the newest log first) meets
// while its window is still open; nothing otherwise. RULES is the kit's logs/quota-arret.json (the app
// writes it: lireReglesQuota); "modele" stands for every model's own week (seven_day_fable…).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type regleSeuil struct {
	seuil  float64
	action string
}

func reglesSeuils(chemin string) map[string]regleSeuil {
	out := map[string]regleSeuil{}
	f := cheminOutil(chemin)
	st, err := os.Stat(f)
	if err != nil || st.Size() > 64<<10 {
		return out
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return out
	}
	var d map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&d) != nil {
		return out
	}
	for _, cle := range []string{"five_hour", "seven_day", "modele"} {
		r := dict(d[cle])
		if r == nil {
			continue
		}
		seuil, ok := nombreJSON(r["seuil"])
		action, _ := texte(r["action"])
		if ok && seuil >= 1 && seuil <= 100 && (action == "apres" || action == "maintenant" || action == "pause") {
			out[cle] = regleSeuil{seuil, action}
		}
	}
	return out
}

// dernierEvenementQuota: the rate_limit_info of the latest rate_limit_event, in the newest log that has
// one (its last 2 MB); nil when none.
func dernierEvenementQuota(journaux []string) (map[string]any, bool) {
	type j struct {
		chemin string
		date   time.Time
	}
	var js []j
	for _, c := range journaux {
		var d time.Time
		if st, err := os.Stat(cheminOutil(c)); err == nil {
			d = st.ModTime()
		}
		js = append(js, j{c, d})
	}
	sort.SliceStable(js, func(a, b int) bool { return js[a].date.After(js[b].date) })
	for _, x := range js {
		f, err := os.Open(cheminOutil(x.chemin))
		if err != nil {
			continue
		}
		if st, err := f.Stat(); err == nil && st.Size() > 2<<20 {
			_, _ = f.Seek(st.Size()-(2<<20), io.SeekStart)
		}
		var lignes []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 64<<20)
		for sc.Scan() {
			lignes = append(lignes, sc.Text())
		}
		f.Close()
		for i := len(lignes) - 1; i >= 0; i-- {
			if !strings.Contains(lignes[i], `"rate_limit_event"`) {
				continue
			}
			var o map[string]any
			dec := json.NewDecoder(strings.NewReader(lignes[i]))
			dec.UseNumber()
			if dec.Decode(&o) != nil || o == nil || o["type"] != "rate_limit_event" {
				continue
			}
			info := dict(o["rate_limit_info"])
			if info == nil {
				info = map[string]any{}
			}
			return info, true
		}
	}
	return nil, false
}

// titrePython: str.title() (each word's first letter upper case, the others lower case).
func titrePython(s string) string {
	var b strings.Builder
	debut := true
	for _, r := range s {
		if unicode.IsLetter(r) {
			if debut {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(unicode.ToLower(r))
			}
			debut = false
		} else {
			b.WriteRune(r)
			debut = true
		}
	}
	return b.String()
}

func quotaVerifier(fichierRegles string, journaux []string) string {
	r := reglesSeuils(fichierRegles)
	if len(r) == 0 {
		return ""
	}
	info, ok := dernierEvenementQuota(journaux)
	if !ok || len(info) == 0 {
		return ""
	}
	fenetres := dict(info["unifiedWindows"])
	noms := make([]string, 0, len(fenetres))
	for n := range fenetres {
		noms = append(noms, n)
	}
	rang := func(n string) string { // five_hour, seven_day, then the others by name (quota.py's order)
		switch n {
		case "five_hour":
			return "0"
		case "seven_day":
			return "1"
		}
		return "2" + n
	}
	sort.Slice(noms, func(a, b int) bool { return rang(noms[a]) < rang(noms[b]) })
	maintenant := float64(time.Now().UnixNano()) / 1e9
	for _, nom := range noms {
		f := dict(fenetres[nom])
		if f == nil {
			continue
		}
		regle, ok := r[nom]
		if !ok && strings.HasPrefix(nom, "seven_day_") {
			regle, ok = r["modele"]
		}
		u, okU := nombreJSON(f["utilization"])
		reinit, okR := nombreJSON(f["resetsAt"])
		if !ok || !okU || !okR || reinit <= maintenant {
			continue
		}
		if u*100 >= regle.seuil {
			libelle := map[string]string{"five_hour": "5-hour session", "seven_day": "week, all models"}[nom]
			if libelle == "" {
				libelle = "week of " + titrePython(strings.ReplaceAll(strings.TrimPrefix(nom, "seven_day_"), "_", " "))
			}
			return fmt.Sprintf("%s\t%d\t%s at %d%% (threshold %d%%)\n", regle.action, int64(reinit), libelle, int64(u*100), int64(regle.seuil))
		}
	}
	return ""
}

func lancerQuota(args []string) int {
	if len(args) >= 2 && args[0] == "verifier" {
		fmt.Print(quotaVerifier(args[1], args[2:]))
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: athanor quota verifier RULES LOG...")
	return 2
}
