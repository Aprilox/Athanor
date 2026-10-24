//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// exeKit: the kit's Athanor.exe, else this very program.
func exeKit(k Kit) string {
	e := filepath.Join(k.Racine, "Athanor.exe")
	if st, err := os.Stat(e); err == nil && st.Mode().IsRegular() {
		return e
	}
	if s, err := os.Executable(); err == nil {
		return s
	}
	return e
}

// envNatifKit: the environment of a command run in native Windows: the PATH as Windows has it now (a
// program just installed is found), and the Git Bash Claude Code's Bash tool uses.
func envNatifKit() []string {
	env := []string{}
	for _, e := range os.Environ() {
		u := strings.ToUpper(e)
		if !strings.HasPrefix(u, "PATH=") && !strings.HasPrefix(u, "CLAUDE_CODE_GIT_BASH_PATH=") {
			env = append(env, e)
		}
	}
	env = append(env, "PATH="+cheminFrais())
	if b := bashNatif(); b != "" {
		env = append(env, "CLAUDE_CODE_GIT_BASH_PATH="+b)
	}
	return env
}

// commandeKit: COMMAND ARGS run by Athanor.exe (native Windows) or by the kit's athanor in WSL (wsl.exe
// --exec: no shell, each argument as it is).
func commandeKit(k Kit, args ...string) *exec.Cmd {
	if modeNatif.Load() {
		c := commande(exeKit(k), argsKit(args)...)
		c.Dir = k.Racine
		c.Env = envNatifKit()
		return c
	}
	c := commande("wsl.exe", append([]string{"-d", k.Distro(), "--cd", k.Racine, "--exec", "./athanor"}, argsKit(args)...)...)
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	return c
}

// demarrerKitDetache: COMMAND ARGS hidden and outside the application's job if possible (closing the
// application must not take the pilot with it). CREATE_NO_WINDOW without DETACHED_PROCESS: Windows ignores
// the first when both are given, and wsl.exe (a console program) then opens a console window of its own.
func demarrerKitDetache(k Kit, args ...string) error {
	lancer := func(drapeaux uint32) error {
		c := commandeKit(k, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: drapeaux}
		if err := c.Start(); err != nil {
			return err
		}
		go func() { _ = c.Wait() }()
		return nil
	}
	if err := lancer(createNoWindow | createNewProcessGroup | createBreakawayFromJob); err != nil {
		return lancer(createNoWindow | createNewProcessGroup)
	}
	return nil
}

// lancerDetacheExe: a Windows program started on its own, outside the application's job (it outlives
// the application: the update that replaces it).
func lancerDetacheExe(exe string, args ...string) error {
	lancer := func(drapeaux uint32) error {
		c := exec.Command(exe, args...)
		c.Dir = filepath.Dir(exe)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: drapeaux}
		if err := c.Start(); err != nil {
			return err
		}
		go func() { _ = c.Wait() }()
		return nil
	}
	if err := lancer(createNewProcessGroup | createBreakawayFromJob); err != nil {
		return lancer(createNewProcessGroup)
	}
	return nil
}

// fenetreKit: an interactive step (sudo, a token, a GitHub sign-in, an installation) in a console window of
// its own, which stays open until Enter.
func fenetreKit(k Kit, titre string, args ...string) error {
	if modeNatif.Load() {
		return demarrerFenetre(titre, append([]string{exeKit(k), "--fenetre", titre}, argsKit(args)...)...)
	}
	return demarrerFenetre(titre, append([]string{"wsl.exe", "-d", k.Distro(), "--cd", k.Racine, "--exec", "./athanor", "--fenetre", titre}, argsKit(args)...)...)
}

// fenetreWindows: a step of the Windows side itself (an installation with winget, the administrator task,
// the network for Unreal), run by Athanor.exe in a console window of its own, whatever the mode.
func fenetreWindows(k Kit, titre string, args ...string) error {
	return demarrerFenetre(titre, append([]string{exeKit(k), "--fenetre", titre}, args...)...)
}

// optionFond: how the application starts a pilot: in WSL, the wsl.exe that started it stays as long as it
// runs (Windows stops the distribution a few seconds after its last wsl.exe ends); natively, simply in the
// background.
func optionFond() string {
	if modeNatif.Load() {
		return "--background"
	}
	return "--keep"
}

// garderWSLVivant: a pilot that outlived the application (WSL mode): a wsl.exe that waits for its end again.
func garderWSLVivant(k Kit, p Projet) error {
	if modeNatif.Load() {
		return nil
	}
	return demarrerKitDetache(k, "demarrer", "--attach", p.Dossier)
}
