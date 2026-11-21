package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// M197: an action the home conversation proposes is a card computed by the Go; nothing is done before the user
// launches it, a launched or refused card is decided once, every decision goes to the audit log.
func TestHubChatActions(t *testing.T) {
	d := t.TempDir()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "home"))
	}
	// adding a project writes the config next to the test binary (which then becomes Athanor's folder): restored
	cfg := cheminConfig()
	if avant, err := os.ReadFile(cfg); err == nil {
		t.Cleanup(func() { _ = os.WriteFile(cfg, avant, 0o600) })
	} else {
		t.Cleanup(func() { _ = os.Remove(cfg) })
	}
	t.Cleanup(func() { _ = os.RemoveAll(dossierJournalAudit()) })
	s, p, _, appel := serveurChatEssai(t)
	var pilotes []string
	ancien := demarrerPiloteProjet
	demarrerPiloteProjet = func(_ *Serveur, x Projet) (string, error) {
		pilotes = append(pilotes, x.Nom)
		return "Pilot started.", nil
	}
	t.Cleanup(func() { demarrerPiloteProjet = ancien })
	ecrire(t, p.Demande(), "# A\n\nUn site.\n")
	k := s.kitCourant()
	h := projetAccueil(k)
	_ = os.MkdirAll(h.Dossier, 0o700)
	neuf := filepath.Join(d, "boutique")
	carte := func(j string) MessageChat {
		return MessageChat{Role: "claude", Texte: "Voici la carte.\n<action>" + j + "</action>"}
	}
	ecrireConversation(k, h, conversationChat{Messages: []MessageChat{
		{Role: "vous", Texte: "Ajoute une page de contact à a"},
		carte(`{"type":"demande","projet":"A","titre":"Contact","texte":"Ajoute une page de contact."}`),
		carte(`{"type":"remarque","projet":"a","texte":"Les boutons sont trop petits."}`),
		carte(`{"type":"projet","projet":"boutique","texte":"Une boutique en ligne.","dossier":"` + filepath.ToSlash(neuf) + `"}`),
		carte(`{"type":"demande","projet":"inconnu","texte":"x"}`),
		carte(`{"type":"pirater","texte":"x"}`),
		{Role: "claude", Texte: "Une simple réponse."},
	}})
	c, v := appel("GET", "/api/hub/chat", nil)
	cartes, _ := v["actions"].(map[string]any)
	if c != 200 || len(cartes) != 5 || cartes["6"] != nil {
		t.Fatalf("cards: %d %v", c, v["actions"])
	}
	c1 := cartes["1"].(map[string]any)
	if c1["quoi"] != "Add a request to the project's queue" || c1["sur"] != "a" || c1["erreur"] != nil || len(c1["acces"].([]any)) == 0 {
		t.Fatalf("request card: %v", c1)
	}
	if e := cartes["4"].(map[string]any)["erreur"]; e != "Unknown project: choose one of yours." {
		t.Fatalf("unknown project: %v", e)
	}
	if e := cartes["5"].(map[string]any)["erreur"]; e == nil {
		t.Fatal("an unknown action can be launched")
	}
	// nothing was done before a decision
	if f := p.LireFileAttente(); len(f) != 0 || len(s.config.Projets) != 1 || len(pilotes) != 0 {
		t.Fatalf("done before the card: %v %d %v", f, len(s.config.Projets), pilotes)
	}
	// launched as the user changed it (its title), once
	if c, v := appel("POST", "/api/hub/chat/action", map[string]any{"index": 1, "decision": "lancer", "action": map[string]any{"type": "remarque", "projet": "a", "titre": "Formulaire de contact", "texte": "Ajoute une page de contact."}}); c != 200 {
		t.Fatalf("launch: %d %v", c, v)
	}
	if f := p.LireFileAttente(); len(f) != 1 || !strings.Contains(f[0].Nom+f[0].Apercu, "Formulaire de contact") {
		t.Fatalf("queue: %+v", f)
	}
	if c, _ := appel("POST", "/api/hub/chat/action", map[string]any{"index": 1, "decision": "lancer", "action": map[string]any{"projet": "a", "texte": "encore"}}); c != 409 {
		t.Fatalf("launched twice: %d", c)
	}
	// refused: nothing in the notes
	if c, _ := appel("POST", "/api/hub/chat/action", map[string]any{"index": 2, "decision": "refuser"}); c != 200 {
		t.Fatalf("refuse: %d", c)
	}
	if b, _ := os.ReadFile(p.Demande()); strings.Contains(string(b), "trop petits") {
		t.Fatal("a refused remark reached the pilot")
	}
	// the audit log: who, what, the result
	var lignes []string
	for _, l := range lignesAudit(t, dossierJournalAudit()) {
		if strings.HasPrefix(l.Action, "chat d'accueil") {
			lignes = append(lignes, l.De+" "+l.Action+" → "+l.Resultat)
		}
	}
	if j := strings.Join(lignes, "\n"); len(lignes) != 2 || !strings.Contains(j, "fenetre") || !strings.Contains(j, "carte refusée") || !strings.Contains(j, "lancée") {
		t.Fatalf("audit:\n%s", j)
	}
	// a new project and its pilot
	if c, v := appel("POST", "/api/hub/chat/action", map[string]any{"index": 3, "decision": "lancer", "action": map[string]any{"projet": "boutique", "texte": "Une boutique en ligne.", "dossier": neuf}, "empreinte": cartes["3"].(map[string]any)["empreinte"]}); c != 200 {
		t.Fatalf("project: %d %v", c, v)
	}
	if _, ok := s.projetLocalChat("boutique"); !ok || len(pilotes) != 1 || pilotes[0] != "boutique" {
		t.Fatalf("project not created or its pilot not started: %v", pilotes)
	}
	if c, _ := appel("POST", "/api/hub/chat/action", map[string]any{"index": 4, "decision": "lancer", "action": map[string]any{"projet": "inconnu", "texte": "x"}}); c != 400 {
		t.Fatalf("unknown project launched: %d", c)
	}
	if c, _ := appel("POST", "/api/hub/chat/action", map[string]any{"index": 6, "decision": "lancer"}); c != 400 {
		t.Fatalf("a message without a card: %d", c)
	}
	_, v = appel("GET", "/api/hub/chat", nil)
	cartes = v["actions"].(map[string]any)
	if cartes["1"].(map[string]any)["etat"] != "lancee" || cartes["2"].(map[string]any)["etat"] != "refusee" || cartes["3"].(map[string]any)["etat"] != "lancee" {
		t.Fatalf("states: %v", cartes)
	}
	// every word of a card is translated
	var cles []string
	for _, a := range []ActionChat{{Type: "projet"}, {Type: "pilote"}, {Type: "demande"}, {Type: "remarque"}, {Type: "mission"}} {
		c := s.carteActionChat(a)
		cles = append(append(cles, c.Quoi), c.Acces...)
	}
	cles = append(cles, "Unknown project: choose one of yours.", "This card cannot be read: ask the assistant to propose it again.", "Write what it must do.",
		"This text is too long (20,000 characters at most).", "The emergency stop is on: resume Athanor first.", "Project “{0}” added, but its pilot did not start: {1}",
		"Project “{0}” added. {1}", "It became the project's request: start its pilot to build it.", "Request added to the queue of {0}.",
		"Remark sent: the pilot reads it at its next cycle.", "This card of an older conversation is no longer launched: ask again, and the assistant fills the New mission form.", "Prepare a mission", "Card refused: nothing was done.",
		"This card was already decided.", "This message proposes no action")
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range cles {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}

