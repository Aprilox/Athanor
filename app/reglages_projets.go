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
	"strconv"
	"strings"
	"sync"
)

// reglagesModeles: the model settings and the variable of the kit's .env that holds their global value.
var reglagesModeles = map[string]string{"modele": "MODEL", "modele_secours": "FALLBACK_MODEL",
	"modele_sous_agents": "SUBAGENT_MODEL", "modele_taches_simples": "CHEAP_MODEL"}

// clesReglagesProjet: the settings a project may replace, in the page's order. A quota_<window> value is
// "<percent>:<action>" (a rule of quota.go), "0" for no automatic stop.
var clesReglagesProjet = []string{"modele", "modele_secours", "modele_sous_agents", "modele_taches_simples",
	"quota_five_hour", "quota_seven_day", "quota_modele", "permission"}

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

// reglageValide: v is a value setting cle accepts (a known model, a known mode, a quota rule).
func reglageValide(k Kit, cle, v string) bool {
	fenetre, quota := strings.CutPrefix(cle, "quota_")
	switch {
	case reglagesModeles[cle] != "":
		return slices.Contains(modelesConnus(k), v) || cle == "modele_sous_agents" && v == "auto"
	case quota && fenetresRegles[fenetre]:
		seuil, action, _ := strings.Cut(v, ":")
		n, err := strconv.Atoi(seuil)
		return v == "0" || err == nil && n >= 1 && n <= 100 && actionsRegles[action] && v == fmt.Sprintf("%d:%s", n, action)
	case cle == "permission":
		return slices.Contains(modesAutorisation, v)
	}
	return false
}

// reglageGlobal: the value every project follows by default ("" for an unknown setting or an empty model).
func reglageGlobal(cf Config, cle string) string {
	k := Kit{Racine: cf.Kit}
	fenetre, quota := strings.CutPrefix(cle, "quota_")
	switch {
	case reglagesModeles[cle] != "":
		v := ""
		if cle == "modele_taches_simples" {
			v = "haiku" // CHEAP_MODEL absent from the .env (definitionAgent)
		}
		for _, l := range LireEnv(k.Env()) {
			if l.Cle == reglagesModeles[cle] {
				v = strings.TrimSpace(l.Valeur)
			}
		}
		return v
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
// longer valid: a model that left the list).
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

// definirReglageProjet: the project's own value of cle ("" or "defaut": back to the global one); returns the own
// value it had before. An unknown setting or value is an erreurEntree.
func definirReglageProjet(projet, cle, valeur string) (string, error) {
	if valeur == "defaut" {
		valeur = ""
	}
	if !slices.Contains(clesReglagesProjet, cle) {
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
	s.ecrireJSON(w, map[string]any{"projet": nom, "reglages": vueReglagesProjet(nom)})
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
	s.ecrireJSON(w, map[string]any{"message_key": "Project setting saved.", "args": []string{}, "reglages": vueReglagesProjet(nom)})
}
