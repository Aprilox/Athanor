package main

// A project's own models (Manage › Working mode › Models): each of the six model settings can be set for one
// project, in its .athanor/.env, beside its working modes. Left empty, a project follows Settings › Claude.
// The pilot reads them at the start of each cycle, so a change applies from the next cycle, without a restart.
// The file is in the workshop, which Claude can write: a value is kept only when it looks like a model name.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// clesModelesProjet: the model settings a project can set for itself, in the order the page shows them.
var clesModelesProjet = []string{"MODEL", "FALLBACK_MODEL", "FRAMING_MODEL", "WORKER_MODEL", "SUBAGENT_MODEL", "CHEAP_MODEL"}

// reModeleProjet: an alias (opus, sonnet[1m]), a full name (claude-opus-4-5) or a cloud provider's id
// (us.anthropic.claude-…:0). Never a leading "-": the value becomes an argument of Claude Code.
var reModeleProjet = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/\[\]-]{0,99}$`)

func estCleModeleProjet(cle string) bool {
	for _, c := range clesModelesProjet {
		if c == cle {
			return true
		}
	}
	return false
}

// modeleProjetValide: a value a project may set for that model setting ("auto" only for the sub-agents).
func modeleProjetValide(cle, v string) bool {
	if strings.EqualFold(v, "auto") {
		return cle == "SUBAGENT_MODEL"
	}
	return reModeleProjet.MatchString(v)
}

// modelesProjetTexte: the models a project's .athanor/.env sets, valid ones only (the last line wins).
func modelesProjetTexte(texteEnv string) map[string]string {
	r := map[string]string{}
	for _, l := range strings.Split(texteEnv, "\n") {
		l = strings.TrimSuffix(l, "\r")
		i := strings.Index(l, "=")
		if i <= 0 {
			continue
		}
		cle, v := strings.TrimSpace(l[:i]), strings.TrimSpace(l[i+1:])
		if !estCleModeleProjet(cle) {
			continue
		}
		if v == "" || !modeleProjetValide(cle, v) {
			delete(r, cle)
			continue
		}
		r[cle] = v
	}
	return r
}

// modelesProjet: modelesProjetTexte of that file.
func modelesProjet(env string) map[string]string {
	b, _ := os.ReadFile(env)
	return modelesProjetTexte(string(b))
}

// resumeModeles: the project's models in one stable line ("MODEL=opus WORKER_MODEL=sonnet"), "" for none.
func resumeModeles(m map[string]string) string {
	var l []string
	for _, c := range clesModelesProjet {
		if v, ok := m[c]; ok {
			l = append(l, c+"="+v)
		}
	}
	return strings.Join(l, " ")
}

// modelesGlobauxPilote: the six models as the pilot takes them from Settings when it starts.
func (p *pilote) modelesGlobauxPilote() map[string]string {
	return map[string]string{
		"MODEL": p.model, "FALLBACK_MODEL": p.fallbackModel, "FRAMING_MODEL": p.framingModel,
		"WORKER_MODEL": p.workerModel, "SUBAGENT_MODEL": p.subagentModel, "CHEAP_MODEL": p.cheapModel,
	}
}

// appliquerModelesProjet: the models of this cycle, the project's own over the ones of Settings, read from the
// project's .athanor/.env as it is now. What it read is kept for the state (modelesLus); a change is logged
// when annoncer (not at the pilot's start, before its lock: another pilot of the project may be running).
func (p *pilote) appliquerModelesProjet(annoncer bool) {
	if p.modelesGlobaux == nil {
		p.modelesGlobaux = p.modelesGlobauxPilote()
	}
	b, _ := os.ReadFile(filepath.Join(p.etat, ".env"))
	propres := modelesProjetTexte(string(b))
	choix := func(cle string) string {
		if v, ok := propres[cle]; ok {
			return v
		}
		return p.modelesGlobaux[cle]
	}
	p.model, p.fallbackModel, p.framingModel = choix("MODEL"), choix("FALLBACK_MODEL"), choix("FRAMING_MODEL")
	p.workerModel, p.subagentModel, p.cheapModel = choix("WORKER_MODEL"), choix("SUBAGENT_MODEL"), choix("CHEAP_MODEL")
	resume := resumeModeles(propres)
	p.modelesLus = resume
	if annoncer && resume != p.modelesAnnonce {
		if resume != "" {
			p.log("This project's own models (Manage › Working mode): " + resume + ". The others as in Settings.")
		} else if p.modelesAnnonce != "" {
			p.log("This project's own models removed: the models of Settings from this cycle.")
		}
		p.modelesAnnonce = resume
	}
}
