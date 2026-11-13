package main

// The mission card (ARCHITECTURE §Missions et orchestrateur): what the orchestrator proposes, checked by
// validerCarte; the planned approvals, what will be touched and the estimate are worked out by calculerCarte,
// never taken from the agent. Both are pure (contexteCarte is a snapshot), so they are tested without HTTP.

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
)

type ProjetCarte struct {
	Nom     string `json:"nom"`
	Nouveau bool   `json:"nouveau"`
}

type BriefCarte struct {
	Brief string      `json:"brief"`
	Modes *ModesCarte `json:"modes,omitempty"` // Dev, a new project only: its working modes (as Manage › Working mode)
}

// ModesCarte: the three independent switches of a new project, written at the launch by Projet.ecrireMode.
type ModesCarte struct {
	Parallele bool `json:"parallele"`
	Eco       bool `json:"eco"`
	Rapide    bool `json:"rapide"`
}

// nomsModes: "parallel, eco, fast" (the switched-on ones), "usual" when none, for the mission's history.
func nomsModes(m ModesCarte) string {
	var l []string
	for _, x := range []struct {
		on  bool
		nom string
	}{{m.Parallele, "parallel"}, {m.Eco, "eco"}, {m.Rapide, "fast"}} {
		if x.on {
			l = append(l, x.nom)
		}
	}
	if len(l) == 0 {
		return "usual"
	}
	return strings.Join(l, ", ")
}

type EnvCarte struct {
	Nom        string   `json:"nom"` // dev | staging | prod
	URL        string   `json:"url"`
	Protection string   `json:"protection"` // aucune | auth_proxy | access
	Serveurs   []string `json:"serveurs"`   // inventory ids
	// worked out by calculerCarte (C81): the policy the launch writes, and the existing environment when the
	// mission changes it; Proprietaire: the owner gave this environment in Modify (never the agent)
	Politique    string    `json:"politique,omitempty"`
	Avant        *EnvCarte `json:"avant,omitempty"`
	Proprietaire bool      `json:"proprietaire,omitempty"`
	Plancher     string    `json:"plancher,omitempty"` // R5: "accord" when the existing environment already asked; no policy change goes under it
}

// AccesCarte: a vault entry the mission uses, by name; ID, Type and Portee come from the vault, ARemplir says
// the entry does not exist yet (the owner fills it, the agent never creates one); Decoche is the owner's.
type AccesCarte struct {
	Entree   string `json:"entree"`
	Pour     string `json:"pour"` // the project, "project/env" or a server id of the card
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Portee   string `json:"portee,omitempty"`
	ARemplir bool   `json:"a_remplir,omitempty"`
	Decoche  bool   `json:"decoche,omitempty"`
}

// ApprobationPrevue: Type mise_en_prod | plan_serveurs | deploiement | durcissement | deverrouiller_coffre.
type ApprobationPrevue struct {
	Type  string `json:"type"`
	Cible string `json:"cible,omitempty"`
}

type ToucheCarte struct {
	Serveurs []string `json:"serveurs"` // readable names
	DNS      []string `json:"dns"`
	Zones    []string `json:"zones"`
	Fichiers []string `json:"fichiers"`
	Dossier  string   `json:"dossier,omitempty"` // the project's folder (a new one: where it will be created)
}

// EstimationCarte: a range; Mesure false = no stats.tsv on this PC (25 min a cycle, cost unknown).
type EstimationCarte struct {
	Jalons     int     `json:"jalons"`
	MinutesMin int     `json:"minutes_min"`
	MinutesMax int     `json:"minutes_max"`
	CoutMin    float64 `json:"cout_min,omitempty"` // list price in USD, the measure of the quota stats.tsv gives
	CoutMax    float64 `json:"cout_max,omitempty"`
	Mesure     bool    `json:"mesure"`
}

type CarteMission struct {
	Titre         string       `json:"titre"`
	Projet        ProjetCarte  `json:"projet"`
	Dev           BriefCarte   `json:"dev"`
	Envs          []EnvCarte   `json:"envs"`
	Serveurs      BriefCarte   `json:"serveurs"`
	Acces         []AccesCarte `json:"acces"`
	JalonsEstimes int          `json:"jalons_estimes"`
	// worked out by calculerCarte: whatever the agent sends here is replaced
	Approbations          []ApprobationPrevue `json:"approbations"`
	Touche                ToucheCarte         `json:"touche"`
	Estimation            EstimationCarte     `json:"estimation"`
	AvertissementServeurs string              `json:"avertissement_serveurs,omitempty"`
	AvertissementInfra    string              `json:"avertissement_infra,omitempty"` // C80: an infra entry ticked in native mode
}

