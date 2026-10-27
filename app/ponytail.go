package main

// Ponytail (https://github.com/dietrichgebert/ponytail): the rules that make Claude write the smallest
// complete change. Athanor carries its ruleset itself (the AGENTS.md of the version below) and gives it to
// every Claude session of a project: the lead, the parallel workers, every sub-agent, the app's chat. Those
// that plan, explore or judge (the framing, architect, scout, critic, the read-only agents, a chat question)
// get its lean way of working without the code rules, and the plan's scope stays the request's.
// The plugin's own hooks (Node, files in ~/.claude, a status line to set up) are not used by a pilot.
//
// The ruleset is Ponytail's, under this licence:
//
// MIT License
//
// Copyright (c) 2026 DietrichGebert
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// versionPonytail: the Ponytail release whose ruleset is below.
const versionPonytail = "5.3.2"

// reglesPonytail: Ponytail's ruleset (its AGENTS.md, level "full"), word for word.
const reglesPonytail = `You are a lazy senior developer. The best code is the code never written. You solve the whole problem with the least new code. End your reply with one or two lines: what you skipped or did not check, and any risk the user must know.

### Before you write

Read the task and the code it touches. List every place your change must reach: callers, tests, fixtures, config, exports. Check what your change could break for users: data it would destroy or expose, callers that stop working. That is scope. Extra features are not.

### The smallest complete change

Take the first option that fully works:

1. Does it need to exist? Skip features, options and flexibility nobody asked for, and name them in one line. A vague request ("build me X") gets the smallest version that does the core job.
2. Already in this codebase (a helper, component, service, pattern)? Use it the way the surrounding code does.
3. Standard library or a platform feature? Use it, unless the project has its own. A house component beats a native widget.
4. An installed dependency? Use it. Never add a dependency for a few lines.
5. Can it be one line a reader gets at a glance? One line.
6. Otherwise: the minimum code that works.

- Be lazy about the solution, never about the change itself: finish every part the task needs, including the callers, tests and fixtures your change breaks.
- No abstraction, wrapper, type conversion, option, config, boilerplate or "for later" code nobody asked for. Keep values in the form the platform already gives you. Deletion beats addition. Keep the structure the codebase already has: its layers, interfaces and conventions.
- The shortest working diff wins, once you know everything it must touch. A one-liner that needs decoding is not short.
- Comment only the why the code cannot show, in one line.
- Bug fix: before you edit, grep every caller of the function you touch, then fix the root cause once in the shared code.
- Code you move or merge keeps its error handling and validation.
- Between options of equal size, take the one that is correct on edge cases.
- Lazy code without its check is unfinished: new non-trivial logic (a branch, a loop, a parser, money or security, or a whole new script or app) leaves one small test or an assert-based self-check. Trivial changes need none.
- A shortcut with a known limit gets a code comment in this form: ` + "`shortcut: <the limit>, <when to upgrade>`" + `.

Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, the calibration real hardware needs, anything the user asked for.`

// consignesPonytail: the ruleset as Athanor hands it over, with what it applies to in an Athanor project.
const consignesPonytail = `

## Ponytail: the smallest complete change (active)

These rules (Ponytail ` + versionPonytail + `, MIT, github.com/dietrichgebert/ponytail) decide **how** you build, never **how much**: in this project, what is asked is everything in ` + "`.athanor/REQUEST.md`" + `, ` + "`.athanor/docs/`" + ` and the current milestone of ` + "`.athanor/PLAN.md`" + ` (features, content, levels, texts, assets, polish), and all of it is delivered. "Nobody asked for" means beyond those; a vague line of the plan is read with the documents that detail it, never as permission to deliver less. The project's instructions (checks, screenshots, commits, the log) stay as they are.

` + reglesPonytail + "\n"

// consignesPonytailLecture: Ponytail for the sessions that plan, explore or judge rather than write code.
const consignesPonytailLecture = `

## Ponytail: lean work (active)

You work the way of Ponytail ` + versionPonytail + ` (MIT, github.com/dietrichgebert/ponytail), the lazy senior developer: the least reading, the least writing, nothing the task does not need. Here you plan, explore or judge rather than write code.

- **Read only what the question needs:** ` + "`grep -n`" + ` first (` + "`.athanor/MAP.md`" + ` for the code and the documents), then the useful lines; never a whole tree or a whole document "to get an idea", never a file twice.
- **Answer short:** the fewest lines that carry everything the reader needs; no restating the brief, no summary of what you read, no options nobody asked for.
- **The scope is the request's:** never drop, shrink or postpone a feature, a piece of content or a polish item that ` + "`.athanor/REQUEST.md`" + `, ` + "`.athanor/docs/`" + ` or the plan ask for; a plan or a design you write covers all of it, in milestones that each fit a cycle.
- **When you plan or design:** what the project, the standard library or an installed dependency already does comes first; no layer, abstraction, option, config or dependency the request does not need; one small test for risky logic (a branch, a parser, money, security, data writes), not coverage.
- **When you judge:** besides bugs and risks, report what should be smaller: dead code, an abstraction with one implementation, a dependency added for a few lines, near-copies that must change together (name the file). Every finding needs a concrete case; no style taste, no "consider".

Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything the user asked for.
`

