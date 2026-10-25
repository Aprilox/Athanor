//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// In a job that forbids leaving it (GitHub's runners): cloudflared is started inside it instead of failing.
// The test program is put in such a job in a child process of its own (ATHANOR_ESSAI_JOB), to leave the
// rest of the tests alone.
func TestTunnelDansUnJobFerme(t *testing.T) {
	if os.Getenv("ATHANOR_ESSAI_JOB") == "1" {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION // no JOB_OBJECT_LIMIT_BREAKAWAY_OK
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			t.Fatal(err)
		}
		if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
			t.Skip("cannot put this process in a job here:", err)
		}
		// leaving the job is refused, as on GitHub's runners
		c := exec.Command(os.Args[0], "-test.run", "^$")
		c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createBreakawayFromJob}
		if err := c.Start(); err == nil {
			_ = c.Wait()
			t.Skip("this system lets a process leave a job that forbids it")
		}
		s := nouveauServeur(Config{Kit: t.TempDir()})
		s.config.Distance = DistanceConfig{Actif: true, Port: 45557}
		installerFauxCloudflared(t, s)
		t.Cleanup(s.arreterTunnel)
		go s.lancerTunnel()
		if e := attendreTunnel(t, s, ""); e.pid == 0 {
			t.Fatal("no tunnel")
		}
		return
	}
	c := exec.Command(os.Args[0], "-test.run", "^TestTunnelDansUnJobFerme$", "-test.v")
	c.Env = append(os.Environ(), "ATHANOR_ESSAI_JOB=1")
	out, err := c.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatal(err)
	}
}
