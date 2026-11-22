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

// The names older installed versions rely on to update themselves: shown as "Athanor Hub" on screen, but
// the release repository, the published files, the data folder and the instance file never change.
var nomsPubliesFiges = map[string]string{
	"windows/amd64": "Athanor-Windows-x64.exe",
	"windows/arm64": "Athanor-Windows-ARM64.exe",
	"linux/amd64":   "Athanor-Linux-x64",
	"linux/arm64":   "Athanor-Linux-ARM64",
	"darwin/arm64":  "Athanor-macOS.zip",
	"darwin/amd64":  "Athanor-macOS.zip",
}

// TestMajCompatNomsFiges: the repository and each system's file are the ones every earlier version asks for,
// and the release workflow still publishes exactly those files with their checksums.
func TestMajCompatNomsFiges(t *testing.T) {
	t.Setenv("ATHANOR_MAJ_API", "")
	if u := apiDerniereVersion(); u != "https://api.github.com/repos/Aprilox/Athanor/releases/latest" {
		t.Fatalf("release repository changed: %s", u)
	}
	avant := systemeMaj
	t.Cleanup(func() { systemeMaj = avant })
	for sys, nom := range nomsPubliesFiges {
		systemeMaj = sys
		if got := nomFichierMaj(); got != nom {
			t.Errorf("%s: %q, expected %q", sys, got, nom)
		}
	}
	brut, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	b := strings.ReplaceAll(string(brut), "\r\n", "\n")
	for _, nom := range []string{"Athanor-Windows-x64.exe", "Athanor-Windows-ARM64.exe", "Athanor-Linux-x64", "Athanor-Linux-ARM64", "Athanor-macOS.zip"} {
		if !strings.Contains(b, "publie/"+nom+"\n") {
			t.Errorf("release.yml no longer publishes %s", nom)
		}
	}
	// The checksum file lists the published files under the same names (since 2.29.0; 2.26-2.28 looked for
	// Athanor.exe, athanor-linux, athanor-macos, which no release publishes any more).
	if !strings.Contains(b, `(cd publie && sha256sum ./* | sed 's#\./##' > SHA256SUMS.txt)`) {
		t.Error("release.yml no longer writes SHA256SUMS.txt from the published files")
	}
}

// TestMajCompatAncienneCopie: a 2.x copy finds this release's file for each system and checks it against
// the release's SHA256SUMS.txt (a stand-in GitHub, never the real one).
func TestMajCompatAncienneCopie(t *testing.T) {
	avant := systemeMaj
	t.Cleanup(func() { systemeMaj = avant })
	contenus, sommes := map[string][]byte{}, ""
	for _, nom := range nomsPubliesFiges {
		contenus[nom] = []byte("new " + nom)
		h := sha256.Sum256(contenus[nom])
		sommes += hex.EncodeToString(h[:]) + "  " + nom + "\n"
	}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := ""
		for nom := range contenus {
			assets += fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/f/%s"},`, nom, srv.URL, nom)
		}
		fmt.Fprintf(w, `{"tag_name":"v%s","html_url":"x","body":"","assets":[%s{"name":"SHA256SUMS.txt","browser_download_url":"%s/s"}]}`, versionAthanor, assets, srv.URL)
	})
	mux.HandleFunc("/f/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(contenus[strings.TrimPrefix(r.URL.Path, "/f/")])
	})
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sommes) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("ATHANOR_MAJ_API", srv.URL+"/latest")
	d := t.TempDir()
	for sys, nom := range nomsPubliesFiges {
		systemeMaj = sys
		v, _, _, fichier, urlSommes, err := lireDerniereVersion()
		if err != nil || v != versionAthanor || !versionPlusRecente(v, "2.52.1") || fichier != srv.URL+"/f/"+nom || urlSommes == "" {
			t.Fatalf("%s: %s %s %v", sys, v, fichier, err)
		}
		f, fs := filepath.Join(d, "maj.part"), filepath.Join(d, "SHA256SUMS.txt")
		if err := telechargerMaj(urlSommes, fs, 1<<20, nil); err != nil {
			t.Fatal(err)
		}
		if err := telechargerMaj(fichier, f, 1<<20, nil); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(fs)
		if s, _ := sha256Fichier(f); s == "" || s != sommeAttendue(b, nomFichierMaj()) {
			t.Fatalf("%s: checksum not found or wrong", sys)
		}
	}
}

// TestMajCompatDonneesFigees: an existing install keeps its folder and its instance file (the second launch
// finds the first one instead of starting twice).
func TestMajCompatDonneesFigees(t *testing.T) {
	if filepath.Base(filepath.Dir(fichierInstance())) != "Athanor" || filepath.Base(fichierInstance()) != "instance.json" {
		t.Fatalf("instance file moved: %s", fichierInstance())
	}
	if runtime.GOOS == "windows" {
		t.Setenv("LocalAppData", `C:\Users\u\AppData\Local`)
		if dossierAthanorDefaut() != `C:\Users\u\AppData\Local\Athanor` || dossierDonneesApp() != `C:\Users\u\AppData\Local\Athanor` {
			t.Fatalf("data folder moved: %s", dossierAthanorDefaut())
		}
	} else {
		want := "Athanor" // macOS
		if runtime.GOOS == "linux" {
			want = "athanor" // ~/.local/share/athanor since 2.26
		}
		if filepath.Base(dossierDonneesApp()) != want {
			t.Fatalf("data folder moved: %s", dossierDonneesApp())
		}
	}
}

// TestMajCompat2xVers3: this 3.x is newer than every 2.x, and this executable (standing for the downloaded one)
// replaces the executable of a 2.52.1 folder made in a temporary folder, keeping its data.
func TestMajCompat2xVers3(t *testing.T) {
	if !strings.HasPrefix(versionAthanor, "3.") {
		t.Fatalf("versionAthanor %s", versionAthanor)
	}
	for _, ancienne := range []string{"2.53.0", "2.52.1", "2.9.0", "2.24.29", "3.0.0", "3.0.1", "3.1.0", "3.1.1", "3.1.2", "3.1.3", "3.1.4", "3.1.5"} {
		if !versionPlusRecente(versionAthanor, ancienne) || versionPlusRecente(ancienne, versionAthanor) {
			t.Fatalf("%s not newer than %s", versionAthanor, ancienne)
		}
	}
	avecArchive(t, kitDeTest)
	d := t.TempDir()
	for rel, c := range map[string]string{"system/version.txt": "2.52.1\n", filepath.Base(exeDuKit(d)): "old 2.52.1 executable", ".env": "KEEP=1\n", "logs/x.log": "log"} {
		f := filepath.Join(d, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := deployerKit(d)
	if err != nil || !r.Exe {
		t.Fatalf("executable not replaced: %+v %v", r, err)
	}
	if lire(t, exeDuKit(d)) == "old 2.52.1 executable" || strings.TrimSpace(lire(t, filepath.Join(d, "system", "version.txt"))) != versionAthanor {
		t.Fatalf("the 2.52.1 folder was not moved to %s", versionAthanor)
	}
	if lire(t, filepath.Join(d, ".env")) != "KEEP=1\n" || lire(t, filepath.Join(d, "logs", "x.log")) != "log" {
		t.Fatal("the update touched the user's data")
	}
}
