package main

// The workshop shared on GitHub (an option of the project's GitHub link, off by default): what lets
// someone else take the project over (a friend who clones it, then goes on with their Athanor) goes into
// the commits; what belongs to this PC (the values given to Claude, the raw logs, the pilot's state, the
// workers' copies…) never does. Off: .athanor/ stays out of git altogether.
//
// Both are said in the project's .git/info/exclude (local, never pushed), in a block of Athanor's own.

import (
	"os"
	"path/filepath"
	"strings"
)

// atelierPartage: the files and folders of .athanor/ the shared workshop holds.
var atelierPartage = []string{"REQUEST.md", "PLAN.md", "LOG.md", "check.sh", "SOLUTIONS.md", "BUGS.md", "BLOCKERS.md",
	"DECISIONS.md", "FINAL_REPORT.md", "TEST_GUIDE.md", "docs/", "queue/", "done/", "archives/", "agents/", "attachments/request/"}

// atelierPartageable: a path of the repository (".athanor/PLAN.md") the shared workshop may publish.
func atelierPartageable(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if !strings.HasPrefix(rel, ".athanor/") {
		return false
	}
	r := strings.TrimPrefix(rel, ".athanor/")
	for _, a := range atelierPartage {
		if strings.HasSuffix(a, "/") {
			if strings.HasPrefix(r, a) && !strings.Contains(r, "/../") {
				return true
			}
		} else if r == a {
			return true
		}
	}
	return false
}

const enteteExclusionAtelier = "# Athanor: its workshop (.athanor/)"

// lignesExclusionAtelier: Athanor's block of .git/info/exclude.
func lignesExclusionAtelier(partage bool) []string {
	if !partage {
		return []string{enteteExclusionAtelier + " kept out of git", ".athanor/"}
	}
	l := []string{enteteExclusionAtelier + " shared on GitHub, its files of this PC kept out", ".athanor/*"}
	for _, a := range atelierPartage {
		if a == "attachments/request/" {
			l = append(l, "!.athanor/attachments/", ".athanor/attachments/*")
		}
		l = append(l, "!.athanor/"+a)
	}
	return l
}

// ecrireExclusionAtelier: the project's .git/info/exclude with Athanor's block for that choice (the other
// lines as they were).
func ecrireExclusionAtelier(projet string, partage bool) error {
	// a git repository of its own (the pilot makes it): never a .git made here
	if st, err := os.Lstat(filepath.Join(projet, ".git", "HEAD")); err != nil || !st.Mode().IsRegular() || estLien(filepath.Join(projet, ".git")) {
		return nil
	}
	f := filepath.Join(projet, ".git", "info", "exclude")
	if estLien(f) || estLien(filepath.Dir(f)) {
		return os.ErrPermission
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, _ := os.ReadFile(f)
	var garde []string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, enteteExclusionAtelier) || strings.HasPrefix(t, ".athanor/") || strings.HasPrefix(t, "!.athanor/") || t == ".athanor" {
			continue
		}
		garde = append(garde, l)
	}
	for len(garde) > 0 && strings.TrimSpace(garde[len(garde)-1]) == "" {
		garde = garde[:len(garde)-1]
	}
	texte := strings.Join(append(garde, lignesExclusionAtelier(partage)...), "\n") + "\n"
	if texte == string(b) {
		return nil
	}
	return publierFichier(f, []byte(texte))
}
