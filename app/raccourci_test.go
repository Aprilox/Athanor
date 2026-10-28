//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Linux: athanor.desktop in the folder chosen (a path with spaces and quotes kept as it is), executable,
// with the icon; the same entry in the applications menu; an unknown or relative folder refused.
func TestRaccourciLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux")
	}
	kit := filepath.Join(t.TempDir(), `Mon "kit" $HOME`)
	_ = os.MkdirAll(filepath.Join(kit, "system"), 0o755)
	ecrireEssai(t, exeDuKit(kit), "#!/bin/sh\necho athanor-lance \"$@\"\n")
	_ = os.Chmod(exeDuKit(kit), 0o755)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "donnees"))
	bureau := t.TempDir()
	for _, mauvais := range []string{"relatif/dossier", filepath.Join(bureau, "absent")} {
		if _, err := creerRaccourci(kit, mauvais); err == nil {
			t.Fatalf("%q accepted", mauvais)
		}
	}
	l, err := creerRaccourci(kit, bureau)
	if err != nil {
		t.Fatal(err)
	}
	menu := filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications", "athanor.desktop")
	if len(l) != 2 || l[0] != filepath.Join(bureau, "athanor.desktop") || l[1] != menu {
		t.Fatalf("made: %v", l)
	}
	st, _ := os.Stat(l[0])
	e := lire(t, l[0])
	if st.Mode().Perm()&0o100 == 0 || !strings.Contains(e, "Type=Application") || !strings.Contains(e, "Icon="+filepath.Join(kit, "system", "athanor.png")) || lire(t, menu) != e {
		t.Fatalf("entry (%v):\n%s", st.Mode(), e)
	}
	if b, err := os.ReadFile(filepath.Join(kit, "system", "athanor.png")); err != nil || len(b) < 1000 {
		t.Fatal("no icon")
	}
	// the Exec line, read as the desktops read it, runs the installed Athanor
	var exec1 string
	for _, x := range strings.Split(e, "\n") {
		if strings.HasPrefix(x, "Exec=") {
			exec1 = strings.TrimPrefix(x, "Exec=")
		}
	}
	if got := deEchapperExecDesktop(exec1); got != exeDuKit(kit) {
		t.Fatalf("Exec %q reads %q", exec1, got)
	}
}

// deEchapperExecDesktop: what a desktop reads in a quoted Exec= argument (the reverse of the escaping).
func deEchapperExecDesktop(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, `"`), `"`)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// macOS: an Athanor.app that opens the installed Athanor with its bundle named (its Dock icon), its own
// identifier, replaced when made again; the downloaded Athanor.app, if there, is left alone.
func TestRaccourciMac(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh")
	}
	d := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	exe := filepath.Join(t.TempDir(), "kit d'Athanor", "athanor")
	ecrireEssai(t, exe, "#!/bin/sh\necho \"$ATHANOR_APP_MAC|$*\" > "+trace+"\n")
	_ = os.Chmod(exe, 0o755)
	app, err := creerAppRaccourciMac(d, exe, []byte("icns"))
	if err != nil {
		t.Fatal(err)
	}
	if app != filepath.Join(d, "Athanor.app") || lire(t, filepath.Join(app, "Contents", "Resources", "athanor.icns")) != "icns" {
		t.Fatal("bundle")
	}
	p := lire(t, filepath.Join(app, "Contents", "Info.plist"))
	if !strings.Contains(p, "<string>ai.athanor.app.raccourci</string>") || !strings.Contains(p, "<key>CFBundleExecutable</key><string>athanor</string>") {
		t.Fatalf("plist:\n%s", p)
	}
	c := exec.Command(filepath.Join(app, "Contents", "MacOS", "athanor"), "--un", "deux")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("the launcher: %v %s", err, out)
	}
	reel, _ := filepath.EvalSymlinks(app)
	if got := strings.TrimSpace(lire(t, trace)); got != reel+"|--un deux" {
		t.Fatalf("the launcher ran %q", got)
	}
	if _, err := creerAppRaccourciMac(d, exe, nil); err != nil {
		t.Fatalf("made again: %v", err)
	}
	autre := t.TempDir()
	_ = os.MkdirAll(filepath.Join(autre, "Athanor.app", "Contents", "MacOS"), 0o755)
	if _, err := creerAppRaccourciMac(autre, exe, nil); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("the downloaded Athanor.app replaced: %v", err)
	}
}

// The window: the folder proposed, the shortcut made and remembered; a device of the remote access never
// writes on the computer.
func TestApiRaccourci(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux")
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "donnees"))
	kit := t.TempDir()
	_ = os.MkdirAll(filepath.Join(kit, "system"), 0o755)
	ecrireEssai(t, exeDuKit(kit), "x")
	s := nouveauServeur(Config{Kit: kit})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(m, corps string) (int, map[string]any) {
		r := httptest.NewRequest(m, "/api/raccourci", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	if c, v := appel("GET", ""); c != 200 || v["defaut"] == "" || v["systeme"] != runtime.GOOS {
		t.Fatalf("GET: %d %v", c, v)
	}
	bureau := t.TempDir()
	c, v := appel("POST", `{"dossier":"`+bureau+`"}`)
	if l, _ := v["crees"].([]any); c != 200 || len(l) != 2 {
		t.Fatalf("POST: %d %v", c, v)
	}
	if _, v := appel("GET", ""); len(v["raccourcis"].([]any)) != 2 {
		t.Fatalf("remembered: %v", v)
	}
	r := httptest.NewRequest("POST", "/api/raccourci", nil)
	if !interditDistance(r) || interditDistance(httptest.NewRequest("GET", "/api/raccourci", nil)) {
		t.Fatal("the remote access")
	}
}
