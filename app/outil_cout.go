package main

// athanor cout apprendre TARIFS LOG | estimer TARIFS LOG [CONFIG]: the cost of a cycle whose log has no
// "result" line (the pilot closed it after its milestone, or the watchdog stopped it): Claude Code only
// writes the cost in that last line (replaces system/cout.py, same files and outputs).
//   apprendre  learns the price of each model from the "result" line of LOG (its modelUsage)
//   estimer    prints, as JSON, the lead's turns of LOG (each message once) and their cost at the learned
//              prices, plus the cost of that session's sub-agents, read from Claude Code's transcripts in
//              CONFIG (~/.claude: projects/*/<session>/subagents/agent-*.jsonl); total_cost_usd is absent
//              while a model used has no learned price
// Prices are never written here: they change, and depend on the plan. They are learned from this PC's own
// results, at least 3 per model, in TARIFS (a JSON file of the kit's logs, outside the sandbox).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxEchantillons = 40
	maxFichierCout  = 1 << 20
)

var clesJetons = []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "output_tokens"}

// entierPositif: cout.py's entier(): a non-negative JSON integer, else 0.
func entierPositif(v any) int64 {
	if i, ok := entierJSON(v); ok && i >= 0 {
		return i
	}
	return 0
}

// lireTarifs: TARIFS, an object ({} when missing, a link, too large or not JSON).
func lireTarifs(chemin string) objetJSON {
	var d objetJSON
	f := cheminOutil(chemin)
	st, err := os.Lstat(f)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxFichierCout {
		return d
	}
	b, err := os.ReadFile(f)
	if err != nil || json.Unmarshal(b, &d) != nil {
		return objetJSON{}
	}
	return d
}

func ecrireTarifs(chemin string, d objetJSON) {
	f := cheminOutil(chemin)
	brut, err := json.Marshal(d)
	if err != nil {
		return
	}
	var joli bytes.Buffer
	if json.Indent(&joli, brut, "", " ") != nil { // as Python's indent=1
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(f), ".tarifs-")
	if err != nil {
		return
	}
	_, err = tmp.Write(joli.Bytes())
	tmp.Close()
	if err != nil || os.Rename(tmp.Name(), f) != nil {
		os.Remove(tmp.Name())
	}
}

// variablesPrix: three prices: cache writes (the few uncached input tokens priced alike), reads, output.
func variablesPrix(i, cr, cw, o float64) [3]float64 { return [3]float64{i + cw, cr, o} }

// ajusterPrix: least squares over [i, cr, cw, o, cost] samples, dollars per million tokens; nil when the
// prices cannot be told apart.
func ajusterPrix(ech [][5]float64) []float64 {
	if len(ech) < 3 {
		return nil
	}
	X := make([][3]float64, len(ech))
	y := make([]float64, len(ech))
	for n, e := range ech {
		v := variablesPrix(e[0], e[1], e[2], e[3])
		for a := range v {
			X[n][a] = v[a] / 1e6
		}
		y[n] = e[4]
	}
	var A [3][3]float64
	var B [3]float64
	for a := 0; a < 3; a++ {
		for c := 0; c < 3; c++ {
			for _, r := range X {
				A[a][c] += r[a] * r[c]
			}
		}
		for n, r := range X {
			B[a] += r[a] * y[n]
		}
	}
	var coef []float64
	ok := true
	for c := 0; c < 3 && ok; c++ {
		p := c
		for k := c + 1; k < 3; k++ {
			if math.Abs(A[k][c]) > math.Abs(A[p][c]) {
				p = k
			}
		}
		if math.Abs(A[p][c]) < 1e-9 {
			ok = false
			break
		}
		A[c], A[p] = A[p], A[c]
		B[c], B[p] = B[p], B[c]
		for k := 0; k < 3; k++ {
			if k != c {
				m := A[k][c] / A[c][c]
				for j := 0; j < 3; j++ {
					A[k][j] -= m * A[c][j]
				}
				B[k] -= m * B[c]
			}
		}
	}
	if ok {
		coef = []float64{B[0] / A[0][0], B[1] / A[1][1], B[2] / A[2][2]}
	}
	if coef == nil || math.Min(coef[0], math.Min(coef[1], coef[2])) < 0 {
		// not enough variety in the samples: one price, with the usual ratios between the three
		rapports := [3]float64{1.0, 0.05, 2.0}
		total, somme := 0.0, 0.0
		for _, r := range X {
			for a := 0; a < 3; a++ {
				total += r[a] * rapports[a]
			}
		}
		for _, v := range y {
			somme += v
		}
		if total <= 0 {
			return nil
		}
		k := somme / total
		coef = []float64{k * rapports[0], k * rapports[1], k * rapports[2]}
	}
	for i := range coef {
		coef[i] = math.RoundToEven(coef[i]*1e6) / 1e6
	}
	return coef
}

