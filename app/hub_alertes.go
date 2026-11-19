package main

// The hub's bell: one alert per element a module says waits for the owner (ElementHub.Attente), in a
// generic form {module, element, title, reason, actions[]} so that later approvals (Accept, Refuse,
// Never ask again) are only more actions. Ignore hides an alert without deleting anything: its
// fingerprint (element + reason) is kept in dossierModule("hub"); a changed reason is a new alert.

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// ActionAlerte: one button of an alert; Page (and Projet, the Dev project to select first) for "voir",
// else the POST the client sends as is (Route under /api/, Corps); Distance: allowed from a paired device.
type ActionAlerte struct {
	ID       string         `json:"id"`      // "voir" | "ignorer" | "rouvrir" | "accepter" | "refuser" | "toujours"
	Libelle  string         `json:"libelle"` // i18n key
	Page     string         `json:"page,omitempty"`
	Projet   string         `json:"projet,omitempty"`
	Route    string         `json:"route,omitempty"`
	Corps    map[string]any `json:"corps,omitempty"`
	Distance bool           `json:"distance,omitempty"`
	Accord   bool           `json:"accord,omitempty"` // the page asks for the owner's password first (M137: prod incidents)
}

type AlerteHub struct {
	Empreinte string         `json:"empreinte"`
	Module    string         `json:"module"`
	Element   string         `json:"element"`
	Titre     string         `json:"titre"`
	Raison    string         `json:"raison"` // i18n key
	Args      []any          `json:"args,omitempty"`
	Actions   []ActionAlerte `json:"actions"`
	Ignoree   string         `json:"ignoree,omitempty"` // when it was ignored (RFC 3339), history only
	Info      bool           `json:"info,omitempty"`    // an information, never shown as something that failed (M302)
	// an agent's pending approval (alertesApprobations): what the decision needs (env, details, expiry)
	Approbation *Approbation `json:"approbation,omitempty"`
}

// empreinteAlerte: the element and its reason; the same alert ignored once stays hidden, any change
// of the reason or of its arguments (2 tasks instead of 1) makes it come back.
func empreinteAlerte(e ElementHub) string {
	b, _ := json.Marshal([]any{e.Module, e.ID, e.Attente.Raison, e.Attente.Args, e.Attente.Cle})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:12])
}

// alertesDe: the alerts of the elements waiting for the owner, in the elements' order.
func alertesDe(el []ElementHub) []AlerteHub {
	out := []AlerteHub{}
	for _, e := range el {
		if e.Attente == nil || strings.HasPrefix(e.Attente.Cle, "relais:") { // a failed deployment: alertesRelais or its incident rings already
			continue
		}
		out = append(out, AlerteHub{Empreinte: empreinteAlerte(e), Module: e.Module, Element: e.ID, Titre: e.Titre,
			Raison: e.Attente.Raison, Args: e.Attente.Args, Actions: append(slices.Clone(e.Attente.Actions),
				ActionAlerte{ID: "voir", Libelle: "View", Page: e.Attente.Page, Projet: e.Projet},
				ignorerOuRouvrir(empreinteAlerte(e), true),
			)})
	}
	return out
}

func ignorerOuRouvrir(empreinte string, ignorer bool) ActionAlerte {
	a := ActionAlerte{ID: "ignorer", Libelle: "Ignore", Route: "hub/alertes", Corps: map[string]any{"empreinte": empreinte, "ignorer": ignorer}}
	if !ignorer {
		a.ID, a.Libelle = "rouvrir", "Reopen"
	}
	return a
}

// alertesApprobations: one alert per pending approval, its key the approval's id (stable), with no
// Ignore (apiAlertes does not know it): only a decision, or its expiry, makes it go. The decisions are
// allowed from a paired device (the route itself is opened to devices by M63).
// shortcut: only the Dev pilot asks for approvals today, so Module is "dev"; a module field comes with Servers.
func alertesApprobations(l []Approbation) []AlerteHub {
	out := []AlerteHub{}
	for _, x := range l {
		if x.Etat != "attente" {
			continue
		}
		raison, args := "Approval requested ({0}): {1}", []any{x.Action, x.Raison}
		if x.Action == "demander_acces" {
			raison, args = "Access requested: {0}", []any{x.Raison}
		}
		toujours := "Never ask again for this project"
		if x.Action == actionPlanServeur {
			raison, args, toujours = "Plan for {0}: {1}", []any{cmp.Or(x.Details["nom"], x.Details["serveur"]), x.Raison}, "Never ask again for this server"
		}
		if x.Action == actionDeploiement {
			raison, args = "Deploy {0}? Commit {1}", []any{x.Details["hote"], cmp.Or(x.Details["commit"], x.Details["sha"])}
			if x.Production {
				raison, args = "Deploy {0} to production? Commit {1}", []any{x.Details["hote"], cmp.Or(x.Details["commit"], x.Details["sha"])}
			}
			if x.Details["mission"] != "" { // P7: a mission's prod accord says where and what changes, decided from a phone
				raison, args = "Deploy {0} to production? Commit {1} → {2}. {3} new commit(s): {4}", []any{x.Details["hote"], x.Details["commit"],
					cmp.Or(x.Details["url"], "—"), cmp.Or(x.Details["commits"], "?"), cmp.Or(x.Details["changements"], "—")}
			}
		}
		titre, module := x.Projet, "dev"
		if x.Agent == agentDurcir { // B80: Athanor's own plan, titled by the server, its fixed summary translated
			titre, raison, args = cmp.Or(x.Details["nom"], x.Details["serveur"]), x.Raison, nil
		}
		decision := func(id, libelle string) ActionAlerte {
			return ActionAlerte{ID: id, Libelle: libelle, Route: "hub/approbations", Corps: map[string]any{"id": x.ID, "decision": id}, Distance: true}
		}
		actions := []ActionAlerte{decision("accepter", "Accept"), decision("refuser", "Refuse"), decision("toujours", toujours)}
		if x.Action == actionPlanServeur && x.Details["regle"] != "oui" || x.Production || x.Action == actionDeploiement && x.Details["fichiers_build"] != "" { // destructive, prod or changed build files: an accord every time
			actions = actions[:2]
		}
		out = append(out, AlerteHub{Empreinte: "appr-" + x.ID, Module: module, Element: x.Projet, Titre: titre, Raison: raison, Args: args, Approbation: &x,
			Actions: actions})
	}
	return out
}

