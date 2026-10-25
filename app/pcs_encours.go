package main

// Several computers (pcs.go): the work in progress of a milestone is never lost when its computer goes
// away (switched off, its pilot stopped, the program it needs not working). Every 10 minutes, and when it
// lets a milestone go, the computer sends its unfinished work to GitHub, on a branch of its own
// athanor/encours/<computer>/<milestone>: an orphan commit with the patch from what GitHub has (never the
// project's files themselves, so GitHub runs none of the repository's workflows for it). The computer that
// takes the milestone next starts from it. A computer that comes back after another took its milestone over
// puts its own unfinished work aside (kept in its .git, never deleted) and starts from the team's.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	brancheEnCoursPC = "athanor/encours/"
	refsEnCoursPC    = "refs/athanor/encours/"
	maxPatchPC       = 20 << 20 // beyond, the work in progress is not sent (a build output committed by mistake)
)

// EnCoursPC: what the work in progress says about itself (info.json).
type EnCoursPC struct {
	Jalon string `json:"jalon"`
	PC    string `json:"pc"`
	Nom   string `json:"nom"`
	Base  string `json:"base"` // the commit of GitHub the patch starts from
	Quand string `json:"quand"`
}

var reRefJalonPC = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func refJalonPC(jalon string) string {
	s := strings.Trim(reRefJalonPC.ReplaceAllString(jalon, "-"), ".-")
	if len(s) > 40 || s != jalon {
		// cut or changed: a short hash keeps two milestones apart
		h := sha256.Sum256([]byte(jalon))
		if len(s) > 31 {
			s = s[:31]
		}
		s = strings.TrimRight(s, ".-") + "-" + hex.EncodeToString(h[:4])
	}
	if s == "" {
		s = "jalon"
	}
	return s
}

// indexTemporairePC: a fresh path for an index of its own (never shared by two calls at once).
func indexTemporairePC(projet string) (string, error) {
	f, err := os.CreateTemp(filepath.Join(projet, ".git"), "athanor-index-*")
	if err != nil {
		return "", err
	}
	nom := f.Name()
	f.Close()
	_ = os.Remove(nom) // git makes it: an empty file is not an index
	return nom, nil
}

// gIndexPC: git on the project with an index of its own (the project's index, and Claude's, untouched).
func gIndexPC(projet, index string, args ...string) ([]byte, error) {
	c := gTravailGH(projet, args...)
	c.Env = append(c.Env, "GIT_INDEX_FILE="+index)
	return c.Output()
}