// contexteCarte: what the card is checked against, read once (s.contexteCarte).
type contexteCarte struct {
	projets      []Projet
	inventaire   []ServeurInventaire
	hotes        map[string]string // host name → "project/env", every configured environment
	entrees      []EntreeCoffre    // without values
	zones        []string          // the providers' DNS zones
	infraOuvert  bool
	natif        bool
	envs         map[string]map[string]Environnement // project → its configured environments
	forcerInfra  map[string]bool                     // project → infra entries forced in native mode
	minutesCycle float64                             // measured average, 0 = unknown
	coutCycle    float64
	masquer      func(string) string
	regle        func(RegleAccord) bool // the owner's "never ask again" rules in place (nil: none)
}

const (
	maxTitreCarte  = 120
	maxBriefCarte  = 4000
	maxAccesCarte  = 20
	maxEntreeCarte = 100
	minutesDefaut  = 25.0
)

var politiquesMission = []string{"prod", "tout", "autonome"}

// validerCarte checks c in place against x: texts trimmed, masked and bounded, the project, the environments
// (validerEnvironnement: URL, protection, servers and their roles; a host taken elsewhere is refused) and the
// vault entries it names (resolved, or flagged to fill).
func validerCarte(c *CarteMission, x contexteCarte) error {
	masquer := x.masquer
	if masquer == nil {
		masquer = func(t string) string { return t }
	}
	texte := func(t string) string { return strings.TrimSpace(masquer(t)) }
	c.Titre, c.Dev.Brief, c.Serveurs.Brief = texte(c.Titre), texte(c.Dev.Brief), texte(c.Serveurs.Brief)
	c.Projet.Nom = strings.TrimSpace(c.Projet.Nom)
	switch {
	case c.Titre == "" || len([]rune(c.Titre)) > maxTitreCarte || strings.ContainsAny(c.Titre, "\r\n"):
		return erreurEntree(fmt.Sprintf("The card needs a title of one line, %d characters at most", maxTitreCarte))
	case len([]rune(c.Dev.Brief)) > maxBriefCarte || len([]rune(c.Serveurs.Brief)) > maxBriefCarte:
		return erreurEntree(fmt.Sprintf("A lane's brief is %d characters at most", maxBriefCarte))
	case c.Dev.Brief == "" && c.Serveurs.Brief == "":
		return erreurEntree("The card needs at least one lane: a Dev brief or a Servers brief")
	case c.JalonsEstimes < 1 || c.JalonsEstimes > 60:
		return erreurEntree("jalons_estimes is between 1 and 60")
	case len(c.Envs) > len(envsProjet):
		return erreurEntree("One environment of each kind at most: dev, staging, prod")
	case len(c.Acces) > maxAccesCarte:
		return erreurEntree(fmt.Sprintf("%d vault entries at most", maxAccesCarte))
	}
	i := slices.IndexFunc(x.projets, func(p Projet) bool { return strings.EqualFold(p.Nom, c.Projet.Nom) })
	switch {
	case c.Projet.Nouveau && !reNomProjet.MatchString(c.Projet.Nom):
		return erreurEntree("Invalid project name (letters, digits, spaces, dashes, 60 characters at most)")
	case c.Projet.Nouveau && i >= 0:
		return erreurEntreeArgs{"A project named {0} already exists: set nouveau to false to work on it", []string{x.projets[i].Nom}}
	case c.Projet.Nouveau && c.Dev.Brief == "":
		return erreurEntree("A new project needs a Dev brief")
	case !c.Projet.Nouveau && c.Dev.Modes != nil: // an existing project's modes are the owner's, in Manage
		return erreurEntree("Working modes are chosen for a new project only")
	case c.Serveurs.Modes != nil:
		return erreurEntree("Working modes belong to the Dev lane")
	case !c.Projet.Nouveau && (i < 0 || x.projets[i].Archive):
		return erreurEntreeArgs{"There is no project named {0}: give an existing one, or set nouveau to true", []string{c.Projet.Nom}}
	case !c.Projet.Nouveau:
		c.Projet.Nom = x.projets[i].Nom
	}
	vus, avecServeurs := map[string]bool{}, false
	for k := range c.Envs {
		e := &c.Envs[k]
		if vus[e.Nom] {
			return erreurEntreeArgs{"The environment {0} is given twice", []string{e.Nom}}
		}
		vus[e.Nom] = true
		v, err := validerEnvironnement(e.Nom, Environnement{URL: e.URL, Protection: e.Protection, Serveurs: e.Serveurs, Politique: "accord"}, x.inventaire)
		if err != nil {
			return err
		}
		e.URL, e.Protection, e.Serveurs, e.Politique, e.Avant = v.URL, v.Protection, v.Serveurs, "", nil
		h := hoteURL(e.URL)
		if h == "" {
			continue
		}
		if a, ok := x.hotes[h]; ok && a != c.Projet.Nom+"/"+e.Nom {
			return erreurEntreeArgs{errEnvHotePris, []string{h, a}}
		}
		for _, autre := range c.Envs[:k] {
			if hoteURL(autre.URL) == h {
				return erreurEntreeArgs{errEnvHotePris, []string{h, c.Projet.Nom + "/" + autre.Nom}}
			}
		}
	}
	for _, e := range c.Envs {
		avecServeurs = avecServeurs || len(e.Serveurs) > 0
	}
	if c.Serveurs.Brief != "" && !avecServeurs {
		return erreurEntree("The Servers lane needs an environment with at least one server")
	}
	for k := range c.Acces {
		a := &c.Acces[k]
		a.Entree, a.Pour = strings.TrimSpace(a.Entree), strings.TrimSpace(a.Pour)
		if a.Entree == "" || len([]rune(a.Entree)) > maxEntreeCarte {
			return erreurEntree(fmt.Sprintf("A vault entry is named, in %d characters at most", maxEntreeCarte))
		}
		if !pourValide(a.Pour, c) {
			return erreurEntreeArgs{"pour of {0} is the project, project/env of the card or one of its servers", []string{a.Entree}}
		}
		a.ID, a.Type, a.Portee, a.ARemplir = "", "", "", true
		j := slices.IndexFunc(x.entrees, func(e EntreeCoffre) bool { return e.Nom == a.Entree })
		if j < 0 {
			j = slices.IndexFunc(x.entrees, func(e EntreeCoffre) bool { return strings.EqualFold(e.Nom, a.Entree) })
		}
		if j >= 0 {
			a.Entree, a.ID, a.Type, a.Portee, a.ARemplir = x.entrees[j].Nom, x.entrees[j].ID, x.entrees[j].Type, x.entrees[j].Portee, false
		}
	}
	acces := c.Acces[:0] // B83: one line per (entry, target), unticked when any copy was
	for _, a := range c.Acces {
		if k := slices.IndexFunc(acces, func(b AccesCarte) bool { return memeAcces(a, b) }); k >= 0 {
			acces[k].Decoche = acces[k].Decoche || a.Decoche
			continue
		}
		acces = append(acces, a)
	}
	c.Acces = acces
	return nil
}

