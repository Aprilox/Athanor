package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMissionP7 (M175, P7): a mission's prod accord, decided from the phone. Asked once dev is checked, it is in
// the bell (Accept and Refuse only, from a device), in For you and in a Web Push that opens the mission, each with
// the short SHA, the prod URL and what changes since the version prod serves (a commit subject holding a vault value
// shows it masked, nowhere in clear); an unpaired device, then a paired but locked one, decide nothing; the paired
// phone, unlocked, accepts without retyping the password and prod starts once, for that SHA; the same accord replayed
// starts nothing more.
func TestMissionP7(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	const temoin = "TEMOIN-p7-accord-5d2e"
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, temoin); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.secretsOuverts(), temoin) {
		t.Fatal("the witness value is not among the vault's readable values: the masking would not be tested")
	}
	// the tunnel and a paired phone with notifications on (as TestMissionTelephone), first: pairing moves dossierAthanor()
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
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a1", Nom: "web-1", Hote: "203.0.113.5", Roles: []string{"dev", "prod"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	depot, v1, _ := depotSiteExemple(t)
	// a third commit whose subject holds the vault's value (an agent's mistake): the accord shows it masked
	ecrireFichier(t, filepath.Join(depot, "site", "contact.html"), "<h1>Contact</h1>\n", 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "M3: formulaire de contact (clé " + temoin + ")"}} {
		c := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)...)
		c.Dir = depot
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	head, _ := gitSortie(depot, "rev-parse", "HEAD")
	p := Projet{Nom: "site", Dossier: depot}
	s.config.Projets, s.config.Langue = append(s.config.Projets, p), "fr"
	for env, u := range map[string]string{"dev": "https://dev.site.test", "prod": "https://site.test"} {
		if err := s.acces.definirAncien("site", env, Environnement{URL: u, Serveurs: []string{"a1"}, Protection: "aucune", Politique: "accord"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{dossierMissions(), fichierJalonsFaits(), cheminHistorique("site", "dev"), cheminHistorique("site", "prod")} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644) // the Studio lane finished
	lance, maintenant := time.Now().Add(-time.Minute), time.Now().UTC().Format(time.RFC3339)
	_ = noterHistorique(Deploiement{ID: "prod0", Projet: "site", Env: "prod", Commit: v1, Etat: "ok", Debut: maintenant, Serveurs: []string{"a1"},
		Verification: &VerifDeploiement{OK: true, T: maintenant}}) // prod serves v1 today
	_ = noterHistorique(Deploiement{ID: "dev1", Projet: "site", Env: "dev", Commit: head, Etat: "ok", Debut: maintenant, Serveurs: []string{"a1"},
		Verification: &VerifDeploiement{OK: true, T: maintenant}})

	var mu sync.Mutex
	var recus []MessagePush
	var bruts []string
	var lances []string // the prod deployments the engine was asked for
	ancienClient, ancienDeploiement := clientPush, commencerDeploiementMission
	t.Cleanup(func() { clientPush, commencerDeploiementMission = ancienClient, ancienDeploiement })
	clientPush = &http.Client{Transport: allerRetour(func(r *http.Request) (*http.Response, error) {
		var m MessagePush
		b, _ := io.ReadAll(r.Body)
		clair := dechiffrerPushEssai(t, ua, auth, b)
		_ = json.Unmarshal(clair, &m)
		mu.Lock()
		recus, bruts = append(recus, m), append(bruts, string(clair))
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody, Request: r}, nil
	})}
	commencerDeploiementMission = func(_ *Serveur, projet, env, commit string, o OptionsDeploiement) (Deploiement, func(context.Context) (Deploiement, error), error) {
		mu.Lock()
		lances = append(lances, env+" "+commit)
		mu.Unlock()
		d := Deploiement{ID: "prod1", Projet: projet, Env: env, Commit: commit, Etat: "en_cours", Auteur: o.Auteur, Accorde: o.Accorde,
			Serveurs: []string{"a1"}, Debut: time.Now().UTC().Format(time.RFC3339)}
		_ = noterHistorique(d)
		return d, func(context.Context) (Deploiement, error) { return d, nil }, nil
	}
	recu := func(tag string) (MessagePush, string, bool) {
		for range 200 {
			mu.Lock()
			i := slices.IndexFunc(recus, func(m MessagePush) bool { return m.Tag == tag })
			var m MessagePush
			var brut string
			if i >= 0 {
				m, brut = recus[i], bruts[i]
			}
			mu.Unlock()
			if i >= 0 {
				return m, brut, true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return MessagePush{}, "", false
	}
	observer := func(id string) Mission {
		t.Helper()
		for range 2 {
			s.observerMission(id)
		}
		m, _ := lireMission(id)
		return m
	}
	decider := func(cookie, id, decision string) int {
		return appelDistance(h, "POST", "/api/hub/approbations", cookie, `{"id":"`+id+`","decision":"`+decision+`"}`, api...).Code
	}

	carte, _ := json.Marshal(CarteMission{Titre: "Site de Martin", Projet: ProjetCarte{Nom: "site"}, Envs: []EnvCarte{{Nom: "dev"}, {Nom: "prod"}}})
	m := Mission{ID: "missionp7", Phrase: "Le site de Martin, en dev puis en prod", Etat: "lancee", Carte: carte, Politique: "autonome", Cree: lance.UTC().Format(formatCree),
		Voies:      []VoieMission{{Module: "dev", Ref: "site", Etat: "lancee"}},
		Historique: []EvenementMission{{T: lance.UTC().Format(formatCree), Etat: "lancee", Detail: "proprietaire"}}}
	if err := enregistrerMission(m); err != nil {
		t.Fatal(err)
	}

	// 1. dev checked on HEAD: the accord is asked (even in "autonome"), with where it goes and what changes
	if m = observer(m.ID); m.Relais == nil || m.Relais.Etape != "accord_prod" || len(lances) != 0 {
		t.Fatalf("the relay does not ask the prod accord: %+v %v", m.Relais, lances)
	}
	x, _ := s.approbations.Lire(m.Relais.Accord)
	court, avant := shaCourt(head), shaCourt(v1)
	if x.Etat != "attente" || x.Details["sha"] != head || x.Details["commit"] != court || x.Details["avant"] != avant || x.Details["commits"] != "2" ||
		x.Details["url"] != "https://site.test" || x.Details["hote"] != "site.test" || !strings.HasPrefix(x.Details["changements"], "M3: formulaire de contact (clé ") ||
		!strings.Contains(x.Details["changements"], "; v2") {
		t.Fatalf("the accord's details: %+v", x.Details)
	}
	// the bell: the mission's prod accord, Accept and Refuse only, both allowed from a device
	actives, _ := s.alertesHub(nil)
	i := slices.IndexFunc(actives, func(a AlerteHub) bool { return a.Empreinte == "appr-"+x.ID })
	if i < 0 {
		t.Fatal("the prod accord is not in the bell")
	}
	if a := actives[i]; a.Raison != "Deploy {0} to production? Commit {1} → {2}. {3} new commit(s): {4}" || len(a.Args) != 5 || a.Args[0] != "site.test" ||
		a.Args[1] != court || a.Args[2] != "https://site.test" || a.Args[3] != "2" || a.Args[4] != x.Details["changements"] ||
		len(a.Actions) != 2 || a.Actions[0].ID != "accepter" || a.Actions[1].ID != "refuser" || !a.Actions[0].Distance || !a.Actions[1].Distance {
		t.Fatalf("bell: %+v", a)
	}
	// the Web Push, in the owner's language, opens the mission
	n, brut, ok := recu("appr-" + x.ID)
	if !ok || n.Titre != "Mise en prod de site.test ?" || n.URL != "/#missions/"+m.ID ||
		n.Texte != "site · commit "+court+" → https://site.test · 2 nouveau(x) commit(s) : "+x.Details["changements"] {
		t.Fatalf("push: %+v", n)
	}

	// 2. an unpaired device, then the paired phone locked: nothing decided, nothing deployed
	avecMdpPreuve(s)
	for _, c := range []string{"", "appareil-inconnu"} {
		if code := decider(c, x.ID, "accepter"); code == 200 {
			t.Fatalf("unpaired device %q decides: %d", c, code)
		}
	}
	if code := decider(cle, x.ID, "accepter"); code == 200 {
		t.Fatalf("locked phone decides: %d", code)
	}
	if m = observer(m.ID); s.approbations.PourAgent(x.ID) != "attente" || len(lances) != 0 || m.Relais.Etape != "accord_prod" {
		t.Fatalf("decided without an unlocked paired phone: %s %v %+v", s.approbations.PourAgent(x.ID), lances, m.Relais)
	}

	// 3. unlocked once, For you shows it; accepted without the password in the request → prod starts, for that SHA
	if w := appelDistance(h, "POST", "/api/deverrouiller", cle, `{"mot_de_passe":"`+mdpPreuveTest+`"}`, api...); w.Code != 200 {
		t.Fatalf("phone unlocks: %d %s", w.Code, w.Body)
	}
	pv := appelDistance(h, "GET", "/api/hub/approbations", cle, "", api...)
	if pv.Code != 200 || !strings.Contains(pv.Body.String(), x.ID) || !strings.Contains(pv.Body.String(), `"changements"`) {
		t.Fatalf("For you on the phone: %d %s", pv.Code, pv.Body)
	}
	if code := decider(cle, x.ID, "toujours"); code == 200 {
		t.Fatal("a rule for production from the phone")
	}
	if code := decider(cle, x.ID, "accepter"); code != 200 {
		t.Fatalf("the phone accepts: %d", code)
	}
	m = observer(m.ID)
	x, _ = s.approbations.Lire(x.ID)
	if !slices.Equal(lances, []string{"prod " + head}) || m.Relais.Etape != "prod_deploye" || x.Etat != "acceptee" || x.Par != "appareil:"+idTel || x.Utilisee == "" {
		t.Fatalf("accepted from the phone: %v %+v %+v", lances, m.Relais, x)
	}
	// replayed: the used accord starts nothing more
	if code := decider(cle, x.ID, "accepter"); code == 200 {
		t.Error("an accord decided twice")
	}
	if observer(m.ID); len(lances) != 1 {
		t.Fatalf("the accord replayed deploys again: %v", lances)
	}

	// the witness value nowhere: the accord, the push's payload, For you, the mission and its journal
	l, _ := s.approbations.Liste()
	bl, _ := json.Marshal(l)
	bm, _ := json.Marshal(m)
	for nom, v := range map[string]string{"approvals": string(bl), "push": brut, "For you": pv.Body.String(), "mission": string(bm)} {
		if strings.Contains(v, temoin) {
			t.Errorf("the vault's value in %s", nom)
		}
	}
}
