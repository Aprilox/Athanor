package main

// The team's workshop on GitHub, encrypted (2.46): when the master cannot be reached, the nodes go on
// through a copy of the team's workshop on GitHub that only the team can read, and the master merges it
// with its own each time it can. It is one ref of the repository, refs/athanor/atelier (not a branch:
// never in the list of branches, no workflow runs for it): a commit whose tree holds
//   manifeste        the version (as the master's store has it) encrypted with the team's key
//   o-<name>         each file's content encrypted, under a name derived from the key (GitHub sees neither
//                    the paths, nor the contents, nor whether two projects hold the same file)
// The same content always encrypts the same way (its nonce derived from it), so a change only sends the
// files that changed. A write is taken with git push --force-with-lease: two computers writing at the same
// moment, the second is refused and merges again (errConflitAtelier), as with the master's store.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	refAtelierGH   = "refs/athanor/atelier"    // on GitHub
	refAtelierLuGH = "refs/athanor/atelier-lu" // in the private folder: the one read last
)

// clesAtelierGH: the team's key, as the two keys of the workshop on GitHub (encryption, names).
func clesAtelierGH(cle string) (cipher.AEAD, []byte, error) {
	k, err := base64.StdEncoding.DecodeString(cle)
	if err != nil || len(k) != 32 {
		return nil, nil, errors.New("the team's key is unreadable")
	}
	h := sha256.Sum256(append([]byte("athanor workshop: encryption\x00"), k...))
	n := sha256.Sum256(append([]byte("athanor workshop: names\x00"), k...))
	b, _ := aes.NewCipher(h[:])
	g, _ := cipher.NewGCM(b)
	return g, n[:], nil
}

func hmacAtelier(cle []byte, x []byte) []byte {
	m := hmac.New(sha256.New, cle)
	m.Write(x)
	return m.Sum(nil)
}

// chiffrerAtelierGH: the same content and key, the same bytes (its nonce taken from them).
func chiffrerAtelierGH(g cipher.AEAD, noms []byte, b []byte) []byte {
	nonce := hmacAtelier(noms, append([]byte("nonce\x00"), b...))[:g.NonceSize()]
	return g.Seal(append([]byte{}, nonce...), nonce, b, nil)
}

func dechiffrerAtelierGH(g cipher.AEAD, b []byte) ([]byte, error) {
	if len(b) < g.NonceSize() {
		return nil, errors.New("too short")
	}
	return g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
}

// nomObjetAtelierGH: a content's name in the tree (from its sha256 and the key: nothing of it shows).
func nomObjetAtelierGH(noms []byte, sha string) string {
	return "o-" + hex.EncodeToString(hmacAtelier(noms, []byte("nom\x00"+sha)))[:40]
}

// magasinGitHub: the team's workshop on GitHub, encrypted.
type magasinGitHub struct {
	d, miroir string
	r         reglagesGH
	cle       string
	lu        string // the commit read last ("": none on GitHub)
	version   int64
}

func ouvrirMagasinGitHub(projet, cle string) (*magasinGitHub, error) {
	d, miroir, r, err := contexteGH(projet)
	if err != nil {
		return nil, err
	}
	return &magasinGitHub{d: d, miroir: miroir, r: r, cle: cle}, nil
}

func (m *magasinGitHub) dernier() (ManifesteAtelier, error) {
	v := ManifesteAtelier{Fichiers: map[string]string{}}
	g, _, err := clesAtelierGH(m.cle)
	if err != nil {
		return v, err
	}
	out, err := gitDistantGH(m.d, m.miroir, m.r, "ls-remote", "{url}", refAtelierGH)
	if err != nil {
		return v, fmt.Errorf("GitHub not read: %s", derniereLigneSortie(out))
	}
	sha := ""
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[1] == refAtelierGH && reShaGH.MatchString(f[0]) {
			sha = f[0]
		}
	}
	m.lu, m.version = sha, 0
	if sha == "" {
		_ = gmGH(m.miroir, nil, "update-ref", "-d", refAtelierLuGH).Run()
		return v, nil // nothing on GitHub yet
	}
	if gmGH(m.miroir, nil, "cat-file", "-e", sha+"^{commit}").Run() != nil {
		if out, err := gitDistantGH(m.d, m.miroir, m.r, "fetch", "--no-tags", "--no-write-fetch-head", "{url}", "+"+refAtelierGH+":"+refAtelierLuGH); err != nil {
			return v, fmt.Errorf("GitHub not read: %s", derniereLigneSortie(out))
		}
	}
	_ = gmGH(m.miroir, nil, "update-ref", refAtelierLuGH, sha).Run()
	b, err := gmGH(m.miroir, nil, "cat-file", "blob", sha+":manifeste").Output()
	if err != nil {
		return v, errors.New("the team's workshop on GitHub has no version")
	}
	clair, err := dechiffrerAtelierGH(g, b)
	if err != nil {
		return v, errors.New("the team's workshop on GitHub is not this team's (another key)")
	}
	if err := json.Unmarshal(clair, &v); err != nil {
		return v, err
	}
	if v.Fichiers == nil {
		v.Fichiers = map[string]string{}
	}
	m.version = v.Version
	return v, nil
}

