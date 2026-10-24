package main

// Athanor's folder (the "kit"): everything that is Athanor's own and changes — settings and token (.env),
// logs and the pilots' private folders (logs/), the tools Claude installs (tools/), the Skills & MCP library
// (extensions/), the repairs made on this PC (local/) — and the copies it runs: its own executable, the
// Linux one of the WSL mode, the texts for Claude (template/), its sources (app/, for "Rebuild Athanor").
//
// Since 2.26 the executable is a single file that can be anywhere, and creates nothing next to itself: its
// folder is the user's data folder (%LOCALAPPDATA%\Athanor, ~/.local/share/athanor, ~/Library/Application
// Support/Athanor), created only once the user accepted (premierLancement). The copy of that folder runs
// (the downloaded file only installs or updates it: embarque.go).
//
// An older installation (a kit folder with its athanor-config*.json next to the executable) keeps its
// folder as it is: nothing moves.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// nomConfig: athanor-config.json (Windows), athanor-config-linux.json, athanor-config-darwin.json.
func nomConfig() string {
	if runtime.GOOS == "windows" {
		return "athanor-config.json"
	}
	return "athanor-config-" + runtime.GOOS + ".json"
}

// dossierAthanorDefaut: the user's data folder for Athanor on this system.
func dossierAthanorDefaut() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LocalAppData"); d != "" {
			return filepath.Join(d, "Athanor")
		}
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, "Athanor")
		}
	}
	return dossierDonneesApp()
}

// dossierExecutable: the folder of the running executable.
func dossierExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

// installationAncienne: the executable sits in a kit folder of an older version (its configuration
// file next to it, or its .env and template/): that folder stays Athanor's folder.
func installationAncienne(d string) bool {
	if d == "" {
		return false
	}
	for _, n := range []string{nomConfig(), "athanor-config.json", "autopilote-config.json"} {
		if st, err := os.Stat(filepath.Join(d, n)); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	_, errEnv := os.Stat(filepath.Join(d, ".env"))
	return errEnv == nil && estDossierKit(d)
}

// dossierAthanor: Athanor's folder for the application: the older installation the executable sits in,
// else the user's data folder (it may not exist yet: premierLancement).
func dossierAthanor() string {
	if d := dossierExecutable(); installationAncienne(d) {
		return d
	}
	return dossierAthanorDefaut()
}

// versionKit: the version of Athanor whose files a folder holds ("" when unknown).
func versionKit(d string) string {
	b, err := os.ReadFile(filepath.Join(d, "system", "version.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// versionPlusRecente: a newer than b ("2.25.0" > "2.24.29"; an empty b is older than anything).
func versionPlusRecente(a, b string) bool {
	if b == "" {
		return a != ""
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// texteConsentement: what the first launch says before anything is written (English or French).
func texteConsentement(d string) (titre, texte string) {
	if langueSysteme() == "fr" {
		return "Athanor — premier lancement", fmt.Sprintf("Athanor range tout ce qu'il garde (sa copie, ses réglages, ses journaux, les outils qu'il installe) dans un dossier à lui :\n\n    %s\n\nAilleurs, il n'écrit que le dossier .athanor de chacun de vos projets, et ce que vous lui demandez (programmes à installer, démarrage avec l'ordinateur).\n\nLe fichier téléchargé peut rester où vous voulez : il ouvre Athanor.\n\nCréer ce dossier maintenant ?\n\nNon : Athanor se ferme sans rien écrire.", d)
	}
	return "Athanor — first launch", fmt.Sprintf("Athanor keeps everything of its own (its copy, its settings, its logs, the tools it installs) in a folder of its own:\n\n    %s\n\nElsewhere it only writes the .athanor folder of each of your projects, and what you ask for (programs to install, starting with the computer).\n\nThe downloaded file can stay wherever you like: it opens Athanor.\n\nCreate this folder now?\n\nNo: Athanor closes without writing anything.", d)
}

// langueSysteme: "fr" when the system speaks French, else "en".
func langueSysteme() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if l := strings.ToLower(os.Getenv(v)); l != "" {
			if strings.HasPrefix(l, "fr") {
				return "fr"
			}
			if l != "c" && l != "posix" && !strings.HasPrefix(l, "c.") {
				return "en"
			}
		}
	}
	return langueSystemeOS()
}

// premierLancement: Athanor's folder exists (an older installation, a previous launch), or the user
// accepts its creation (a dialog of the system; --oui accepts beforehand). False: nothing was written, the
// application closes.
func premierLancement(d string, accepte bool) bool {
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		if _, err := os.Stat(filepath.Join(d, nomConfig())); err == nil || estDossierKit(d) {
			return true
		}
	}
	if !accepte {
		titre, texte := texteConsentement(d)
		if !demanderConsentement(titre, texte) {
			return false
		}
	}
	return os.MkdirAll(d, 0o755) == nil
}
