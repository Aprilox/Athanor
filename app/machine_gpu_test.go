package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNomGPU(t *testing.T) {
	for avant, apres := range map[string]string{"Intel(R) UHD Graphics": "Intel UHD Graphics", "NVIDIA GeForce RTX 5060 Laptop GPU": "NVIDIA GeForce RTX 5060 Laptop GPU",
		"AMD Radeon(TM) Graphics": "AMD Radeon Graphics", "  Apple   M2 ": "Apple M2"} {
		if g := nomGPU(avant); g != apres {
			t.Errorf("%q → %q, want %q", avant, g, apres)
		}
	}
}

// TestUtilisationMoteurs: per engine the processes add up, per card its busiest engine counts (the Task Manager's
// figure); the physical parts of one card are one card; an instance of another counter is ignored.
func TestUtilisationMoteurs(t *testing.T) {
	v := utilisationMoteurs(map[string]float64{
		"pid_100_luid_0x00000000_0x0001391E_phys_0_eng_0_engtype_3D":           20,
		"pid_200_luid_0x00000000_0x0001391E_phys_0_eng_0_engtype_3D":           15,
		"pid_100_luid_0x00000000_0x0001391E_phys_0_eng_5_engtype_VideoDecode":  30,
		"pid_300_luid_0x00000000_0x00014317_phys_0_eng_0_engtype_3D":           70,
		"pid_301_luid_0x00000000_0x00014317_phys_1_eng_0_engtype_3D":           50, // phys_1: another physical engine of the same card
		"pid_302_luid_0x00000000_0x00014350_phys_0_eng_1_engtype_Copy":         0,
		"luid_0x00000000_0x0001391E_phys_0":                                    99, // a "GPU Adapter Memory" instance
		"pid_400_luid_0x00000000_0x0001AAAA_phys_0_eng_0_engtype_3D_extra_ext": 140,
	})
	want := map[string]float64{"luid_0x00000000_0x0001391e": 35, "luid_0x00000000_0x00014317": 70, "luid_0x00000000_0x00014350": 0, "luid_0x00000000_0x0001aaaa": 100}
	if len(v) != len(want) {
		t.Fatalf("got %v, want %v", v, want)
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s: %v, want %v (all: %v)", k, v[k], w, v)
		}
	}
	if k := cleAdaptateur("luid_0x00000000_0x0001391E_phys_0"); k != "luid_0x00000000_0x0001391e" {
		t.Errorf("memory instance key %q", k)
	}
}

func TestParNvidiaSmi(t *testing.T) {
	g := parNvidiaSmi("NVIDIA GeForce RTX 5060 Laptop GPU, 23, 1536, 8151\nTesla T4, [N/A], 0, 15360\n\n, 1, 2, 3\nbroken line\n")
	if len(g) != 2 {
		t.Fatalf("%+v", g)
	}
	if g[0].Nom != "NVIDIA GeForce RTX 5060 Laptop GPU" || g[0].Utilisation != 23 || g[0].VRAMUtil != 1536<<20 || g[0].VRAMTotal != 8151<<20 {
		t.Errorf("first card %+v", g[0])
	}
	if g[1].Nom != "Tesla T4" || g[1].Utilisation != -1 || g[1].VRAMTotal != 15360<<20 {
		t.Errorf("an unknown use must stay unknown: %+v", g[1])
	}
}

