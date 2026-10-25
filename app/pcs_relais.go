package main

// Several computers (pcs.go), when a program of one computer does not work (Blender, Unreal…): Claude
// tries one quick repair, then, rather than asking the user, says so in .athanor/CANNOT_HERE.md and ends
// its cycle. The pilot notes it (this computer cannot run that program for that milestone), lets the
// milestone go and says it on GitHub: the other computers try in its place, those that have the program's
// word first. When none of them can either (or none is running), the computer that was meant to build it
// first takes it back and Claude asks the user, saying which computers tried. A computer that builds such a
// milestone learns the program's word: such milestones go to it first from then on.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RefusPC: a program this computer could not run, for a milestone.
type RefusPC struct {
	Outil    string `json:"outil"`              // one word, as in "Needs:" (blender)
	Jalon    string `json:"jalon"`              // the milestone it was building
	Pourquoi string `json:"pourquoi,omitempty"` // one line, Claude's
	Quand    string `json:"quand"`              // RFC 3339, UTC
}

const (
	fichierRefusClaude = "CANNOT_HERE.md" // written by Claude in .athanor/ (never shared: not in atelierPartage)
	dureeRefusPC       = 12 * time.Hour   // then the computer tries that program again (it may have been repaired)
	maxRefusPC         = 20
)

func (r RefusPC) valide(maintenant time.Time) bool {
	t, err := time.Parse(time.RFC3339, r.Quand)
	return err == nil && maintenant.Sub(t) < dureeRefusPC && r.Outil != "" && r.Jalon != ""
}

// lireRefusPC: this computer's refusals on the project, the expired ones left out.
func lireRefusPC(d string) []RefusPC {
	var v, garde []RefusPC
	b, err := os.ReadFile(d + "/pcs-refus.json")
	if err != nil || json.Unmarshal(b, &v) != nil {
		return nil
	}
	maintenant := time.Now()
	for _, r := range v {
		if r.valide(maintenant) {
			garde = append(garde, r)
		}
	}
	return garde
}

