//go:build windows

package main

import (
	"net"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pGetSystemTimes       = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemTimes")
	pGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx: MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length, MemoryLoad                                                                                   uint32
	TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
}

func systemeMachine() string { return "windows" }

// diskPerformance: DISK_PERFORMANCE (IOCTL_DISK_PERFORMANCE), its times in 100 ns units.
type diskPerformance struct {
	BytesRead, BytesWritten, ReadTime, WriteTime, IdleTime int64
	ReadCount, WriteCount, QueueDepth, SplitCount          uint32
	QueryTime                                              int64
	StorageDeviceNumber                                    uint32
	StorageManagerName                                     [8]uint16
}

const ioctlDiskPerformance = 0x70020

// lireMachine: GetSystemTimes, GlobalMemoryStatusEx, each fixed drive's space and activity, each link's counters.
func lireMachine() lectureMachine {
	l := lectureMachine{t: time.Now(), coeurs: runtime.NumCPU()}
	var idle, kernel, user windows.Filetime
	if r, _, _ := pGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r != 0 {
		ft := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
		l.cpuTotal = ft(kernel) + ft(user) // kernel time includes the idle time
		l.cpuOccupe = l.cpuTotal - ft(idle)
	}
	m := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r != 0 {
		l.ramTotal, l.ramUtil = m.TotalPhys, m.TotalPhys-m.AvailPhys
	}
	l.gpus = lireGPU() // machine_gpu_windows.go
	buf := make([]uint16, 256)
	if n, err := windows.GetLogicalDriveStrings(uint32(len(buf)), &buf[0]); err == nil {
		for _, racine := range strings.Split(windows.UTF16ToString(buf[:n]), "\x00") {
			if racine == "" {
				continue
			}
			p, _ := windows.UTF16PtrFromString(racine)
			if windows.GetDriveType(p) != windows.DRIVE_FIXED {
				continue // a CD, a network share, a card reader without a card
			}
			var libre, total, totalLibre uint64
			if windows.GetDiskFreeSpaceEx(p, &libre, &total, &totalLibre) != nil || total == 0 {
				continue
			}
			d := disqueBrut{nom: strings.TrimSuffix(racine, `\`), total: total, libre: libre}
			if perf, ok := performanceVolume(d.nom); ok {
				d.occupe, d.duree = uint64(perf.QueryTime-perf.IdleTime), uint64(perf.QueryTime)
				d.lus, d.ecrits, d.activiteConnue = uint64(perf.BytesRead), uint64(perf.BytesWritten), true
			}
			l.disques = append(l.disques, d)
		}
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagUp == 0 || len(i.HardwareAddr) == 0 {
			continue
		}
		row := windows.MibIfRow{Index: uint32(i.Index)}
		if windows.GetIfEntry(&row) != nil {
			continue
		}
		desc := strings.ToLower(string(row.Descr[:row.DescrLen]))
		if strings.Contains(desc, "virtual") || strings.Contains(desc, "hyper-v") || strings.Contains(desc, "vpn") || strings.Contains(desc, "loopback") {
			continue // a virtual switch (WSL, Hyper-V) or a VPN, not a card of this computer
		}
		genre := "autre"
		switch row.Type {
		case 71: // IF_TYPE_IEEE80211
			genre = "wifi"
		case 6: // IF_TYPE_ETHERNET_CSMACD
			genre = "ethernet"
		}
		l.reseaux = append(l.reseaux, reseauBrut{nom: i.Name, genre: genre, entree: uint64(row.InOctets), sortie: uint64(row.OutOctets)})
	}
	return l
}

// performanceVolume: a volume's I/O counters (none when the system keeps no disk counters).
func performanceVolume(lettre string) (diskPerformance, bool) {
	var p diskPerformance
	nom, _ := windows.UTF16PtrFromString(`\\.\` + lettre)
	h, err := windows.CreateFile(nom, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return p, false
	}
	defer windows.CloseHandle(h)
	var n uint32
	err = windows.DeviceIoControl(h, ioctlDiskPerformance, nil, 0, (*byte)(unsafe.Pointer(&p)), uint32(unsafe.Sizeof(p)), &n, nil)
	return p, err == nil && p.QueryTime > 0
}