func coutApprendre(tarifs, journal string) {
	mu, ok := modelUsageOrdonne(journal)
	if !ok {
		return
	}
	deverrouiller, err := verrouillerFichier(cheminOutil(tarifs) + ".lock")
	if err != nil {
		return
	}
	defer deverrouiller()
	d := lireTarifs(tarifs)
	var modeles objetJSON
	if b, ok := d.valeur["modeles"]; !ok || json.Unmarshal(b, &modeles) != nil {
		modeles = objetJSON{}
	}
	cles := mu.cles
	if len(cles) > 10 {
		cles = cles[:10]
	}
	for _, nom := range cles {
		var u map[string]any
		dec := json.NewDecoder(strings.NewReader(string(mu.valeur[nom])))
		dec.UseNumber()
		if dec.Decode(&u) != nil || u == nil || len([]rune(nom)) > 80 {
			continue
		}
		c, ok := nombreJSON(u["costUSD"])
		if !ok || c <= 0.01 {
			continue
		}
		e := [5]float64{float64(entierPositif(u["inputTokens"])), float64(entierPositif(u["cacheReadInputTokens"])),
			float64(entierPositif(u["cacheCreationInputTokens"])), float64(entierPositif(u["outputTokens"])), c}
		var anciens []any
		var m map[string]any
		if b, ok := modeles.valeur[nom]; ok {
			dec := json.NewDecoder(strings.NewReader(string(b)))
			dec.UseNumber()
			_ = dec.Decode(&m)
		}
		anciens = liste(m["echantillons"])
		var ech [][5]float64
		var brut []any
		for _, x := range anciens {
			l := liste(x)
			if len(l) != 5 {
				continue
			}
			var v [5]float64
			bon := true
			for i := range l {
				f, ok := nombreJSON(l[i])
				if !ok {
					bon = false
				}
				v[i] = f
			}
			if bon {
				ech = append(ech, v)
				brut = append(brut, x)
			}
		}
		ech = append(ech, e)
		brut = append(brut, []any{int64(e[0]), int64(e[1]), int64(e[2]), int64(e[3]), c})
		if len(ech) > maxEchantillons {
			ech = ech[len(ech)-maxEchantillons:]
			brut = brut[len(brut)-maxEchantillons:]
		}
		var prix any
		if p := ajusterPrix(ech); p != nil {
			prix = p
		}
		var entree objetJSON
		entree.mettre("echantillons", brut)
		entree.mettre("prix_par_million", prix)
		modeles.mettre(nom, entree)
	}
	modeles.garderDerniers(30)
	var sortie objetJSON
	sortie.mettre("modeles", modeles)
	ecrireTarifs(tarifs, sortie)
}

// modelUsageOrdonne: the modelUsage object of the log's last "result" line, its keys in their order;
// false when that line has none (or it is empty).
func modelUsageOrdonne(journal string) (objetJSON, bool) {
	var dernier objetJSON
	b, err := os.ReadFile(cheminOutil(journal))
	if err != nil {
		return dernier, false
	}
	for _, l := range strings.Split(strings.ToValidUTF8(string(b), "\uFFFD"), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "{") || !strings.Contains(l, `"result"`) {
			continue
		}
		var o struct {
			Type string          `json:"type"`
			MU   json.RawMessage `json:"modelUsage"`
		}
		if json.Unmarshal([]byte(l), &o) != nil || o.Type != "result" {
			continue
		}
		dernier = objetJSON{}
		if len(o.MU) > 0 && o.MU[0] == '{' {
			_ = json.Unmarshal(o.MU, &dernier)
		}
	}
	return dernier, len(dernier.cles) > 0
}

type tourModele struct {
	modele string
	usage  map[string]any
}

