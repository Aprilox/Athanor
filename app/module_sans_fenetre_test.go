package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestModuleSansFenetre: the real executable as a module without a window (athanor module servers): only
// 127.0.0.1, no Dev page, paired from its terminal (athanor module appairer), then a hub relays a servers
// request and an envoyer_bus to it, logged on both sides, I6 checked at the module for a prod server.
func TestModuleSansFenetre(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the executable")
	}
	// not t.TempDir: on a loaded Windows the killed module's folder stays held longer than the 2 s testing
	// retries, which failed this test twice in a full check (M311b, M311d) though it checks nothing of it
	kit, err := os.MkdirTemp("", "TestModuleSansFenetre")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 100 && os.RemoveAll(kit) != nil; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	})
	exe := filepath.Join(kit, "athanor")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-ldflags=-s -w", "-o", exe, ".").CombinedOutput(); err != nil {
		sautSiDefender(t, string(out))
		t.Fatalf("go build: %v\n%s", err, out)
	}
	b, _ := json.Marshal(Config{PasVerifierMaj: true})
	ecrireFichier(t, filepath.Join(kit, nomConfig()), string(b), 0o600)
	local, config := filepath.Join(kit, "local"), filepath.Join(kit, "config")
	annonces := config
	if runtime.GOOS == "darwin" { // os.UserConfigDir there: HOME/Library/Application Support, never XDG
		annonces = filepath.Join(kit, "home", "Library", "Application Support")
	}
	// a prod server in the module's inventory (both places Athanor's folder can be, next to the exe or local)
	inv, _ := json.Marshal(fichierServeurs{Version: 1, Serveurs: []ServeurInventaire{{ID: "abc123", Nom: "vps-prod", Hote: "192.0.2.10", Port: 22,
		Utilisateur: "deploy", Roles: []string{"prod"}, Etat: "ok", Cree: time.Now()}}})
	for _, d := range []string{kit, filepath.Join(local, "Athanor")} {
		ecrireFichier(t, filepath.Join(d, "modules", "servers", "serveurs.json"), string(inv), 0o600)
	}
	env := append(os.Environ(), "ATHANOR_TEST=1", "LocalAppData="+local, "LOCALAPPDATA="+local, "APPDATA="+config,
		"HOME="+filepath.Join(kit, "home"), "XDG_CACHE_HOME="+filepath.Join(kit, "cache"), "XDG_CONFIG_HOME="+config)
	module := exec.Command(exe, "module", "servers", "--nom", "vps-test")
	module.Dir, module.Env = kit, env
	if err := module.Start(); err != nil {
		sautSiDefender(t, err.Error())
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = module.Process.Kill(); _ = module.Wait() })

	// its announcement: its name, its port, the Servers module and no Dev
	var a Annonce
	for i := 0; i < 300 && a.Port == 0; i++ {
		f, _ := filepath.Glob(filepath.Join(annonces, "athanor-instances", "*.json"))
		if len(f) == 1 {
			if c, err := os.ReadFile(f[0]); err == nil {
				_ = json.Unmarshal(c, &a)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if a.Port == 0 {
		t.Fatal("the module never announced itself")
	}
	if a.Nom != "vps-test" || !slices.Contains(a.Modules, "servers") || slices.Contains(a.Modules, "dev") || slices.Contains(a.Modules, "missions") {
		t.Fatalf("announcement = %+v, want vps-test with servers, without dev nor missions", a)
	}
	// I7: 127.0.0.1 only, never this computer's network address
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(a.Port), 2*time.Second); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	if ips, _ := net.InterfaceAddrs(); len(ips) > 0 {
		for _, x := range ips {
			if n, ok := x.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				if c, err := net.DialTimeout("tcp", n.IP.String()+":"+strconv.Itoa(a.Port), time.Second); err == nil {
					c.Close()
					t.Errorf("the module answers on %s", n.IP)
				}
			}
		}
	}

	// the terminal pairing: the code, then the owner's yes typed in the same terminal
	appairer := exec.Command(exe, "module", "appairer")
	appairer.Dir, appairer.Env = kit, env
	entree, _ := appairer.StdinPipe()
	sortie, _ := appairer.StdoutPipe()
	if err := appairer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appairer.Process.Kill() })
	var mu sync.Mutex
	var lu bytes.Buffer
	go func() {
		tampon := make([]byte, 512)
		for {
			n, err := sortie.Read(tampon)
			mu.Lock()
			lu.Write(tampon[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	attendreTexte := func(motif *regexp.Regexp) []string {
		t.Helper()
		for i := 0; i < 300; i++ {
			mu.Lock()
			m := motif.FindStringSubmatch(lu.String())
			mu.Unlock()
			if m != nil {
				return m
			}
			time.Sleep(100 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("terminal never showed %s; it shows:\n%s", motif, lu.String())
		return nil
	}
	code := attendreTexte(regexp.MustCompile(`Pairing code: (\d{6})`))[1]

	// the hub (in this process): it adds the module by its address and that code
	t.Setenv("APPDATA", filepath.Join(t.TempDir(), "appdata"))
	hub := serveurCoffreTest(t)
	ancien := pauseStatutDistant
	pauseStatutDistant = 20 * time.Millisecond
	t.Cleanup(func() { pauseStatutDistant = ancien })
	for _, f := range []string{cheminClients(), cheminDistants()} {
		_ = os.Remove(f)
		t.Cleanup(func() { _ = os.Remove(f) })
	}
	if err := hub.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := hub.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := hub.ajouterDistant("http://127.0.0.1:"+strconv.Itoa(a.Port), code); err != nil {
		t.Fatal(err)
	}
	attendreTexte(regexp.MustCompile(`asks to pair with this module\. Accept\? \[y/N\]`))
	_, _ = io.WriteString(entree, "y\n")
	if err := appairer.Wait(); err != nil {
		t.Fatalf("athanor module appairer: %v\n%s", err, lu.String())
	}
	var d Distant
	for i := 0; i < 300 && d.ID == ""; i++ {
		m := hub.dst()
		m.mu.Lock()
		if len(m.liste) == 1 {
			d = m.liste[0]
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if d.ID == "" {
		t.Fatal("the hub never kept the module")
	}
	hub.rafraichirDistant(d.ID)
	m := hub.dst()
	m.mu.Lock()
	d, etat := m.liste[0], m.etats[d.ID]
	m.mu.Unlock()
	if etat != "en_ligne" || !slices.Contains(d.Modules, "servers") || slices.Contains(d.Modules, "dev") || !slices.Contains(d.Outils, "envoyer_bus") {
		t.Fatalf("remote seen as %s, modules %v, tools %v", etat, d.Modules, d.Outils)
	}

	// the hub's bus relays to the module: its servers, a sheet, then a refresh that waits for the module's owner (I6)
	vers := "distant-" + d.ID
	rep := hub.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "serveurs"})
	if rep.Etat != "ok" || !strings.Contains(rep.Donnees["texte"].(string), "vps-prod") {
		t.Fatalf("servers through the bus = %+v", rep)
	}
	rep = hub.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "envoyer_bus", Donnees: map[string]any{"action": "fiche", "donnees": map[string]any{"serveur": "abc123"}}})
	if rep.Etat != "ok" || !strings.Contains(rep.Donnees["texte"].(string), `"etat": "ok"`) {
		t.Fatalf("fiche through the bus = %+v", rep)
	}
	rep = hub.envoyerBus(MessageHub{De: "dev", Vers: vers, Action: "envoyer_bus", Donnees: map[string]any{"action": "rafraichir_fiche", "donnees": map[string]any{"serveur": "abc123"}}})
	if rep.Etat != "ok" || !strings.Contains(rep.Donnees["texte"].(string), `"etat": "attente"`) {
		t.Fatalf("a prod refresh must wait for the module's owner, got %+v", rep)
	}

	// logged on both sides
	journal := func(dossier string) string {
		var tout strings.Builder
		_ = filepath.WalkDir(dossier, func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() && strings.Contains(filepath.ToSlash(p), "hub/journal/") {
				c, _ := os.ReadFile(p)
				tout.Write(c)
			}
			return nil
		})
		return tout.String()
	}
	if j := journal(dossierAthanor()); !strings.Contains(j, "envoyer_bus") || !strings.Contains(j, vers) {
		t.Error("the hub's audit log misses the bus request")
	}
	jm := journal(kit)
	for _, x := range []string{"client:", "rafraichir_fiche", "module-decider"} {
		if !strings.Contains(jm, x) {
			t.Errorf("the module's audit log misses %q", x)
		}
	}
}