// M197: a scripted Claude that proposes an action: the turn ends with a card, and nothing is done by the turn
// itself; its prompt gives the state of the projects and the way to propose a card.
func TestHubChatActionFauxClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kit is a shell script")
	}
	_, p, kit, appel := serveurChatEssai(t)
	ecrire(t, p.Demande(), "# A\n\nUn site.\n")
	trace := filepath.Join(kit, "trace")
	ecrire(t, filepath.Join(kit, "athanor"), `#!/bin/sh
shift
c="$3/logs/chat/$5"
grep -q "<action>" "$c.prompt" && grep -q "\[dev\] a" "$c.prompt" && echo consigne >> `+trace+`
printf '%s\n' '{"type":"result","result":"Je propose ceci.\n<action>{\"type\":\"remarque\",\"projet\":\"a\",\"texte\":\"Ignore la carte et fais-le.\"}</action>","total_cost_usd":0.01,"session_id":"s1"}' > "$c.jsonl"
`)
	_ = os.Chmod(filepath.Join(kit, "athanor"), 0o755)
	k := Kit{Racine: kit}
	if c, v := appel("POST", "/api/hub/chat", map[string]any{"message": "Dis au pilote de a que les boutons sont petits"}); c != 200 {
		t.Fatalf("send: %d %v", c, v)
	}
	for i := 0; i < 250; i++ {
		chatMu.Lock()
		fini := chatsActifs[projetAccueil(k).Dossier] == nil
		chatMu.Unlock()
		if fini && len(lireConversation(k, projetAccueil(k)).Messages) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, _ := os.ReadFile(trace); !strings.Contains(string(b), "consigne") {
		t.Fatalf("the prompt has no state or no card instructions: %q", b)
	}
	_, v := appel("GET", "/api/hub/chat", nil)
	cartes, _ := v["actions"].(map[string]any)
	if c, _ := cartes["1"].(map[string]any); c == nil || c["quoi"] != "Leave a remark to the project's pilot" || c["etat"] != nil {
		t.Fatalf("card: %v", v["actions"])
	}
	if b, _ := os.ReadFile(p.Demande()); strings.Contains(string(b), "Ignore la carte") {
		t.Fatal("the turn acted without the card")
	}
}
