package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// M196: a folder pointed to the home conversation is checked on the Go side: a full path, no "..", no link
// nor junction, never Athanor's folders, a secrets folder, a system folder, a drive or the home folder.
func TestDossierChatPortee(t *testing.T) {
	kit, _ := filepath.EvalSymlinks(t.TempDir())
	k := Kit{Racine: kit}
	base, _ := filepath.EvalSymlinks(t.TempDir()) // macOS: /var is a link to /private/var
	ok := filepath.Join(base, "site")
	_ = os.MkdirAll(filepath.Join(ok, "sous"), 0o755)
	_ = os.MkdirAll(filepath.Join(base, ".env"), 0o755)
	ecrire(t, filepath.Join(base, "f.txt"), "x")
	if c, err := verifierDossierChat(k, ok+string(filepath.Separator)); err != nil || c != ok {
		t.Fatalf("a good folder: %q %v", c, err)
	}
	home, _ := os.UserHomeDir()
	systeme := "/etc"
	if runtime.GOOS == "windows" {
		systeme = os.Getenv("SystemRoot")
	}
	cas := []struct {
		chemin string
		err    error
	}{
		{"site", errDossierAbsolu},
		{"", errDossierAbsolu},
		{filepath.Join(ok, "sous") + string(filepath.Separator) + ".." + string(filepath.Separator) + "sous", errDossierPoints},
		{filepath.Join(base, "absent"), errDossierAbsent},
		{filepath.Join(base, "f.txt"), errDossierAbsent},
		{filepath.Join(base, ".env"), errDossierSecret},
		{kit, errDossierAthanor},
		{filepath.Join(kit, "logs"), errDossierAthanor},
		{filepath.Dir(kit), errDossierAthanor}, // a parent of the kit holds it
		{systeme, errDossierSysteme},
		{filepath.VolumeName(base) + string(filepath.Separator), errDossierSysteme},
		{home, errDossierSysteme},
	}
	_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	for _, c := range cas {
		if _, err := verifierDossierChat(k, c.chemin); !errors.Is(err, c.err) {
			t.Errorf("%q: %v, want %v", c.chemin, err, c.err)
		}
	}
	// a symbolic link (or, on Windows without the right to create one, a junction), and a folder under it
	lien := filepath.Join(base, "lien")
	if err := os.Symlink(ok, lien); err != nil {
		if runtime.GOOS != "windows" || exec.Command("cmd", "/c", "mklink", "/J", lien, ok).Run() != nil {
			t.Skip("no link can be created here")
		}
	}
	for _, c := range []string{lien, filepath.Join(lien, "sous")} {
		if _, err := verifierDossierChat(k, c); !errors.Is(err, errDossierLien) {
			t.Errorf("link %q: %v", c, err)
		}
	}
}

