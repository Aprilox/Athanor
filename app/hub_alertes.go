package main

// The hub's bell: one alert per element a module says waits for the owner (ElementHub.Attente), in a
// generic form {module, element, title, reason, actions[]} so that later approvals (Accept, Refuse,
// Never ask again) are only more actions. Ignore hides an alert without deleting anything: its
// fingerprint (element + reason) is kept in dossierModule("hub"); a changed reason is a new alert.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ActionAlerte: one button of an alert; Page (and Projet, the Dev project to select first) for "voir".
type ActionAlerte struct {
	ID      string `json:"id"`      // "voir" | "ignorer" (later "accepter", "refuser", "jamais")
	Libelle string `json:"libelle"` // i18n key
	Page    string `json:"page,omitempty"`
	Projet  string `json:"projet,omitempty"`
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
}

// empreinteAlerte: the element and its reason; the same alert ignored once stays hidden, any change
// of the reason or of its arguments (2 tasks instead of 1) makes it come back.
func empreinteAlerte(e ElementHub) string {
	b, _ := json.Marshal([]any{e.Module, e.ID, e.Attente.Raison, e.Attente.Args})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:12])
}

// alertesDe: the alerts of the elements waiting for the owner, in the elements' order.
func alertesDe(el []ElementHub) []AlerteHub {
	out := []AlerteHub{}
	for _, e := range el {
		if e.Attente == nil {
			continue
		}
		out = append(out, AlerteHub{Empreinte: empreinteAlerte(e), Module: e.Module, Element: e.ID, Titre: e.Titre,
			Raison: e.Attente.Raison, Args: e.Attente.Args, Actions: []ActionAlerte{
				{ID: "voir", Libelle: "View", Page: e.Attente.Page, Projet: e.Projet},
				{ID: "ignorer", Libelle: "Ignore"},
			}})
	}
	return out
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
	if !ignorerAlerte(alertesDe(el), c.Empreinte, c.Ignorer) {
		s.erreur(w, http.StatusNotFound, "This alert no longer exists.")
		return
	}
	s.ecrireJSON(w, map[string]any{"ok": true})
}
