package main

// Per-project settings (owner's note of 2026-11-17 01h48, M280). One rule for all of them: reglageProjet is the
// project's own value when it has a valid one, else the global one (the kit's .env, the Overview's automatic stops,
// Settings › Permissions). hub/reglages_projets.json keeps {project: {key: value}}; no file, no key or "defaut" is
// the global value, so a project that never chose anything behaves as before.
// The working mode (parallel, eco, fast) is already the project's own (.athanor/.env, /api/mode) and has no global
// value: it stays there.

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// clesReglagesProjet: the settings a project may replace, in the page's order. A quota_<window> value is
// "<percent>:<action>" (a rule of quota.go), "0" for no automatic stop; connexions: the ids of Settings › Claude ›
// Connections it may use, in their order there ("": all of them, connexions.go).
// The project's own models are set in its Working mode (modeles_projet.go, 2.53.0), its one place.
var clesReglagesProjet = []string{"quota_five_hour", "quota_seven_day", "quota_modele", "connexions", "permission"}

var reglagesProjetsMu sync.Mutex

func fichierReglagesProjets() string {
	return filepath.Join(dossierModule("hub"), "reglages_projets.json")
}

func lireReglagesProjets() map[string]map[string]string {
	m := map[string]map[string]string{}
	lireJSONFichier(fichierReglagesProjets(), &m)
	return m
}

func ecrireReglagesProjets(m map[string]map[string]string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return ecrireAtomique(fichierReglagesProjets(), b, 0o600)
}

// reglageValide: v is a value setting cle accepts (a known mode, a quota rule).
func reglageValide(k Kit, cle, v string) bool {
	fenetre, quota := strings.CutPrefix(cle, "quota_")
	switch {
	case quota && fenetresRegles[fenetre]:
		seuil, action, _ := strings.Cut(v, ":")
		n, err := strconv.Atoi(seuil)
		return v == "0" || err == nil && n >= 1 && n <= 100 && actionsRegles[action] && v == fmt.Sprintf("%d:%s", n, action)
	case cle == "permission":
		return slices.Contains(modesAutorisation, v)
	case cle == "connexions":
		return !slices.ContainsFunc(strings.Split(v, ","), func(id string) bool { return !reIDConnexion.MatchString(id) })
	}
	if _, ok := cleEnvProjet(cle); ok {
		return v != "" && len(v) <= 200 && !strings.ContainsAny(v, "\r\n")
	}
	return false
}

// cleEnvProjet: "env:KEY", a setting of Settings › Claude (the tool's .env) a project replaces for itself. Never the
// connection (its own list), the models (the project's Working mode) nor a secret.
func cleEnvProjet(cle string) (string, bool) {
	k, ok := strings.CutPrefix(cle, "env:")
	return k, ok && reCle.MatchString(k) && !slices.Contains(clesConnexionClaude, k) && !estCleModeleProjet(k) && !EstSecret(k)
}

// envProjetNom: the settings of Settings › Claude the project replaces ({KEY: value}).
func envProjetNom(cf Config, nom string) map[string]string {
	out := map[string]string{}
	for cle := range lireReglagesProjets()[nom] {
		if k, ok := cleEnvProjet(cle); ok {
			if v := reglagePropre(cf, nom, cle); v != "" {
				out[k] = v
			}
		}
	}
	return out
}

// envProjetPilote: envProjetNom of the project of this folder (a pilot's, when it starts: they win over .env).
func envProjetPilote(dossier string) map[string]string {
	cf := lireConfig()
	return envProjetNom(cf, nomProjetDossier(cf, dossier))
}

// texteEnvProjet: those settings as .env lines added after the tool's own (empreinteReglages: a change of them is a
// change of the settings the pilot runs with, so it restarts at the end of its cycle as for Settings).
func texteEnvProjet(m map[string]string) string {
	var l []string
	for k, v := range m {
		l = append(l, "\n"+k+"="+v)
	}
	sort.Strings(l)
	return strings.Join(l, "")
}