func ecrireRefusPC(d string, v []RefusPC) error {
	if len(v) > maxRefusPC {
		v = v[len(v)-maxRefusPC:]
	}
	if len(v) == 0 {
		removeSiExiste(d + "/pcs-refus.json")
		return nil
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return ecrireFichierPrive(d+"/pcs-refus.json", string(b)+"\n")
}

// lireRefusClaude: what Claude wrote in .athanor/CANNOT_HERE.md ("tool: blender", "why: …"); the file is
// then removed (once read, it is the pilot's).
func lireRefusClaude(etat string) (outil, pourquoi string, ok bool) {
	f := filepath.Join(etat, fichierRefusClaude)
	if !fichierRegulierPilote(f) {
		return "", "", false
	}
	b, err := os.ReadFile(f)
	_ = os.Remove(f)
	if err != nil || len(b) > 64<<10 {
		return "", "", false
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		l := strings.TrimSpace(strings.TrimLeft(sc.Text(), "-* "))
		k, v, trouve := strings.Cut(l, ":")
		if !trouve {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "tool", "outil", "program":
			// one word ("Blender 4.2" → blender): the one Needs: lines use
			if f := strings.Fields(strings.Trim(strings.TrimSpace(v), "`*\"'")); len(f) > 0 {
				if m := motsPC(strings.Trim(f[0], ".,;:()`")); len(m) > 0 {
					outil = m[0]
				}
			}
		case "why", "pourquoi", "reason":
			pourquoi = coupeValide(strings.TrimSpace(v), 200)
		}
	}
	return outil, pourquoi, outil != ""
}

// refusDe: whether r (the refusals of one computer) covers milestone j: that milestone, or its program.
func refusDe(r []RefusPC, j Jalon, maintenant time.Time) (RefusPC, bool) {
	for _, x := range r {
		if !x.valide(maintenant) {
			continue
		}
		if x.Jalon == j.ID {
			return x, true
		}
		for _, b := range j.Besoins {
			if b == x.Outil {
				return x, true
			}
		}
	}
	return RefusPC{}, false
}

// essaiPC: a computer that tried a milestone and could not run its program.
type essaiPC struct {
	ID, Nom string
	RefusPC
}

// essaisJalonPC: who tried milestone j and could not (this computer included), the first one first: the
// one that was meant to build it.
func essaisJalonPC(j Jalon, moi MachinePC, mesRefus []RefusPC, autres []PresencePC, maintenant time.Time) []essaiPC {
	var v []essaiPC
	if r, ok := refusDe(mesRefus, j, maintenant); ok {
		v = append(v, essaiPC{moi.ID, moi.Nom, r})
	}
	for _, p := range autres {
		if r, ok := refusDe(p.Refus, j, maintenant); ok {
			v = append(v, essaiPC{p.ID, p.Nom, r})
		}
	}
	sort.SliceStable(v, func(a, b int) bool { return v[a].Quand < v[b].Quand })
	return v
}

// texteRepriseJalon: what Claude is told when no computer of the team could run the milestone's program.
func texteRepriseJalon(j Jalon, essais []essaiPC) string {
	if len(essais) == 0 {
		return ""
	}
	var l []string
	for i, e := range essais {
		s := e.Nom
		if i == 0 {
			s += " (the computer meant to build it)"
		}
		if e.Pourquoi != "" {
			s += ": " + e.Pourquoi
		}
		l = append(l, s)
	}
	return fmt.Sprintf(" No computer of the team could run %s for this milestone; they tried: %s. Do not repeat their attempts: one check that the cause is still there (or a repair none of them tried), then ask the user with `athanor todo add` what to repair, naming the computers that tried and the one meant to build it, and go on with what does not need %s.",
		essais[0].Outil, strings.Join(l, "; "), essais[0].Outil)
}

// noterRefusPC: Claude said a program does not work here: the refusal kept and published, the milestone
// let go (the others try in its place). The message for the log.
func (e *equipePC) noterRefusPC(jalon, outil, pourquoi string) string {
	libere := e.verrou()
	v := append(lireRefusPC(e.d), RefusPC{Outil: outil, Jalon: jalon, Pourquoi: pourquoi, Quand: time.Now().UTC().Format(time.RFC3339)})
	_ = ecrireRefusPC(e.d, v)
	removeSiExiste(e.d + "/pcs-jalon")
	_ = e.publier()
	libere()
	msg := fmt.Sprintf("Several computers: %s does not work on this computer", outil)
	if pourquoi != "" {
		msg += " (" + pourquoi + ")"
	}
	return msg + ": " + jalon + " is left to the other computers, which try in its place."
}

// apprendreOutilPC: this computer built a milestone that needed a program (or that another computer could
// not build for one): the program's word becomes one of its own, and its refusal of it goes. The message.
func (e *equipePC) apprendreOutilPC(j Jalon, maintenant time.Time) string {
	outils := map[string]bool{}
	for _, b := range j.Besoins {
		outils[b] = true
	}
	for _, p := range e.pcs {
		if r, ok := refusDe(p.Refus, j, maintenant); ok && r.Jalon == j.ID {
			outils[r.Outil] = true
		}
	}
	mesRefus := lireRefusPC(e.d)
	var garde []RefusPC
	for _, r := range mesRefus {
		if !outils[r.Outil] {
			garde = append(garde, r)
		}
	}
	if len(garde) != len(mesRefus) {
		_ = ecrireRefusPC(e.d, garde)
	}
	m := lireMachinePC()
	connus := map[string]bool{}
	for _, x := range m.Mots {
		connus[x] = true
	}
	var nouveaux []string
	for o := range outils {
		if !connus[o] && len(m.Mots) < 12 {
			m.Mots = append(m.Mots, o)
			nouveaux = append(nouveaux, o)
		}
	}
	if len(nouveaux) == 0 {
		return ""
	}
	sort.Strings(nouveaux)
	if ecrireMachinePC(m) != nil {
		return ""
	}
	e.moi = m
	return fmt.Sprintf("Several computers: %s works on this computer: its milestones come to this computer first from now on (Remote › This computer).", strings.Join(nouveaux, ", "))
}