// TestGpusAmdgpu: a card of the amdgpu driver is read; a connector (card0-HDMI-A-1) and a card of another driver
// (no gpu_busy_percent) are not; without product_name the card still has a name.
func TestGpusAmdgpu(t *testing.T) {
	r := t.TempDir()
	ecrire := func(rel, c string) {
		f := filepath.Join(r, "sys", "class", "drm", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ecrire("card0/device/gpu_busy_percent", "42\n")
	ecrire("card0/device/mem_info_vram_used", "2147483648\n")
	ecrire("card0/device/mem_info_vram_total", "8589934592\n")
	ecrire("card0/device/product_name", "AMD Radeon RX 7600\n")
	ecrire("card0-HDMI-A-1/device/gpu_busy_percent", "99\n")
	ecrire("card1/device/vendor", "0x8086\n") // an Intel card: nothing to read
	ecrire("card2/device/gpu_busy_percent", "7\n")
	g := gpusAmdgpu(r)
	if len(g) != 2 {
		t.Fatalf("%+v", g)
	}
	if g[0].Nom != "AMD Radeon RX 7600" || g[0].Utilisation != 42 || g[0].VRAMUtil != 2<<30 || g[0].VRAMTotal != 8<<30 {
		t.Errorf("card0 %+v", g[0])
	}
	if g[1].Nom != "AMD Radeon (card2)" || g[1].Utilisation != 7 || g[1].VRAMTotal != 0 {
		t.Errorf("card2 %+v", g[1])
	}
}

func TestFamilleGPU(t *testing.T) {
	for entree, famille := range map[string]string{"AGXAcceleratorG14X": "apple", "AppleParavirtGPU": "apple", "Apple M3 Max": "apple",
		"AMDRadeonX6000_AMDNavi14GraphicsAccelerator": "amd", "sppci_vendor_amd": "amd", "sppci_vendor_ati": "amd", "ATI Radeon HD 6750M": "amd",
		"IntelAccelerator": "intel", "sppci_vendor_intel": "intel", "nvAccelerator": "nvidia", "NVIDIA GeForce GT 750M": "nvidia",
		"IOAcceleratorES": "", "Integrated graphics": ""} {
		if f := familleGPU(entree); f != famille {
			t.Errorf("%q → %q, want %q", entree, f, famille)
		}
	}
}

// system_profiler SPDisplaysDataType -json of an Intel Mac (its Intel card first) and of Apple silicon
const profilerIntelMac = `{"SPDisplaysDataType" : [
  {"_name" : "Intel UHD Graphics 630", "sppci_model" : "Intel UHD Graphics 630", "spdisplays_vendor" : "sppci_vendor_intel", "spdisplays_vram_shared" : "1536 MB", "sppci_bus" : "spdisplays_builtin"},
  {"_name" : "AMD Radeon Pro 5500M", "sppci_model" : "AMD Radeon Pro 5500M", "spdisplays_vendor" : "sppci_vendor_amd", "spdisplays_vram" : "8 GB", "sppci_bus" : "spdisplays_pcie_device"}]}`
const profilerApple = `{"SPDisplaysDataType" : [{"_name" : "Apple M2 Pro", "sppci_model" : "Apple M2 Pro", "spdisplays_vendor" : "sppci_vendor_Apple", "sppci_cores" : "19", "sppci_bus" : "spdisplays_builtin"}]}`

func TestParSystemProfiler(t *testing.T) {
	c := parSystemProfiler([]byte(profilerIntelMac))
	if len(c) != 2 || c[0] != (carteMac{famille: "intel", nom: "Intel UHD Graphics 630"}) || c[1] != (carteMac{famille: "amd", nom: "AMD Radeon Pro 5500M", vram: 8 << 30}) {
		t.Fatalf("Intel Mac: %+v", c)
	}
	if c := parSystemProfiler([]byte(profilerApple)); len(c) != 1 || c[0] != (carteMac{famille: "apple", nom: "Apple M2 Pro", coeurs: 19}) {
		t.Fatalf("Apple silicon: %+v", c)
	}
	if c := parSystemProfiler([]byte(`{"SPDisplaysDataType":[{"sppci_model":"X","spdisplays_vram":"4096 MB"}]}`)); len(c) != 1 || c[0].vram != 4<<30 {
		t.Fatalf("MB: %+v", c)
	}
	if parSystemProfiler([]byte("not json")) != nil {
		t.Fatal("a broken answer gives no card")
	}
}

// TestParIoreg: Apple silicon (its unified memory and its cores), an Intel Mac's two cards named from system_profiler
// though listed in another order (or from their maker before it answered), an old Mac's NVIDIA card.
func TestParIoreg(t *testing.T) {
	apple := `+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X, id 0x1000003a1, registered, matched, active, busy 0 (0 ms), retain 52>
    {
      "IOClass" = "AGXAcceleratorG14X"
      "model" = "Apple M2 Pro"
      "gpu-core-count" = 19
      "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=1622016000,"Tiler Utilization %"=3,"Renderer Utilization %"=11,"Device Utilization %"=12,"SplitSceneCount"=0,"In use system memory"=734003200}
    }
+-o IOAcceleratorES  <class IOAcceleratorES, id 0x100000789>
    {
      "IOClass" = "IOAcceleratorES"
    }
`
	g := parIoreg(apple, nil)
	if len(g) != 1 || g[0] != (GPUVue{Nom: "Apple M2 Pro", Utilisation: 12, Partagee: 734003200, Unifiee: true, Coeurs: 19}) {
		t.Fatalf("Apple silicon: %+v", g)
	}
	intelMac := `+-o AMDRadeonX6000_AMDNavi14GraphicsAccelerator  <class AMDRadeonX6000_AMDNavi14GraphicsAccelerator, id 0x100000456>
    {
      "PerformanceStatistics" = {"Device Utilization %"=4,"vramUsedBytes"=1073741824,"vramFreeBytes"=7516192768,"GPU Activity(%)"=4}
    }
+-o IntelAccelerator  <class IntelAccelerator, id 0x100000321>
    {
      "PerformanceStatistics" = {"Device Utilization %"=9,"In use system memory"=268435456}
    }
`
	g = parIoreg(intelMac, parSystemProfiler([]byte(profilerIntelMac)))
	if len(g) != 2 || g[0] != (GPUVue{Nom: "AMD Radeon Pro 5500M", Utilisation: 4, VRAMUtil: 1 << 30, VRAMTotal: 8 << 30}) ||
		g[1] != (GPUVue{Nom: "Intel UHD Graphics 630", Utilisation: 9, Partagee: 256 << 20}) {
		t.Fatalf("Intel Mac: %+v", g)
	}
	if g = parIoreg(intelMac, nil); len(g) != 2 || g[0].Nom != "AMD Radeon" || g[1].Nom != "Intel Graphics" || g[0].Unifiee || g[1].Unifiee {
		t.Fatalf("before system_profiler answered: %+v", g)
	}
	nvidia := `+-o nvAccelerator  <class nvAccelerator, id 0x100000999>
    {
      "PerformanceStatistics" = {"Device Utilization %"=30,"vramUsedBytes"=536870912,"vramFreeBytes"=1610612736}
    }
`
	if g = parIoreg(nvidia, []carteMac{{famille: "nvidia", nom: "NVIDIA GeForce GT 750M", vram: 2 << 30}}); len(g) != 1 || g[0] != (GPUVue{Nom: "NVIDIA GeForce GT 750M", Utilisation: 30, VRAMUtil: 512 << 20, VRAMTotal: 2 << 30}) {
		t.Fatalf("an old Mac's NVIDIA card: %+v", g)
	}
}

// TestGPUCetOrdinateur: this computer's real cards, read twice like the home page does: each has a name, its use
// is known from the second reading (Windows), within 0–100, and the view sends them. A computer without one passes.
func TestGPUCetOrdinateur(t *testing.T) {
	a := lireMachine()
	time.Sleep(1200 * time.Millisecond)
	b := lireMachine()
	v := vueMachine(&a, &b)
	if v.GPU == nil {
		t.Fatal("the view must send a list, empty or not")
	}
	for _, g := range v.GPU {
		t.Logf("%s: %.0f %%, VRAM %d MiB / %d MiB, shared %d MiB", g.Nom, g.Utilisation, g.VRAMUtil>>20, g.VRAMTotal>>20, g.Partagee>>20)
		if g.Nom == "" || g.Utilisation > 100 || g.Utilisation < -1 {
			t.Errorf("card %+v", g)
		}
		if runtime.GOOS == "windows" && g.Utilisation < 0 {
			t.Errorf("%s: use still unknown at the second reading", g.Nom)
		}
	}
	if len(v.GPU) == 0 {
		t.Log("no graphics card read on this computer")
	}
}
