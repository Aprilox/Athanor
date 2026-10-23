package main

// athanor autotest [KIT]: checks that this executable works with this kit, without starting anything:
// its built-in files, the kit's scripts and texts, the replacements of local/. Exit code 0 when all is
// well. A rebuilt executable must pass it before it replaces the running one (self-repair, plan 1.6).

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type resultatAutotest struct {
	nom    string
	ok     bool
	detail string
}

func autotest(racine string) []resultatAutotest {
	var r []resultatAutotest
	ajouter := func(nom string, ok bool, detail string) { r = append(r, resultatAutotest{nom, ok, detail}) }
	for _, f := range []string{"static/index.html", "static/app.js", "static/app.css", "static/i18n.js"} {
		b, err := fs.ReadFile(statique, f)
		ajouter("built-in "+f, err == nil && len(b) > 0, fmt.Sprintf("%d bytes", len(b)))
	}
	var c Catalogue
	ajouter("built-in catalog", json.Unmarshal(catalogueJSON, &c) == nil && len(c.MCP) > 0, fmt.Sprintf("%d MCP servers, %d skills", len(c.MCP), len(c.Skills)))
	if racine == "" {
		ajouter("kit", false, "not found (athanor autotest KIT)")
		return r
	}
	k := Kit{racine}
	ajouter("kit", k.Valide(), racine)
	for _, f := range []string{"system/pilot.sh", "system/parallel.sh", "system/safety.sh", "template/CLAUDE.md", "template/PC_TOOLS_GUIDE.md", ".env.example"} {
		st, err := os.Stat(filepath.Join(racine, filepath.FromSlash(f)))
		ajouter(f, err == nil && st.Size() > 0, "")
	}
	guides, _ := filepath.Glob(filepath.Join(racine, "template", "guides", "*.md"))
	ajouter("template/guides", len(guides) > 0, fmt.Sprintf("%d guides", len(guides)))
	agents, _ := filepath.Glob(filepath.Join(racine, "template", ".claude", "agents", "*.md"))
	ajouter("template/.claude/agents", len(agents) > 0, fmt.Sprintf("%d sub-agents", len(agents)))
	for _, rel := range remplacements(racine) {
		if !remplaceTexte(rel) {
			ajouter("local/"+rel, true, "ignored: only app/catalogue.json and template/… are replaced")
			continue
		}
		ok, detail := true, "replaces the kit's copy"
		if filepath.Ext(rel) == ".json" {
			b, _ := os.ReadFile(filepath.Join(racine, "local", filepath.FromSlash(rel)))
			if !json.Valid(b) {
				ok, detail = false, "invalid JSON: the built-in copy is used instead"
			}
		}
		ajouter("local/"+rel, ok, detail)
	}
	return r
}

func lancerAutotest(args []string) int {
	racine := ""
	if len(args) > 0 {
		racine = args[0]
	} else if c := lireConfig(); c.Kit != "" {
		racine = c.Kit
	} else {
		racine = devinerKit()
	}
	definirKitTextes(racine)
	echecs := 0
	fmt.Printf("Athanor %s autotest\n", versionAthanor)
	for _, x := range autotest(racine) {
		etat := "ok  "
		if !x.ok {
			etat = "FAIL"
			echecs++
		}
		if x.detail != "" {
			fmt.Printf("%s %s: %s\n", etat, x.nom, x.detail)
		} else {
			fmt.Printf("%s %s\n", etat, x.nom)
		}
	}
	if echecs > 0 {
		fmt.Printf("%d check(s) failed.\n", echecs)
		return 1
	}
	fmt.Println("All good.")
	return 0
}