func (m *magasinGitHub) objets(shas []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(shas) == 0 {
		return out, nil
	}
	g, noms, err := clesAtelierGH(m.cle)
	if err != nil {
		return nil, err
	}
	if m.lu == "" {
		return nil, errors.New("the team's workshop on GitHub was not read")
	}
	for _, s := range shas {
		if !reShaAtelier.MatchString(s) {
			return nil, errors.New("bad object name")
		}
		b, err := gmGH(m.miroir, nil, "cat-file", "blob", m.lu+":"+nomObjetAtelierGH(noms, s)).Output()
		if err != nil {
			return nil, fmt.Errorf("file %s missing from the team's workshop on GitHub", s[:12])
		}
		clair, err := dechiffrerAtelierGH(g, b)
		if err != nil || shaAtelier(clair) != s {
			return nil, errors.New("a file of the team's workshop on GitHub is damaged")
		}
		out[s] = clair
	}
	return out, nil
}

func (m *magasinGitHub) ecrire(base int64, fichiers map[string]string, objets map[string][]byte, par string) (int64, error) {
	if base != m.version {
		return 0, errConflitAtelier
	}
	g, noms, err := clesAtelierGH(m.cle)
	if err != nil {
		return 0, err
	}
	for rel, s := range fichiers {
		if !cheminAtelierValide(rel) || !reShaAtelier.MatchString(s) {
			return 0, errors.New("bad path in the workshop: " + tronquerRunes(rel, 80))
		}
	}
	ecrireBlob := func(b []byte) (string, error) {
		c := gmGH(m.miroir, nil, "hash-object", "-w", "--stdin")
		c.Stdin = bytes.NewReader(b)
		s, err := sortieGH(c)
		if err != nil || !reShaGH.MatchString(s) {
			return "", errors.New("the workshop cannot be written for GitHub")
		}
		return s, nil
	}
	// the contents already on GitHub keep their blob; the others are encrypted now
	anciens := map[string]string{}
	if m.lu != "" {
		if out, err := sortieGH(gmGH(m.miroir, nil, "ls-tree", m.lu)); err == nil {
			for _, l := range strings.Split(out, "\n") {
				if f := strings.Fields(l); len(f) == 4 && f[1] == "blob" {
					anciens[f[3]] = f[2]
				}
			}
		}
	}
	entrees := map[string]string{}
	for _, s := range fichiers {
		nom := nomObjetAtelierGH(noms, s)
		if _, ok := entrees[nom]; ok {
			continue
		}
		if b, ok := anciens[nom]; ok {
			entrees[nom] = b
			continue
		}
		c, ok := objets[s]
		if !ok || shaAtelier(c) != s {
			return 0, errors.New("a file of the new version was not given")
		}
		blob, err := ecrireBlob(chiffrerAtelierGH(g, noms, c))
		if err != nil {
			return 0, err
		}
		entrees[nom] = blob
	}
	v := ManifesteAtelier{Version: base + 1, Fichiers: fichiers, Par: par, Quand: time.Now().UTC().Format(time.RFC3339)}
	j, _ := json.Marshal(v)
	manifeste, err := ecrireBlob(chiffrerAtelierGH(g, noms, j))
	if err != nil {
		return 0, err
	}
	var arbre strings.Builder
	arbre.WriteString("100644 blob " + manifeste + "\tmanifeste\n")
	for nom, b := range entrees {
		arbre.WriteString("100644 blob " + b + "\t" + nom + "\n")
	}
	c := gmGH(m.miroir, nil, "mktree")
	c.Stdin = strings.NewReader(arbre.String())
	tree, err := sortieGH(c)
	if err != nil || !reShaGH.MatchString(tree) {
		return 0, errors.New("the workshop cannot be written for GitHub")
	}
	ident := []string{"GIT_AUTHOR_NAME=Athanor", "GIT_AUTHOR_EMAIL=athanor@users.noreply.github.com",
		"GIT_COMMITTER_NAME=Athanor", "GIT_COMMITTER_EMAIL=athanor@users.noreply.github.com"}
	args := []string{"commit-tree", tree, "-m", "Athanor: the team's workshop (encrypted)"}
	if m.lu != "" {
		args = append(args, "-p", m.lu)
	}
	commit, err := sortieGH(gmGH(m.miroir, ident, args...))
	if err != nil || !reShaGH.MatchString(commit) {
		return 0, errors.New("the workshop cannot be written for GitHub")
	}
	out, err := gitDistantGH(m.d, m.miroir, m.r, "push", "--porcelain", "--no-verify", "--force-with-lease="+refAtelierGH+":"+m.lu, "{url}", commit+":"+refAtelierGH)
	if err != nil {
		if strings.Contains(out, "stale info") || strings.Contains(out, "rejected") || strings.Contains(out, "[rejected]") {
			return 0, errConflitAtelier
		}
		return 0, fmt.Errorf("GitHub refused the workshop: %s", derniereLigneSortie(out))
	}
	m.lu, m.version = commit, v.Version
	_ = gmGH(m.miroir, nil, "update-ref", refAtelierLuGH, commit).Run()
	return v.Version, nil
}
