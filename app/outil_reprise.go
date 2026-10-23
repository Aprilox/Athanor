package main

// athanor reprise PROJECT PRIVATE_DIR: at the pilot's start, what a session stopped in the middle, or a
// project brought from another PC (or another folder), leaves behind. What is safe is repaired here (a git
// lock left by a stop: the pilot holds the project's lock, no Claude runs now); the rest is written for
// Claude in .athanor/RESUME.md (CLAUDE.md: handled before any new work); a project that comes from
// elsewhere gets its milestones checked against the folder (.athanor/REVERIFY). One line per thing done,
// for the pilot's log.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var reCheminAutrePC = regexp.MustCompile("(?:/mnt/[a-z]/|/home/[^/\\s\"'`]+/|/Users/[^/\\s\"'`]+/|[A-Za-z]:[\\\\/])[^\\s\"'`;|&<>)]*")

// cheminIci: an absolute path as this system sees it (C:\x -> /mnt/c/x under WSL).
func cheminIci(p string) string {
	if len(p) > 2 && p[1] == ':' && runtimeWindows() {
		return p
	}
	if len(p) > 2 && p[1] == ':' {
		return "/mnt/" + strings.ToLower(p[:1]) + strings.ReplaceAll(p[2:], `\`, "/")
	}
	return p
}

// gitignoreAthanor: the project's .gitignore only changed by the pilot itself (its "Local secrets" block,
// pilot.sh): not work left by a session.
func gitignoreAthanor(projet string, nouveau bool) bool {
	var ajouts []string
	if nouveau {
		b, err := lireOctets(filepath.Join(projet, ".gitignore"), 1<<20)
		if err != nil {
			return false
		}
		ajouts = strings.Split(string(b), "\n")
	} else {
		out, err := gitP(projet, nil, "diff", "--no-color", "--no-ext-diff", "-U0", "--", ".gitignore").Output()
		if err != nil {
			return false
		}
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") && strings.TrimSpace(l[1:]) != "" {
				return false
			}
			if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
				ajouts = append(ajouts, l[1:])
			}
		}
	}
	permis := map[string]bool{"# Local secrets (added by Athanor)": true, ".env": true, ".env.*": true, "!.env.example": true, "!.env.sample": true, "!.env.template": true, "!.env.dist": true}
	vu := false
	for _, l := range ajouts {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !permis[l] {
			return false
		}
		vu = true
	}
	return vu
}

func reprendreProjet(projet, prive string) (lignes []string, notes []string) {
	gd := filepath.Join(projet, ".git")
	if st, err := os.Lstat(gd); err == nil && st.IsDir() {
		// git locks left by a stop in the middle: every later commit would fail on them
		var verrous []string
		for _, f := range []string{"index.lock", "HEAD.lock", "config.lock", "packed-refs.lock", "shallow.lock"} {
			verrous = append(verrous, filepath.Join(gd, f))
		}
		_ = filepath.WalkDir(filepath.Join(gd, "refs"), func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && strings.HasSuffix(p, ".lock") {
				verrous = append(verrous, p)
			}
			return nil
		})
		for _, f := range verrous {
			st, err := os.Lstat(f)
			if err != nil || !st.Mode().IsRegular() || time.Since(st.ModTime()) < 2*time.Minute {
				continue
			}
			if os.Remove(f) == nil {
				r, _ := filepath.Rel(projet, f)
				lignes = append(lignes, "Repaired: a git lock left by a stop in the middle was removed ("+filepath.ToSlash(r)+").")
			}
		}
		// git operations left in the middle
		for _, x := range []struct{ f, quoi string }{{"MERGE_HEAD", "a merge"}, {"rebase-merge", "a rebase"}, {"rebase-apply", "a rebase"},
			{"CHERRY_PICK_HEAD", "a cherry-pick"}, {"REVERT_HEAD", "a revert"}} {
			if _, err := os.Lstat(filepath.Join(gd, x.f)); err == nil {
				notes = append(notes, fmt.Sprintf("Git: %s was left in the middle (`git status` shows it). Finish it (resolve, commit) or abort it, according to what the milestone needs.", x.quoi))
				break
			}
		}
		// work not committed yet
		if out, err := gitP(projet, nil, "status", "--porcelain", "--untracked-files=normal").Output(); err == nil {
			var l []string
			for _, x := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				chemin := strings.TrimSpace(x)
				if len(chemin) > 3 {
					chemin = strings.Trim(strings.TrimSpace(x[3:]), `"`)
				}
				if strings.TrimSpace(x) != "" && !strings.HasPrefix(chemin, ".athanor/") && !strings.HasPrefix(chemin, ".claude/") &&
					!(chemin == ".gitignore" && gitignoreAthanor(projet, strings.HasPrefix(x, "??"))) {
					l = append(l, x)
				}
			}
			if len(l) > 0 {
				liste := l
				if len(liste) > 15 {
					liste = liste[:15]
				}
				suite := ""
				if len(l) > 15 {
					suite = fmt.Sprintf(" (and %d more)", len(l)-15)
				}
				notes = append(notes, "The previous session stopped before committing: these files hold work not committed yet: `"+
					strings.Join(liste, "`, `")+"`"+suite+". Read them (`git status`, `git diff`) and finish the milestone from them, keeping what is good; never throw them away unread.")
			}
		}
	}
	// paths of another PC (or of the project's former folder) in the check
	if b, err := lireOctets(filepath.Join(projet, ".athanor", "check.sh"), 1<<20); err == nil {
		vus := map[string]bool{}
		var ailleurs []string
		for _, p := range reCheminAutrePC.FindAllString(string(b), -1) {
			p = strings.TrimRight(p, ".,:")
			if vus[p] {
				continue
			}
			vus[p] = true
			ici := cheminIci(p)
			if strings.HasPrefix(strings.ToLower(filepath.ToSlash(ici)), strings.ToLower(filepath.ToSlash(projet))) {
				continue
			}
			if _, err := os.Stat(ici); err == nil {
				continue
			}
			ailleurs = append(ailleurs, p)
		}
		if len(ailleurs) > 0 {
			if len(ailleurs) > 10 {
				ailleurs = ailleurs[:10]
			}
			notes = append(notes, "`.athanor/check.sh` names paths that do not exist on this PC (another PC, or the project's former folder): `"+
				strings.Join(ailleurs, "`, `")+"`. Make them relative to the project (or this PC's own paths), then run it.")
		}
	}
	// a project that comes from elsewhere: no cycle of this pilot yet, but a plan already under way
	if _, err := os.Stat(filepath.Join(prive, "cycle")); os.IsNotExist(err) {
		if b, err := os.ReadFile(filepath.Join(projet, ".athanor", "PLAN.md")); err == nil {
			coches := len(regexp.MustCompile(`(?m)^- \[[xX]\] `).FindAll(b, -1))
			total := len(regexp.MustCompile(`(?m)^- \[[ xX]\] `).FindAll(b, -1))
			if coches > 0 {
				lignes = append(lignes, fmt.Sprintf("This project comes from another PC or another folder: Athanor carries on from its plan (%d of %d milestones ticked); Claude first checks them against the folder.", coches, total))
				if _, err := os.Stat(filepath.Join(projet, ".athanor", "REVERIFY")); os.IsNotExist(err) {
					_ = fichierAtelier(projet, "REVERIFY", false, strings.NewReader(time.Now().Format("2006-01-02 15:04")+": the project comes from another PC or another folder (its pilot's history is not on this PC)\n"))
				}
			}
		}
	}
	if len(notes) == 0 {
		return lignes, nil
	}
	texte := "# Resuming an interrupted project\n\nAt its start, the pilot found signs that the previous session stopped in the middle, or that the project ran on another PC. Before any new work:\n\n- " +
		strings.Join(notes, "\n- ") + "\n\nThen note it in `.athanor/LOG.md`, delete this file, and carry on with the plan where it is (never start it over).\n"
	if fichierAtelier(projet, "RESUME.md", false, strings.NewReader(texte)) == nil {
		lignes = append(lignes, "Interrupted work found (see .athanor/RESUME.md): Claude handles it first, then carries on with the plan.")
	}
	return lignes, notes
}

func lancerReprise(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor reprise PROJECT PRIVATE_DIR")
		return 2
	}
	lignes, _ := reprendreProjet(args[0], args[1])
	for _, l := range lignes {
		fmt.Println(l)
	}
	return 0
}
