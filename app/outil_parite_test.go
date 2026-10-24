package main

// The Go helpers against the Python scripts they replace (system/relais.py, cout.py, quota.py): the same
// logs in, the same outputs and files out. Skipped where python3 or the kit's scripts are missing.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dossierOracles: the former scripts (Python, bash) the Go code was checked against, kept out of the
// repository (ATHANOR_ORACLES, else testdata/oracles when present): without them, the parity tests skip.
func dossierOracles() string {
	if d := os.Getenv("ATHANOR_ORACLES"); d != "" {
		return d
	}
	return filepath.Join("testdata", "oracles")
}

func scriptPython(t *testing.T, nom string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the oracles are Linux scripts")
	}
	s, _ := filepath.Abs(filepath.Join(dossierOracles(), "python", nom)) // the former Python helpers, as test oracles
	if _, err := os.Stat(s); err != nil {
		t.Skip("no " + nom)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	return s
}

func python(t *testing.T, script string, args ...string) (string, int) {
	t.Helper()
	c := exec.Command("python3", append([]string{"-I", script}, args...)...)
	out, err := c.Output()
	code := 0
	if e, ok := err.(*exec.ExitError); ok {
		code = e.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func ecrireLignes(t *testing.T, chemin string, lignes ...any) {
	t.Helper()
	var b strings.Builder
	for _, l := range lignes {
		switch x := l.(type) {
		case string:
			b.WriteString(x)
		default:
			j, _ := json.Marshal(x)
			b.Write(j)
		}
		b.WriteString("\n")
	}
	if err := os.WriteFile(chemin, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

type msg = map[string]any

func assistant(id, modele string, parent any, usage msg, contenu ...msg) msg {
	c := make([]any, len(contenu))
	for i := range contenu {
		c[i] = contenu[i]
	}
	return msg{"type": "assistant", "parent_tool_use_id": parent, "session_id": "0f3a9c1e-5b7d-4e8a-9c21-7d4e5f6a8b90",
		"message": msg{"id": id, "model": modele, "usage": usage, "content": c}}
}

// journalDeTest: a lead with text, tools (one still running), a sub-agent's turns, a synthetic message,
// streamed duplicates, noise lines, a quota event and, optionally, a "result" line.
func journalDeTest(t *testing.T, dir string, resultat bool, enCours bool) string {
	u := func(i, cr, cw, o int) msg {
		return msg{"input_tokens": i, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw, "output_tokens": o}
	}
	long := strings.Repeat("très long texte avec des   espaces\tet des retours\n", 30)
	l := []any{
		`{"type":"system","subtype":"init","session_id":"0f3a9c1e-5b7d-4e8a-9c21-7d4e5f6a8b90"}`,
		"pas du json",
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.41,"resetsAt":` + fmt.Sprint(time.Now().Unix()+3600) + `}}}}`,
		assistant("m1", "claude-opus-5-5", nil, u(10, 1000, 200, 50), msg{"type": "text", "text": "Je commence : lecture du plan."}),
		assistant("m1", "claude-opus-5-5", nil, u(12, 1000, 220, 80), msg{"type": "tool_use", "id": "t1", "name": "Read", "input": msg{"file_path": "C:\\Users\\x\\Desktop\\projet\\src\\server\\Main.luau"}}),
		msg{"type": "user", "parent_tool_use_id": nil, "message": msg{"content": []any{msg{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}}}},
		assistant("m2", "claude-opus-5-5", nil, u(3, 5000, 100, 300), msg{"type": "tool_use", "id": "t2", "name": "Bash", "input": msg{"command": "bash .athanor/check.sh quick", "description": ""}}),
		msg{"type": "user", "parent_tool_use_id": nil, "message": msg{"content": []any{msg{"type": "tool_result", "tool_use_id": "t2", "content": "CHECK: PASSED"}}}},
		assistant("s1", "claude-sonnet-5-5", "t9", u(100, 2000, 300, 400), msg{"type": "text", "text": "sous-agent"}),
		assistant("syn", "<synthetic>", nil, u(1, 1, 1, 1), msg{"type": "text", "text": "synthétique"}),
		`{"type":"stream_event","event":{}}`,
		assistant("m3", "claude-opus-5-5", nil, u(5, 9000, 50, 120), msg{"type": "text", "text": long}, msg{"type": "tool_use", "id": "t3", "name": "Grep", "input": msg{"pattern": "TODO"}}),
		msg{"type": "user", "parent_tool_use_id": nil, "message": msg{"content": []any{msg{"type": "tool_result", "tool_use_id": "t3", "content": "rien"}}}},
		assistant("m4", "claude-haiku-5-5", nil, u(7, 7000, 70, 700), msg{"type": "tool_use", "id": "t4", "name": "Task", "input": msg{"prompt": "Vérifie les captures d'écran du jalon M12, puis fais un rapport court."}}),
	}
	if !enCours {
		l = append(l, msg{"type": "user", "parent_tool_use_id": nil, "message": msg{"content": []any{msg{"type": "tool_result", "tool_use_id": "t4", "content": "fait"}}}})
	}
	if resultat {
		l = append(l, msg{"type": "result", "subtype": "success", "total_cost_usd": 4.21,
			"modelUsage": msg{"claude-opus-5-5": msg{"inputTokens": 30, "cacheReadInputTokens": 15000, "cacheCreationInputTokens": 370, "outputTokens": 550, "costUSD": 2.5},
				"claude-haiku-5-5": msg{"inputTokens": 7, "cacheReadInputTokens": 7000, "cacheCreationInputTokens": 70, "outputTokens": 700, "costUSD": 0.4},
				"claude-mini":      msg{"inputTokens": 1, "outputTokens": 1, "costUSD": 0.001}}})
	}
	f := filepath.Join(dir, fmt.Sprintf("cycle-%v-%v.jsonl", resultat, enCours))
	ecrireLignes(t, f, l...)
	return f
}

func TestPariteRelais(t *testing.T) {
	s := scriptPython(t, "relais.py")
	d := t.TempDir()
	h := filepath.Join(d, "HANDOFF.md")
	_ = os.WriteFile(h, []byte("  Jalon M12 : étape 2 faite.\r\nReste : l'écran de chargement.\n\n"+strings.Repeat("ligne\n", 70)), 0o644)
	for _, enCours := range []bool{false, true} {
		j := journalDeTest(t, d, false, enCours)
		py, _ := python(t, s, "contexte", j)
		if g := fmt.Sprintln(relaisContexte(j)); g != py {
			t.Errorf("contexte: go %q, python %q", g, py)
		}
		_, code := python(t, s, "sur", j)
		if relaisSur(j) != (code == 0) {
			t.Errorf("sur (running=%v): go %v, python exit %d", enCours, relaisSur(j), code)
		}
		for _, handoff := range []string{h, "", filepath.Join(d, "absent.md")} {
			args := []string{"resume", j}
			if handoff != "" {
				args = append(args, handoff)
			}
			py, _ = python(t, s, args...)
			if g := relaisResume(j, handoff); g != py {
				t.Errorf("resume (handoff %q):\n--- go\n%s\n--- python\n%s", handoff, g, py)
			}
		}
	}
	py, _ := python(t, s, "contexte", filepath.Join(d, "absent.jsonl"))
	if g := fmt.Sprintln(relaisContexte(filepath.Join(d, "absent.jsonl"))); g != py {
		t.Errorf("missing log: go %q, python %q", g, py)
	}
}

func jsonDeFichierTest(t *testing.T, f string) any {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", f, err)
	}
	return v
}

func TestPariteCout(t *testing.T) {
	s := scriptPython(t, "cout.py")
	d := t.TempDir()
	tarifsGo, tarifsPy := filepath.Join(d, "tarifs-go.json"), filepath.Join(d, "tarifs-py.json")
	// three results per model, with variety, so that the prices can be fitted; then one more
	for n := 0; n < 4; n++ {
		j := filepath.Join(d, fmt.Sprintf("r%d.jsonl", n))
		ecrireLignes(t, j, msg{"type": "result", "modelUsage": msg{
			"claude-opus-5-5":   msg{"inputTokens": 10 + n, "cacheReadInputTokens": 100000 * (n + 1), "cacheCreationInputTokens": 20000 + 5000*n*n, "outputTokens": 3000 + 700*n, "costUSD": 0.5 + 0.21*float64(n) + 0.05*float64(n*n)},
			"claude-sonnet-5-5": msg{"inputTokens": 5, "cacheReadInputTokens": 50000, "cacheCreationInputTokens": 10000, "outputTokens": 1000, "costUSD": 0.11},
		}})
		coutApprendre(tarifsGo, j)
		python(t, s, "apprendre", tarifsPy, j)
		if g, p := jsonDeFichierTest(t, tarifsGo), jsonDeFichierTest(t, tarifsPy); !procheJSON(g, p) {
			t.Fatalf("tarifs after %d results:\n go %v\n py %v", n+1, g, p)
		}
	}
	if p := jsonDeFichierTest(t, tarifsGo); !strings.Contains(fmt.Sprint(p), "prix_par_million:[") {
		t.Fatalf("no price fitted: %v", p)
	}
	// the estimate, with and without the sub-agents' transcripts
	config := filepath.Join(d, "config")
	sa := filepath.Join(config, "projects", "-c-projet", "0f3a9c1e-5b7d-4e8a-9c21-7d4e5f6a8b90", "subagents")
	_ = os.MkdirAll(sa, 0o755)
	ecrireLignes(t, filepath.Join(sa, "agent-1.jsonl"), assistant("a1", "claude-sonnet-5-5", "x", msg{"input_tokens": 9, "cache_read_input_tokens": 9000, "output_tokens": 900}))
	for _, resultat := range []bool{false, true} {
		j := journalDeTest(t, d, resultat, false)
		for _, cfg := range []string{"", config} {
			args := []string{"estimer", tarifsPy, j}
			if cfg != "" {
				args = append(args, cfg)
			}
			py, _ := python(t, s, args...)
			var g, p any
			_ = json.Unmarshal([]byte(coutEstimer(tarifsPy, j, cfg)), &g)
			_ = json.Unmarshal([]byte(py), &p)
			if !procheJSON(g, p) {
				t.Errorf("estimer (config %q):\n go %v\n py %v", cfg, g, p)
			}
		}
	}
	// a log whose "result" has no modelUsage teaches nothing
	j := filepath.Join(d, "sans.jsonl")
	ecrireLignes(t, j, msg{"type": "result", "total_cost_usd": 1})
	avant := jsonDeFichierTest(t, tarifsGo)
	coutApprendre(tarifsGo, j)
	if !reflect.DeepEqual(avant, jsonDeFichierTest(t, tarifsGo)) {
		t.Error("a result without modelUsage changed the prices")
	}
}

// procheJSON: the same JSON, numbers equal to 1e-6 relative (Python rounds half to even on the binary
// value, Go on the decimal one).
func procheJSON(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k := range x {
			if !procheJSON(x[k], y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !procheJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		if !ok {
			return false
		}
		e := x - y
		if e < 0 {
			e = -e
		}
		m := x
		if m < 0 {
			m = -m
		}
		return e <= 1e-6*(1+m)
	default:
		return reflect.DeepEqual(a, b)
	}
}

func TestPariteQuota(t *testing.T) {
	s := scriptPython(t, "quota.py")
	d := t.TempDir()
	futur, passe := time.Now().Unix()+3600, time.Now().Unix()-60
	evt := func(fen string) string {
		return `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{` + fen + `}}}`
	}
	ancien := filepath.Join(d, "ancien.jsonl")
	ecrireLignes(t, ancien, evt(fmt.Sprintf(`"five_hour":{"utilization":0.99,"resetsAt":%d}`, futur)))
	_ = os.Chtimes(ancien, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	recent := filepath.Join(d, "recent.jsonl")
	ecrireLignes(t, recent, "rien",
		evt(fmt.Sprintf(`"five_hour":{"utilization":0.2,"resetsAt":%d}`, futur)),
		evt(fmt.Sprintf(`"seven_day":{"utilization":0.5,"resetsAt":%d},"seven_day_fable":{"utilization":0.86,"resetsAt":%d},"five_hour":{"utilization":0.81,"resetsAt":%d},"seven_day_opus_x":{"utilization":0.95,"resetsAt":%d}`, futur, futur, passe, futur)),
		`{"type":"assistant","message":{"content":"pas de \"rate_limit_event\" ici"}}`)
	vide := filepath.Join(d, "vide.jsonl")
	ecrireLignes(t, vide, `{"type":"rate_limit_event"}`)
	cas := []string{
		`{"five_hour":{"seuil":80,"action":"apres"}}`,
		`{"modele":{"seuil":85,"action":"pause"}}`,
		`{"modele":{"seuil":90,"action":"maintenant"}}`,
		`{"seven_day":{"seuil":50,"action":"apres"},"modele":{"seuil":80,"action":"pause"}}`,
		`{"seven_day":{"seuil":0,"action":"apres"}}`,
		`{"five_hour":{"seuil":80,"action":"autre"}}`,
		`pas du json`,
	}
	nonVides := 0
	defer func() {
		if nonVides < 3 {
			t.Errorf("only %d rules met: the cases test too little", nonVides)
		}
	}()
	for i, c := range cas {
		r := filepath.Join(d, fmt.Sprintf("regles%d.json", i))
		_ = os.WriteFile(r, []byte(c), 0o644)
		for _, js := range [][]string{{ancien, recent}, {ancien}, {vide}, {filepath.Join(d, "absent.jsonl")}} {
			py, _ := python(t, s, append([]string{"verifier", r}, js...)...)
			g := quotaVerifier(r, js)
			if g != py {
				t.Errorf("rules %s, logs %v: go %q, python %q", c, js, g, py)
			}
			if g != "" {
				nonVides++
			}
		}
	}
}

func TestTitrePython(t *testing.T) {
	for in, out := range map[string]string{"fable": "Fable", "opus x": "Opus X", "opus 4x": "Opus 4X", "OPUS": "Opus"} {
		if g := titrePython(in); g != out {
			t.Errorf("%q: %q", in, g)
		}
	}
}
