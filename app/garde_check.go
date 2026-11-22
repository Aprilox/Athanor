package main

// The full check run again on a project that has not changed since it passed: about a third of Claude's full
// checks (measured on 120 cycles of three projects with 3.1.6), each one minutes of waiting for the same result.
// The Bash hook refuses that run; any change of the project (its commit, a file changed or added, check.sh) lets
// the next one through, and a check that failed can always be run again.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// reCheckComplet: a command that runs .athanor/check.sh (group 1: its quick mode).
var reCheckComplet = regexp.MustCompile(`(?:^|[\s;&|(])(?:bash|sh)\s+(?:\./)?\.athanor/check\.sh(\s+quick\b)?`)

// reSortieCheck: the file the check's output goes to (> file, >> file, &> file, | tee file).
var reSortieCheck = regexp.MustCompile(`\.athanor/check\.sh[^;&|>]*(?:&?>{1,2}|\|\s*tee(?:\s+-a)?)\s*["']?([^\s"';&|]+)`)

// passageCheck: the last full check started on the project, and on which state of it.
type passageCheck struct {
	Empreinte string `json:"empreinte"`
	Journal   string `json:"journal"`
	Debut     int64  `json:"debut"`
}

// gardeCheckComplet: the refusal of a full check that already passed on this exact state of the project; otherwise
// the run is noted (.athanor/.verif/athanor-full-check.json) and goes through.
func gardeCheckComplet(commande string, e map[string]any) string {
	complet := false
	for _, m := range reCheckComplet.FindAllStringSubmatch(commande, -1) {
		complet = complet || m[1] == ""
	}
	if !complet {
		return ""
	}
	projet := os.Getenv("PROJECT")
	if projet == "" {
		projet, _ = e["cwd"].(string)
	}
	empreinte := empreinteProjet(projet)
	if empreinte == "" {
		return ""
	}
	f := filepath.Join(projet, ".athanor", ".verif", "athanor-full-check.json")
	var avant passageCheck
	if b, err := lireBorne(f, 64<<10); err == nil && json.Unmarshal(b, &avant) == nil && avant.Empreinte == empreinte && checkPasse(avant) {
		return fmt.Sprintf("Refused by the pilot: the full check already passed on this exact state of the project "+
			"(%s, started at %s) and nothing changed since: running it again would give the same result. Go on "+
			"(tick, log, commit). After any change of the project or of check.sh, run it again.",
			avant.Journal, time.Unix(avant.Debut, 0).Format("15:04"))
	}
	journal := ""
	if m := reSortieCheck.FindStringSubmatch(commande); m != nil {
		if journal = os.ExpandEnv(m[1]); !filepath.IsAbs(journal) {
			journal = filepath.Join(projet, journal)
		}
	}
	if estLien(filepath.Join(projet, ".athanor")) || estLien(filepath.Dir(f)) {
		return "" // never written through a link placed there
	}
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	b, _ := json.Marshal(passageCheck{Empreinte: empreinte, Journal: journal, Debut: time.Now().Unix()})
	_ = publierFichier(f, b)
	return ""
}

// checkPasse: the noted run wrote its CHECK: PASSED (and no CHECK: FAILED) into its output file, after it started.
func checkPasse(p passageCheck) bool {
	if p.Journal == "" {
		return false
	}
	st, err := os.Lstat(p.Journal)
	if err != nil || !st.Mode().IsRegular() || st.ModTime().Unix() < p.Debut {
		return false
	}
	b, err := lireBorne(p.Journal, 20<<20)
	return err == nil && bytes.Contains(b, []byte("CHECK: PASSED")) && !bytes.Contains(b, []byte("CHECK: FAILED"))
}

// empreinteProjet: the state of the project a check sees: its commit, every file changed or added since (size and
// time), and check.sh. "" when git cannot tell.
func empreinteProjet(projet string) string {
	if projet == "" {
		return ""
	}
	tete, err := gitSortie(projet, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	// the workshop apart (its outputs change at each run), even where .git/info/exclude does not say it; check.sh below
	etat, err := gitP(projet, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".", ":(exclude).athanor").Output()
	if err != nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s", tete, etat)
	for _, l := range strings.Split(string(etat), "\x00") {
		if len(l) > 3 {
			ajouterEmpreinte(h, filepath.Join(projet, l[3:]))
		}
	}
	ajouterEmpreinte(h, filepath.Join(projet, ".athanor", "check.sh"))
	return hex.EncodeToString(h.Sum(nil))
}

func ajouterEmpreinte(h hash.Hash, f string) {
	if st, err := os.Lstat(f); err == nil {
		fmt.Fprintf(h, "\n%s %d %d", f, st.Size(), st.ModTime().UnixNano())
	}
}