// M196: the files guard of the chat: a folder read only cannot be written, a read and write one can, nothing
// else is reached, and a secrets file is never read, even inside a pointed folder.
func TestGardeDossiersChat(t *testing.T) {
	racine := t.TempDir()
	projet, lecture, ecriture := filepath.Join(racine, "accueil"), filepath.Join(racine, "docs"), filepath.Join(racine, "site")
	args := []string{"--sans-secrets", projet, ecriture, "--lecture=" + lecture}
	appel := func(outil, chemin string) string {
		b, _ := json.Marshal(map[string]any{"tool_name": outil, "tool_input": map[string]any{"file_path": chemin}, "cwd": projet})
		return gardeFichiers(args, b)
	}
	for _, c := range []struct {
		outil, chemin string
		permis        bool
	}{
		{"Read", filepath.Join(lecture, "a.md"), true},
		{"Write", filepath.Join(lecture, "a.md"), false},
		{"Read", filepath.Join(ecriture, "index.html"), true},
		{"Edit", filepath.Join(ecriture, "index.html"), true},
		{"Read", filepath.Join(racine, "autre", "x.md"), false},
		{"Read", filepath.Join(lecture, ".env"), false},
		{"Read", filepath.Join(ecriture, ".env.local"), false},
		{"Read", filepath.Join(ecriture, ".athanor", "secrets.env"), false},
		{"Read", filepath.Join(ecriture, ".athanor", "secrets.env.ancien"), false},
	} {
		if g := appel(c.outil, c.chemin); (g == "") != c.permis {
			t.Errorf("%s %s: %q", c.outil, c.chemin, g)
		}
	}
	// M240l5: a search (Grep's glob and path, Glob's pattern) aimed at a secrets file or at .athanor is refused
	recherche := func(outil string, entree map[string]any) string {
		b, _ := json.Marshal(map[string]any{"tool_name": outil, "tool_input": entree, "cwd": projet})
		return gardeFichiers(args, b)
	}
	for _, c := range []struct {
		outil  string
		entree map[string]any
		permis bool
	}{
		{"Grep", map[string]any{"pattern": ".", "path": ".athanor", "glob": "secrets.env"}, false},
		{"Grep", map[string]any{"pattern": ".", "glob": "secrets.*"}, false},
		{"Grep", map[string]any{"pattern": "KEY", "glob": "*.env*"}, false},
		{"Grep", map[string]any{"pattern": "KEY", "glob": "**/{a,.e*}"}, false},
		{"Grep", map[string]any{"pattern": "x", "path": filepath.Join(projet, ".athanor")}, false},
		{"Grep", map[string]any{"pattern": "x", "path": filepath.Join(projet, ".ENV")}, false},
		{"Glob", map[string]any{"pattern": "**/.env*"}, false},
		{"Glob", map[string]any{"pattern": ".ath*/**"}, false},
		{"Glob", map[string]any{"pattern": "**/secret?.env"}, false},
		{"Grep", map[string]any{"pattern": "secrets.env|\\.env", "glob": "*.go"}, true}, // Grep's pattern is the text searched
		{"Grep", map[string]any{"pattern": "x", "glob": "**/*.md", "path": ecriture}, true},
		{"Glob", map[string]any{"pattern": "**/*"}, true},
		{"Glob", map[string]any{"pattern": "src/*.{js,ts}"}, true},
	} {
		if g := recherche(c.outil, c.entree); (g == "") != c.permis {
			t.Errorf("%s %v: %q", c.outil, c.entree, g)
		}
	}
	// the settings of the session: the guard's folders, and the deny rules of the secrets files
	sortie := filepath.Join(racine, "reglages.json")
	if err := reglagesChatInternet(projet, sortie, false, DossierChat{Chemin: lecture}, DossierChat{Chemin: ecriture, Ecriture: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sortie)
	r := strings.ReplaceAll(string(b), `\\`, "/")
	for _, x := range []string{"--sans-secrets", "--lecture=" + filepath.ToSlash(lecture), filepath.ToSlash(ecriture), "Read(**/.env)", "Read(**/secrets.env)", "Read(**/secrets.env.*)"} {
		if !strings.Contains(r, x) {
			t.Errorf("settings without %q: %s", x, r)
		}
	}
}

// M196: the folders of the home conversation: refused ones never stored, kept with the conversation, gone with a
// new one; a turn reads them, writes only with Writing on and a folder read and write.
func TestHubChatDossiers(t *testing.T) {
	_, _, kit, appel := serveurChatEssai(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	site := filepath.Join(base, "site")
	_ = os.MkdirAll(site, 0o755)
	if c, v := appel("POST", "/api/hub/chat/pouvoirs", map[string]any{"dossiers": []map[string]any{{"chemin": kit}}}); c != 400 || v["erreur_key"] != errDossierAthanor.Error() {
		t.Fatalf("the kit pointed: %d %v", c, v)
	}
	if c, v := appel("POST", "/api/hub/chat/pouvoirs", map[string]any{"dossiers": []map[string]any{{"chemin": site, "ecriture": true}, {"chemin": site + string(filepath.Separator)}}}); c != 200 {
		t.Fatalf("a good folder: %d %v", c, v)
	}
	k := Kit{Racine: kit}
	if l := lireConversation(k, projetAccueil(k)).Pouvoirs.Dossiers; len(l) != 1 || l[0].Chemin != site || !l[0].Ecriture {
		t.Fatalf("stored: %+v", l)
	}
	if runtime.GOOS != "windows" { // the fake kit is a shell script
		trace := filepath.Join(kit, "trace")
		ecrire(t, filepath.Join(kit, "athanor"), `#!/bin/sh
shift
c="$3/logs/chat/$5"
echo "mode=$6" >> `+trace+`
cat "$c.dossiers" >> `+trace+`
grep -q "read and write" "$c.prompt" && echo ecrit >> `+trace+`
echo '{"type":"result","result":"Fait.","total_cost_usd":0.01,"session_id":"s1"}' > "$c.jsonl"
`)
		_ = os.Chmod(filepath.Join(kit, "athanor"), 0o755)
		tour := func(n int) string {
			t.Helper()
			_ = os.Remove(trace)
			if c, v := appel("POST", "/api/hub/chat", map[string]any{"message": "Corrige le titre"}); c != 200 {
				t.Fatalf("send: %d %v", c, v)
			}
			for i := 0; i < 250; i++ {
				chatMu.Lock()
				fini := chatsActifs[projetAccueil(k).Dossier] == nil
				chatMu.Unlock()
				if fini && len(lireConversation(k, projetAccueil(k)).Messages) >= n {
					b, _ := os.ReadFile(trace)
					return string(b)
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("no answer")
			return ""
		}
		// Writing off: the folder is given read only
		if b := tour(2); !strings.Contains(b, "mode=question") || !strings.Contains(b, `"ecriture":false`) || strings.Contains(b, "ecrit\n") {
			t.Fatalf("Writing off: %s", b)
		}
		appel("POST", "/api/hub/chat/pouvoirs", map[string]any{"ecriture": true, "dossiers": []map[string]any{{"chemin": site, "ecriture": true}}})
		if b := tour(4); !strings.Contains(b, "mode=modif") || !strings.Contains(b, `"ecriture":true`) || !strings.Contains(b, "ecrit\n") {
			t.Fatalf("Writing on: %s", b)
		}
		// the folder gone since: the turn is refused, nothing starts
		_ = os.RemoveAll(site)
		if c, v := appel("POST", "/api/hub/chat", map[string]any{"message": "Encore"}); c != 400 || v["erreur_key"] != errDossierAbsent.Error() {
			t.Fatalf("a folder gone: %d %v", c, v)
		}
	}
	appel("POST", "/api/hub/chat/effacer", nil)
	if l := lireConversation(k, projetAccueil(k)).Pouvoirs.Dossiers; len(l) != 0 {
		t.Fatalf("a new conversation keeps the folders: %+v", l)
	}
}

// M196: files attached to the home conversation: images, PDF and text only, 20 MB at most, into its own
// attachments, then named in the message.
func TestHubChatPieces(t *testing.T) {
	s, _, _, _ := serveurChatEssai(t)
	mux := http.NewServeMux()
	s.routes(mux)
	envoi := func(nom string, corps string) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/api/hub/chat/piece", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.Header.Set("X-Nom-Fichier", url.QueryEscape(nom))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	if c, v := envoi("outil.exe", "MZ"); c != 400 || !strings.Contains(v["erreur_key"].(string), "not accepted") {
		t.Fatalf("an exe: %d %v", c, v)
	}
	if c, v := envoi(".env", "A=1"); c != 400 {
		t.Fatalf("a .env: %d %v", c, v)
	}
	if c, v := envoi("gros.pdf", strings.Repeat("x", taillePieceAccueil+1)); c != 400 || v["erreur_key"] != errPieceAccueilTropGrosse.Error() {
		t.Fatalf("too large: %d %v", c, v)
	}
	c, v := envoi("capture écran.png", "png")
	if c != 200 || v["chemin"] != ".athanor/attachments/chat/capture écran.png" {
		t.Fatalf("an image: %d %v", c, v)
	}
	p := projetAccueil(s.kitCourant())
	if b, _ := os.ReadFile(filepath.Join(p.Atelier(), "attachments", "chat", "capture écran.png")); string(b) != "png" {
		t.Fatalf("file: %q", b)
	}
	if l, err := piecesChat(p, []string{v["chemin"].(string)}); err != nil || len(l) != 1 {
		t.Fatalf("named in the message: %v %v", l, err)
	}
}

// TestAliasMac: macOS's own /var, /tmp and /etc links are followed as the system does; nothing else is.
func TestAliasMac(t *testing.T) {
	for c, mac := range map[string]string{"/var/folders/x": "/private/var/folders/x", "/tmp": "/private/tmp", "/etc/hosts": "/private/etc/hosts",
		"/variable/x": "/variable/x", "/Users/a/var": "/Users/a/var"} {
		want := c
		if runtime.GOOS == "darwin" {
			want = mac
		}
		if g := sansAliasMac(c); g != want {
			t.Errorf("%s: %s, want %s", c, g, want)
		}
	}
}
