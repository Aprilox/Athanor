//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestProgrammesDuDossier: a program holding a file of a folder is named (Restart Manager), stopped only when its
// pid was confirmed, and the folder is then free.
func TestProgrammesDuDossier(t *testing.T) {
	if f := os.Getenv("ATHANOR_TIENT"); f != "" { // the helper: holds the file open until it is stopped
		h, err := os.Open(f)
		if err != nil {
			os.Exit(2)
		}
		os.Stdout.WriteString("pret\n")
		time.Sleep(2 * time.Minute)
		_ = h.Close()
		return
	}
	d := t.TempDir()
	ecrireFichier(t, filepath.Join(d, "src", "a.txt"), "a", 0o644)
	cmd := exec.Command(os.Args[0], "-test.run=^TestProgrammesDuDossier$")
	cmd.Env = append(os.Environ(), "ATHANOR_TIENT="+filepath.Join(d, "src", "a.txt"))
	sortie, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		t.Fatal("helper:", err)
	}
	fini := make(chan struct{})
	go func() { _ = cmd.Wait(); close(fini) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-fini })
	if l, _ := bufio.NewReader(sortie).ReadString('\n'); l != "pret\n" {
		t.Fatalf("helper not ready: %q", l)
	}
	pid := cmd.Process.Pid
	l := programmesDuDossier(d)
	if i := slices.IndexFunc(l, func(b ProgrammeBloquant) bool { return b.PID == pid }); i < 0 || !l[i].Arretable || l[i].Nom == "" {
		t.Fatalf("the program holding a file is not named: %+v (pid %d)", l, pid)
	}
	if slices.ContainsFunc(l, func(b ProgrammeBloquant) bool { return b.PID == os.Getpid() }) {
		t.Error("Athanor names itself")
	}
	if n := arreterBloquants(d, []int{pid + 1}); len(n) != 0 {
		t.Errorf("a pid that holds nothing was stopped: %v", n)
	}
	if n := arreterBloquants(d, []int{pid}); len(n) != 1 {
		t.Fatalf("the confirmed program was not stopped: %v", n)
	}
	select {
	case <-fini:
	case <-time.After(10 * time.Second):
		t.Fatal("the program still runs")
	}
	if l := programmesDuDossier(d); slices.ContainsFunc(l, func(b ProgrammeBloquant) bool { return b.PID == pid }) {
		t.Errorf("still named once stopped: %+v", l)
	}
}