// instantanePC: a commit of the project as it is now (its uncommitted files too, as git add -A sees them)
// on top of HEAD, nothing changed in the folder; "" when there is nothing more than HEAD.
func instantanePC(projet string) (string, error) {
	index, err := indexTemporairePC(projet)
	if err != nil {
		return "", err
	}
	defer os.Remove(index)
	if _, err := gIndexPC(projet, index, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := gIndexPC(projet, index, "add", "-A"); err != nil {
		return "", err
	}
	arbre, err := gIndexPC(projet, index, "write-tree")
	if err != nil {
		return "", err
	}
	tete, _ := gProjetGH(projet+"/.git", "rev-parse", "HEAD")
	arbreTete, _ := gProjetGH(projet+"/.git", "rev-parse", "HEAD^{tree}")
	if strings.TrimSpace(string(arbre)) == arbreTete {
		return tete, nil
	}
	c := gTravailGH(projet, "commit-tree", strings.TrimSpace(string(arbre)), "-p", "HEAD", "-m", "Athanor: work in progress")
	c.Env = append(c.Env, "GIT_AUTHOR_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com")
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// envoyerEnCoursPC: this computer's unfinished work on milestone jalon, sent to its branch on GitHub
// (replaced each time). The message for the log ("" when there was nothing to send).
func (e *equipePC) envoyerEnCoursPC(jalon string) string {
	if jalon == "" || jalon == jalonCadragePC || jalon == jalonLivraisonPC {
		return ""
	}
	libere := e.verrou()
	defer libere()
	// the base: the last commit of GitHub this computer's work stands on (read in the private folder, which
	// sees the project's commits)
	tete, _ := gProjetGH(e.projet+"/.git", "rev-parse", "HEAD")
	base, err := sortieGH(gmGH(e.miroir, nil, "merge-base", tete, refsDistantPC+firstNonEmpty(e.r.Branche, "main")))
	if err != nil || !reShaGH.MatchString(base) || !reShaGH.MatchString(tete) {
		return ""
	}
	if taille := tailleNonSuivisPC(e.projet); taille > maxPatchPC {
		// renders, builds…: never copied into .git every 10 minutes
		return fmt.Sprintf("Several computers: the files of %s not committed are too big to be sent (%d MB): the next computer starts from the last push.", jalon, taille>>20)
	}
	snap, err := instantanePC(e.projet)
	if err != nil || !reShaGH.MatchString(snap) || snap == base {
		return ""
	}
	patch, trop, err := patchPC(e.miroir, base, snap)
	if err != nil || len(patch) == 0 {
		return ""
	}
	if trop {
		return fmt.Sprintf("Several computers: the work in progress of %s is too big to be sent (over %d MB): the next computer starts from the last push.", jalon, maxPatchPC>>20)
	}
	if trouves, ok := analyserGH(e.miroir, snap, e.r.Atelier); !ok || len(trouves) > 0 {
		return "Several computers: the work in progress of " + jalon + " was not sent: it holds what looks like a secret (it goes with the next push's check)."
	}
	info, _ := json.MarshalIndent(EnCoursPC{Jalon: jalon, PC: e.moi.ID, Nom: e.moi.Nom, Base: base, Quand: time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	executer := func(entree []byte, args ...string) (string, error) {
		c := gmGH(e.miroir, nil, args...)
		c.Stdin = bytes.NewReader(entree)
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
	bInfo, err1 := executer(info, "hash-object", "-w", "--stdin")
	bPatch, err2 := executer(patch, "hash-object", "-w", "--stdin")
	if err1 != nil || err2 != nil {
		return ""
	}
	arbre, err := executer([]byte("100644 blob "+bInfo+"\tinfo.json\n100644 blob "+bPatch+"\tpatch.diff\n"), "mktree")
	if err != nil {
		return ""
	}
	c := gmGH(e.miroir, []string{"GIT_AUTHOR_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com"}, "commit-tree", arbre, "-m", "Athanor: work in progress of "+jalon+" ("+e.moi.Nom+")")
	out, err := c.Output()
	commit := strings.TrimSpace(string(out))
	if err != nil || !reShaGH.MatchString(commit) {
		return ""
	}
	if _, err := gitDistantGH(e.d, e.miroir, e.r, "push", "--porcelain", "--no-verify", "--force", "{url}",
		commit+":refs/heads/"+brancheEnCoursPC+e.moi.ID+"/"+refJalonPC(jalon)); err != nil {
		return ""
	}
	return ""
}

// patchPC: the patch from base to snap, made in the private folder (no configuration of the user or of the
// project: no colours, no other prefixes), read at most to maxPatchPC (trop: beyond).
func patchPC(miroir, base, snap string) ([]byte, bool, error) {
	c := gmGH(miroir, nil, "-c", "diff.noprefix=false", "-c", "diff.mnemonicPrefix=false", "-c", "color.ui=never",
		"diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
		"--src-prefix=a/", "--dst-prefix=b/", "--submodule=short", base, snap)
	out, err := c.StdoutPipe()
	if err != nil || c.Start() != nil {
		return nil, false, fmt.Errorf("diff")
	}
	b, _ := io.ReadAll(io.LimitReader(out, maxPatchPC+1))
	if len(b) > maxPatchPC {
		_ = c.Process.Kill()
		_ = c.Wait()
		return nil, true, nil
	}
	if err := c.Wait(); err != nil {
		return nil, false, err
	}
	return b, false, nil
}

// tailleNonSuivisPC: the size of the files not committed nor ignored (what git add -A would store).
func tailleNonSuivisPC(projet string) int64 {
	out, err := gTravailGH(projet, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return 0
	}
	var n int64
	for _, f := range strings.Split(string(out), "\x00") {
		if st, err := os.Lstat(filepath.Join(projet, filepath.FromSlash(f))); f != "" && err == nil && st.Mode().IsRegular() {
			n += st.Size()
		}
	}
	return n
}

// lireEnCoursPC: the works in progress on GitHub (fetched by recupererPCs), the newest first.
func lireEnCoursPC(miroir string) []EnCoursPC {
	refs, err := sortieGH(gmGH(miroir, nil, "for-each-ref", "--format=%(refname)", refsEnCoursPC))
	if err != nil {
		return nil
	}
	var v []EnCoursPC
	for _, ref := range strings.Split(refs, "\n") {
		if ref == "" {
			continue
		}
		b, err := gmGH(miroir, nil, "cat-file", "blob", ref+":info.json").Output()
		if err != nil || len(b) > 64<<10 {
			continue
		}
		var x EnCoursPC
		if json.Unmarshal(b, &x) != nil || !reIDMachine.MatchString(x.PC) || !reShaGH.MatchString(x.Base) ||
			ref != refsEnCoursPC+x.PC+"/"+refJalonPC(x.Jalon) {
			continue
		}
		x.Nom = coupeValide(x.Nom, 60)
		v = append(v, x)
	}
	sort.SliceStable(v, func(a, b int) bool { return v[a].Quand > v[b].Quand })
	return v
}

// nettoyerEnCoursPC: this computer's works in progress on milestones the team has finished, removed from
// GitHub (those it let go stay: the computer that takes them next goes on from them, then removes them).
func (e *equipePC) nettoyerEnCoursPC() {
	faits := faitsDistantPC(e.miroir, e.r)
	for _, x := range lireEnCoursPC(e.miroir) {
		if x.PC == e.moi.ID && faits[x.Jalon] {
			_, _ = gitDistantGH(e.d, e.miroir, e.r, "push", "--porcelain", "--no-verify", "{url}",
				":refs/heads/"+brancheEnCoursPC+x.PC+"/"+refJalonPC(x.Jalon))
		}
	}
}

// faitsDistantPC: the milestones ticked in the plan GitHub has.
func faitsDistantPC(miroir string, r reglagesGH) map[string]bool {
	v := map[string]bool{}
	b, err := gmGH(miroir, nil, "cat-file", "blob", refsDistantPC+firstNonEmpty(r.Branche, "main")+":.athanor/PLAN.md").Output()
	if err != nil {
		return v
	}
	for _, j := range identifierPlanPC(analyserPlan(string(b))) {
		if j.Fait {
			v[j.ID] = true
		}
	}
	return v
}

// reprendreEnCoursPC: the milestone just taken had unfinished work of another computer: merged into the
// project, so that this one goes on from it. What Claude is told ("" when there was none).
func (e *equipePC) reprendreEnCoursPC(j Jalon, ident []string) (string, string) {
	var x *EnCoursPC
	for _, y := range lireEnCoursPC(e.miroir) {
		if y.Jalon == j.ID && y.PC != e.moi.ID {
			y := y
			x = &y
			break
		}
	}
	if x == nil {
		return "", ""
	}
	ref := refsEnCoursPC + x.PC + "/" + refJalonPC(x.Jalon)
	patch, err := gmGH(e.miroir, nil, "cat-file", "blob", ref+":patch.diff").Output()
	if err != nil {
		return "", ""
	}
	if gTravailGH(e.projet, "cat-file", "-e", x.Base+"^{commit}").Run() != nil || !propreTravailPC(e.projet) {
		return "", fmt.Sprintf("Several computers: %s's unfinished work on %s could not be brought in (this folder has changes not committed, or lacks its base): this computer starts from the team's last push.", x.Nom, j.ID)
	}
	// the patch applied to its base in an index of its own, then merged like the others' work
	index, err := indexTemporairePC(e.projet)
	if err != nil {
		return "", ""
	}
	defer os.Remove(index)
	if _, err := gIndexPC(e.projet, index, "read-tree", x.Base); err != nil {
		return "", ""
	}
	c := gTravailGH(e.projet, "apply", "--cached", "--binary", "--whitespace=nowarn")
	c.Env = append(c.Env, "GIT_INDEX_FILE="+index)
	c.Stdin = bytes.NewReader(patch)
	if out, err := c.CombinedOutput(); err != nil {
		return "", fmt.Sprintf("Several computers: %s's unfinished work on %s does not apply (%s): this computer starts from the team's last push.", x.Nom, j.ID, derniereLigneSortie(string(out)))
	}
	arbre, err := gIndexPC(e.projet, index, "write-tree")
	if err != nil {
		return "", ""
	}
	cc := gTravailGH(e.projet, "commit-tree", strings.TrimSpace(string(arbre)), "-p", x.Base, "-m", "Work in progress of "+j.ID+" by "+x.Nom)
	cc.Env = append(cc.Env, "GIT_AUTHOR_NAME="+firstNonEmpty(x.Nom, "Athanor"), "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com")
	out, err := cc.Output()
	commit := strings.TrimSpace(string(out))
	if err != nil || !reShaGH.MatchString(commit) {
		return "", ""
	}
	_ = gTravailGH(e.projet, "update-ref", "refs/athanor/repris/"+refJalonPC(j.ID), commit).Run() // kept, whatever happens
	avant, _ := gProjetGH(e.projet+"/.git", "rev-parse", "HEAD")
	args := append(append([]string{}, ident...), "merge", "--no-edit", "-m", "Go on with "+j.ID+" from the work in progress of "+x.Nom, commit)
	if err := gTravailGH(e.projet, args...).Run(); err != nil {
		_ = gTravailGH(e.projet, "merge", "--abort").Run()
		if apres, _ := gProjetGH(e.projet+"/.git", "rev-parse", "HEAD"); apres != avant || !propreTravailPC(e.projet) {
			_ = gTravailGH(e.projet, "reset", "--hard", "--quiet", avant).Run()
		}
		return fmt.Sprintf(" %s started this milestone and could not finish it; its unfinished work is in commit %s (`git show %s`), which does not merge by itself with this folder: take from it what helps.", x.Nom, commit[:12], commit[:12]),
			fmt.Sprintf("Several computers: %s's unfinished work on %s does not merge by itself: Claude takes from it what helps.", x.Nom, j.ID)
	}
	// brought in: that branch is done with (this computer's pushes carry it now)
	_, _ = gitDistantGH(e.d, e.miroir, e.r, "push", "--porcelain", "--no-verify", "{url}", ":refs/heads/"+brancheEnCoursPC+x.PC+"/"+refJalonPC(x.Jalon))
	return fmt.Sprintf(" %s started this milestone and could not finish it: its unfinished work is merged into this folder (the last commit). Check what is done and what is left, then finish it.", x.Nom),
		fmt.Sprintf("Several computers: this computer goes on with %s from %s's unfinished work.", j.ID, x.Nom)
}

// reprisParAutrePC: this computer held milestone actuel (since depuis; laisse: it let it go when its pilot
// stopped) and the team has gone on without it: another running computer holds it (and took it first, or
// this one had let it go), another computer's work in progress on it is newer, or it is ticked on GitHub.
// This computer's unfinished work on it is then put aside (a commit kept in refs/athanor/sauve/, never
// deleted) and its folder goes back to the team's last push, so that two versions of one milestone never
// meet. misDeCote false: this computer goes on with it.
func (e *equipePC) reprisParAutrePC(actuel, depuis string, laisse bool) (msg string, misDeCote bool) {
	if actuel == "" || actuel == jalonCadragePC || actuel == jalonLivraisonPC {
		return "", false
	}
	libere := e.verrou()
	defer libere()
	pourquoi := ""
	for _, p := range e.actifs() {
		if p.Jalon == actuel && (laisse || p.Depuis < depuis || (p.Depuis == depuis && p.ID < e.moi.ID)) {
			pourquoi = fmt.Sprintf("%s builds %s now", p.Nom, actuel)
			break
		}
	}
	if pourquoi == "" && faitsDistantPC(e.miroir, e.r)[actuel] && !faitIciPC(e.projet, actuel) {
		pourquoi = fmt.Sprintf("the team finished %s meanwhile", actuel) // (not this computer: its plan has it to do)
	}
	if pourquoi == "" && laisse {
		for _, x := range lireEnCoursPC(e.miroir) {
			if x.Jalon == actuel && x.PC != e.moi.ID && x.Quand > depuis {
				pourquoi = fmt.Sprintf("%s went further on %s meanwhile", x.Nom, actuel)
				break
			}
		}
	}
	if pourquoi == "" {
		return "", false
	}
	removeSiExiste(e.d + "/pcs-jalon")
	branche := firstNonEmpty(e.r.Branche, "main")
	local := "refs/remotes/athanor/" + branche
	msg = "Several computers: " + pourquoi + ": this computer takes another milestone."
	if _, err := gProjetGH(e.projet+"/.git", "-c", "protocol.file.allow=always", "fetch", "--no-tags", "--no-write-fetch-head", "--quiet",
		e.miroir, "+"+refsDistantPC+branche+":"+local); err != nil {
		return msg, true
	}
	en, _ := gProjetGH(e.projet+"/.git", "rev-list", "--count", local+"..HEAD")
	if en == "0" && propreTravailPC(e.projet) && len(nonSuivisPC(e.projet)) == 0 {
		return msg, true
	}
	if gTravailGH(e.projet, "merge-base", local, "HEAD").Run() != nil {
		return msg, true
	}
	snap, err := instantanePC(e.projet)
	if err != nil || !reShaGH.MatchString(snap) {
		return msg + " (its own unfinished work stays in the folder: it could not be put aside)", true
	}
	garde := "refs/athanor/sauve/" + refJalonPC(actuel) + "-" + time.Now().Format("20060102-150405")
	if gTravailGH(e.projet, "update-ref", garde, snap).Run() != nil {
		return msg + " (its own unfinished work stays in the folder: it could not be put aside)", true
	}
	// the files it created and never committed leave the folder, or the team's version of them could not
	// come in; each one only once seen, with the same content, in the commit kept
	for _, f := range nonSuivisPC(e.projet) {
		if strings.HasPrefix(f, ".athanor/") {
			continue
		}
		chemin := filepath.Join(e.projet, filepath.FromSlash(f))
		garde, err1 := gProjetGH(e.projet+"/.git", "rev-parse", "-q", "--verify", snap+":"+f)
		ici, err2 := gTravailGH(e.projet, "hash-object", "--", f).Output() // as git add stored it
		if err1 == nil && err2 == nil && reShaGH.MatchString(garde) && strings.TrimSpace(string(ici)) == garde {
			_ = os.Remove(chemin)
		}
	}
	_ = gTravailGH(e.projet, "reset", "--hard", "--quiet", local).Run()
	return msg + fmt.Sprintf(" Its own unfinished work on it is kept in %s (commit %s).", garde, snap[:12]), true
}

// faitIciPC: milestone jalon is ticked in this folder's plan.
func faitIciPC(projet, jalon string) bool {
	b, err := os.ReadFile(filepath.Join(projet, ".athanor", "PLAN.md"))
	if err != nil {
		return false
	}
	for _, j := range identifierPlanPC(analyserPlan(string(b))) {
		if j.ID == jalon {
			return j.Fait
		}
	}
	return false
}

// nonSuivisPC: the files of the folder neither committed nor ignored.
func nonSuivisPC(projet string) []string {
	out, err := gTravailGH(projet, "ls-files", "-z", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil
	}
	var v []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			v = append(v, f)
		}
	}
	return v
}
