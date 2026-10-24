package main

import (
	"archive/tar"
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
	for _, c := range [][3]string{{"linux", "amd64", "x86_64-unknown-linux-musl"}, {"darwin", "arm64", "aarch64-apple-darwin"}, {"windows", "amd64", ""}} {
		if got := cibleRTK(c[0], c[1]); got != c[2] {
			t.Errorf("%s/%s: %q", c[0], c[1], got)
		}
	}
}

// The real download (ATHANOR_TEST_RESEAU=1): RTK's archive for this machine, its rtk runs.
func TestTelechargerRTKReseau(t *testing.T) {
	if os.Getenv("ATHANOR_TEST_RESEAU") == "" {
		t.Skip("ATHANOR_TEST_RESEAU not set")
	}
	dest := filepath.Join(t.TempDir(), "rtk")
	if err := telechargerRTK("https://github.com/rtk-ai/rtk/releases/download/"+versionRTK+"/rtk-x86_64-unknown-linux-musl.tar.gz", dest); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dest); err != nil || st.Size() < 100000 {
		t.Fatalf("%v %v", st, err)
	}
}
