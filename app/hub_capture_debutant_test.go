package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestCaptureDebutant: a newcomer's first steps on a fresh install (no project, an empty vault, the welcome
// wizard not seen yet): wizard → first project → a key given → the first request started. The script
// (debutant.mjs, beside CAPTURE_SCRIPT) only clicks what is on screen, from the home page. Skipped without
// ATHANOR_CAPTURE, when CAPTURE_MOTIF does not match "debutant", and in the "avant:" half of the full check.
func TestCaptureDebutant(t *testing.T) {
	sortie, script := os.Getenv("ATHANOR_CAPTURE"), os.Getenv("CAPTURE_SCRIPT")
	if sortie == "" {
		t.Skip("ATHANOR_CAPTURE not set")
	}
	if m := os.Getenv("CAPTURE_MOTIF"); m != "" && !regexp.MustCompile("(?i)"+m).MatchString("debutant") {
		t.Skip("CAPTURE_MOTIF does not match debutant")
	}
	if strings.HasPrefix(os.Getenv("CAPTURE_PARTIE"), "avant:") {
		t.Skip("taken by the other half of the full check")
	}
	d := t.TempDir()
	envNavigateur := os.Environ()
	for _, v := range []string{"LOCALAPPDATA", "LocalAppData", "HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, filepath.Join(d, "athanor"))
	}
	// the tooling seen installed (WSL's distribution name written by its installer): the journey starts after it
	kit := filepath.Join(d, "outil")
	for f, c := range map[string]string{"template/CLAUDE.md": "# kit\n", "system/distro.txt": "Ubuntu\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(kit, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(kit, f), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ancienNatif := modeNatif.Load()
	modeNatif.Store(false)
	defer modeNatif.Store(ancienNatif)
	s := nouveauServeur(Config{Kit: kit, PasAnimation: true})
	s.cleAcces = aleatoire(24)
	s.coffre, _ = coffreTest(t, nouveauFauxSysteme()) // a new vault: empty, no password
	s.coffreErr = nil
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
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
	fichierURL := filepath.Join(t.TempDir(), "url.txt")
	if err := os.WriteFile(fichierURL, []byte(s.lienOuverture("http://"+ecoute.Addr().String()+"/")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, fin := context.WithTimeout(context.Background(), 3*time.Minute)
	defer fin()
	projet := filepath.Join(d, "Dev", "boutique-montres")
	cmd := exec.CommandContext(ctx, "node", filepath.Join(filepath.Dir(script), "debutant.mjs"), fichierURL, sortie)
	cmd.Env = append(envNavigateur, "CAPTURE_DEBUTANT_DOSSIER="+projet)
	sorties, err := cmd.CombinedOutput()
	t.Logf("%s", sorties)
	if err != nil {
		t.Fatalf("debutant.mjs: %v", err)
	}
	// what the newcomer did is really there: the project, its request, the key given to it
	s.mu.Lock()
	n := len(s.config.Projets)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d projects after the journey, want 1", n)
	}
	if b, _ := os.ReadFile(filepath.Join(projet, ".athanor", "REQUEST.md")); !strings.Contains(string(b), "montres") {
		t.Fatalf("request not written: %q", b)
	}
	if b, _ := os.ReadFile(cheminAcces()); !strings.Contains(string(b), "boutique-montres") {
		t.Fatalf("no access given to the project: %q", b)
	}
}
