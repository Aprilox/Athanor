package main

// The orchestrator's session (ARCHITECTURE.md §Missions et orchestrateur, M153): a Claude session like the ops
// lead's (no built-in tool, the hub's tools only, an empty working folder, no secret in its environment), in
// hub/missions/<id>-session/, removed at its end with its token revoked. A later session (an answer, a change
// asked for the card) resumes the same Claude session with that text first. A failed or overlong session puts the
// mission in echec with a readable reason.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	prefixeMission     = "missions:" // the project of an orchestrator's session: never a real project
	agentOrchestrateur = "orchestrateur"
)

// delaiOrchestrateur: one orchestrator session (a variable: tests shorten it).
var delaiOrchestrateur = 10 * time.Minute

var (
	errOrchestrateurOccupe = errors.New("the orchestrator is already working on this mission")
	errOrchestrateurDelai  = errors.New("the orchestrator took more than 10 minutes: nothing was prepared, send the mission again")
)

// orchestrateurEnCours: a running session, its token's hash and how to stop it.
type orchestrateurEnCours struct {
	hache   string
	annuler context.CancelCauseFunc
}

var orchestrateursEnCours = struct {
	sync.Mutex
	m map[string]*orchestrateurEnCours // by mission id
}{m: map[string]*orchestrateurEnCours{}}

// orchestrateurActif: the session of hache is a running orchestrator's (its token stays valid while it runs).
func orchestrateurActif(hache string) bool {
	orchestrateursEnCours.Lock()
	defer orchestrateursEnCours.Unlock()
	for _, e := range orchestrateursEnCours.m {
		if e.hache == hache {
			return true
		}
	}
	return false
}

// sessionOrchestrateur runs one Claude session in dossier (consigne.txt, reprise, hub-jeton and mcp.json are there;
// it works in dossier/travail and writes its stream to session.jsonl). Tests replace it.
var sessionOrchestrateur = func(s *Serveur, ctx context.Context, dossier string) error {
	return executerSessionKit(ctx, commandeSessionKit(s, "session-orchestrateur", dossier), delaiOrchestrateur+time.Minute)
}

func dossierSessionMission(id string) string { return filepath.Join(dossierMissions(), id+"-session") }

// mentionsLisibles: the mission's mentions by readable name (a server with its id and host, an entry with its type).
func (s *Serveur) mentionsLisibles(m MentionsMission) []string {
	var l []string
	for _, p := range m.Projets {
		l = append(l, "project "+p)
	}
	serveurs, _ := lireServeurs()
	for _, id := range m.Serveurs {
		if i := slices.IndexFunc(serveurs, func(x ServeurInventaire) bool { return x.ID == id }); i >= 0 {
			l = append(l, "server "+serveurs[i].Nom+" (id "+id+", "+serveurs[i].Hote+")")
		}
	}
	if s.coffre != nil && s.coffreErr == nil {
		entrees := s.coffre.Liste()
		for _, id := range m.Cles {
			if i := slices.IndexFunc(entrees, func(e EntreeCoffre) bool { return e.ID == id }); i >= 0 {
				l = append(l, "vault entry "+entrees[i].Nom+" ("+entrees[i].Type+")")
			}
		}
	}
	if m.Nouveau {
		l = append(l, "a new project")
	}
	return l
}

