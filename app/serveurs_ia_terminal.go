package main

// The assistant beside a server's terminal (M231e), mode "separate connection": a Servers task (serveurs_taches.go)
// whose every tool call is kept on the task as it happens, so that the page shows each command, its output and its
// result live, and the one running. The calls go through the same hook as any agent's (avantOutil: perimeter, risk
// class, a plan the owner accepted, backups); each opens its own SSH connection with the vault's key, so the
// assistant never sees a key and never touches the owner's own terminal.
//
// Mode "live in my terminal" (M231f): the same calls, through the same guards and connections (backups, automatic
// undo, outputs as data kept whole), are also shown as they run on the owner's terminal screen, whose typing is
// refused meanwhile (apiTerminalAgir); nothing is ever sent to the owner's shell, so its state is never mixed with
// the assistant's. Take over (apiReprendreMainTache) unlocks it and the task goes on in its own connection.

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"
)

const (
	maxActionsTache = 200  // the latest calls kept on a task
	maxSortieAction = 4000 // characters of an output kept on a task (the agent got up to sortieMaxOutil)
)

// ActionTache: one tool call of a task's agent, as the assistant's panel shows it. The output is the server's,
// masked of the vault's values: data shown to the owner, never instructions.
type ActionTache struct {
	N      int    `json:"n"`
	T      string `json:"t"`
	Outil  string `json:"outil"`
	Quoi   string `json:"quoi"`
	Classe string `json:"classe,omitempty"` // lecture | changement | destructrice
	Etat   string `json:"etat"`             // en_cours | ok | erreur | refusee
	Code   int    `json:"code"`
	Sortie string `json:"sortie,omitempty"`
	Coupee bool   `json:"coupee,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// tacheDeSession: the running task a session token belongs to (its ops lead, critic or verifier), else nil.
func tacheDeSession(hache string) *tacheEnCours {
	tachesEnCours.Lock()
	defer tachesEnCours.Unlock()
	for _, e := range tachesEnCours.m {
		e.mu.Lock()
		a := e.annexe
		e.mu.Unlock()
		if e.hache == hache || a == hache {
			return e
		}
	}
	return nil
}

// tacheDirecte: the running task working live in the terminal session, else nil (its typing is then locked).
func tacheDirecte(session string) *tacheEnCours {
	tachesEnCours.Lock()
	defer tachesEnCours.Unlock()
	for _, e := range tachesEnCours.m {
		e.mu.Lock()
		ok := e.t.Terminal == session
		e.mu.Unlock()
		if ok {
			return e
		}
	}
	return nil
}

// ecrireDansTerminal: texte (already masked of the vault's values) shown on the screen of the terminal t works
// live in, its line ends as a terminal wants them; nothing without one.
func ecrireDansTerminal(t *tacheEnCours, texte string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	id := t.t.Terminal
	t.mu.Unlock()
	terminaux.Lock()
	x := terminaux.m[id]
	terminaux.Unlock()
	if id == "" || x == nil {
		return
	}
	x.afficher([]byte(strings.ReplaceAll(strings.ReplaceAll(texte, "\r\n", "\n"), "\n", "\r\n")))
}

// ligneAction: a call as the owner's terminal shows it ("$ command" for a command).
func ligneAction(a ActionTache) string {
	q := a.Outil + " " + a.Quoi
	if a.Outil == "ssh_commande" {
		q = "$ " + a.Quoi
	}
	return "\n\x1b[1;36m[assistant]\x1b[0m " + strings.TrimSpace(q) + "\n"
}

// quoiAction: what a call does, in one line: its command, its action and name, its paths, its rule (never a
// file's content).
func quoiAction(a map[string]json.RawMessage) string {
	var x argsOutilServeur
	if b, err := json.Marshal(a); err == nil {
		_ = json.Unmarshal(b, &x)
	}
	var l []string
	for _, v := range []string{x.Commande, x.Action, x.Nom, x.Regle, x.Chemin, x.Local, x.Distant, x.Dossier} {
		if v != "" {
			l = append(l, v)
		}
	}
	if x.Port > 0 {
		l = append(l, fmt.Sprintf("%d/%s", x.Port, x.Proto))
	}
	return strings.Join(l, " ")
}

// debutAction: a call of x's session starts, or was refused (refus); nil without a running task.
func (s *Serveur) debutAction(x SessionHub, outil string, a map[string]json.RawMessage, e Etape, refus error) (*tacheEnCours, int) {
	t := tacheDeSession(x.Hache)
	if t == nil {
		return nil, 0
	}
	act := ActionTache{T: time.Now().UTC().Format(time.RFC3339), Outil: outil, Quoi: couper(s.masquerSecrets(quoiAction(a)), 500), Etat: "en_cours"}
	if refus != nil {
		if c, err := classerEtape(outil, maps.Clone(a)); err == nil {
			act.Classe = c.Classe.String()
		}
		act.Etat, act.Code, act.Detail = "refusee", -1, couper(s.masquerSecrets(refus.Error()), 500)
	} else {
		act.Classe = e.Classe.String()
	}
	t.mu.Lock()
	if n := len(t.t.Actions); n > 0 {
		act.N = t.t.Actions[n-1].N + 1
	}
	t.t.Actions = append(t.t.Actions, act)
	if len(t.t.Actions) > maxActionsTache {
		t.t.Actions = t.t.Actions[len(t.t.Actions)-maxActionsTache:]
	}
	t.mu.Unlock()
	_ = enregistrerTache(t.copie())
	l := ligneAction(act)
	if refus != nil {
		l += "\x1b[33m✗ " + act.Detail + "\x1b[0m\n"
	}
	ecrireDansTerminal(t, l)
	return t, act.N
}

// finAction: the call n of t ended with r (or err).
func (s *Serveur) finAction(t *tacheEnCours, n int, r ResultatOutil, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	ecran := ""
	for i := range t.t.Actions {
		a := &t.t.Actions[i]
		if a.N != n {
			continue
		}
		a.Etat, a.Code, a.Coupee = "ok", r.Code, r.Coupee || len(r.Sortie) > maxSortieAction
		a.Sortie = couper(s.masquerSecrets(r.Sortie), maxSortieAction)
		if err != nil || r.Code != 0 {
			a.Etat = "erreur"
		}
		if err != nil {
			a.Detail = couper(s.masquerSecrets(err.Error()), 500)
		}
		ecran = a.Sortie
		if ecran != "" && !strings.HasSuffix(ecran, "\n") {
			ecran += "\n"
		}
		if a.Etat == "erreur" {
			ecran += fmt.Sprintf("\x1b[31m✗ exit %d %s\x1b[0m\n", a.Code, a.Detail)
		}
	}
	t.mu.Unlock()
	ecrireDansTerminal(t, ecran)
	_ = enregistrerTache(t.copie())
}
