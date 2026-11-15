package main

import (
	"context"
	"errors"
	"slices"
	"time"
)

// A step that needs the infra key while the vault is locked waits for the unlock instead of failing (note 12h28
// §3): the bell says what waits and why, the rest goes on, and the step resumes where it was once unlocked.

type attenteInfra struct {
	Module, Element string
}

// valeurInfra: the vault entry id's value; locked, it waits for the unlock (or ctx's end), shown in the bell.
func (s *Serveur) valeurInfra(ctx context.Context, id, module, element string) (string, error) {
	n := 0
	defer func() {
		if n != 0 {
			s.attenteMu.Lock()
			delete(s.attentes, n)
			s.attenteMu.Unlock()
		}
	}()
	for {
		s.attenteMu.Lock()
		if s.infraOuverte == nil {
			s.infraOuverte, s.attentes = make(chan struct{}), map[int]attenteInfra{}
		}
		ouverte := s.infraOuverte // taken before reading: an unlock in between is never missed
		s.attenteMu.Unlock()
		v, err := s.coffre.Valeur(id)
		if !errors.Is(err, errCoffreVerrouille) || !s.fenetreVerrouillee() {
			return v, err // the window open: its own request answers at once ("vault locked"), never hangs
		}
		if n == 0 {
			s.attenteMu.Lock()
			s.attenteN++
			n = s.attenteN
			s.attentes[n] = attenteInfra{module, element}
			s.attenteMu.Unlock()
			s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: module, Action: "étape infra en attente du déverrouillage", Resultat: element})
		}
		select {
		case <-ouverte:
		case <-ctx.Done():
			return "", err
		}
	}
}

// fenetreVerrouillee: a password and the computer's window locked: only background work (pilots, missions, agents'
// tools) can ask for the infra key then.
func (s *Serveur) fenetreVerrouillee() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.MotDePasseHash != "" && s.session == ""
}

// signalerInfraOuverte wakes the steps waiting for the infra key (deverrouillerCoffre).
func (s *Serveur) signalerInfraOuverte() {
	s.attenteMu.Lock()
	defer s.attenteMu.Unlock()
	if s.infraOuverte != nil {
		close(s.infraOuverte)
	}
	s.infraOuverte = make(chan struct{})
}

// alertesAttenteInfra: one bell alert per element waiting for the unlock.
func (s *Serveur) alertesAttenteInfra() []AlerteHub {
	s.attenteMu.Lock()
	defer s.attenteMu.Unlock()
	out, vus := []AlerteHub{}, map[attenteInfra]bool{}
	for _, a := range s.attentes {
		if vus[a] {
			continue
		}
		vus[a] = true
		out = append(out, AlerteHub{Empreinte: "infra-attente-" + a.Module + "-" + a.Element, Module: a.Module, Element: a.Element, Titre: "Waiting for the unlock",
			Raison: "{0} waits for the vault's infra keys (a server action). The rest goes on; it resumes where it was once you unlock.", Args: []any{a.Element},
			Actions: []ActionAlerte{{ID: "voir", Libelle: "Unlock", Page: "coffre"}}})
	}
	slices.SortFunc(out, func(x, y AlerteHub) int { return cmpTexte(x.Empreinte, y.Empreinte) })
	return out
}

func cmpTexte(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// dureesInfraMission: how long a launched mission keeps the infra key open despite the automatic lock.
var dureesInfraMission = map[string]time.Duration{"1h": time.Hour, "4h": 4 * time.Hour, "8h": 8 * time.Hour}

// garderInfraMission: the owner's choice at launch ("mission" = until it ends or stops, or a duration), kept on it.
func garderInfraMission(id, choix string, t time.Time) error {
	jusqua := ""
	if d, ok := dureesInfraMission[choix]; ok {
		jusqua = t.Add(d).UTC().Format(time.RFC3339)
	} else if choix == "mission" {
		jusqua = "mission"
	} else {
		return nil
	}
	_, err := modifierMission(id, func(m *Mission) error { m.InfraJusqua = jusqua; return nil })
	return err
}

// infraGardee: a running mission was granted the infra key until now at least.
func infraGardee(t time.Time) bool {
	return slices.ContainsFunc(lireMissions(), func(m Mission) bool {
		if !missionSuivie(m.Etat) || m.InfraJusqua == "" {
			return false
		}
		f, err := time.Parse(time.RFC3339, m.InfraJusqua)
		return m.InfraJusqua == "mission" || (err == nil && t.Before(f))
	})
}
