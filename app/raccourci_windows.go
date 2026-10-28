package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// dossierConnuWindows: a folder where Windows says it is (the Desktop moved by OneDrive, a redirected Start
// menu), asked of Windows itself: instant, and no text encoding to get wrong.
func dossierConnuWindows(id *windows.KNOWNFOLDERID) string {
	d, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
	if err != nil || d == "" {
		return ""
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return ""
	}
	return d
}

// dossierRaccourciDefaut: the user's Desktop.
func dossierRaccourciDefaut() string {
	if d := dossierConnuWindows(windows.FOLDERID_Desktop); d != "" {
		return d
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Desktop")
	}
	return ""
}

// scriptLnkWindows: the .lnk through Windows' own WScript.Shell (the paths given in the environment: never
// quoted into the command). The Start menu gets one too (Athanor found by Windows' search), as the Linux
// menu does; never an error.
const scriptLnkWindows = `$ErrorActionPreference = 'Stop'
$w = New-Object -ComObject WScript.Shell
function Faire($f) {
  $s = $w.CreateShortcut($f)
  $s.TargetPath = $env:ATHANOR_CIBLE
  $s.WorkingDirectory = $env:ATHANOR_DOSSIER
  $s.IconLocation = $env:ATHANOR_CIBLE + ',0'
  $s.Description = 'Athanor'
  $s.Save()
}
Faire $env:ATHANOR_LNK
if ($env:ATHANOR_MENU) { try { Faire $env:ATHANOR_MENU } catch {} }`

// delaiRaccourciWindows: PowerShell never keeps the window waiting longer than this.
var delaiRaccourciWindows = 60 * time.Second

func creerRaccourciSysteme(dossier, exe, kit string) ([]string, error) {
	lnk := filepath.Join(dossier, "Athanor.lnk")
	menu := ""
	if m := dossierConnuWindows(windows.FOLDERID_Programs); m != "" {
		if x := filepath.Join(m, "Athanor.lnk"); !strings.EqualFold(x, lnk) {
			menu = x
		}
	}
	c := commande("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", scriptLnkWindows)
	cacherFenetre(c)
	c.Env = append(os.Environ(), "ATHANOR_LNK="+lnk, "ATHANOR_CIBLE="+exe, "ATHANOR_DOSSIER="+kit, "ATHANOR_MENU="+menu)
	var sortie bytes.Buffer
	c.Stdout, c.Stderr = &sortie, &sortie
	if err := executerAvecDelai(c, delaiRaccourciWindows); err != nil {
		return nil, errors.New("The shortcut could not be made: " + strings.TrimSpace(derniereLigneTexte(sortie.String())+" "+err.Error()))
	}
	if _, err := os.Stat(lnk); err != nil {
		return nil, errors.New("The shortcut could not be made.")
	}
	faits := []string{lnk}
	if menu != "" {
		if _, err := os.Stat(menu); err == nil {
			faits = append(faits, menu)
		}
	}
	return faits, nil
}

// executerAvecDelai: the command run, stopped when it takes longer than d.
func executerAvecDelai(c *exec.Cmd, d time.Duration) error {
	if err := c.Start(); err != nil {
		return err
	}
	fini := make(chan error, 1)
	go func() { fini <- c.Wait() }()
	select {
	case err := <-fini:
		return err
	case <-time.After(d):
		_ = c.Process.Kill()
		<-fini
		return errors.New("PowerShell did not answer")
	}
}
