package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// a cloudflared that prints what the real one prints, with an address of its own, then waits: this test
// program itself, copied under cloudflared's name (the same on every system)
func init() {
	if os.Getenv("ATHANOR_FAUX_CLOUDFLARED") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "2026-10-25T10:00:00Z INF Requesting new quick Tunnel on trycloudflare.com...")
	fmt.Fprintf(os.Stderr, "2026-10-25T10:00:01Z INF |  https://essai-%d.trycloudflare.com  |\n", os.Getpid())
	fmt.Fprintln(os.Stderr, "2026-10-25T10:00:02Z INF Registered tunnel connection connIndex=0 location=cdg01 protocol=quic")
	for {
		time.Sleep(time.Second)
	}
}

func installerFauxCloudflared(t *testing.T, s *Serveur) {
	t.Helper()
	t.Setenv("ATHANOR_FAUX_CLOUDFLARED", "1")
	moi, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(moi)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(s.cheminCloudflared()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.cheminCloudflared(), b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func attendreTunnel(t *testing.T, s *Serveur, pas string) etatTunnel {
	t.Helper()
	// a minute at most: on Windows the antivirus may hold a program just copied for a few seconds
	derniere := ""
	for i := 0; i < 600; i++ {
		d := s.dist()
		d.mu.Lock()
		e := d.tunnel
		d.mu.Unlock()
		if e.etat == "pret" && e.adresse != "" && e.adresse != pas {
			return e
		}
		if e.etat == "erreur" {
			derniere = e.message // the pilot tries again by itself
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("tunnel not ready (last error: %s)", derniere)
	return etatTunnel{}
}

func TestTunnelGardeEtRelance(t *testing.T) {
	kit := t.TempDir()
	s := nouveauServeur(Config{Kit: kit})
	s.config.Distance = DistanceConfig{Actif: true, Port: 45555}
	installerFauxCloudflared(t, s)
	t.Cleanup(s.arreterTunnel) // even after a failure: nothing left holding the test's folder
	go s.lancerTunnel()
	e := attendreTunnel(t, s, "")
	if !strings.HasPrefix(e.adresse, "https://essai-") || s.hoteTunnel() != strings.TrimPrefix(e.adresse, "https://") {
		t.Fatalf("address: %+v", e)
	}
	defer terminerProcessus(e.pid)
	if b, _ := os.ReadFile(filepath.Join(kit, "logs", "distance", "cloudflared.log")); !strings.Contains(string(b), "Registered") {
		t.Fatalf("log: %s", b)
	}

	// Athanor restarts (an update): the same tunnel, the same address
	s.arreterTunnelSansTuer()
	s2 := nouveauServeur(Config{Kit: kit})
	s2.config.Distance = DistanceConfig{Actif: true, Port: 45555}
	t.Cleanup(s2.arreterTunnel)
	go s2.lancerTunnel()
	e2 := attendreTunnel(t, s2, "")
	if e2.adresse != e.adresse || e2.pid != e.pid {
		t.Fatalf("kept tunnel: %+v, before %+v", e2, e)
	}

	// it ends (network dropped): a new one, a new address
	terminerProcessus(e.pid)
	e3 := attendreTunnel(t, s2, e.adresse)
	if e3.pid == e.pid {
		t.Fatal("same process")
	}
	// switched off: stopped, nothing left
	s2.arreterTunnel()
	time.Sleep(300 * time.Millisecond)
	if cloudflaredVivant(e3.pid) {
		terminerProcessus(e3.pid)
		t.Fatal("tunnel left running")
	}
	if _, err := os.Stat(filepath.Join(kit, "logs", "distance", "tunnel.json")); err == nil {
		t.Fatal("tunnel.json left")
	}
}

func TestInstallerCloudflared(t *testing.T) {
	prog := []byte("#!/bin/sh\necho cloudflared\n")
	nom := nomAssetCloudflared()
	contenu := prog
	if strings.HasSuffix(nom, ".tgz") {
		t.Skip("the Mac archive is checked in TestCloudflaredRelease")
	}
	h := sha256.Sum256(contenu)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"2026.9.0","body":"%s: %s\nautre: %s","assets":[{"name":"%s","browser_download_url":"%s/dl/%s"}]}`,
				nom, hex.EncodeToString(h[:]), strings.Repeat("0", 64), nom, srv.URL, nom)
		case "/dl/" + nom:
			_, _ = w.Write(contenu)
		}
	}))
	defer srv.Close()
	ancA, ancO := apiCloudflared, origineCloudflared
	defer func() { apiCloudflared, origineCloudflared = ancA, ancO }()
	apiCloudflared, origineCloudflared = srv.URL+"/latest", srv.URL+"/dl/"
	s := nouveauServeur(Config{Kit: t.TempDir()})
	if err := s.installerCloudflared(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.cheminCloudflared()); string(b) != string(prog) {
		t.Fatal("not installed")
	}
	// a file that does not match its checksum: refused
	contenu = []byte("autre chose")
	_ = os.Remove(s.cheminCloudflared())
	if err := s.installerCloudflared(); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad file: %v", err)
	}
	if _, err := os.Stat(s.cheminCloudflared()); err == nil {
		t.Fatal("bad file installed")
	}
	// from another address than cloudflared's releases: refused
	contenu = prog
	origineCloudflared = "https://github.com/cloudflare/cloudflared/releases/download/"
	if err := s.installerCloudflared(); err == nil {
		t.Fatal("download from elsewhere accepted")
	}
	_ = runtime.GOOS
}
