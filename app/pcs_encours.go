package main

// Several computers (pcs.go): the work in progress of a milestone is never lost when its computer goes
// away (switched off, its pilot stopped, the program it needs not working). Every 10 minutes, and when it
// lets a milestone go, the computer sends its unfinished work to GitHub, on a ref of its own
// refs/athanor/encours/<computer> (not a branch: hidden from the list of branches, and GitHub runs none of
// the repository's workflows when it changes or goes): an orphan commit with, for each milestone it left
// unfinished, the patch from what GitHub has. A milestone the team finished leaves it; with none left, the
// ref goes. The computer that
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
	refDistantEnCoursPC = "refs/athanor/encours/" // on GitHub (a ref, not a branch)
	brancheEnCoursPC    = "athanor/encours/"      // where 2.36 put it: removed once
	refsEnCoursPC       = "refs/athanor/encours/" // in the private folder
	gardeIgnoresPC      = 30 * 24 * time.Hour     // the copies of ignored files (copierIgnoresPC), kept so long
	gardeMinIgnoresPC   = 5
	maxPatchPC          = 20 << 20 // beyond, the work in progress is not sent (a build output committed by mistake)
)

// EnCoursPC: what the work in progress says about itself (info.json).
type EnCoursPC struct {
	Jalon string `json:"jalon"`
	PC    string `json:"pc"`
	Nom   string `json:"nom"`
	Base  string `json:"base"` // the commit of GitHub the patch starts from
	Quand string `json:"quand"`
}

// apresPC: moment a is after moment b (RFC 3339, with or without fractions of a second; "" is before all).
func apresPC(a, b string) bool {
	ta, ea := time.Parse(time.RFC3339Nano, a)
	tb, eb := time.Parse(time.RFC3339Nano, b)
	switch {
	case ea != nil:
		return false
	case eb != nil:
		return true
	}
	return ta.After(tb)
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
	info, _ := json.MarshalIndent(EnCoursPC{Jalon: jalon, PC: e.moi.ID, Nom: e.moi.Nom, Base: base, Quand: time.Now().UTC().Format(time.RFC3339Nano)}, "", "  ")
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
	lignes := "100644 blob " + bInfo + "\tinfo.json\n100644 blob " + bPatch + "\tpatch.diff\n"
	if porte, err := sortieGH(gmGH(e.miroir, nil, "rev-parse", "-q", "--verify", refPortePC+refJalonPC(jalon))); err == nil && reShaGH.MatchString(porte) {
		lignes += "040000 tree " + porte + "\tavant\n" // another computer's work it could not merge: passed on
	}
	dossier, err := executer([]byte(lignes), "mktree")
	if err != nil {
		return ""
	}
	entrees := e.mesEntreesEnCoursPC()
	entrees[refJalonPC(jalon)] = dossier
	_ = e.publierEnCoursPC(entrees, "Athanor: work in progress of "+jalon+" ("+e.moi.Nom+")")
	return ""
}

// mesEntreesEnCoursPC: the milestones of this computer's branch (name → its tree), those the team has
// finished left out.
func (e *equipePC) mesEntreesEnCoursPC() map[string]string {
	v := map[string]string{}
	ref := refsEnCoursPC + e.moi.ID
	out, err := gmGH(e.miroir, nil, "ls-tree", ref).Output()
	if err != nil {
		return v
	}
	faits := faitsDistantPC(e.miroir, e.r)
	// a milestone another computer went on with (its work in progress on it newer): this one's is out of date
	plusRecent := map[string]string{}
	for _, y := range lireEnCoursPC(e.miroir) {
		if y.PC != e.moi.ID && apresPC(y.Quand, plusRecent[y.Jalon]) {
			plusRecent[y.Jalon] = y.Quand
		}
	}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// "040000 tree <sha>\t<name>"
		meta, nom, ok := strings.Cut(l, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || f[1] != "tree" || !reShaGH.MatchString(f[2]) {
			continue
		}
		b, err := gmGH(e.miroir, nil, "cat-file", "blob", f[2]+":info.json").Output()
		var x EnCoursPC
		if err != nil || json.Unmarshal(b, &x) != nil || faits[x.Jalon] || apresPC(plusRecent[x.Jalon], x.Quand) {
			continue
		}
		v[nom] = f[2]
	}
	return v
}

