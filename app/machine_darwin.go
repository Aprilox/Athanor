//go:build darwin

package main

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

func systemeMachine() string { return "macos" }

var (
	machOnce       sync.Once
	machHostSelf   func() uint32
	hostStatistics func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32
	portsMac       map[string]string
)

// cpuMac: the processors' ticks (user, system, idle, nice) from host_statistics, HOST_CPU_LOAD_INFO.
func cpuMac() (occupe, total uint64) {
	machOnce.Do(func() {
		if lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			purego.RegisterLibFunc(&machHostSelf, lib, "mach_host_self")
			purego.RegisterLibFunc(&hostStatistics, lib, "host_statistics")
		}
		if b, err := exec.Command("networksetup", "-listallhardwareports").Output(); err == nil {
			portsMac = portsMateriels(string(b))
		}
	})
	if hostStatistics == nil {
		return 0, 0
	}
	var t [4]uint32
	n := uint32(4)
	if hostStatistics(machHostSelf(), 3, unsafe.Pointer(&t[0]), &n) != 0 {
		return 0, 0
	}
	return uint64(t[0]) + uint64(t[1]) + uint64(t[3]), uint64(t[0]) + uint64(t[1]) + uint64(t[2]) + uint64(t[3])
}

// lireMachine: host_statistics, the memory size and vm_stat, the local volumes' space (/ and /Volumes/…), netstat.
func lireMachine() lectureMachine {
	l := lectureMachine{t: time.Now(), coeurs: runtime.NumCPU()}
	l.cpuOccupe, l.cpuTotal = cpuMac()
	if t, err := unix.SysctlUint64("hw.memsize"); err == nil {
		l.ramTotal = t
		if b, err := exec.Command("vm_stat").Output(); err == nil {
			l.ramUtil = min(vmStat(string(b)), t)
		}
	}
	if n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT); err == nil && n > 0 {
		fs := make([]unix.Statfs_t, n)
		if n, err = unix.Getfsstat(fs, unix.MNT_NOWAIT); err == nil {
			for _, f := range fs[:n] {
				point := unix.ByteSliceToString(f.Mntonname[:])
				if f.Flags&unix.MNT_LOCAL == 0 || f.Blocks == 0 || point != "/" && !strings.HasPrefix(point, "/Volumes/") {
					continue
				}
				l.disques = append(l.disques, disqueBrut{nom: point, total: f.Blocks * uint64(f.Bsize), libre: f.Bavail * uint64(f.Bsize)})
			}
		}
	}
	if b, err := exec.Command("netstat", "-ibn").Output(); err == nil {
		for _, r := range reseauxNetstat(string(b)) {
			if p, ok := portsMac[r.nom]; ok {
				r.nom, r.genre = p+" ("+r.nom+")", genreReseau(p)
			}
			l.reseaux = append(l.reseaux, r)
		}
	}
	return l
}
