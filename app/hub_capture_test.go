package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestCaptureHub serves the real interface on a free local port, with a temporary data folder and 20
// fake projects, then lets CAPTURE_SCRIPT (Playwright) take the screenshots into ATHANOR_CAPTURE.
// No tunnel, Web Push or background task is started. Skipped without ATHANOR_CAPTURE.
func TestCaptureHub(t *testing.T) {
	sortie, script := os.Getenv("ATHANOR_CAPTURE"), os.Getenv("CAPTURE_SCRIPT")
	if sortie == "" {
		t.Skip("ATHANOR_CAPTURE not set")
	}
	if script == "" {
		t.Fatal("CAPTURE_SCRIPT not set")
	}
	d := t.TempDir()
	// the Athanor folder (the audit log in hub/journal…) in the test's folder, never this PC's; the browser keeps the real environment
	envNavigateur := os.Environ()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "athanor"))
	}
	var projets []Projet
	for i := 0; i < 20; i++ {
		projets = append(projets, fauxProjet(t, d, fmt.Sprintf("projet-%02d", i), 12, i%13))
	}
	// an archived one: the selector shows its "Archived projects" link
	archive := fauxProjet(t, d, "projet-archive", 12, 12)
	archive.Archive = true
	projets = append(projets, archive)
	// Home's "In progress" (accueil-*): two projects wait for the owner, three have a running pilot
	// (its pilot.pid dated ahead, so it stays fresh for the whole run)
	for _, p := range projets[3:5] {
		if err := os.WriteFile(filepath.Join(p.Atelier(), "for-you", "tasks.json"), []byte(`{"taches":[{"id":"T1","titre":"Créer un compte"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range projets[5:8] {
		f := filepath.Join(p.Atelier(), "pilot.pid")
		if err := os.WriteFile(f, []byte("999999\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(f, time.Now().Add(time.Hour), time.Now().Add(time.Hour))
	}
	// a cycle log that reports the quota: the Home tile draws its bar
	quota := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.41,"resetsAt":` + fmt.Sprint(time.Now().Unix()+3600) + `}}}}` + "\n"
	// and a few turns of the lead (the Log and Team pages of the docs' images: docs/images/log.png, team.png)
	tours := `{"type":"assistant","message":{"content":[{"type":"text","text":"M4: tiles and level 1. I start with the tile set, then the first level."}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"bash .athanor/check.sh quick tiles","description":"Run the quick check on the tiles"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Agent","input":{"subagent_type":"tester","description":"tester (sonnet): checks level 1","prompt":"Play level 1 and report its bugs."}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Quick check passed: 42 tests, 3 screenshots. Committing M4."}]}}
`
	if err := os.WriteFile(filepath.Join(projets[0].LogsDir(), "cycle-001.jsonl"), []byte(quota+tours), 0o644); err != nil {
		t.Fatal(err)
	}
	// the kit folder: a page's background check (Setup & health…) may still hold a file of it for a moment
	// on Windows, so it is removed with retries instead of t.TempDir()'s single try
	kit, err := os.MkdirTemp("", "capture-kit")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := 0; i < 20 && os.RemoveAll(kit) != nil; i++ {
			time.Sleep(500 * time.Millisecond)
		}
		if _, err := os.Stat(kit); err == nil {
			t.Errorf("capture kit left behind: %s", kit)
		}
	}()
	s := nouveauServeur(Config{Kit: kit, Projets: projets, ProjetActif: "projet-00", Langue: "fr", Theme: "clair", BienvenueVue: true, PasAnimation: true})
	s.cleAcces = aleatoire(24)
	s.coffre, s.coffreErr = coffreCapture(t), nil // never this PC's own vault
	accesCapture(t, s, projets[0], projets[1])
	// the server writes its config beside the test binary, which then becomes Athanor's folder (dossierAthanor):
	// written now, so the audit lines below land where the pages read them
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	portSSH := serveursCapture(t)
	historiqueCapture(t, projets[0])
	fournisseursCapture(t, s)
	// the agents' pending approvals (cloche-*, appr-*): an access, a staging deploy, a prod one refused by the script
	for _, a := range []Approbation{
		{Projet: "projet-01", Env: "dev", Mission: "M4", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "STRIPE_SECRET_KEY"},
			Raison: "Le paiement de test du jalon M4 a besoin de la clé Stripe de test."},
		{Projet: "projet-02", Env: "staging", Mission: "M9", Agent: "pilote", Action: actionDeploiement, Details: map[string]string{"commit": "4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9", "hote": "recette.boutique.fr", "serveurs": "c0ffee02"},
			Raison: "commit 4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9 → recette.boutique.fr"},
		{Projet: "projet-02", Env: "prod", Mission: "M9", Agent: "pilote", Action: actionDeploiement, Details: map[string]string{"commit": "4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9", "hote": "boutique.fr", "serveurs": "c0ffee01,c0ffee03"},
			Raison: "commit 4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9 → boutique.fr"},
	} {
		if _, err := s.approbations.Demander(a); err != nil {
			t.Fatal(err)
		}
	}
	// the audit log (audit-*): an access given, a module's request and its answer (same ID), a "Force", an emergency stop
	for _, l := range []LigneAudit{
		{Type: "evenement", De: "fenetre", Vers: "hub", Projet: "projet-00", Action: "accorder", Resultat: "Stripe test [sec-1, code] → STRIPE_KEY, test"},
		{ID: "bus-7", Type: "demande", De: "dev", Vers: "hub", Projet: "projet-01", Mission: "M4", Agent: "pilote", Action: "demander_acces", Resultat: "Stripe test"},
		{ID: "bus-7", Type: "reponse", De: "hub", Vers: "dev", Projet: "projet-01", Action: "demander_acces", Resultat: "en attente"},
		{Type: "evenement", De: "appareil:telephone", Vers: "hub", Projet: "projet-00", Action: "forcer_infra_natif", Resultat: "true"},
		{Type: "evenement", De: "fenetre", Vers: "hub", Action: "arret_urgence", Resultat: "3 pilotes arrêtés"},
	} {
		s.ecrireAudit(l)
	}
	// P2 (p2-*): "Test" of a pasted GitHub token reaches this fake GitHub, never the real one
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghp_") {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-OAuth-Scopes", "repo, workflow")
		_, _ = w.Write([]byte(`{"login":"martin-dev"}`))
	}))
	defer gh.Close()
	ancienGH := urlGitHub
	urlGitHub = gh.URL
	defer func() { urlGitHub = ancienGH }()
	mux := http.NewServeMux()
	s.routes(mux)
	s.routesInterface(mux)
	// verifierAcces: a password only while it gives keys (hub_preuve.go), never for the other pages
	mux.HandleFunc("POST /test/mdp-preuve", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("on") == "" {
			s.mu.Lock()
			s.config.MotDePasseHash, s.session = "", ""
			s.mu.Unlock()
			return
		}
		avecMdpPreuve(s)
		http.SetCookie(w, &http.Cookie{Name: cookieSessionTest.Name, Value: cookieSessionTest.Value, Path: "/"})
	})
	// incident-* (M137): projet-00's staging red twice in a row (an incident), prod healthy, dev's certificate soon; off afterwards
	t.Cleanup(func() { _ = os.Remove(fichierSanteEnvs()) })
	mux.HandleFunc("POST /test/incident", func(w http.ResponseWriter, r *http.Request) {
		_ = os.Remove(fichierSanteEnvs())
		if r.URL.Query().Get("on") == "" {
			return
		}
		echec := &VerifDeploiement{OK: false, Problemes: []string{"statut"}, HTTP: ResultatHTTP{URL: "https://preprod.example.org", Statut: 502}, T: "2026-11-12T09:15:00Z"}
		modifierSante(func(f *fichierSante) {
			f.Dernier = "2026-11-12T09:15:00Z"
			f.Envs["projet-00/staging"] = SanteEnv{Etat: "incident", Rouges: 2, RougeDepuis: "2026-11-12T09:00:00Z", T: "2026-11-12T09:15:00Z", Verification: echec,
				Problemes: []ProblemeSante{{Code: "verif", Rouge: true}}}
			f.Envs["projet-00/prod"] = SanteEnv{Etat: "ok", T: "2026-11-12T09:15:00Z"}
			f.Envs["projet-00/dev"] = SanteEnv{Etat: "alerte", T: "2026-11-12T09:15:00Z", Problemes: []ProblemeSante{{Code: "certificat", Args: []any{10}}}}
		})
	})
	// mission-carte-* (M156): the bakery's card, proposed (?on=1), with an open question (?question=1); removed otherwise
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	// mission-cloche-tel (M158): ?lancer=1 adds capture03, the same card whose orchestrator asks to launch it
	// mission-pause-* (M172): Resume starts the project's pilot, Pause and Stop stop it; none in the screenshots' kit
	demarrer, arreter := demarrerPiloteMission, arreterPiloteMission
	demarrerPiloteMission = func(*Serveur, Projet) (string, error) { return "", nil }
	arreterPiloteMission = func(*Serveur, Projet) error { return nil }
	t.Cleanup(func() { demarrerPiloteMission, arreterPiloteMission = demarrer, arreter })
	mux.HandleFunc("POST /test/mission", func(w http.ResponseWriter, r *http.Request) {
		s.fermerLancementMission("capture03", "hub", "refuser", "capture") // never left pending for the next screenshots
		enAttente, _ := s.approbations.Liste()
		for _, x := range enAttente { // mission-accord-tel's prod accord neither
			if x.Mission == "capture06" && x.Etat == "attente" {
				s.refuserAccordMission(x.ID, "hub", "capture")
			}
		}
		// M172: the follower Resume started ends before capture06 comes back
		if _, suivie := suivisMission.Load("capture06"); suivie {
			_, _ = modifierMission("capture06", func(m *Mission) error { m.Etat = "arretee"; return nil })
			for i := 0; i < 80; i++ {
				if _, ok := suivisMission.Load("capture06"); !ok {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if f := lireSante(); f.Envs["projet-01/prod"].Etat != "" { // mission-incident-* (M172): its incident, gone with its taking over
			modifierSante(func(f *fichierSante) {
				delete(f.Envs, "projet-01/prod")
				for k := range f.Mains {
					if strings.HasPrefix(k, "sante-projet-01/prod") {
						delete(f.Mains, k)
					}
				}
			})
		}
		_ = os.RemoveAll(dossierMissions())
		_ = os.Remove(filepath.Join(dossierTaches("c0ffee02"), "7a5c0e11d2b4.json")) // ?liste=1's task
		_ = os.Remove(filepath.Join(dossierTaches("c0ffee01"), "9b1d3e5f7a20.json")) // and capture06's
		if r.URL.Query().Get("on") == "" {
			return
		}
		ctx := s.contexteCarte()
		c := CarteMission{Titre: "Site vitrine de la boulangerie Martin", Projet: ProjetCarte{Nom: "boulangerie-martin", Nouveau: true},
			Dev: BriefCarte{Brief: "Un site vitrine : la boutique, les horaires, les pains du jour et un formulaire de commande pour le retrait."},
			Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.boulangerie-martin.fr", Protection: "auth_proxy", Serveurs: []string{"c0ffee02"}},
				{Nom: "prod", URL: "https://boulangerie-martin.fr", Protection: "aucune", Serveurs: []string{"c0ffee01"}}},
			Serveurs: BriefCarte{Brief: "Noms DNS chez Cloudflare, certificats, proxy Caddy, mot de passe devant dev, pare-feu."},
			Acces: []AccesCarte{{Entree: "CLOUDFLARE_API_TOKEN", Pour: "boulangerie-martin/prod"}, {Entree: "SSH_VPS_PROD", Pour: "c0ffee01"},
				{Entree: "STRIPE_SECRET_KEY", Pour: "boulangerie-martin"}, {Entree: "SMTP_PASSWORD", Pour: "boulangerie-martin"}},
			JalonsEstimes: 8}
		if err := validerCarte(&c, ctx); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		b, _ := json.Marshal(calculerCarte(c, "prod", ctx))
		cree := time.Now().UTC().Format(formatCree)
		m := Mission{ID: "capture01", Phrase: "Un site vitrine pour la boulangerie Martin sur @VPS production, dev protégée, prod sur boulangerie-martin.fr",
			Mentions: MentionsMission{Serveurs: []string{"c0ffee01"}, Nouveau: true}, Etat: "proposee", Carte: b, Politique: "prod",
			Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{{T: cree, Etat: "proposee"}}, Cree: cree}
		if r.URL.Query().Get("question") != "" {
			m.Questions = append(m.Questions, QuestionMission{ID: "q1", Texte: "Quel serveur pour l'environnement de dev ?", Choix: []string{"Home server", "VPS production"}, Cree: cree})
			if r.URL.Query().Get("long") != "" { // mission-question-tel (B90): a choice longer than a phone's line
				m.Questions[0].Choix = append(m.Questions[0].Choix, "autre domaine très long qui ne tient pas sur une ligne")
			}
		}
		if err := enregistrerMission(m); err != nil {
			http.Error(w, err.Error(), 500)
		}
		if r.URL.Query().Get("lancer") != "" {
			m.ID, m.Questions = "capture03", []QuestionMission{}
			if err := enregistrerMission(m); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err := outilLancerMission(s, SessionHub{Mission: m.ID, Agent: agentOrchestrateur}, json.RawMessage(`{"id":"capture03"}`)); err != nil {
				http.Error(w, err.Error(), 500)
			}
		}
		// missions-* (M159): ?liste=1 adds missions launched (a project's pilot and a Servers task waiting for the
		// owner), finished, failed and cancelled, older than the bakery's
		// ?direct=1 (M171): capture06 alone, so the list's counts stay
		if liste, direct := r.URL.Query().Get("liste") != "", r.URL.Query().Get("direct") != ""; liste || direct {
			tache := TacheServeurs{ID: "7a5c0e11d2b4", Texte: "Noms DNS et proxy", Serveurs: []string{"c0ffee02"}, Etat: "attente_accord", Env: "projet-02/staging",
				Cree: "2026-11-12T08:00:00Z", Historique: []EvenementTache{}}
			if err := enregistrerTache(tache); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			c := CarteMission{Titre: "Paiement en ligne de la boutique", Projet: ProjetCarte{Nom: "projet-02"}, Dev: BriefCarte{Brief: "Ajouter le paiement par carte au panier."},
				Envs:     []EnvCarte{{Nom: "staging", URL: "https://recette.boutique.fr", Protection: "auth_proxy", Serveurs: []string{"c0ffee02"}}},
				Serveurs: BriefCarte{Brief: "Nom DNS de recette, certificat et proxy."}, JalonsEstimes: 3}
			if err := validerCarte(&c, ctx); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			bc, _ := json.Marshal(calculerCarte(c, "tout", ctx))
			// mission-direct-* (M171): capture06 launched, its dev checked (relay at the prod accord), a Servers task
			// applying its plan, 52 journal entries (50 shown, See more), an attention and an error among them
			tache6 := TacheServeurs{ID: "9b1d3e5f7a20", Texte: "Prod de projet-01", Serveurs: []string{"c0ffee01"}, Etat: "application", Env: "projet-01/prod",
				Cree: "2026-11-12T08:00:00Z", Historique: []EvenementTache{}}
			if err := enregistrerTache(tache6); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			c6 := CarteMission{Titre: "Formulaire de contact de projet-01", Projet: ProjetCarte{Nom: "projet-01"}, Dev: BriefCarte{Brief: "Un formulaire de contact avec envoi d'e-mail."},
				Envs: []EnvCarte{{Nom: "dev", URL: "https://dev.projet-01.fr", Protection: "auth_proxy", Serveurs: []string{"c0ffee02"}},
					{Nom: "prod", URL: "https://projet-01.fr", Protection: "aucune", Serveurs: []string{"c0ffee01"}}},
				Serveurs: BriefCarte{Brief: "Noms DNS, certificats et proxy de dev et de prod."}, JalonsEstimes: 4}
			if err := validerCarte(&c6, ctx); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			b6, _ := json.Marshal(calculerCarte(c6, "prod", ctx))
			var j6 []EntreeJournal
			heure := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
			noter := func(id, niveau, cle string, p ...string) {
				heure = heure.Add(4 * time.Minute)
				j6 = append(j6, EntreeJournal{ID: id, T: heure.Format(formatCree), Cle: cle, Params: p, Niveau: niveau})
			}
			noter("verif:d0:ok", "info", "{0} checked: {1} answers", "dev", "https://dev.projet-01.fr")
			for i := 1; i <= 16; i++ {
				sha := fmt.Sprintf("%07x", 0x4f2a9c1+i*7919)
				noter(fmt.Sprintf("jalon:M%d", i), "info", "Milestone delivered: {0}", fmt.Sprintf("M%d", i))
				if i == 9 {
					noter("deploi:d9:echec", "erreur", "Deployment to {0} failed: version {1}", "dev", sha)
				}
				noter(fmt.Sprintf("deploi:d%d:ok", i), "info", "Deployment to {0} done: version {1}", "dev", sha)
				noter(fmt.Sprintf("verif:d%d:ok", i), "info", "{0} checked: {1} answers", "dev", "https://dev.projet-01.fr")
			}
			noter("accord:a1", "attention", "Your accord is asked: {0} ({1})", "plan_serveurs", "prod")
			noter("redemarrage:1", "info", "Athanor restarted: the mission resumes")
			for _, x := range []Mission{
				{ID: "capture06", Phrase: "Ajouter un formulaire de contact à @projet-01, en dev puis en prod sur projet-01.fr", Etat: "lancee", Carte: b6, Politique: "prod",
					Voies:  []VoieMission{{Module: "dev", Ref: "projet-01", Etat: "lancee"}, {Module: "servers", Ref: tache6.ID, Etat: "preparation"}},
					Relais: &RelaisMission{Etape: "dev_verifie", Commit: "4f2a9c1e8b7d60a3f5c2e9d14b8a7f60c3e2d1b9", DeploiementDev: "d16"}, Journal: j6,
					Historique: []EvenementMission{{T: "2026-11-12T07:58:00Z", Etat: "proposee"}, {T: "2026-11-12T08:00:00Z", Etat: "lancee", Detail: "hub"}}, Cree: "2026-11-12T07:55:00Z"},
				{ID: "capture02", Phrase: "Ajouter le paiement par carte à @projet-02 et le mettre en recette sur recette.boutique.fr", Etat: "lancee", Carte: bc, Politique: "tout",
					Voies:      []VoieMission{{Module: "dev", Ref: "projet-02", Etat: "lancee"}, {Module: "servers", Ref: tache.ID, Etat: "preparation"}},
					Historique: []EvenementMission{{T: "2026-11-12T07:55:00Z", Etat: "proposee"}, {T: "2026-11-12T08:00:00Z", Etat: "lancee", Detail: "hub"}}, Cree: "2026-11-12T07:50:00Z"},
				{ID: "capture04", Phrase: "Une page de contact pour @projet-01", Etat: "terminee", Politique: "prod", Voies: []VoieMission{{Module: "dev", Ref: "projet-01", Etat: "lancee"}},
					Historique: []EvenementMission{{T: "2026-11-10T10:00:00Z", Etat: "terminee"}}, Cree: "2026-11-10T09:00:00Z"},
				{ID: "capture05", Phrase: "Mettre @projet-03 en ligne sur un serveur", Etat: "echec", Politique: "prod", Voies: []VoieMission{},
					// B87: a launch stopped by a failed step, its reason a key of i18n.js (the bell and the detail page translate it)
					Historique: []EvenementMission{{T: "2026-11-09T09:59:00Z", Etat: "lancee", Detail: "hub"}, {T: "2026-11-09T10:00:00Z", Etat: "echec", Detail: "the project projet-03 no longer exists",
						Message: &MessageMoteur{Cle: "the project {0} no longer exists", Args: []any{"projet-03"}}}}, Cree: "2026-11-09T09:58:00Z"},
			} {
				if x.ID == "capture06" && !direct || x.ID != "capture06" && !liste {
					continue
				}
				// M172: ?refus=1, the prod accord refused (Ask again); ?incident=1, the prod deployed then red (P6)
				if x.ID == "capture06" && r.URL.Query().Get("refus") != "" {
					x.Relais = &RelaisMission{Etape: "accord_prod", Commit: x.Relais.Commit, DeploiementDev: "d16", Accord: "a9"}
					x.Journal = append(slices.Clone(x.Journal), EntreeJournal{ID: "accord:a9:refusee", T: heure.Add(4 * time.Minute).Format(formatCree),
						Cle: "Accord not given ({2}): {0} ({1})", Params: []string{"deploiement", "prod", "refusee"}, Niveau: "attention"})
				}
				if x.ID == "capture06" && r.URL.Query().Get("incident") != "" {
					x.Relais = &RelaisMission{Etape: "prod_deploye", Commit: x.Relais.Commit, DeploiementDev: "d16", DeploiementProd: "p1"}
					x.Journal = append(slices.Clone(x.Journal), EntreeJournal{ID: "incident:p1", T: heure.Add(4 * time.Minute).Format(formatCree),
						Cle: "Production incident on {0}: retry, go back to the previous version or take over", Params: []string{"projet-01.fr"}, Niveau: "erreur"})
					echec := &VerifDeploiement{OK: false, Problemes: []string{"statut"}, HTTP: ResultatHTTP{URL: "https://projet-01.fr", Statut: 502}, T: "2026-11-12T09:15:00Z"}
					modifierSante(func(f *fichierSante) {
						f.Envs["projet-01/prod"] = SanteEnv{Etat: "incident", Rouges: 2, RougeDepuis: "2026-11-12T09:00:00Z", T: "2026-11-12T09:15:00Z", Verification: echec,
							Problemes: []ProblemeSante{{Code: "verif", Rouge: true}}}
					})
				}
				x.Mentions, x.Questions = MentionsMission{Projets: []string{}, Serveurs: []string{}, Cles: []string{}}, []QuestionMission{}
				if err := enregistrerMission(x); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				// mission-accord-tel (M175, P7): ?accord=1, capture06's prod accord pending, as the relay asks it
				if x.ID == "capture06" && r.URL.Query().Get("accord") != "" {
					sha := x.Relais.Commit
					x.Relais = &RelaisMission{Etape: "accord_prod", Commit: sha, DeploiementDev: "d16"}
					y, err := s.demanderApprobation(Approbation{Projet: "projet-01", Env: "prod", Mission: x.ID, Agent: "relais", Action: actionDeploiement,
						Raison: "mission " + x.ID + ": commit " + sha + " → projet-01.fr",
						Details: map[string]string{"mission": x.ID, "empreinte": empreinteCarte(x), "sha": sha, "commit": shaCourt(sha), "env": "prod", "url": "https://projet-01.fr",
							"hote": "projet-01.fr", "serveurs": "VPS production", "avant": "3b8e0d2", "commits": "3",
							"changements": "M4: envoi de l'e-mail de contact; M3: formulaire de contact; M2: page Contact dans le menu"}})
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
					x.Relais.Accord = y.ID
					if err := enregistrerMission(x); err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				}
			}
		}
	})
	// mission-direct-clair (M171): a new journal entry of capture06, shown by the live page within 2 s
	mux.HandleFunc("POST /test/mission-journal", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		s.noterJournalMission("capture06", s.entreeJournal("test:"+now.UTC().Format(formatCree), now, "attention", "A Servers plan on {0} waits for your accord", "VPS production"))
	})
	// plan-serveur-* (M91): two Servers plans to decide, created after the appr-* screenshots so their counts stay
	mux.HandleFunc("POST /test/plans", func(w http.ResponseWriter, r *http.Request) {
		if l, _ := s.approbations.Liste(); slices.ContainsFunc(l, func(x Approbation) bool { return x.Action == actionPlanServeur }) {
			return // already there: each plan-serveur-* screenshot may run alone (quick mode)
		}
		long := strings.Repeat("/var/www/boutique/releases/2026-11-12T10-00-00Z", 7) // B48: an agent's text without a space
		for _, p := range []struct {
			id, nom, env, resume string
			etapes               []EtapePlan
		}{
			{"c0ffee02", "Home server", "dev", "Mettre nginx à jour et recharger sa configuration pour l'aperçu de la boutique.", []EtapePlan{
				{Outil: "ssh_commande", Arguments: json.RawMessage(`{"commande":"apt-get install -y nginx"}`), Pourquoi: "La version installée a une faille corrigée en 1.26."},
				{Outil: "fichier_ecrire", Arguments: json.RawMessage(`{"chemin":"/etc/nginx/sites-available/boutique","contenu":"server { listen 80; root /var/www/boutique; }"}`), Pourquoi: "Servir l'aperçu depuis " + long},
				{Outil: "service", Arguments: json.RawMessage(`{"action":"reload","nom":"nginx"}`), Pourquoi: "Prendre la nouvelle configuration sans couper le site <b>maintenant</b>."}}},
			{"c0ffee01", "VPS production", "prod", "Supprimer les anciennes versions de la boutique pour libérer 4 Go.", []EtapePlan{
				{Outil: "ssh_commande", Arguments: json.RawMessage(`{"commande":"rm -rf /var/www/boutique/releases/2025-*"}`), Pourquoi: "Le disque est plein à 94 %.", Sauvegarde: []string{"/var/www/boutique/releases"}}}},
		} {
			pl := PlanServeur{Serveur: p.id, Nom: p.nom, Env: p.env, Projet: "projet-03", Mission: "M12", Agent: agentServeurs, Resume: p.resume}
			haut := ClasseLecture
			for _, e := range p.etapes {
				var a map[string]json.RawMessage
				_ = json.Unmarshal(e.Arguments, &a)
				et, err := classerEtape(e.Outil, a)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				e.Arguments, e.Classe, e.Raison, haut = et.Arguments, et.Classe.String(), et.Raison, max(haut, et.Classe)
				pl.Etapes = append(pl.Etapes, e)
			}
			regle := map[bool]string{true: "oui", false: "non"}[p.env == "dev" && haut < ClasseDestructrice]
			ap, err := s.approbations.Demander(Approbation{Projet: pl.Projet, Env: p.env, Mission: pl.Mission, Agent: pl.Agent, Action: actionPlanServeur, Raison: p.resume,
				Details: map[string]string{"serveur": p.id, "nom": p.nom, "classe": haut.String(), "etapes": fmt.Sprint(len(pl.Etapes)), "regle": regle, "seule_cle": map[bool]string{true: "oui"}[p.env == "prod"]}})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if p.env == "prod" { // C73: the card that says only Athanor's key gets in after it
				pl.SeuleCle = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEcleDeCaptureAthanorPasUneVraieCle0000000000"
			}
			pl.ID, pl.Cree, pl.Expire = ap.ID, ap.Cree, ap.Expire
			if err := s.enregistrerPlan(pl); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
	})
	// durcir-cloche-clair (B80): Athanor's own hardening plan of "web-1", its project internal
	mux.HandleFunc("POST /test/durcir", func(w http.ResponseWriter, r *http.Request) {
		if l, _ := s.approbations.Liste(); slices.ContainsFunc(l, func(x Approbation) bool { return x.Agent == agentDurcir }) {
			return
		}
		etapes, err := s.planDurcissement(ServeurInventaire{ID: "c0ffee04", Nom: "web-1", Utilisateur: "debian"}, map[string]bool{"utilisateur": true})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		pl := PlanServeur{Serveur: "c0ffee04", Nom: "web-1", Env: "dev", Projet: prefixeServeur + "c0ffee04", Agent: agentDurcir, Resume: resumeDurcissement, Etapes: etapes}
		ap, err := s.approbations.Demander(Approbation{Projet: pl.Projet, Env: pl.Env, Agent: agentDurcir, Action: actionPlanServeur, Raison: resumeDurcissement,
			Details: map[string]string{"serveur": "c0ffee04", "nom": "web-1", "classe": classeHaute(etapes), "etapes": fmt.Sprint(len(etapes)), "regle": "non"}})
		if err == nil {
			pl.ID, pl.Cree, pl.Expire = ap.ID, ap.Cree, ap.Expire
			err = s.enregistrerPlan(pl)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	ecoute, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ecoute) }()
	defer srv.Close()

	// the access address carries a single-use token: only in a file of t.TempDir(), removed at the end
	fichierURL := filepath.Join(t.TempDir(), "url.txt")
	if err := os.WriteFile(fichierURL, []byte(s.lienOuverture("http://"+ecoute.Addr().String()+"/")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, fin := context.WithTimeout(context.Background(), 5*time.Minute)
	defer fin()
	cmd := exec.CommandContext(ctx, "node", script, fichierURL, sortie)
	cmd.Env = append(envNavigateur, "CAPTURE_P3_SECRETS="+cheminSecrets(projets[1]), fmt.Sprint("CAPTURE_SSH_PORT=", portSSH)) // P3: the infra value never lands there
	sorties, err := cmd.CombinedOutput()
	t.Logf("%s", sorties)
	if err != nil {
		t.Fatalf("capture script: %v", err)
	}
}

// coffreCapture: a vault with a fake keystore in t.TempDir(), its infra key opened by a password of its
// own (Athanor itself has none: the page shows the infra scope unavailable), values made at run time.
func coffreCapture(t *testing.T) *Coffre {
	c, _ := coffreTest(t, nouveauFauxSysteme())
	mdp := aleatoire(12)
	if err := c.ChangerMotDePasse("", mdp); err != nil {
		t.Fatal(err)
	}
	if err := c.Deverrouiller(mdp); err != nil {
		t.Fatal(err)
	}
	var migration etatMigration
	for _, e := range []EntreeCoffre{
		{Nom: "CLOUDFLARE_API_TOKEN", Famille: "secret", Type: "cloudflare", Portee: "infra", Note: "DNS et tunnels de example.org"},
		{Nom: "SSH_VPS_PROD", Famille: "secret", Type: "ssh", Portee: "infra", Note: "Serveur de production, utilisateur deploy"},
		{Nom: "GITHUB_TOKEN", Famille: "secret", Type: "github", Portee: "code", Note: "Reads private repositories", Source: "projet:boutique"},
		{Nom: "ANTHROPIC_API_KEY", Famille: "secret", Type: "anthropic", Portee: "code", Source: "mcp:assistant"},
		{Nom: "STRIPE_SECRET_KEY", Famille: "secret", Type: "stripe", Portee: "code", Note: "Mode test de la boutique"},
		{Nom: "NODE_ENV", Famille: "variable", Note: "Mode des builds"},
		{Nom: "SITE_URL", Famille: "variable"},
	} {
		v := aleatoire(24)
		if e.Famille == "variable" {
			v = map[string]string{"NODE_ENV": "production", "SITE_URL": "https://example.org"}[e.Nom]
		}
		if e.Famille == "variable" {
			e.Type = "autre"
		}
		if e.Type == "ssh" { // a real key: "Test" reads its type without the network
			v = clePEMTest()
		}
		x, err := c.Ajouter(e, v)
		if err != nil {
			t.Fatal(e.Nom, err)
		}
		if e.Source != "" { // the entries with a source come from the import of the old files
			migration.Entrees = append(migration.Entrees, x.ID)
		}
	}
	if err := migration.enregistrer(cheminMigration(c)); err != nil {
		t.Fatal(err)
	}
	return c
}

// accesCapture: the Access page of projet-00 (acces-*): grants on dev and prod, an automatic one, an infra
// entry kept by Athanor, and a line of the old secrets.env that is not in the vault (the fallback).
func accesCapture(t *testing.T, s *Serveur, p, p3 Projet) {
	_ = os.Remove(cheminAcces()) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(cheminAcces()) })
	ids := map[string]EntreeCoffre{}
	for _, e := range s.coffre.Liste() {
		ids[e.Nom] = e
	}
	for _, g := range []struct{ nom, env, auto string }{
		{"STRIPE_SECRET_KEY", "dev", ""}, {"GITHUB_TOKEN", "dev", "migration"}, {"NODE_ENV", "dev", ""},
		{"CLOUDFLARE_API_TOKEN", "dev", ""}, {"STRIPE_SECRET_KEY", "prod", ""}, {"SITE_URL", "prod", ""},
	} {
		if _, err := s.acces.Ajouter(p.Nom, ids[g.nom], g.env, "", g.auto); err != nil {
			t.Fatal(g.nom, err)
		}
	}
	// P3 (p3-*): projet-01 starts with 2 accesses on dev; "Cloudflare – martin.fr" is added by the script itself
	for _, nom := range []string{"STRIPE_SECRET_KEY", "NODE_ENV"} {
		if _, err := s.acces.Ajouter(p3.Nom, ids[nom], "dev", "", ""); err != nil {
			t.Fatal(nom, err)
		}
	}
	// M112 (projet-environnements-*): prod configured on c0ffee01 with its deployment key; staging set by the script
	if err := s.acces.modifier(func(f *fichierAcces) error {
		projetDe(f, p.Nom).Environnements = map[string]*Environnement{"prod": {URL: "https://boutique.example.org", Serveurs: []string{"c0ffee01"},
			Protection: "access", Politique: "accord", Deploiement: &ConfigDeploiement{Methode: "statique", Source: "git"}},
			// M136: dev deployed after each milestone (its box ticked, its last relay in suivi-relais-clair)
			"dev": {Serveurs: []string{}, Protection: "aucune", Politique: "accord", DeployerAuJalon: true}}
		projetDe(f, p.Nom).CleDeploiement = "cle-deploiement-capture"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cheminSecrets(p), []byte("MAILJET_API_KEY='"+aleatoire(16)+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fournisseursCapture: the Providers page (serveurs-fournisseurs-*): Cloudflare on a fake API (the script
// presses Test: zones exemple.fr and autre.org), a never-tested provider used by projet-00/prod (its removal
// is refused) and an expired one; fournisseurs.json is removed at the end.
func fournisseursCapture(t *testing.T, s *Serveur) {
	cf := fauxCloudflare(t)
	var cle EntreeCoffre
	for _, e := range s.coffre.Liste() {
		if e.Nom == "CLOUDFLARE_API_TOKEN" {
			cle = e
		}
	}
	jeton, err := s.coffre.Valeur(cle.ID)
	if err != nil {
		t.Fatal(err)
	}
	cf.jetons[jeton] = "ok"
	t.Cleanup(func() { _ = os.Remove(fichierFournisseursChemin()) })
	hier := time.Now().UTC().Add(-26 * time.Hour)
	if err := modifierFournisseurs(func([]Fournisseur) ([]Fournisseur, error) {
		return []Fournisseur{
			{ID: "cf-capture", Type: "cloudflare", Nom: "Cloudflare – exemple.fr", Cle: cle.ID, Zones: []ZoneFournisseur{}, Droits: []string{}},
			{ID: "cf-ancien", Type: "cloudflare", Nom: "Ancien compte", Cle: cle.ID, Zones: []ZoneFournisseur{}, Droits: []string{}, Teste: hier, Etat: "expire",
				Bilan: &MessageTest{Cle: "This Cloudflare token has expired: create a new one and replace it in the Vault.", Args: []string{}}},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.modifier(func(f *fichierAcces) error {
		projetDe(f, "projet-00").Environnements["prod"].Fournisseur = "cf-capture"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// serveursCapture: the Servers page (serveurs-*): four servers (two with a sheet read from testdata, one
// waiting for its key, one unreachable) and an in-memory SSH server whose port the wizard capture types,
// so "Continue" reads a real fingerprint. Returns that port.
func serveursCapture(t *testing.T) int {
	ssh := lancerSSHMemoire(t, nil, "", func(string, []byte) (string, int) { return "", 1 })
	maintenant := time.Now()
	liste := []ServeurInventaire{
		{ID: "c0ffee01", Nom: "VPS production", Hote: "vps.example.org", Port: 22, Utilisateur: "deploy", EmpreinteHote: "SHA256:q3XyV0b8mFh2kLtR7cWnJ5sPzA1dE9gUoK4iY6vBxN0", Roles: []string{"prod"}, Etat: "ok", Cree: maintenant.Add(-72 * time.Hour), Vu: maintenant.Add(-20 * time.Minute), MotDePasseCoupe: true},
		{ID: "c0ffee02", Nom: "Home server", Hote: "192.168.1.40", Port: 2222, Utilisateur: "pi", Roles: []string{"dev"}, Etat: "ok", Cree: maintenant.Add(-48 * time.Hour), Vu: maintenant.Add(-3 * time.Hour)},
		{ID: "c0ffee03", Nom: "Staging", Hote: "staging.example.org", Port: 22, Utilisateur: "root", Roles: []string{"dev", "prod"}, Etat: "inconnu", Cree: maintenant.Add(-10 * time.Minute)},
		{ID: "c0ffee04", Nom: "Ancien VPS", Hote: "old.example.net", Port: 22, Utilisateur: "admin", Roles: nil, Etat: "injoignable", Cree: maintenant.Add(-900 * time.Hour), Vu: maintenant.Add(-200 * time.Hour)},
	}
	if err := modifierServeurs(func([]ServeurInventaire) ([]ServeurInventaire, error) { return liste, nil }); err != nil {
		t.Fatal(err)
	}
	for id, f := range map[string]string{"c0ffee01": "fiche-debian.txt", "c0ffee02": "fiche-alpine.txt"} {
		fiche := analyserFiche(lireTestdata(t, f))
		fiche.Date = maintenant
		b, _ := json.Marshal(fiche)
		d := filepath.Join(dossierModule("servers"), id)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "fiche.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// M96: the runbook of c0ffee01 with an agent's entry and a change Athanor ran
	_ = os.Remove(fichierRunbook("c0ffee01"))
	for _, e := range [][3]string{{"historique", "agent", "nginx 1.26 vérifié, certificat valable jusqu'en mars."},
		{"historique", "Athanor", `ssh_commande (destructrice, plan 4f2a, step 1) {"commande":"rm -rf /srv/boutique/v1"} → code 0`},
		{"sauvegardes", "Athanor", "backup: /var/backups/athanor/4f2a-1.tar.gz (12 KB; /srv/boutique/v1) (plan 4f2a, step 1)"}} {
		if err := ajouterRunbook("c0ffee01", e[0], e[1], e[2], 0); err != nil {
			t.Fatal(err)
		}
	}
	return ssh.port
}

// historiqueCapture (M133, projet-environnements-*): prod's history for its Deployments block, written once
// dossierAthanor is the test binary's folder: v1 checked, v2 failed its check, then back to v1.
func historiqueCapture(t *testing.T, p Projet) {
	acc := ResultatHTTP{URL: "https://boutique.example.org", Statut: 302, Redirection: "https://equipe.cloudflareaccess.com/cdn-cgi/access/login",
		TLS: &InfoTLS{Valide: true, JoursRestants: 74}, Manquants: []string{"Permissions-Policy"}}
	ouvert := acc
	ouvert.Statut, ouvert.Redirection = 200, ""
	v1, v2 := jugerVerification(Environnement{Protection: "access"}, acc), jugerVerification(Environnement{Protection: "access"}, ouvert)
	for _, d := range []Deploiement{
		{ID: "cap1", Commit: "4f1c2a9e8b7d6c5b4a39281706f5e4d3c2b1a090", Etat: "ok", Auteur: "agent", Debut: "2026-11-10T09:12:00Z", Verification: &v1},
		{ID: "cap2", Commit: "9a8b7c6d5e4f30211203f4e5d6c7b8a9f0e1d2c3", Etat: "revenu", Auteur: "proprietaire", Debut: "2026-11-11T16:40:00Z", Verification: &v2, Message: messageVerification(&v2)},
		{ID: "cap3", Commit: "4f1c2a9e8b7d6c5b4a39281706f5e4d3c2b1a090", Etat: "ok", Auteur: "proprietaire", Retour: true, Debut: "2026-11-11T16:45:00Z", Verification: &v1},
	} {
		d.Projet, d.Env, d.Methode, d.Serveurs = p.Nom, "prod", "statique", []string{"c0ffee01"}
		if err := noterHistorique(d); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(cheminHistorique(p.Nom, "prod")) })
	// M136: the dev relay after the last milestone, deployed and checked
	d := Deploiement{ID: "cap4", Projet: p.Nom, Env: "dev", Commit: "7e6d5c4b3a29180f7e6d5c4b3a29180f7e6d5c4b", Methode: "statique", Etat: "ok", Auteur: "relais",
		Serveurs: []string{"c0ffee02"}, Debut: "2026-11-12T08:30:00Z", Verification: &v1}
	if err := noterHistorique(d); err != nil {
		t.Fatal(err)
	}
	ecrireRelais(p.Nom, RelaisJalon{Jalon: "M4 : Le panier et le paiement", Commit: d.Commit, Date: d.Debut, Etat: "lance", ID: d.ID})
	t.Cleanup(func() { _ = os.Remove(cheminHistorique(p.Nom, "dev")); oublierRelais(p.Nom) })
}
