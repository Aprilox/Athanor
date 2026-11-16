package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionTelephone (M158): the orchestrator's lancer_mission is an approval of the Missions module in the bell
// (Accept and Refuse only) and a Web Push; a paired phone, unlocked, accepts it and the mission is launched as by
// Launch (never with a rule); refused, nothing is launched; expired, accepting it launches nothing; Launch on the PC
// closes a pending request; an orchestrator's question is a bell alert with its choices, answered from the phone,
// which resumes the session with the answer.
func TestMissionTelephone(t *testing.T) {
	s := serveurCoffreTest(t)
	boutique := Projet{Nom: "boutique", Dossier: filepath.Join(t.TempDir(), "boutique")}
	ecrireFichier(t, boutique.Demande(), "# Boutique\n", 0o644)
	s.config.Projets, s.config.Langue = []Projet{boutique}, "fr"
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })

	// the tunnel and a paired phone with notifications on (as TestApprobationTelephone)
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	s.mux = mux
	s.config.Distance.Actif = true
	s.dist().tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
	h := s.gestionDistance(mux)
	cle := appairerTest(t, s, h)
	api := []string{"X-Athanor", "jeton"}
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	s.mu.Lock()
	s.config.Distance.Appareils[0].Push = &AbonnementPush{Endpoint: "https://fcm.googleapis.com/fcm/send/tel",
		P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
	idTel := s.config.Distance.Appareils[0].ID
	s.mu.Unlock()
	var mu sync.Mutex
	var recus []MessagePush
	var pilotes, reprises []string
	ancienClient, ancienPilote, ancienneSession := clientPush, demarrerPiloteMission, sessionOrchestrateur
	t.Cleanup(func() {
		clientPush, demarrerPiloteMission, sessionOrchestrateur = ancienClient, ancienPilote, ancienneSession
	})
	clientPush = &http.Client{Transport: allerRetour(func(r *http.Request) (*http.Response, error) {
		var m MessagePush
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(dechiffrerPushEssai(t, ua, auth, b), &m)
		mu.Lock()
		recus = append(recus, m)
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody, Request: r}, nil
	})}
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		pilotes = append(pilotes, p.Nom)
		return "Pilot started", nil
	}
	sessionOrchestrateur = func(_ *Serveur, _ context.Context, dossier string) error { // the fake Claude: reads, ends
		b, _ := os.ReadFile(filepath.Join(dossier, "consigne.txt"))
		mu.Lock()
		reprises = append(reprises, string(b))
		mu.Unlock()
		j := `{"type":"system","subtype":"init","session_id":"sess-orch"}` + "\n" + `{"type":"result","is_error":false,"result":"ok"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	recu := func(tag string) (MessagePush, bool) {
		for range 200 {
			mu.Lock()
			i := slices.IndexFunc(recus, func(m MessagePush) bool { return m.Tag == tag })
			var m MessagePush
			if i >= 0 {
				m = recus[i]
			}
			mu.Unlock()
			if i >= 0 {
				return m, true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return MessagePush{}, false
	}
	proposer := func(titre string) string {
		t.Helper()
		c := CarteMission{Titre: titre, Projet: ProjetCarte{Nom: "boutique"}, Dev: BriefCarte{Brief: "Ajouter " + titre}, JalonsEstimes: 1}
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(calculerCarte(c, "prod", x))
		m := Mission{ID: aleatoire(8), Phrase: titre, Etat: "proposee", Politique: "prod", Carte: b, Voies: []VoieMission{},
			Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}
		if err := enregistrerMission(m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	demander := func(id string) Approbation { // the orchestrator calls lancer_mission
		t.Helper()
		if _, err := outilLancerMission(s, SessionHub{Mission: id, Agent: agentOrchestrateur}, json.RawMessage(`{"id":"`+id+`"}`)); err != nil {
			t.Fatal(err)
		}
		l, _ := s.approbations.Liste()
		i := slices.IndexFunc(l, func(x Approbation) bool { return x.Mission == id && x.Etat == "attente" })
		if i < 0 {
			t.Fatal("no pending launch request")
		}
		return l[i]
	}
	decider := func(id, decision string) int {
		return appelDistance(h, "POST", "/api/hub/approbations", cle, `{"id":"`+id+`","decision":"`+decision+`"}`, api...).Code
	}
	alerte := func(empreinte string) (AlerteHub, bool) {
		actives, _ := s.alertesHub(nil)
		i := slices.IndexFunc(actives, func(a AlerteHub) bool { return a.Empreinte == empreinte })
		if i < 0 {
			return AlerteHub{}, false
		}
		return actives[i], true
	}

	// 1. the request: in the bell (Missions, Accept and Refuse from a device), a Web Push in the owner's language
	paiement := proposer("Page de paiement")
	d := demander(paiement)
	a, ok := alerte("appr-" + d.ID)
	if !ok || a.Module != "missions" || a.Titre != "Page de paiement" || a.Raison != "Launch this mission? Environments: {0}. Access: {1}" ||
		len(a.Args) != 2 || a.Args[0] != "—" || a.Args[1] != "—" || len(a.Actions) != 2 ||
		a.Actions[0].ID != "accepter" || a.Actions[1].ID != "refuser" || !a.Actions[0].Distance {
		t.Fatalf("bell: %+v", a)
	}
	if p, _ := lireMission(paiement); d.Details["empreinte"] != empreinteCarte(p) || d.Details["envs"] != "—" || d.Details["acces"] != "—" || d.Details["prod"] != "" {
		t.Fatalf("the request's details: %v", d.Details)
	}
	if m, ok := recu("appr-" + d.ID); !ok || m.Titre != "Lancer la mission Page de paiement ?" || m.URL != "/#missions/"+paiement ||
		!strings.HasPrefix(m.Texte, "Environnements : —. Accès : —.") {
		t.Fatalf("push: %+v", m)
	}
	// locked phone: nothing decided; unlocked: never with a rule, accepted → launched as by Launch
	if c := decider(d.ID, "accepter"); c == 200 || s.approbations.PourAgent(d.ID) != "attente" {
		t.Fatalf("locked phone: %d", c)
	}
	avecMdpPreuve(s)
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"`+mdpPreuveTest+`"}`, api...); w.Code != 200 {
		t.Fatalf("phone unlocks: %d %s", w.Code, w.Body)
	}
	if c := decider(d.ID, "toujours"); c != http.StatusConflict {
		t.Fatalf("a rule for a mission: %d", c)
	}
	if c := decider(d.ID, "accepter"); c != 200 {
		t.Fatalf("phone accepts: %d", c)
	}
	m, _ := lireMission(paiement)
	x, _ := s.approbations.Lire(d.ID)
	if m.Etat != "lancee" || len(m.Voies) != 1 || m.Voies[0] != (VoieMission{Module: "dev", Ref: "boutique", Etat: "lancee"}) || !slices.Equal(pilotes, []string{"boutique"}) ||
		x.Etat != "acceptee" || x.Par != "appareil:"+idTel || !slices.ContainsFunc(m.Historique, func(e EvenementMission) bool { return e.Detail == "appareil:"+idTel }) { // M254: the launch's steps follow
		t.Fatalf("launched from the phone: %+v, %+v, %v", m, x, pilotes)
	}
	if _, ok := alerte("appr-" + d.ID); ok {
		t.Error("a decided request stays in the bell")
	}

	// 2. refused: nothing launched, the card stays to launch
	livraison := proposer("Frais de livraison")
	d = demander(livraison)
	if c := decider(d.ID, "refuser"); c != 200 {
		t.Fatalf("phone refuses: %d", c)
	}
	if m, _ := lireMission(livraison); m.Etat != "proposee" || len(pilotes) != 1 {
		t.Fatalf("refused, yet launched: %s %v", m.Etat, pilotes)
	}

	// C77: while a request waits, neither the agent nor the owner changes the card; changed anyway (an older
	// request, a change racing the request) → accepting refuses the request, nothing is launched
	d = demander(livraison)
	if _, err := outilProposerMission(s, SessionHub{Mission: livraison, Agent: agentOrchestrateur}, json.RawMessage(`{"carte":{"titre":"Autre"}}`)); err == nil ||
		!strings.Contains(err.Error(), "launch request") {
		t.Fatalf("proposer_mission while the request waits: %v", err)
	}
	if w := appelDistance(h, "POST", "/api/missions/"+livraison+"/modifier", cle, `{"politique":"tout"}`, api...); w.Code != http.StatusConflict {
		t.Fatalf("modifier while the request waits: %d %s", w.Code, w.Body)
	}
	politique := func(p string) {
		t.Helper()
		if _, err := modifierMission(livraison, func(m *Mission) error { m.Politique = p; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	politique("tout")
	if c := decider(d.ID, "accepter"); c != http.StatusConflict {
		t.Fatalf("a changed card accepted: %d", c)
	}
	if x, _ := s.approbations.Lire(d.ID); x.Etat != "refusee" {
		t.Fatalf("the request of a changed card: %+v", x)
	}
	if m, _ := lireMission(livraison); m.Etat != "proposee" || len(pilotes) != 1 {
		t.Fatalf("changed card, yet launched: %s %v", m.Etat, pilotes)
	}
	politique("prod")
	// C79: a change between Launch's checks and the launch (Modify on another device) → 409, nothing launched
	apresControleLancement = func(string) { politique("tout") }
	_, err := s.lancerMission(livraison, "fenetre", "", "")
	apresControleLancement = func(string) {}
	if err != errCarteChangee {
		t.Fatalf("launch racing a change: %v", err)
	}
	if m, _ := lireMission(livraison); m.Etat != "proposee" || len(pilotes) != 1 {
		t.Fatalf("raced, yet launched: %s %v", m.Etat, pilotes)
	}
	politique("prod")

	// 3. Launch on the PC closes the pending request (accepted by the window); 4. expired: accepting launches nothing
	d = demander(livraison)
	if _, err := s.lancerMission(livraison, "fenetre", "", ""); err != nil {
		t.Fatal(err)
	}
	s.fermerLancementMission(livraison, "fenetre", "accepter", "mission lancée") // what apiLancerMission does after it
	if x, _ := s.approbations.Lire(d.ID); x.Etat != "acceptee" || x.Par != "fenetre" {
		t.Fatalf("request left after Launch: %+v", x)
	}
	avis := proposer("Avis clients")
	d = demander(avis)
	s.approbations.Maintenant = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if c := decider(d.ID, "accepter"); c != http.StatusConflict {
		t.Fatalf("expired accepted: %d", c)
	}
	s.approbations.Maintenant = nil
	if m, _ := lireMission(avis); m.Etat != "proposee" || len(pilotes) != 2 {
		t.Fatalf("expired, yet launched: %s %v", m.Etat, pilotes)
	}

	// 5. a question with choices: in the bell (each choice a button allowed on a device), a push, answered from the
	// phone → the orchestrator's session resumed with the answer, the alert gone
	if _, err := outilQuestionProprietaire(s, SessionHub{Mission: avis, Agent: agentOrchestrateur},
		json.RawMessage(`{"texte":"Quel domaine ?","choix":["avis.fr","<b>autre</b>"]}`)); err != nil {
		t.Fatal(err)
	}
	m, _ = lireMission(avis)
	q := m.Questions[0]
	var aq AlerteHub
	actives, _ := s.alertesHub((moduleMissions{}).Elements(s))
	for _, x := range actives {
		if x.Element == avis && x.Raison == "The orchestrator asks: {0}" {
			aq = x
		}
	}
	if len(aq.Actions) != 4 || aq.Actions[0].Libelle != "avis.fr" || aq.Actions[1].Libelle != "<b>autre</b>" || !aq.Actions[0].Distance ||
		aq.Actions[0].Route != "missions/"+avis+"/repondre" || aq.Actions[0].Corps["question"] != q.ID || aq.Actions[2].ID != "voir" {
		t.Fatalf("question in the bell: %+v", aq)
	}
	if _, ok := recu("question-" + q.ID); !ok {
		t.Error("no push for the question")
	}
	corps, _ := json.Marshal(aq.Actions[0].Corps)
	if w := appelDistance(h, "POST", "/api/"+aq.Actions[0].Route, cle, string(corps), api...); w.Code != 200 {
		t.Fatalf("phone answers: %d %s", w.Code, w.Body)
	}
	for i := 0; i < 200 && orchestrateurTravaille(avis); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	m, _ = lireMission(avis)
	mu.Lock()
	derniere := reprises[len(reprises)-1]
	mu.Unlock()
	if m.Questions[0].Reponse != "avis.fr" || !strings.Contains(derniere, "avis.fr") {
		t.Fatalf("answer %q, resumed with %q", m.Questions[0].Reponse, derniere)
	}
	if el := (moduleMissions{}).Elements(s); slices.ContainsFunc(el, func(e ElementHub) bool { return e.Attente != nil && e.ID == avis }) {
		t.Error("an answered question stays in the bell")
	}

	// 6. B84: a paired device never changes the card's environments nor its project (403, card intact); C78: it launches
	// without the password (unlocked with it)
	tel := proposer("Depuis le téléphone")
	avant, _ := lireMission(tel)
	for _, corps := range []string{`{"envs":[{"nom":"prod","url":"https://pirate.example.org","protection":"aucune"}]}`, `{"projet":{"nom":"boutique"}}`} {
		if w := appelDistance(h, "POST", "/api/missions/"+tel+"/modifier", cle, corps, api...); w.Code != http.StatusForbidden {
			t.Fatalf("envs from the phone: %d %s", w.Code, w.Body)
		}
	}
	if m, _ := lireMission(tel); string(m.Carte) != string(avant.Carte) {
		t.Fatalf("card changed from the phone: %s", m.Carte)
	}
	if w := appelDistance(h, "POST", "/api/missions/"+tel+"/lancer", cle, `{"empreinte":"`+empreinteCarte(avant)+`"}`, api...); w.Code != 200 {
		t.Fatalf("launch from the phone: %d %s", w.Code, w.Body)
	}
}
