//go:build !windows

package main

// Linux or macOS (native kit, or code checking): same functions, without WSL or virtual desktop.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func cacherFenetre(c *exec.Cmd) {}

func commandeWSL(k Kit, script string) *exec.Cmd {
	c := exec.Command("bash", "-lc", script)
	c.Dir = k.Racine
	return c
}

// DemarrerDetache: on Linux, the pilot simply goes to the background (setsid in start.sh --background).
func DemarrerDetache(k Kit, script string) error {
	if strings.Contains(script, "--attach") {
		return nil // no WSL to keep awake outside Windows
	}
	c := exec.Command("bash", "-lc", strings.Replace(script, "--keep", "--background", 1))
	c.Dir = k.Racine
	_, err := c.Output()
	return err
}

// FenetreWSL on Linux: the step runs in a terminal window of the desktop (the first one found).
func FenetreWSL(k Kit, titre, script string) error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("Interactive window: no graphical session; run in a terminal: " + script)
	}
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(k.Journaux(), "window-*.sh")
	if err != nil {
		return err
	}
	_, err = f.WriteString("cd " + citerShell(k.Racine) + " || exit 1\n" + script + "\necho\nread -p 'Press Enter to close' _\nrm -f -- \"$0\"\n")
	f.Close()
	if err != nil {
		return err
	}
	for _, t := range [][]string{{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"}, {"konsole", "-e"}, {"xfce4-terminal", "-x"},
		{"mate-terminal", "-x"}, {"kitty"}, {"alacritty", "-e"}, {"xterm", "-e"}} {
		if chemin, err := exec.LookPath(t[0]); err == nil {
			c := exec.Command(chemin, append(t[1:], "bash", "-l", f.Name())...)
			c.Dir = k.Racine
			return c.Start()
		}
	}
	os.Remove(f.Name())
	return errors.New("Interactive window: no terminal found; run in a terminal: " + script)
}
func FenetrePowerShell(k Kit, s string) error { return errors.New("PowerShell: Windows only") }
func Ouvrir(chemin string) error              { return exec.Command("xdg-open", chemin).Start() }
func OuvrirFenetreApp(url string)             { _ = exec.Command("xdg-open", url).Start() }
func FenetreApp(url string) bool              { return false }

// Linux: the window is the browser's (no notification area icon); a second launch opens another one.
func permettrePremierPlan()                                               {}
func montrerFenetre()                                                     { OuvrirFenetreApp(lienApp()) }
func IconeNotification(langue func() string, ouvrir, quitter func()) bool { return false }

// Linux: "open when the computer starts" = the desktop session's autostart entry
// (~/.config/autostart/athanor.desktop, the freedesktop standard: GNOME, KDE, Xfce, Cinnamon…).
func fichierAutostart() string {
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
	entree := "[Desktop Entry]\nType=Application\nName=Athanor\nComment=Claude Code building whole projects\nExec=\"" +
		strings.ReplaceAll(exe, `"`, `\"`) + "\"\nPath=" + filepath.Dir(exe) + "\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(f, []byte(entree), 0o644)
}
func DemarrageWindowsActif() bool {
	_, err := os.Stat(fichierAutostart())
	return err == nil
}
func migrerSysteme() {}

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
	if !actif {
		os.Remove(f)
		return
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("xvfb\n"), 0o644)
}

func (b *Bureau) Etat() EtatBureau {
	b.mu.Lock()
	defer b.mu.Unlock()
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