// lancerOrchestrateur: one session for mission id, in the background. The first one gets the instructions; once
// the mission has a Claude session, the next one resumes it with enTete first (the owner's answer or change).
func (s *Serveur) lancerOrchestrateur(id, enTete string) error {
	m, err := lireMission(id)
	if err != nil {
		return err
	}
	if s.jetons == nil {
		return errors.New("the hub's sessions cannot be read")
	}
	orchestrateursEnCours.Lock()
	defer orchestrateursEnCours.Unlock()
	if orchestrateursEnCours.m[id] != nil {
		return errOrchestrateurOccupe
	}
	consigne := consigneOrchestrateur(m.Phrase, s.mentionsLisibles(m.Mentions))
	switch {
	case m.Session != "":
		consigne = consigneRepriseOrchestrateur(enTete)
	case enTete != "":
		consigne += "\n" + consigneRepriseOrchestrateur(enTete)
	}
	dossier := dossierSessionMission(id)
	_ = os.RemoveAll(dossier) // left by a crash
	projet := prefixeMission + id
	err = os.MkdirAll(filepath.Join(dossier, "travail"), 0o700)
	var jeton string
	if err == nil {
		jeton, err = s.jetons.CreerSession(SessionHub{Projet: projet, Mission: id, Agent: agentOrchestrateur, Outils: outilsOrchestrateur})
	}
	if err == nil {
		err = ecrireAtomique(filepath.Join(dossier, "hub-jeton"), []byte(jeton+"\n"), 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dossier, "consigne.txt"), []byte(s.masquerSecrets(consigne)), 0o600)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dossier, "reprise"), []byte(m.Session), 0o600)
	}
	if err != nil {
		_, _ = s.jetons.Revoquer(func(x SessionHub) bool { return x.Projet == projet })
		_ = os.RemoveAll(dossier)
		return err
	}
	ctx, annuler := context.WithCancelCause(context.Background())
	e := &orchestrateurEnCours{hache: hacherJeton(jeton), annuler: annuler}
	orchestrateursEnCours.m[id] = e
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "missions", Projet: projet, Mission: id, Agent: agentOrchestrateur,
		Action: "orchestrateur lancé", Resultat: map[bool]string{true: "reprise", false: "nouvelle session"}[m.Session != ""]})
	go s.executerOrchestrateur(ctx, e, id, dossier)
	return nil
}

// executerOrchestrateur: the session, then its end: token revoked, folder removed, the mission in echec if the
// session failed, overran or ended without a card nor a question.
func (s *Serveur) executerOrchestrateur(ctx context.Context, e *orchestrateurEnCours, id, dossier string) {
	ctx, fin := context.WithTimeoutCause(ctx, delaiOrchestrateur, errOrchestrateurDelai)
	go func() { // a revoked token (emergency stop, the owner's revocation, expiry) stops the session
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			if !slices.ContainsFunc(s.jetons.Liste(), func(x SessionHub) bool { return x.Hache == e.hache }) {
				e.annuler(errTacheRevoquee)
			}
		}
	}()
	journal := filepath.Join(dossier, "session.jsonl")
	err := sessionOrchestrateur(s, ctx, dossier)
	sid, resultat, enErreur := lireSessionClaude(journal)
	b, _ := os.ReadFile(journal)
	switch cause := context.Cause(ctx); {
	case bytes.Contains(b, []byte(`"error":"authentication_failed"`)):
		err = errClaudeNonConnecte
	case cause != nil:
		err = cause
	case err != nil:
		err = errors.New("the orchestrator's session failed: " + err.Error())
	case enErreur:
		err = errors.New("the orchestrator's session ended with an error: " + couper(resultat, 200))
	case sid == "":
		err = errors.New("the orchestrator's session did not start (no session in its journal)")
	}
	fin()

	orchestrateursEnCours.Lock()
	delete(orchestrateursEnCours.m, id)
	orchestrateursEnCours.Unlock()
	s.revoquerJetons("orchestrateur terminé", func(x SessionHub) bool { return x.Hache == e.hache })
	_ = os.RemoveAll(dossier)
	_, _ = modifierMission(id, func(m *Mission) error {
		if sid != "" {
			m.Session = sid
		}
		if err == nil && m.Etat == "preparation" && !slices.ContainsFunc(m.Questions, func(q QuestionMission) bool { return q.Reponse == "" }) {
			err = errors.New("the orchestrator ended without proposing a card")
			if resultat != "" {
				err = errors.New(err.Error() + ": " + couper(resultat, 200))
			}
		}
		if err != nil && (m.Etat == "preparation" || m.Etat == "proposee") { // a cancelled or launched mission keeps its state
			m.Etat = "echec"
			m.Historique = append(m.Historique, EvenementMission{T: time.Now().UTC().Format(formatCree), Etat: "echec",
				Detail: couper(s.masquerSecrets(err.Error()), 300)})
		}
		return nil
	})
	issue := "ok"
	if err != nil {
		issue = couper(s.masquerSecrets(err.Error()), 300)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "missions", Projet: prefixeMission + id, Mission: id, Agent: agentOrchestrateur,
		Action: "orchestrateur terminé", Resultat: issue})
}
