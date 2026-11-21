package main

// This computer's graphics cards for the home page: each one's name, how busy it is and its video memory (VRAM).
// Read by machine_*.go (Windows: the system's GPU counters, the ones of the Task Manager; Linux: nvidia-smi and
// the amdgpu files; macOS: ioreg); what each system gives is turned into GPUVue here, on every system, for the tests.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GPUVue: a value the system does not give is -1 (Utilisation) or 0 (memory).
type GPUVue struct {
	Nom         string  `json:"nom"`
	Utilisation float64 `json:"utilisation"`        // %, its busiest engine (3D, video, copy…), -1 unknown
	VRAMUtil    uint64  `json:"vram_util"`          // bytes of its own memory used
	VRAMTotal   uint64  `json:"vram_total"`         // bytes of its own memory, 0 unknown or none (integrated)
	Partagee    uint64  `json:"partagee,omitempty"` // bytes of the computer's memory it uses (integrated, Apple)
}

// nomGPU: a card's name without the makers' (R) and (TM): "Intel(R) UHD Graphics" → "Intel UHD Graphics".
func nomGPU(nom string) string {
	for _, m := range []string{"(R)", "(r)", "(TM)", "(tm)", "®", "™"} {
		nom = strings.ReplaceAll(nom, m, "")
	}
	return strings.Join(strings.Fields(nom), " ")
}

// cleAdaptateur: "luid_0x00000000_0x0001391E" in lower case from a GPU counter's instance or a DXGI LUID; the
// physical adapters of one linked adapter (phys_0, phys_1) count as one.
func cleAdaptateur(instance string) string {
	i := strings.Index(instance, "luid_")
	if i < 0 {
		return ""
	}
	s := strings.ToLower(instance[i:])
	if j := strings.Index(s, "_phys"); j >= 0 {
		s = s[:j]
	}
	return s
}

// utilisationMoteurs: the "GPU Engine" counter's instances (pid_…_luid_…_phys_0_eng_3_engtype_3D: one per process
// and engine) added up per engine, then each adapter's busiest engine, as the Task Manager shows it.
func utilisationMoteurs(valeurs map[string]float64) map[string]float64 {
	moteurs := map[[2]string]float64{} // (luid_…_phys_N, engine number): one physical engine
	for inst, v := range valeurs {
		i, j := strings.Index(inst, "luid_"), strings.Index(inst, "_eng_")
		if i < 0 || j < i {
			continue
		}
		eng, _, _ := strings.Cut(inst[j+5:], "_")
		moteurs[[2]string{strings.ToLower(inst[i:j]), eng}] += v
	}
	res := map[string]float64{}
	for k, v := range moteurs {
		c := cleAdaptateur(k[0])
		res[c] = max(res[c], borne(v))
	}
	return res
}

// parNvidiaSmi: the lines of nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total
// --format=csv,noheader,nounits (MiB); "[N/A]" or "[Not Supported]" is unknown.
func parNvidiaSmi(sortie string) []GPUVue {
	var res []GPUVue
	for _, l := range strings.Split(sortie, "\n") {
		c := strings.Split(l, ",")
		if len(c) != 4 || strings.TrimSpace(c[0]) == "" {
			continue
		}
		g := GPUVue{Nom: strings.TrimSpace(c[0]), Utilisation: -1}
		if u, err := strconv.ParseFloat(strings.TrimSpace(c[1]), 64); err == nil {
			g.Utilisation = borne(u)
		}
		mio := func(s string) uint64 { n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64); return n << 20 }
		g.VRAMUtil, g.VRAMTotal = mio(c[2]), mio(c[3])
		res = append(res, g)
	}
	return res
}

// gpusAmdgpu: the AMD cards of /sys/class/drm (racine: "/" or a test's folder): gpu_busy_percent and the VRAM
// counters of the amdgpu driver; the other drivers write none of them.
func gpusAmdgpu(racine string) []GPUVue {
	var res []GPUVue
	cartes, _ := filepath.Glob(filepath.Join(racine, "sys", "class", "drm", "card*"))
	for _, c := range cartes {
		if strings.Contains(filepath.Base(c), "-") { // card0-HDMI-A-1: a connector of card0
			continue
		}
		dev := filepath.Join(c, "device")
		lire := func(f string) string { b, _ := os.ReadFile(filepath.Join(dev, f)); return strings.TrimSpace(string(b)) }
		occupe := lire("gpu_busy_percent")
		if occupe == "" {
			continue
		}
		g := GPUVue{Nom: lire("product_name"), Utilisation: -1}
		if g.Nom == "" {
			g.Nom = "AMD Radeon (" + filepath.Base(c) + ")"
		}
		if u, err := strconv.ParseFloat(occupe, 64); err == nil {
			g.Utilisation = borne(u)
		}
		g.VRAMUtil, _ = strconv.ParseUint(lire("mem_info_vram_used"), 10, 64)
		g.VRAMTotal, _ = strconv.ParseUint(lire("mem_info_vram_total"), 10, 64)
		res = append(res, g)
	}
	return res
}

var (
	reIoregModele   = regexp.MustCompile(`"model" = "([^"]+)"`)
	reIoregNombre   = func(cle string) *regexp.Regexp { return regexp.MustCompile(`"` + regexp.QuoteMeta(cle) + `"=(\d+)`) }
	reIoregOccupe   = reIoregNombre("Device Utilization %")
	reIoregPartagee = reIoregNombre("In use system memory")
	reIoregVRAMUtil = reIoregNombre("vramUsedBytes")
	reIoregVRAMLib  = reIoregNombre("vramFreeBytes")
)

// parIoreg: the IOAccelerator entries of ioreg -r -d 1 -w 0 -c IOAccelerator, one per graphics card: Apple's
// (unified memory: the part it uses) or an AMD card of an Intel Mac (its VRAM).
func parIoreg(sortie string) []GPUVue {
	var res []GPUVue
	for _, e := range strings.Split(sortie, "+-o ")[1:] {
		g := GPUVue{Nom: "GPU", Utilisation: -1}
		if m := reIoregModele.FindStringSubmatch(e); m != nil {
			g.Nom = m[1]
		}
		nombre := func(re *regexp.Regexp) (uint64, bool) {
			m := re.FindStringSubmatch(e)
			if m == nil {
				return 0, false
			}
			n, err := strconv.ParseUint(m[1], 10, 64)
			return n, err == nil
		}
		if u, ok := nombre(reIoregOccupe); ok {
			g.Utilisation = borne(float64(u))
		}
		g.Partagee, _ = nombre(reIoregPartagee)
		if u, ok := nombre(reIoregVRAMUtil); ok {
			l, _ := nombre(reIoregVRAMLib)
			g.VRAMUtil, g.VRAMTotal = u, u+l
		}
		if g.Utilisation < 0 && g.Partagee == 0 && g.VRAMTotal == 0 {
			continue // an entry without statistics
		}
		res = append(res, g)
	}
	return res
}
