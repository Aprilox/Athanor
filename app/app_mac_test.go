package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBundleMac(t *testing.T) {
	cas := map[string]string{
		"/Applications/Athanor.app/Contents/MacOS/athanor":        "/Applications/Athanor.app",
		"/Users/x/Downloads/Athanor 2.app/Contents/MacOS/athanor": "/Users/x/Downloads/Athanor 2.app",
		"/Users/x/Library/Application Support/Athanor/athanor":    "",
		"/Applications/Athanor.app/Contents/Resources/athanor":    "",
		"/tmp/Contents/MacOS/athanor":                             "",
	}
	for exe, veut := range cas {
		if g := bundleMac(exe); g != veut {
			t.Errorf("%s: %q, want %q", exe, g, veut)
		}
	}
	d := t.TempDir()
	b := filepath.Join(d, "Athanor.app")
	os.MkdirAll(b, 0o755)
	t.Setenv("ATHANOR_APP_MAC", b)
	if bundleAthanorMac() != b {
		t.Fatal("bundle not seen")
	}
	if bundleDurableMac() != b {
		t.Fatal("a bundle in place is durable")
	}
	tr := filepath.Join(d, "AppTranslocation", "X", "d", "Athanor.app")
	os.MkdirAll(tr, 0o755)
	t.Setenv("ATHANOR_APP_MAC", tr)
	if bundleAthanorMac() != tr || bundleDurableMac() != "" {
		t.Fatal("a translocated bundle has its Dock icon, but is not reopened later")
	}
	t.Setenv("ATHANOR_APP_MAC", filepath.Join(d, "Gone.app"))
	if bundleAthanorMac() != "" {
		t.Fatal("a missing bundle is not one")
	}
}

func TestTextesAppMac(t *testing.T) {
	for _, l := range []string{"fr", "en", "es", "de", "it", "pt", ""} {
		x := textesAppMac(l)
		for _, s := range []string{x.ouvrir, x.quitter, x.aucun, x.enCours, x.titreQuitter, x.texteQuitter, x.continuer, x.arreter, x.annuler} {
			if s == "" {
				t.Fatalf("%s: empty text", l)
			}
		}
		if !strings.Contains(x.enCours, "%s") || !strings.Contains(x.titreQuitter, "%s") {
			t.Fatalf("%s: the pilots' names have no place", l)
		}
	}
}

func machoFactice(t *testing.T, f string, cpu uint32, n int) {
	b := make([]byte, n)
	binary.LittleEndian.PutUint32(b, 0xfeedfacf)
	binary.LittleEndian.PutUint32(b[4:], cpu)
	for i := 32; i < n; i++ {
		b[i] = byte(i)
	}
	if err := os.WriteFile(f, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestConstruireAppMac(t *testing.T) {
	src, dist := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "winres"), 0o755)
	os.WriteFile(filepath.Join(src, "winres", "athanor.icns"), []byte("icns"), 0o644)
	machoFactice(t, filepath.Join(dist, "athanor-darwin-arm64"), 0x0100000c, 5000)
	machoFactice(t, filepath.Join(dist, "athanor-darwin-amd64"), 0x01000007, 7000)
	if err := construireAppMac(src, dist, "9.8.7"); err != nil {
		t.Fatal(err)
	}
	c := filepath.Join(dist, "Athanor.app", "Contents")
	plist, _ := os.ReadFile(filepath.Join(c, "Info.plist"))
	for _, m := range []string{"<string>athanor</string>", "<string>9.8.7</string>", idBundleMac} {
		if !strings.Contains(string(plist), m) {
			t.Fatalf("Info.plist without %s", m)
		}
	}
	b, err := os.ReadFile(filepath.Join(c, "MacOS", "athanor"))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(b) != 0xcafebabe || binary.BigEndian.Uint32(b[4:]) != 2 {
		t.Fatal("not a universal executable")
	}
	for i, veut := range []struct {
		cpu  uint32
		size int
		src  string
	}{{0x0100000c, 5000, "athanor-darwin-arm64"}, {0x01000007, 7000, "athanor-darwin-amd64"}} {
		e := b[8+20*i:]
		off, n := binary.BigEndian.Uint32(e[8:]), binary.BigEndian.Uint32(e[12:])
		if binary.BigEndian.Uint32(e) != veut.cpu || int(n) != veut.size || off%(1<<14) != 0 {
			t.Fatalf("slice %d: cpu %x size %d offset %d", i, binary.BigEndian.Uint32(e), n, off)
		}
		orig, _ := os.ReadFile(filepath.Join(dist, veut.src))
		if string(b[off:off+n]) != string(orig) {
			t.Fatalf("slice %d differs", i)
		}
	}
	if st, _ := os.Stat(filepath.Join(c, "MacOS", "athanor")); runtime.GOOS != "windows" && st.Mode()&0o111 == 0 { // Windows keeps no such mode
		t.Fatal("not executable")
	}
}
