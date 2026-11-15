package main

import (
	"os"
	"path/filepath"
)

// Tests never see the real data folder: run without the check's fake HOME, `go test` read the owner's
// vault and rewrote %LocalAppData%\Athanor\athanor-config.json (cheminConfig under tests).
// shortcut: the temporary HOME is left behind, add a TestMain if it ever matters.
func init() {
	if os.Getenv("ATHANOR_FAUX_HOME") != "" { // the check already gives each test process its own HOME
		return
	}
	h, err := os.MkdirTemp("", "athanor-tests-home-")
	if err != nil {
		panic("tests without an isolated HOME: " + err.Error())
	}
	for _, d := range []string{"AppData/Local", "AppData/Roaming", ".config", ".local/share", ".cache"} {
		if err := os.MkdirAll(filepath.Join(h, d), 0o700); err != nil {
			panic(err)
		}
	}
	// the tests that commit need an identity, as the check's .gitconfig gives
	_ = os.WriteFile(filepath.Join(h, ".gitconfig"), []byte("[user]\n\tname = essai\n\temail = essai@example.invalid\n[init]\n\tdefaultBranch = main\n"), 0o600)
	for v, d := range map[string]string{"HOME": "", "USERPROFILE": "", "LOCALAPPDATA": "AppData/Local", "APPDATA": "AppData/Roaming",
		"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share", "XDG_CACHE_HOME": ".cache"} {
		os.Setenv(v, filepath.Join(h, d))
	}
}
