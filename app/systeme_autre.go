//go:build !windows

package main

// Linux or macOS (native kit, or code checking): same functions, without WSL or virtual desktop.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

func cacherFenetre(c *exec.Cmd) {}

func Ouvrir(chemin string) error {
	if runtime.GOOS == "darwin" {
		return commande("open", chemin).Start()
	}
	return commande("xdg-open", chemin).Start()
}

// OuvrirFenetreApp: an application window of a Chromium browser (no address bar, its own profile), as the
// Windows fallback does; else the default browser.
func OuvrirFenetreApp(url string) {
	profil := filepath.Join(dossierDonneesApp(), "navigateur")
	options := []string{"--app=" + url, "--user-data-dir=" + profil, "--window-size=1240,820", "--no-first-run", "--no-default-browser-check"}
	if runtime.GOOS == "darwin" {
		for _, nom := range []string{"Google Chrome", "Microsoft Edge", "Chromium", "Brave Browser"} {
			if _, err := os.Stat(filepath.Join("/Applications", nom+".app")); err == nil {
				if commande("open", append([]string{"-na", nom, "--args"}, options...)...).Start() == nil {
					return
				}
			}
		}
		_ = commande("open", url).Start()
		return
	}
	for _, nom := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if chemin, err := exec.LookPath(nom); err == nil {
			c := commande(chemin, options...)
			if c.Start() == nil {
				go func() { _ = c.Wait() }()
				return
			}
		}
	}
	_ = commande("xdg-open", url).Start()
}

// dossierDonneesApp: Athanor's own data (the browser profile of its window): ~/Library/Application
// Support/Athanor on macOS, ~/.local/share/athanor on Linux.
func dossierDonneesApp() string {
	if runtime.GOOS == "darwin" {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, "Library", "Application Support", "Athanor")
		}
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "athanor")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "share", "athanor")
	}
	return filepath.Join(os.TempDir(), "athanor")
}

func FenetreApp(url string) bool { return false }

// Linux: the window is the browser's (no notification area icon); a second launch opens another one.
func permettrePremierPlan()                                               {}
func montrerFenetre()                                                     { OuvrirFenetreApp(lienApp()) }
func IconeNotification(langue func() string, ouvrir, quitter func()) bool { return false }

// Linux: "open when the computer starts" = the desktop session's autostart entry
// (~/.config/autostart/athanor.desktop, the freedesktop standard: GNOME, KDE, Xfce, Cinnamon…); macOS: a
// launch agent of the user (~/Library/LaunchAgents/ai.athanor.app.plist), started at login.
func fichierAutostart() string {
	if runtime.GOOS == "darwin" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(h, "Library", "LaunchAgents", "ai.athanor.app.plist")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "autostart", "athanor.desktop")
}

func DemarrageWindows(actif bool) error {
	f := fichierAutostart()
	if f == "" {
		return errors.New("No configuration folder for the autostart entry")
	}
	if !actif {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return os.WriteFile(f, []byte(agentLancementMac(exe)), 0o644)
	}
	entree := "[Desktop Entry]\nType=Application\nName=Athanor\nComment=Claude Code building whole projects\nExec=\"" +
		strings.ReplaceAll(exe, `"`, `\"`) + "\"\nPath=" + filepath.Dir(exe) + "\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(f, []byte(entree), 0o644)
}
func DemarrageWindowsActif() bool {
	_, err := os.Stat(fichierAutostart())
	return err == nil
}
func migrerSysteme() {}

// agentLancementMac: the launch agent that opens Athanor at login.
func agentLancementMac(exe string) string {
	x := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>ai.athanor.app</string>
	<key>ProgramArguments</key><array><string>` + x(exe) + `</string></array>
	<key>WorkingDirectory</key><string>` + x(filepath.Dir(exe)) + `</string>
	<key>RunAtLoad</key><true/>
</dict>
</plist>
`
}

// Bureau on Linux: Claude's desktop is the private X server each pilot runs (system/ecran.sh). The
// option only writes or removes logs/bureau-isole, which the pilots read at every cycle; "View" opens
// the picture of that screen in the application (GET /api/ecran).
type Bureau struct {
	mu            sync.Mutex
	kit           Kit
	Programmes    []string
	Actif         bool
	PilotesActifs func() bool
	Langue        func() string
}
type EtatBureau struct {
	Disponible bool     `json:"disponible"`
	Message    string   `json:"message"`
	Deplacees  int      `json:"deplacees"`
	Vues       []string `json:"vues"`
	Isole      bool     `json:"isole"`
	Restantes  int      `json:"restantes"`
	Ecran      bool     `json:"ecran"` // Linux: "View" shows Claude's screen in the application
}

func NouveauBureau(k Kit) *Bureau {
	b := &Bureau{kit: k, Actif: true}
	b.marquer()
	return b
}
func (b *Bureau) DefinirKit(k Kit) {
	b.mu.Lock()
	ancien := b.kit.Racine
	b.kit = k
	b.mu.Unlock()
	if ancien != "" && ancien != k.Racine { // the previous tool folder's pilots go back to the user's screen
		os.Remove(filepath.Join(ancien, "logs", "bureau-isole"))
	}
	b.marquer()
}
func (b *Bureau) DefinirDossiers(d []string) {}
func (b *Bureau) DefinirReglages(actif bool, programmes []string) {
	b.mu.Lock()
	b.Actif, b.Programmes = actif, programmes
	b.mu.Unlock()
	b.marquer()
}

// marquer writes or removes logs/bureau-isole in the tool folder (read by the pilots at every cycle).
func (b *Bureau) marquer() {
	b.mu.Lock()
	racine, actif := b.kit.Racine, b.Actif
	b.mu.Unlock()
	if racine == "" {
		return
	}
	f := filepath.Join(racine, "logs", "bureau-isole")
	if !actif || runtime.GOOS != "linux" { // macOS: no private X server, the windows open on the screen
		os.Remove(f)
		return
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("xvfb\n"), 0o644)
}

func (b *Bureau) Etat() EtatBureau {
	b.mu.Lock()
	defer b.mu.Unlock()
	if runtime.GOOS != "linux" {
		return EtatBureau{Message: "Virtual desktop: Windows only."}
	}
	if !b.Actif {
		return EtatBureau{Message: "Virtual desktop disabled (Options).", Ecran: true}
	}
	return EtatBureau{Disponible: true, Ecran: true, Message: "“Claude” screen ready: the Linux windows Claude opens go there, never on your screen."}
}
func (b *Bureau) Voir() error        { return errors.New("Windows only") }
func (b *Bureau) Revenir() error     { return errors.New("Windows only") }
func (b *Bureau) RamenerTout() error { return errors.New("Windows only") }

// LancerIsole (Athanor.exe --isole): Claude's isolated desktop exists on Windows only.
func LancerIsole(args []string) int {
	fmt.Fprintln(os.Stderr, "--isole: Windows only")
	return 2
}

func remettreBureau() {}

func GardienRetour(args []string) int { return 2 }

// arreterArbre stops a command (its own process; the script's timeout bounds the rest).
func arreterArbre(c *exec.Cmd) {
	if c != nil && c.Process != nil {
		_ = c.Process.Kill()
	}
}
