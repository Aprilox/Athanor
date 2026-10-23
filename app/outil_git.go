package main

// gitP: system/safety.sh's g: git on a project with nothing executable coming from the repository (its
// filters, hooks, fsmonitor, external diff, pager, editor, signing, ssh, credentials are all neutralised),
// since Claude can write the project's .git/config.

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var reFiltreGit = regexp.MustCompile(`^filter\.(.*)\.[^.]+$`)

func gitP(dossier string, env []string, args ...string) *exec.Cmd {
	var filtres []string
	if out, err := exec.Command("git", "-C", dossier, "config", "--name-only", "--get-regexp", `^filter\.`).Output(); err == nil {
		vus := map[string]bool{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if m := reFiltreGit.FindStringSubmatch(l); m != nil && !vus[m[1]] {
				vus[m[1]] = true
				filtres = append(filtres, m[1])
			}
		}
		sort.Strings(filtres)
	}
	a := []string{"-C", dossier, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "diff.external=", "-c", "core.pager=cat",
		"-c", "core.editor=true", "-c", "sequence.editor=true", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.sshCommand=false",
		"-c", "protocol.allow=never", "-c", "credential.helper="}
	for _, n := range filtres {
		a = append(a, "-c", "filter."+n+".clean=cat", "-c", "filter."+n+".smudge=cat", "-c", "filter."+n+".process=", "-c", "filter."+n+".required=false")
	}
	c := exec.Command("git", append(a, args...)...)
	c.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	cacherFenetre(c)
	return c
}

func gitSortie(dossier string, args ...string) (string, error) {
	out, err := gitP(dossier, nil, args...).Output()
	return strings.TrimRight(string(out), "\n"), err
}
