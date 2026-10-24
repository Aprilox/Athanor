package main

// athanor pilote: the parallel mode (parallel.sh's parallele_cycle and what it calls): a scheduler keeps
// up to PARALLEL_MAX workers busy, each in its own git worktree and branch auto/<milestone>, merges each
// one as soon as it finishes and starts the next milestones whose dependencies are met; then one
// verification session by the lead. Same files, same messages, same Claude sessions as the script.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const paralleleDepart = 2

// exeAthanor: this executable's sub-command (what parallel.sh calls through $ATHANOR_GO).
func (p *pilote) exeAthanor(env []string, args ...string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "athanor"
	}
	c := commande(exe, args...)
	c.Env = append(os.Environ(), env...)
	cacherFenetre(c)
	return c
}

func (p *pilote) journalPilote() *os.File {
	f, _ := os.OpenFile(filepath.Join(p.prive, "pilot.log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	return f
}

// pretsParallele: the milestones ready to start: unchecked, every dependency ticked, not in exclus.
func (p *pilote) pretsParallele(exclus map[string]bool) [][4]string {
	var r [][4]string
	for _, j := range p.jalonsPretsParallele() {
		if !exclus[j[0]] {
			r = append(r, j)
		}
	}
	return r
}

func (p *pilote) consignesBaseParallele() string {
	if os.Getenv("ATHANOR_SANS_COAUTEUR") == "1" && fichierRegulierPilote(filepath.Join(p.prive, "CLAUDE.auteur.md")) {
		return filepath.Join(p.prive, "CLAUDE.auteur.md")
	}
	return p.consignes
}

func consignesOuvrierParallele(ident, ligne string, executor, reprise bool) string {
	aides := "Do not launch any subagent."
	if executor {
		aides = "The only sub-agent you may call is `executor` (cheap model), for mechanical work that would flood your context: running the test suite or a long build and returning only the failures, searching the code, the same small edit on many of your files. Give it a closed brief: what, where (only your milestone's files, in this folder), how to verify. A short command, run it yourself."
	}
	repriseTexte := ""
	if reprise {
		repriseTexte = "- **This branch already holds partial work** from an earlier attempt that was stopped (quota, watchdog): read its commits (`git log --oneline -5`, `git show --stat HEAD`) and carry on from there, never start over.\n"
	}
	// as the heredoc of parallel.sh writes it (the empty substitution leaves an empty line)
	return "\n## You are a parallel worker\n\nYou carry out **only** this milestone, in this folder (git worktree of the project, branch `auto/" + ident + "`):\n\n> " + ligne + "\n\n" + strings.TrimSuffix(repriseTexte, "\n") + "\n" +
		"- Other workers are doing other milestones at the same time, each in their own worktree; the pilot merges your branch as soon as you finish. Touch only the files of your milestone; if you must modify a shared file (common stylesheet, header...), make the minimal change and flag it in your report.\n" +
		"- **The team board** `.athanor/BOARD.md` (written by the pilot every few seconds) shows what every other worker is doing and the files each one holds: its sheet's Files and the files it reserved. Read it before you start, and before you modify any file that is not in your sheet's Files. To take such a file, first add its path to `.athanor/RESERVE` (one path per line, relative to the project), wait 20 s, then read the board: it shows the file under your name, or the reservation refused because another worker holds it. Never modify a file another worker holds: do without it, or say in your report exactly what change it needs, for the lead.\n" +
		"- Keep `.athanor/HANDOFF.md` up to date, 10 lines at most, each time you finish a step: done, in progress, next, traps. If your context gets full, the pilot hands your branch to a fresh worker, who starts from that file.\n" +
		"- Do not modify `.athanor/PLAN.md` or `.athanor/LOG.md`: the project lead handles them after the merge.\n" +
		"- " + aides + "\n" +
		"- Check with `bash .athanor/check.sh quick <pattern>` (or your milestone's tests if there is no script), then `git add -A && git commit -m \"" + ident + ": ...\"` on your branch. Commit even partial work, and say so.\n" +
		"- Something only the user can do (an account, a payment, a code, a key)? Do not ask anyone and do not create a task: do what you can without it and say exactly what is missing in your report; the lead hands it to the user.\n" +
		"- End with a report of at most 10 lines: done / not done / files touched / what the lead needs to know to merge.\n"
}

func consignesFusionParallele(fusionnes, aFusionner, echoues string) string {
	ou := func(s string) string {
		if s == "" {
			return "none"
		}
		return s
	}
	return "\n## Verification after a parallel cycle\n\nThe pilot has just run workers in parallel (reports in `.athanor/WORKER_REPORTS.md`):\n" +
		"- **Merged automatically** (no conflict, already in the current branch): " + ou(fusionnes) + ". Verify them, do not redo them.\n" +
		"- **To merge by you** (conflicts): " + ou(aFusionner) + ". For each, in plan order: `git merge --no-ff auto/<milestone>`, resolve the conflict keeping the intent of both sides, complete the merge, run `bash .athanor/check.sh quick <pattern>`.\n" +
		"- **Failed or empty** (no usable commit): " + ou(echoues) + ". Their branch may hold partial work; decide whether to keep it or redo the milestone later.\n" +
		"Then **one** full check `bash .athanor/check.sh` and fix whatever the merges broke (small fixes only; a real rework becomes a note in `.athanor/BUGS.md`). In `.athanor/PLAN.md`, keep ticked only the milestones that really pass; untick the others and note in `.athanor/BUGS.md` what is missing. One log entry, one commit.\n"
}

type ouvrier struct {
	ident, ligne, journal string
	cmd                   *exec.Cmd
	fini                  chan struct{}
}

func (o *ouvrier) enVie() bool {
	select {
	case <-o.fini:
		return false
	default:
		return true
	}
}

func lireTexteBrut(f string) string { b, _ := os.ReadFile(f); return string(b) }

// copierSansLiens: cp -R -P then the links deleted (a worker's edit stays its own; no link followed).
func copierSansLiens(src, dst string) {
	_ = filepath.Walk(src, func(c string, st os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(src, c)
		d := filepath.Join(dst, r)
		switch {
		case st.Mode()&os.ModeSymlink != 0:
			return nil
		case st.IsDir():
			_ = os.MkdirAll(d, 0o755)
		case st.Mode().IsRegular():
			if b, err := os.ReadFile(c); err == nil {
				_ = os.WriteFile(d, b, st.Mode().Perm())
			}
		}
		return nil
	})
}

// lancerOuvrier: parallele_lancer_ouvrier: the worker's worktree, its workshop, settings, MCP servers and
// instructions, then its Claude session (its stream straight into its log).
func (p *pilote) lancerOuvrier(ident, ligne, journal, modele string) (*ouvrier, bool) {
	trav := filepath.Join(p.etat, "work", ident)
	if gitP(p.projet, nil, "worktree", "remove", "--force", trav).Run() != nil {
		_ = os.RemoveAll(trav)
	}
	_ = gitP(p.projet, nil, "worktree", "prune").Run()
	reprise := false
	// an earlier attempt left commits on its branch (stopped by the quota or the watchdog): kept, resumed
	if gitP(p.projet, nil, "rev-parse", "-q", "--verify", "auto/"+ident+"^{commit}").Run() == nil &&
		entierPilote(sortieGit(p.projet, "rev-list", "--count", "HEAD..auto/"+ident), 0) > 0 &&
		gitP(p.projet, nil, "worktree", "add", "-q", trav, "auto/"+ident).Run() == nil {
		reprise = true
		// on top of what was merged since, so that its own merge does not conflict; as it was if that fails
		if gitP(trav, nil, "rebase", "-q", sortieGit(p.projet, "rev-parse", "HEAD")).Run() == nil {
			p.log(fmt.Sprintf("Worker %s: resumes the work already on auto/%s, brought up to date.", ident, ident))
		} else {
			_ = gitP(trav, nil, "rebase", "--abort").Run()
			p.log(fmt.Sprintf("Worker %s: resumes the work already on auto/%s (not brought up to date: it conflicts, the merge will tell).", ident, ident))
		}
	} else {
		_ = gitP(p.projet, nil, "branch", "-D", "auto/"+ident).Run()
		if gitP(p.projet, nil, "worktree", "add", "-q", "-b", "auto/"+ident, trav, "HEAD").Run() != nil {
			p.log(fmt.Sprintf("Worker %s: cannot create the worktree.", ident))
			return nil, false
		}
	}
	at := filepath.Join(trav, ".athanor")
	_ = os.RemoveAll(at)
	_ = os.MkdirAll(filepath.Join(at, "screenshots"), 0o755)
	_ = os.MkdirAll(filepath.Join(at, ".verif"), 0o755)
	for _, f := range []string{"PLAN.md", "REQUEST.md", "check.sh", "PC_TOOLS.md", "PC_TOOLS_GUIDE.md", "MAP.md", "BUGS.md", "SOLUTIONS.md", "secrets.env"} {
		src := filepath.Join(p.etat, f)
		if fichierRegulierPilote(src) && !estLien(src) {
			if b, err := os.ReadFile(src); err == nil {
				st, _ := os.Stat(src)
				_ = os.WriteFile(filepath.Join(at, f), b, st.Mode().Perm())
			}
		}
	}
	// the working documents and the files attached to the requests: the worker's sandbox sees its worktree only
	for _, f := range []string{"docs", "attachments"} {
		src := filepath.Join(p.etat, f)
		if dossierExistePilote(src) && !estLien(src) {
			copierSansLiens(src, filepath.Join(at, f))
		}
	}
	// the worker's team: "executor" (cheap model), plus the project's own sub-agents
	agents := ""
	if p.agentsParJSON {
		c := p.exeAthanor([]string{"SUBAGENT_MODEL=" + p.subagentModel, "CHEAP_MODEL=" + p.cheapModel}, "equipe", "json", p.kit, p.projet, filepath.Join(p.prive, "agents"), "--ouvrier")
		out, _ := c.Output()
		agents = strings.TrimRight(string(out), "\r\n")
		if agents == "{}" {
			agents = ""
		}
	}
	consignes := lireTexteBrut(p.consignesBaseParallele()) + consignesOuvrierParallele(ident, ligne, agents != "", reprise)
	if fRelais := filepath.Join(p.prive, "relais-"+ident+".md"); tailleFichierPilote(fRelais) > 0 {
		consignes += "\n" + lireTexteBrut(fRelais)
	}
	fConsignes := filepath.Join(p.prive, "CLAUDE.ouvrier-"+ident+".md")
	_ = os.WriteFile(fConsignes, []byte(consignes), 0o644)
	_ = os.Remove(filepath.Join(p.prive, "relais-"+ident+".md"))
	// the worker's sandbox: its worktree (PROJECT), plus write access to the project's .git
	fReglages := filepath.Join(p.prive, "reglages-"+ident+".json")
	if p.exeAthanor([]string{"ATHANOR_PROJET_PRINCIPAL=" + p.projet, "PROJECT=" + trav}, "reglages", p.kit, fReglages).Run() != nil {
		p.log(fmt.Sprintf("Worker %s: cannot build the settings.", ident))
		return nil, false
	}
	c := p.exeAthanor(nil, "reglages-ouvrier", fReglages, filepath.Join(p.projet, ".git"))
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if c.Run() != nil {
		return nil, false
	}
	but := fmt.Sprintf("/goal The milestone %s of .athanor/PLAN.md is carried out according to your worker instructions, checked, and committed on branch auto/%s (git log shows it), with your report as the final answer. Or stop after %d turns. %s", ident, ident, p.maxTurnsPerCycle, p.phraseLangue)
	// its MCP servers work on its worktree; the skills are the project's
	fMCP := filepath.Join(p.prive, "mcp-"+ident+".json")
	c = p.exeAthanor(nil, "extensions", "mcp", p.kit, p.projet, fMCP, "--dossier-mcp", trav, "--principal", p.projet)
	if j := p.journalPilote(); j != nil {
		c.Stderr = j
		defer j.Close()
	}
	_ = c.Run()
	if fichierRegulierPilote(fMCP) && tailleFichierPilote(fMCP+".sauvegarde.md") > 0 {
		if f, err := os.OpenFile(fConsignes, os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = f.WriteString("\n" + lireTexteBrut(fMCP+".sauvegarde.md"))
			f.Close()
		}
	}
	a := []string{"-p", but, "--permission-mode", "bypassPermissions", "--settings", fReglages, "--permission-prompts", "none", "--output-format", "stream-json", "--verbose"}
	a = append(a, p.argsSurs...)
	a = append(a, "--append-system-prompt", strings.TrimRight(lireTexteBrut(fConsignes), "\r\n"))
	if agents != "" {
		a = append(a, "--agents", agents)
	}
	if fichierRegulierPilote(fMCP) {
		a = append(a, "--mcp-config", fMCP)
	}
	if p.pluginAthanor != "" {
		a = append(a, "--plugin-dir", p.pluginAthanor)
	}
	for _, d := range p.pluginsDirs {
		a = append(a, "--plugin-dir", d)
	}
	if modele != "" {
		a = append(a, "--model", modele)
	}
	if p.fallbackModel != "" && modele != p.fallbackModel {
		a = append(a, "--fallback-model", p.fallbackModel)
	}
	if p.effort != "" {
		a = append(a, "--effort", p.effort)
	}
	a = p.argsNatifs(a, filepath.Join(p.prive, "ouvrier-"+ident))
	prog := p.progClaude(filepath.Join(p.prive, "ouvrier_"+ident+".winpid"))
	j, err := os.OpenFile(journal, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, false
	}
	cmd := commande(prog[0], append(prog[1:], a...)...)
	cmd.Dir = trav
	env := []string{}
	for _, e := range p.envClaude() {
		if !strings.HasPrefix(e, "PROJECT=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, "PROJECT="+trav)
	cmd.Stdout, cmd.Stderr = j, j
	preparerSessionClaude(cmd)
	if err := cmd.Start(); err != nil {
		j.Close()
		p.log(fmt.Sprintf("Worker %s: cannot start Claude (%v).", ident, err))
		return nil, false
	}
	j.Close()
	_ = os.WriteFile(filepath.Join(p.prive, "ouvrier_"+ident+".sid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(p.prive, "ouvrier_"+ident+".pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	if p.natif {
		if _, err := os.Stat(filepath.Join(p.prive, "ouvrier_"+ident+".winpid")); err != nil {
			_ = os.WriteFile(filepath.Join(p.prive, "ouvrier_"+ident+".winpid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
		}
	}
	o := &ouvrier{ident: ident, ligne: ligne, journal: journal, cmd: cmd, fini: make(chan struct{})}
	go func() {
		defer p.recupererPanique()
		err := cmd.Wait()
		code, tue := 0, false
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() >= 0 {
				code = ee.ExitCode()
			} else {
				tue = true // stopped by a signal (watchdog, relay): no exit code, as for the script's session
			}
		}
		if !tue {
			_ = os.WriteFile(filepath.Join(p.prive, "ouvrier_"+ident+".code"), []byte(strconv.Itoa(code)+"\n"), 0o644)
		}
		close(o.fini)
	}()
	return o, true
}

func sortieGit(dossier string, args ...string) string {
	out, err := gitP(dossier, nil, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resultatsJournal: the .result of the log's result lines (jq -r '.result // empty'), the last n lines.
func resultatsJournal(journal string, n int) string {
	var lignes []string
	f, err := os.Open(journal)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		l, err := r.ReadBytes('\n')
		if len(l) > 0 && bytes.Contains(l, []byte(`"result"`)) {
			var m map[string]any
			d := json.NewDecoder(bytes.NewReader(l))
			d.UseNumber()
			if d.Decode(&m) == nil && m["type"] == "result" {
				switch v := m["result"].(type) {
				case nil:
				case string:
					lignes = append(lignes, strings.Split(v, "\n")...)
				case bool:
					if v {
						lignes = append(lignes, "true")
					}
				default:
					b, _ := json.Marshal(v)
					lignes = append(lignes, string(b))
				}
			}
		}
		if err != nil {
			break
		}
	}
	if len(lignes) > n {
		lignes = lignes[len(lignes)-n:]
	}
	if len(lignes) == 0 {
		return ""
	}
	return strings.Join(lignes, "\n") + "\n"
}

// terminerOuvrier: parallele_terminer_ouvrier: leftover work committed, statistics, report, then the
// automatic merge when it applies cleanly. fusionne | a_fusionner | echoue.
func (p *pilote) terminerOuvrier(o *ouvrier, prefixe, modeleOuvrier string) string {
	ident := o.ident
	trav := filepath.Join(p.etat, "work", ident)
	<-o.fini
	arreterSessionClaude(true, filepath.Join(p.prive, "ouvrier_"+ident+".sid"), filepath.Join(p.prive, "ouvrier_"+ident+".winpid"), filepath.Join(p.prive, "arret.log"))
	_ = os.Remove(filepath.Join(p.prive, "ouvrier_"+ident+".winpid"))
	code := entierPilote(lireTexteCourt(filepath.Join(p.prive, "ouvrier_"+ident+".code")), 1)
	if dossierExistePilote(trav) && sortieGit(trav, "status", "--porcelain") != "" {
		if gitP(trav, nil, "add", "-A").Run() == nil && gitP(trav, nil, "commit", "-qm", ident+": uncommitted worker work (recorded by the pilot)").Run() == nil {
			p.log(fmt.Sprintf("Worker %s: uncommitted work recorded on auto/%s.", ident, ident))
		}
	}
	journal := prefixe + "-" + ident + ".jsonl"
	etat := "erreur"
	if journalQuotaPilote(journal) {
		etat = "quota"
	} else if code == 0 {
		etat = "ok"
	}
	ancien := p.modeleCycle
	if modeleOuvrier != "" {
		p.modeleCycle = modeleOuvrier
	}
	p.enregistrerStats(journal, etat, fmt.Sprintf("%d·%s", p.cycle, ident), "ref:auto/"+ident)
	p.modeleCycle = ancien
	// what the worker learned about the tools or the project: kept for everyone
	_ = p.exeAthanor(nil, "solutions", "fusionner", filepath.Join(p.etat, "SOLUTIONS.md"), filepath.Join(trav, ".athanor", "SOLUTIONS.md")).Run()
	commits := entierPilote(sortieGit(p.projet, "rev-list", "--count", "HEAD..auto/"+ident), 0)
	if f, err := os.OpenFile(filepath.Join(p.prive, "rapports_ouvriers.md"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644); err == nil {
		fmt.Fprintf(f, "\n## %s (branch auto/%s, exit code %d, commits: %d)\n%s", ident, ident, code, commits, resultatsJournal(journal, 40))
		f.Close()
	}
	if gitP(p.projet, nil, "worktree", "remove", "--force", trav).Run() != nil {
		_ = os.RemoveAll(trav)
	}
	_ = gitP(p.projet, nil, "worktree", "prune").Run()
	if code != 0 || commits <= 0 {
		p.log(fmt.Sprintf("Worker %s finished without a usable result (exit %d, %d commit(s)).", ident, code, commits))
		return "echoue"
	}
	// automatic merge: clean = merged now (the lead verifies at the end of the cycle); conflict = for the lead
	if gitP(p.projet, nil, "merge", "--no-ff", "--no-edit", "-q", "-m", fmt.Sprintf("Merge auto/%s (worker %s)", ident, ident), "auto/"+ident).Run() == nil {
		c := p.exeAthanor(nil, "plan", "cocher", filepath.Join(p.etat, "PLAN.md"), ident)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		_ = c.Run()
		if f, err := os.OpenFile(filepath.Join(p.etat, "LOG.md"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644); err == nil {
			fmt.Fprintf(f, "- %s — %s merged automatically by the pilot (branch auto/%s, %d commit(s)); verified by the lead at the end of the cycle.\n", time.Now().Format("2006-01-02 15:04"), ident, ident, commits)
			f.Close()
		}
		_ = gitP(p.projet, nil, "branch", "-d", "auto/"+ident).Run()
		p.log(fmt.Sprintf("Worker %s merged automatically (%d commit(s)).", ident, commits))
		return "fusionne"
	}
	_ = gitP(p.projet, nil, "merge", "--abort").Run()
	p.log(fmt.Sprintf("Worker %s finished: conflicts, the lead will merge auto/%s.", ident, ident))
	return "a_fusionner"
}

var reReservation = regexp.MustCompile("^[[:space:]`]*|[[:space:]`]*$")

func avantTiret(ligne string) string {
	if i := strings.Index(ligne, " — "); i >= 0 {
		return ligne[:i]
	}
	return ligne
}

func joindre(l []string) string { return strings.Join(l, " ") }

// cycleParallele: one full parallel cycle (parallele_cycle). prefixe = the cycle log without .jsonl.
// Sets p.fichierLog, p.quotaOuvriers and p.modeleVerif; 3: no worker could start (back to serial).
func (p *pilote) cycleParallele(prefixe, template string) int {
	modeleOuvrier := p.workerModel
	if modeleOuvrier == "" {
		modeleOuvrier = template
	}
	quotaVu := false
	var courants, fusionnes, aFusionner, echoues, traites []string
	ouvriers := map[string]*ouvrier{}
	lignes, reserves, debuts := map[string]string{}, map[string]string{}, map[string]string{}
	resLus, relaisN := map[string]int{}, map[string]int{}
	ctxVu, relaisVu := map[string]int64{}, map[string]int64{}
	attentes, attendus, refuses := "", "", ""
	limite := p.parallelMax
	if paralleleDepart < limite {
		limite = paralleleDepart
	}
	_ = os.MkdirAll(filepath.Join(p.etat, "work"), 0o755)
	// uncommitted changes in the main checkout (a lead session cut short) would make the merges fail
	if sortieGit(p.projet, "status", "--porcelain", "--untracked-files=no") != "" {
		if gitP(p.projet, nil, "add", "-u").Run() == nil && gitP(p.projet, nil, "commit", "-q", "-m", "Work in progress before the parallel cycle (lead)").Run() == nil {
			p.log("Uncommitted changes of the lead committed before the cycle.")
		}
	}
	p.branchesPartielles() // stale branches out before any worktree is made
	for _, motif := range []string{"ouvrier_*.sid", "ouvrier_*.code", "ouvrier_*.pid", "mcp-*.json"} {
		l, _ := filepath.Glob(filepath.Join(p.prive, motif))
		for _, f := range l {
			_ = os.Remove(f)
		}
	}
	fRapports := filepath.Join(p.prive, "rapports_ouvriers.md")
	_ = os.WriteFile(fRapports, []byte("# Worker reports ("+time.Now().Format("2006-01-02 15:04")+")\n"), 0o644)
	debut := time.Now().Unix()
	dans := func(l []string, x string) bool {
		for _, y := range l {
			if y == x {
				return true
			}
		}
		return false
	}
	enCours := func(sauf string) []string {
		var r []string
		for _, c := range courants {
			if c != sauf {
				r = append(r, c+"\t"+lignes[c]+"\t"+reserves[c])
			}
		}
		return r
	}
	// the team board, in every running worker's workshop: who does what, which files each one holds
	tableau := func() {
		var b strings.Builder
		b.WriteString("# Team board\n\n")
		b.WriteString("Written by the pilot every few seconds (" + time.Now().Format("15:04:05") + "). Never modify a file another worker holds: the Files of its sheet, or a file it reserved.\n\n")
		b.WriteString("## At work now\n")
		for _, c := range courants {
			res := ""
			if reserves[c] != "" {
				res = " · reserved: " + reserves[c]
			}
			d := debuts[c]
			if d == "" {
				d = "?"
			}
			fmt.Fprintf(&b, "- **%s** since %s: %s%s\n", c, d, couperOctets(lignes[c], 400), res)
		}
		if len(fusionnes)+len(aFusionner)+len(echoues) > 0 {
			ou := func(l []string) string {
				if len(l) == 0 {
					return "none"
				}
				return joindre(l)
			}
			fmt.Fprintf(&b, "\n## Done in this cycle\n- merged: %s · to merge by the lead: %s · failed: %s\n", ou(fusionnes), ou(aFusionner), ou(echoues))
		}
		if attendus != "" {
			b.WriteString("\n## Waiting for a file a worker holds\n-" + attendus + "\n")
		}
		if refuses != "" {
			b.WriteString("\n## Reservations refused\n" + refuses)
		}
		t := b.String()
		sauf3 := func(s string) string {
			l := strings.SplitN(s, "\n", 4)
			if len(l) < 4 {
				return ""
			}
			return strings.TrimRight(l[3], "\n")
		}
		for _, c := range courants {
			at := filepath.Join(p.etat, "work", c, ".athanor")
			if !dossierExistePilote(at) || estLien(at) {
				continue
			}
			f := filepath.Join(at, "BOARD.md")
			if sauf3(lireTexteBrut(f)) == sauf3(t) {
				continue
			}
			// the worker can write in its workshop: never through a link it left there
			neuf := f + ".neuf"
			_ = os.Remove(neuf)
			if o, err := os.OpenFile(neuf, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
				_, _ = o.WriteString(t)
				o.Close()
				_ = os.Rename(neuf, f)
			}
		}
		attendus = ""
	}
	// new lines of a worker's .athanor/RESERVE: granted unless another running worker holds the file
	reservations := func(c string) {
		f := filepath.Join(p.etat, "work", c, ".athanor", "RESERVE")
		if !fichierRegulierPilote(f) || estLien(f) {
			return
		}
		autres := enCours(c)
		n := 0
		// the lines ended by a line end only (read -r leaves an unfinished last line for the next look)
		var lignesRes []string
		if texte := lireTexteBrut(f); strings.Contains(texte, "\n") {
			lignesRes = strings.Split(texte[:strings.LastIndexByte(texte, '\n')], "\n")
		}
		for i, l := range lignesRes {
			if i >= 200 {
				break
			}
			n++
			if n <= resLus[c] {
				continue
			}
			l = strings.ReplaceAll(l, "\r", "")
			l = reReservation.ReplaceAllString(l, "")
			l = strings.TrimPrefix(l, "./")
			if l == "" {
				continue
			}
			if strings.HasPrefix(l, "/") || strings.Contains(l, "..") || strings.Contains(l, ",") || strings.HasPrefix(l, ".athanor") {
				refuses += "- " + c + ": " + l + " (not a file of the project)\n"
				continue
			}
			autre := ""
			if len(autres) > 0 {
				autre = chevauchement(filepath.Join(p.etat, "PLAN.md"), "Files: "+l, autres)
			}
			if autre != "" {
				refuses += "- " + c + ": " + l + " (held by " + autre + ")\n"
				p.log(fmt.Sprintf("Worker %s: reservation of %s refused, %s holds it.", c, l, autre))
			} else if reserves[c] != "" {
				reserves[c] += "," + l
			} else {
				reserves[c] = l
			}
		}
		resLus[c] = n
	}
	statutOuvriers := func() {
		p.etatOuvriers = joindre(courants)
		s := joindre(courants)
		if s == "" {
			s = "none"
		}
		if len(fusionnes) > 0 {
			s += " · merged: " + joindre(fusionnes)
		}
		p.statut("Workers in parallel: " + s)
	}
	// fills the free slots with ready milestones (none once the quota was met)
	lancerPrets := func() {
		if quotaVu {
			return
		}
		for len(courants) < limite {
			n := len(courants)
			exclus := map[string]bool{}
			for _, t := range traites {
				exclus[t] = true
			}
			for _, j := range p.pretsParallele(exclus) {
				if len(courants) >= limite {
					break
				}
				ident, ligne := j[0], j[3]
				if dans(traites, ident) {
					continue
				}
				if autres := enCours(""); len(autres) > 0 {
					if occupe := chevauchement(filepath.Join(p.etat, "PLAN.md"), ligne, autres); occupe != "" {
						// it starts as soon as the other one is done
						if !strings.Contains(" "+attentes+" ", " "+ident+" ") {
							attentes += " " + ident
							p.log(fmt.Sprintf("Worker %s waits for %s: they change the same files.", ident, occupe))
						}
						attendus += " " + ident + "(" + occupe + ")"
						continue
					}
				}
				traites = append(traites, ident)
				lignes[ident] = ligne
				debuts[ident] = time.Now().Format("15:04")
				if o, ok := p.lancerOuvrier(ident, ligne, prefixe+"-"+ident+".jsonl", modeleOuvrier); ok {
					ouvriers[ident] = o
					courants = append(courants, ident)
					p.log(fmt.Sprintf("Worker %s started: %s", ident, avantTiret(ligne)))
				} else {
					echoues = append(echoues, ident)
				}
			}
			if len(courants) <= n {
				break
			}
		}
		statutOuvriers()
		tableau()
	}
	avec := ""
	if modeleOuvrier != "" {
		avec = ", with " + modeleOuvrier
	}
	p.log(fmt.Sprintf("Cycle %d (parallel): %d workers to start with, up to %d%s", p.cycle, limite, p.parallelMax, avec))
	lancerPrets()
	if len(courants) == 0 {
		p.log("No worker could start: back to the serial mode for this cycle.")
		return 3
	}
	arreter := func(c string, kill bool) {
		arreterSessionClaude(kill, filepath.Join(p.prive, "ouvrier_"+c+".sid"), filepath.Join(p.prive, "ouvrier_"+c+".winpid"), filepath.Join(p.prive, "arret.log"))
	}
	for len(courants) > 0 {
		select {
		case <-time.After(10 * time.Second):
		case <-p.arretCh:
			p.quitterSurSignal()
		}
		maintenant := time.Now().Unix()
		var restants []string
		for _, ident := range courants {
			o := ouvriers[ident]
			journal := prefixe + "-" + ident + ".jsonl"
			if o.enVie() {
				reservations(ident)
				// context full: its branch goes to a fresh worker, between two steps (10 min later at most), 3 times a cycle at most
				fRelais := filepath.Join(p.prive, "relais_ouvrier-"+ident)
				if p.relayContextK > 0 && relaisN[ident] < 3 && maintenant-ctxVu[ident] >= 60 && !fichierRegulierPilote(fRelais) {
					ctxVu[ident] = maintenant
					if ctx := p.relaisContexte(journal); ctx >= p.relayContextK*1000 {
						if relaisVu[ident] == 0 {
							relaisVu[ident] = maintenant
						}
						if p.relaisSur(journal) || maintenant-relaisVu[ident] >= 600 {
							_ = os.WriteFile(filepath.Join(p.prive, "relais-"+ident+".md"), []byte(p.relaisResume(journal, filepath.Join(p.etat, "work", ident, ".athanor", "HANDOFF.md"))), 0o644)
							_ = os.WriteFile(fRelais, nil, 0o644)
							p.log(fmt.Sprintf("Worker %s: context full (%dk tokens), a fresh worker takes over its branch.", ident, ctx/1000))
							arreter(ident, false)
							time.Sleep(5 * time.Second)
							arreter(ident, true)
						}
					}
				}
				derniere := debut
				if st, err := os.Stat(journal); err == nil {
					derniere = st.ModTime().Unix()
				}
				if maintenant-derniere >= int64(p.idleMaxMin*p.secondsPerMinute) || maintenant-debut >= int64(p.cycleMaxMin*p.secondsPerMinute) {
					p.log(fmt.Sprintf("Worker %s stopped by the watchdog.", ident))
					arreter(ident, false)
					time.Sleep(5 * time.Second)
					arreter(ident, true)
				}
				restants = append(restants, ident)
				continue
			}
			assainirAtelier(p.etat)
			issue := p.terminerOuvrier(o, prefixe, modeleOuvrier)
			delete(ouvriers, ident)
			if fRelais := filepath.Join(p.prive, "relais_ouvrier-"+ident); fichierRegulierPilote(fRelais) {
				// relayed: its work is committed on its branch, a fresh worker resumes it at once
				_ = os.Remove(fRelais)
				relaisN[ident]++
				delete(relaisVu, ident)
				if issue == "echoue" {
					var garde []string
					for _, x := range traites {
						if x != ident {
							garde = append(garde, x)
						}
					}
					traites = garde
					delete(reserves, ident)
					delete(resLus, ident)
					continue
				}
			}
			delete(reserves, ident)
			delete(resLus, ident)
			switch issue {
			case "fusionne":
				fusionnes = append(fusionnes, ident)
			case "a_fusionner":
				aFusionner = append(aFusionner, ident)
			default:
				echoues = append(echoues, ident)
			}
			if journalQuotaPilote(journal) {
				if !quotaVu {
					p.log(fmt.Sprintf("Worker %s met the quota: no other worker starts in this cycle.", ident))
				}
				quotaVu = true
			} else if issue != "echoue" && limite < p.parallelMax {
				limite++ // a delivery without quota trouble: one more worker at once
			}
			_ = publierFichier(filepath.Join(p.etat, "WORKER_REPORTS.md"), []byte(lireTexteBrut(fRapports)))
		}
		courants = restants
		tableau()
		// the cycle's time budget is spent: no new worker, the lead verifies what is there
		if maintenant-debut < int64(p.cycleMaxMin*p.secondsPerMinute) {
			lancerPrets()
		} else {
			p.etatOuvriers = joindre(courants)
			s := joindre(courants)
			if s == "" {
				s = "none"
			}
			p.statut("Workers in parallel: " + s)
		}
	}
	p.etatOuvriers = ""
	// every worker met the quota and nothing came in: no verification session; the pilot reads the quota,
	// and its reset time, from a worker's log, then waits as after a serial cycle
	if quotaVu && len(fusionnes) == 0 && len(aFusionner) == 0 {
		p.log(fmt.Sprintf("Workers finished: all stopped by the quota (%s). No verification by the lead.", joindre(echoues)))
		for _, ident := range echoues {
			journal := prefixe + "-" + ident + ".jsonl"
			if !journalQuotaPilote(journal) {
				continue
			}
			_ = os.WriteFile(prefixe+".jsonl", []byte(evenementsQuota(journal)), 0o644)
			break
		}
		p.quotaOuvriers = true
		p.fichierLog = prefixe + ".jsonl"
		return 1
	}
	// every worker merged cleanly: the verification only runs the checks and ticks the plan, execution
	// work for the workers' model; a conflict or a failure needs the lead's judgement, on its own model
	verif := template
	if len(aFusionner) == 0 && len(echoues) == 0 && modeleOuvrier != "" && modeleOuvrier != template {
		verif = modeleOuvrier
		p.log(fmt.Sprintf("Workers finished: merged %s, all cleanly. The verification runs with %s.", joindre(fusionnes), verif))
	} else {
		ou := func(l []string) string {
			if len(l) == 0 {
				return "none"
			}
			return joindre(l)
		}
		p.log(fmt.Sprintf("Workers finished: merged %s; to merge %s; failed %s. The lead verifies.", ou(fusionnes), ou(aFusionner), ou(echoues)))
	}
	p.modeleVerif = verif
	p.statut(fmt.Sprintf("Merging milestones %s %s", joindre(aFusionner), joindre(fusionnes)))
	p.fichierLog = prefixe + ".jsonl"
	branches := joindre(aFusionner)
	if branches == "" {
		branches = "(none)"
	}
	but := fmt.Sprintf("/goal The parallel cycle is verified according to your verification instructions: the branches %s are merged, bash .athanor/check.sh has been run and its output is shown, .athanor/PLAN.md ticks exactly the milestones that pass, .athanor/LOG.md has a new entry, git log shows the commit. Or stop after %d turns. %s", branches, p.maxTurnsPerCycle, p.phraseLangue)
	consignes := strings.TrimRight(lireTexteBrut(p.consignesBaseParallele())+consignesFusionParallele(joindre(fusionnes), joindre(aFusionner), joindre(echoues)), "\r\n")
	args := []string{"-p", but, "--permission-mode", "bypassPermissions", "--settings", p.reglages, "--permission-prompts", "none", "--output-format", "stream-json", "--verbose"}
	args = append(args, p.argsSurs...)
	args = append(args, "--append-system-prompt", consignes)
	if p.agentsParJSON {
		args = append(args, "--agents", p.agentsJSON)
	}
	if fichierRegulierPilote(filepath.Join(p.prive, "mcp.json")) {
		args = append(args, "--mcp-config", filepath.Join(p.prive, "mcp.json"))
	}
	if p.pluginAthanor != "" {
		args = append(args, "--plugin-dir", p.pluginAthanor)
	}
	for _, d := range p.pluginsDirs {
		args = append(args, "--plugin-dir", d)
	}
	if verif != "" {
		args = append(args, "--model", verif)
	}
	if p.fallbackModel != "" && verif != p.fallbackModel {
		args = append(args, "--fallback-model", p.fallbackModel)
	}
	if p.effort != "" {
		args = append(args, "--effort", p.effort)
	}
	// the verification touches several milestones: not closed at the first one; no relay
	clore := p.closeAfterMilestone
	p.closeAfterMilestone = "0"
	code := p.lancerClaudeSurveille(p.fichierLog, args, false, 0, false)
	p.closeAfterMilestone = clore
	// the workers' model ran out during the verification: the pilot treats it as the workers' quota
	if verif != template && journalQuotaPilote(p.fichierLog) {
		p.quotaOuvriers = true
	}
	// merged branches: deleted; the others stay visible until the next attempt
	for _, ident := range append(append([]string{}, aFusionner...), echoues...) {
		for _, l := range strings.Split(sortieGitBrute(p.projet, "branch", "--merged", "HEAD"), "\n") {
			if l == "  auto/"+ident {
				_ = gitP(p.projet, nil, "branch", "-d", "auto/"+ident).Run()
			}
		}
	}
	return code
}

func sortieGitBrute(dossier string, args ...string) string {
	out, _ := gitP(dossier, nil, args...).Output()
	return string(out)
}

func couperOctets(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// evenementsQuota: the rate limit events and the result (type, subtype, is_error, result) of a worker's
// log, one compact JSON per line, as jq -c writes them.
func evenementsQuota(journal string) string {
	var b strings.Builder
	f, err := os.Open(journal)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		l, err := r.ReadBytes('\n')
		t := bytes.TrimSpace(l)
		if len(t) > 0 && t[0] == '{' {
			var m map[string]json.RawMessage
			if json.Unmarshal(t, &m) == nil {
				var typ string
				_ = json.Unmarshal(m["type"], &typ)
				switch typ {
				case "rate_limit_event":
					var c bytes.Buffer
					if json.Compact(&c, t) == nil {
						b.Write(c.Bytes())
						b.WriteByte('\n')
					}
				case "result":
					champ := func(k string) string {
						if v, ok := m[k]; ok {
							var c bytes.Buffer
							if json.Compact(&c, v) == nil {
								return c.String()
							}
						}
						return "null"
					}
					fmt.Fprintf(&b, `{"type":%s,"subtype":%s,"is_error":%s,"result":%s}`+"\n", champ("type"), champ("subtype"), champ("is_error"), champ("result"))
				}
			}
		}
		if err != nil {
			break
		}
	}
	return b.String()
}
