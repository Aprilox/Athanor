package main

// Several connections to Claude (Settings › Claude › Connections): subscriptions and API keys, each with a name, on
// or off, in an order of priority. They live in the tool folder's .env.connexions (0600; a ".env.*" name, so the
// guards keep it from Claude like .env). The first one on is copied into .env (CLAUDE_AUTH and its token or key):
// everything that reads .env (the chat, health, the terminal, configurer) goes on with it.
//
// A quota rule whose action is "bascule" (quota.go) makes the pilots skip a connection at its limit: at the threshold
// at the end of the cycle, at Claude's own refusal at once, until that window starts again (logs/connexions-etat.json,
// shared by every pilot: the quota is the account's). All of them at their limit: the pilot pauses until the first
// one is back. Without such a rule, the first connection on is used, as with a single one.

import (
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type ConnexionClaude struct {
	ID     string `json:"id"`
	Nom    string `json:"nom"`
	Type   string `json:"type"` // subscription, api
	Secret string `json:"secret"`
	Actif  bool   `json:"actif"`
	// its own limit (0: none): a subscription's percent of any window, an API key's dollars spent on it since
	// BudgetDepuis (set when the budget is first given, and by Count again from now)
	Seuil        int     `json:"seuil,omitempty"`
	Budget       float64 `json:"budget,omitempty"`
	BudgetDepuis int64   `json:"budget_depuis,omitempty"`
}

// limitePropre: a connection with a limit of its own: it switches to the next one at it, whatever the rules say.
func (c ConnexionClaude) limitePropre() bool { return c.Seuil > 0 || c.Budget > 0 }

// limitesPropres: a connection of the list has its own limit (the pilots switch connections then).
func limitesPropres(kit string) bool {
	return slices.ContainsFunc(lireListeConnexions(kit), ConnexionClaude.limitePropre)
}

// depenseConnexion: the dollars spent on the connection id since depuis (Unix seconds), from logs/depenses.tsv.
func depenseConnexion(kit, id string, depuis int64) float64 {
	b, err := os.ReadFile(fichierDepenses(kit))
	if err != nil || id == "" {
		return 0
	}
	total := 0.0
	for _, l := range strings.Split(string(b), "\n") {
		ch := strings.Split(strings.TrimSuffix(l, "\r"), "\t")
		if len(ch) < 6 || ch[5] != id {
			continue
		}
		t, err1 := strconv.ParseInt(ch[0], 10, 64)
		c, err2 := strconv.ParseFloat(ch[2], 64)
		if err1 == nil && err2 == nil && t >= depuis && c > 0 {
			total += c
		}
	}
	return math.Round(total*100) / 100
}

// budgetPropreAtteint: an API key's own budget spent.
func budgetPropreAtteint(kit string, c connexionClaude) bool {
	return c.api() && c.BudgetPropre > 0 && depenseConnexion(kit, c.ID, c.DepuisPropre) >= c.BudgetPropre
}

// EtatConnexion: a connection at its limit until Jusqua (Unix seconds), and why.
type EtatConnexion struct {
	Jusqua int64  `json:"jusqua"`
	Raison string `json:"raison"`
}

var reIDConnexion = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

func fichierConnexions(kit string) string { return filepath.Join(kit, ".env.connexions") }
func fichierEtatsConnexions(kit string) string {
	return filepath.Join(kit, "logs", "connexions-etat.json")
}

// lireListeConnexions: the list saved in Settings (nil when there is none yet).
func lireListeConnexions(kit string) []ConnexionClaude {
	var d struct {
		Liste []ConnexionClaude `json:"liste"`
	}
	lireJSONFichier(fichierConnexions(kit), &d)
	var l []ConnexionClaude
	for _, c := range d.Liste {
		if reIDConnexion.MatchString(c.ID) && (c.Type == modeConnexionAbonnement || c.Type == modeConnexionAPI) && c.Secret != "" {
			l = append(l, c)
		}
	}
	return l
}

// secretChoisi: the token or the key the connection uses (the other one of .env stays unused).
func (c connexionClaude) secretChoisi() string {
	if c.api() {
		return c.Cle
	}
	return c.Jeton
}

// connexionsEffectives: the list, with .env's own connection on top when .env holds one that is not the list's first
// on (put by the terminal, configurer or an older Athanor): it is never lost, and it is what the user chose last. No
// list: .env's connection alone (nil when .env has none either: Claude Code's own sign-in).
func connexionsEffectivesLignes(lignes []LigneEnv, kit string, origine map[string]string) []ConnexionClaude {
	l, env := lireListeConnexions(kit), connexionDesLignes(lignes, origine)
	if !env.presente() {
		return l
	}
	for _, x := range l {
		if x.Actif {
			if x.Type == env.Mode && x.Secret == env.secretChoisi() {
				return l
			}
			break
		}
	}
	return append([]ConnexionClaude{{ID: "env-" + env.empreinte(), Type: env.Mode, Secret: env.secretChoisi(), Actif: true}}, l...)
}

func connexionsEffectives(kit string, origine map[string]string) []ConnexionClaude {
	return connexionsEffectivesLignes(LireEnv(filepath.Join(kit, ".env")), kit, origine)
}

func lireEtatsConnexions(kit string) map[string]EtatConnexion {
	m := map[string]EtatConnexion{}
	lireJSONFichier(fichierEtatsConnexions(kit), &m)
	return m
}

// marquerConnexion: the connection id at its limit until jusqua (0: free again).
// shortcut: two pilots writing at the same instant keep one of the two marks; the other one is marked again at its
// next check (within a cycle). A lock file if it is ever seen.
func marquerConnexion(kit, id string, jusqua int64, raison string) {
	m := lireEtatsConnexions(kit)
	for k, e := range m {
		if e.Jusqua <= time.Now().Unix() {
			delete(m, k) // past: forgotten
		}
	}
	if jusqua > time.Now().Unix() {
		m[id] = EtatConnexion{Jusqua: jusqua, Raison: raison}
	} else {
		delete(m, id)
	}
	_ = ecrireJSONFichier(fichierEtatsConnexions(kit), m, 0o644)
}

// ChoixConnexion: the connection a run of Claude uses. ALimite: every connection allowed is at its limit (the
// current one is kept), Reprise: the earliest of their resets (0: unknown, an API budget spent).
type ChoixConnexion struct {
	Connexion connexionClaude
	ALimite   bool
	Reprise   int64
}

// choisirConnexion: the first connection on of the list among permises (the project's choice, nil: all of them; ids
// gone are ignored, and none left of its choice is all of them: a project is never left without one); with bascule
// (the rules switch, or a connection has its own limit), the first not at its limit (an API key: its own budget, or
// .env's budget of every API key, spent).
// actuel: the id the pilot runs on, kept when all of them are at their limit.
func choisirConnexion(lignes []LigneEnv, kit string, origine map[string]string, permises []string, bascule bool, actuel string) ChoixConnexion {
	base := connexionDesLignes(lignes, origine)
	if len(lireListeConnexions(kit)) == 0 {
		return ChoixConnexion{Connexion: base} // no list yet: .env's connection, as with a single one
	}
	var l []ConnexionClaude
	for _, c := range connexionsEffectivesLignes(lignes, kit, origine) {
		if c.Actif && (len(permises) == 0 || slices.Contains(permises, c.ID)) {
			l = append(l, c)
		}
	}
	if len(l) == 0 && len(permises) > 0 {
		return choisirConnexion(lignes, kit, origine, nil, bascule, actuel)
	}
	if len(l) == 0 { // every connection switched off: none, as asked
		base.Jeton, base.Cle = "", ""
		return ChoixConnexion{Connexion: base}
	}
	avec := func(c ConnexionClaude) connexionClaude {
		x := base
		x.ID, x.Nom, x.Mode, x.Jeton, x.Cle = c.ID, c.Nom, c.Type, "", ""
		x.SeuilPropre, x.BudgetPropre, x.DepuisPropre = c.Seuil, c.Budget, c.BudgetDepuis
		if c.Type == modeConnexionAPI {
			x.Cle = c.Secret
		} else {
			x.Jeton = c.Secret
		}
		return x
	}
	if !bascule && !slices.ContainsFunc(l, ConnexionClaude.limitePropre) {
		return ChoixConnexion{Connexion: avec(l[0])}
	}
	etats, maintenant := lireEtatsConnexions(kit), time.Now().Unix()
	garde, reprise := l[0], int64(0)
	for _, c := range l {
		x := avec(c)
		if e := etats[c.ID]; e.Jusqua > maintenant {
			if reprise == 0 || e.Jusqua < reprise {
				reprise = e.Jusqua
			}
		} else if a, _ := budgetAtteint(kit, x); !a && !budgetPropreAtteint(kit, x) {
			return ChoixConnexion{Connexion: x}
		}
		if c.ID == actuel {
			garde = c
		}
	}
	return ChoixConnexion{Connexion: avec(garde), ALimite: true, Reprise: reprise}
}

// basculeDesRegles: rules that switch connections at a limit (one window's is enough).
func basculeDesRegles(r map[string]regleSeuil) bool {
	for _, x := range r {
		if x.action == "bascule" {
			return true
		}
	}
	return false
}

// connexionsDuProjet: the connections the project of this folder may use (its own setting), nil for all of them.
func connexionsDuProjet(dossier string) []string {
	cf := lireConfig()
	nom := nomProjetDossier(cf, dossier)
	if nom == "" {
		return nil
	}
	if v := reglagePropre(cf, nom, "connexions"); v != "" {
		return strings.Split(v, ",")
	}
	return nil
}

// ----------------------------------------------------------------------------- each connection's own quota

// fichierConnexionsCycles: in a project's logs/, which connection ran each cycle (the cycle's log name without
// .jsonl → its id): the quota a log reports is that connection's.
func fichierConnexionsCycles(dossierLogs string) string {
	return filepath.Join(dossierLogs, "connexions.json")
}

// cleCycle: a cycle log's key there: its name without .jsonl, a parallel worker's under its lead's.
func cleCycle(journal string) string {
	n := strings.TrimSuffix(filepath.Base(journal), ".jsonl")
	if o := OuvrierDe(journal); o != "" {
		n = strings.TrimSuffix(n, "-"+o)
	}
	return n
}

// noterConnexionCycle: the pilot starts a cycle on the connection id (nothing without a list: a single connection).
func noterConnexionCycle(journal, id string) {
	if id == "" {
		return
	}
	f := fichierConnexionsCycles(filepath.Dir(journal))
	m := map[string]string{}
	lireJSONFichier(f, &m)
	m[cleCycle(journal)] = id
	if len(m) > 2*maxJournauxCycle { // the logs purged long ago: forgotten (their names sort by date)
		cles := slices.Sorted(maps.Keys(m))
		for _, k := range cles[:len(m)-2*maxJournauxCycle] {
			delete(m, k)
		}
	}
	_ = ecrireJSONFichier(f, m, 0o644)
}

var (
	quotasConnMu    sync.Mutex
	quotasConnCache map[string]*EtatQuota
	quotasConnQuand time.Time
)

// quotasParConnexion: each connection's latest quota report (connection id → quota), from the newest cycle logs of
// these projects; cached a few seconds like quotaActuel.
func quotasParConnexion(projets []Projet) map[string]*EtatQuota {
	quotasConnMu.Lock()
	defer quotasConnMu.Unlock()
	if time.Since(quotasConnQuand) < 8*time.Second {
		return quotasConnCache
	}
	quotasConnQuand = time.Now()
	out, cycles := map[string]*EtatQuota{}, map[string]map[string]string{}
	for i, j := range journauxCycleRecents(projets) {
		if i >= 60 { // a connection with no cycle among the last 60 has no recent report
			break
		}
		d := filepath.Dir(j.chemin)
		if cycles[d] == nil {
			m := map[string]string{}
			lireJSONFichier(fichierConnexionsCycles(d), &m)
			cycles[d] = m
		}
		if id := cycles[d][cleCycle(j.chemin)]; id != "" && out[id] == nil {
			if q := quotaDuJournal(j.chemin); q != nil {
				q.Lu = j.date.Unix()
				out[id] = q
			}
		}
	}
	quotasConnCache = out
	return out
}

// QuotaConnexion: Overview › Subscription quota, one per subscription on when there are several.
type QuotaConnexion struct {
	ID       string     `json:"id"`
	Nom      string     `json:"nom"`
	Utilisee bool       `json:"utilisee,omitempty"`
	Jusqua   int64      `json:"jusqua,omitempty"`
	Quota    *EtatQuota `json:"quota,omitempty"`
}

func (s *Serveur) quotasConnexions(projets []Projet) []QuotaConnexion {
	var l []QuotaConnexion
	for _, c := range s.vueConnexions() {
		if c.Type == modeConnexionAbonnement && c.Actif {
			l = append(l, QuotaConnexion{ID: c.ID, Nom: c.Nom, Utilisee: c.Utilisee, Jusqua: c.Jusqua})
		}
	}
	if len(l) < 2 {
		return nil // a single subscription: the Overview's quota is its own
	}
	q := quotasParConnexion(projets)
	for i := range l {
		l[i].Quota = q[l[i].ID]
	}
	return l
}

// ----------------------------------------------------------------------------- Settings › Claude › Connections

// ConnexionVue: a connection as the page shows it: never its secret, its last 4 characters.
type ConnexionVue struct {
	ID       string  `json:"id"`
	Nom      string  `json:"nom"`
	Type     string  `json:"type"`
	Apercu   string  `json:"apercu"`
	Actif    bool    `json:"actif"`
	Fichier  bool    `json:"fichier,omitempty"` // .env's own (changed outside this list)
	Jusqua   int64   `json:"jusqua,omitempty"`
	Raison   string  `json:"raison,omitempty"`
	Utilisee bool    `json:"utilisee,omitempty"` // the one a run of Claude would take now
	Seuil    int     `json:"seuil,omitempty"`
	Budget   float64 `json:"budget,omitempty"`
	Depense  float64 `json:"depense,omitempty"` // an API key: spent on it since its budget's count started
}

func (s *Serveur) vueConnexions() []ConnexionVue {
	kit := s.kitCourant().Racine
	lignes := LireEnv(filepath.Join(kit, ".env"))
	choix := choisirConnexion(lignes, kit, nil, nil, basculeDesRegles(reglesSeuils(fichierReglesQuota(s.kitCourant()))), "")
	etats, maintenant, sauvee := lireEtatsConnexions(kit), time.Now().Unix(), lireListeConnexions(kit)
	l := []ConnexionVue{}
	for _, c := range connexionsEffectivesLignes(lignes, kit, nil) {
		v := ConnexionVue{ID: c.ID, Nom: c.Nom, Type: c.Type, Actif: c.Actif,
			Fichier:  len(sauvee) > 0 && !slices.ContainsFunc(sauvee, func(x ConnexionClaude) bool { return x.ID == c.ID }),
			Utilisee: c.Actif && c.Type == choix.Connexion.Mode && c.Secret == choix.Connexion.secretChoisi()}
		if n := utf8.RuneCountInString(c.Secret); n > 8 {
			v.Apercu = "…" + string([]rune(c.Secret)[n-4:])
		}
		v.Seuil, v.Budget = c.Seuil, c.Budget
		if c.Type == modeConnexionAPI {
			v.Depense = depenseConnexion(kit, c.ID, c.BudgetDepuis)
		}
		if e := etats[c.ID]; e.Jusqua > maintenant {
			v.Jusqua, v.Raison = e.Jusqua, e.Raison
		}
		l = append(l, v)
	}
	return l
}

// apiConnexions: GET /api/connexions, the list without its secrets.
func (s *Serveur) apiConnexions(w http.ResponseWriter, r *http.Request) {
	s.ecrireJSON(w, map[string]any{"liste": s.vueConnexions()})
}

// apiConnexionsModifier: POST /api/connexions {action: ajouter (nom, type, secret) | modifier (id, nom?, actif?, secret?) |
// supprimer | monter | descendre | liberer (id)}. The first write takes .env's connection (and its unused token or
// key, switched off) into the list: nothing is lost. Then the first one on is copied into .env.
func (s *Serveur) apiConnexionsModifier(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Action string   `json:"action"`
		ID     string   `json:"id"`
		Nom    *string  `json:"nom"`
		Type   string   `json:"type"`
		Secret string   `json:"secret"`
		Actif  *bool    `json:"actif"`
		Seuil  *int     `json:"seuil"`  // 0: as the rules
		Budget *float64 `json:"budget"` // 0: none
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	c.Secret = strings.TrimSpace(c.Secret)
	if c.Nom != nil {
		*c.Nom = strings.TrimSpace(*c.Nom)
	}
	if (c.Nom != nil && (utf8.RuneCountInString(*c.Nom) > 60 || strings.ContainsAny(*c.Nom, "\r\n"))) || strings.ContainsAny(c.Secret, "\r\n\t ") || len(c.Secret) > 600 ||
		c.Seuil != nil && (*c.Seuil < 0 || *c.Seuil > 100) || c.Budget != nil && !(*c.Budget >= 0 && *c.Budget <= 1e6) {
		s.erreur(w, http.StatusBadRequest, "Invalid setting")
		return
	}
	kit := s.kitCourant().Racine
	connexionsMu.Lock()
	defer connexionsMu.Unlock()
	l := listeAEcrire(kit)
	i := slices.IndexFunc(l, func(x ConnexionClaude) bool { return x.ID == c.ID })
	if c.Action != "ajouter" && i < 0 {
		s.erreur(w, http.StatusNotFound, "Unknown connection")
		return
	}
	switch c.Action {
	case "ajouter":
		if c.Type != modeConnexionAbonnement && c.Type != modeConnexionAPI || c.Secret == "" {
			s.erreur(w, http.StatusBadRequest, "Invalid setting")
			return
		}
		x := ConnexionClaude{ID: aleatoire(4), Type: c.Type, Secret: c.Secret, Actif: true}
		if c.Nom != nil {
			x.Nom = *c.Nom
		}
		l = append(l, x)
	case "modifier":
		if c.Nom != nil {
			l[i].Nom = *c.Nom
		}
		if c.Actif != nil {
			l[i].Actif = *c.Actif
		}
		if c.Seuil != nil {
			l[i].Seuil = *c.Seuil
		}
		if c.Budget != nil { // a first budget counts from now; a budget changed later keeps its count
			if l[i].Budget == 0 && *c.Budget > 0 {
				l[i].BudgetDepuis = time.Now().Unix()
			}
			l[i].Budget = math.Round(*c.Budget*100) / 100
		}
		if c.Secret != "" { // a new token or key (renewed, another account): its old limit is not its own
			l[i].Secret = c.Secret
			marquerConnexion(kit, l[i].ID, 0, "")
		}
	case "supprimer":
		l = slices.Delete(l, i, i+1)
	case "monter", "descendre":
		j := i - 1
		if c.Action == "descendre" {
			j = i + 1
		}
		if j >= 0 && j < len(l) {
			l[i], l[j] = l[j], l[i]
		}
	case "liberer":
		marquerConnexion(kit, c.ID, 0, "")
	case "zero": // credit added to that key: its spend counted again from now
		l[i].BudgetDepuis = time.Now().Unix()
		marquerConnexion(kit, c.ID, 0, "")
	default:
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := ecrireConnexions(kit, l); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "connexion Claude: " + c.Action, Resultat: c.ID})
	s.ecrireJSON(w, map[string]any{"message_key": "Connections saved: the running pilots take them at once.", "args": []string{}, "liste": s.vueConnexions()})
}

