package main

// The clean-up of an older Athanor folder, at an update (deployerKit): before 2.25 the folder was a copy of
// the repository, with bash and Python scripts (commands/, system/*.sh, system/*.py…), its docs and its
// README. No version uses them any more, and an update only rewrites what it ships (template/, the
// executables, .env.example, LICENSE). Each such file is removed when it is exactly as a version shipped it
// (its git blob hash, with LF or CRLF line ends: anciens_fichiers.go); one the user changed is moved, never
// lost, into logs/ancien-kit/<date>/. The folders left empty go. Never touched: a file at another path
// (the user's own, .env, the settings, logs/, tools/, extensions/, local/, the links and RTK of
// system/bin, the state files Athanor writes in system/), and anything behind a link.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// listeAnciens: path → the hashes of its shipped versions (a test stands in for anciensFichiersKit).
var listeAnciens = func() map[string]map[string]bool { return lireListeAnciens(anciensFichiersKit) }

func lireListeAnciens(texte string) map[string]map[string]bool {
	v := map[string]map[string]bool{}
	for _, l := range strings.Split(texte, "\n") {
		h, p, ok := strings.Cut(strings.TrimSpace(l), "\t")
		if !ok || len(h) != 40 || !cheminAncienSur(p) {
			continue
		}
		if v[p] == nil {
			v[p] = map[string]bool{}
		}
		v[p][h] = true
	}
	return v
}

// cheminAncienSur: a relative path inside the folder, never one Athanor still uses there.
func cheminAncienSur(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." || c == ".." {
			return false
		}
	}
	// (system/bin: its old bash scripts go; the commands Athanor puts there are links, made again as a pilot
	// starts, and RTK is in no version's list)
	for _, garde := range []string{"system/bin-windows/", "template/", "app/", "logs/", "tools/", "extensions/", "local/"} {
		if strings.HasPrefix(p, garde) {
			return false
		}
	}
	switch p {
	case ".env", ".env.example", "LICENSE", "Athanor.exe", "athanor", "system/version.txt":
		return false
	}
	return true
}

// blobGit: the hash git gives a file of this content.
func blobGit(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// sansLienAncien: no folder between d and the file is a link (the clean-up never reaches outside d).
func sansLienAncien(d, rel string) bool {
	courant := d
	parties := strings.Split(rel, "/")
	for _, c := range parties[:len(parties)-1] {
		courant = filepath.Join(courant, c)
		st, err := os.Lstat(courant)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// BilanNettoyage: what the clean-up did.
type BilanNettoyage struct {
	Retires []string // as shipped: removed
	Ecartes []string // changed by the user: moved into Garde
	Garde   string
}

// nettoyerAncienKit: the files of older versions that no version uses any more, out of Athanor's folder d.
func nettoyerAncienKit(d string) BilanNettoyage {
	var bilan BilanNettoyage
	liste := listeAnciens()
	chemins := make([]string, 0, len(liste))
	for p := range liste {
		chemins = append(chemins, p)
	}
	sort.Strings(chemins)
	dossiers := map[string]bool{}
	for _, rel := range chemins {
		if !sansLienAncien(d, rel) {
			continue
		}
		f := filepath.Join(d, filepath.FromSlash(rel))
		st, err := os.Lstat(f)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		for x := filepath.Dir(rel); x != "." && x != "/"; x = filepath.Dir(x) {
			dossiers[filepath.ToSlash(x)] = true
		}
		b, err := lireBorne(f, 32<<20)
		if err == nil && (liste[rel][blobGit(b)] || liste[rel][blobGit(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))]) {
			if os.Remove(f) == nil {
				bilan.Retires = append(bilan.Retires, rel)
			}
			continue
		}
		// changed by the user (or too big to read): kept, out of the way
		if bilan.Garde == "" {
			bilan.Garde = filepath.Join(d, "logs", "ancien-kit", time.Now().Format("20060102-150405"))
		}
		cible := filepath.Join(bilan.Garde, filepath.FromSlash(rel))
		if os.MkdirAll(filepath.Dir(cible), 0o755) == nil && os.Rename(f, cible) == nil {
			bilan.Ecartes = append(bilan.Ecartes, rel)
		}
	}
	// Python's caches of the old scripts (system/__pycache__/*.pyc): only made from them
	cache := filepath.Join(d, "system", "__pycache__")
	if st, err := os.Lstat(cache); err == nil && st.IsDir() {
		if l, err := os.ReadDir(cache); err == nil {
			for _, e := range l {
				if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".pyc") && os.Remove(filepath.Join(cache, e.Name())) == nil {
					bilan.Retires = append(bilan.Retires, "system/__pycache__/"+e.Name())
				}
			}
		}
		dossiers["system/__pycache__"] = true
	}
	// the folders left empty, the deepest first (system/ itself stays: Athanor writes in it)
	var l []string
	for x := range dossiers {
		if x != "system" {
			l = append(l, x)
		}
	}
	sort.Slice(l, func(i, j int) bool {
		return strings.Count(l[i], "/") > strings.Count(l[j], "/") || (strings.Count(l[i], "/") == strings.Count(l[j], "/") && l[i] > l[j])
	})
	for _, x := range l {
		p := filepath.Join(d, filepath.FromSlash(x))
		if st, err := os.Lstat(p); err == nil && st.IsDir() && st.Mode()&os.ModeSymlink == 0 {
			_ = os.Remove(p) // only when empty
		}
	}
	if len(bilan.Retires)+len(bilan.Ecartes) > 0 {
		ecrireBilanNettoyage(d, bilan)
	}
	return bilan
}

func lireBorne(f string, max int64) ([]byte, error) {
	o, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer o.Close()
	b, err := io.ReadAll(io.LimitReader(o, max+1))
	if err == nil && int64(len(b)) > max {
		return nil, fmt.Errorf("too big")
	}
	return b, err
}

// ecrireBilanNettoyage: logs/nettoyage-<date>.txt, what went and where the changed files are.
func ecrireBilanNettoyage(d string, b BilanNettoyage) {
	var s strings.Builder
	fmt.Fprintf(&s, "Athanor %s: files of older versions that no version uses any more.\n\n", versionAthanor)
	if len(b.Retires) > 0 {
		fmt.Fprintf(&s, "Removed (as a version shipped them): %d\n", len(b.Retires))
		for _, r := range b.Retires {
			s.WriteString("  " + r + "\n")
		}
	}
	if len(b.Ecartes) > 0 {
		fmt.Fprintf(&s, "\nChanged since, so kept in %s: %d\n", b.Garde, len(b.Ecartes))
		for _, r := range b.Ecartes {
			s.WriteString("  " + r + "\n")
		}
	}
	_ = os.MkdirAll(filepath.Join(d, "logs"), 0o755)
	_ = os.WriteFile(filepath.Join(d, "logs", "nettoyage-"+time.Now().Format("20060102-150405")+".txt"), []byte(s.String()), 0o644)
}
