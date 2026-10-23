package main

// athanor github …: the parts of system/github.sh that were Python (same outputs, same files):
//   athanor github scanner messages|diff     (stdin: git log) the secrets of the commits about to be
//                                            pushed: "place<TAB>kind" lines, never a value, then #FIN#;
//                                            exit 3 when the diff cannot be read to the end
//   athanor github atelier PROJECT NAME ecrire|effacer   a file of the project's workshop written (stdin)
//                                            or deleted without following a link Claude could have put there
//   athanor github crochet PROJECT 0|1      the commit-msg hook that removes Claude's co-author lines

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Python's \b, for a pattern that starts (or ends) with a word character: no word character before (after).
const avantMot, apresMot = `(?:^|[^\p{L}\p{N}_])`, `(?:$|[^\p{L}\p{N}_])`

var reglesSecrets = []struct {
	genre string
	re    *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN (?:RSA |DSA |EC |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY`)},
	{"AWS key", regexp.MustCompile(avantMot + `(?:AKIA|ASIA)[0-9A-Z]{16}` + apresMot)},
	{"GitHub token", regexp.MustCompile(avantMot + `(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})`)},
	{"Anthropic key", regexp.MustCompile(avantMot + `sk-ant-[A-Za-z0-9_-]{20,}`)},
	{"OpenAI key", regexp.MustCompile(avantMot + `sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}T3BlbkFJ[A-Za-z0-9_-]{20,}|` + avantMot + `sk-[A-Za-z0-9]{48}` + apresMot)},
	{"Slack token", regexp.MustCompile(avantMot + `xox[abposr]-[A-Za-z0-9-]{10,}`)},
	// (a last character that may be "-": Python's \b after it needs a word character next)
	{"Google API key", regexp.MustCompile(avantMot + `AIza[0-9A-Za-z_-]{34}(?:[0-9A-Za-z_]` + apresMot + `|-[\p{L}\p{N}_])`)},
	{"GitLab token", regexp.MustCompile(avantMot + `glpat-[A-Za-z0-9_-]{20,}`)},
	{"Stripe key", regexp.MustCompile(avantMot + `(?:sk|rk)_live_[0-9A-Za-z]{20,}`)},
	{"npm token", regexp.MustCompile(avantMot + `npm_[A-Za-z0-9]{36}` + apresMot)},
	{"Hugging Face token", regexp.MustCompile(avantMot + `hf_[A-Za-z0-9]{34,}` + apresMot)},
	{"Discord token", regexp.MustCompile(avantMot + `[MN][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{26,}(?:[A-Za-z0-9_]` + apresMot + `|-[\p{L}\p{N}_])`)},
}

var (
	reFichierEnv    = regexp.MustCompile(`(?:^|/)\.env(?:\.([^/]+))?$`)
	envPermis       = map[string]bool{"example": true, "sample": true, "template": true, "dist": true, "default": true, "defaults": true}
	reCleFichier    = regexp.MustCompile(`(?:^|/)(?:id_rsa|id_dsa|id_ecdsa|id_ed25519)$|\.(?:pem|p12|pfx|keystore|jks)$`)
	reAtelierPublie = regexp.MustCompile(`(?:^|/)\.athanor(?:/|$)|^\.claude(?:/|$)`)
	reWorkflow      = regexp.MustCompile(`^\.github/(?:workflows|actions)/`)
	reEnteteHunk    = regexp.MustCompile(`^@@ -[0-9]+(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)
)

func genreSecret(texte string) string {
	for _, r := range reglesSecrets {
		if r.re.MatchString(texte) {
			return r.genre
		}
	}
	return ""
}

func genresFichier(f string) []string {
	var g []string
	if m := reFichierEnv.FindStringSubmatch(f); m != nil && !envPermis[m[1]] {
		g = append(g, "environment file")
	}
	if reCleFichier.MatchString(f) {
		g = append(g, "private key file")
	}
	if reAtelierPublie.MatchString(f) {
		g = append(g, "Athanor workshop")
	}
	if reWorkflow.MatchString(f) {
		g = append(g, "GitHub Actions workflow")
	}
	return g
}

// cheminDiff: a "+++ b/PATH" path, unquoted as git quotes it ("…" with C escapes).
func cheminDiff(brut string) string {
	if len(brut) > 1 && brut[0] == '"' && brut[len(brut)-1] == '"' {
		brut = decoderUTF8Python(decoderEchappementsC(brut[1 : len(brut)-1]))
	}
	return strings.TrimPrefix(brut, "b/")
}

