package main

import (
	"runtime"
	"slices"
	"testing"
	"time"
)

// TestMachineFormats: the texts Linux and macOS give, read as they are (every system runs this).
func TestMachineFormats(t *testing.T) {
	if o, tot := cpuProcStat("cpu  100 20 30 800 50 5 5 10 7 3\ncpu0 1 2 3 4 5 6 7 8 9 10\n"); o != 170 || tot != 1020 {
		t.Errorf("/proc/stat: %d %d", o, tot)
	}
	if tot, u := memProc("MemTotal:       16000000 kB\nMemFree:  1000 kB\nMemAvailable:   4000000 kB\n"); tot != 16000000*1024 || u != 12000000*1024 {
		t.Errorf("/proc/meminfo: %d %d", tot, u)
	}
	m := montagesProc("/dev/sda2 /home ext4 rw 0 0\nproc /proc proc rw 0 0\n/dev/sda1 / ext4 rw 0 0\n/dev/loop3 /snap/core squashfs ro 0 0\n/dev/sdb1 /mnt/Mes\\040Photos ntfs rw 0 0\n/dev/sda1 /var/x ext4 rw 0 0\n/dev/nvme0n1p1 /boot/efi vfat rw 0 0\n")
	if len(m) != 3 || m[0] != [2]string{"/dev/sda1", "/"} || m[1][1] != "/home" || m[2][1] != "/mnt/Mes Photos" {
		t.Errorf("/proc/mounts: %v", m)
	}
	if d := disquesProc("   8       0 sda 100 0 2000 50 300 0 4000 80 0 1234 130 0 0 0 0\n   8       1 sda1 1 0 8 1 1 0 8 1 0 9 2\n"); d["sda"] != [3]uint64{1234, 2000 * 512, 4000 * 512} || d["sda1"][0] != 9 {
		t.Errorf("/proc/diskstats: %v", d)
	}
	r := reseauxProc("Inter-|   Receive\n face |bytes    packets\n    lo: 500 5 0 0 0 0 0 0 500 5 0 0 0 0 0 0\n  eth0: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0\nwlan0: 30 1 0 0 0 0 0 0 40 1 0 0 0 0 0 0\ndocker0: 9 1 0 0 0 0 0 0 9 1 0 0 0 0 0 0\n")
	if len(r) != 2 || r[0] != (reseauBrut{"eth0", "ethernet", 1000, 2000}) || r[1].genre != "wifi" {
		t.Errorf("/proc/net/dev: %+v", r)
	}
	ns := "Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll\n" +
		"lo0        16384 <Link#1>                        100     0      5000      100     0      5000     0\n" +
		"en0        1500  <Link#6>    a4:83:e7:00:00:01  2000     0    3000000    1500     0     400000     0\n" +
		"en0        1500  192.168.1     192.168.1.20     2000     -    3000000    1500     -     400000     -\n" +
		"en5        1500  <Link#9>                       0     0          0        0     0          0     0\n" +
		"utun0      1380  <Link#10>                      5     0        500        5     0        500     0\n"
	if r := reseauxNetstat(ns); len(r) != 1 || r[0].nom != "en0" || r[0].entree != 3000000 || r[0].sortie != 400000 {
		t.Errorf("netstat -ibn: %+v", r)
	}
	if p := portsMateriels("\nHardware Port: Wi-Fi\nDevice: en0\nEthernet Address: a4\n\nHardware Port: Thunderbolt Ethernet\nDevice: en5\n"); p["en0"] != "Wi-Fi" || genreReseau(p["en5"]) != "ethernet" || genreReseau(p["en0"]) != "wifi" {
		t.Errorf("networksetup: %v", p)
	}
	if u := vmStat("Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:      1000.\nPages active:     2000.\nPages wired down:  500.\nPages occupied by compressor: 100.\n"); u != 2600*16384 {
		t.Errorf("vm_stat: %d", u)
	}
}

// TestMachineVue: two readings become percentages and rates; a 32-bit counter that wrapped is counted across.
func TestMachineVue(t *testing.T) {
	t0 := time.Now()
	a := &lectureMachine{t: t0, cpuOccupe: 100, cpuTotal: 1000, disques: []disqueBrut{{nom: "C:", total: 100, libre: 40, occupe: 0, duree: 0, lus: 0, ecrits: 0, activiteConnue: true}},
		reseaux: []reseauBrut{{nom: "Wi-Fi", genre: "wifi", entree: 1<<32 - 1000, sortie: 0}, {nom: "Ethernet", genre: "ethernet"}}}
	b := &lectureMachine{t: t0.Add(2 * time.Second), cpuOccupe: 400, cpuTotal: 2000, coeurs: 8, ramTotal: 16, ramUtil: 4,
		disques: []disqueBrut{{nom: "C:", total: 100, libre: 40, occupe: 5_000_000, duree: 20_000_000, lus: 4000, ecrits: 2000, activiteConnue: true}, {nom: "D:", total: 50, libre: 50}},
		reseaux: []reseauBrut{{nom: "Wi-Fi", genre: "wifi", entree: 3000, sortie: 4000}, {nom: "Ethernet", genre: "ethernet"}}}
	v := vueMachine(a, b)
	if v.CPU != 30 || v.Coeurs != 8 || v.RAMUtil != 4 || len(v.Disques) != 2 || v.Disques[0].Activite != 25 || v.Disques[0].Lecture != 2000 ||
		v.Disques[1].Activite != -1 || v.Disques[1].Lecture != -1 || v.Reseaux[0].Nom != "Wi-Fi" || v.Reseaux[0].Entree != 2000 || v.Reseaux[0].Sortie != 2000 {
		t.Errorf("view: %+v", v)
	}
	if v := vueMachine(nil, b); v.CPU != -1 || v.Reseaux[0].Entree != 0 {
		t.Errorf("first reading: %+v", v)
	}
}

// TestMachineReelle: this computer's own reading (whatever its system) holds something sensible.
func TestMachineReelle(t *testing.T) {
	a := lireMachine()
	time.Sleep(300 * time.Millisecond)
	b := lireMachine()
	v := vueMachine(&a, &b)
	if v.Coeurs < 1 {
		t.Fatalf("no processor: %+v", v)
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}
	if v.CPU < 0 || v.CPU > 100 || v.RAMTotal == 0 || v.RAMUtil == 0 || v.RAMUtil > v.RAMTotal || len(v.Disques) == 0 ||
		slices.ContainsFunc(v.Disques, func(d DisqueVue) bool { return d.Total == 0 || d.Libre > d.Total || d.Activite > 100 }) {
		t.Errorf("this computer: %+v", v)
	}
	t.Logf("%s: CPU %.0f %%, RAM %d/%d MB, disks %+v, links %+v", v.Systeme, v.CPU, v.RAMUtil>>20, v.RAMTotal>>20, v.Disques, v.Reseaux)
}