// vueEnvProjet: Manage › Settings › Claude for this project: each setting of the tool's .env a project may replace,
// in the order of the file, with the value of Settings and the project's own.
func vueEnvProjet(k Kit, projet string) []ReglageVue {
	cf, l, vus := lireConfig(), []ReglageVue{}, map[string]bool{}
	for _, f := range []string{k.Env(), k.EnvExemple()} { // .env first: its values; the example adds what it lacks
		for _, x := range LireEnv(f) {
			if _, ok := cleEnvProjet("env:" + x.Cle); ok && !vus[x.Cle] {
				vus[x.Cle] = true
				propre := reglagePropre(cf, projet, "env:"+x.Cle)
				l = append(l, ReglageVue{Cle: x.Cle, Valeur: cmp.Or(propre, x.Valeur), Propre: propre, Defaut: x.Valeur})
			}
		}
	}
	return l
}

// reglageGlobal: the value every project follows by default ("" for an unknown setting).
func reglageGlobal(cf Config, cle string) string {
	k := Kit{Racine: cf.Kit}
	fenetre, quota := strings.CutPrefix(cle, "quota_")
	switch {
	case quota && fenetresRegles[fenetre]:
		if r, ok := lireReglesQuota(k)[fenetre]; ok {
			return fmt.Sprintf("%d:%s", r.Seuil, r.Action)
		}
		return "0"
	case cle == "permission":
		if m := cf.PermissionsUsage["missions"]; slices.Contains(modesAutorisation, m) {
			return m
		}
		return "manuel"
	}
	return ""
}

// reglagePropre: the project's own value of cle, "" when it follows the default (never chosen, or a value no
// longer valid).
func reglagePropre(cf Config, projet, cle string) string {
	v := lireReglagesProjets()[projet][cle]
	if v == "" || !reglageValide(Kit{Racine: cf.Kit}, cle, v) {
		return ""
	}
	return v
}

// reglageProjet: the one resolution rule, the project's own value, else the global one.
func reglageProjet(projet, cle string) string {
	cf := lireConfig()
	return cmp.Or(reglagePropre(cf, projet, cle), reglageGlobal(cf, cle))
}

// nomProjetDossier: the name of the project that works in this folder ("" when the application knows none).
func nomProjetDossier(cf Config, dossier string) string {
	for _, p := range cf.Projets {
		if memeFichier(p.Dossier, dossier) {
			return p.Nom
		}
	}
	return ""
}

// variablesReglagesProjet: the variables of the kit's .env the project of this folder replaced by a model of its
// own ({"MODEL": "opus"}, its Working mode's Models); empty when it follows the defaults or is no project of the
// application. The chat reads it when a session starts; the pilot reads the same file each cycle itself.
func variablesReglagesProjet(dossier string) map[string]string {
	cf := lireConfig()
	for _, p := range cf.Projets {
		if memeFichier(p.Dossier, dossier) {
			return modelesProjet(p.envProjet()) // its Working mode's Models (modeles_projet.go), the pilot's one source
		}
	}
	return map[string]string{}
}

// reglesQuotaProjet: the automatic stops of the kit (fichierRegles, quota-arret.json) with the project's own rule
// in their place, window by window ("0": no stop for this project).
func reglesQuotaProjet(dossier, fichierRegles string) map[string]regleSeuil {
	r, cf := reglesSeuils(fichierRegles), lireConfig()
	nom := nomProjetDossier(cf, dossier)
	if nom == "" {
		return r
	}
	for fenetre := range fenetresRegles {
		seuil, action, _ := strings.Cut(reglagePropre(cf, nom, "quota_"+fenetre), ":")
		if n, _ := strconv.Atoi(seuil); n > 0 {
			r[fenetre] = regleSeuil{float64(n), action}
		} else if seuil == "0" {
			delete(r, fenetre)
		}
	}
	return r
}

