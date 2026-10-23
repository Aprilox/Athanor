package main

// remplacables.go: the kit's replaceable texts. What is text stays text: the verified catalog (and, read by the pilot, the
// instructions, guides and sub-agents of template/) can be repaired on a PC without recompiling: a file of
// <kit>/local/ takes the place of the same one (local/app/catalogue.json, local/template/CLAUDE.md…).
// An update of the kit never writes into local/, so a repair made here is never lost; `athanor autotest`
// lists what local/ replaces.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

var racineTextes atomic.Value // string: the kit in use (set by the server, and by autotest)

func definirKitTextes(racine string) { racineTextes.Store(racine) }

// texteLocal: the kit's local copy of rel ("app/catalogue.json"), nil when there is none (or not a plain
// file, or too large).
func texteLocal(rel string) []byte {
	r, _ := racineTextes.Load().(string)
	if r == "" {
		return nil
	}
	f := filepath.Join(r, "local", filepath.FromSlash(rel))
	st, err := os.Lstat(f)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
		return nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	return b
}

// jsonRemplacable: the local copy of rel when it is valid JSON, else the copy built into the executable.
func jsonRemplacable(rel string, integre []byte) []byte {
	if b := texteLocal(rel); b != nil && json.Valid(b) {
		return b
	}
	return integre
}

// remplacements: the files of <kit>/local/ (relative, "/"-separated), sorted.
func remplacements(racine string) []string {
	var out []string
	base := filepath.Join(racine, "local")
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if rel, e := filepath.Rel(base, p); e == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// remplaceTexte: whether rel is one of the texts local/ may replace (the rest of local/ is ignored).
func remplaceTexte(rel string) bool {
	return rel == "app/catalogue.json" || strings.HasPrefix(rel, "template/")
}
