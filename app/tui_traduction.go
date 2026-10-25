package main

// The terminal interface speaks the app's language with the app's own translations (static/i18n.js: the
// keys are the English texts), so both always say the same.

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

var (
	dicosTUI   = map[string]map[string]string{}
	dicosTUIMu sync.Mutex
)

// dicoTUI: the translations of a language (nil for English, or one i18n.js does not have).
func dicoTUI(langue string) map[string]string {
	if langue == "" || langue == "en" {
		return nil
	}
	dicosTUIMu.Lock()
	defer dicosTUIMu.Unlock()
	if d, ok := dicosTUI[langue]; ok {
		return d
	}
	b, err := statique.ReadFile("static/i18n.js")
	var d map[string]string
	if err == nil {
		d = lireDicoJS(string(b), langue)
		if prefixesTUI == nil {
			prefixesTUI = lirePrefixesJS(string(b))
		}
	}
	dicosTUI[langue] = d
	return d
}

// prefixesTUI: the keys that start a longer text (an error followed by its details): i18n.js's PREFIXES.
var prefixesTUI []string

func lirePrefixesJS(js string) []string {
	i := strings.Index(js, "const PREFIXES = [")
	if i < 0 {
		return []string{}
	}
	i += len("const PREFIXES = ")
	j := strings.Index(js[i:], "];")
	var l []string
	if j < 0 || json.Unmarshal([]byte(js[i:i+j+1]), &l) != nil {
		return []string{}
	}
	return l
}

// lireDicoJS: the block "<langue>: { … }," of i18n.js's DICT (its lines of "key": "text", pairs; its
// comment lines left out).
func lireDicoJS(js, langue string) map[string]string {
	debut := strings.Index(js, "\n"+langue+": {\n")
	if debut < 0 {
		return nil
	}
	debut += len(langue) + 5
	fin := strings.Index(js[debut:], "\n},")
	if fin < 0 {
		return nil
	}
	var b strings.Builder
	for _, l := range strings.Split(js[debut:debut+fin], "\n") {
		if t := strings.TrimSpace(l); t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	corps := strings.TrimRight(strings.TrimSpace(b.String()), ",")
	d := map[string]string{}
	if json.Unmarshal([]byte("{"+corps+"}"), &d) != nil {
		return nil
	}
	return d
}

// traduireTUI: key in this language, {0}, {1}… replaced by args.
func traduireTUI(d map[string]string, cle string, args ...any) string {
	s := cle
	if v, ok := d[cle]; ok && v != "" {
		s = v
	} else if d != nil {
		for _, p := range prefixesTUI {
			if strings.HasPrefix(cle, p) {
				if v, ok := d[p]; ok {
					s = v + cle[len(p):]
				}
				break
			}
		}
	}
	for i, a := range args {
		var t string
		switch x := a.(type) {
		case string:
			t = x
		case int:
			t = strconv.Itoa(x)
		case int64:
			t = strconv.FormatInt(x, 10)
		default:
			b, _ := json.Marshal(x)
			t = strings.Trim(string(b), `"`)
		}
		s = strings.ReplaceAll(s, "{"+strconv.Itoa(i)+"}", t)
	}
	return s
}
