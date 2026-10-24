//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A panic in a goroutine of the pilot: its report kept, Claude's session stopped, pilot.pid and the lock
// released, exit 70 (pilot.sh then carries on in bash).
func TestPlanterPilote(t *testing.T) {
	if d := os.Getenv("ATHANOR_TEST_PLANTER"); d != "" {
		p := &pilote{prive: filepath.Join(d, "prive"), etat: filepath.Join(d, "etat"), projet: d, arretCh: make(chan struct{}), battementStop: make(chan struct{})}
		vf, ok := verrouPilote(filepath.Join(p.prive, "verrou"))
		if !ok {
			os.Exit(3)
		}
		p.verrouFichier = vf
		_ = os.WriteFile(filepath.Join(p.prive, "pilot.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
		go func() {
			defer p.recupererPanique()
			var m map[string]int
			m["boom"] = 1 // a nil map: a real Go panic
		}()
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	d := t.TempDir()
	for _, s := range []string{"prive", "etat"} {
		_ = os.MkdirAll(filepath.Join(d, s), 0o755)
	}
	// a Claude session (its own process group), as the pilot starts it
	claude := exec.Command("sleep", "60")
	claude.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := claude.Start(); err != nil {
		t.Fatal(err)
	}
	fini := make(chan struct{})
	go func() { _ = claude.Wait(); close(fini) }()
	_ = os.WriteFile(filepath.Join(d, "prive", "claude_sid"), []byte(strconv.Itoa(claude.Process.Pid)+"\n"), 0o644)

	c := exec.Command(os.Args[0], "-test.run=^TestPlanterPilote$")
	c.Env = append(os.Environ(), "ATHANOR_TEST_PLANTER="+d)
	out, err := c.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != codeRepliPilote {
		t.Fatalf("exit %v, want %d\n%s", err, codeRepliPilote, out)
	}
	rapport, _ := os.ReadFile(filepath.Join(d, "prive", "pilote-go.crash"))
	if !strings.Contains(string(rapport), "panic: assignment to entry in nil map") || !strings.Contains(string(rapport), "TestPlanterPilote") {
		t.Fatalf("report:\n%s", rapport)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "prive", "pilot.log")); !strings.Contains(string(b), "The Go pilot hit an internal error (assignment to entry in nil map); its report: ") {
		t.Fatalf("log: %s", b)
	}
	if _, err := os.Stat(filepath.Join(d, "prive", "pilot.pid")); err == nil {
		t.Fatal("pilot.pid left")
	}
	select {
	case <-fini:
	case <-time.After(10 * time.Second):
		_ = claude.Process.Kill()
		t.Fatal("Claude's session left running")
	}
	if vf, ok := verrouPilote(filepath.Join(d, "prive", "verrou")); !ok {
		t.Fatal("the lock is still held")
	} else {
		vf.Close()
	}
}
