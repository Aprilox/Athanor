package main

// Plans (M90, ARCHITECTURE.md §Plans): a Servers agent proposes its changes as a plan (proposer_plan), the
// owner accepts or refuses it as an approval "plan_serveur" (the window with the password retyped, or a paired
// device). An accepted step runs once, only as it was accepted (the tool and its canonical arguments), before
// the plan expires (2 h); any other change is refused "propose a plan". Destructive or prod: an accord every
// time (I6), never a rule; a change on a non-prod server: the owner may say "do not ask again for this server".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	actionPlanServeur = "plan_serveur"
	dureePlan         = 2 * time.Hour // a plan not decided, or not run, within this delay is over
	maxEtapesPlan     = 30
)

var errPlanSansRegle = erreurEntree("This plan has a destructive step or targets a prod server: it is accepted once, never with a rule")

// EtapePlan: one step as the code classed it; Arguments are canonical (argsOutilServeur marshalled).
type EtapePlan struct {
	Outil      string          `json:"outil"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Lisible    string          `json:"lisible,omitempty"` // the arguments as the page shows them (masked, cut)
	Classe     string          `json:"classe"`
	Raison     string          `json:"raison"`
	Pourquoi   string          `json:"pourquoi"`
	Sauvegarde []string        `json:"sauvegarde,omitempty"`
	Executee   string          `json:"executee,omitempty"`
}

// PlanServeur: hub/plans/<id>.json, id = its approval's.
type PlanServeur struct {
	ID       string      `json:"id"`
	Serveur  string      `json:"serveur"`
	Nom      string      `json:"nom"`
	Env      string      `json:"env"`
	Projet   string      `json:"projet"`
	Mission  string      `json:"mission,omitempty"`
	Agent    string      `json:"agent"`
	Resume   string      `json:"resume"`
	Cree     string      `json:"cree"`
	Expire   string      `json:"expire"`
	Critique string      `json:"critique,omitempty"` // the security critic's verdict (M98)
	Etapes   []EtapePlan `json:"etapes"`
}

// verrouPlans: a step is marked run under it, so it runs once.
var verrouPlans sync.Mutex

func init() {
	parmi := func(d string, l ...string) map[string]any {
		return map[string]any{"type": "string", "description": d, "enum": append([]string{}, l...)}
	}
	outils := slices.Sorted(func(f func(string) bool) {
		for k := range outilsServeurs {
			if !f(k) {
				return
			}
		}
	})
	etape := schemaObjet(map[string]any{
		"outil":      parmi("The Servers tool of this step", outils...),
		"arguments":  map[string]any{"type": "object", "description": "The tool's own arguments, exactly as you will call it (without serveur)"},
		"classe":     parmi("Your own class of this step; the code's class is kept when higher", "lecture", "changement", "destructrice"),
		"pourquoi":   schemaTexte("Why this step, in one sentence"),
		"sauvegarde": map[string]any{"type": "array", "items": schemaTexte("Absolute path on the server"), "description": "Paths to back up before a destructive step"},
	}, "outil", "arguments", "pourquoi")
	outilsHub["proposer_plan"] = OutilHub{Description: "Propose the changes you want to make on a server as a plan for the owner. " +
		"Once accepted, call each step's tool with exactly its arguments, each step once, within 2 hours. A change outside an accepted plan is refused.",
		Schema: schemaObjet(map[string]any{"serveur": schemaTexte("The server's id, from serveurs_liste"),
			"resume": schemaTexte("What the plan does, in one or two sentences"),
			"etapes": map[string]any{"type": "array", "items": etape, "description": "The steps, in order"}}, "serveur", "resume", "etapes"),
		Faire: outilProposerPlan}
}

// argumentsDe: a's keys are tool nom's own arguments (a server is named by its id only).
func argumentsDe(nom string, a map[string]json.RawMessage) error {
	props := outilsHub[nom].Schema["properties"].(map[string]any)
	for k := range a {
		if _, ok := props[k]; !ok || k == "serveur" {
			return fmt.Errorf("invalid arguments: unknown %q (a server is named by its id only, a tool takes only its own arguments)", couper(k, 40))
		}
	}
	return nil
}

func (s *Serveur) dossierPlans() string { return filepath.Join(s.approbations.dossier, "plans") }

func (s *Serveur) lirePlan(id string) (PlanServeur, error) {
	var p PlanServeur
	b, err := os.ReadFile(filepath.Join(s.dossierPlans(), id+".json"))
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	return p, err
}

func (s *Serveur) enregistrerPlan(p PlanServeur) error {
	d := s.dossierPlans()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(p) // compact: MarshalIndent would re-indent the canonical arguments a step is matched on
	return ecrireAtomique(filepath.Join(d, p.ID+".json"), b, 0o600)
}

// purgerPlans removes the plans written long ago (their approval is purged after the same delay).
func (s *Serveur) purgerPlans() {
	l, _ := os.ReadDir(s.dossierPlans())
	for _, f := range l {
		if i, err := f.Info(); err == nil && time.Since(i.ModTime()) > gardeApprobations {
			_ = os.Remove(filepath.Join(s.dossierPlans(), f.Name()))
		}
	}
}

// serveurProd: the server's inventory entry and whether it has the prod role (unknown: prod, the safe side).
func serveurProd(id string) (ServeurInventaire, bool) {
	l, _ := lireServeurs()
	if i := slices.IndexFunc(l, func(v ServeurInventaire) bool { return v.ID == id }); i >= 0 {
		return l[i], slices.Contains(l[i].Roles, "prod")
	}
	return ServeurInventaire{ID: id}, true
}

func reglePlan(projet, id string) RegleAccord {
	return RegleAccord{Projet: projet, Env: "dev", Action: actionPlanServeur + ":" + id}
}

func outilProposerPlan(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		Serveur string `json:"serveur"`
		Resume  string `json:"resume"`
		Etapes  []struct {
			Outil      string                     `json:"outil"`
			Arguments  map[string]json.RawMessage `json:"arguments"`
			Classe     string                     `json:"classe"`
			Pourquoi   string                     `json:"pourquoi"`
			Sauvegarde []string                   `json:"sauvegarde"`
		} `json:"etapes"`
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if err := s.serveurPerimetre(x, "proposer_plan", a.Serveur); err != nil {
		return "", err
	}
	resume, err := texteRequis("resume", a.Resume, 500)
	if err != nil {
		return "", err
	}
	if len(a.Etapes) == 0 || len(a.Etapes) > maxEtapesPlan {
		return "", fmt.Errorf("a plan has 1 to %d steps", maxEtapesPlan)
	}
	srv, prod := serveurProd(a.Serveur)
	p := PlanServeur{Serveur: a.Serveur, Nom: srv.Nom, Env: "dev", Projet: x.Projet, Mission: x.Mission, Agent: x.Agent, Resume: resume}
	if prod {
		p.Env = "prod"
	}
	haut, resumeEtapes := ClasseLecture, []string{}
	for i, e := range a.Etapes {
		echec := func(err error) (string, error) {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "serveur:" + a.Serveur, Projet: x.Projet, Mission: x.Mission,
				Agent: x.Agent, Action: "serveurs.proposer_plan", Resultat: "refusé : " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)})
			return "", err
		}
		if _, ok := outilsServeurs[e.Outil]; !ok {
			return echec(fmt.Errorf("step %d: unknown tool %q", i+1, couper(e.Outil, 40)))
		}
		if e.Arguments == nil {
			e.Arguments = map[string]json.RawMessage{}
		}
		if err := argumentsDe(e.Outil, e.Arguments); err != nil {
			return echec(fmt.Errorf("step %d: %w", i+1, err))
		}
		if e.Classe != "" {
			e.Arguments["classe"], _ = json.Marshal(e.Classe)
		}
		et, err := classerEtape(e.Outil, e.Arguments)
		if err != nil {
			return echec(fmt.Errorf("step %d: %w", i+1, err))
		}
		pourquoi, err := texteRequis("pourquoi", e.Pourquoi, 300)
		if err != nil {
			return echec(fmt.Errorf("step %d: %w", i+1, err))
		}
		var sauvegarde []string
		for _, c := range e.Sauvegarde[:min(len(e.Sauvegarde), 20)] {
			sauvegarde = append(sauvegarde, couper(c, 300)) // shortcut: checked and archived by M92
		}
		haut = max(haut, et.Classe)
		p.Etapes = append(p.Etapes, EtapePlan{Outil: et.Outil, Arguments: et.Arguments, Classe: et.Classe.String(), Raison: et.Raison, Pourquoi: pourquoi, Sauvegarde: sauvegarde})
		resumeEtapes = append(resumeEtapes, et.Outil+" "+et.Classe.String())
	}
	regle := !prod && haut < ClasseDestructrice
	demande := Approbation{Projet: x.Projet, Env: p.Env, Mission: x.Mission, Agent: x.Agent, Action: actionPlanServeur, Raison: resume,
		Details: map[string]string{"serveur": p.Serveur, "nom": p.Nom, "classe": haut.String(), "etapes": strconv.Itoa(len(p.Etapes)), "regle": map[bool]string{true: "oui", false: "non"}[regle]}}
	// the owner's "do not ask again for this server": the plan is accepted at once, without a ping
	parRegle := regle && s.approbations.Regle(reglePlan(x.Projet, p.Serveur))
	var ap Approbation
	if parRegle {
		ap, err = s.approbations.Demander(demande)
	} else {
		ap, err = s.demanderApprobation(demande)
	}
	if err != nil {
		return "", err
	}
	p.ID, p.Cree, p.Expire = ap.ID, ap.Cree, ap.Expire
	s.purgerPlans()
	if err := s.enregistrerPlan(p); err != nil {
		_, _, _ = s.deciderApprobation("hub", ap.ID, "refuser", "", "", "")
		return "", fmt.Errorf("the plan could not be recorded: %v", err)
	}
	etat := "attente"
	if parRegle {
		if ap, _, err = s.deciderApprobation("regle", ap.ID, "accepter", "", "", ""); err != nil {
			return "", err
		}
		etat = ap.Etat
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "serveur:" + p.Serveur, Projet: x.Projet, Mission: x.Mission, Env: p.Env,
		Agent: x.Agent, Action: "serveurs.proposer_plan", Resultat: "plan " + p.ID + " → " + etat + " : " + strings.Join(resumeEtapes, ", ")})
	etapes := []map[string]any{}
	for i, e := range p.Etapes {
		etapes = append(etapes, map[string]any{"n": i + 1, "outil": e.Outil, "classe": e.Classe, "raison": e.Raison})
	}
	return enJSON(map[string]any{"plan": p.ID, "etat": etat, "expire": p.Expire, "etapes": etapes,
		"consigne": "Once the owner accepts, call each step's tool with exactly its arguments, once, before the plan expires. Until then a step is refused with the plan's state."}), nil
}

// etapeAccordee: e is a step not run yet of a plan of x on server id that the owner accepted and that has not
// expired: it is marked run (once) and its plan's id and number returned. Otherwise "" and, when a plan holds
// e, why it cannot run (pending, refused, expired, already run).
func (s *Serveur) etapeAccordee(x SessionHub, id string, e Etape) (string, int, string, error) {
	l, _ := s.approbations.Liste()
	t := s.approbations.maintenant()
	verrouPlans.Lock()
	defer verrouPlans.Unlock()
	info := "" // the newest plan's reason
	for _, ap := range l {
		if ap.Action != actionPlanServeur || ap.Projet != x.Projet || ap.Agent != x.Agent || ap.Mission != x.Mission || ap.Details["serveur"] != id {
			continue
		}
		p, err := s.lirePlan(ap.ID)
		if err != nil {
			continue
		}
		pareille := func(et EtapePlan) bool { return et.Outil == e.Outil && bytes.Equal(et.Arguments, e.Arguments) }
		i := slices.IndexFunc(p.Etapes, func(et EtapePlan) bool { return pareille(et) && et.Executee == "" })
		exp, err := time.Parse(time.RFC3339, p.Expire)
		pourquoi := ""
		switch {
		case i < 0 && slices.ContainsFunc(p.Etapes, pareille):
			pourquoi = "plan " + p.ID + " already ran this step: a step runs once"
		case i < 0:
		case ap.Etat == "acceptee" && err == nil && t.Before(exp):
			p.Etapes[i].Executee = t.Format(time.RFC3339)
			if err := s.enregistrerPlan(p); err != nil {
				return "", 0, "", err
			}
			return p.ID, i + 1, "", nil
		case ap.Etat == "attente":
			pourquoi = "plan " + p.ID + " waits for the owner's decision"
		case ap.Etat == "acceptee":
			pourquoi = "plan " + p.ID + " has expired"
		default:
			pourquoi = "plan " + p.ID + " was " + map[string]string{"refusee": "refused", "expiree": "not decided in time"}[ap.Etat]
		}
		if info == "" {
			info = pourquoi
		}
	}
	return "", 0, info, nil
}

// planOuRegle (called by avantOutil for a change or a destructive call): the call runs as a step of an
// accepted plan, or as a change under the owner's rule for this non-prod server; otherwise refused.
func (s *Serveur) planOuRegle(x SessionHub, id string, e Etape) error {
	plan, n, info, err := s.etapeAccordee(x, id, e)
	if err != nil {
		return err
	}
	audit := func(res string) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "serveur:" + id, Projet: x.Projet, Mission: x.Mission,
			Agent: x.Agent, Action: "serveurs." + e.Outil, Resultat: res})
	}
	if plan != "" {
		audit(fmt.Sprintf("accordé : plan %s, étape %d (%s)", plan, n, e.Classe))
		return nil
	}
	if _, prod := serveurProd(id); e.Classe == ClasseChangement && !prod && s.approbations.Regle(reglePlan(x.Projet, id)) {
		audit("accordé : règle « ne plus demander » pour ce serveur (changement)")
		return nil
	}
	if info != "" {
		info = "; " + info
	}
	return fmt.Errorf("plan required: this is a %s action (%s); a change runs only as a step of a plan the owner accepted, with exactly its arguments: propose a plan (proposer_plan)%s", e.Classe, e.Raison, info)
}

// plansAffiches: the plans of the approvals l, as the page shows them (arguments masked and cut).
func (s *Serveur) plansAffiches(l []Approbation) map[string]PlanServeur {
	out := map[string]PlanServeur{}
	for _, x := range l {
		if x.Action != actionPlanServeur {
			continue
		}
		p, err := s.lirePlan(x.ID)
		if err != nil {
			continue
		}
		for i := range p.Etapes {
			p.Etapes[i].Lisible = couper(s.masquerSecrets(string(p.Etapes[i].Arguments)), 4000)
			p.Etapes[i].Arguments = nil
		}
		p.Resume = s.masquerSecrets(p.Resume)
		out[x.ID] = p
	}
	return out
}
