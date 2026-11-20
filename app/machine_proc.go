package main

// The text formats Linux gives in /proc (machine_linux.go reads them; parsed here so that every system tests them).

import (
	"strconv"
	"strings"
)

// cpuProcStat: the "cpu" line of /proc/stat: busy and total ticks (idle and iowait are idle).
func cpuProcStat(texte string) (occupe, total uint64) {
	for _, l := range strings.Split(texte, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		for i, x := range f[1:] {
			n, _ := strconv.ParseUint(x, 10, 64)
			if i >= 7 { // steal counts, guest and guest_nice are already in user and nice
				if i == 7 {
					total += n
					occupe += n
				}
				continue
			}
			total += n
			if i != 3 && i != 4 {
				occupe += n
			}
		}
		return occupe, total
	}
	return 0, 0
}

// memProc: /proc/meminfo: total and used (total minus available), in bytes.
func memProc(texte string) (total, util uint64) {
	var dispo uint64
	for _, l := range strings.Split(texte, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			dispo = n * 1024
		}
	}
	if dispo > total {
		dispo = total
	}
	return total, total - dispo
}

// montagesProc: /proc/mounts: the mount points of real disks (a /dev/ device, never a loop, a snap or a CD), each
// device once, "/" first.
func montagesProc(texte string) (l [][2]string) {
	vus := map[string]bool{}
	for _, x := range strings.Split(texte, "\n") {
		f := strings.Fields(x)
		if len(f) < 3 || !strings.HasPrefix(f[0], "/dev/") || strings.HasPrefix(f[0], "/dev/loop") || strings.HasPrefix(f[1], "/snap/") ||
			strings.HasPrefix(f[1], "/boot") || f[2] == "squashfs" || f[2] == "iso9660" || vus[f[0]] {
			continue
		}
		vus[f[0]] = true
		m := [2]string{f[0], strings.ReplaceAll(f[1], `\040`, " ")}
		if m[1] == "/" {
			l = append([][2]string{m}, l...)
		} else {
			l = append(l, m)
		}
	}
	return l
}

// disquesProc: /proc/diskstats: per device name, the ms spent doing I/O and the bytes read and written (sectors of
// 512 bytes, whatever the disk's own size).
func disquesProc(texte string) map[string][3]uint64 {
	m := map[string][3]uint64{}
	for _, l := range strings.Split(texte, "\n") {
		f := strings.Fields(l)
		if len(f) < 14 {
			continue
		}
		lus, _ := strconv.ParseUint(f[5], 10, 64)
		ecrits, _ := strconv.ParseUint(f[9], 10, 64)
		ms, _ := strconv.ParseUint(f[12], 10, 64)
		m[f[2]] = [3]uint64{ms, lus * 512, ecrits * 512}
	}
	return m
}

// reseauxProc: /proc/net/dev: per link (the loopback left out), bytes received and sent.
func reseauxProc(texte string) []reseauBrut {
	var l []reseauBrut
	for _, x := range strings.Split(texte, "\n") {
		nom, reste, ok := strings.Cut(x, ":")
		nom = strings.TrimSpace(nom)
		f := strings.Fields(reste)
		if !ok || len(f) < 9 || nom == "lo" || strings.HasPrefix(nom, "docker") || strings.HasPrefix(nom, "veth") || strings.HasPrefix(nom, "br-") {
			continue
		}
		in, _ := strconv.ParseUint(f[0], 10, 64)
		out, _ := strconv.ParseUint(f[8], 10, 64)
		l = append(l, reseauBrut{nom: nom, genre: genreReseau(nom), entree: in, sortie: out})
	}
	return l
}

// reseauxNetstat: macOS's "netstat -ibn": per link, its <Link#> line's bytes in and out (the loopback, tunnels and
// links that moved nothing left out).
func reseauxNetstat(texte string) []reseauBrut {
	var l []reseauBrut
	var ent []string
	for _, x := range strings.Split(texte, "\n") {
		f := strings.Fields(x)
		if len(f) > 0 && f[0] == "Name" {
			ent = f
			continue
		}
		if len(ent) == 0 || len(f) < len(ent)-1 || !strings.HasPrefix(f[2], "<Link#") {
			continue
		}
		col := func(nom string) uint64 {
			i := indexOf(ent, nom)
			if len(f) == len(ent)-1 && i > 3 { // a link without an address: one column less
				i--
			}
			if i < 0 || i >= len(f) {
				return 0
			}
			n, _ := strconv.ParseUint(f[i], 10, 64)
			return n
		}
		in, out := col("Ibytes"), col("Obytes")
		if f[0] == "lo0" || strings.HasPrefix(f[0], "utun") || strings.HasPrefix(f[0], "gif") || strings.HasPrefix(f[0], "stf") || in+out == 0 {
			continue
		}
		l = append(l, reseauBrut{nom: f[0], genre: genreReseau(f[0]), entree: in, sortie: out})
	}
	return l
}

func indexOf(l []string, x string) int {
	for i, y := range l {
		if y == x {
			return i
		}
	}
	return -1
}

// portsMateriels: macOS's "networksetup -listallhardwareports": device → its port's name (Wi-Fi, Ethernet…).
func portsMateriels(texte string) map[string]string {
	m := map[string]string{}
	var port string
	for _, l := range strings.Split(texte, "\n") {
		if p, ok := strings.CutPrefix(l, "Hardware Port: "); ok {
			port = strings.TrimSpace(p)
		} else if d, ok := strings.CutPrefix(l, "Device: "); ok && port != "" {
			m[strings.TrimSpace(d)] = port
		}
	}
	return m
}

// vmStat: macOS's "vm_stat": the memory in use as Activity Monitor counts it (active, wired and compressed pages).
func vmStat(texte string) uint64 {
	page := uint64(4096)
	var pages uint64
	for _, l := range strings.Split(texte, "\n") {
		if i := strings.Index(l, "page size of "); i >= 0 {
			if n, err := strconv.ParseUint(strings.Fields(l[i+len("page size of "):])[0], 10, 64); err == nil {
				page = n
			}
			continue
		}
		nom, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), "."), 10, 64)
		switch strings.TrimSpace(nom) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			pages += n
		}
	}
	return pages * page
}
