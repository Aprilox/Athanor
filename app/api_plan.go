package main

// Editing the plan from the app: move a milestone up or down, remove it, or edit PLAN.md as a whole.
// The plan is Claude's working document: the app edits it carefully (block by block, atomically) and
// the pilot and Claude re-read it at the start of every cycle.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var reDependsOn = regexp.MustCompile(`(Depends on:\s*)([^.;\n]*)`)

// blocPlan: a top-level milestone line with the indented lines that follow it, or any other line
// (heading, blank line, introduction) on its own.
type blocPlan struct {
	lignes []string
	jalon  bool
	id     string
	fait   bool
}

func decouperPlan(texte string) []blocPlan {
	var blocs []blocPlan
	for _, l := range strings.Split(texte, "\n") {
		estJalon := (strings.HasPrefix(l, "- [ ] ") || strings.HasPrefix(l, "- [x] ") || strings.HasPrefix(l, "- [X] "))
		indentee := len(l) > 0 && (l[0] == ' ' || l[0] == '\t')
		switch {
		case estJalon:
			blocs = append(blocs, blocPlan{lignes: []string{l}, jalon: true, id: identJalon(strings.TrimSpace(l[6:])), fait: l[3] != ' '})
		case indentee && len(blocs) > 0 && blocs[len(blocs)-1].jalon:
			blocs[len(blocs)-1].lignes = append(blocs[len(blocs)-1].lignes, l)
		default:
			blocs = append(blocs, blocPlan{lignes: []string{l}})
		}
	}
	return blocs
}

func assemblerPlan(blocs []blocPlan) string {
	var out []string
	for _, b := range blocs {
		out = append(out, b.lignes...)
	}
	return strings.Join(out, "\n")
}

// ecrirePlan replaces PLAN.md atomically, never through a symbolic link.
func ecrirePlan(p Projet, texte string) error {
	if fi, err := os.Lstat(p.Plan()); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("PLAN.md is a symbolic link")
	}
	t := filepath.Join(p.Atelier(), ".plan.tmp")
	if err := os.WriteFile(t, []byte(texte), 0o644); err != nil {
		return err
	}
	return os.Rename(t, p.Plan())
}

func (p Projet) noterLog(message string) {
	f, err := os.OpenFile(filepath.Join(p.Atelier(), "LOG.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "- %s — %s\n", time.Now().Format("2006-01-02 15:04"), message)
}

// enCours: the milestone is being built right now (lead or a parallel worker).
func (p Projet) enCours(id string) bool {
	e, ok := p.LireEtat()
	if !ok || !p.PiloteActif() {
		return false
	}
	for _, o := range strings.Fields(e.Ouvriers) {
		if o == id {
			return true
		}
	}
	return e.Phase == "travail" && e.Ouvriers == "" && identJalon(e.JalonEnCours) == id
}

func (s *Serveur) apiPlanTexte(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	b, _ := os.ReadFile(p.Plan())
	s.ecrireJSON(w, map[string]string{"texte": string(b)})
}

func (s *Serveur) apiEcrirePlan(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Texte string `json:"texte"`
	}
	if err := lireCorps(r, &c); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	if err := ecrirePlan(p, c.Texte); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	p.noterLog("PLAN.md edited by the user from the app.")
	s.msg(w, "PLAN.md saved. Claude follows it from the next cycle.")
}

// apiActionJalon: {"id": "M13", "action": "monter" | "descendre" | "retirer"}
func (s *Serveur) apiActionJalon(w http.ResponseWriter, r *http.Request) {
	var c struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := lireCorps(r, &c); err != nil || c.ID == "" {
		s.erreur(w, http.StatusBadRequest, "Missing milestone")
		return
	}
	p, ok := s.projetRequis(w, r)
	if !ok {
		return
	}
	b, err := os.ReadFile(p.Plan())
	if err != nil {
		s.erreur(w, http.StatusNotFound, "No plan yet")
		return
	}
	blocs := decouperPlan(string(b))
	i := -1
	for k, bl := range blocs {
		if bl.jalon && bl.id == c.ID {
			i = k
			break
		}
	}
	if i < 0 {
		s.erreur(w, http.StatusNotFound, "Unknown milestone")
		return
	}
	if blocs[i].fait {
		s.erreur(w, http.StatusConflict, "This milestone is already done")
		return
	}
	if p.enCours(c.ID) {
		s.erreur(w, http.StatusConflict, "Claude is building this milestone right now: stop after the cycle first")
		return
	}
	switch c.Action {
	case "monter", "descendre":
		pas := -1
		if c.Action == "descendre" {
			pas = 1
		}
		j := i + pas
		for j >= 0 && j < len(blocs) && !blocs[j].jalon {
			j += pas
		}
		if j < 0 || j >= len(blocs) {
			s.msg(w, "Already at the end.")
			return
		}
		if blocs[j].fait {
			s.erreur(w, http.StatusConflict, "Done milestones stay where they are")
			return
		}
		blocs[i], blocs[j] = blocs[j], blocs[i]
		p.noterLog(fmt.Sprintf("Plan: %s moved by the user.", c.ID))
	case "retirer":
		retire := blocs[i]
		blocs = append(blocs[:i], blocs[i+1:]...)
		// the removed milestone disappears from the other milestones' dependencies
		for k := range blocs {
			if !blocs[k].jalon {
				continue
			}
			blocs[k].lignes[0] = reDependsOn.ReplaceAllStringFunc(blocs[k].lignes[0], func(m string) string {
				sm := reDependsOn.FindStringSubmatch(m)
				var gardes []string
				for _, d := range strings.FieldsFunc(sm[2], func(r rune) bool { return r == ',' || r == ' ' }) {
					if identJalon(d) != c.ID {
						gardes = append(gardes, d)
					}
				}
				if len(gardes) == 0 {
					gardes = []string{"none"}
				}
				return sm[1] + strings.Join(gardes, ", ")
			})
		}
		p.noterLog(fmt.Sprintf("Plan: %s removed by the user (%s).", c.ID, strings.TrimSpace(strings.TrimPrefix(retire.lignes[0], "- [ ] "))))
	default:
		s.erreur(w, http.StatusBadRequest, "Unknown action")
		return
	}
	if err := ecrirePlan(p, assemblerPlan(blocs)); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.msg(w, "Plan updated. Claude follows it from the next cycle.")
}
