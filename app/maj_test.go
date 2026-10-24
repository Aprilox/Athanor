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
)

func TestSommeAttendueEtNotes(t *testing.T) {
	s := []byte(strings.Repeat("a", 64) + "  Athanor.exe\n" + strings.Repeat("b", 64) + " *athanor-linux\nbad line\n")
	if sommeAttendue(s, "Athanor.exe") != strings.Repeat("a", 64) || sommeAttendue(s, "athanor-linux") != strings.Repeat("b", 64) || sommeAttendue(s, "athanor-macos") != "" {
		t.Fatal("SHA256SUMS.txt misread")
	}
	if n := notesMaj("## What's new\n\n- x\n\n## Download\n\n| a |"); n != "## What's new\n\n- x" {
		t.Fatalf("notes: %q", n)
	}
	if notesMaj("## Download\n| a |") != "" {
		t.Fatal("downloads kept in the notes")
	}
}

// serveurMaj: a stand-in of GitHub's latest release, serving a file whose "version" prints version.
func serveurMaj(t *testing.T, version string, sommeFausse bool) *httptest.Server {
	t.Helper()
	contenu := []byte("#!/bin/sh\necho " + version + "\n")
	h := sha256.Sum256(contenu)
	somme := hex.EncodeToString(h[:])
	if sommeFausse {
		somme = strings.Repeat("0", 64)
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v%s","html_url":"https://example.invalid/r","body":"## What's new\n- y\n## Download\n","assets":[{"name":%q,"browser_download_url":"%s/f"},{"name":"SHA256SUMS.txt","browser_download_url":"%s/s"}]}`, version, nomFichierMaj(), srv.URL, srv.URL)
	})
	mux.HandleFunc("/f", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(contenu) })
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  %s\n", somme, nomFichierMaj()) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("ATHANOR_MAJ_API", srv.URL+"/latest")
	return srv
}

func TestVerifierEtPreparerMaj(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executable is a shell script")
	}
	avecArchive(t, kitDeTest)
	serveurMaj(t, "99.1.0", false)
	k := Kit{t.TempDir()}
	e := verifierMaj(k)
	if !e.Possible || !e.Disponible || e.Derniere != "99.1.0" || e.Erreur != "" || !strings.Contains(e.Notes, "What's new") {
		t.Fatalf("check: %+v", e)
	}
	f, err := preparerMaj(k)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(f) != dossierMaj(k) || !fichierRegulierPilote(f) {
		t.Fatalf("downloaded to %s", f)
	}
}

func TestPreparerMajRefuseUneMauvaiseEmpreinte(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executable is a shell script")
	}
	avecArchive(t, kitDeTest)
	serveurMaj(t, "99.2.0", true)
	k := Kit{t.TempDir()}
	verifierMaj(k)
	if _, err := preparerMaj(k); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a file with the wrong checksum was accepted: %v", err)
	}
	l, _ := filepath.Glob(filepath.Join(dossierMaj(k), "athanor-*"))
	for _, x := range l {
		if !strings.HasSuffix(x, ".part") {
			t.Fatalf("refused file left in place: %s", x)
		}
	}
}

func TestMajPasPourUneVersionPlusAncienne(t *testing.T) {
	avecArchive(t, kitDeTest)
	serveurMaj(t, "1.0.0", false)
	if e := verifierMaj(Kit{t.TempDir()}); e.Disponible {
		t.Fatal("an older version offered as an update")
	}
}

func TestMajImpossibleDansUneCopieDuCode(t *testing.T) {
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	_ = os.MkdirAll(filepath.Join(d, ".git"), 0o755)
	if ok, raison := majPossible(Kit{d}); ok || raison != "source" {
		t.Fatal("update offered in a copy of the source code")
	}
	archiveKitGz = nil
	if ok, _ := majPossible(Kit{t.TempDir()}); ok {
		t.Fatal("update offered by a plain build")
	}
}
