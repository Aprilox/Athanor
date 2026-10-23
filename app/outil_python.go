package main

// What the helpers need of Python's behaviour to write the same bytes as the scripts they replace:
// json.dump, str.strip, repr, pathlib's paths, the UTF-8 decoder with errors="replace".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// espacePython: str.isspace (Unicode's white space, plus the separators \x1c-\x1f).
func espacePython(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

func stripPython(s string) string  { return strings.TrimFunc(s, espacePython) }
func lstripPython(s string) string { return strings.TrimLeftFunc(s, espacePython) }
func rstripPython(s string) string { return strings.TrimRightFunc(s, espacePython) }

// reprPython: repr() of a str.
func reprPython(s string) string {
	q := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == rune(q):
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == ' ' || (unicode.IsPrint(r) && r != utf8.RuneError):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(q)
	return b.String()
}

// decoderUTF8Python: bytes.decode("utf-8", "replace"): one U+FFFD per maximal invalid subsequence.
func decoderUTF8Python(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || n > 1 {
			out.WriteRune(r)
			i += n
			continue
		}
		// how many bytes of a valid start follow (they are replaced together)
		c := b[i]
		lo, hi, besoin := byte(0x80), byte(0xbf), 0
		switch {
		case c >= 0xc2 && c <= 0xdf:
			besoin = 1
		case c == 0xe0:
			lo, besoin = 0xa0, 2
		case c >= 0xe1 && c <= 0xec, c == 0xee, c == 0xef:
			besoin = 2
		case c == 0xed:
			hi, besoin = 0x9f, 2
		case c == 0xf0:
			lo, besoin = 0x90, 3
		case c >= 0xf1 && c <= 0xf3:
			besoin = 3
		case c == 0xf4:
			hi, besoin = 0x8f, 3
		}
		j := i + 1
		for k := 0; k < besoin && j < len(b); k++ {
			if b[j] < lo || b[j] > hi {
				break
			}
			lo, hi = 0x80, 0xbf
			j++
		}
		out.WriteRune(utf8.RuneError)
		i = j
	}
	return out.String()
}

// texteUniverselPython: a file read as open(encoding="utf-8", errors="replace") reads it.
func texteUniverselPython(b []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(decoderUTF8Python(b), "\r\n", "\n"), "\r", "\n")
}

// cheminPur: str(PurePosixPath(p)) ("" is ".", repeated / and "." parts go, a leading // stays).
func cheminPur(p string) string {
	if runtimeWindows() {
		return cheminPurWindows(p)
	}
	debut := ""
	if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
		debut = "//"
	} else if strings.HasPrefix(p, "/") {
		debut = "/"
	}
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c != "" && c != "." {
			parts = append(parts, c)
		}
	}
	if debut == "" && len(parts) == 0 {
		return "."
	}
	return debut + strings.Join(parts, "/")
}

// cheminPurWindows: str(PureWindowsPath(p)) for the usual forms (C:/x, x\y, \\server\share).
func cheminPurWindows(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	lecteur := ""
	if len(p) >= 2 && p[1] == ':' {
		lecteur, p = p[:2], p[2:]
	}
	racine := ""
	if strings.HasPrefix(p, `\`) {
		racine = `\`
	}
	var parts []string
	for _, c := range strings.Split(p, `\`) {
		if c != "" && c != "." {
			parts = append(parts, c)
		}
	}
	if lecteur == "" && racine == "" && len(parts) == 0 {
		return "."
	}
	return lecteur + racine + strings.Join(parts, `\`)
}

// joindrePur: PurePath(a) / b.
func joindrePur(a, b string) string {
	if strings.HasPrefix(b, "/") || (runtimeWindows() && (strings.HasPrefix(b, `\`) || (len(b) >= 2 && b[1] == ':'))) {
		return cheminPur(b)
	}
	return cheminPur(a + "/" + b)
}

// jsonPython: json.dumps(v, ensure_ascii=ascii, indent=…): indent "" is the compact form (", " and ": ").
// v: objetJSON (keys in order), map[string]any (sorted keys), lists, text, numbers, booleans, nil.
func jsonPython(v any, indent string, ascii bool) []byte {
	var b bytes.Buffer
	ecrireJSONPy(&b, v, indent, 0, ascii)
	return b.Bytes()
}

func ecrireJSONPy(b *bytes.Buffer, v any, indent string, niveau int, ascii bool) {
	ligne := func(n int) {
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, n))
		}
	}
	sep := func() {
		if indent != "" {
			b.WriteByte(',')
		} else {
			b.WriteString(", ")
		}
	}
	liste := func(n int, f func(i int)) {
		if n == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				sep()
			}
			ligne(niveau + 1)
			f(i)
		}
		ligne(niveau)
		b.WriteByte(']')
	}
	objet := func(cles []string, val func(k string) any) {
		if len(cles) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, k := range cles {
			if i > 0 {
				sep()
			}
			ligne(niveau + 1)
			ecrireTextePy(b, k, ascii)
			b.WriteString(": ")
			ecrireJSONPy(b, val(k), indent, niveau+1, ascii)
		}
		ligne(niveau)
		b.WriteByte('}')
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		ecrireTextePy(b, x, ascii)
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(reprFlottant(x))
	case json.Number:
		b.WriteString(string(x))
	case json.RawMessage:
		var y any
		switch t := bytes.TrimSpace(x); {
		case len(t) > 0 && t[0] == '{':
			var o objetJSON
			if json.Unmarshal(t, &o) == nil {
				ecrireJSONPy(b, o, indent, niveau, ascii)
				return
			}
		case len(t) > 0 && t[0] == '[':
			var l []json.RawMessage
			if json.Unmarshal(t, &l) == nil {
				liste(len(l), func(i int) { ecrireJSONPy(b, l[i], indent, niveau+1, ascii) })
				return
			}
		}
		d := json.NewDecoder(bytes.NewReader(x))
		d.UseNumber()
		_ = d.Decode(&y)
		ecrireJSONPy(b, y, indent, niveau, ascii)
	case objetJSON:
		objet(x.cles, func(k string) any { return x.valeur[k] })
	case *objetJSON:
		ecrireJSONPy(b, *x, indent, niveau, ascii)
	case map[string]any:
		cles := make([]string, 0, len(x))
		for k := range x {
			cles = append(cles, k)
		}
		sort.Strings(cles)
		objet(cles, func(k string) any { return x[k] })
	case []string:
		liste(len(x), func(i int) { ecrireTextePy(b, x[i], ascii) })
	case []objetJSON:
		liste(len(x), func(i int) { ecrireJSONPy(b, x[i], indent, niveau+1, ascii) })
	case []any:
		liste(len(x), func(i int) { ecrireJSONPy(b, x[i], indent, niveau+1, ascii) })
	default:
		r, _ := json.Marshal(x)
		ecrireJSONPy(b, json.RawMessage(r), indent, niveau, ascii)
	}
}

func ecrireTextePy(b *bytes.Buffer, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(b, `\u%04x`, r)
			case ascii && r > 0x7f && r < 0x10000:
				fmt.Fprintf(b, `\u%04x`, r)
			case ascii && r >= 0x10000:
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