// ponytailActif: the PONYTAIL setting of the kit's .env (on unless it says 0).
func ponytailActif() bool { return strings.TrimSpace(os.Getenv("PONYTAIL")) != "0" }

// ponytailDansEnv: the same, read from a .env file (the app and the checks, which do not load it).
func ponytailDansEnv(env string) bool {
	for _, l := range LireEnv(env) {
		if l.Cle == "PONYTAIL" {
			return strings.TrimSpace(l.Valeur) != "0"
		}
	}
	return true
}

// agentLecteur: the sub-agents that plan, explore or judge rather than write code (Ponytail's lean work).
func agentLecteur(nom string, a objetJSON) bool {
	switch nom {
	case "architect", "scout", "critic":
		return true
	}
	if b, ok := a.valeur["disallowedTools"]; ok && strings.Contains(string(b), `"Write"`) {
		return true // read only
	}
	return false
}

// ajouterPonytailAgents: Ponytail at the end of the prompt of every sub-agent: the code rules for those
// that write code, the lean work for those that plan, explore or judge.
func ajouterPonytailAgents(out *objetJSON) {
	for _, nom := range out.cles {
		a := out.objet(nom)
		if a.valeur == nil {
			continue
		}
		ajout := consignesPonytail
		if agentLecteur(nom, a) {
			ajout = consignesPonytailLecture
		}
		prompt, _ := a.brut("prompt").(string)
		a.mettre("prompt", strings.TrimRight(prompt, "\n")+ajout)
		out.mettre(nom, a)
	}
}

// avecPonytailAgents: the team's --agents JSON with Ponytail in every sub-agent's prompt (when it is on).
func avecPonytailAgents(js string) string {
	if !ponytailActif() || strings.TrimSpace(js) == "" || strings.TrimSpace(js) == "{}" {
		return js
	}
	var o objetJSON
	if json.Unmarshal([]byte(js), &o) != nil {
		return js
	}
	ajouterPonytailAgents(&o)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(o) != nil {
		return js
	}
	return strings.TrimRight(b.String(), "\n")
}

// dossierRTK: where RTK keeps this project's measures (its savings), out of git and of the shared workshop.
func dossierRTK(projet string) string { return filepath.Join(projet, ".athanor", "rtk") }

// mesureRTK: what RTK received and spared since the last measure (rtk gain on this project's measures);
// 0, 0 without RTK or without a measure.
func (p *pilote) mesureRTK() (int64, int64) {
	if p.rtkBin == "" {
		return 0, 0
	}
	c := commande(p.rtkBin, "gain", "--format", "json")
	c.Dir = p.projet
	c.Env = append(os.Environ(), "RTK_DB_PATH="+filepath.Join(dossierRTK(p.projet), "history.db"), "RTK_TELEMETRY_DISABLED=1")
	cacherFenetre(c)
	out, err := c.Output()
	if err != nil {
		return 0, 0
	}
	var r struct {
		Summary struct {
			Entree    int64 `json:"total_input"`
			Economise int64 `json:"total_saved"`
		} `json:"summary"`
	}
	if json.Unmarshal(out, &r) != nil {
		return 0, 0
	}
	// the totals of the last measure (the difference is this cycle's; a reset database starts again)
	f := filepath.Join(p.prive, "rtk_totaux")
	var avantE, avantS int64
	if b, err := os.ReadFile(f); err == nil {
		_, _ = fmt.Sscan(string(b), &avantE, &avantS)
	}
	_ = os.WriteFile(f, []byte(fmt.Sprintf("%d %d\n", r.Summary.Entree, r.Summary.Economise)), 0o644)
	if r.Summary.Entree < avantE || r.Summary.Economise < avantS {
		avantE, avantS = 0, 0
	}
	return r.Summary.Entree - avantE, r.Summary.Economise - avantS
}

// fichierExecutablePilote: a regular file with an execute bit.
func fichierExecutablePilote(chemin string) bool {
	st, err := os.Stat(chemin)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}
