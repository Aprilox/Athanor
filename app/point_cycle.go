package main

// "Where things stand": what the pilot already knows, given at the start of each cycle so that a fresh session
// does not spend its first minutes finding it out (measured on 120 cycles of three projects: 2 to 6.6 min before
// the first edit, and one to three checks run only to see where things stood). And what cost the most time in
// the last cycle, so that the slow commands are made faster without testing less.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// commandeCycle: one Bash command of the lead during a cycle, and how long it took.
type commandeCycle struct {
	cmd   string
	duree time.Duration
}

// commandesDuCycle: the lead's Bash commands of a cycle's log, with their durations (from its timestamps).
func commandesDuCycle(journal string) []commandeCycle {
	type appel struct {
		t   time.Time
		cmd string
	}
	appels := map[string]appel{}
	var out []commandeCycle
	for _, o := range lignesJSON(journal, 0) {
		quand, _ := o["timestamp"].(string)
		t, err := time.Parse(time.RFC3339Nano, quand)
		if err != nil || o["parent_tool_use_id"] != nil {
			continue
		}
		for _, b := range contenuMessage(o) {
			m := dict(b)
			switch {
			case o["type"] == "assistant" && m["type"] == "tool_use" && m["name"] == "Bash":
				id, _ := m["id"].(string)
				cmd, _ := dict(m["input"])["command"].(string)
				appels[id] = appel{t, cmd}
			case o["type"] == "user" && m["type"] == "tool_result":
				id, _ := m["tool_use_id"].(string)
				if a, ok := appels[id]; ok {
					out = append(out, commandeCycle{a.cmd, t.Sub(a.t)})
					delete(appels, id)
				}
			}
		}
	}
	return out
}

type commandeLente struct {
	cmd   string
	n     int
	total time.Duration
}

var (
	reEspaces   = regexp.MustCompile(`\s+`)
	reCdDevant  = regexp.MustCompile(`^cd\s+("[^"]*"|'[^']*'|\S+)\s*(&&|;)\s*`)
	reSortieCmd = regexp.MustCompile(`\s*(?:[0-9]?>{1,2}|&>)\s*\S+`)
)

// cleCommande: the command reduced to what it runs (no cd before it, no redirection, one line, 100 characters).
func cleCommande(cmd string) string {
	c := reEspaces.ReplaceAllString(strings.TrimSpace(cmd), " ")
	c = reCdDevant.ReplaceAllString(c, "")
	c = strings.TrimSpace(reSortieCmd.ReplaceAllString(c, ""))
	return couperCaracteres(c, 100)
}

// commandesLentes: the commands that took 2 min or more in all over the cycle (the check: 1 min), slowest first,
// four at most.
func commandesLentes(cmds []commandeCycle) []commandeLente {
	par := map[string]*commandeLente{}
	for _, c := range cmds {
		k := cleCommande(c.cmd)
		if par[k] == nil {
			par[k] = &commandeLente{cmd: k}
		}
		par[k].n++
		par[k].total += c.duree
	}
	var out []commandeLente
	for _, l := range par {
		if l.total >= 2*time.Minute || strings.Contains(l.cmd, ".athanor/check.sh") && l.total >= time.Minute {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].total > out[j].total })
	return out[:min(len(out), 4)]
}

func dureeCourte(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Seconds()))
	}
	return fmt.Sprintf("%d min %02d s", int(d.Minutes()), int(d.Seconds())%60)
}

// pointDeDepart: the section "Where things stand" of a cycle's instructions.
func pointDeDepart(projet, etat string, lentes []commandeLente) string {
	var b strings.Builder
	b.WriteString("\n\n## Where things stand (written by the pilot just before this cycle)\n\n")
	b.WriteString("What the pilot knows already: trust it, and never run a command or a check only to find out what it says.\n\n")
	if pl, err := lireBorne(filepath.Join(etat, "PLAN.md"), 4<<20); err == nil {
		if m := reJalonOuvertPilote.FindSubmatch(pl); m != nil {
			b.WriteString("- **Next unticked milestone of the plan:** " + couperCaracteres(strings.TrimSuffix(string(m[1]), "\r"), 600) + "\n")
		}
	}
	if out, err := gitSortie(projet, "log", "-5", "--pretty=%h %s"); err == nil && strings.TrimSpace(out) != "" {
		b.WriteString("- **Last commits:** " + strings.Join(strings.Split(strings.TrimSpace(out), "\n"), " · ") + "\n")
	}
	if out, err := gitSortie(projet, "status", "--short", "--", ".", ":(exclude).athanor"); err == nil {
		if l := strings.Split(strings.TrimSpace(out), "\n"); out == "" {
			b.WriteString("- **Not committed:** nothing, the folder is as its last commit.\n")
		} else {
			if len(l) > 15 {
				l = append(l[:15], fmt.Sprintf("… and %d more", len(l)-15))
			}
			b.WriteString("- **Not committed:** " + strings.Join(l, " · ") + "\n")
		}
	}
	var r passageCheck
	if j, err := lireBorne(filepath.Join(etat, ".verif", "athanor-full-check.json"), 64<<10); err == nil && json.Unmarshal(j, &r) == nil && r.Journal != "" {
		heure := time.Unix(r.Debut, 0).Format("15:04")
		passe := checkPasse(r)
		switch {
		case passe && r.Empreinte == empreinteProjet(projet):
			fmt.Fprintf(&b, "- **Full check:** it passed on exactly this state of the project (started at %s, output in `%s`). Its result still holds: do not run it again before you change something.\n", heure, cheminRelatif(projet, r.Journal))
		case passe:
			fmt.Fprintf(&b, "- **Last full check:** passed (started at %s); the project changed since.\n", heure)
		default:
			if o, err := os.Lstat(r.Journal); err == nil && o.Mode().IsRegular() && o.ModTime().Unix() >= r.Debut {
				fmt.Fprintf(&b, "- **Last full check:** it did not pass (started at %s, output in `%s`): start from its output, not from a new run.\n", heure, cheminRelatif(projet, r.Journal))
			}
		}
	}
	var total time.Duration
	for _, l := range lentes {
		total += l.total
	}
	if total >= 5*time.Minute {
		b.WriteString("\n### What cost the most time in the last cycle\n\n")
		for _, l := range lentes {
			fmt.Fprintf(&b, "- %s (%d×): `%s`\n", dureeCourte(l.total), l.n, strings.ReplaceAll(l.cmd, "`", "'"))
		}
		b.WriteString("\nLook once whether one of them can be faster **without testing less**: skip a step whose inputs did not change, keep builds incremental, run independent steps in parallel, poll a condition instead of waiting a fixed time. If so, do it first (10 minutes at most) and note it in `.athanor/SOLUTIONS.md`; if it cannot be faster, note why there in one line, and go on with the milestone. When that line is already there, go on at once.\n")
	}
	return b.String()
}

func cheminRelatif(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}
