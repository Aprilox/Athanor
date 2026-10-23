package main

// athanor relais contexte LOG | sur LOG | resume LOG [HANDOFF]: what a fresh session needs to take over a
// milestone when the previous one's context was full (replaces system/relais.py, same outputs).
//   contexte  the size of the lead's context at its latest turn (tokens)
//   sur       exit 0 when the session is between two steps (no tool call of the lead waits), 1 otherwise
//   resume    the relay notes in Markdown: the session's HANDOFF file, its last actions and messages
// Everything read from the log was written by the model: it is quoted, never run.

import (
	"fmt"
	"os"
	"strings"
)

const maxLectureRelais = 8 << 20 // only the end of a long log matters

func lignesRelais(chemin string) []map[string]any {
	var out []map[string]any
	for _, o := range lignesJSON(chemin, maxLectureRelais) {
		switch o["type"] {
		case "rate_limit_event", "tool_progress", "stream_event":
			continue
		}
		out = append(out, o)
	}
	return out
}

func duChef(o map[string]any) bool { return o["parent_tool_use_id"] == nil }

func contenuMessage(o map[string]any) []any { return liste(dict(o["message"])["content"]) }

func relaisContexte(journal string) int64 {
	var n int64
	for _, o := range lignesRelais(journal) {
		if o["type"] != "assistant" || !duChef(o) {
			continue
		}
		u := dict(dict(o["message"])["usage"])
		if u == nil {
			continue
		}
		var t int64
		for _, k := range []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
			if v, ok := entierJSON(u[k]); ok {
				t += v
			}
		}
		if t != 0 {
			n = t
		}
	}
	return n
}

// relaisSur: no tool call of the lead waits for its result.
func relaisSur(journal string) bool {
	enCours := map[string]bool{}
	for _, o := range lignesRelais(journal) {
		if !duChef(o) {
			continue
		}
		for _, b := range contenuMessage(o) {
			m := dict(b)
			if m == nil {
				continue
			}
			switch m["type"] {
			case "tool_use":
				if id, ok := texte(m["id"]); ok {
					enCours[id] = true
				}
			case "tool_result":
				if id, ok := texte(m["tool_use_id"]); ok {
					delete(enCours, id)
				}
			}
		}
	}
	return len(enCours) == 0
}

// courtRelais: whitespace folded, at most n characters (the last one an ellipsis), as relais.py.
func courtRelais(t string, n int) string {
	t = strings.Join(strings.Fields(t), " ")
	r := []rune(t)
	if len(r) <= n {
		return t
	}
	return string(r[:n-1]) + "…"
}

// textePython: a JSON value as Python's str() would write the common cases (text, numbers).
func textePython(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(x)
	}
}

// vraiPython: Python's truthiness for a JSON value.
func vraiPython(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		f, ok := nombreJSON(v)
		return !ok || f != 0
	}
}

// lignesPython: str.splitlines for the usual line ends.
func lignesPython(s string) []string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	l := strings.Split(s, "\n")
	if len(l) > 0 && l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	return l
}

func relaisResume(journal, handoff string) string {
	var actions, textes []string
	for _, o := range lignesRelais(journal) {
		if o["type"] != "assistant" || !duChef(o) {
			continue
		}
		for _, b := range contenuMessage(o) {
			m := dict(b)
			if m == nil {
				continue
			}
			switch m["type"] {
			case "tool_use":
				i := dict(m["input"])
				chemin, _ := texte(i["file_path"])
				chemin = strings.ReplaceAll(chemin, "\\", "/")
				if strings.Count(chemin, "/") > 3 {
					p := strings.Split(chemin, "/")
					chemin = "…/" + strings.Join(p[len(p)-3:], "/")
				}
				var quoi any = ""
				for _, v := range []any{i["description"], chemin, i["command"], i["pattern"], i["prompt"]} {
					if vraiPython(v) {
						quoi = v
						break
					}
				}
				nom, ok := m["name"]
				if !ok {
					nom = "?"
				}
				actions = append(actions, courtRelais(textePython(nom), 40)+": "+courtRelais(textePython(quoi), 180))
			case "text":
				t := ""
				if v, ok := m["text"]; ok {
					t = textePython(v)
				}
				if strings.TrimSpace(t) != "" {
					textes = append(textes, courtRelais(t, 500))
				}
			}
		}
	}
	var b strings.Builder
	b.WriteString("## Relay: you take over a milestone in progress\n\n")
	b.WriteString("The previous session's context was full, so the pilot stopped it between two steps and started you with a fresh one. Its work is in the folder: committed (`git log`) or not yet (`git status`, `git diff`). Carry on from where it was; do not redo what it did, and check before trusting what it says.\n\n")
	note := ""
	if handoff != "" {
		h := cheminOutil(handoff)
		if st, err := os.Lstat(h); err == nil && st.Mode().IsRegular() && st.Size() < 64<<10 {
			if c, err := os.ReadFile(h); err == nil {
				note = strings.TrimSpace(strings.ToValidUTF8(string(c), "�"))
			}
		}
	}
	if note != "" {
		b.WriteString("### Where it said it was (`.athanor/HANDOFF.md`)\n\n")
		l := lignesPython(note)
		if len(l) > 60 {
			l = l[:60]
		}
		for i, x := range l {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("> " + x)
		}
		b.WriteString("\n\n")
	}
	if len(actions) > 0 {
		b.WriteString("### Its last actions\n\n")
		if len(actions) > 20 {
			actions = actions[len(actions)-20:]
		}
		for _, a := range actions {
			b.WriteString("- " + strings.ReplaceAll(a, "\n", " ") + "\n")
		}
		b.WriteString("\n")
	}
	if len(textes) > 0 {
		b.WriteString("### Its last messages\n\n")
		if len(textes) > 3 {
			textes = textes[len(textes)-3:]
		}
		for _, t := range textes {
			b.WriteString("> " + strings.ReplaceAll(t, "\n", " ") + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func lancerRelais(args []string) int {
	if len(args) >= 2 {
		switch args[0] {
		case "contexte":
			fmt.Println(relaisContexte(args[1]))
			return 0
		case "sur":
			if relaisSur(args[1]) {
				return 0
			}
			return 1
		case "resume":
			h := ""
			if len(args) > 2 {
				h = args[2]
			}
			fmt.Print(relaisResume(args[1], h))
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "usage: athanor relais contexte LOG | sur LOG | resume LOG [HANDOFF]")
	return 2
}