// decoderEchappementsC: codecs.escape_decode (\n \t \" \\ \ooo \xhh…).
func decoderEchappementsC(s string) []byte {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out = append(out, s[i])
			continue
		}
		i++
		switch c := s[i]; c {
		case '\n':
		case '\\', '\'', '"':
			out = append(out, c)
		case 'a':
			out = append(out, 7)
		case 'b':
			out = append(out, 8)
		case 'f':
			out = append(out, 12)
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, 11)
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					out = append(out, byte(v))
					i += 2
					continue
				}
			}
			out = append(out, '\\', c)
		default:
			if c >= '0' && c <= '7' {
				j := i
				for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
					j++
				}
				v, _ := strconv.ParseUint(s[i:j], 8, 16)
				out = append(out, byte(v))
				i = j - 1
				continue
			}
			out = append(out, '\\', c)
		}
	}
	return out
}

// scannerSecrets: github.sh's scanner; the code of exit (0, 3 when the diff cannot be trusted).
func scannerSecrets(mode string, entree io.Reader, sortie io.Writer) int {
	vus := map[string]bool{}
	ajouter := func(cle string) {
		if !vus[cle] {
			vus[cle] = true
			fmt.Fprintln(sortie, cle)
		}
	}
	commit, fichier, ligne, resteA, resteB := "", "", 0, 0, 0
	aFichier := false
	r := bufio.NewReaderSize(entree, 1<<20)
	for {
		brut, err := r.ReadBytes('\n')
		if len(brut) == 0 && err != nil {
			break
		}
		l := strings.TrimRight(decoderUTF8Python(brut), "\n")
		if mode == "messages" {
			if strings.HasPrefix(l, "\x01") {
				rr := []rune(l[1:])
				commit = string(rr[:min(12, len(rr))])
				continue
			}
			if g := genreSecret(l); g != "" {
				ajouter(fmt.Sprintf("commit %s (message)\t%s", commit, g))
			}
			continue
		}
		if resteA > 0 || resteB > 0 { // inside a hunk: its header gave the exact number of lines
			c := ""
			if l != "" {
				c = l[:1]
			}
			switch c {
			case "+":
				resteB--
				if g := genreSecret(l); g != "" && aFichier && fichier != "" {
					ajouter(fmt.Sprintf("%s:%d\t%s", fichier, ligne, g))
				}
				ligne++
				continue
			case "-":
				resteA--
				continue
			case " ":
				resteA--
				resteB--
				ligne++
				continue
			case "\\":
				continue
			}
			return 3 // not a diff line where one is expected: never trust the rest
		}
		if strings.HasPrefix(l, "\x01") || strings.HasPrefix(l, "diff --git ") || strings.HasPrefix(l, "diff --cc ") {
			fichier, aFichier = "", false
			continue
		}
		if strings.HasPrefix(l, "+++ ") {
			if l == "+++ /dev/null" {
				fichier, aFichier = "", false
			} else {
				fichier, aFichier = cheminDiff(l[4:]), true
				if fichier != "" {
					for _, g := range genresFichier(fichier) {
						ajouter(fichier + "\t" + g)
					}
				}
			}
			continue
		}
		if m := reEnteteHunk.FindStringSubmatch(l); m != nil {
			resteA, resteB = 1, 1
			if m[1] != "" {
				resteA, _ = strconv.Atoi(m[1])
			}
			if m[3] != "" {
				resteB, _ = strconv.Atoi(m[3])
			}
			ligne, _ = strconv.Atoi(m[2])
			continue
		}
	}
	if resteA > 0 || resteB > 0 {
		return 3
	}
	fmt.Fprintln(sortie, "#FIN#")
	return 0
}

const marqueCrochet = "# athanor: commits without Claude as co-author"

var texteCrochet = []byte("#!/bin/sh\n" + marqueCrochet + " (option of the GitHub block, Manage page)\n" +
	`sed -i -e "/^[Cc]o-[Aa]uthored-[Bb]y:.*[Cc]laude/d" -e "/Generated with \[Claude Code\]/d" -e "/^.\{0,4\}Generated with Claude/d" "$1"` + "\n")

func crochetAthanor(actuel []byte) bool { return bytes.Contains(actuel, []byte(marqueCrochet)) }

func lancerGitHub(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: athanor github scanner messages|diff | atelier PROJECT NAME ecrire|effacer | crochet PROJECT 0|1")
		return 2
	}
	switch args[0] {
	case "scanner":
		if len(args) < 2 {
			return 2
		}
		w := bufio.NewWriter(os.Stdout)
		code := scannerSecrets(args[1], os.Stdin, w)
		w.Flush()
		return code
	case "atelier":
		if len(args) < 4 {
			return 2
		}
		if err := fichierAtelier(args[1], args[2], args[3] == "effacer", os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "crochet":
		if len(args) < 2 {
			return 2
		}
		actif := len(args) > 2 && args[2] == "1"
		if err := crochetCommit(args[1], actif); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	return 2
}