// definirReglageProjet: the project's own value of cle ("" or "defaut": back to the global one); returns the own
// value it had before. An unknown setting or value is an erreurEntree.
func definirReglageProjet(projet, cle, valeur string) (string, error) {
	if valeur == "defaut" {
		valeur = ""
	}
	if _, env := cleEnvProjet(cle); !slices.Contains(clesReglagesProjet, cle) && !env {
		return "", erreurEntree("Unknown project setting")
	}
	if valeur != "" && !reglageValide(Kit{Racine: lireConfig().Kit}, cle, valeur) {
		return "", erreurEntree("This value is not allowed for this project setting")
	}
	reglagesProjetsMu.Lock()
	defer reglagesProjetsMu.Unlock()
	m := lireReglagesProjets()
	avant := m[projet][cle]
	if avant == valeur {
		return avant, nil // nothing to write: a project that never chose anything leaves no file
	}
	if valeur != "" {
		if m[projet] == nil {
			m[projet] = map[string]string{}
		}
		m[projet][cle] = valeur
	} else if delete(m[projet], cle); len(m[projet]) == 0 {
		delete(m, projet)
	}
	return avant, ecrireReglagesProjets(m)
}

// deplacerReglagesProjet: a renamed project keeps its own settings; nouveau "": the project is removed, they go.
func deplacerReglagesProjet(ancien, nouveau string) {
	reglagesProjetsMu.Lock()
	defer reglagesProjetsMu.Unlock()
	m := lireReglagesProjets()
	v, ok := m[ancien]
	if !ok {
		return
	}
	delete(m, ancien)
	if nouveau != "" {
		m[nouveau] = v
	}
	_ = ecrireReglagesProjets(m)
}

// ReglageVue: a setting as the page shows it: the value that applies, the project's own ("": by default) and the
// global one.
type ReglageVue struct {
	Cle    string `json:"cle"`
	Valeur string `json:"valeur"`
	Propre string `json:"propre"`
	Defaut string `json:"defaut"`
}

func vueReglagesProjet(projet string) []ReglageVue {
	cf := lireConfig()
	var l []ReglageVue
	for _, cle := range clesReglagesProjet {
		propre, defaut := reglagePropre(cf, projet, cle), reglageGlobal(cf, cle)
		l = append(l, ReglageVue{Cle: cle, Valeur: cmp.Or(propre, defaut), Propre: propre, Defaut: defaut})
	}
	return l
}

// apiReglagesProjet: GET /api/projets/{nom}/reglages.
func (s *Serveur) apiReglagesProjet(w http.ResponseWriter, r *http.Request) {
	nom := r.PathValue("nom")
	if _, ok := s.projetNomme(nom); !ok {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	s.ecrireJSON(w, map[string]any{"projet": nom, "reglages": vueReglagesProjet(nom), "env": vueEnvProjet(s.kitCourant(), nom)})
}

// apiReglageProjetModifier: POST /api/projets/{nom}/reglages {cle, valeur, confirmer, mot_de_passe}: valeur "" or
// "defaut" goes back to the global value. The Ignore permissions mode as everywhere (choixIgnorer): Athanor's
// window, confirmed, with the password. Every change is in the audit log, with the value before and after.
func (s *Serveur) apiReglageProjetModifier(w http.ResponseWriter, r *http.Request) {
	nom := r.PathValue("nom")
	if _, ok := s.projetNomme(nom); !ok {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	var c struct {
		Cle        string `json:"cle"`
		Valeur     string `json:"valeur"`
		Confirmer  bool   `json:"confirmer"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if c.Cle == "permission" && c.Valeur == "ignorer" {
		if code, err := s.choixIgnorer(r, c.Valeur, c.Confirmer, c.MotDePasse); err != nil {
			s.erreur(w, code, err.Error())
			return
		}
	}
	avant, err := definirReglageProjet(nom, c.Cle, c.Valeur)
	if err != nil {
		code := http.StatusInternalServerError
		if _, ok := err.(erreurEntree); ok {
			code = http.StatusBadRequest
		}
		s.erreur(w, code, err.Error())
		return
	}
	if apres := lireReglagesProjets()[nom][c.Cle]; apres != avant {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: nom, Action: "reglage du projet: " + c.Cle,
			Resultat: cmp.Or(avant, "defaut") + " → " + cmp.Or(apres, "defaut")})
	}
	s.ecrireJSON(w, map[string]any{"message_key": "Project setting saved.", "args": []string{}, "reglages": vueReglagesProjet(nom), "env": vueEnvProjet(s.kitCourant(), nom)})
}