// messagesDe: {message id: (model, usage)} of a log, each message once (one line per content block, and the
// streamed partial lines: the last one has the final usage), in order.
func messagesDe(chemin string, chefSeul bool) (map[string]tourModele, []string) {
	messages := map[string]tourModele{}
	var ordre []string
	for _, o := range lignesJSON(chemin, 0) {
		if o["type"] != "assistant" || (chefSeul && o["parent_tool_use_id"] != nil) {
			continue
		}
		m := dict(o["message"])
		u := dict(m["usage"])
		if u == nil {
			u = map[string]any{}
		}
		modele, _ := texte(m["model"])
		if strings.HasPrefix(modele, "<") { // "<synthetic>": written by Claude Code, not a model turn
			continue
		}
		cle, ok := texte(m["id"])
		if !ok {
			cle = fmt.Sprintf("n%d", len(ordre))
		}
		if _, deja := messages[cle]; !deja {
			ordre = append(ordre, cle)
		}
		messages[cle] = tourModele{modele, u}
	}
	return messages, ordre
}

var reSession = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

func sessionsDe(journal string) []string {
	var vus []string
	for _, o := range lignesJSON(journal, 0) {
		sid, ok := texte(o["session_id"])
		if !ok || !reSession.MatchString(sid) {
			continue
		}
		deja := false
		for _, v := range vus {
			deja = deja || v == sid
		}
		if !deja {
			vus = append(vus, sid)
		}
	}
	if len(vus) > 5 {
		vus = vus[:5]
	}
	return vus
}

// transcriptsSousAgents: Claude Code keeps each sub-agent's turns in projects/<folder>/<session>/subagents.
func transcriptsSousAgents(journal, config string) []string {
	var out []string
	if config == "" {
		return out
	}
	base := cheminOutil(config)
	for _, sid := range sessionsDe(journal) {
		f, _ := filepath.Glob(filepath.Join(echapperGlob(base), "projects", "*", sid, "subagents", "agent-*.jsonl"))
		sort.Strings(f)
		if len(f) > 200 {
			f = f[:200]
		}
		for _, x := range f {
			if st, err := os.Lstat(x); err == nil && st.Mode().IsRegular() && st.Size() < 200*maxFichierCout {
				out = append(out, x)
			}
		}
	}
	return out
}

func echapperGlob(p string) string {
	r := strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]")
	return r.Replace(p)
}

func coutDe(messages map[string]tourModele, ordre []string, prix map[string][]float64, tot map[string]int64) (float64, bool) {
	cout, connu := 0.0, true
	for _, cle := range ordre {
		t := messages[cle]
		v := [4]int64{}
		for n, k := range clesJetons {
			v[n] = entierPositif(t.usage[k])
			if tot != nil {
				tot[k] += v[n]
			}
		}
		if p, ok := prix[t.modele]; ok && len(p) == 3 {
			x := variablesPrix(float64(v[0]), float64(v[1]), float64(v[2]), float64(v[3]))
			cout += (x[0]*p[0] + x[1]*p[1] + x[2]*p[2]) / 1e6
		} else if v[0]+v[1]+v[2]+v[3] > 0 {
			connu = false
		}
	}
	return cout, connu
}

func coutEstimer(tarifs, journal, config string) string {
	messages, ordre := messagesDe(journal, true)
	prix := map[string][]float64{}
	d := lireTarifs(tarifs)
	var modeles objetJSON
	if b, ok := d.valeur["modeles"]; ok && json.Unmarshal(b, &modeles) == nil {
		for _, k := range modeles.cles {
			var v struct {
				Prix []float64 `json:"prix_par_million"`
			}
			if json.Unmarshal(modeles.valeur[k], &v) == nil && v.Prix != nil {
				prix[k] = v.Prix
			}
		}
	}
	tot := map[string]int64{}
	for _, k := range clesJetons {
		tot[k] = 0
	}
	cout, connu := coutDe(messages, ordre, prix, tot)
	fichiers := transcriptsSousAgents(journal, config)
	for _, f := range fichiers {
		m, o := messagesDe(f, false)
		c, k := coutDe(m, o, prix, nil)
		cout += c
		connu = connu && k
	}
	var out objetJSON
	out.mettre("num_turns", len(ordre))
	var usage objetJSON
	for _, k := range clesJetons {
		usage.mettre(k, tot[k])
	}
	out.mettre("usage", usage)
	out.mettre("sous_agents", len(fichiers))
	if connu {
		out.mettre("total_cost_usd", math.RoundToEven(cout*1e4)/1e4)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func lancerCout(args []string) int {
	if (len(args) == 3 || len(args) == 4) && (args[0] == "apprendre" || args[0] == "estimer") {
		if args[0] == "apprendre" {
			coutApprendre(args[1], args[2])
			return 0
		}
		config := ""
		if len(args) == 4 {
			config = args[3]
		}
		fmt.Println(coutEstimer(args[1], args[2], config))
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: athanor cout apprendre TARIFS LOG | estimer TARIFS LOG [CONFIG]")
	return 2
}
