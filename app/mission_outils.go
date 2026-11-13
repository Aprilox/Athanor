package main

// The orchestrator's tools (ARCHITECTURE.md §Missions et orchestrateur, outilsOrchestrateur): it reads the state of
// the modules, asks the bus for reading actions only (actionsOrchestrateur), records its card (checked and computed
// by the Go), asks the owner one question, and asks the owner to launch. Its token holds only these: every tool of
// Dev, Servers, Cloudflare or the deployments is unknown to it. No answer carries a vault value nor a file path.

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// actionMission: the approval lancer_mission asks for (accepting it launches the mission, M158).
const actionMission = "mission"

const (
	maxQuestion      = 500
	maxChoixQuestion = 4
	maxChoix         = 100
)

func init() {
	objet, texte := schemaObjet, schemaTexte
	outilsHub["modules_etat"] = OutilHub{Description: "Athanor's state: its modules, the projects (state, plan, environments with URL and protection), the servers (id, name, state, hardened) and the names and types of the vault entries (never a value).",
		Schema: objet(map[string]any{}), Faire: outilModulesEtat}
	outilsHub["proposer_mission"] = OutilHub{Description: "Record the mission card the owner will read. Athanor checks it and computes the approvals, what will be touched and the estimate; an error says what to fix.",
		Schema: objet(map[string]any{"carte": map[string]any{"type": "object", "description": "titre, projet{nom, nouveau}, dev{brief, modes{parallele, eco, rapide} for a new project only}, envs[]{nom, url, protection, serveurs[]}, serveurs{brief}, acces[]{entree, pour}, jalons_estimes"}}, "carte"),
		Faire:  outilProposerMission}
	outilsHub["demander_module"] = OutilHub{Description: "Ask a module a reading action on the bus. Allowed today: module dev, action etat_projet, donnees {\"projet\": name}.",
		Schema: objet(map[string]any{"module": texte("The module, e.g. dev"), "action": texte("The action, e.g. etat_projet"), "donnees": map[string]any{"type": "object", "description": "e.g. {\"projet\": \"name\"}"}}, "module", "action"),
		Faire:  outilDemanderModule}
	outilsHub["question_proprietaire"] = OutilHub{Description: "Ask the owner one short question, with at most 4 choices (they can also answer freely). Then stop: Athanor resumes your session with the answer.",
		Schema: objet(map[string]any{"texte": texte("The question, one or two sentences"), "choix": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": maxChoixQuestion}}, "texte"),
		Faire:  outilQuestionProprietaire}
	outilsHub["lancer_mission"] = OutilHub{Description: "Ask the owner to launch the proposed card (only when the owner's message asks for it). Accepting it launches the mission as the Launch button does.",
		Schema: objet(map[string]any{"id": texte("The mission's id, as proposer_mission gave it")}, "id"), Faire: outilLancerMission}
}

// questionOuverte: the mission's unanswered question, if any.
func questionOuverte(m Mission) (QuestionMission, bool) {
	i := slices.IndexFunc(m.Questions, func(q QuestionMission) bool { return q.Reponse == "" })
	if i < 0 {
		return QuestionMission{}, false
	}
	return m.Questions[i], true
}

// preparable: the orchestrator may still change the card or ask a question.
func preparable(m Mission) error {
	if m.Etat != "preparation" && m.Etat != "proposee" {
		return fmt.Errorf("this mission is %s: its card can no longer be changed", m.Etat)
	}
	if _, ok := questionOuverte(m); ok {
		return errors.New("a question waits for the owner's answer: stop now, Athanor resumes your session with it")
	}
	return nil
}

func outilModulesEtat(s *Serveur, _ SessionHub, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	modules := []map[string]any{}
	for _, m := range modulesHub() {
		r := m.Resume(s)
		stats := map[string]any{}
		for _, x := range r.Stats {
			stats[x.Cle] = x.Valeur
		}
		modules = append(modules, map[string]any{"id": m.ID(), "nom": m.Nom(), "etat": r.Etat, "stats": stats})
	}
	inv, _ := lireServeurs()
	noms := map[string]string{}
	serveurs := []map[string]any{}
	for _, v := range inv {
		noms[v.ID] = v.Nom
		serveurs = append(serveurs, map[string]any{"id": v.ID, "nom": v.Nom, "hote": v.Hote, "etat": v.Etat, "roles": v.Roles, "durci": v.MotDePasseCoupe})
	}
	projets := []map[string]any{}
	for _, pv := range s.vuesProjets("") {
		p := map[string]any{"nom": pv.Nom, "actif": pv.Actif, "phase": pv.Phase, "besoin": pv.Besoin,
			"plan": fmt.Sprintf("%d/%d milestones; now: %s", pv.Faits, pv.Jalons, firstNonEmpty(pv.JalonTitre, pv.Jalon))}
		if s.acces != nil && s.accesErr == nil {
			envs := []map[string]any{}
			for nom, e := range s.acces.Environnements(pv.Nom) {
				srv := []string{}
				for _, id := range e.Serveurs {
					srv = append(srv, firstNonEmpty(noms[id], id))
				}
				envs = append(envs, map[string]any{"nom": nom, "url": e.URL, "protection": e.Protection, "serveurs": srv})
			}
			slices.SortFunc(envs, func(a, b map[string]any) int { return strings.Compare(a["nom"].(string), b["nom"].(string)) })
			p["environnement"], p["environnements"] = s.acces.Projet(pv.Nom).Env, envs
		}
		projets = append(projets, p)
	}
	coffre := []map[string]string{}
	if s.coffre != nil && s.coffreErr == nil {
		for _, e := range s.coffre.Liste() {
			coffre = append(coffre, map[string]string{"nom": e.Nom, "type": e.Type, "famille": e.Famille, "portee": e.Portee})
		}
	}
	return enJSON(map[string]any{"modules": modules, "projets": projets, "serveurs": serveurs, "coffre": coffre}), nil
}

func outilProposerMission(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ Carte json.RawMessage }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	var c CarteMission
	if len(a.Carte) == 0 || string(a.Carte) == "null" {
		return "", errors.New("carte is required")
	}
	if err := json.Unmarshal(a.Carte, &c); err != nil {
		return "", fmt.Errorf("the card does not have the expected shape (%v): fix it and call proposer_mission again", err)
	}
	if _, ok := s.accordMissionAttente(x.Mission); ok { // C77: the card the owner is asked to launch stays as it is
		return "", errors.New("the owner has your launch request to decide: the card can no longer be changed, stop now")
	}
	for i := range c.Acces { // C83: unticking is the owner's, never the agent's
		c.Acces[i].Decoche = false
	}
	for i := range c.Envs { // C81: an environment given by the owner never comes from the agent
		c.Envs[i].Proprietaire = false
	}
	ctx := s.contexteCarte()
	if err := validerCarte(&c, ctx); err != nil {
		return "", errors.New("the card was refused, fix it and call proposer_mission again: " + err.Error())
	}
	m, err := modifierMission(x.Mission, func(m *Mission) error {
		if err := preparable(*m); err != nil {
			return err
		}
		var avant CarteMission // C83: the boxes the owner unticked stay unticked, by (entry, pour)
		_ = json.Unmarshal(m.Carte, &avant)
		for i := range c.Acces {
			c.Acces[i].Decoche = slices.ContainsFunc(avant.Acces, func(a AccesCarte) bool { return a.Decoche && memeAcces(a, c.Acces[i]) })
		}
		c = calculerCarte(c, m.Politique, ctx)
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		m.Carte, m.Etat = b, "proposee"
		m.Historique = append(m.Historique, EvenementMission{T: time.Now().UTC().Format(formatCree), Etat: "proposee", Detail: c.Titre})
		return nil
	})
	if err != nil {
		return "", err
	}
	return enJSON(map[string]any{"mission": m.ID, "etat": m.Etat, "carte": c,
		"suite": "The owner sees the card now. Say in one or two plain sentences what it proposes, then stop."}), nil
}

func outilDemanderModule(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		Module, Action string
		Donnees        map[string]any
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	module, err := texteRequis("module", a.Module, 64)
	if err != nil {
		return "", err
	}
	action, err := texteRequis("action", a.Action, 64)
	if err != nil {
		return "", err
	}
	projet, _ := a.Donnees["projet"].(string)
	env := ""
	if s.acces != nil && s.accesErr == nil && projet != "" {
		env = s.acces.Projet(projet).Env
	}
	rep := s.envoyerBus(MessageHub{De: agentOrchestrateur, Vers: module, Action: action, Projet: projet, Env: env,
		Mission: x.Mission, Agent: x.Agent, Donnees: a.Donnees})
	if rep.Etat != "ok" {
		return "", fmt.Errorf("%s: %s", rep.Etat, rep.Raison)
	}
	return enJSON(rep.Donnees), nil
}

func outilQuestionProprietaire(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		Texte string
		Choix []string
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	texte, err := texteRequis("texte", a.Texte, maxQuestion)
	if err != nil {
		return "", err
	}
	if len(a.Choix) > maxChoixQuestion {
		return "", fmt.Errorf("%d choices at most", maxChoixQuestion)
	}
	q := QuestionMission{ID: aleatoire(8), Texte: s.masquerSecrets(texte), Choix: []string{}, Cree: time.Now().UTC().Format(formatCree)}
	for _, c := range a.Choix {
		if c = strings.TrimSpace(c); c != "" {
			q.Choix = append(q.Choix, s.masquerSecrets(couper(c, maxChoix)))
		}
	}
	m, err := modifierMission(x.Mission, func(m *Mission) error {
		if err := preparable(*m); err != nil {
			return err
		}
		m.Questions = append(m.Questions, q)
		return nil
	})
	if err != nil {
		return "", err
	}
	s.notifierAppareils(MessagePush{Titre: s.textePush("besoin") + " · " + titreMission(m), Texte: q.Texte, URL: "/#missions", Tag: "question-" + q.ID})
	return "question recorded: stop now, Athanor resumes your session with the owner's answer", nil
}

func outilLancerMission(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct{ ID string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.ID) != x.Mission {
		return "", errors.New("id must be your mission's id, " + x.Mission)
	}
	m, err := lireMission(x.Mission)
	if err != nil {
		return "", err
	}
	if err := preparable(m); err != nil {
		return "", err
	}
	if m.Etat != "proposee" {
		return "", errors.New("there is no card yet: call proposer_mission first")
	}
	if s.approbations == nil {
		return "", errors.New("Athanor's approvals are unavailable right now")
	}
	attente := func(id string) string {
		return "waiting for the owner, id " + id + ": once accepted, Athanor launches the mission as if the owner pressed Launch. Stop now."
	}
	if d, ok := s.accordMissionAttente(m.ID); ok {
		return attente(d.ID), nil
	}
	titre := titreMission(m)
	details := resumeCarteAccord(m)
	details["mission"], details["titre"], details["empreinte"] = m.ID, titre, empreinteCarte(m)
	d, err := s.demanderApprobation(Approbation{Projet: prefixeMission + m.ID, Mission: m.ID, Agent: x.Agent, Action: actionMission,
		Details: details, Raison: "Launch the mission: " + titre})
	if err != nil {
		return "", errors.New("the request could not be recorded, try again later")
	}
	return attente(d.ID), nil
}

// titreMission: the card's title, or the start of the owner's sentence before a card exists.
func titreMission(m Mission) string {
	var c struct{ Titre string }
	if json.Unmarshal(m.Carte, &c) == nil && c.Titre != "" {
		return c.Titre
	}
	return couper(m.Phrase, 80)
}

// memeAcces: the same vault entry (its name in any case) for the same target.
func memeAcces(a, b AccesCarte) bool {
	return strings.EqualFold(a.Entree, b.Entree) && a.Pour == b.Pour
}

// empreinteCarte (C77): the stored card (compacted) and the policy, hashed; the launch accord carries it, and
// accepting it launches only that very card.
func empreinteCarte(m Mission) string {
	var b bytes.Buffer
	_ = json.Compact(&b, m.Carte)
	h := sha256.Sum256(append([]byte(m.Politique+"\n"), b.Bytes()...))
	return hex.EncodeToString(h[:])
}

// resumeCarteAccord: what the launch accord shows (bell, For you, push): the environments with their host, prod
// when the card has it, and the ticked access, "—" for none.
func resumeCarteAccord(m Mission) map[string]string {
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	envs, acces, prod := []string{}, []string{}, ""
	for _, e := range c.Envs {
		envs = append(envs, strings.TrimSpace(e.Nom+" "+hoteURL(e.URL)))
		if e.Nom == "prod" {
			prod = "oui"
		}
	}
	for _, a := range c.Acces {
		if !a.Decoche {
			acces = append(acces, a.Entree+" → "+a.Pour)
		}
	}
	return map[string]string{"envs": cmp.Or(strings.Join(envs, ", "), "—"), "acces": cmp.Or(strings.Join(acces, ", "), "—"), "prod": prod}
}

// accordMissionAttente: mission id's pending launch request (lancer_mission), if any.
func (s *Serveur) accordMissionAttente(id string) (Approbation, bool) {
	if s.approbations == nil {
		return Approbation{}, false
	}
	l, _ := s.approbations.Liste()
	i := slices.IndexFunc(l, func(d Approbation) bool { return d.Mission == id && d.Action == actionMission && d.Etat == "attente" })
	if i < 0 {
		return Approbation{}, false
	}
	return l[i], true
}
