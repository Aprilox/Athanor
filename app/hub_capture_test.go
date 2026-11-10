package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	var projets []Projet
	for i := 0; i < 20; i++ {
		projets = append(projets, fauxProjet(t, d, fmt.Sprintf("projet-%02d", i), 12, i%13))
	}
	// an archived one: the selector shows its "Archived projects" link
	archive := fauxProjet(t, d, "projet-archive", 12, 12)
	archive.Archive = true
	projets = append(projets, archive)
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
	cmd.Env = append(os.Environ(), "CAPTURE_P3_SECRETS="+cheminSecrets(projets[1])) // P3: the infra value never lands there
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
	if err := os.MkdirAll(p.Atelier(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cheminSecrets(p), []byte("MAILJET_API_KEY='"+aleatoire(16)+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