func pourValide(pour string, c *CarteMission) bool {
	if pour == c.Projet.Nom {
		return true
	}
	for _, e := range c.Envs {
		if pour == c.Projet.Nom+"/"+e.Nom || slices.Contains(e.Serveurs, pour) {
			return true
		}
	}
	return false
}

// hoteURL: the host name of a URL already checked by validerURLEnv, "" without one.
func hoteURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Hostname())
}

// calculerCarte: c (validated) with what the Go works out for the mission's policy; the agent's values of
// these fields are dropped.
func calculerCarte(c CarteMission, politique string, x contexteCarte) CarteMission {
	if !slices.Contains(politiquesMission, politique) {
		politique = "prod" // the default policy
	}
	c.Approbations, c.Touche, c.AvertissementServeurs = []ApprobationPrevue{}, ToucheCarte{Serveurs: []string{}, DNS: []string{}, Zones: []string{}, Fichiers: []string{}}, ""
	ajouter := func(l *[]string, v string) {
		if v != "" && !slices.Contains(*l, v) {
			*l = append(*l, v)
		}
	}
	c.Envs = slices.Clone(c.Envs)
	for k := range c.Envs {
		c.Envs[k] = envSansAffaiblir(c.Envs[k], politique, x.envs[c.Projet.Nom], c.Projet.Nouveau)
	}
	nomServeur := map[string]ServeurInventaire{}
	for _, sv := range x.inventaire {
		nomServeur[sv.ID] = sv
	}
	// C84: the accords asked as at the run: a rule in place lets through what it covers, except in a tout
	// mission (C82: its launch ends the deployment rules, its plans ignore the servers' rules) and for prod
	regle := func(r RegleAccord) bool { return politique != "tout" && x.regle != nil && x.regle(r) }
	var durcir []string
	for _, e := range c.Envs {
		if e.Nom == "prod" {
			c.Approbations = append(c.Approbations, ApprobationPrevue{"mise_en_prod", e.URL})
		}
		if len(e.Serveurs) == 0 {
			continue
		}
		// shortcut: a plan under the servers' rules still asks when it is destructive or builds; its steps are not known yet
		if c.Serveurs.Brief != "" && (e.Nom == "prod" || slices.ContainsFunc(e.Serveurs, func(id string) bool {
			return slices.Contains(nomServeur[id].Roles, "prod") || !regle(reglePlan(prefixeTache, id))
		})) {
			c.Approbations = append(c.Approbations, ApprobationPrevue{"plan_serveurs", e.Nom})
		}
		if e.Nom != "prod" && e.Politique == "accord" && !regle(RegleAccord{Projet: c.Projet.Nom, Env: e.Nom, Action: actionDeploiement}) {
			c.Approbations = append(c.Approbations, ApprobationPrevue{"deploiement", e.Nom})
		}
		for _, id := range e.Serveurs {
			sv := nomServeur[id]
			ajouter(&c.Touche.Serveurs, sv.Nom)
			if !sv.MotDePasseCoupe { // shortcut: the inventory only records the hardening's key step; read the server's state if needed
				ajouter(&durcir, sv.Nom)
			}
		}
		if h := hoteURL(e.URL); h != "" {
			ajouter(&c.Touche.DNS, h)
			ajouter(&c.Touche.Zones, zoneHote(h, x.zones))
			ajouter(&c.Touche.Fichiers, "/etc/caddy/sites/"+h+".caddy")
		}
		ajouter(&c.Touche.Fichiers, baseDeploiement(c.Projet.Nom, e.Nom))
	}
	for _, n := range durcir {
		c.Approbations = append(c.Approbations, ApprobationPrevue{"durcissement", n})
	}
	if !x.infraOuvert && slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Portee == "infra" && !a.Decoche }) {
		c.Approbations = append(c.Approbations, ApprobationPrevue{Type: "deverrouiller_coffre"})
	}
	if c.Projet.Nouveau {
		c.Touche.Dossier = dossierProjetNeuf(c.Projet.Nom, x.projets) // where Lancer creates it, unless chosen on the PC
	} else if i := slices.IndexFunc(x.projets, func(p Projet) bool { return p.Nom == c.Projet.Nom }); i >= 0 {
		c.Touche.Dossier = x.projets[i].Dossier
	}
	if x.natif && (c.Serveurs.Brief != "" || len(c.Touche.Serveurs) > 0) {
		c.AvertissementServeurs = errServeursNatif.Error()
	}
	c.AvertissementInfra = ""
	if x.natif && (c.Projet.Nouveau || !x.forcerInfra[c.Projet.Nom]) && slices.ContainsFunc(c.Acces, func(a AccesCarte) bool { return a.Portee == "infra" && !a.Decoche }) {
		c.AvertissementInfra = errAccesInfraNatif.Error()
	}
	m, est := x.minutesCycle, EstimationCarte{Jalons: c.JalonsEstimes, Mesure: x.minutesCycle > 0}
	if m <= 0 {
		m = minutesDefaut
	}
	// a range of one to two: a milestone takes from three quarters to one and a half average cycles
	est.MinutesMin, est.MinutesMax = int(math.Round(float64(c.JalonsEstimes)*m*0.75)), int(math.Round(float64(c.JalonsEstimes)*m*1.5))
	if est.Mesure && x.coutCycle > 0 {
		est.CoutMin, est.CoutMax = math.Round(float64(c.JalonsEstimes)*x.coutCycle*75)/100, math.Round(float64(c.JalonsEstimes)*x.coutCycle*150)/100
	}
	c.Estimation = est
	return c
}

