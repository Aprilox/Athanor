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

var (
	reFiltreGit = regexp.MustCompile(`^filter\.(.*)\.[^.]+$`)
	reDriverGit = regexp.MustCompile(`^merge\.(.+)\.driver$`) // a merge driver ("merge.NAME.driver")
)

// optionsSuresGit: what makes git on the project run nothing the repository's configuration asks for: the
// fixed options, then its filters made to do nothing and its merge drivers made to fail (a conflict, never
// a command nor one side silently kept). env: the environment git reads the configuration with.
func optionsSuresGit(dossier string, env []string) []string {
	a := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "diff.external=", "-c", "core.pager=cat",
		"-c", "core.editor=true", "-c", "sequence.editor=true", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.sshCommand=false",
		"-c", "protocol.allow=never", "-c", "credential.helper=", "-c", "merge.verifySignatures=false", "-c", "gpg.program=false",
		"-c", "gpg.ssh.program=false", "-c", "gpg.x509.program=false", "-c", "submodule.recurse=false"}
	c := commande("git", "-C", dossier, "config", "--name-only", "--get-regexp", `^(filter|merge)\.`)
	c.Env = env
	out, err := c.Output()
	if err != nil {
		return a
	}
	var filtres, drivers []string
	vus := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if m := reFiltreGit.FindStringSubmatch(l); m != nil && !vus["f"+m[1]] {
			vus["f"+m[1]] = true
			filtres = append(filtres, m[1])
		} else if m := reDriverGit.FindStringSubmatch(l); m != nil && !vus["m"+m[1]] {
			vus["m"+m[1]] = true
			drivers = append(drivers, m[1])
		}
	}
	sort.Strings(filtres)
	sort.Strings(drivers)
	for _, n := range filtres {
		a = append(a, "-c", "filter."+n+".clean=cat", "-c", "filter."+n+".smudge=cat", "-c", "filter."+n+".process=", "-c", "filter."+n+".required=false")
	}
	for _, n := range drivers {
		a = append(a, "-c", "merge."+n+".driver=false")
	}
	return a
}

func gitP(dossier string, env []string, args ...string) *exec.Cmd {
	a := append([]string{"-C", dossier}, optionsSuresGit(dossier, nil)...)
	c := commande("git", append(a, args...)...)
	c.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	cacherFenetre(c)
	return c
}

func gitSortie(dossier string, args ...string) (string, error) {
	out, err := gitP(dossier, nil, args...).Output()
	return strings.TrimRight(string(out), "\n"), err
}
