package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDeuxInstances (CI job two-instances): two real Athanor executables, each in its own folders, driven as
// their owner drives them (the single-use address of instance.json, as the terminal interface does). B shows
// a 6-digit code; A adds B by its address and that code; B's owner accepts; A reads B's state and projects,
// then orders a pause: the fake pilot of B's project stops at the end of its cycle. A client B never paired
// is refused.
func TestDeuxInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the executable")
	}
	racine := t.TempDir()
	exe := filepath.Join(racine, "athanor")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-ldflags=-s -w", "-o", exe, ".").CombinedOutput(); err != nil {
		sautSiDefender(t, string(out))
		t.Fatalf("go build: %v\n%s", err, out)
	}
	jeu := filepath.Join(racine, "projets", "jeu")
	ecrireFichier(t, filepath.Join(jeu, ".athanor", "PLAN.md"), "# Plan\n\n- [x] M1 : Squelette\n- [ ] M2 : Tranche complète\n", 0o644)

	// one instance: its own copy of the executable (its folder is Athanor's), config, home and caches
	lancer := func(nom string, projets []Projet) *clientTUI {
		t.Helper()
		d := filepath.Join(racine, nom)
		copie := filepath.Join(d, filepath.Base(exe))
		c, _ := os.ReadFile(exe)
		ecrireFichier(t, copie, string(c), 0o755)
		b, _ := json.Marshal(Config{PasVerifierMaj: true, ModeNatif: true, Projets: projets}) // Windows: the kit runs without WSL
		ecrireFichier(t, filepath.Join(d, nomConfig()), string(b), 0o600)
		cache, config := filepath.Join(d, "cache"), filepath.Join(d, "config")
		cmd := exec.Command(copie)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "ATHANOR_TEST=1", "ATHANOR_SANS_FENETRE=1", "ATHANOR_TEST_CODE_FICHIER="+filepath.Join(d, "code.txt"), "LocalAppData="+cache, "LOCALAPPDATA="+cache,
			"APPDATA="+config, "HOME="+filepath.Join(d, "home"), "XDG_CACHE_HOME="+cache, "XDG_CONFIG_HOME="+config)
		var journal bytes.Buffer
		cmd.Stdout, cmd.Stderr = &journal, &journal
		if err := cmd.Start(); err != nil {
			sautSiDefender(t, err.Error())
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			if t.Failed() {
				t.Logf("output of %s:\n%s", nom, journal.String())
			}
		})
		// instance.json (the user's cache folder, wherever the system puts it under this instance's folders)
		var info infoInstance
		for i := 0; i < 300 && info.Port == 0; i++ {
			_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
				if err == nil && !e.IsDir() && e.Name() == "instance.json" {
					if x, err := os.ReadFile(p); err == nil {
						_ = json.Unmarshal(x, &info)
					}
				}
				return nil
			})
			time.Sleep(100 * time.Millisecond)
		}
		if info.Port == 0 {
			t.Fatalf("%s never wrote instance.json", nom)
		}
		req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(info.Port)+"/api/instance/terminal", nil)
		req.Header.Set("X-Athanor-Instance", info.Jeton)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var rep struct{ Lien string }
		_ = json.NewDecoder(r.Body).Decode(&rep)
		r.Body.Close()
		cli, err := nouveauClientTUI(rep.Lien)
		if err != nil {
			t.Fatalf("%s: single-use address %q: %v", nom, rep.Lien, err)
		}
		return cli
	}
	a := lancer("a", nil)
	b := lancer("b", []Projet{{Nom: "jeu", Dossier: jeu}})

	// B opens a code (Settings › MCP clients › Connect); the CI reads it through the test hook
	// (ATHANOR_TEST_CODE_FICHIER); A adds B by its address and that code
	var clients struct {
		Code     string
		Demandes []demandeClient
	}
	if err := b.appel("POST", "clients", map[string]string{"action": "code"}, &clients); err != nil {
		t.Fatalf("B's code: %v", err)
	}
	lu, _ := os.ReadFile(filepath.Join(racine, "b", "code.txt"))
	if len(lu) != 6 || string(lu) != clients.Code {
		t.Fatalf("code from the hook %q, shown %q", lu, clients.Code)
	}
	clients.Code = string(lu)
	var faux struct{}
	mauvais := map[bool]string{true: "111111", false: "000000"}[clients.Code == "000000"]
	if err := a.appel("POST", "distants", map[string]string{"action": "ajouter", "url": b.base, "code": mauvais}, &faux); err == nil {
		t.Fatal("A added B with a wrong code")
	}
	if err := a.appel("POST", "distants", map[string]string{"action": "ajouter", "url": b.base, "code": clients.Code}, &faux); err != nil {
		t.Fatalf("A adds B: %v", err)
	}
	// B's owner accepts, with the project and the tools a hub needs to follow and drive it
	for i := 0; i < 100 && len(clients.Demandes) == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := b.appel("GET", "clients", nil, &clients); err != nil {
			t.Fatal(err)
		}
	}
	if len(clients.Demandes) != 1 || clients.Demandes[0].Sorte != "athanor" {
		t.Fatalf("B's requests = %+v, want one from an Athanor", clients.Demandes)
	}
	if err := b.appel("POST", "clients", map[string]any{"action": "accepter", "ref": clients.Demandes[0].Ref,
		"projets": []string{"jeu"}, "outils": []string{"etat", "projets", "pilote"}}, &faux); err != nil {
		t.Fatalf("B accepts: %v", err)
	}

	// A reads B's state: online, with Dev, its tools, and its project
	type distantVu struct {
		ID, Etat        string
		Modules, Outils []string
	}
	var vu distantVu
	for i := 0; i < 200 && vu.Etat != "en_ligne"; i++ {
		time.Sleep(100 * time.Millisecond)
		var inst struct{ Distants struct{ Liste []distantVu } }
		if err := a.appel("GET", "instances", nil, &inst); err != nil {
			t.Fatal(err)
		}
		if len(inst.Distants.Liste) == 1 {
			vu = inst.Distants.Liste[0]
		}
	}
	if vu.Etat != "en_ligne" || !slices.Contains(vu.Modules, "dev") || !slices.Contains(vu.Outils, "pilote") || slices.Contains(vu.Outils, "ajouter_demande") {
		t.Fatalf("A sees B as %+v", vu)
	}

	// B's fake pilot: one cycle every 50 ms, it stops at the end of the cycle once asked to
	arret := make(chan struct{})
	go func() {
		for i := 0; i < 1200; i++ {
			time.Sleep(50 * time.Millisecond) // the cycle's work
			if _, err := os.Stat(filepath.Join(jeu, ".athanor", "STOP")); err == nil {
				close(arret)
				return
			}
		}
	}()
	// the pause ordered from A (B's project is known to A once its list was read: retried until then)
	var err error
	for i := 0; i < 100; i++ {
		err = a.appel("POST", "distants", map[string]string{"action": "pilote", "id": vu.ID, "projet": "jeu", "ordre": "pause_fin_cycle"}, &faux)
		if err == nil || err.Error() != "Unknown remote project." {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("pause from A: %v", err)
	}
	select {
	case <-arret:
	case <-time.After(20 * time.Second):
		t.Fatal("B's pilot never got the stop at the end of its cycle")
	}
	// an order B did not grant is refused by A before reaching B
	if err := a.appel("POST", "distants", map[string]string{"action": "pilote", "id": vu.ID, "projet": "inconnu", "ordre": "pause_fin_cycle"}, &faux); err == nil {
		t.Error("A relayed an order for a project B does not have")
	}

	// a client B never paired: refused on B's MCP, with no token, an invented one or a session of the window
	for nom, entete := range map[string]string{"sans jeton": "", "jeton inventé": "Bearer " + aleatoire(24), "Basic": "Basic YTpi"} {
		req, _ := http.NewRequest("POST", b.base+"/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pilote","arguments":{"projet":"jeu","action":"demarrer"}}}`))
		req.Header.Set("Content-Type", "application/json")
		if entete != "" {
			req.Header.Set("Authorization", entete)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusUnauthorized && r.StatusCode != http.StatusForbidden {
			t.Errorf("%s: B's MCP answered %d", nom, r.StatusCode)
		}
	}
	if err := b.appel("POST", "mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, &faux); err == nil {
		t.Error("the window's session opened B's MCP")
	}
}

// TestCodeTestInactif: the code file of the CI is written only under ATHANOR_TEST=1.
func TestCodeTestInactif(t *testing.T) {
	f := filepath.Join(t.TempDir(), "code.txt")
	t.Setenv("ATHANOR_TEST_CODE_FICHIER", f)
	for _, v := range []string{"", "0", "true"} {
		t.Setenv("ATHANOR_TEST", v)
		ecrireCodeTest("123456")
		if _, err := os.Stat(f); err == nil {
			t.Fatalf("code written with ATHANOR_TEST=%q", v)
		}
	}
	t.Setenv("ATHANOR_TEST", "1")
	ecrireCodeTest("123456")
	if b, _ := os.ReadFile(f); string(b) != "123456" {
		t.Fatalf("code file = %q under ATHANOR_TEST=1", b)
	}
}
