package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func archiveTest(t *testing.T, fichiers map[string]string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	w := tar.NewWriter(gz)
	for nom, contenu := range fichiers {
		_ = w.WriteHeader(&tar.Header{Name: nom, Mode: 0o755, Size: int64(len(contenu)), Typeflag: tar.TypeReg})
		_, _ = w.Write([]byte(contenu))
	}
	w.Close()
	gz.Close()
	return b.Bytes()
}

func TestExtraireRTK(t *testing.T) {
	d := t.TempDir()
	dest := filepath.Join(d, "bin", "rtk")
	if err := extraireRTK(bytes.NewReader(archiveTest(t, map[string]string{"LICENSE": "x", "rtk-1/rtk": "#!/bin/sh\necho rtk\n"})), dest); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	st, _ := os.Stat(dest)
	if string(b) != "#!/bin/sh\necho rtk\n" || (st.Mode()&0o111 == 0 && runtime.GOOS != "windows") {
		t.Fatalf("%q %v", b, st.Mode())
	}
	if err := extraireRTK(bytes.NewReader(archiveTest(t, map[string]string{"README": "x"})), filepath.Join(d, "autre")); err == nil {
		t.Fatal("an archive without rtk accepted")
	}
	if _, err := os.Stat(filepath.Join(d, "autre")); err == nil {
		t.Fatal("a file written without rtk")
	}
	for _, c := range [][3]string{{"linux", "amd64", "x86_64-unknown-linux-musl"}, {"darwin", "arm64", "aarch64-apple-darwin"}, {"windows", "amd64", "x86_64-pc-windows-msvc"}, {"windows", "386", ""}} {
		if got := cibleRTK(c[0], c[1]); got != c[2] {
			t.Errorf("%s/%s: %q", c[0], c[1], got)
		}
	}
}

// Windows' archive is a zip: its rtk.exe (whatever the folder), never another file in its place.
func TestExtraireRTKZip(t *testing.T) {
	zipTest := func(fichiers map[string]string) []byte {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for nom, contenu := range fichiers {
			f, _ := w.Create(nom)
			_, _ = f.Write([]byte(contenu))
		}
		w.Close()
		return b.Bytes()
	}
	d := t.TempDir()
	dest := filepath.Join(d, "bin-windows", "rtk.exe")
	if err := extraireRTKZip(bytes.NewReader(zipTest(map[string]string{"README.md": "x", "rtk-x86_64-pc-windows-msvc/rtk.exe": "MZ programme"})), dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "MZ programme" {
		t.Fatalf("%q", b)
	}
	if _, err := os.Stat(dest + ".nouveau"); err == nil {
		t.Fatal("the temporary file left behind")
	}
	// a new version replaces the old one
	if err := extraireRTKZip(bytes.NewReader(zipTest(map[string]string{"RTK.EXE": "MZ v2"})), dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "MZ v2" {
		t.Fatalf("%q", b)
	}
	if err := extraireRTKZip(bytes.NewReader(zipTest(map[string]string{"rtk": "linux", "rtk.exe.sig": "x"})), filepath.Join(d, "autre.exe")); err == nil {
		t.Fatal("a zip without rtk.exe accepted")
	}
	if err := extraireRTKZip(bytes.NewReader([]byte("not a zip")), filepath.Join(d, "autre.exe")); err == nil {
		t.Fatal("a broken zip accepted")
	}
	if _, err := os.Stat(filepath.Join(d, "autre.exe")); err == nil {
		t.Fatal("a file written without rtk.exe")
	}
}

// Where RTK lives and where it comes from, system by system: the native Windows one beside Claude's
// commands (on its PATH), apart from the Linux one of the WSL mode.
func TestCheminEtURLRTK(t *testing.T) {
	kit := filepath.Join("k", "kit")
	if got := cheminRTKPour(kit, "windows"); got != filepath.Join(kit, "system", "bin-windows", "rtk.exe") {
		t.Errorf("windows: %s", got)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if got := cheminRTKPour(kit, goos); got != filepath.Join(kit, "system", "bin", "rtk") {
			t.Errorf("%s: %s", goos, got)
		}
	}
	if cheminRTK(kit) != cheminRTKPour(kit, runtime.GOOS) {
		t.Error("cheminRTK is not this system's")
	}
	if got := urlRTK("x86_64-pc-windows-msvc"); got != "https://github.com/rtk-ai/rtk/releases/download/"+versionRTK+"/rtk-x86_64-pc-windows-msvc.zip" {
		t.Error(got)
	}
	if got := urlRTK("aarch64-apple-darwin"); got != "https://github.com/rtk-ai/rtk/releases/download/"+versionRTK+"/rtk-aarch64-apple-darwin.tar.gz" {
		t.Error(got)
	}
	// usable: a program there (executable outside Windows); nothing, an empty file or a folder: no
	d := t.TempDir()
	f := filepath.Join(d, "rtk")
	if rtkUtilisable(f) || rtkUtilisable(d) {
		t.Fatal("nothing taken for RTK")
	}
	_ = os.WriteFile(f, nil, 0o755)
	if rtkUtilisable(f) {
		t.Fatal("an empty file taken for RTK")
	}
	_ = os.WriteFile(f, []byte("x"), 0o644)
	_ = os.Chmod(f, 0o644)
	if rtkUtilisable(f) != (runtime.GOOS == "windows") {
		t.Fatal("a file without its execute bit")
	}
	_ = os.Chmod(f, 0o755)
	if !rtkUtilisable(f) {
		t.Fatal("RTK not seen")
	}
}

// The real download (ATHANOR_TEST_RESEAU=1): RTK's archive for this machine, its rtk runs.
func TestTelechargerRTKReseau(t *testing.T) {
	if os.Getenv("ATHANOR_TEST_RESEAU") == "" {
		t.Skip("ATHANOR_TEST_RESEAU not set")
	}
	for _, cible := range []string{"x86_64-unknown-linux-musl", "x86_64-pc-windows-msvc", "aarch64-apple-darwin"} {
		dest := filepath.Join(t.TempDir(), "rtk")
		if err := telechargerRTK(urlRTK(cible), dest); err != nil {
			t.Fatal(cible, err)
		}
		if st, err := os.Stat(dest); err != nil || st.Size() < 100000 {
			t.Fatalf("%s: %v %v", cible, st, err)
		}
	}
}
