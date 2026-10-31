//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// blenderOuvertNatif: a Blender runs on this computer (the Snap's and the Flatpak's are "blender" too).
var blenderOuvertNatif = func() bool { return commande("pgrep", "-x", "blender").Run() == nil }

// blenderNatif: passerelle blender on native Linux: Blender run as it is (blender -b -P script.py…), or
// --open [FILE.blend]: its window, detached, on a .blend of the project (nothing when one is open).
func (p *passerelle) blenderNatif(args []string) int {
	exe := os.Getenv("BLENDER_EXE")
	if exe == "" || !fichierP(exe) {
		exe = trouverBlenderLinux(filepath.SplitList(os.Getenv("PATH")), os.Getenv("HOME"))
	}
	if exe == "" {
		p.absent("Blender")
	}
	if argGH(args, 0) != "--open" {
		// a headless run: shown in the preview window when the project asks for it (blender_apercu.go)
		a := p.preparerApercuBlender(args, func(c string) string { return c })
		c := exec.Command(exe, a.args...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, p.sortie, p.erreur
		err := c.Run()
		p.montrerApercuBlender(a, func(fichier, rechargeur string) error {
			v := exec.Command(exe, fichier, "--python", rechargeur)
			v.Dir = filepath.Dir(fichier)
			v.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // the window outlives this command
			if err := v.Start(); err != nil {
				return err
			}
			return v.Process.Release()
		})
		var e *exec.ExitError
		if errors.As(err, &e) {
			return e.ExitCode()
		}
		if err != nil {
			p.refus(1, "Blender did not start: %v", err)
		}
		return 0
	}
	if blenderOuvertNatif() {
		fmt.Fprintln(p.sortie, "Blender is already open.")
		return 0
	}
	var a []string
	if fichier := argGH(args, 1); fichier != "" {
		if !strings.HasSuffix(minuscules(fichier), ".blend") {
			p.refus(2, "Refused: a .blend file only.")
		}
		if !fichierP(fichier) {
			p.refus(2, "File not found: %s", fichier)
		}
		reel := realpathP(fichier)
		if !prefixeP(reel, realpathP(p.projet)+"/") {
			p.refus(2, "Refused: the file must be inside the project (%s).", reel)
		}
		a = append(a, reel)
	}
	c := exec.Command(exe, a...)
	c.Dir = p.projet
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // Blender outlives this command
	if err := c.Start(); err != nil {
		p.refus(1, "Blender did not start: %v", err)
	}
	_ = c.Process.Release()
	for i := 0; i < 20; i++ {
		if blenderOuvertNatif() {
			fmt.Fprintln(p.sortie, "Blender started.")
			return 0
		}
		time.Sleep(attenteBlender)
	}
	p.refus(1, "Blender did not start.")
	return 1
}

// attenteBlender: between two looks for Blender's window (the tests' is short).
var attenteBlender = 2 * time.Second
