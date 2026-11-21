//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func systemeMachine() string { return "linux" }

// nvidiaSmi: NVIDIA's tool, installed with its driver ("" without one).
var nvidiaSmi = sync.OnceValue(func() string { p, _ := exec.LookPath("nvidia-smi"); return p })

// lireMachine: /proc for the processors, the memory, the disks' activity and the links; statfs for the space.
func lireMachine() lectureMachine {
	l := lectureMachine{t: time.Now(), coeurs: runtime.NumCPU()}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		l.cpuOccupe, l.cpuTotal = cpuProcStat(string(b))
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		l.ramTotal, l.ramUtil = memProc(string(b))
	}
	if p := nvidiaSmi(); p != "" {
		ctx, fin := context.WithTimeout(context.Background(), 1500*time.Millisecond) // a driver in trouble can hang it
		if b, err := exec.CommandContext(ctx, p, "--query-gpu=name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits").Output(); err == nil {
			l.gpus = parNvidiaSmi(string(b))
		}
		fin()
	}
	l.gpus = append(l.gpus, gpusAmdgpu("/")...)
	stats := map[string][3]uint64{}
	if b, err := os.ReadFile("/proc/diskstats"); err == nil {
		stats = disquesProc(string(b))
	}
	if b, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, m := range montagesProc(string(b)) {
			var st unix.Statfs_t
			if unix.Statfs(m[1], &st) != nil || st.Blocks == 0 {
				continue
			}
			d := disqueBrut{nom: m[1], total: st.Blocks * uint64(st.Bsize), libre: st.Bavail * uint64(st.Bsize)}
			dev := filepath.Base(m[0])
			if r, err := filepath.EvalSymlinks(m[0]); err == nil { // /dev/mapper/x → /dev/dm-0
				dev = filepath.Base(r)
			}
			if x, ok := stats[dev]; ok {
				d.occupe, d.lus, d.ecrits, d.activiteConnue = x[0], x[1], x[2], true
			}
			l.disques = append(l.disques, d)
		}
	}
	if b, err := os.ReadFile("/proc/net/dev"); err == nil {
		for _, r := range reseauxProc(string(b)) {
			if _, err := os.Stat(filepath.Join("/sys/class/net", r.nom, "wireless")); err == nil {
				r.genre = "wifi"
			} else if _, err := os.Stat(filepath.Join("/sys/class/net", r.nom, "device")); err != nil && !strings.HasPrefix(r.nom, "e") {
				continue // a virtual link (a bridge, a VPN tunnel) and not a card
			}
			l.reseaux = append(l.reseaux, r)
		}
	}
	return l
}
