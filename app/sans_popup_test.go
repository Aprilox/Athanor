package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// No test may make the computer ask its owner anything (firewall, UAC, Keychain): everything stays on 127.0.0.1,
// without elevation (owner's remark of 2026-11-13 23h21).

// reseauReelPermis: whether a test may open a socket outside the loopback (the real mDNS socket). Windows' firewall
// and macOS ask the owner about such a socket; Linux does not, and the CI runners set ATHANOR_TEST_RESEAU=1.
func reseauReelPermis() bool {
	return runtime.GOOS == "linux" || os.Getenv("ATHANOR_TEST_RESEAU") == "1"
}

// The real Keychain (macOS) or Secret Service (Linux) may show an access or unlock prompt: the in-process tests get
// a fake keystore whenever this system has a real one (DPAPI, on Windows, never asks). ATHANOR_TEST_TROUSSEAU_REEL=1
// (the CI runners) keeps the real one.
func init() {
	if runtime.GOOS != "windows" && os.Getenv("ATHANOR_TEST_TROUSSEAU_REEL") != "1" && enveloppeSystemeLocale() != nil {
		faux := nouveauFauxSysteme()
		systemeCoffre = func() enveloppeSysteme { return faux }
	}
}

// TestSansPopup reads every Go file, tests included: a listener is on 127.0.0.1 (or is ecouterLocal itself), the
// exceptions are listed here; no test elevates (UAC, sudo, pkexec) nor touches the real keystore unguarded.
func TestSansPopup(t *testing.T) {
	fichiers, _ := filepath.Glob("*.go")
	ecoute := regexp.MustCompile(`\bnet\.Listen\w*\(|ListenAndServe\w*\(|\.ListenPacket\(|\.Listen\(ctx|ListenConfig\{|httptest\.NewUnstartedServer\(|listenaddress=`)
	// the explicit exceptions: file -> what its line holds
	exceptions := map[string]string{
		"hub_mdns.go":                 "net.ListenMulticastUDP(", // mDNS, off by default, the owner turns it on
		"outil_win_unreal_windows.go": "listenaddress=0.0.0.0",   // the WSL relay for Unreal, run and elevated by the owner
	}
	eleve := regexp.MustCompile(`executerEleve\(|relancerEleveJournal\(|"runas"|"autoriser-admin"|"relais-unreal"|"reseau-unreal"|"pkexec"|"apt(-get)?"|"--privileged"|"netsh(\.exe)?"|exec\.Command\("sudo"|"security", "(add|find)`)
	sudoPermis := map[string]bool{"deploiements_k3s_test.go": true} // TestK3sReel: the k3s workflow only (ATHANOR_TEST_K3S=1), sudo -n never asks
	vus := map[string]bool{}
	for _, f := range fichiers {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		test := strings.HasSuffix(f, "_test.go")
		dans := false // inside func ecouterLocal (main.go)
		for i, ligne := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ligne, "func ") {
				dans = f == "main.go" && strings.HasPrefix(ligne, "func ecouterLocal(")
			}
			if f == "sans_popup_test.go" || strings.Contains(ligne, "regexp.MustCompile(") || strings.Contains(ligne, "strings.Contains(ligne, ") ||
				strings.HasPrefix(strings.TrimSpace(ligne), "//") {
				continue // this test, the patterns of TestI7Listeners, comments
			}
			if ecoute.MatchString(ligne) {
				switch {
				case strings.Contains(ligne, `"127.0.0.1:`) || (strings.Contains(ligne, `"127.0.0.1"`) && strings.Contains(ligne, "Listen")):
				case dans:
				case exceptions[f] != "" && strings.Contains(ligne, exceptions[f]):
					vus[f] = true
				default:
					t.Errorf("%s:%d listens outside 127.0.0.1: %s", f, i+1, strings.TrimSpace(ligne))
				}
			}
			if test && eleve.MatchString(ligne) {
				if m := eleve.FindString(ligne); !(m == `exec.Command("sudo"` && sudoPermis[f]) {
					t.Errorf("%s:%d a test asks for rights or the real keystore (%s): %s", f, i+1, m, strings.TrimSpace(ligne))
				}
			}
		}
	}
	for f := range exceptions {
		if !vus[f] {
			t.Errorf("exception %s no longer found: remove it from the list", f)
		}
	}
	// the one test of the real keystore stays guarded
	b, _ := os.ReadFile("coffre_systeme_test.go")
	if !strings.Contains(string(b), `os.Getenv("ATHANOR_TEST_TROUSSEAU_REEL")`) {
		t.Error("TestCoffreSystemeReel no longer guarded by ATHANOR_TEST_TROUSSEAU_REEL")
	}
}
