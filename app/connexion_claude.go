package main

// How Athanor connects to Claude (Settings › Claude › Connection): the subscription (its token,
// CLAUDE_CODE_OAUTH_TOKEN) or an API key (ANTHROPIC_API_KEY, paid per use), chosen by CLAUDE_AUTH. Only the
// chosen one reaches Claude Code: with both set, Claude Code would take the API key and bill it.
//
// The pilot reads the connection again at each cycle and every few seconds of its waits: a new token or a
// new key put in Settings (another account, a key with credit left) is used at once, the wait cut short.
//
// API mode: what each Claude Code run cost (its total_cost_usd) is written into the tool's
// logs/depenses.tsv; with a budget (API_BUDGET_USD), the pilot waits once it is spent, counted from
// API_BUDGET_SINCE (set when the budget or the key is saved, and by Overview › API spend › Count again).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	modeConnexionAbonnement = "subscription"
	modeConnexionAPI        = "api"
)

// the settings of the connection, in .env
var clesConnexionClaude = []string{"CLAUDE_AUTH", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "API_BUDGET_USD", "API_BUDGET_SINCE"}

type connexionClaude struct {
	Mode   string  // subscription, api
	Jeton  string  // the subscription's token
	Cle    string  // the API key
	Budget float64 // API mode: the most to spend, in dollars (0: no limit)
	Depuis int64   // API mode: the spend counted from (Unix seconds; 0: from the first one written)
}

func (c connexionClaude) api() bool { return c.Mode == modeConnexionAPI }

// presente: something to connect with (the subscription can also be the sign-in of Claude Code itself,
// its .credentials.json: see the pilot's connexionPresente).
func (c connexionClaude) presente() bool {
	if c.api() {
		return c.Cle != ""
	}
	return c.Jeton != ""
}

// empreinte: what tells a change of connection (never shown, never written).
func (c connexionClaude) empreinte() string {
	h := sha256.Sum256([]byte(c.Mode + "\x00" + c.Jeton + "\x00" + c.Cle))
	return hex.EncodeToString(h[:8])
}

// nom: the connection, for the log and the screens.
func (c connexionClaude) nom() string {
	if c.api() {
		return "API key"
	}
	return "subscription token"
}

// origineConnexion: the connection's variables as the process got them (before .env is read): what stands
// when .env says nothing (a token given in the environment).
func origineConnexion() map[string]string {
	o := map[string]string{}
	for _, k := range clesConnexionClaude {
		if v, ok := os.LookupEnv(k); ok {
			o[k] = v
		}
	}
	return o
}

// lireConnexionClaude: the connection of the tool's .env (else of origine).
func lireConnexionClaude(kit string, origine map[string]string) connexionClaude {
	v := map[string]string{}
	for k, x := range origine {
		v[k] = x
	}
	for _, l := range LireEnv(filepath.Join(kit, ".env")) {
		for _, k := range clesConnexionClaude {
			if l.Cle == k && strings.TrimSpace(l.Valeur) != "" {
				v[k] = strings.TrimSpace(l.Valeur)
			}
		}
	}
	c := connexionClaude{Mode: modeConnexionAbonnement, Jeton: v["CLAUDE_CODE_OAUTH_TOKEN"], Cle: v["ANTHROPIC_API_KEY"]}
	if strings.EqualFold(v["CLAUDE_AUTH"], modeConnexionAPI) {
		c.Mode = modeConnexionAPI
	}
	c.Budget = lireMontant(v["API_BUDGET_USD"])
	c.Depuis, _ = strconv.ParseInt(v["API_BUDGET_SINCE"], 10, 64)
	return c
}

// lireMontant: "100", "100.50", "100,50 $", "$100": the amount; 0 when it is none.
func lireMontant(s string) float64 {
	s = strings.TrimSpace(strings.NewReplacer("$", "", "USD", "", "usd", "", " ", "", ",", ".").Replace(s))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	return f
}