// publierEnCoursPC: this computer's ref with these milestones (none: the ref leaves GitHub).
func (e *equipePC) publierEnCoursPC(entrees map[string]string, message string) error {
	if len(entrees) == 0 {
		if gmGH(e.miroir, nil, "rev-parse", "-q", "--verify", refsEnCoursPC+e.moi.ID).Run() != nil {
			return nil
		}
		if _, err := gitDistantGH(e.d, e.miroir, e.r, "push", "--porcelain", "--no-verify", "{url}", ":"+refDistantEnCoursPC+e.moi.ID); err != nil {
			return err
		}
		return gmGH(e.miroir, nil, "update-ref", "-d", refsEnCoursPC+e.moi.ID).Run()
	}
	var noms []string
	for n := range entrees {
		noms = append(noms, n)
	}
	sort.Strings(noms)
	var b strings.Builder
	for _, n := range noms {
		fmt.Fprintf(&b, "040000 tree %s\t%s\n", entrees[n], n)
	}
	c := gmGH(e.miroir, nil, "mktree")
	c.Stdin = strings.NewReader(b.String())
	out, err := c.Output()
	arbre := strings.TrimSpace(string(out))
	if err != nil || !reShaGH.MatchString(arbre) {
		return fmt.Errorf("mktree")
	}
	c = gmGH(e.miroir, []string{"GIT_AUTHOR_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com"}, "commit-tree", arbre, "-m", message)
	out, err = c.Output()
	commit := strings.TrimSpace(string(out))
	if err != nil || !reShaGH.MatchString(commit) {
		return fmt.Errorf("commit-tree")
	}
	if _, err := gitDistantGH(e.d, e.miroir, e.r, "push", "--porcelain", "--no-verify", "--force", "{url}",
		commit+":"+refDistantEnCoursPC+e.moi.ID); err != nil {
		return err
	}
	_ = gmGH(e.miroir, nil, "update-ref", refsEnCoursPC+e.moi.ID, commit).Run() // known here at once
	return nil
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
		id := strings.TrimPrefix(ref, refsEnCoursPC)
		if !reIDMachine.MatchString(id) {
			continue
		}
		out, err := gmGH(miroir, nil, "ls-tree", ref).Output()
		if err != nil {
			continue
		}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			meta, nom, ok := strings.Cut(l, "\t")
			f := strings.Fields(meta)
			if !ok || len(f) != 3 || f[1] != "tree" {
				continue
			}
			b, err := gmGH(miroir, nil, "cat-file", "blob", ref+":"+nom+"/info.json").Output()
			if err != nil || len(b) > 64<<10 {
				continue
			}
			var x EnCoursPC
			if json.Unmarshal(b, &x) != nil || x.PC != id || !reShaGH.MatchString(x.Base) || nom != refJalonPC(x.Jalon) {
				continue
			}
			x.Nom = coupeValide(x.Nom, 60)
			v = append(v, x)
		}
	}
	sort.SliceStable(v, func(a, b int) bool { return apresPC(v[a].Quand, v[b].Quand) })
	return v
}

