package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestOrchestrateurPerimetre (M154): a scripted fake Claude calls the orchestrator's real tools through the real
// /api/mcp route with its session's token: every other tool of the hub (Dev, Servers, Cloudflare, deployments) is
// unknown to it, demander_module outside the white list is refused, a question is one at a time and in the bell,
// the card is checked and computed by the Go, lancer_mission asks the owner once; no vault value in the answers,
// the mission or the audit log, and every call is audited.
func TestOrchestrateurPerimetre(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	const temoin = "TEMOIN-perimetre-91ab"
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, temoin); err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"prod"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	dossierProjet := filepath.Join(t.TempDir(), "boutique")
	s.config.Projets = append(s.config.Projets, Projet{Nom: "boutique", Dossier: dossierProjet})
	mux := http.NewServeMux()
	s.routes(mux)
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	for _, n := range []string{"etat", "demander_deploiement", "ssh_commande", "dns_ecrire", "deployer"} { // the scopes the loop below covers
		if _, ok := outilsHub[n]; !ok {
			t.Fatalf("tool %s is not registered: the test would not cover its scope", n)
		}
	}
	const id = "mission154a"
	if err := enregistrerMission(Mission{ID: id, Phrase: "Mettre la boutique en ligne sur web-1", Etat: "preparation", Politique: "prod",
		Cree: time.Now().UTC().Format(formatCree)}); err != nil {
		t.Fatal(err)
	}
	carte := `{"titre":"Boutique en ligne","projet":{"nom":"boutique"},"dev":{"brief":"Finir la boutique"},` +
		`"envs":[{"nom":"prod","url":"https://boutique.example.org","protection":"aucune","serveurs":["a1"]}],` +
		`"serveurs":{"brief":"DNS, certificat, proxy"},"acces":[{"entree":"Jeton Cloudflare","pour":"a1"}],"jalons_estimes":4,` +
		`"approbations":[],"estimation":{"jalons":99}}`

	var reponses []string
	ancien := sessionOrchestrateur
	t.Cleanup(func() { sessionOrchestrateur = ancien })
	sessionOrchestrateur = func(_ *Serveur, _ context.Context, dossier string) error {
		jeton, _ := os.ReadFile(filepath.Join(dossier, "hub-jeton"))
		b := "Bearer " + strings.TrimSpace(string(jeton))
		appel := func(nom, args string) (string, bool) {
			rep := appelMCP(t, mux, b, appelOutil(nom, args))
			if rep.Error != nil {
				return rep.Error.Message, true
			}
			reponses = append(reponses, rep.Result.Content[0].Text)
			return rep.Result.Content[0].Text, rep.Result.IsError
		}
		attendu := func(nom, args, contient string, erreur bool) string {
			txt, e := appel(nom, args)
			if e != erreur || !strings.Contains(txt, contient) {
				t.Errorf("%s %s: error %v, %q (expected error %v with %q)", nom, args, e, txt, erreur, contient)
			}
			return txt
		}
		var noms []string
		for _, o := range appelMCP(t, mux, b, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`).Result.Tools {
			noms = append(noms, o.Name)
		}
		if !slices.Equal(noms, outilsOrchestrateur) {
			t.Errorf("tools/list: %v", noms)
		}
		for nom := range outilsHub { // Dev, Servers, Cloudflare, deployments: unknown to this token
			if !slices.Contains(outilsOrchestrateur, nom) {
				attendu(nom, "{}", "unknown tool", true)
			}
		}

		etat := attendu("modules_etat", "{}", `"web-1"`, false)
		for _, v := range []string{`"boutique"`, `"Jeton Cloudflare"`, `"durci": false`, `"missions"`} {
			if !strings.Contains(etat, v) {
				t.Errorf("modules_etat without %s: %s", v, etat)
			}
		}
		for _, v := range []string{dossierProjet, strings.ReplaceAll(dossierProjet, `\`, `\\`), strings.ReplaceAll(dossierAthanor(), `\`, `\\`)} {
			if strings.Contains(etat, v) {
				t.Errorf("modules_etat names a path %s", v)
			}
		}
		attendu("modules_etat", `{"projet":"boutique"}`, "invalid arguments", true)

		attendu("demander_module", `{"module":"dev","action":"etat_projet","donnees":{"projet":"boutique"}}`, `"boutique"`, false)
		attendu("demander_module", `{"module":"dev","action":"supprimer","donnees":{"projet":"boutique"}}`, "not allowed to the orchestrator", true)
		attendu("demander_module", `{"module":"serveurs","action":"lancer_tache","donnees":{"projet":"boutique"}}`, "not allowed to the orchestrator", true)
		attendu("demander_module", `{"module":"dev","action":"etat_projet","donnees":{"projet":"inconnu"}}`, "unknown project", true)

		attendu("question_proprietaire", `{"texte":"Quel nom ?","choix":["a","b","c","d","e"]}`, "4 choices at most", true)
		attendu("question_proprietaire", `{"texte":"  "}`, "texte is required", true)
		attendu("question_proprietaire", `{"texte":"Quel domaine ?","choix":["boutique.example.org","`+temoin+`"]}`, "stop now", false)
		attendu("question_proprietaire", `{"texte":"Encore ?"}`, "waits for the owner's answer", true)
		attendu("proposer_mission", `{"carte":`+carte+`}`, "waits for the owner's answer", true)
		el := moduleMissions{}.Elements(s)
		if len(el) != 1 || el[0].ID != id || el[0].Attente == nil || el[0].Attente.Args[0] != "Quel domaine ?" {
			t.Errorf("the question in the bell: %+v", el)
		}
		if _, err := modifierMission(id, func(m *Mission) error { m.Questions[0].Reponse = "boutique.example.org"; return nil }); err != nil {
			t.Error(err)
		}
		if el := (moduleMissions{}).Elements(s); len(el) != 0 {
			t.Errorf("an answered question stays in the bell: %+v", el)
		}

		attendu("lancer_mission", `{"id":"`+id+`"}`, "call proposer_mission first", true)
		attendu("proposer_mission", `{"carte":{"titre":""}}`, "the card was refused", true)
		attendu("proposer_mission", `{"carte":"x"}`, "expected shape", true)
		attendu("proposer_mission", `{"carte":`+carte+`}`, `"mise_en_prod"`, false)
		attendu("lancer_mission", `{"id":"autre"}`, "your mission's id", true)
		premier := attendu("lancer_mission", `{"id":"`+id+`"}`, "waiting for the owner, id ", false)
		if second := attendu("lancer_mission", `{"id":"`+id+`"}`, "waiting for the owner", false); second != premier {
			t.Errorf("a second lancer_mission is a new approval: %q then %q", premier, second)
		}
		j := `{"type":"system","subtype":"init","session_id":"sess-154"}` + "\n" + `{"type":"result","is_error":false,"result":"Carte proposée."}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}

	if err := s.lancerOrchestrateur(id, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		orchestrateursEnCours.Lock()
		fini := orchestrateursEnCours.m[id] == nil
		orchestrateursEnCours.Unlock()
		if fini {
			break
		}
		if i > 300 {
			t.Fatal("the orchestrator did not end")
		}
		time.Sleep(20 * time.Millisecond)
	}

	m, err := lireMission(id)
	if err != nil {
		t.Fatal(err)
	}
	var c CarteMission
	if err := json.Unmarshal(m.Carte, &c); err != nil || m.Etat != "proposee" || c.Titre != "Boutique en ligne" || c.Estimation.Jalons != 4 ||
		len(c.Acces) != 1 || c.Acces[0].Type != "cloudflare" || !slices.ContainsFunc(c.Approbations, func(a ApprobationPrevue) bool { return a.Type == "mise_en_prod" }) {
		t.Fatalf("the mission after the session: %s %+v %v", m.Etat, c, err)
	}
	if len(m.Questions) != 1 || slices.Contains(m.Questions[0].Choix, temoin) {
		t.Errorf("the question (its vault value masked): %+v", m.Questions)
	}
	l, _ := s.approbations.Liste()
	n := 0
	for _, a := range l {
		if a.Action == actionMission && a.Mission == id && a.Etat == "attente" && a.Details["titre"] == "Boutique en ligne" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("lancer_mission's approvals: %d, %+v", n, l)
	}

	b, _ := json.Marshal(reponses)
	f, _ := os.ReadFile(filepath.Join(dossierMissions(), id+".json"))
	if strings.Contains(string(b), temoin) || strings.Contains(string(f), temoin) {
		t.Error("a vault value in an answer or in the mission")
	}
	refus, appels := 0, map[string]int{}
	for _, la := range lignesAudit(t, dossierJournalAudit()) {
		if lb, _ := json.Marshal(la); strings.Contains(string(lb), temoin) {
			t.Errorf("a vault value in the audit log: %s", lb)
		}
		if la.Agent == agentOrchestrateur && la.Mission == id && strings.HasPrefix(la.Action, "mcp ") {
			appels[strings.TrimPrefix(la.Action, "mcp ")]++
			if la.Resultat == "refusé : outil hors du jeton" {
				refus++
			}
		}
	}
	if refus != len(outilsHub)-len(outilsOrchestrateur) || appels["modules_etat"] != 2 || appels["demander_module"] != 4 ||
		appels["question_proprietaire"] != 4 || appels["proposer_mission"] != 4 || appels["lancer_mission"] != 4 {
		t.Errorf("audited calls: %d refusals, %v", refus, appels)
	}
}