// environnement: env with only the chosen connection (the other one taken out).
func (c connexionClaude) environnement(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if strings.EqualFold(k, "CLAUDE_CODE_OAUTH_TOKEN") || strings.EqualFold(k, "ANTHROPIC_API_KEY") {
			continue
		}
		out = append(out, e)
	}
	if c.api() {
		if c.Cle != "" {
			out = append(out, "ANTHROPIC_API_KEY="+c.Cle)
		}
	} else if c.Jeton != "" {
		out = append(out, "CLAUDE_CODE_OAUTH_TOKEN="+c.Jeton)
	}
	return out
}

// appliquerConnexionProcessus: this process's environment with only the chosen connection (the short
// runs: chat, health, Fix with Claude; they read .env when they start).
func appliquerConnexionProcessus(kit string, origine map[string]string) {
	c := lireConnexionClaude(kit, origine)
	_ = os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	if c.api() {
		if c.Cle != "" {
			_ = os.Setenv("ANTHROPIC_API_KEY", c.Cle)
		}
	} else if c.Jeton != "" {
		_ = os.Setenv("CLAUDE_CODE_OAUTH_TOKEN", c.Jeton)
	}
}

// ----------------------------------------------------------------------------- the spend (API mode)

func fichierDepenses(kit string) string { return filepath.Join(kit, "logs", "depenses.tsv") }

var depensesMu sync.Mutex

// noterDepense: what a run of Claude Code cost (its total_cost_usd), with the connection it ran on.
func noterDepense(kit, mode string, cout float64, projet, origine string) {
	if cout <= 0 || math.IsNaN(cout) || math.IsInf(cout, 0) || kit == "" {
		return
	}
	propre := func(s string) string { return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s) }
	depensesMu.Lock()
	defer depensesMu.Unlock()
	_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	f, err := os.OpenFile(fichierDepenses(kit), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%d\t%s\t%s\t%s\t%s\n", time.Now().Unix(), propre(mode), strconv.FormatFloat(cout, 'f', 4, 64), propre(projet), propre(origine))
}

// depenseAPI: the dollars spent in API mode since depuis (Unix seconds), and how many runs.
func depenseAPI(kit string, depuis int64) (float64, int) {
	f, err := os.Open(fichierDepenses(kit))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	total, n := 0.0, 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		ch := strings.Split(sc.Text(), "\t")
		if len(ch) < 3 || ch[1] != modeConnexionAPI {
			continue
		}
		t, err1 := strconv.ParseInt(ch[0], 10, 64)
		c, err2 := strconv.ParseFloat(ch[2], 64)
		if err1 != nil || err2 != nil || t < depuis || c <= 0 {
			continue
		}
		total += c
		n++
	}
	return math.Round(total*100) / 100, n
}

// EtatDepense: Overview › API spend (in place of the subscription's quota in API mode).
type EtatDepense struct {
	Depense float64 `json:"depense"`          // dollars, since Depuis
	Budget  float64 `json:"budget,omitempty"` // 0: no limit
	Depuis  int64   `json:"depuis,omitempty"`
	Runs    int     `json:"runs"`
	Atteint bool    `json:"atteint,omitempty"`
	Cle     bool    `json:"cle"` // an API key is set
}

func etatDepenseKit(kit string) *EtatDepense {
	c := lireConnexionClaude(kit, nil)
	if !c.api() {
		return nil
	}
	d, n := depenseAPI(kit, c.Depuis)
	return &EtatDepense{Depense: d, Budget: c.Budget, Depuis: c.Depuis, Runs: n, Atteint: c.Budget > 0 && d >= c.Budget, Cle: c.Cle != ""}
}

// budgetAtteint: API mode with a budget, and it is spent (the dollars spent, the budget).
func budgetAtteint(kit string, c connexionClaude) (bool, float64) {
	if !c.api() || c.Budget <= 0 {
		return false, 0
	}
	d, _ := depenseAPI(kit, c.Depuis)
	return d >= c.Budget, d
}

