package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// MessageMoteur (B79): what the deployment engine tells, as an English template that is a key of i18n.js
// ({0}, {1}…) and its arguments, each a text or another message, so that the page says it in its language.
// Error() is the English text (the history's erreur, the push notifications, the tests).
type MessageMoteur struct {
	Cle   string `json:"cle"`
	Args  []any  `json:"args,omitempty"` // string or *MessageMoteur
	cause error
}

var reArgMessage = regexp.MustCompile(`\{(\d+)\}`)

func (m *MessageMoteur) Error() string {
	return reArgMessage.ReplaceAllStringFunc(m.Cle, func(x string) string { // one pass: an argument's own "{1}" stays
		if i, _ := strconv.Atoi(x[1 : len(x)-1]); i < len(m.Args) {
			return fmt.Sprint(m.Args[i])
		}
		return x
	})
}

func (m *MessageMoteur) Unwrap() error { return m.cause }

// UnmarshalJSON: the arguments read back from the history as texts or messages (not maps).
func (m *MessageMoteur) UnmarshalJSON(b []byte) error {
	var v struct {
		Cle  string            `json:"cle"`
		Args []json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	m.Cle, m.Args = v.Cle, nil
	for _, r := range v.Args {
		var s string
		if json.Unmarshal(r, &s) == nil {
			m.Args = append(m.Args, s)
			continue
		}
		sous := &MessageMoteur{}
		if err := json.Unmarshal(r, sous); err != nil {
			return err
		}
		m.Args = append(m.Args, sous)
	}
	return nil
}

// errMoteur: an engine error to translate; an error argument stays a message (its own key) or becomes its text,
// the first one is what errors.Is and errors.As see through.
func errMoteur(cle string, args ...any) error {
	m := &MessageMoteur{Cle: cle}
	for _, a := range args {
		if e, ok := a.(error); ok {
			if m.cause == nil {
				m.cause = e
			}
			m.Args = append(m.Args, messageDe(e))
			continue
		}
		m.Args = append(m.Args, fmt.Sprint(a))
	}
	return m
}

// messageDe: err as a message; an error the engine did not word is its own key (shown as it is when i18n.js
// does not know it). nil for nil.
func messageDe(err error) *MessageMoteur {
	var m *MessageMoteur
	if err == nil {
		return nil
	}
	if errors.As(err, &m) && m.Error() == err.Error() {
		return m
	}
	return &MessageMoteur{Cle: err.Error()}
}

// suite: m then n, "m; n" (m nil: n alone).
func suite(m *MessageMoteur, n error) *MessageMoteur {
	if m == nil {
		return messageDe(n)
	}
	return &MessageMoteur{Cle: "{0}; {1}", Args: []any{m, messageDe(n)}}
}

// masquer: a copy of m with f applied to its key and every text (B77: a server may echo a secret).
func (m *MessageMoteur) masquer(f func(string) string) *MessageMoteur {
	if m == nil {
		return nil
	}
	c := &MessageMoteur{Cle: f(m.Cle)}
	for _, a := range m.Args {
		if sous, ok := a.(*MessageMoteur); ok {
			c.Args = append(c.Args, sous.masquer(f))
		} else {
			c.Args = append(c.Args, f(fmt.Sprint(a)))
		}
	}
	return c
}

// problemesVerif: the external check's problems in words (the interface's PROBLEMES_VERIF gives their details).
var problemesVerif = map[string]string{"injoignable": "the site does not answer", "tls": "the certificate is not valid",
	"statut": "unexpected status", "ouvert": "the site opens without identification"}

// messageVerification: "the external verification failed: the site does not answer, …: <its error>".
func messageVerification(v *VerifDeploiement) *MessageMoteur {
	liste := &MessageMoteur{}
	for i, p := range v.Problemes {
		liste.Cle += ", "[:min(i, 1)*2] + "{" + strconv.Itoa(i) + "}"
		liste.Args = append(liste.Args, &MessageMoteur{Cle: cmp.Or(problemesVerif[p], p)})
	}
	if v.HTTP.Erreur == "" {
		return &MessageMoteur{Cle: "the external verification failed: {0}", Args: []any{liste}}
	}
	return &MessageMoteur{Cle: "the external verification failed: {0}: {1}", Args: []any{liste, couper(v.HTTP.Erreur, 200)}}
}

// echecLisible: what failed in an incident, the deployment's message when there is one (the page translates it).
func echecLisible(s *Serveur, x incidentLieu) any {
	if x.Message != nil {
		return x.Message.masquer(s.masquerSecrets)
	}
	return s.masquerSecrets(couper(x.Echec, 300))
}

// noterMessage: d's message masked and its English text in Erreur.
func (s *Serveur) noterMessage(d *Deploiement) {
	d.Message, d.Erreur = d.Message.masquer(s.masquerSecrets), ""
	if d.Message != nil {
		d.Erreur = d.Message.Error()
	}
}