var connexionsMu sync.Mutex

// listeAEcrire: the list as it stands, .env's own connection included as a real one; the very first time, .env's
// unused token or key comes too, switched off.
func listeAEcrire(kit string) []ConnexionClaude {
	lignes := LireEnv(filepath.Join(kit, ".env"))
	l := connexionsEffectivesLignes(lignes, kit, nil)
	if len(lireListeConnexions(kit)) == 0 {
		env := connexionDesLignes(lignes, nil)
		autre := ConnexionClaude{Type: modeConnexionAPI, Secret: env.Cle}
		if env.api() {
			autre = ConnexionClaude{Type: modeConnexionAbonnement, Secret: env.Jeton}
		}
		if autre.Secret != "" {
			autre.ID = aleatoire(4)
			l = append(l, autre)
		}
	}
	return l
}

// ecrireConnexions: the list saved, then its first connection on copied into .env (none on: .env's token and key
// emptied, the pilots wait for one).
func ecrireConnexions(kit string, l []ConnexionClaude) error {
	if err := ecrireJSONFichier(fichierConnexions(kit), map[string]any{"liste": l}, 0o600); err != nil {
		return err
	}
	v := map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "", "ANTHROPIC_API_KEY": ""}
	if i := slices.IndexFunc(l, func(x ConnexionClaude) bool { return x.Actif }); i >= 0 {
		v["CLAUDE_AUTH"] = l[i].Type
		if l[i].Type == modeConnexionAPI {
			v["ANTHROPIC_API_KEY"] = l[i].Secret
		} else {
			v["CLAUDE_CODE_OAUTH_TOKEN"] = l[i].Secret
		}
	}
	k := Kit{Racine: kit}
	if !existeP(k.Env()) {
		if b, err := os.ReadFile(k.EnvExemple()); err == nil {
			_ = os.WriteFile(k.Env(), b, 0o600)
		}
	}
	return EcrireEnv(k.Env(), v)
}

// connexionCourante: the connection a short run of Claude takes now (the chat, health, Improve): the first one on,
// the first not at its limit when the connections switch.
func connexionCourante(kit string) connexionClaude {
	r := reglesSeuils(filepath.Join(kit, "logs", "quota-arret.json"))
	return choisirConnexion(LireEnv(filepath.Join(kit, ".env")), kit, nil, nil, basculeDesRegles(r), "").Connexion
}
