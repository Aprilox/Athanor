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
	"cmp"
	"encoding/json"
	"errors"
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
	N       int    `json:"n"`
	T       string `json:"t"`
	Outil   string `json:"outil"`
	Libelle string `json:"libelle,omitempty"` // C1-10: the tool in plain English, an i18n.js key (libellesOutils)
	Quoi    string `json:"quoi"`
	Classe  string `json:"classe,omitempty"` // lecture | changement | destructrice
	Etat    string `json:"etat"`             // en_cours | ok | erreur | refusee
	Code    int    `json:"code"`
	Sortie  string `json:"sortie,omitempty"`
	Coupee  bool   `json:"coupee,omitempty"`
	Detail  string `json:"detail,omitempty"`
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

// tacheDirecte: the task whose command runs now in the terminal session, else nil; its typing and files are locked
// only then (M242a: before, any running task of that terminal locked it, waiting for a decision or stopped too).
func tacheDirecte(session string) *tacheEnCours {
	tachesEnCours.Lock()
	defer tachesEnCours.Unlock()
	maintenant := time.Now()
	for _, e := range tachesEnCours.m {
		e.mu.Lock()
		ok := e.t.Terminal == session && e.verrouille(maintenant)
		e.mu.Unlock()
		if ok {
			return e
		}
	}
	return nil
}

// gardeFouVerrou: once a call locked the owner's terminal, no command of the assistant for delaiClavierLibre (it
// thinks, waits, or its call's end was lost) says on that terminal that the keyboard is free again; true when said.
func (s *Serveur) gardeFouVerrou(e *tacheEnCours, maintenant time.Time) bool {
	e.mu.Lock()
	libre := e.annonce && !e.verrouille(maintenant) && maintenant.Sub(e.finAppel) >= delaiClavierLibre && e.t.Terminal != ""
	if libre {
		e.annonce = false
	}
	e.mu.Unlock()
	if libre {
		ecrireDansTerminal(e, "\x1b[2m"+s.texteTerminal("No command of the assistant is running: you can type until its next one.")+"\x1b[0m\n")
	}
	return libre
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

// libellesOutils: the Servers tools as a beginner reads them (C1-10); the English texts are i18n.js keys, so the
// page and the terminal's screen say them in the owner's language (TestLibellesOutils checks every language).
var libellesOutils = map[string]string{
	"ssh_commande": "Run a command", "serveurs_liste": "List the servers", "serveur_fiche": "Read the server's sheet", "fichier_lire": "Read a file",
	"fichier_ecrire": "Write a file", "sftp_envoyer": "Send a file", "sftp_recuperer": "Fetch a file",
	"service": "Service", "docker": "Docker", "pare_feu": "Firewall", "proposer_plan": "Propose a plan",
	"confirmer_changement": "Confirm a change", "runbook_lire": "Read the server logbook", "runbook_ajouter": "Add to the server logbook",
	"environnement_lire": "Read the project's deployment", "deployer": "Deploy", "revenir_version": "Go back to the previous version",
	"fournisseurs": "Cloudflare zones and rights", "dns_lister": "List the DNS records", "dns_ecrire": "Write a DNS record",
	"dns_supprimer": "Delete a DNS record", "ssl_lire": "Read the SSL settings", "ssl_regler": "Set SSL",
	"certificat_origine": "Origin certificate", "access_proteger": "Protect with Access", "access_retirer": "Remove the Access protection",
}

// texteTerminal: a text written on the terminal's screen, in the app's language (C1-9).
func (s *Serveur) texteTerminal(cle string, args ...any) string {
	s.mu.Lock()
	l := s.config.Langue
	s.mu.Unlock()
	return traduireTUI(dicoTUI(l), cle, args...)
}

// ligneAction: a call as the owner's terminal shows it ("$ command" for a command, else the tool's label).
func (s *Serveur) ligneAction(a ActionTache) string {
	q := s.texteTerminal(cmp.Or(a.Libelle, a.Outil)) + " " + a.Quoi
	if a.Outil == "ssh_commande" {
		q = "$ " + a.Quoi
	}
	return "\n\x1b[1;36m[" + s.texteTerminal("Assistant") + "]\x1b[0m " + strings.TrimSpace(q) + "\n"
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

// errPlanRequis: the step runs only inside an accepted plan; detailPlanRequis says it to the owner (a key of i18n.js).
var errPlanRequis = errors.New("plan required")

const detailPlanRequis = "Not run: with these permissions it only runs as a step of an accepted plan."

// debutAction: a call of x's session starts, or was refused (refus); nil without a running task.
func (s *Serveur) debutAction(x SessionHub, outil string, a map[string]json.RawMessage, e Etape, refus error) (*tacheEnCours, int) {
	t := tacheDeSession(x.Hache)
	if t == nil {
		return nil, 0
	}
	act := ActionTache{T: time.Now().UTC().Format(time.RFC3339), Outil: outil, Libelle: libellesOutils[outil], Quoi: couper(s.masquerSecrets(quoiAction(a)), 500), Etat: "en_cours"}
	if refus != nil {
		if c, err := classerEtape(outil, maps.Clone(a)); err == nil {
			act.Classe = c.Classe.String()
		}
		act.Etat, act.Code, act.Detail = "refusee", -1, couper(s.masquerSecrets(refus.Error()), 500)
		if errors.Is(refus, errPlanRequis) { // nobody refused anything: the assistant proposes its plan next (the sentence is the owner's, the error the model's)
			act.Etat, act.Detail = "plan_requis", detailPlanRequis
		}
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
	t.annonce = t.annonce || t.verrouille(time.Now())
	t.mu.Unlock()
	_ = enregistrerTache(t.copie())
	l := s.ligneAction(act)
	if refus != nil {
		l += "\x1b[33m✗ " + s.texteTerminal(act.Detail) + "\x1b[0m\n"
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
	t.finAppel = time.Now()
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
			ecran += "\x1b[31m✗ " + strings.TrimSpace(s.texteTerminal("Exit code {0}", a.Code)+" "+s.texteTerminal(a.Detail)) + "\x1b[0m\n" // M275: a known detail in the owner's language too
		}
	}
	t.mu.Unlock()
	ecrireDansTerminal(t, ecran)
	_ = enregistrerTache(t.copie())
}
