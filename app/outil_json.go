package main

// Shared by the helpers that replace the kit's Python scripts (athanor relais | cout | quota): reading
// Claude Code's stream-json logs, JSON objects whose key order is kept (Python's dicts keep it, and the
// files they wrote must read the same), paths as Git Bash gives them.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// objetJSON: a JSON object whose keys keep their order.
type objetJSON struct {
	cles   []string
	valeur map[string]json.RawMessage
}

func (o *objetJSON) UnmarshalJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('{') {
		return &json.UnmarshalTypeError{Value: "not an object"}
	}
	o.cles, o.valeur = nil, map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return err
		}
		if _, deja := o.valeur[k]; !deja {
			o.cles = append(o.cles, k)
		}
		o.valeur[k] = v
	}
	_, err = d.Token()
	return err
}

func (o objetJSON) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.cles {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		v := o.valeur[k]
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// mettre: sets k (at its place when present, else at the end, as a Python dict does).
func (o *objetJSON) mettre(k string, v any) {
	b, _ := json.Marshal(v)
	if o.valeur == nil {
		o.valeur = map[string]json.RawMessage{}
	}
	if _, deja := o.valeur[k]; !deja {
		o.cles = append(o.cles, k)
	}
	o.valeur[k] = b
}

// garderDerniers: only the last n keys.
func (o *objetJSON) garderDerniers(n int) {
	if len(o.cles) <= n {
		return
	}
	for _, k := range o.cles[:len(o.cles)-n] {
		delete(o.valeur, k)
	}
	o.cles = append([]string(nil), o.cles[len(o.cles)-n:]...)
}

// lignesJSON: the JSON objects of a log, one per line, read from its last max bytes (0: all of it).
func lignesJSON(chemin string, max int64) []map[string]any {
	f, err := os.Open(cheminOutil(chemin))
	if err != nil {
		return nil
	}
	defer f.Close()
	if max > 0 {
		if st, err := f.Stat(); err == nil && st.Size() > max {
			_, _ = f.Seek(st.Size()-max, io.SeekStart)
		}
	}
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		l := strings.TrimSpace(strings.ToValidUTF8(sc.Text(), "\uFFFD"))
		if !strings.HasPrefix(l, "{") {
			continue
		}
		d := json.NewDecoder(strings.NewReader(l))
		d.UseNumber()
		var o map[string]any
		if d.Decode(&o) == nil && o != nil {
			out = append(out, o)
		}
	}
	return out
}

func dict(v any) map[string]any  { m, _ := v.(map[string]any); return m }
func liste(v any) []any          { l, _ := v.([]any); return l }
func texte(v any) (string, bool) { s, ok := v.(string); return s, ok }

// entierJSON: a JSON integer (not a decimal), as Python's isinstance(v, int).
func entierJSON(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok || strings.ContainsAny(string(n), ".eE") {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

// nombreJSON: any JSON number.
func nombreJSON(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

var reCheminGitBash = regexp.MustCompile(`^/([a-zA-Z])(/.*)?$`)

// cheminOutil: on Windows, a Git Bash path (/c/Users/…) becomes C:/Users/…; anything else as it is.
// cheminsOutil: every argument that is a Git Bash path (/c/...) as Windows names it (native Windows only).
func cheminsOutil(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = cheminOutil(a)
	}
	return out
}

func cheminOutil(p string) string {
	if runtime.GOOS == "windows" {
		if m := reCheminGitBash.FindStringSubmatch(p); m != nil {
			return strings.ToUpper(m[1]) + ":" + firstNonEmpty(m[2], "/")
		}
	}
	return p
}

// reprFlottant: Python's repr of a float (the shortest digits that read back the same; an exponent below
// 1e-4 and from 1e16 on; always a decimal point or an exponent).
func reprFlottant(x float64) string {
	switch {
	case math.IsNaN(x):
		return "nan"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	}
	e := strconv.FormatFloat(x, 'e', -1, 64) // d.ddde±XX
	signe := ""
	if e[0] == '-' {
		signe, e = "-", e[1:]
	}
	i := strings.IndexByte(e, 'e')
	chiffres := strings.Replace(e[:i], ".", "", 1)
	exp, _ := strconv.Atoi(e[i+1:])
	if exp < -4 || exp >= 16 {
		m := chiffres[:1]
		if len(chiffres) > 1 {
			m += "." + chiffres[1:]
		}
		s := "+"
		if exp < 0 {
			s, exp = "-", -exp
		}
		return fmt.Sprintf("%s%se%s%02d", signe, m, s, exp)
	}
	if exp < 0 {
		return signe + "0." + strings.Repeat("0", -exp-1) + chiffres
	}
	if len(chiffres) <= exp+1 {
		return signe + chiffres + strings.Repeat("0", exp+1-len(chiffres)) + ".0"
	}
	return signe + chiffres[:exp+1] + "." + chiffres[exp+1:]
}