// alertesIgnorables: every alert the owner may ignore: the elements', the relays', the incidents', the older sites'.
func (s *Serveur) alertesIgnorables(el []ElementHub) []AlerteHub {
	return slices.Concat(alertesDe(el), s.alertesRelais(), s.alertesIncidents(), s.alertesAnciensSites())
}

// alertesHub: the bell (the pending approvals first, then the modules' alerts not ignored) and the
// history of the ignored ones, each with its Reopen.
func (s *Serveur) alertesHub(el []ElementHub) (actives, cachees []AlerteHub) {
	toutes := s.alertesIgnorables(el)
	actives, cachees = trierAlertes(toutes, ignoreesCourantes(toutes))
	for i := range cachees {
		cachees[i].Actions = []ActionAlerte{ignorerOuRouvrir(cachees[i].Empreinte, false)}
	}
	if s.approbations != nil {
		l, _ := s.approbations.Liste()
		actives = append(alertesApprobations(l), actives...)
	}
	actives = append(append(s.alerteUrgence(), s.alertesAttenteInfra()...), actives...)
	return actives, cachees
}

var alertesMu sync.Mutex

func fichierAlertesIgnorees() string {
	return filepath.Join(dossierModule("hub"), "alertes_ignorees.json")
}

// lireIgnorees: fingerprint → when it was ignored (a missing or damaged file = nothing ignored).
func lireIgnorees() map[string]string {
	m := map[string]string{}
	lireJSONFichier(fichierAlertesIgnorees(), &m)
	return m
}

// ignoreesCourantes: the ignored fingerprints of the current alerts; those of alerts gone are dropped
// from the file, so that a situation which ends and comes back later rings again.
func ignoreesCourantes(toutes []AlerteHub) map[string]string {
	alertesMu.Lock()
	defer alertesMu.Unlock()
	m, garde := lireIgnorees(), map[string]string{}
	for _, a := range toutes {
		if le, ok := m[a.Empreinte]; ok {
			garde[a.Empreinte] = le
		}
	}
	if len(garde) < len(m) {
		_ = ecrireJSONFichier(fichierAlertesIgnorees(), garde, 0o600)
	}
	return garde
}

// trierAlertes: the bell's alerts and the history of the ignored ones still current.
func trierAlertes(toutes []AlerteHub, ignorees map[string]string) (actives, cachees []AlerteHub) {
	actives, cachees = []AlerteHub{}, []AlerteHub{}
	for _, a := range toutes {
		if le, ok := ignorees[a.Empreinte]; ok {
			a.Ignoree = le
			cachees = append(cachees, a)
		} else {
			actives = append(actives, a)
		}
	}
	sort.SliceStable(cachees, func(i, j int) bool { return cachees[i].Ignoree > cachees[j].Ignoree })
	return actives, cachees
}

// ignorerAlerte: hides (ignorer) or reopens an alert of the current ones; the fingerprints of alerts
// that no longer exist are dropped on the way (their situation changed: they would come back anyway).
func ignorerAlerte(toutes []AlerteHub, empreinte string, ignorer bool) bool {
	alertesMu.Lock()
	defer alertesMu.Unlock()
	m, garde, trouve := lireIgnorees(), map[string]string{}, false
	for _, a := range toutes {
		if le, ok := m[a.Empreinte]; ok {
			garde[a.Empreinte] = le
		}
		trouve = trouve || a.Empreinte == empreinte
	}
	if !trouve {
		return false
	}
	if ignorer {
		garde[empreinte] = time.Now().UTC().Format(time.RFC3339)
	} else {
		delete(garde, empreinte)
	}
	return ecrireJSONFichier(fichierAlertesIgnorees(), garde, 0o600) == nil
}

// apiAlertes: POST /api/hub/alertes {empreinte, ignorer}: Ignore, or Reopen from the history.
func (s *Serveur) apiAlertes(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Empreinte string `json:"empreinte"`
		Ignorer   bool   `json:"ignorer"`
	}
	if err := lireCorps(r, &c); err != nil || c.Empreinte == "" {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	_, el := s.vueHub()
	if !ignorerAlerte(s.alertesIgnorables(el), c.Empreinte, c.Ignorer) {
		s.erreur(w, http.StatusNotFound, "This alert no longer exists.")
		return
	}
	s.ecrireJSON(w, map[string]any{"ok": true})
}
