package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// clesWeb: the keys the web interface translates: every t("…") of app.js (traduireNav's labels for
// index.html included) and every text of the GUIDE (ids and "#page" links left out).
func clesWeb(t *testing.T) []string {
	b, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := strings.ReplaceAll(string(b), "\r\n", "\n")
	// clesTUI also gives the terminal's own few keys (its pages, its form help): translated all the same
	cles := clesTUI(js)
	vu := map[string]bool{}
	for _, c := range cles {
		vu[c] = true
	}
	i := strings.Index(js, "\nconst GUIDE = [")
	j := strings.Index(js[i+1:], "\n];")
	if i < 0 || j < 0 {
		t.Fatal("no GUIDE in app.js")
	}
	guide := js[i : i+1+j]
	n := 0
	for _, m := range regexp.MustCompile(`(id: )?"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(guide, -1) {
		cle := cleJS(m[2])
		if m[1] != "" || strings.HasPrefix(cle, "#") {
			continue
		}
		n++
		if !vu[cle] {
			vu[cle] = true
			cles = append(cles, cle)
		}
	}
	if n < 80 || !vu["Home"] || !vu["Start a project"] {
		t.Fatalf("keys not found: %d texts in the GUIDE, %d keys", n, len(cles))
	}
	return cles
}

// cleJS: a string literal's content as the text it stands for, like i18n.js's keys read as JSON.
func cleJS(s string) string {
	if v, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return v
	}
	return strings.NewReplacer(`\"`, `"`, `\n`, "\n", `\\`, `\`).Replace(s)
}

func TestWebTraduction(t *testing.T) {
	cles := clesWeb(t)
	for _, e := range []error{errCoffreVerrouille, errCoffreSansMdp, errCoffreMdp, errCoffreInconnu, errCoffreRetraitMdp,
		errCoffreTrousseau, errCoffreFormat, errCoffreAbime, errCoffreIllisible} { // the vault's errors reach the toast (B14)
		cles = append(cles, e.Error())
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		if len(dico) < 1000 {
			t.Fatalf("%s: %d keys", l, len(dico))
		}
		for _, cle := range cles {
			if _, ok := dico[cle]; !ok {
				t.Errorf("%s: no translation of %q", l, cle)
			}
		}
	}
}