// envSansAffaiblir (C81): e with the policy the launch writes, the strictest of the mission's and the existing
// one; unless the owner gave e in Modify, an existing protection is never changed and existing servers never
// replaced. Avant: the existing environment when the mission changes it, for the card's "before → after".
func envSansAffaiblir(e EnvCarte, politique string, actuels map[string]Environnement, nouveau bool) EnvCarte {
	e.Politique, e.Avant = politiqueEnvMission(e.Nom, politique), nil
	a, ok := actuels[e.Nom]
	if nouveau || !ok {
		return e
	}
	if a.Politique == "" {
		a.Politique = "accord" // validerEnvironnement's default
	}
	if a.Protection == "" {
		a.Protection = "aucune"
	}
	if a.Politique == "accord" {
		e.Politique, e.Plancher = "accord", "accord"
	}
	if !e.Proprietaire {
		if a.Protection != "aucune" {
			e.Protection = a.Protection
		}
		if len(a.Serveurs) > 0 {
			e.Serveurs = slices.Clone(a.Serveurs)
		}
	}
	if a.URL != e.URL || a.Protection != e.Protection || a.Politique != e.Politique || !slices.Equal(a.Serveurs, e.Serveurs) {
		e.Avant = &EnvCarte{Nom: e.Nom, URL: a.URL, Protection: a.Protection, Serveurs: slices.Clone(a.Serveurs), Politique: a.Politique}
	}
	return e
}

