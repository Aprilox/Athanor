package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	// a cycle log that reports the quota: the Home tile draws its bar
	quota := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"five_hour":{"utilization":0.41,"resetsAt":` + fmt.Sprint(time.Now().Unix()+3600) + `}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projets[0].LogsDir(), "cycle-001.jsonl"), []byte(quota), 0o644); err != nil {
		t.Fatal(err)
	}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: projets, ProjetActif: "projet-00", Langue: "fr", Theme: "clair", BienvenueVue: true})
	s.cleAcces = aleatoire(24)
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
	sorties, err := exec.CommandContext(ctx, "node", script, fichierURL, sortie).CombinedOutput()
	t.Logf("%s", sorties)
	if err != nil {
		t.Fatalf("capture script: %v", err)
	}
}
