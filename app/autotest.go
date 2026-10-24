package main

// athanor autotest [KIT]: checks that this executable works with this kit, without starting anything:
// its built-in files, the kit's scripts and texts, the replacements of local/. Exit code 0 when all is
// well. A rebuilt executable must pass it before it replaces the running one (self-repair, plan 1.6).

import (
	"bytes"
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
	// the helpers answer as they must (a rebuilt executable that would let everything through fails here)
	ajouter("hook bash", gardeBash([]byte(`{"tool_input":{"command":"sleep 900"}}`)) != "" && gardeBash([]byte(`{"tool_input":{"command":"ls"}}`)) == "", "a long wait refused, ls allowed")
	ajouter("hook fichiers", gardeFichiers([]string{"/projet"}, []byte(`{"tool_name":"Read","cwd":"/projet","tool_input":{"file_path":"/etc/passwd"}}`)) != "", "a file outside the project refused")
	ajouter("JSON as Python writes it", string(jsonPython(map[string]any{"a": []any{1, "é"}}, "", true)) == `{"a": [1, "\u00e9"]}` && reprFlottant(0.1) == "0.1", "")
	petit := redimensionner(&imageRGB{l: 4, h: 2, pix: bytes.Repeat([]byte{200, 100, 50}, 8)}, 2, 1)
	ajouter("image resize", petit.l == 2 && petit.h == 1 && petit.pix[0] == 200 && petit.pix[2] == 50, "")
	n := 0
	_ = fs.WalkDir(sourcesIntegrees, ".", func(string, fs.DirEntry, error) error { n++; return nil })
	ajouter("built-in sources", n > 50, fmt.Sprintf("%d files (athanor reparer --diff)", n))
	if kitEmbarque() { // a plain build (go build, the tests) carries none, and needs a tool folder around it
		_, _, err := fichiersArchive(archiveKitGz)
		ajouter("built-in kit files", err == nil, "template/, .env.example, app/ (a single executable: it updates itself)")
	}
	if racine == "" {
		ajouter("kit", false, "not found (athanor autotest KIT)")
		return r
	}
	k := Kit{racine}
	ajouter("kit", k.Valide(), racine)
	for _, f := range []string{"template/CLAUDE.md", "template/PC_TOOLS_GUIDE.md", ".env.example"} {
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