// coutJournalClaude: the total_cost_usd of the last result of a stream-json log (0 when none).
func coutJournalClaude(chemin string) float64 {
	cout := 0.0
	for _, o := range lignesJSON(chemin, 0) {
		if o["type"] == "result" {
			if c, ok := nombreJSON(o["total_cost_usd"]); ok {
				cout = c
			}
		}
	}
	return cout
}

// reglagesConnexionEnregistres: what saving Settings adds: a new API key, or a first budget, starts the
// count again (a new key is new credit; a first budget is the credit left from now on). A budget raised
// later keeps the count (credit added: Overview › API spend › Count again from now, when wanted).
func reglagesConnexionEnregistres(kit string, valeurs map[string]string) {
	avant := lireConnexionClaude(kit, nil)
	nouvelleCle := false
	if v, ok := valeurs["ANTHROPIC_API_KEY"]; ok && strings.TrimSpace(v) != "" && strings.TrimSpace(v) != avant.Cle {
		nouvelleCle = true
	}
	premierBudget := false
	if v, ok := valeurs["API_BUDGET_USD"]; ok && lireMontant(v) > 0 && avant.Depuis == 0 {
		premierBudget = true
	}
	if nouvelleCle || premierBudget {
		valeurs["API_BUDGET_SINCE"] = strconv.FormatInt(time.Now().Unix(), 10)
	}
}

func init() {
	enregistrerCommande("depense", "depense [KIT] [zero]: the connection to Claude (subscription or API key) and, in API mode, the spend against the budget; zero counts it again from now", lancerDepense)
}

// lancerDepense: athanor depense [KIT] [zero] (the terminal's Overview › API spend).
func lancerDepense(args []string) int {
	zero := false
	kit := ""
	for _, a := range args {
		if a == "zero" || a == "--zero" {
			zero = true
		} else if kit == "" {
			kit = a
		}
	}
	if kit == "" {
		if exe, err := os.Executable(); err == nil {
			kit = filepath.Dir(exe)
		}
	}
	kit = abspathOS(cheminNatifPilote(kit))
	if !existeP(filepath.Join(kit, ".env")) {
		fmt.Fprintln(os.Stderr, "No settings in "+kit+" (.env): athanor configurer, or Settings in the app.")
		return 1
	}
	if zero {
		if err := EcrireEnv(filepath.Join(kit, ".env"), map[string]string{"API_BUDGET_SINCE": strconv.FormatInt(time.Now().Unix(), 10)}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("API spend counted again from now.")
	}
	c := lireConnexionClaude(kit, nil)
	if !c.api() {
		etat := "token set"
		if c.Jeton == "" {
			etat = "no token in .env (Claude Code's own sign-in, if any)"
		}
		fmt.Printf("Connection: Claude subscription (%s). The quota is the plan's: Overview › Subscription quota.\n", etat)
		return 0
	}
	if c.Cle == "" {
		fmt.Println("Connection: API key (paid per use), but no key is set: ANTHROPIC_API_KEY in Settings.")
	} else {
		fmt.Println("Connection: API key (paid per use).")
	}
	d, n := depenseAPI(kit, c.Depuis)
	depuis := "since Athanor 2.49"
	if c.Depuis > 0 {
		depuis = "since " + time.Unix(c.Depuis, 0).Format("2006-01-02 15:04")
	}
	if c.Budget > 0 {
		fmt.Printf("Spent: %.2f $ of %.2f $ (%.0f %%), %.2f $ left, %s, %d runs of Claude.\n", d, c.Budget, math.Min(100, d/c.Budget*100), math.Max(0, c.Budget-d), depuis, n)
		if d >= c.Budget {
			fmt.Println("Budget spent: the pilot waits (raise API_BUDGET_USD, count again with: athanor depense zero, or put another key).")
		}
	} else {
		fmt.Printf("Spent: %.2f $, %s, %d runs of Claude (no budget: API_BUDGET_USD).\n", d, depuis, n)
	}
	return 0
}