// zoneHote: the longest known zone holding h; without one, its last two labels (shortcut: no public suffix
// list, a name under co.uk gives co.uk; read the provider's zones first, as here).
func zoneHote(h string, zones []string) string {
	z := ""
	for _, c := range zones {
		if (h == c || strings.HasSuffix(h, "."+c)) && len(c) > len(z) {
			z = c
		}
	}
	if z != "" {
		return z
	}
	l := strings.Split(h, ".")
	if len(l) <= 2 {
		return h
	}
	return strings.Join(l[len(l)-2:], ".")
}

// moyennesCycles: the average minutes and list cost of a cycle over the stats.tsv of these projects (0, 0 without any).
func moyennesCycles(projets []Projet) (minutes, cout float64) {
	n := 0
	for _, p := range projets {
		for _, l := range p.LireStats() {
			if l.Minutes > 0 {
				minutes, cout, n = minutes+l.Minutes, cout+l.CoutUSD, n+1
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return minutes / float64(n), cout / float64(n)
}

// contexteCarte: a snapshot of what a card is checked against (projects, inventory, hosts, vault names, zones, stats).
func (s *Serveur) contexteCarte() contexteCarte {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	x := contexteCarte{projets: projets, hotes: map[string]string{}, infraOuvert: s.etatCoffre().Infra, natif: modeNatif.Load(), masquer: s.masquerSecrets}
	x.inventaire, _ = lireServeurs() // unreadable: no server, every server of the card is unknown
	if s.acces != nil {
		x.hotes, x.envs, x.forcerInfra = s.acces.Hotes(), map[string]map[string]Environnement{}, map[string]bool{}
		for _, p := range projets {
			x.envs[p.Nom], x.forcerInfra[p.Nom] = map[string]Environnement{}, s.acces.Projet(p.Nom).ForcerInfraNatif
			for n, e := range s.acces.Environnements(p.Nom) {
				if e.URL != "" || len(e.Serveurs) > 0 || e.Protection != "aucune" || e.Deploiement != nil { // configured, not envDefaut
					x.envs[p.Nom][n] = e
				}
			}
		}
	}
	if s.coffre != nil && s.coffreErr == nil {
		x.entrees = s.coffre.Liste()
	}
	if fs, err := lireFournisseurs(); err == nil {
		for _, f := range fs {
			for _, z := range f.Zones {
				x.zones = append(x.zones, z.Nom)
			}
		}
	}
	if s.approbations != nil {
		x.regle = s.approbations.Regle
	}
	x.minutesCycle, x.coutCycle = moyennesCycles(projets)
	return x
}
