package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// dossierRaccourciDefaut: the user's Desktop (where Windows says it is: OneDrive moves it).
func dossierRaccourciDefaut() string {
	c := commande("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"[Console]::OutputEncoding = [Text.Encoding]::UTF8; [Environment]::GetFolderPath('Desktop')")
	cacherFenetre(c)
	if out, err := c.Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Desktop")
	}
	return ""
}

// scriptLnkWindows: the .lnk through Windows' own WScript.Shell (the paths given in the environment: never
// quoted into the command).
// The Start menu gets one too (Athanor found by Windows' search), as the Linux menu does; never an error.
const scriptLnkWindows = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
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
try {
  $m = Join-Path ([Environment]::GetFolderPath('Programs')) 'Athanor.lnk'
  if ($m -ne $env:ATHANOR_LNK) { Faire $m; Write-Output ('MENU=' + $m) }
} catch {}`

func creerRaccourciSysteme(dossier, exe, kit string) ([]string, error) {
	lnk := filepath.Join(dossier, "Athanor.lnk")
	c := commande("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", scriptLnkWindows)
	cacherFenetre(c)
	c.Env = append(os.Environ(), "ATHANOR_LNK="+lnk, "ATHANOR_CIBLE="+exe, "ATHANOR_DOSSIER="+kit)
	out, err := c.CombinedOutput()
	if err != nil {
		return nil, errors.New("The shortcut could not be made: " + derniereLigneTexte(string(out)))
	}
	if _, err := os.Stat(lnk); err != nil {
		return nil, errors.New("The shortcut could not be made.")
	}
	faits := []string{lnk}
	for _, l := range strings.Split(string(out), "\n") {
		if m, ok := strings.CutPrefix(strings.TrimSpace(l), "MENU="); ok {
			if _, err := os.Stat(m); err == nil {
				faits = append(faits, m)
			}
		}
	}
	return faits, nil
}
