//go:build !windows

package main

// Linux or macOS (native kit, or code checking): same functions, without WSL or virtual desktop.

import (
	"errors"
	"os/exec"
	"strings"
)

func cacherFenetre(c *exec.Cmd) {}

func commandeWSL(k Kit, script string) *exec.Cmd {
	c := exec.Command("bash", "-lc", script)
	c.Dir = k.Racine
	return c
}

// DemarrerDetache: on Linux, the pilot simply goes to the background (setsid in start.sh --background).
func DemarrerDetache(k Kit, script string) error {
	c := exec.Command("bash", "-lc", strings.Replace(script, "--keep", "--background", 1))
	c.Dir = k.Racine
	_, err := c.Output()
	return err
}

func FenetreWSL(k Kit, titre, script string) error {
	return errors.New("Interactive window: Windows only; run in a terminal: " + script)
}
func FenetrePowerShell(k Kit, s string) error { return errors.New("PowerShell: Windows only") }
func Ouvrir(chemin string) error              { return exec.Command("xdg-open", chemin).Start() }
func OuvrirFenetreApp(url string)             { _ = exec.Command("xdg-open", url).Start() }
func FenetreApp(url string) bool              { return false }
func DemarrageWindows(actif bool) error {
	return errors.New("Start with Windows: Windows only")
}
func DemarrageWindowsActif() bool { return false }
func migrerSysteme()              {}

type Bureau struct {
	Programmes    []string
	Actif         bool
	PilotesActifs func() bool
}
type EtatBureau struct {
	Disponible bool     `json:"disponible"`
	Message    string   `json:"message"`
	Deplacees  int      `json:"deplacees"`
	Vues       []string `json:"vues"`
}

func NouveauBureau(k Kit) *Bureau            { return &Bureau{Actif: true} }
func (b *Bureau) DefinirKit(k Kit)           {}
func (b *Bureau) DefinirDossiers(d []string) {}
func (b *Bureau) DefinirReglages(actif bool, programmes []string) {
	b.Actif, b.Programmes = actif, programmes
}
func (b *Bureau) Etat() EtatBureau {
	return EtatBureau{Message: "Virtual desktop: Windows only."}
}
func (b *Bureau) Voir() error        { return errors.New("Windows only") }
func (b *Bureau) Revenir() error     { return errors.New("Windows only") }
func (b *Bureau) RamenerTout() error { return errors.New("Windows only") }