// nettoyerEnCoursPC: this computer's works in progress on milestones the team has finished, removed from
// GitHub (those it let go stay: the computer that takes them next goes on from them, then removes them).
func (e *equipePC) nettoyerEnCoursPC() {
	menageIgnoresPC(dossierAvantEquipePC(e.d))
	if refs, err := sortieGH(gmGH(e.miroir, nil, "for-each-ref", "--format=%(refname)", refPortePC)); err == nil && refs != "" {
		faits := map[string]bool{}
		for id := range faitsDistantPC(e.miroir, e.r) {
			faits[refJalonPC(id)] = true
		}
		for _, r := range strings.Split(refs, "\n") {
			if faits[strings.TrimPrefix(r, refPortePC)] {
				_ = gmGH(e.miroir, nil, "update-ref", "-d", r).Run()
			}
		}
	}
	avant := 0
	for _, x := range lireEnCoursPC(e.miroir) {
		if x.PC == e.moi.ID {
			avant++
		}
	}
	if entrees := e.mesEntreesEnCoursPC(); len(entrees) != avant {
		_ = e.publierEnCoursPC(entrees, "Athanor: works in progress of "+e.moi.Nom)
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
// project, so that this one goes on from it. What Claude is told ("" when there was none). A work in
// progress may carry the one before it that its computer could not merge (avant/): it is merged next, or
// carried on in this computer's own, so that no computer's work is left behind along the way.
func (e *equipePC) reprendreEnCoursPC(j Jalon, ident []string) (string, string) {
	// the newest work in progress on it (a computer that went on from another's sends its own, newer, which
	// holds both); this computer's own the newest: nothing to bring in
	var x *EnCoursPC
	for _, y := range lireEnCoursPC(e.miroir) {
		if y.Jalon == j.ID && (x == nil || apresPC(y.Quand, x.Quand)) {
			y := y
			x = &y
		}
	}
	if x == nil || x.PC == e.moi.ID {
		return "", ""
	}
	arbre, err := sortieGH(gmGH(e.miroir, nil, "rev-parse", "-q", "--verify", refsEnCoursPC+x.PC+":"+refJalonPC(x.Jalon)))
	if err != nil || !reShaGH.MatchString(arbre) {
		return "", ""
	}
	var suites, msgs []string
	porte, fusionne := "", false
	for n := 0; arbre != "" && n < maxChainePC; n++ {
		info, ok := infoEnCoursPC(e.miroir, arbre)
		if !ok {
			break
		}
		suite, msg, ok := e.fusionnerEnCoursPC(j, info, arbre, ident)
		if suite != "" {
			suites = append(suites, suite)
		}
		if msg != "" {
			msgs = append(msgs, msg)
		}
		if !ok {
			porte = arbre // carried on in this computer's own work in progress, with what is before it
			break
		}
		fusionne = true
		suivant, err := sortieGH(gmGH(e.miroir, nil, "rev-parse", "-q", "--verify", arbre+":avant"))
		if err != nil || !reShaGH.MatchString(suivant) {
			break
		}
		arbre = suivant
	}
	if porte != "" {
		_ = gmGH(e.miroir, nil, "update-ref", refPortePC+refJalonPC(j.ID), porte).Run()
	}
	if fusionne || porte != "" {
		// this computer's own work in progress on it, sent now, is the newest from now on (it holds the
		// others', merged or carried on); the others' entries leave their refs when they see it
		_ = e.envoyerEnCoursPC(j.ID)
	}
	return strings.Join(suites, ""), strings.Join(msgs, " ")
}

const (
	refPortePC  = "refs/athanor/porte/" // (private folder) the work in progress of another computer not merged, carried on
	maxChainePC = 5
)

// infoEnCoursPC: the info.json of a work in progress (its tree).
func infoEnCoursPC(miroir, arbre string) (EnCoursPC, bool) {
	var x EnCoursPC
	b, err := gmGH(miroir, nil, "cat-file", "blob", arbre+":info.json").Output()
	if err != nil || len(b) > 64<<10 || json.Unmarshal(b, &x) != nil || !reShaGH.MatchString(x.Base) || !reIDMachine.MatchString(x.PC) {
		return x, false
	}
	x.Nom = coupeValide(x.Nom, 60)
	return x, true
}

// fusionnerEnCoursPC: one work in progress (its tree) merged into the project; ok false: not merged (what
// Claude and the log are told say why, and where it is).
func (e *equipePC) fusionnerEnCoursPC(j Jalon, x EnCoursPC, arbre string, ident []string) (string, string, bool) {
	patch, err := gmGH(e.miroir, nil, "cat-file", "blob", arbre+":patch.diff").Output()
	if err != nil {
		return "", "", false
	}
	if gTravailGH(e.projet, "cat-file", "-e", x.Base+"^{commit}").Run() != nil || !propreTravailPC(e.projet) {
		return "", fmt.Sprintf("Several computers: %s's unfinished work on %s could not be brought in (this folder has changes not committed, or lacks its base): this computer starts from the team's last push, and passes that work on.", x.Nom, j.ID), false
	}
	// the patch applied to its base in an index of its own, then merged like the others' work
	index, err := indexTemporairePC(e.projet)
	if err != nil {
		return "", "", false
	}
	defer os.Remove(index)
	if _, err := gIndexPC(e.projet, index, "read-tree", x.Base); err != nil {
		return "", "", false
	}
	c := gTravailGH(e.projet, "apply", "--cached", "--binary", "--whitespace=nowarn")
	c.Env = append(c.Env, "GIT_INDEX_FILE="+index)
	c.Stdin = bytes.NewReader(patch)
	if out, err := c.CombinedOutput(); err != nil {
		return "", fmt.Sprintf("Several computers: %s's unfinished work on %s does not apply (%s): this computer starts from the team's last push, and passes that work on.", x.Nom, j.ID, derniereLigneSortie(string(out))), false
	}
	t, err := gIndexPC(e.projet, index, "write-tree")
	if err != nil {
		return "", "", false
	}
	cc := gTravailGH(e.projet, "commit-tree", strings.TrimSpace(string(t)), "-p", x.Base, "-m", "Work in progress of "+j.ID+" by "+x.Nom)
	cc.Env = append(cc.Env, "GIT_AUTHOR_NAME="+firstNonEmpty(x.Nom, "Athanor"), "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com")
	out, err := cc.Output()
	commit := strings.TrimSpace(string(out))
	if err != nil || !reShaGH.MatchString(commit) {
		return "", "", false
	}
	_ = gTravailGH(e.projet, "update-ref", "refs/athanor/repris/"+refJalonPC(j.ID)+"-"+x.PC, commit).Run() // kept, whatever happens
	avant, _ := gProjetGH(e.projet+"/.git", "rev-parse", "HEAD")
	args := append(append([]string{}, ident...), "merge", "--no-edit", "-m", "Go on with "+j.ID+" from the work in progress of "+x.Nom, commit)
	if err := gTravailGH(e.projet, args...).Run(); err != nil {
		_ = gTravailGH(e.projet, "merge", "--abort").Run()
		if apres, _ := gProjetGH(e.projet+"/.git", "rev-parse", "HEAD"); apres != avant || !propreTravailPC(e.projet) {
			_ = gTravailGH(e.projet, "reset", "--hard", "--quiet", avant).Run()
		}
		return fmt.Sprintf(" %s started this milestone and could not finish it; its unfinished work is in commit %s (`git show %s`), which does not merge by itself with this folder: take from it what helps.", x.Nom, commit[:12], commit[:12]),
			fmt.Sprintf("Several computers: %s's unfinished work on %s does not merge by itself: Claude takes from it what helps, and it is passed on.", x.Nom, j.ID), false
	}
	return fmt.Sprintf(" %s started this milestone and could not finish it: its unfinished work is merged into this folder (`Go on with %s…` commit). Check what is done and what is left, then finish it.", x.Nom, j.ID),
		fmt.Sprintf("Several computers: this computer goes on with %s from %s's unfinished work.", j.ID, x.Nom), true
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
			if x.Jalon == actuel && x.PC != e.moi.ID && apresPC(x.Quand, depuis) {
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
	// files git ignores here but the team's version has: reset would overwrite them, and they are in no
	// commit: copied aside first (Athanor's data for the project)
	copies := e.copierIgnoresPC(local)
	_ = gTravailGH(e.projet, "reset", "--hard", "--quiet", local).Run()
	msg += fmt.Sprintf(" Its own unfinished work on it is kept in %s (commit %s).", garde, snap[:12])
	if copies != "" {
		msg += " Its files ignored by git that the team's version replaces are copied in " + copies + "."
	}
	return msg, true
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

// copierIgnoresPC: the ignored files of the folder that the team's branch has too, with another content,
// copied into Athanor's data for the project (where, "" if none).
func (e *equipePC) copierIgnoresPC(local string) string {
	ign, err := gTravailGH(e.projet, "ls-files", "-z", "--others", "--ignored", "--exclude-standard").Output()
	if err != nil || len(ign) == 0 {
		return ""
	}
	leurs, err := gTravailGH(e.projet, "ls-tree", "-r", "-z", "--name-only", local).Output()
	if err != nil {
		return ""
	}
	dansLeur := map[string]bool{}
	for _, f := range strings.Split(string(leurs), "\x00") {
		dansLeur[f] = f != ""
	}
	dest := ""
	for _, f := range strings.Split(string(ign), "\x00") {
		if !dansLeur[f] {
			continue
		}
		src := filepath.Join(e.projet, filepath.FromSlash(f))
		st, err := os.Lstat(src)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		leur, _ := gProjetGH(e.projet+"/.git", "rev-parse", "-q", "--verify", local+":"+f)
		ici, _ := gTravailGH(e.projet, "hash-object", "--", f).Output()
		if strings.TrimSpace(string(ici)) == leur {
			continue // the same: nothing lost
		}
		if dest == "" {
			menageIgnoresPC(dossierAvantEquipePC(e.d))
			dest = filepath.Join(dossierAvantEquipePC(e.d), "ignores-"+time.Now().Format("20060102-150405"))
		}
		b, err := os.ReadFile(src)
		cible := filepath.Join(dest, filepath.FromSlash(f))
		if err == nil && os.MkdirAll(filepath.Dir(cible), 0o700) == nil {
			_ = os.WriteFile(cible, b, 0o600)
		}
	}
	return dest
}

// menageIgnoresPC: the copies of ignored files older than gardeIgnoresPC leave Athanor's data (the
// gardeMinIgnoresPC most recent always stay).
func menageIgnoresPC(dossier string) {
	l, _ := filepath.Glob(filepath.Join(dossier, "ignores-*"))
	sort.Strings(l) // "ignores-<date>": the oldest first
	for i, f := range l {
		if len(l)-i <= gardeMinIgnoresPC {
			break
		}
		if st, err := os.Stat(f); err == nil && st.IsDir() && time.Since(st.ModTime()) > gardeIgnoresPC {
			_ = os.RemoveAll(f)
		}
	}
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
