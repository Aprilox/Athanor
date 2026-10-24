package main

import (
	"archive/zip"
	"bytes"
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
	if strings.HasSuffix(nomFichierMaj(), ".zip") { // the Mac: Athanor.app zipped
		contenu = zipMacDeTest(t, contenu)
	}
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

func TestTextesProgresMaj(t *testing.T) {
	for _, l := range []string{"fr", "en", "es", "de", "it", "pt", "xx"} {
		tx := textesProgresMaj(l)
		for i, s := range tx {
			if s == "" {
				t.Fatalf("%s: text %d empty", l, i)
			}
		}
		if !strings.Contains(tx[0], "%s") {
			t.Fatalf("%s: title without the version", l)
		}
	}
	if textesProgresMaj("fr")[5] != "Ouverture d'Athanor…" {
		t.Fatal("French texts")
	}
}

func TestLangueDuKit(t *testing.T) {
	d := t.TempDir()
	if langueDuKit(d) != "" {
		t.Fatal("no settings: no language")
	}
	if err := os.WriteFile(filepath.Join(d, nomConfig()), []byte(`{"langue":"de"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := langueDuKit(d); g != "de" {
		t.Fatalf("got %q", g)
	}
}

// zipMacDeTest: a zip of Athanor.app as the release makes it, its executable being exe.
func zipMacDeTest(t *testing.T, exe []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, d := range []string{"Athanor.app/", "Athanor.app/Contents/", "Athanor.app/Contents/MacOS/"} {
		if _, err := z.Create(d); err != nil {
			t.Fatal(err)
		}
	}
	w, _ := z.Create("__MACOSX/Athanor.app/Contents/MacOS/._athanor")
	_, _ = w.Write([]byte("resource fork"))
	w, _ = z.Create(executableZipMac)
	_, _ = w.Write(exe)
	w, _ = z.Create("Athanor.app/Contents/Info.plist")
	_, _ = w.Write([]byte("<plist/>"))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtraireZipMac(t *testing.T) {
	d := t.TempDir()
	zf := filepath.Join(d, "a.zip")
	if err := os.WriteFile(zf, zipMacDeTest(t, []byte("EXE")), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(d, "athanor")
	if err := extraireZipMac(zf, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "EXE" {
		t.Fatalf("got %q", b)
	}
	os.WriteFile(zf, []byte("not a zip"), 0o644)
	if extraireZipMac(zf, dest) == nil {
		t.Fatal("a broken archive accepted")
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("Other.app/Contents/MacOS/other")
	w.Write([]byte("x"))
	z.Close()
	os.WriteFile(zf, b.Bytes(), 0o644)
	if err := extraireZipMac(zf, dest); err == nil || !strings.Contains(err.Error(), "no Athanor") {
		t.Fatalf("an archive without Athanor: %v", err)
	}
}

func TestNomFichierMaj(t *testing.T) {
	n := nomFichierMaj()
	if n == "" || !strings.HasPrefix(n, "Athanor-") {
		t.Fatalf("%q", n)
	}
}

// the Mac's update, seen from any Unix: Athanor-macOS.zip downloaded, checked, its executable taken out
func TestPreparerMajZipMac(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executable is a shell script")
	}
	avant := systemeMaj
	systemeMaj = "darwin/arm64"
	t.Cleanup(func() { systemeMaj = avant })
	avecArchive(t, kitDeTest)
	serveurMaj(t, "99.3.0", false)
	k := Kit{t.TempDir()}
	if e := verifierMaj(k); !e.Disponible {
		t.Fatalf("check: %+v", e)
	}
	f, err := preparerMaj(k)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); !strings.Contains(string(b), "echo 99.3.0") {
		t.Fatalf("not the executable of the zip: %q", b)
	}
	if l, _ := filepath.Glob(filepath.Join(dossierMaj(k), "*.part")); len(l) > 0 {
		t.Fatalf("archive left: %v", l)
	}
}
