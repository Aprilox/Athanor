package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestOrchestrateurSession (M153): the orchestrator's session with a scripted fake Claude: an empty working folder,
// a token valid only while it runs (agent orchestrateur, its tools only), the instructions with the sentence and the
// resolved mentions, the resume with a text first, the folder removed and the token revoked at the end; a failed,
// overlong or cardless session puts the mission in echec with a readable reason; no vault value anywhere.
func TestOrchestrateurSession(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s) // an infra entry needs Athanor's password
	const temoin = "TEMOIN-orchestrateur-7c1e"
	cle, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATHANOR_DIVERS", temoin) // a vault value in Athanor's environment under an innocent name
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "srv1", Nom: "web-1", Hote: "203.0.113.5"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienDelai := delaiOrchestrateur
	t.Cleanup(func() { delaiOrchestrateur = ancienDelai })

	type vue struct{ consigne, reprise string }
	var vues []vue
	var jouer func(ctx context.Context, dossier string) (string, string, bool) // session id, result, is_error
	ancien := sessionOrchestrateur
	t.Cleanup(func() { sessionOrchestrateur = ancien })
	sessionOrchestrateur = func(_ *Serveur, ctx context.Context, dossier string) error {
		lire := func(f string) string {
			b, _ := os.ReadFile(filepath.Join(dossier, f))
			return strings.TrimSpace(string(b))
		}
		_ = filepath.Walk(dossier, func(p string, i os.FileInfo, _ error) error {
			if b, _ := os.ReadFile(p); i != nil && !i.IsDir() && strings.Contains(string(b), temoin) {
				t.Errorf("vault value in the session folder: %s", p)
			}
			return nil
		})
		if e, err := os.ReadDir(filepath.Join(dossier, "travail")); err != nil || len(e) != 0 {
			t.Errorf("the working folder is missing or not empty: %v %v", e, err)
		}
		x, ok := s.verifierJeton(lire("hub-jeton"))
		if !ok || x.Agent != agentOrchestrateur || !slices.Equal(x.Outils, outilsOrchestrateur) || !strings.HasPrefix(x.Projet, prefixeMission) {
			t.Errorf("the session's token: %+v %v", x, ok)
		}
		vues = append(vues, vue{lire("consigne.txt"), lire("reprise")})
		id, res, enErreur := jouer(ctx, dossier)
		r, _ := json.Marshal(map[string]any{"type": "result", "is_error": enErreur, "result": res})
		j := ""
		if id != "" {
			j = `{"type":"system","subtype":"init","session_id":"` + id + `"}` + "\n"
		}
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j+string(r)+"\n"), 0o600)
	}
	nouvelle := func(id string) {
		t.Helper()
		m := Mission{ID: id, Phrase: "Mettre la boutique en ligne sur web-1 avec " + temoin, Etat: "preparation", Politique: "prod",
			Mentions: MentionsMission{Projets: []string{"boutique"}, Serveurs: []string{"srv1"}, Cles: []string{cle.ID}},
			Cree:     time.Now().UTC().Format(formatCree)}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
	}
	finie := func(id string) Mission {
		t.Helper()
		for range 100 {
			orchestrateursEnCours.Lock()
			en := orchestrateursEnCours.m[id] != nil
			orchestrateursEnCours.Unlock()
			if _, err := os.Stat(dossierSessionMission(id)); !en && os.IsNotExist(err) {
				m, _ := lireMission(id)
				return m
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("the orchestrator did not end")
		return Mission{}
	}
	jetonsOrchestrateur := func() int {
		return len(slices.DeleteFunc(s.jetons.Liste(), func(x SessionHub) bool { return x.Agent != agentOrchestrateur }))
	}

	// 1. the first session asks a question (as question_proprietaire will, M154): the mission waits, its session kept
	nouvelle("mis001")
	jouer = func(context.Context, string) (string, string, bool) {
		_, _ = modifierMission("mis001", func(m *Mission) error {
			m.Questions = append(m.Questions, QuestionMission{ID: "q1", Texte: "Quel nom de domaine ?", Choix: []string{"a", "b"}})
			return nil
		})
		return "sess-1", "I asked the owner.", false
	}
	if err := s.lancerOrchestrateur("mis001", ""); err != nil {
		t.Fatal(err)
	}
	m := finie("mis001")
	if m.Etat != "preparation" || m.Session != "sess-1" || jetonsOrchestrateur() != 0 {
		t.Fatalf("after a question: %+v, %d orchestrator tokens", m, jetonsOrchestrateur())
	}
	c := vues[0].consigne
	for _, attendu := range []string{"orchestrator", "build, deploy and change nothing yourself", "proposer_mission", "question_proprietaire",
		"<owner-request>\nMettre la boutique en ligne sur web-1", "project boutique", "server web-1 (id srv1, 203.0.113.5)", "vault entry Jeton Cloudflare (cloudflare)"} {
		if !strings.Contains(c, attendu) {
			t.Errorf("instructions without %q:\n%s", attendu, c)
		}
	}
	if vues[0].reprise != "" {
		t.Errorf("first session resumed %q", vues[0].reprise)
	}

	// 2. the answer resumes the same Claude session with the text first; the card proposed (proposer_mission, M154)
	jouer = func(context.Context, string) (string, string, bool) {
		_, _ = modifierMission("mis001", func(m *Mission) error { m.Etat = "proposee"; return nil })
		return "sess-1", "Card proposed.", false
	}
	if err := s.lancerOrchestrateur("mis001", "The owner answered: <b>"); err != nil {
		t.Fatal(err)
	}
	if m = finie("mis001"); m.Etat != "proposee" || vues[1].reprise != "sess-1" || !strings.HasPrefix(vues[1].consigne, "<owner-message>\nThe owner answered: &lt;b>") {
		t.Fatalf("resume: %+v %+v", m, vues[1])
	}

	// 3. an overlong session (a change asked, M155): stopped, the mission in echec with a readable reason; one at a time
	delaiOrchestrateur = 300 * time.Millisecond
	jouer = func(ctx context.Context, _ string) (string, string, bool) {
		<-ctx.Done()
		return "sess-1", "", false
	}
	if err := s.lancerOrchestrateur("mis001", "Put the shop on staging too."); err != nil {
		t.Fatal(err)
	}
	if err := s.lancerOrchestrateur("mis001", "again"); err != errOrchestrateurOccupe {
		t.Errorf("second launch while running: %v", err)
	}
	if m = finie("mis001"); m.Etat != "echec" || !strings.Contains(m.Historique[len(m.Historique)-1].Detail, "more than 10 minutes") {
		t.Fatalf("overlong session: %+v", m)
	}
	delaiOrchestrateur = ancienDelai

	// 3b. B85, C85: a session in error after a valid card: the card stays proposee and launchable, the error in its
	// history, its launch request still pending; before any card: echec, and its pending launch request refused
	for _, cas := range []struct {
		id, etat string
		carte    json.RawMessage
	}{{"mis005", "proposee", json.RawMessage(`{"titre":"Boutique"}`)}, {"mis006", "preparation", nil}} {
		nouvelle(cas.id)
		_, _ = modifierMission(cas.id, func(m *Mission) error { m.Etat, m.Carte = cas.etat, cas.carte; return nil })
		if _, err := s.demanderApprobation(Approbation{Projet: prefixeMission + cas.id, Mission: cas.id, Agent: agentOrchestrateur, Action: actionMission}); err != nil {
			t.Fatal(err)
		}
		jouer = func(context.Context, string) (string, string, bool) { return "sess-9", "exit status 1", true }
		if err := s.lancerOrchestrateur(cas.id, "A change."); err != nil {
			t.Fatal(err)
		}
		m := finie(cas.id)
		h := m.Historique[len(m.Historique)-1]
		_, attente := s.accordMissionAttente(cas.id)
		if cas.carte != nil && (m.Etat != "proposee" || h.Etat != "proposee" || !strings.HasPrefix(h.Detail, "error: ") || !attente) {
			t.Errorf("error after a card: %s %+v, request pending %v", m.Etat, h, attente)
		}
		if cas.carte == nil && (m.Etat != "echec" || attente) {
			t.Errorf("error before a card: %s, request pending %v", m.Etat, attente)
		}
	}

	// 4. a session in error, one that never started, one that ends without a card: echec, each with its reason
	for i, cas := range []struct {
		id, res  string
		enErreur bool
		raison   string
	}{
		{"sess-2", "API overloaded " + temoin, true, "ended with an error: API overloaded"},
		{"", "", false, "did not start"},
		{"sess-3", "Nothing to do.", false, "without proposing a card: Nothing to do."},
	} {
		id := []string{"mis002", "mis003", "mis004"}[i]
		nouvelle(id)
		jouer = func(context.Context, string) (string, string, bool) { return cas.id, cas.res, cas.enErreur }
		if err := s.lancerOrchestrateur(id, ""); err != nil {
			t.Fatal(err)
		}
		m := finie(id)
		if d := m.Historique[len(m.Historique)-1].Detail; m.Etat != "echec" || !strings.Contains(d, cas.raison) || strings.Contains(d, temoin) {
			t.Errorf("%s: %s %q", id, m.Etat, d)
		}
	}
	if err := s.lancerOrchestrateur("inconnue1", ""); err != errMissionInconnue {
		t.Errorf("unknown mission: %v", err)
	}

	// the session's command and arguments: the kit's session-orchestrateur, no secret in its environment, no
	// built-in tool, the hub's MCP server only; nothing of the vault in the audit log
	cmd := commandeSessionKit(s, "session-orchestrateur", dossierSessionMission("mis001"))
	if env := strings.Join(cmd.Env, "\n"); strings.Contains(env, temoin) || !strings.Contains(strings.Join(cmd.Args, " "), "session-orchestrateur") {
		t.Errorf("session command: %v", cmd.Args)
	}
	a := strings.Join(argsSessionServeurs("c", "m.json", "sess-1"), "\x00")
	if !strings.Contains(a, "--tools\x00\x00") || !strings.Contains(a, "--strict-mcp-config") || !strings.Contains(a, "--allowedTools\x00mcp__"+nomMCPHub+"\x00") ||
		!strings.Contains(a, "--resume\x00sess-1") {
		t.Errorf("arguments: %q", a)
	}
	lignes := lignesAudit(t, dossierJournalAudit())
	if b, _ := json.Marshal(lignes); strings.Contains(string(b), temoin) || !strings.Contains(string(b), "orchestrateur terminé") {
		t.Errorf("audit log: %s", b)
	}
}
