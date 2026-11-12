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
	if err := os.WriteFile(filepath.Join(projets[0].LogsDir(), "cycle-001.jsonl"), []byte(quota), 0o644); err != nil {
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
	}()
	s := nouveauServeur(Config{Kit: kit, Projets: projets, ProjetActif: "projet-00", Langue: "fr", Theme: "clair", BienvenueVue: true})
	s.cleAcces = aleatoire(24)
	s.coffre, s.coffreErr = coffreCapture(t), nil // never this PC's own vault
	accesCapture(t, s, projets[0], projets[1])
	// the server writes its config beside the test binary, which then becomes Athanor's folder (dossierAthanor):
	// written now, so the audit lines below land where the pages read them
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	portSSH := serveursCapture(t)
	fournisseursCapture(t, s)
	// the agents' pending approvals (cloche-*, appr-*): an access, a staging deploy, a prod one refused by the script
	for _, a := range []Approbation{
		{Projet: "projet-01", Env: "dev", Mission: "M4", Agent: "pilote", Action: "demander_acces", Details: map[string]string{"nom": "STRIPE_SECRET_KEY"},
			Raison: "Le paiement de test du jalon M4 a besoin de la clé Stripe de test."},
		{Projet: "projet-02", Env: "staging", Mission: "M9", Agent: "pilote", Action: "demander_deploiement", Raison: "La version 1.2 est prête : la déployer sur staging pour la recette."},
		{Projet: "projet-02", Env: "prod", Mission: "M9", Agent: "pilote", Action: "demander_deploiement", Raison: "Mettre la version 1.2 en production."},
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
			{"c0ffee02", "Serveur maison", "dev", "Mettre nginx à jour et recharger sa configuration pour l'aperçu de la boutique.", []EtapePlan{
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
				Details: map[string]string{"serveur": p.id, "nom": p.nom, "classe": haut.String(), "etapes": fmt.Sprint(len(pl.Etapes)), "regle": regle}})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			pl.ID, pl.Cree, pl.Expire = ap.ID, ap.Cree, ap.Expire
			if err := s.enregistrerPlan(pl); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
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
		{Nom: "GITHUB_TOKEN", Famille: "secret", Type: "github", Portee: "code", Note: "Lecture des dépôts privés", Source: "projet:boutique"},
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
			Protection: "access", Politique: "accord"}}
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
		{ID: "c0ffee02", Nom: "Serveur maison", Hote: "192.168.1.40", Port: 2222, Utilisateur: "pi", Roles: []string{"dev"}, Etat: "ok", Cree: maintenant.Add(-48 * time.Hour), Vu: maintenant.Add(-3 * time.Hour)},
		{ID: "c0ffee03", Nom: "Préproduction", Hote: "staging.example.org", Port: 22, Utilisateur: "root", Roles: []string{"dev", "prod"}, Etat: "inconnu", Cree: maintenant.Add(-10 * time.Minute)},
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
		{"sauvegardes", "Athanor", "backup: /var/backups/athanor/4f2a-1.tar.gz (/srv/boutique/v1) (plan 4f2a, step 1)"}} {
		if err := ajouterRunbook("c0ffee01", e[0], e[1], e[2], 0); err != nil {
			t.Fatal(err)
		}
	}
	return ssh.port
}
