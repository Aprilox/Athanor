//go:build linux || darwin

package main

// Athanor in the background (3.1.4): a terminal that finds no Athanor running starts one in its own session,
// out of the terminal's reach, then shows it as it shows an Athanor already open. Closing the terminal or the
// SSH connection leaves it running; the terminal's q asks whether to stop it completely (apiQuitter).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// lancerEnFond: false when it could not start one (Athanor then serves from this terminal, as before).
func lancerEnFond() (int, bool) {
	exe, err := os.Executable()
	if err != nil {
		return 0, false
	}
	journal := filepath.Join(filepath.Dir(fichierInstance()), "athanor-fond.log")
	f, err := os.OpenFile(journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if verrouInstance != nil { // the one in the background takes the instance lock
		_ = verrouInstance.Close()
		verrouInstance = nil
	}
	c := exec.Command(exe, os.Args[1:]...)
	c.Env = append(os.Environ(), "ATHANOR_FOND=1")
	c.Stdout, c.Stderr = f, f
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // its own session: no SIGHUP when the terminal goes
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "Athanor could not start in the background ("+err.Error()+"): it runs in this terminal.")
		return 0, false
	}
	_ = c.Process.Release()
	fmt.Println("Athanor starts in the background…")
	for fin := time.Now().Add(30 * time.Second); time.Now().Before(fin); time.Sleep(250 * time.Millisecond) {
		if lien, _ := lienInstanceTerminal(); lien != "" {
			return lancerTUI(lien, false, ""), true
		}
	}
	fmt.Fprintln(os.Stderr, "Athanor did not answer after 30 seconds: see "+journal)
	return 1, true
}
